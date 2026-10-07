package firstboot

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/configdrive"
	"github.com/vatesfr/vates-kube-os/internal/dashboard"
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// ConfigureOptions are what bringing a node up can be told, beyond its config
// drive. They exist for testing and for a person at a prompt.
type ConfigureOptions struct {
	// DriveDir reads the configuration from this directory instead of finding
	// the attached drive.
	DriveDir string
	// NodeIP uses this address instead of reading it from the network interface.
	NodeIP string
	// DryRun shows what would be done and changes nothing.
	DryRun bool
}

// Configure writes everything the kubelet needs, and nothing that needs the API
// server -- which does not exist yet.
//
// It is idempotent: running it again re-does the same work.
func Configure(opts ConfigureOptions) error {
	drive, err := openDrive(opts.DriveDir)
	if err != nil {
		return err
	}
	defer func() { _ = drive.Close() }() // deferred cleanup: the returned error is the one that matters

	cfg, nodeName, configSource, err := readConfig(drive)
	if err != nil {
		return err
	}
	exposeBinarySource(cfg)
	ip, err := resolveNodeIP(cfg, opts.NodeIP)
	if err != nil {
		return err
	}

	report(cfg, nodeName, ip, configSource)

	files, err := Files(cfg, drive, DefaultPaths(), nodeName, ip)
	if err != nil {
		return err
	}

	if opts.DryRun {
		fmt.Println("\n--dry-run: the following would be installed")
		for _, f := range files {
			// Only a digest is shown. A kubelet kubeconfig and its key are
			// credentials, and a dry run exists to be pasted into a report.
			sum := sha256.Sum256(f.Content)
			fmt.Printf("  %-9s %6d octets  %s  %s\n",
				f.Mode, len(f.Content), fmt.Sprintf("%x", sum[:6]), f.Path)
		}
		if cfg.Role == vatescfg.RoleMaster {
			fmt.Println("\n  then, in order:")
			for _, d := range RequiredDirs(cfg) {
				fmt.Printf("    mkdir -p %s\n", d)
			}
			if JoiningControlPlane(cfg, drive) {
				fmt.Println("    (no kubeadm at this stage)")
				fmt.Println("  from vates-bootstrap, once this node's kubelet is running, etcd can start")
				fmt.Println("  and its etcd member can catch up:")
				fmt.Printf("    %s\n",
					strings.Join(JoinControlPlaneCommand(KubeadmImage(cfg.Kubernetes.Version)), " "))
			} else {
				for _, phase := range KubeadmPhases {
					fmt.Printf("    %s\n",
						strings.Join(KubeadmPhaseCommand(KubeadmImage(cfg.Kubernetes.Version), phase), " "))
				}
				fmt.Println("  (a bootstrapping master generates its own certificate authority here)")
			}
		}
		fmt.Printf("\n  systemctl daemon-reload   (so the kubelet drop-in is read)\n")
		fmt.Printf("  (the kubelet image %s is already the one the unit names,\n",
			KubeletImageRef)
		fmt.Printf("   and its launcher fetches Kubernetes %s on first start)\n",
			cfg.Kubernetes.Version)
		fmt.Println("  then k8s-node.target, which is enabled at boot and Requires this unit,")
		fmt.Println("  starts the kubelet.")
		return nil
	}

	// The clock, before anything is signed with it: Apply mints this node's
	// certificates through kubeadm, and a clock off by minutes produces
	// certificates that are not yet valid and an etcd that refuses to run. One
	// shot and bounded -- a node that cannot reach its servers boots on the
	// hypervisor's clock rather than not booting.
	if err := SyncClock(cfg.NTPServers()); err != nil {
		fmt.Printf("time    : %v\n", err)
	} else {
		fmt.Printf("time    : set from %s\n", strings.Join(cfg.NTPServers(), ", "))
	}

	if err := Apply(cfg, drive, DefaultPaths(), nodeName, ip, OSRunner{}); err != nil {
		return err
	}
	// The file above is what systemd reads at the NEXT boot; this makes the name
	// true now, before the kubelet starts kube-vip, which names its
	// leader-election lock after the hostname. A cluster whose control planes
	// share one hostname has every kube-vip advertising the virtual IP.
	if err := syscall.Sethostname([]byte(nodeName)); err != nil {
		return fmt.Errorf("setting the hostname to %q: %w", nodeName, err)
	}
	fmt.Println("node configured.")
	return nil
}

// BootstrapOptions are what completing a control plane can be told.
type BootstrapOptions struct {
	// DriveDir reads the configuration from this directory instead of finding
	// the attached drive.
	DriveDir string
	// NodeIP uses this address instead of reading it from the network interface.
	NodeIP string
}

// BootstrapNode completes a control plane: the cluster-wide addons and the CNI,
// which need an API server and therefore cannot be part of Configure.
//
// It is idempotent: running it again re-does the same work.
func BootstrapNode(opts BootstrapOptions) error {
	drive, err := openDrive(opts.DriveDir)
	if err != nil {
		return err
	}
	defer func() { _ = drive.Close() }() // deferred cleanup: the returned error is the one that matters

	cfg, nodeName, _, err := readConfig(drive)
	if err != nil {
		return err
	}
	exposeBinarySource(cfg)
	ip, err := resolveNodeIP(cfg, opts.NodeIP)
	if err != nil {
		return err
	}

	fmt.Printf("bootstrapping %s (role %s)\n", nodeName, cfg.Role)
	if err := Bootstrap(cfg, drive, DefaultPaths(), nodeName, ip, OSRunner{}); err != nil {
		return err
	}
	fmt.Println("bootstrap complete.")
	return nil
}

// exposeBinarySource puts the Kubernetes version and the binary mirror into this
// process's environment.
//
// The containers vates-init starts take them BY NAME (`--env NAME`), so
// setting them once here is what lets kubectl and kubeadm reach the right binaries
// without the version being threaded through every command builder.
func exposeBinarySource(cfg *vatescfg.Config) {
	// The keys and values are ours and contain no '=' or NUL, which is the only
	// way Setenv can fail; there is nothing to report and nothing to do about it.
	_ = os.Setenv("KUBERNETES_VERSION", cfg.Kubernetes.Version)
	_ = os.Setenv("KUBERNETES_BINARY_BASE", cfg.BinaryBase())
}

// readConfig reads the node's configuration and its name from the drive.
//
// It returns the source the document was read from -- "user-data" or
// "vates-node.yaml" -- so the boot report can say which path this machine took,
// and so an error names it. The two are not equivalent: user-data is the CAPI
// path, the file is the direct-drive one, and "the node read the wrong source"
// is the first thing to rule out when a provider's payload seems ignored.
func readConfig(drive *configdrive.Drive) (*vatescfg.Config, string, string, error) {
	raw, source, err := drive.NodeConfig()
	if err != nil {
		return nil, "", "", fmt.Errorf("the config drive is attached but carries no node configuration: %w", err)
	}
	cfg, err := vatescfg.Load(raw)
	if err != nil {
		return nil, "", "", fmt.Errorf("reading the node configuration from %s: %w", source, err)
	}
	// The node name is stated by the bootstrap provider in the document when it
	// knows it -- under CAPI, that is the Machine's name, and it is the only
	// source on a hypervisor that writes instance-id but no local-hostname
	// (Xen Orchestra). When the document states no name, the drive's meta-data
	// local-hostname is used, which is where NoCloud puts it and what a
	// hand-built drive carries.
	nodeName := cfg.Node.Name
	if nodeName == "" {
		var nameErr error
		nodeName, nameErr = drive.NodeName()
		if nameErr != nil {
			return nil, "", "", nameErr
		}
	}
	return cfg, nodeName, source, nil
}

// resolveNodeIP returns the node's address, waiting for it if it has to be read
// from the network.
//
// Assigned by DHCP, so it is read at boot rather than configured. It may differ
// between boots, which is fine: the node's identity is its name, not its address.
func resolveNodeIP(cfg *vatescfg.Config, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	// The unit is ordered after network-online.target, but that means the network
	// is up, not that DHCP has finished: on a first boot the lease is usually
	// still being acquired.
	return WaitNodeIP(cfg.Network.Iface, 120*time.Second, func(f string, a ...any) {
		fmt.Printf("  "+f+"\n", a...)
	})
}

// openDrive mounts the attached config drive, or uses an explicit directory.
func openDrive(dir string) (*configdrive.Drive, error) {
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("--config-drive %s: %w", dir, err)
		}
		return configdrive.Open(dir), nil
	}
	d, err := configdrive.Find()
	if err != nil {
		return nil, fmt.Errorf("looking for the config drive: %w", err)
	}
	return d, nil
}

// report prints what the node is and what was decided, before anything is
// changed.
func report(cfg *vatescfg.Config, nodeName, ip, configSource string) {
	fmt.Printf("node    : %s (Kubernetes name)\n", nodeName)
	fmt.Printf("config  : %s\n", configSource)
	fmt.Printf("address : %s on %s (%s)\n", ip, cfg.Network.Iface, cfg.Network.Mode)
	fmt.Printf("role    : %s\n", cfg.Role)
	fmt.Printf("k8s     : %s\n", cfg.Kubernetes.Version)
	fmt.Printf("endpoint: %s\n", cfg.Cluster.ControlPlaneEndpoint)
	if cfg.Cluster.VIP.Enabled() {
		fmt.Printf("vip     : %s on %s (kube-vip)\n", cfg.Cluster.VIP.Address, cfg.VIPInterface())
	} else {
		fmt.Printf("vip     : none, the endpoint is expected to be reachable elsewhere\n")
	}
	for _, w := range cfg.Warnings() {
		fmt.Printf("warning : %s\n", w)
	}
}

// OSRunner performs the side effects on the real machine: the filesystem, the
// processes, and the console's activity line.
type OSRunner struct{}

func (OSRunner) Logf(format string, args ...any) {
	fmt.Printf("  "+format+"\n", args...)
}

// Progress publishes the node's phase for the console. It writes the phase file
// directly: it is a state the console reads, not a command to run.
func (OSRunner) Progress(text string) { dashboard.SetPhase(text) }

func (OSRunner) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (OSRunner) WriteFile(path string, mode os.FileMode, content []byte) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	// Written with the final mode from the start, then chmod'ed, because the
	// umask can only narrow the mode and the intent here is exact: a private key
	// must be 0600 and nothing else.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close() // the write error is the one to report; Close cannot add to it
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (OSRunner) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (OSRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return "."
}
