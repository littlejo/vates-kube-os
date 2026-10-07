// Command vates-sysinit is PID 1 and the boot sequence, plus the `vates-init`
// configuration face it runs in process.
//
// It is a separate binary from the console and the API so that PID 1 -- which
// runs from power-on to power-off -- does not carry grpc, the Kubernetes API
// client, or containerd. See docs/BINARIES.md for the measurements behind that.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/firstboot"
	"github.com/vatesfr/vates-kube-os/internal/sysinit"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

func main() {
	if err := dispatch(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "vates-sysinit: %v\n", err)
		os.Exit(1)
	}
}

// dispatch routes one invocation by argv[0], then by subcommand.
func dispatch(argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	// A subcommand is honoured under the binary's own name too.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "sysinit":
			return sysinit.Pid1(args[1:])
		case "splash":
			return sysinit.Splash(args[1:])
		case "init":
			return firstboot.Command(args[1:])
		}
	}

	// The symlink names first: a symlink is how the system names a verb.
	switch name {
	case "vates-sysinit":
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return sysinit.Pid1(args)
	case "vates-splash":
		return sysinit.Splash(args)
	case "vates-init":
		return firstboot.Command(args)
	case "init":
		// The kernel's default when no init= is given: /sbin/init, a symlink
		// to this binary. As PID 1 it supervises; invoked by hand it is the
		// same as `vates-init`.
		if os.Getpid() == 1 {
			return sysinit.Pid1(args)
		}
		return firstboot.Command(args)
	}

	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "sysinit":
		return sysinit.Pid1(rest)
	case "splash":
		return sysinit.Splash(rest)
	case "init":
		return firstboot.Command(rest)
	case "version", "--version", "-v":
		version.Print("vates-sysinit")
		return nil
	default:
		return fmt.Errorf("unknown command %q", verb)
	}
}

func usage() error {
	return fmt.Errorf("usage: vates-sysinit {sysinit|init|splash|version} [args...]\n" +
		"       (or invoke it under a symlink: vates-sysinit, vates-init, vates-splash, init)")
}
