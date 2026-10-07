// Package console is what the machine shows on its console: the `vates-console`
// face that chooses between the text and graphical dashboards, the
// `vates-dashboard` face that draws or prints a snapshot, and the mock used to
// look at the graphical design without a cluster.
//
// It is the runtime half of the screen; the VT primitives it shares with the
// boot sequence live in the tty subpackage. What is displayed is collected by
// internal/dashboard and drawn by internal/display/{tui,gui}.
package console
