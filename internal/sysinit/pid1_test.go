package sysinit

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/vatesfr/vates-kube-os/internal/firstboot"
)

// TestRebootAction pins the shutdown policy: which signal powers the machine
// off, which reboots it, and which does neither.
func TestRebootAction(t *testing.T) {
	cases := []struct {
		sig  syscall.Signal
		want string
	}{
		{syscall.SIGTERM, "poweroff"},
		{syscall.SIGINT, "poweroff"},
		{syscall.SIGPWR, "poweroff"},
		{syscall.SIGUSR2, "reboot"},
		{syscall.SIGCHLD, ""},
		{syscall.SIGHUP, ""},
	}
	for _, c := range cases {
		if got := rebootAction(c.sig); got != c.want {
			t.Errorf("rebootAction(%v) = %q, want %q", c.sig, got, c.want)
		}
	}
}

// TestPid1MountOrder guards the ordering constraint the table has: /dev must be
// mounted before its sub-mounts, and /sys before the filesystems mounted under
// it. The cgroup hierarchy is mounted separately (mountCgroupV2), after the
// table, so /sys must be in the table for it.
func TestPid1MountOrder(t *testing.T) {
	index := make(map[string]int, len(pid1Mounts))
	for i, m := range pid1Mounts {
		index[m.target] = i
	}
	if index["/dev"] > index["/dev/pts"] || index["/dev"] > index["/dev/shm"] {
		t.Error("/dev must be mounted before /dev/pts and /dev/shm")
	}
	if _, ok := index["/sys"]; !ok {
		t.Error("/sys must be in the mount table (the cgroup and bpf hierarchies are mounted under it)")
	}
	if index["/sys"] > index["/sys/fs/bpf"] {
		t.Error("/sys must be mounted before /sys/fs/bpf")
	}
}

// TestKubeletDefersToTheCloudProviderOnlyWhenAsked pins the mapping from the
// drop-in's CLOUD_PROVIDER to the kubelet's flag -- and its absence when no CCM
// is configured, which must read exactly as before.
func TestKubeletDefersToTheCloudProviderOnlyWhenAsked(t *testing.T) {
	env := map[string]string{"NODE_NAME": "n", "NODE_IP": "10.0.0.1"}
	if args := kubeletArgsFromEnv(env); slices.Contains(args, "--cloud-provider=external") {
		t.Errorf("--cloud-provider was set with no CLOUD_PROVIDER: %v", args)
	}

	env["CLOUD_PROVIDER"] = "external"
	if args := kubeletArgsFromEnv(env); !slices.Contains(args, "--cloud-provider=external") {
		t.Errorf("--cloud-provider is missing with CLOUD_PROVIDER=external: %v", args)
	}
}

// TestConsoleGetsItsModeFromTheDropIn pins a silent bug: the pid1 image has no
// systemd, so the DASHBOARD_MODE the drop-in carries reached nothing and
// `dashboard.mode: tui` on a machine with a screen kept drawing the graphical
// face. The mode the drop-in names must end up in the console's environment.
func TestConsoleGetsItsModeFromTheDropIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "10-mode.conf")
	if err := os.WriteFile(path, firstboot.ConsoleDropInFile("tui"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := consoleEnv(path); !slices.Contains(got, "DASHBOARD_MODE=tui") {
		t.Errorf("consoleEnv = %v, want DASHBOARD_MODE=tui", got)
	}
}

// TestConsoleKeepsItsDefaultWithoutADropIn is the other half: before the node
// has configured itself the drop-in is absent, and that must read as "say
// nothing" (nil), not as an empty mode the console would then have to reject.
func TestConsoleKeepsItsDefaultWithoutADropIn(t *testing.T) {
	if got := consoleEnv(filepath.Join(t.TempDir(), "absent.conf")); got != nil {
		t.Errorf("consoleEnv on a missing drop-in = %v, want nil", got)
	}
}
