// Package gui draws the dashboard straight onto the screen with fogleman/gg.
//
// There is no toolkit and no compositor: the mode is read from the screen, gg
// rasterizes into an image we own, the image is blitted into the mapped
// framebuffer, and a scroll is a memory copy -- no widget tree laid out again
// for every step of the wheel.
//
// What is drawn is decided by internal/dashboard -- the tiles, the tones, the
// shape of an event -- and this package only decides where things go and how big
// they are. There is no second collector: the same one feeds the text console
// and this.
package gui
