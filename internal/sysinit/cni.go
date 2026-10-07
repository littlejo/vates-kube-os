package sysinit

// Where a CNI's installer writes, and why those paths are overlays.
//
// The CNI plugins and their configuration are node-local state that the CNI's
// OWN installer writes at runtime: flannel's DaemonSet copies its plugin into
// /opt/cni/bin and its configuration into /etc/cni/net.d, and Cilium's agent
// does the same with its own. They cannot live on the read-only root, and they
// must not be baked into the image either -- the image names no CNI, precisely
// so the CNI can be changed.
//
// So /opt/cni/bin and /etc/cni/net.d are overlay filesystems. The image ships
// the generic containernetworking plugins (and, today, flannel's configuration)
// as the read-only LOWER layer; everything an installer writes lands in an UPPER
// layer on /var, where it persists across reboots. The root stays read-only: a
// mount point only has to exist, mounting over it writes nothing to it.
//
// The chosen CNI is thus free to add, replace or delete files here, exactly as
// it would on a distribution, and to change, the image does not have to.

import (
	"fmt"
	"os"
	"syscall"
)

// cniOverlay is one overlay: the image's lower layer, the mount point, and the
// upper and work directories on /var.
type cniOverlay struct {
	lower  string
	target string
	upper  string
	work   string
}

// cniOverlays are the two paths a CNI installer writes. Binaries and
// configuration are kept apart because that is how CNI separates them: the
// plugin goes in /opt/cni/bin, the network configuration in /etc/cni/net.d.
var cniOverlays = []cniOverlay{
	{
		lower:  "/usr/share/vates/cni/bin",
		target: "/opt/cni/bin",
		upper:  "/var/lib/vates/cni/bin",
		work:   "/var/lib/vates/cni/bin.work",
	},
	{
		lower:  "/usr/share/vates/cni/net.d",
		target: "/etc/cni/net.d",
		upper:  "/var/lib/vates/etc/cni/net.d",
		work:   "/var/lib/vates/etc/cni/net.d.work",
	},
}

// mountCNIOverlays mounts the two overlays, once, after /var is up.
//
// Best-effort like the rest of the mount path: a machine whose overlays do not
// mount keeps the image's plugins and configuration (it would lack the plugin a
// CNI installs at runtime), which is a degraded but visible state rather than a
// boot that stops.
func mountCNIOverlays() {
	for _, o := range cniOverlays {
		if _, err := os.Stat(o.lower); err != nil {
			kmsg("cni: no lower layer at %s (%v); leaving %s as shipped", o.lower, err, o.target)
			continue
		}
		// The work directory must be empty, and an overlay does not always get
		// to clean it -- a hard power-off leaves it dirty, and the next mount
		// fails with "overlay: workdir is not empty". It holds nothing worth
		// keeping, so it is recreated every boot.
		if err := os.RemoveAll(o.work); err != nil {
			kmsg("cni: clear workdir %s: %v", o.work, err)
		}
		for _, d := range []string{o.upper, o.work} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				kmsg("cni: mkdir %s: %v", d, err)
				continue
			}
		}
		opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", o.lower, o.upper, o.work)
		if err := syscall.Mount("overlay", o.target, "overlay", 0, opts); err != nil {
			kmsg("cni: overlay %s: %v", o.target, err)
			continue
		}
		kmsg("cni: %s mounted as an overlay (lower %s, upper %s)", o.target, o.lower, o.upper)
	}
}
