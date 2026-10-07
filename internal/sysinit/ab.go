package sysinit

// The A/B policy lives in internal/ab, shared with the management API -- which is
// how an operator drives it, because the node has no shell. PID 1 does only the
// one thing that must happen on the machine itself: bless the slot it came up on,
// so a good boot becomes permanent and a bad one is rolled back by systemd-boot.

import "github.com/vatesfr/vates-kube-os/internal/update/ab"

// blessBoot makes the running slot's boot entry permanent, at boot. Best-effort:
// a machine whose ESP cannot be mounted still runs, it just does not bless, and
// a trial entry would eventually fall back -- loudly, which is what we want.
func blessBoot() {
	if err := ab.Bless(); err != nil {
		kmsg("ab: bless: %v", err)
		return
	}
	kmsg("ab: the running slot is blessed (the boot is permanent)")
}
