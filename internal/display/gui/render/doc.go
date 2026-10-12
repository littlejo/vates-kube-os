// Package render draws the console with fogleman/gg.
//
// gg paints rectangles, rounded rectangles and paths onto an image.RGBA. Text
// is laid out here: the colour-span markup the dashboard emits is decoded into
// runs of (text, colour), measured and wrapped with golang.org/x/image/font and
// the pure-Go freetype. The finished image is blitted, with the B/R swap
// XRGB8888 wants, into the mapped DRM buffer on Present.
//
// This package knows only about drawing. It does not know what a node, a tile
// or an event is: that is internal/display/gui's job.
package render
