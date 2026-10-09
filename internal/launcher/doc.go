// Package launcher fetches, verifies, caches and executes one Kubernetes binary.
//
// It is what the node runs instead of a packaged kubelet: the image is built
// once and serves any supported Kubernetes version, the version is chosen per
// machine in vates-node.yaml and reaches this program through the environment.
// It is reached by the four names the rest of the system already expects --
// kubelet, kubeadm, kubectl, mounter -- as symlinks to the one vates binary.
package launcher
