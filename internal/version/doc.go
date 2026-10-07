// Package version holds the version of the whole vates binary.
//
// Each face keeps its own version string for its own `--version`, because the
// image build gates on those exact outputs; this one is what `vates version`
// prints and names the binary as a whole.
package version
