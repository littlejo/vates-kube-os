// Package render draws the console with fogleman/gg.
//
// The console used to be written against cairo and pango, bound with purego:
// a from-scratch build chain (freetype, fontconfig, glib, cairo, harfbuzz,
// pango, pixman, fribidi) whose whole purpose was to paint rectangles and text
// on a screen buffer. gg is pure Go, so that chain -- and the shared libraries
// it installs in the image -- can go.
//
// What replaces what:
//
//   - cairo's shapes and fills  -> gg (rectangles, rounded rectangles, paths)
//   - pango's text layout       -> a small layout here: the same Pango-markup
//     colour spans the dashboard already produces, measured and wrapped
//     with golang.org/x/image/font and the pure-Go freetype.
//   - cairo's screen surface     -> an image.RGBA that is blitted, with the
//     B/R swap XRGB8888 wants, into the mapped DRM buffer on Present.
//
// This package knows only about drawing. It does not know what a node, a tile
// or an event is: that is internal/display/gui's job.
package render
