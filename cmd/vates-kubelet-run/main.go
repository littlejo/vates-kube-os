// Command vates-kubelet-run starts the kubelet container under containerd.
//
// It is a separate binary because it is the only process that needs containerd's
// client library -- by far the heaviest dependency in the tree. Keeping it out
// of PID 1, the console and the API means none of them initializes containerd.
// See docs/BINARIES.md.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/kubeletrun"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

func main() {
	if err := dispatch(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "vates-kubelet-run: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "kubelet-run":
			return kubeletrun.Run(args[1:])
		}
	}

	if name == "vates-kubelet-run" {
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return kubeletrun.Run(args)
	}
	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "kubelet-run":
		return kubeletrun.Run(rest)
	case "version", "--version", "-v":
		version.Print("vates-kubelet-run")
		return nil
	default:
		return fmt.Errorf("unknown command %q", verb)
	}
}

func usage() error {
	return fmt.Errorf("usage: vates-kubelet-run [args...]\n" +
		"       (or invoke it under the symlink vates-kubelet-run)")
}
