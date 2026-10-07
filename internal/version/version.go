// Package version holds the version of the whole vates binary.
//
// Each face keeps its own version string for its own `--version`, because the
// image build gates on those exact outputs; this one is what a `version`
// subcommand prints and names the binary as a whole.
package version

import "fmt"

// Version is the version of the vates binaries.
const Version = "0.1.0"

// Requested reports whether args ask for the version: `version`, `--version` or
// `-v`, and nothing else.
func Requested(args []string) bool {
	return len(args) == 1 && (args[0] == "version" || args[0] == "--version" || args[0] == "-v")
}

// Print writes "<name> <version>" to stdout.
func Print(name string) {
	fmt.Printf("%s %s\n", name, Version)
}
