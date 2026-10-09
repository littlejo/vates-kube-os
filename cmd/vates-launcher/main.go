// Command vates-launcher fetches, verifies, caches and executes one Kubernetes
// binary.
//
// It is what the node runs instead of a packaged kubelet, under the names
// kubelet, kubeadm, kubectl and mounter. It is a separate binary so the host
// carries only the launcher -- with its HTTP client and nothing else -- instead
// of the whole multi-call binary. See docs/BINARIES.md.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/launcher"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

func main() {
	if err := dispatch(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "vates-launcher: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	// A subcommand is honoured under the binary's own name too. A component
	// name (kubelet, kubeadm...) is not a subcommand and falls through.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "launcher":
			return runLauncher(args[1:])
		}
	}

	// The symlink names carry the component: `kubelet`, `kubeadm`, `kubectl`,
	// `mounter` -- the four the rest of the system already expects.
	switch name {
	case "vates-launcher":
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return runLauncher(args)
	case "kubelet", "kubeadm", "kubectl", "mounter":
		return launcher.Launcher(name, args)
	}

	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "launcher":
		return runLauncher(rest)
	case "version", "--version", "-v":
		version.Print("vates-launcher")
		return nil
	default:
		return launcher.Launcher(verb, rest)
	}
}

// runLauncher turns `<component> [args...]` into a launcher call: the component
// is the first argument.
func runLauncher(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: vates-launcher <component> [args...]")
	}
	return launcher.Launcher(args[0], args[1:])
}

func usage() error {
	return fmt.Errorf("usage: vates-launcher <component> [args...]\n" +
		"       (or invoke it under a symlink: kubelet, kubeadm, kubectl, mounter)")
}
