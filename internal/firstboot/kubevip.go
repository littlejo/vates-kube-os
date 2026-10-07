package firstboot

import (
	"github.com/vatesfr/vates-kube-os/internal/configdrive"
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// KubeVIPImage is the kube-vip release used for the control plane's virtual IP.
//
// Pinned rather than floating: this manifest is rendered at boot from a config
// file, and a moving tag would mean two nodes brought up a week apart can be
// running different kube-vip builds.
const KubeVIPImage = "ghcr.io/kube-vip/kube-vip:v1.0.0"

// KubeVIPManifest renders the static pod that owns the control plane's virtual
// IP.
//
// The document itself lives in templates/kube-vip.yaml.tmpl, where it can be
// read as YAML and compared against the manifest kube-vip's own generator
// produces. This function only supplies the values.
//
// hostKubeconfigPath is the HOST path of the kubeconfig kube-vip uses to reach
// the API server for leader election.
//
// Only the host side is configurable. kube-vip mounts it at a fixed
// /etc/kubernetes/admin.conf inside the container -- that is what its own
// manifest generator does, and what the running process reads -- so the mount
// path here is not a choice and changing it produces a kube-vip that can
// authenticate to nothing.
//
// The file must be one that already works when this pod starts, which during
// bootstrap is super-admin.conf and not admin.conf: since Kubernetes 1.29
// admin.conf is not usable until the cluster's cluster-admins binding exists,
// which happens later.
// nodeIP is the address of this node, and it is what kube-vip connects to the
// API server on. Not the loopback address: the API server's certificate lists
// the service address, the node's address and the VIP, but not 127.0.0.1, so
// using loopback fails certificate verification.
// image is the kube-vip image to run, already passed through the configured
// registry mirrors by the caller. It is a parameter rather than the constant read
// here so that the rewrite happens in one place, where the mirrors are known.
func KubeVIPManifest(vip, iface, port, hostKubeconfigPath, nodeIP, image string) ([]byte, error) {
	return render("kube-vip.yaml.tmpl", struct {
		Image              string
		VIP                string
		Port               string
		Interface          string
		Subnet             string
		HostKubeconfigPath string
		NodeIP             string
	}{
		Image:              image,
		VIP:                vip,
		Port:               port,
		Interface:          iface,
		Subnet:             "32",
		HostKubeconfigPath: hostKubeconfigPath,
		NodeIP:             nodeIP,
	})
}

// kubeVIPContainerKubeconfig is where kube-vip expects its kubeconfig inside the
// container. Fixed by kube-vip, not by this project.
const kubeVIPContainerKubeconfig = "/etc/kubernetes/admin.conf"

// SuperAdminKubeconfig is the kubeconfig kube-vip must use while the cluster is
// still bootstrapping. See KubeVIPManifest.
const SuperAdminKubeconfig = "/etc/kubernetes/super-admin.conf"

// AdminKubeconfig is the kubeconfig kube-vip uses on a control plane that joined
// an existing cluster.
//
// The two control planes need different files, and the difference is not
// cosmetic. The bootstrapping one starts kube-vip before the cluster's
// cluster-admins binding exists, so only super-admin.conf works there. A joining
// one starts into an established cluster where admin.conf works -- and where
// super-admin.conf does not exist at all, because kubeadm writes that file only
// on the node that ran `kubeadm init`.
const AdminKubeconfig = "/etc/kubernetes/admin.conf"

// KubeVIPKubeconfigFor returns the kubeconfig kube-vip must use on this node.
//
// Measured, and it cost a failover: the joining control planes were given a
// manifest mounting /etc/kubernetes/super-admin.conf, the kubelet refused it --
//
//	hostPath type check failed: /etc/kubernetes/super-admin.conf is not a file
//
// -- and kube-vip never started on any node but the first. The VIP therefore had
// exactly one possible owner, and killing that node removed the control plane's
// address from the cluster entirely.
func KubeVIPKubeconfigFor(cfg *vatescfg.Config, drive *configdrive.Drive) string {
	if JoiningControlPlane(cfg, drive) {
		return AdminKubeconfig
	}
	return SuperAdminKubeconfig
}

// ManifestsDir is where the kubelet looks for static pods. It is fixed by
// Kubernetes, not by this project.
const ManifestsDir = "/etc/kubernetes/manifests"
