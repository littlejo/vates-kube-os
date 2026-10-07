// Command vates-api is the node's management API.
//
// It is a separate binary so the API server -- which runs continuously and
// listens on the network -- carries grpc, protobuf and TLS and nothing else:
// not PID 1's mounts, not containerd, not the renderers. See docs/BINARIES.md.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/api"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

func main() {
	if err := dispatch(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "vates-api: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "api":
			return api.Run(args[1:])
		}
	}

	if name == "vates-api" {
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return api.Run(args)
	}
	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "api":
		return api.Run(rest)
	case "version", "--version", "-v":
		version.Print("vates-api")
		return nil
	default:
		return fmt.Errorf("unknown command %q", verb)
	}
}

func usage() error {
	return fmt.Errorf("usage: vates-api [--listen :50000] [--ca <ca.crt>] [--ca-key <ca.key>]\n" +
		"       (or invoke it under the symlink vates-api)")
}
