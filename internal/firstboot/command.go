package firstboot

import (
	"flag"
	"fmt"
	"strings"
)

// initVersion is printed by `vates-init --version`. The image build runs that as
// a hard gate: a binary built for the wrong architecture, or one that cannot
// start, would otherwise be discovered only on a machine at first boot.
const initVersion = "0.1.0"

// Command is the `vates-init` face: it dispatches the two subcommands that bring
// a node up from its config drive.
//
// They are two moments that cannot be one:
//
//	configure   writes everything the kubelet needs and is run before the
//	            kubelet starts
//	bootstrap   completes a control plane once its API server answers, which can
//	            only happen after the kubelet has started it
//
// Both are idempotent: running either again re-does the same work. The work
// itself is in this package; this is the command surface over it.
func Command(argv []string) error {
	// The first argument may name a subcommand. One that starts with a dash is a
	// flag of the default subcommand, so that `vates-init --dry-run` keeps
	// working.
	cmd := "configure"
	args := argv
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "configure":
		return runConfigure(args)
	case "bootstrap":
		return runBootstrap(args)
	default:
		return fmt.Errorf("unknown command %q: expected configure or bootstrap", cmd)
	}
}

func runConfigure(argv []string) error {
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	driveDir := fs.String("config-drive", "", "read configuration from this directory instead of finding the attached drive (for testing)")
	nodeIPFlag := fs.String("node-ip", "", "use this address instead of reading it from the network interface")
	dryRun := fs.Bool("dry-run", false, "show what would be done and change nothing")
	showVer := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *showVer {
		fmt.Printf("vates-init %s\n", initVersion)
		return nil
	}
	return Configure(ConfigureOptions{
		DriveDir: *driveDir,
		NodeIP:   *nodeIPFlag,
		DryRun:   *dryRun,
	})
}

func runBootstrap(argv []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	driveDir := fs.String("config-drive", "", "read configuration from this directory instead of finding the attached drive (for testing)")
	nodeIPFlag := fs.String("node-ip", "", "use this address instead of reading it from the network interface")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	return BootstrapNode(BootstrapOptions{
		DriveDir: *driveDir,
		NodeIP:   *nodeIPFlag,
	})
}
