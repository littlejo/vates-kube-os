package sysinit

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// TestKubeletRunArgs pins the `ctr run` command line the kubelet container is
// started with: the flag that makes pod volumes reach the host
// (--rootfs-propagation=rshared), the host namespaces, and the image/id/command
// ordering `ctr run` expects.
func TestKubeletRunArgs(t *testing.T) {
	kubelet := []string{"/usr/local/bin/kubelet", "--config=/etc/kubelet/kubelet.conf"}
	args := kubeletRunArgs(kubelet)

	tests := []struct {
		name string
		want string
	}{
		{"removed on exit", "--rm"},
		{"privileged", "--privileged"},
		{"host network", "--net-host"},
		{"host PID namespace", "pid:/proc/1/ns/pid"},
		{"rootfs propagation", "--rootfs-propagation=rshared"},
		{"the kubelet root is recursively shared", "type=bind,src=/var/lib/kubelet,dst=/var/lib/kubelet,options=rbind:rshared"},
		{"the image", firstboot.KubeletImageRef},
		{"the container id", kubeletContainerID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Contains(args, tt.want) {
				t.Errorf("kubeletRunArgs() is missing %q\nargs: %v", tt.want, args)
			}
		})
	}

	// It must run in this system's own namespace, not the CRI's.
	if got := slices.Index(args, kubeletNamespace); got < 1 || args[got-1] != "-n" {
		t.Errorf("the namespace %q is not passed with -n: %v", kubeletNamespace, args)
	}
	// `ctr run [IMAGE] ID [COMMAND ARGS...]`: the kubelet command must follow
	// the container id, verbatim.
	id := slices.Index(args, kubeletContainerID)
	if id < 0 || id+1 >= len(args) || args[id+1] != kubelet[0] {
		t.Errorf("the kubelet command must follow the container id: id=%d args=%v", id, args)
	}
}

// TestKubeletMounts pins how each host path is mounted, and that an optional
// mount whose source does not exist is dropped -- runc fails a bind whose
// source is missing, which would take the whole container down.
func TestKubeletMounts(t *testing.T) {
	specs := kubeletMountSpecs()
	index := make(map[string]bool, len(specs))
	for _, s := range specs {
		index[s] = true
	}

	tests := []struct {
		name string
		spec string
	}{
		{"the kubelet root is recursively shared", "type=bind,src=/var/lib/kubelet,dst=/var/lib/kubelet,options=rbind:rshared"},
		{"the kubelet config is read-only", "type=bind,src=/etc/kubelet,dst=/etc/kubelet,options=rbind:ro"},
		{"the cluster PKI is writable", "type=bind,src=/etc/kubernetes,dst=/etc/kubernetes,options=rbind"},
		{"the binary cache is writable", "type=bind,src=/var/lib/vates/kubernetes,dst=/var/lib/vates/kubernetes,options=rbind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !index[tt.spec] {
				t.Errorf("mount %q is missing from %v", tt.spec, specs)
			}
		})
	}

	t.Run("a missing optional source is dropped", func(t *testing.T) {
		for _, m := range kubeletContainerMounts {
			if !m.optional {
				continue
			}
			if _, err := os.Stat(m.src); err == nil {
				continue
			}
			for _, s := range specs {
				if strings.Contains(s, "src="+m.src+",") {
					t.Errorf("optional %s has no source but was rendered: %s", m.src, s)
				}
			}
		}
	})
}
