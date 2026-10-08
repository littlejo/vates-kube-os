package firstboot

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/configdrive"
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// JoinEndpointTimeout is how long to wait for the control plane endpoint to
// answer before running `kubeadm join`.
const JoinEndpointTimeout = 10 * time.Minute

// waitForEndpoint waits until something answers on the control plane endpoint.
//
// A joining control plane runs `kubeadm join` during its own configuration,
// before it has a kubelet, and that join has to read the cluster's configuration
// from the cluster. If the endpoint is not yet reachable -- the VIP has only just
// been claimed, or the bridge has not learned the address -- kubeadm spends its
// whole discovery budget and gives up with
//
//	failed to request the cluster-info ConfigMap: client rate limiter Wait
//	returned an error: context deadline exceeded
//
// which reads like a rate limit rather than like an address nobody answers on.
func waitForEndpoint(hostPort string, timeout time.Duration, r Runner) error {
	deadline := time.Now().Add(timeout)
	for attempt := 0; ; attempt++ {
		conn, err := net.DialTimeout("tcp", hostPort, 5*time.Second)
		if err == nil {
			_ = conn.Close() // a probe: the error is the dial's, and Close cannot inform it
			if attempt > 0 {
				r.Logf("the control plane endpoint answered after %d attempt(s)", attempt+1)
			}
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the control plane endpoint %s did not answer within %s: %w", hostPort, timeout, err)
		}
		if attempt == 0 {
			r.Logf("waiting for the control plane endpoint %s", hostPort)
		}
		time.Sleep(5 * time.Second)
	}
}

// JoinConfigPath is where vates-init writes the JoinConfiguration.
const JoinConfigPath = "/etc/kubernetes/kubeadm-join.yaml"

// JoiningControlPlane reports whether this control plane joins an existing
// cluster rather than creating one.
//
// Two signals, because there are two ways to join, and either one means the node
// must not mint a certificate authority:
//
//   - the drive carries the cluster's PKI, so the certificates are provided
//   - vates-node.yaml carries a certificate key, so they are fetched from the cluster
//
// A control plane that RECEIVES the cluster's authority must never create its
// own. Two authorities in one cluster is not a degraded cluster, it is two
// clusters sharing an etcd, and the symptom -- nodes that authenticate to one API
// server and not the other -- appears long after the mistake. Getting this test
// wrong is therefore expensive in a way that is hard to trace back here.
func JoiningControlPlane(cfg *vatescfg.Config, drive *configdrive.Drive) bool {
	return drive.Has(pkiCACert) || cfg.Cluster.CertificateKey != ""
}

// bootstrapFor is the kubelet's --bootstrap-kubeconfig value: a path on a node
// that joins, and empty on the node that creates the cluster.
//
// The bootstrapping node is empty because it has no token until its own
// bootstrap phase runs, which is after its kubelet has started -- so it cannot
// bootstrap, and uses the kubelet.conf kubeadm wrote for it. A joining node has
// its token from the config drive, so its kubelet obtains its own certificate.
func bootstrapFor(cfg *vatescfg.Config, drive *configdrive.Drive) string {
	if JoiningControlPlane(cfg, drive) {
		return bootstrapKubeconfigPath
	}
	return ""
}

// pkiFilesForControlPlane are the pieces of the cluster's PKI a joining control
// plane has to receive.
//
// The certificate authorities and the service-account key pair are the shared
// material. Given those, kubeadm generates the node-specific certificates -- the
// API server's serving certificate with this node's address in it, this node's
// kubelet client certificate -- using the existing authorities. Without them it
// would create new ones.
//
// The kubelet's OWN certificate is not here: it is installed by MasterFiles from
// the drive on BOTH join paths, because kubeadm never writes it when
// kubelet-start is skipped. See MasterFiles.
//
// Modes follow the private-material rule: keys 0600, certificates 0644.
var pkiFilesForControlPlane = []struct {
	from string
	to   string
	mode os.FileMode
}{
	{pkiCACert, "ca.crt", 0o644},
	{"pki/ca.key", "ca.key", 0o600},
	{"pki/sa.key", "sa.key", 0o600},
	{"pki/sa.pub", "sa.pub", 0o644},
	{"pki/front-proxy-ca.crt", "front-proxy-ca.crt", 0o644},
	{"pki/front-proxy-ca.key", "front-proxy-ca.key", 0o600},
	{"pki/etcd/ca.crt", "etcd/ca.crt", 0o644},
	{"pki/etcd/ca.key", "etcd/ca.key", 0o600},
}

// controlPlanePKIFiles stages the received PKI.
func controlPlanePKIFiles(drive *configdrive.Drive, paths Paths) ([]File, error) {
	var files []File
	for _, p := range pkiFilesForControlPlane {
		content, err := drive.File(p.from)
		if err != nil {
			return nil, fmt.Errorf("a joining control plane requires %s on the config drive: %w", p.from, err)
		}
		files = append(files, File{
			Path:    filepath.Join(paths.Kubernetes, "pki", p.to),
			Mode:    p.mode,
			Content: content,
		})
	}
	return files, nil
}

// JoinConfiguration renders the document `kubeadm join` reads.
//
// The discovery token is how the joining node learns the cluster's CA hash, and
// with certificateKey it downloads the shared certificates. nodeRegistration
// carries the same CRI socket and node name as the init path, so a node
// configured by joining and one configured by bootstrapping are identical from
// Kubernetes' point of view.
func JoinConfiguration(cfg *vatescfg.Config, nodeName, nodeIP string) ([]byte, error) {
	return render("kubeadm-join.yaml.tmpl", struct {
		APIVersion           string
		NodeIP               string
		NodeName             string
		CRIEndpoint          string
		ControlPlaneEndpoint string
		Token                string
		CACertHash           string
		CertificateKey       string
		PatchesDir           string
	}{
		APIVersion:           KubeadmAPIVersion,
		NodeIP:               nodeIP,
		NodeName:             nodeName,
		CRIEndpoint:          CRIEndpoint,
		ControlPlaneEndpoint: cfg.Cluster.ControlPlaneEndpoint,
		Token:                cfg.Cluster.Token,
		CACertHash:           cfg.Cluster.CACertHash,
		CertificateKey:       cfg.Cluster.CertificateKey,
		PatchesDir:           PatchesDir,
	})
}

// JoinControlPlaneCommand is the command a joining control plane runs.
//
// It runs from the BOOTSTRAP stage, not from configuration, and that placement is
// the whole trick:
//
//   - `kubeadm join --control-plane` adds this machine's etcd member as a
//     LEARNER, then waits for it to catch up with the cluster, then promotes it
//     to a voting member.
//   - A learner catches up only once this machine's etcd is RUNNING, and etcd
//     runs as a static pod started by the kubelet.
//   - The kubelet is started by k8s-node.target, after vates-init's configure
//     step returns.
//
// Run from configuration, the promotion waits for a learner that cannot start,
// and retries with
//
//	etcdserver: can only promote a learner member which is in sync with leader
//
// Run from bootstrap, the kubelet is already up, so the manifests kubeadm writes
// are picked up at once, etcd starts, catches up, and is promoted.
//
// kubelet-start is still skipped: it writes a kubelet environment file and starts
// kubelet.service, and this system runs the kubelet in a container under its own
// unit. Skipping it is safe HERE, unlike in the earlier attempt to run the join
// before the kubelet -- there is already a kubelet.
func JoinControlPlaneCommand(image string) []string {
	// The binary cache and the version travel with the command: the image is
	// generic and the kubeadm it runs is fetched at run time, like the kubelet's.
	args := kubeadmContainers()
	args = append(args, binarySourceArgs()...)
	return append(args,
		image,
		ctrContainerID("kubeadm-join"),
		"/usr/local/bin/kubeadm", "join",
		"--config", JoinConfigPath,
		"--skip-phases=kubelet-start",
		// Five preflight checks are inapplicable here, for two reasons.
		//
		// The kubelet is already running by the time the join happens, and it
		// obtained its own credentials, where kubeadm expects to write and start
		// them itself:
		//
		//   FileAvailable--etc-kubernetes-kubelet.conf
		//     already there: the kubelet wrote it, through TLS bootstrap
		//   FileAvailable--etc-kubernetes-bootstrap-kubelet.conf
		//     already there: the config drive put it there, so the kubelet could
		//     bootstrap in the first place
		//   Port-10250
		//     "Port 10250 is in use", because it is: by this node's own kubelet
		//
		// And the kubelet container is a FROM-scratch image carrying only what
		// the kubelet itself shells out to, so two programs kubeadm looks for
		// are absent on purpose:
		//
		//   FileExisting-conntrack
		//     the kubelet does not run kube-proxy -- that is its own image. What
		//     the check asks about is nf_conntrack, and it is built into the
		//     kernel here.
		//   FileExisting-nsenter
		//     kubeadm uses it in the kubelet-start phase, which this join skips
		//     (`--skip-phases=kubelet-start`).
		//
		// Kubernetes 1.37 adds three more checks that assume a full host, and
		// which are just as inapplicable to this containerised join:
		//
		//   FileExisting-losetup, FileExisting-cp
		//     losetup (util-linux) and cp (coreutils) are absent on purpose: the
		//     kubelet container is FROM scratch and carries only what the kubelet
		//     shells out to. kubeadm 1.37 merely checks that they exist.
		//   SystemVerification
		//     its cgroup check wants a cgroupfs mount point in /proc/mounts,
		//     which a container does not have; the kernel-module checks it also
		//     covers are known-good for this image's kernel. Without this the
		//     join fails at preflight on 1.37 and a joining control plane never
		//     becomes one (it stays Ready, with no control-plane role).
		//
		// These and nothing else: any other preflight failure is still a failure.
		"--ignore-preflight-errors=FileAvailable--etc-kubernetes-kubelet.conf,FileAvailable--etc-kubernetes-bootstrap-kubelet.conf,Port-10250,FileExisting-conntrack,FileExisting-nsenter,FileExisting-losetup,FileExisting-cp,SystemVerification",
	)
}

// MarkControlPlaneCommand is the command that applies the control-plane role to
// this node, through kubeadm's own phase.
//
// It runs AFTER a successful join, and it exists because the mark the join
// performs is not dependable HERE. `kubeadm join --control-plane` does contain a
// `control-plane-join/mark-control-plane` subphase, but that subphase patches
// the node through kubeadm's apiclient.PatchNode, which gives up WITHOUT an
// error when the node does not yet carry the `kubernetes.io/hostname` label: it
// polls until its API-call timeout and then returns a nil error, so the join
// reports success with an unmarked node. Measured against kubeadm v1.31.0: a
// node created without that label is left untouched and kubeadm exits 0, having
// printed "[mark-control-plane] Marking the node ...".
//
// That matters here because this system starts the kubelet ITSELF, before the
// join, and skips kubeadm's kubelet-start -- so when the node is registered, and
// therefore whether the join's subphase finds it, is this system's timing and
// not kubeadm's. On the node that creates the cluster the same phase is run
// explicitly for the same reason (see BootstrapPhases); a joining control plane
// must not depend on winning a race the first one does not.
//
// The phase is the JOIN one, not the init one: the document on disk is a
// JoinConfiguration, and the join phase reads exactly it. It is idempotent, so
// re-applying what the join already applied costs nothing.
func MarkControlPlaneCommand(image string) []string {
	// The binary cache and the version travel with the command, exactly as they
	// do for the join: the image is generic and the kubeadm it runs is fetched
	// at run time.
	args := kubeadmContainers()
	args = append(args, binarySourceArgs()...)
	return append(args,
		image,
		ctrContainerID("kubeadm-mark-control-plane"),
		"/usr/local/bin/kubeadm", "join", "phase", "control-plane-join", "mark-control-plane",
		"--config", JoinConfigPath,
	)
}
