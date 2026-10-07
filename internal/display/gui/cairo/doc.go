// Package cairo: drawing, bound at run time.
//
// This project's graphical console was first written with GTK, bound by hand
// through purego: two thousand lines of bindings for a screen that shows
// rectangles and text. Measured, that was not only long to maintain, it was
// expensive: GTK lays out a widget tree again on every scroll step, and the
// dashboard paid 51 to 67% of a core to scroll.
//
// Cairo draws, Pango lays out text, and there is NOTHING between the two and the
// screen. Scrolling becomes a memory copy. Measured: 0.5% of a core.
//
// This package knows only about drawing. It does not know what a node, a tile or
// an event is: that is internal/display/gui's job.
package cairo
