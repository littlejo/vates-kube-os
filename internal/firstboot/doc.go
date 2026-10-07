// Package firstboot turns a validated vates-node.yaml plus the config drive into a
// configured node.
//
// The node's bring-up is staged so that as much of it as possible is pure: the
// set of files to write is computed first (Files), and Apply only performs I/O.
// That keeps the interesting logic -- which file, what mode, what content --
// testable without root, without a config drive and without touching a machine.
package firstboot
