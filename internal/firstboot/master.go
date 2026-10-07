package firstboot

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/configdrive"
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// KubeadmImage is the image the kubeadm phases are run from.
//
// It is the same image as the kubelet's, deliberately. kubeadm, kubelet, mounter
// and kubectl are downloaded together and share a library closure, so a second
// image would mean a second Containerfile and a second closure calculation for no
// benefit. The kubelet unit and this both name the same content.
//
// Running kubeadm from a container is what keeps "no Kubernetes binary on the
// host" true: in a kubeadm-based distribution kubeadm is installed on the
// machine itself.
// The version is accepted and ignored. It used to select a per-version image,
// and the callers still pass what they were configured with; keeping the
// parameter means the day an image has to be versioned again there is one place
// to change, and until then the image is the same for every version.
func KubeadmImage(version string) string {
	_ = version
	return KubeletImageRef
}

// KubeadmConfigPath is where vates-init writes the document the phases read.
//
// Not kubeadm's default name: being explicit means the file that produced a
// given cluster is unambiguous, and a stray kubeadm-config.yaml left by hand
// cannot silently take precedence.
const KubeadmConfigPath = "/etc/kubernetes/kubeadm.yaml"

// EtcdDataDir is where etcd keeps its data. It is a hostPath in the etcd static
// pod manifest, so it must exist before the kubelet starts that pod.
const EtcdDataDir = "/var/lib/etcd"

// RequiredDirs are the host directories that must exist before the kubelet is
// started, for either role.
//
// These are not conveniences. Each one is either a bind-mount source for the
// kubelet container -- and the runtime refuses a mount whose source does not
// exist, failing the unit with "statfs <path>: no such file or directory" -- or a
// hostPath in a static pod manifest. Nothing else in the image creates them.
//
// The manifests directory matters especially for a control plane: a manifest
// written after the kubelet has started is eventually picked up, but the VIP must
// be there from the first scan, because the API server is reachable at it.
func RequiredDirs(cfg *vatescfg.Config) []string {
	dirs := []string{
		KubeletRootDir,
		// /run is a tmpfs, so this does not survive a reboot and has to be
		// recreated every boot. flanneld writes the pod network's subnet file
		// into it, and the CNI plugin on the host reads it back; the directory
		// is also labelled for containers, and labelling a path that does not
		// exist fails -- which is how a fresh node failed to configure itself
		// while an already-configured one worked.
		"/run/flannel",
		// The kubelet reads its KubeletConfiguration from here through
		// --config, mounted read-only.
		"/etc/kubelet",
		// Where this node's drop-in is written.
		filepath.Dir(KubeletDropIn),
		// Where the launcher caches the Kubernetes binaries it fetches. A mount
		// source for the kubelet container, so the runtime refuses to start the
		// unit if it is absent.
		BinariesDir,
	}
	if cfg.Role == vatescfg.RoleMaster {
		dirs = append(dirs, EtcdDataDir, ManifestsDir)
	}
	return dirs
}

// MasterFiles computes what a bootstrapping control plane node needs on disk
// before the kubelet is started.
//
// "Bootstrapping" is the important word: this is the node that CREATES the
// cluster, so kubeadm generates the certificate authority and every certificate.
// Control plane nodes joining an existing cluster are a different case -- they
// must receive the cluster's PKI rather than mint their own, or there would be
// two certificate authorities and the cluster would split.
func MasterFiles(cfg *vatescfg.Config, drive *configdrive.Drive, paths Paths, nodeName, nodeIP string) ([]File, error) {
	kubeletConf, err := kubeletConfigFile(cfg, paths)
	if err != nil {
		return nil, err
	}

	var files []File
	if JoiningControlPlane(cfg, drive) {
		// A control plane joining an existing cluster is told how to reach it
		// rather than how to build one.
		if cfg.Cluster.Token == "" {
			return nil, fmt.Errorf("a joining control plane requires cluster.token: " +
				"kubeadm uses it for discovery, and the kubelet uses it to obtain its own certificate")
		}
		// The cluster CA is required on the drive, whether or not the drive also
		// carries the whole PKI: the kubelet verifies the API server with it
		// while it bootstraps, before kubeadm has fetched anything.
		if cfg.PKI.ClusterCA.Cert == "" && !drive.Has(pkiCACert) {
			return nil, fmt.Errorf("a joining control plane requires the cluster CA certificate " +
				"(pki.clusterCA.cert in vates-node.yaml, or pki/ca.crt on the config drive)")
		}
		if !drive.Has(pkiCAKey) && cfg.Cluster.CertificateKey == "" {
			return nil, fmt.Errorf("a joining control plane needs either the cluster's PKI " +
				"(pki/ca.key) or a certificate key in vates-node.yaml: without one, " +
				"kubeadm cannot obtain the control-plane certificates")
		}
		// Two ways to join, and the drive says which:
		//   - the cluster's PKI, keys included (pki/ca.key): kubeadm signs this
		//     node's certificates locally with those authorities
		//   - a certificate key (vates-node.yaml): kubeadm fetches them from the
		//     cluster
		if drive.Has(pkiCAKey) {
			pki, err := controlPlanePKIFiles(drive, paths)
			if err != nil {
				return nil, err
			}
			files = append(files, pki...)
		} else {
			content, err := clusterCACert(cfg, drive)
			if err != nil {
				return nil, err
			}
			files = append(files, File{
				Path:    filepath.Join(paths.Kubernetes, "pki", "ca.crt"),
				Mode:    0o644,
				Content: content,
			})
		}

		// The kubelet's credentials, on EITHER join path: it bootstraps its own
		// certificate from the token. kubeadm would write kubelet.conf through
		// its kubelet-start phase, which this system skips -- so without this the
		// containerised kubelet dies in a restart loop with
		//   invalid kubeconfig: stat /etc/kubernetes/kubelet.conf: no such file
		files = append(files, bootstrapKubeletConfFile(cfg, paths))

		joinCfg, err := JoinConfiguration(cfg, nodeName, nodeIP)
		if err != nil {
			return nil, err
		}
		files = append(files, File{
			Path:    JoinConfigPath,
			Mode:    0o600,
			Content: joinCfg,
		})
	} else {
		// A bootstrapping control plane normally generates the cluster CA. When
		// the provider states one in the document, the node REUSES it instead:
		// writing the files before kubeadm runs makes kubeadm keep them, so
		// every control plane shares one authority from the first machine -- the
		// model a CAPI control plane provider owns (it holds the CA, no node
		// does).
		if cfg.PKI.ClusterCA.Cert != "" {
			files = append(files, File{
				Path:    filepath.Join(paths.Kubernetes, "pki", "ca.crt"),
				Mode:    0o644,
				Content: ensureNewline(cfg.PKI.ClusterCA.Cert),
			})
			if cfg.PKI.ClusterCA.Key != "" {
				files = append(files, File{
					Path:    filepath.Join(paths.Kubernetes, "pki", "ca.key"),
					Mode:    0o600,
					Content: ensureNewline(cfg.PKI.ClusterCA.Key),
				})
			}
		}
		kubeadmCfg, err := KubeadmConfig(cfg, nodeName, nodeIP)
		if err != nil {
			return nil, err
		}
		files = append(files, File{
			Path:    KubeadmConfigPath,
			Mode:    0o600,
			Content: kubeadmCfg,
		})
	}

	files = append(files,
		kubeletConf,
		File{
			Path:    ConsoleDropIn,
			Mode:    0o644,
			Content: ConsoleDropInFile(string(cfg.DashboardModeValue())),
		},
		File{
			Path: KubeletDropIn,
			Mode: 0o644,
			Content: KubeletDropInFile(nodeName, nodeIP, cfg.Kubernetes.Version,
				cfg.BinaryBase(), bootstrapFor(cfg, drive), cfg.Cloud.Provider),
		},
	)

	// The virtual IP is owned by a static pod, so its manifest goes in the same
	// directory as etcd's and the API server's. On a BOOTSTRAPPING control plane
	// it has to be present before the kubelet's first scan of that directory,
	// because the API server is reachable at the VIP and kube-vip is what puts it
	// there.
	//
	// A JOINING control plane is the exception, and not for tidiness: the
	// kubeconfig kube-vip needs there is admin.conf, which kubeadm writes DURING
	// the join -- so a manifest written now would name a file that does not exist
	// yet and the kubelet would refuse the mount. JoiningControlPlane stages it
	// once the join has run. See KubeVIPKubeconfigFor.
	if cfg.Cluster.VIP.Enabled() && !JoiningControlPlane(cfg, drive) {
		m, err := KubeVIPManifest(cfg.Cluster.VIP.Address, cfg.VIPInterface(), "6443", KubeVIPKubeconfigFor(cfg, drive), nodeIP, cfg.ImageFor(KubeVIPImage))
		if err != nil {
			return nil, err
		}
		files = append(files, File{
			Path:    filepath.Join(ManifestsDir, "kube-vip.yaml"),
			Mode:    0o600,
			Content: m,
		})
	}

	return files, nil
}

// KubeadmPhaseCommand builds the command that runs one kubeadm init phase.
//
// The container gets the host's /etc/kubernetes and /var/lib/etcd because those
// are exactly what the phases produce and what the static pods later consume.
// --net=host is not needed by the phases themselves, but it keeps the container
// from needing its own network namespace for no benefit.
//
// The phases are run individually rather than through `kubeadm init` because
// `kubeadm init` also installs and starts a host kubelet, and this system runs
// the kubelet in a container.
//
// phase is a complete phase path such as "etcd local"; it is split into
// arguments here so the callers cannot forget that etcd has no `all`.
func KubeadmPhaseCommand(image, phase string) []string {
	args := kubeadmContainers()
	// kubeadm is fetched at run time, like the kubelet: the image is generic and
	// the version comes from vates-node.yaml, so the cache and the version travel
	// with every phase.
	args = append(args, binarySourceArgs()...)
	args = append(args, image, ctrContainerID("kubeadm"), "/usr/local/bin/kubeadm", "init", "phase")
	args = append(args, strings.Fields(phase)...)
	args = append(args, "--config", KubeadmConfigPath)
	return args
}
