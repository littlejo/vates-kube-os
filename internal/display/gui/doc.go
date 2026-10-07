// Package gui draws the dashboard straight onto the screen with cairo.
//
// The GTK window this replaces spent 83 MB and between half and two thirds of a
// core to scroll, because a toolkit lays out a widget tree again for every step
// of the wheel. Here there is no toolkit and no compositor: the mode is read from
// the screen, a buffer is mapped, cairo writes into it, and a scroll is a memory
// copy. Measured on the same machine: 23 MB and half a percent of a core.
//
// What is drawn is decided by internal/dashboard -- the tiles, the tones, the
// shape of an event -- and this package only decides where things go and how big
// they are. There is no second collector: the same one feeds the text console,
// the GTK window and this.
package gui
