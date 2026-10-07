// Package tty gathers the virtual-console primitives the boot screen and the
// console share: whether the machine has a screen at all, taking the VT into and
// out of graphics mode, and loading the font the text dashboard draws with.
//
// It is a package of its own because two callers with no other business in
// common need it -- PID 1 and the splash during boot, and the console afterwards
// -- and neither should import the other.
package tty
