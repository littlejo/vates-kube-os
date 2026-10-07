package firstboot

import (
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// KubeadmAPIVersion is the kubeadm configuration API used.
//
// v1beta4 is the version in Kubernetes 1.31. It is stated explicitly rather than
// left to kubeadm's default so that a change of cluster version shows up as a
// parse error naming the field, instead of kubeadm silently reading a document
// written against an older schema.
const KubeadmAPIVersion = "kubeadm.k8s.io/v1beta4"

// KubeadmConfig renders the document kubeadm's phases read.
//
// The control plane is brought up by running kubeadm's individual phases
// (certs, kubeconfig, etcd, control-plane) rather than `kubeadm init`, because
// `kubeadm init` also installs and starts a host kubelet, and this system runs
// the kubelet in a container. The phases produce the certificates and the static
// pod manifests; the kubelet unit starts them.
//
// nodeIP is the address the API server binds. It is the node's own address and
// NOT the VIP: the VIP is owned by kube-vip and is only how clients reach
// whichever API server currently holds it, never an address this process listens
// on. Putting the VIP in advertiseAddress would make every API server claim an
// address it does not own.
func KubeadmConfig(cfg *vatescfg.Config, nodeName, nodeIP string) ([]byte, error) {
	return render("kubeadm.yaml.tmpl", struct {
		APIVersion           string
		NodeIP               string
		NodeName             string
		CRIEndpoint          string
		KubernetesVersion    string
		ControlPlaneEndpoint string
		PodSubnet            string
		ServiceSubnet        string
		DNSDomain            string
		// ImageRepository is empty unless a mirror is configured, and empty
		// means "do not mention it": kubeadm's own defaults are then used, which
		// is what a cluster with internet access needs.
		ImageRepository string
	}{
		APIVersion:           KubeadmAPIVersion,
		NodeIP:               nodeIP,
		NodeName:             nodeName,
		CRIEndpoint:          CRIEndpoint,
		KubernetesVersion:    cfg.Kubernetes.Version,
		ControlPlaneEndpoint: cfg.Cluster.ControlPlaneEndpoint,
		PodSubnet:            cfg.CNI.CIDR,
		// Stated explicitly rather than left to kubeadm's defaults, because the
		// kubelet is told the DNS address derived from them: the two must agree,
		// and agreement by two independent defaults is not agreement.
		ServiceSubnet: cfg.Cluster.ServiceCIDR,
		DNSDomain:     cfg.Cluster.DNSDomain,
		// Emitting all three of imageRepository, etcd and dns is deliberate.
		// kubeadm's own defaults put CoreDNS under {repository}/coredns/coredns
		// and etcd directly under the registry, so setting imageRepository alone
		// would move the control plane images and leave those two behind -- a
		// mirror would be configured, half the cluster would come from it, and
		// the other half would still try to reach the internet. Stated here, the
		// mirror's layout is ONE rule: every cluster image is {base}/{name}.
		ImageRepository: cfg.Registry.Kubernetes,
	})
}

// KubeadmPhases is the ordered list of kubeadm phases that bring up a
// bootstrapping control plane without touching a host kubelet.
//
// Each entry is a complete phase path, subcommand included, because they are
// not uniform: certs, kubeconfig and control-plane each take `all`, but etcd
// takes only `local` -- `kubeadm init phase etcd all` is not a command, and
// discovering that at boot would cost a failed control plane for no reason.
//
// Every phase is idempotent and safe to re-run, which matters because
// vates-init may be re-run after a failed boot.
//
//   - certs all         the cluster's certificate authority and every leaf
//   - kubeconfig all    the kubeconfigs, including super-admin.conf kube-vip needs
//   - etcd local        the etcd static pod manifest
//   - control-plane all the apiserver, controller-manager and scheduler manifests
//
// The addons (CoreDNS, kube-proxy) are deliberately absent: they need a
// reachable API server, which only exists once the kubelet has started these
// pods. They are applied in a later, separate step.
var KubeadmPhases = []string{
	"certs all",
	"kubeconfig all",
	"etcd local",
	"control-plane all",
}
