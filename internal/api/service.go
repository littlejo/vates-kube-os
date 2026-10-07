package api

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	apiv1 "github.com/vatesfr/vates-kube-os/proto/vates/api/v1"

	"github.com/vatesfr/vates-kube-os/internal/update/ab"
)

// The cluster's admin kubeconfigs, as kubeadm writes them.
//
// SuperAdminKubeconfig exists only on the node that ran `kubeadm init`.
// AdminKubeconfig exists on every control plane, including one that joined.
//
// The API serves whichever is present, and the distinction matters: a virtual
// IP lands on any control plane, so making the API depend on super-admin.conf
// alone would make it report a joining control plane as unready, and refuse to
// hand out a kubeconfig when the VIP happened to sit there.
const (
	SuperAdminKubeconfig = "/etc/kubernetes/super-admin.conf"
	AdminKubeconfig      = "/etc/kubernetes/admin.conf"
)

// clusterAdminKubeconfig is the most privileged kubeconfig this node holds.
func clusterAdminKubeconfig() (string, error) {
	return pickKubeconfig(SuperAdminKubeconfig, AdminKubeconfig)
}

// pickKubeconfig prefers the bootstrap's super-admin.conf and falls back to the
// admin.conf every control plane has. Paths are arguments so the choice can be
// tested without a cluster.
func pickKubeconfig(superAdmin, admin string) (string, error) {
	if fileExists(superAdmin) {
		return superAdmin, nil
	}
	if fileExists(admin) {
		return admin, nil
	}
	return "", fmt.Errorf("no cluster admin kubeconfig on this node (%s or %s)", superAdmin, admin)
}

// service implements VatesAPI by reading the node's own state. It is
// deliberately thin: every answer comes from a file Kubernetes itself wrote.
type service struct {
	apiv1.UnimplementedVatesAPIServer
	// joinMaterial, when set, produces fresh credentials for joining machines.
	// Nil on a worker, and on a control plane that has not bootstrapped.
	joinMaterial func(context.Context) (JoinMaterial, error)
}

func (s *service) GetStatus(_ context.Context, _ *apiv1.GetStatusRequest) (*apiv1.GetStatusResponse, error) {
	// Readiness is "this node holds the cluster's admin credential": the kubelet
	// has registered and kubeadm has written the kubeconfig. A node without
	// either is not serving a control plane yet.
	kubeconfig, err := clusterAdminKubeconfig()
	if err != nil {
		kubeconfig = ""
	}
	return &apiv1.GetStatusResponse{
		NodeName:          nodeName(),
		ControlPlane:      fileExists("/etc/kubernetes/pki/ca.key"),
		KubernetesVersion: kubernetesVersion(),
		ClusterEndpoint:   clusterEndpoint(kubeconfig),
		Ready:             kubeconfig != "",
	}, nil
}

// kubeletDropIn is where vates-init records the node's Kubernetes name.
const kubeletDropIn = "/etc/systemd/system/k8s-kubelet.service.d/10-node.conf"

// nodeName is the Kubernetes node name, NOT the OS hostname: the image is built
// with one hostname for every machine, and the node name is what the provider
// gave this one (it is passed to the kubelet as --hostname-override). Reporting
// os.Hostname() here would tell a provider its node is called "vates-build".
func nodeName() string {
	if b, err := os.ReadFile(kubeletDropIn); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Environment=NODE_NAME="); ok {
				if v = strings.TrimSpace(v); v != "" {
					return v
				}
			}
		}
	}
	name, _ := os.Hostname()
	return name
}

func (s *service) GetKubeconfig(_ context.Context, _ *apiv1.GetKubeconfigRequest) (*apiv1.GetKubeconfigResponse, error) {
	kubeconfig, err := clusterAdminKubeconfig()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"this node does not carry the cluster's admin kubeconfig: %v", err)
	}
	b, err := os.ReadFile(kubeconfig)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "reading %s: %v", kubeconfig, err)
	}
	return &apiv1.GetKubeconfigResponse{Kubeconfig: b}, nil
}

// GetJoinMaterial hands a joining machine what it needs. It is the SSH
// replacement for the one thing that used to require a shell: join credentials
// live only on the bootstrapped control plane, and this is the only channel to
// them outside CAPI, where the provider holds the CA instead.
func (s *service) GetJoinMaterial(ctx context.Context, _ *apiv1.GetJoinMaterialRequest) (*apiv1.GetJoinMaterialResponse, error) {
	if s.joinMaterial == nil {
		return nil, status.Error(codes.FailedPrecondition,
			"this node cannot issue join material: only a bootstrapped control plane holds the cluster CA")
	}
	m, err := s.joinMaterial(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "collecting join material: %v", err)
	}
	return &apiv1.GetJoinMaterialResponse{
		Token:          m.Token,
		CertificateKey: m.CertificateKey,
		CaCertHash:     m.CACertHash,
	}, nil
}

// GetBootSlots reports the A/B state -- which root the node is running and which
// entry loader.conf points at -- so an operator can see where a machine is before
// and after an update.
func (s *service) GetBootSlots(_ context.Context, _ *apiv1.GetBootSlotsRequest) (*apiv1.GetBootSlotsResponse, error) {
	st, err := ab.GetStatus()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "reading the boot entries: %v", err)
	}
	return &apiv1.GetBootSlotsResponse{
		Running:      st.Running,
		DefaultEntry: st.Default,
		Entries:      st.Entries,
	}, nil
}

// SetBootSlot points the next boot at a slot and marks its entry a trial. The
// caller must have WRITTEN that slot first -- this only flips the boot entry --
// and systemd-boot's boot counting rolls it back if it does not come up.
func (s *service) SetBootSlot(_ context.Context, req *apiv1.SetBootSlotRequest) (*apiv1.SetBootSlotResponse, error) {
	if req.GetSlot() != "a" && req.GetSlot() != "b" {
		return nil, status.Errorf(codes.InvalidArgument, "slot %q is not a or b", req.GetSlot())
	}
	if err := ab.Switch(req.GetSlot()); err != nil {
		return nil, status.Errorf(codes.Internal, "switching to slot %s: %v", req.GetSlot(), err)
	}
	return &apiv1.SetBootSlotResponse{}, nil
}

// authorize rejects a caller whose certificate is valid under the cluster CA but
// is not a management client. Every node certificate in the cluster is signed by
// the same CA, so without this a kubelet's own certificate could call the API.
//
// The accepted identity is an Organization of vates:admin, or system:masters
// (what the admin kubeconfig carries). The rule is intentionally narrow and
// lives here, in one place.
func authorize(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no peer information")
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "no verified client certificate")
	}
	cert := info.State.VerifiedChains[0][0]
	for _, org := range append(cert.Subject.Organization, cert.Issuer.Organization...) {
		if org == "vates:admin" || org == "system:masters" {
			return handler(ctx, req)
		}
	}
	return nil, status.Errorf(codes.PermissionDenied,
		"certificate %q is not a management client (want O=vates:admin or O=system:masters)", cert.Subject.CommonName)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// kubernetesVersion reports the version the node was told to run. vates-init
// exports it, so it is in the environment when the API runs as a service; the
// cache directory is the fallback.
func kubernetesVersion() string {
	if v := os.Getenv("KUBERNETES_VERSION"); v != "" {
		return v
	}
	entries, err := os.ReadDir("/var/lib/vates/kubernetes")
	if err != nil || len(entries) == 0 {
		return ""
	}
	return entries[0].Name()
}

// clusterEndpoint reads the API endpoint from an admin kubeconfig's server line.
// A one-line scan is enough; the file is four lines of YAML.
func clusterEndpoint(kubeconfigPath string) string {
	if kubeconfigPath == "" {
		return ""
	}
	b, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "server:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// nodeLogFiles are the node's own logs, in the order an operator wants them when
// bring-up failed: what PID 1 did, then each supervised service.
var nodeLogFiles = []string{
	"/var/lib/vates/pid1.log",
	"/var/log/vates/kubelet.log",
	"/var/log/vates/containerd.log",
	"/var/log/vates/vates-api.log",
}

// GetLogs returns the tail of each log. A node has no shell, so this is the only
// way an operator -- or `make cluster`, on failure -- can see why a machine did
// not come up. It answers even when the node never became Ready, which is
// exactly when it is needed.
func (s *service) GetLogs(_ context.Context, req *apiv1.GetLogsRequest) (*apiv1.GetLogsResponse, error) {
	lines := int(req.GetLines())
	if lines <= 0 {
		lines = 200
	}
	return &apiv1.GetLogsResponse{Logs: nodeLogs(lines)}, nil
}

func nodeLogs(lines int) string {
	var b strings.Builder
	for _, p := range nodeLogFiles {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "=== %s ===\n", p)
		b.WriteString(tailLines(string(data), lines))
		b.WriteString("\n\n")
	}
	if b.Len() == 0 {
		return "(no logs yet)\n"
	}
	return b.String()
}

// tailLines returns at most the last n lines of s, its final newline dropped.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
