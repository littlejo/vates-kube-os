package sysinit

import (
	"os"
	"os/exec"

	"github.com/vatesfr/vates-kube-os/internal/firstboot"
)

// startNTP runs the NTP daemon that keeps the clock true once boot is done.
//
// It is started after configure, which is what writes firstboot.NTPConfPath, and
// is a supervised child, so it is reaped and stopped with the machine. A missing
// daemon or a missing configuration is not an error: the node simply keeps the
// clock it booted with.
func startNTP() {
	if _, err := os.Stat(firstboot.NTPConfPath); err != nil {
		kmsg("ntp: no %s: %v", firstboot.NTPConfPath, err)
		return
	}
	if _, err := exec.LookPath("ntpd"); err != nil {
		kmsg("ntp: %v", err)
		return
	}
	if err := startChild("ntpd", "ntpd", "-n"); err != nil {
		kmsg("ntp: %v", err)
	}
}
