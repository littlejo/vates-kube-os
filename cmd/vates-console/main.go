// Command vates-console is the machine's screen: the `vates-console` face that
// chooses between the text and graphical dashboards, and the `vates-dashboard`
// face that draws or prints a snapshot.
//
// It is a separate binary so the long-running dashboard carries the renderers
// (cairo, DRM, bubbletea) and nothing else -- not the API, not containerd.
// See docs/BINARIES.md.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vatesfr/vates-kube-os/internal/console"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

func main() {
	if err := dispatch(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "vates-console: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	// A subcommand is honoured under the binary's own name too: the console
	// starts the graphical dashboard by exec'ing itself as
	// `vates-console dashboard --gui`.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "console":
			return console.Console(args[1:])
		case "dashboard":
			return console.Dashboard(args[1:])
		}
	}

	switch name {
	case "vates-console":
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return console.Console(args)
	case "vates-dashboard":
		if version.Requested(args) {
			version.Print(name)
			return nil
		}
		return console.Dashboard(args)
	}

	if len(args) == 0 {
		return usage()
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "console":
		return console.Console(rest)
	case "dashboard":
		return console.Dashboard(rest)
	case "version", "--version", "-v":
		version.Print("vates-console")
		return nil
	default:
		return fmt.Errorf("unknown command %q", verb)
	}
}

func usage() error {
	return fmt.Errorf("usage: vates-console {console|dashboard|version} [args...]\n" +
		"       (or invoke it under a symlink: vates-console, vates-dashboard)")
}
