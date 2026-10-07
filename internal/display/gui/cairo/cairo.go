package cairo

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Cairo surface formats. RGB24 is what a 32-bit DRM screen buffer wants: three
// colour bytes and one ignored.
const (
	FormatRGB24  = 1
	FormatARGB32 = 0
)

// Pango wrap modes.
const (
	WrapWord     = 0
	WrapChar     = 1
	WrapWordChar = 2
)

// Pango ellipsize modes.
const (
	EllipsizeNone   = 0
	EllipsizeStart  = 1
	EllipsizeMiddle = 2
	EllipsizeEnd    = 3
)

// Pango measures text in units of 1/1024 of a pixel.
const pangoScale = 1024

// A Font is a family and a size, resolved once.
type Font struct{ desc uintptr }

type api struct {
	// cairo
	imageSurfaceForData func(unsafe.Pointer, int, int, int, int) uintptr
	surfaceFlush        func(uintptr)
	surfaceMarkDirty    func(uintptr)
	surfaceDestroy      func(uintptr)
	imageSurfaceFromPNG func(string) uintptr
	surfaceStatus       func(uintptr) int
	create              func(uintptr) uintptr
	destroy             func(uintptr)
	setSourceRGB        func(uintptr, float64, float64, float64)
	setSourceSurface    func(uintptr, uintptr, float64, float64)
	paint               func(uintptr)
	rectangle           func(uintptr, float64, float64, float64, float64)
	fill                func(uintptr)
	stroke              func(uintptr)
	setLineWidth        func(uintptr, float64)
	arc                 func(uintptr, float64, float64, float64, float64, float64)
	moveTo              func(uintptr, float64, float64)
	lineTo              func(uintptr, float64, float64)
	closePath           func(uintptr)
	clip                func(uintptr)
	save                func(uintptr)
	restore             func(uintptr)
	translate           func(uintptr, float64, float64)
	scale               func(uintptr, float64, float64)
	surfaceWidth        func(uintptr) int
	surfaceHeight       func(uintptr) int

	// pangocairo and pango
	createLayout       func(uintptr) uintptr
	showLayout         func(uintptr, uintptr)
	layoutSetMarkup    func(uintptr, string, int)
	layoutSetWidth     func(uintptr, int)
	layoutSetWrap      func(uintptr, int)
	layoutSetEllipsize func(uintptr, int)
	layoutSetHeight    func(uintptr, int)
	layoutFontDesc     func(uintptr, uintptr)
	layoutPixelSize    func(uintptr, *int, *int)
	fontDescFromStr    func(string) uintptr
	fontDescFree       func(uintptr)

	objectUnref func(uintptr)
}

var a api

func load(lib string, bind func(handle uintptr)) {
	h, err := purego.Dlopen(lib, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(fmt.Sprintf("cairo: %s: %v", lib, err))
	}
	bind(h)
}

func reg(h uintptr, name string, fptr any) { purego.RegisterLibFunc(fptr, h, name) }

// loadLib is run once, on the first use of drawing, NOT in an init().
//
// This package is linked into the one binary the image carries, and that binary
// also runs as the kubelet launcher inside a container that has NO cairo. An
// eager load made that container panic at startup -- "libcairo.so.2: cannot open
// shared object file" -- before the launcher could say anything, so the node
// never started. Loading lazily means the libraries are opened only when
// something actually draws, which only the console ever does.
var loadLib sync.Once

func ensure() {
	loadLib.Do(func() {
		load("libcairo.so.2", func(h uintptr) {
			reg(h, "cairo_image_surface_create_for_data", &a.imageSurfaceForData)
			reg(h, "cairo_surface_flush", &a.surfaceFlush)
			reg(h, "cairo_surface_mark_dirty", &a.surfaceMarkDirty)
			reg(h, "cairo_surface_destroy", &a.surfaceDestroy)
			reg(h, "cairo_image_surface_create_from_png", &a.imageSurfaceFromPNG)
			reg(h, "cairo_surface_status", &a.surfaceStatus)
			reg(h, "cairo_create", &a.create)
			reg(h, "cairo_destroy", &a.destroy)
			reg(h, "cairo_set_source_rgb", &a.setSourceRGB)
			reg(h, "cairo_set_source_surface", &a.setSourceSurface)
			reg(h, "cairo_paint", &a.paint)
			reg(h, "cairo_rectangle", &a.rectangle)
			reg(h, "cairo_fill", &a.fill)
			reg(h, "cairo_stroke", &a.stroke)
			reg(h, "cairo_set_line_width", &a.setLineWidth)
			reg(h, "cairo_arc", &a.arc)
			reg(h, "cairo_move_to", &a.moveTo)
			reg(h, "cairo_line_to", &a.lineTo)
			reg(h, "cairo_close_path", &a.closePath)
			reg(h, "cairo_clip", &a.clip)
			reg(h, "cairo_save", &a.save)
			reg(h, "cairo_restore", &a.restore)
			reg(h, "cairo_translate", &a.translate)
			reg(h, "cairo_scale", &a.scale)
			reg(h, "cairo_image_surface_get_width", &a.surfaceWidth)
			reg(h, "cairo_image_surface_get_height", &a.surfaceHeight)
		})
		load("libpangocairo-1.0.so.0", func(h uintptr) {
			reg(h, "pango_cairo_create_layout", &a.createLayout)
			reg(h, "pango_cairo_show_layout", &a.showLayout)
		})
		load("libpango-1.0.so.0", func(h uintptr) {
			reg(h, "pango_layout_set_markup", &a.layoutSetMarkup)
			reg(h, "pango_layout_set_width", &a.layoutSetWidth)
			reg(h, "pango_layout_set_wrap", &a.layoutSetWrap)
			reg(h, "pango_layout_set_ellipsize", &a.layoutSetEllipsize)
			reg(h, "pango_layout_set_height", &a.layoutSetHeight)
			reg(h, "pango_layout_set_font_description", &a.layoutFontDesc)
			reg(h, "pango_layout_get_pixel_size", &a.layoutPixelSize)
			reg(h, "pango_font_description_from_string", &a.fontDescFromStr)
			reg(h, "pango_font_description_free", &a.fontDescFree)
		})
		load("libgobject-2.0.so.0", func(h uintptr) {
			reg(h, "g_object_unref", &a.objectUnref)
		})
	})
}

// Surface is a cairo image, here laid over the screen's own memory.
type Surface struct{ h uintptr }

// SurfaceOn lays a cairo surface over an existing buffer: the screen's. That is
// the point of this whole package -- cairo has no copy to make in order to
// display, it writes where the controller reads.
func SurfaceOn(data unsafe.Pointer, width, height, stride int) Surface {
	ensure()
	return Surface{h: a.imageSurfaceForData(data, FormatRGB24, width, height, stride)}
}

// PNG loads an image from a file. Cairo reads the PNG itself: no image library
// on top.
func PNG(path string) Surface { ensure(); return Surface{h: a.imageSurfaceFromPNG(path)} }

func (s Surface) Valid() bool { return s.h != 0 && a.surfaceStatus(s.h) == 0 }
func (s Surface) Width() int  { return a.surfaceWidth(s.h) }
func (s Surface) Height() int { return a.surfaceHeight(s.h) }
func (s Surface) Destroy()    { a.surfaceDestroy(s.h) }

// Context is the drawing state: the current colour, the current line.
type Context struct{ h uintptr }

func New(s Surface) *Context { ensure(); return &Context{h: a.create(s.h)} }

func (c *Context) Free() { a.destroy(c.h) }

// Flush tells cairo that the memory is about to be touched by something else --
// a scroll that copies pixels, for instance.
func (c *Context) Flush(s Surface)     { a.surfaceFlush(s.h) }
func (c *Context) MarkDirty(s Surface) { a.surfaceMarkDirty(s.h) }

func (c *Context) RGB(r, g, b float64) { a.setSourceRGB(c.h, r, g, b) }

func (c *Context) Rect(x, y, w, h float64) { a.rectangle(c.h, x, y, w, h) }
func (c *Context) Fill()                   { a.fill(c.h) }

// Stroke draws the outline of the current path: that is what gives a tile its
// hairline, one pixel wide, the way CSS does.
func (c *Context) Stroke(width float64) {
	a.setLineWidth(c.h, width)
	a.stroke(c.h)
}

func (c *Context) MoveTo(x, y float64) { a.moveTo(c.h, x, y) }
func (c *Context) LineTo(x, y float64) { a.lineTo(c.h, x, y) }
func (c *Context) Clip()               { a.clip(c.h) }

// ClosePath closes the current path, which a filled shape needs: an open path is
// filled as if a line joined its ends anyway, but the STROKE would run the
// closing line twice and thicken one side of an arrow.
func (c *Context) ClosePath() { a.closePath(c.h) }

// Save and Restore bracket a clip, so that what is drawn inside a panel does not
// spill onto the rest of the screen.
func (c *Context) Save()    { a.save(c.h) }
func (c *Context) Restore() { a.restore(c.h) }

// Rounded is a rectangle with round corners: four arcs and four short sides.
func (c *Context) Rounded(x, y, w, h, r float64) {
	if r > h/2 {
		r = h / 2
	}
	if r > w/2 {
		r = w / 2
	}
	const q = 1.5707963267948966 // pi/2
	a.moveTo(c.h, x+r, y)
	a.arc(c.h, x+w-r, y+r, r, -q, 0)
	a.arc(c.h, x+w-r, y+h-r, r, 0, q)
	a.arc(c.h, x+r, y+h-r, r, q, 2*q)
	a.arc(c.h, x+r, y+r, r, 2*q, 3*q)
	a.closePath(c.h)
}

// NewFont resolves a family and a size once: a face is chosen, not renegotiated
// on every line.
func NewFont(description string) Font { ensure(); return Font{desc: a.fontDescFromStr(description)} }

func (f Font) Free() { a.fontDescFree(f.desc) }

// Text draws text with its wrapping, and returns the height it took.
//
// The text is PANGO MARKUP: that is what carries colour, and it avoids style
// tags -- the only alternative, gtk_text_buffer_create_tag, is a variadic C
// function, which purego cannot call.
func (c *Context) Text(f Font, markup string, x, y, wrapWidth int) int {
	l := a.createLayout(c.h)
	defer a.objectUnref(l)
	a.layoutFontDesc(l, f.desc)
	a.layoutSetMarkup(l, markup, -1)
	if wrapWidth > 0 {
		a.layoutSetWidth(l, wrapWidth*pangoScale)
		a.layoutSetWrap(l, WrapWordChar)
	}
	var w, h int
	a.layoutPixelSize(l, &w, &h)
	a.moveTo(c.h, float64(x), float64(y))
	a.showLayout(c.h, l)
	return h
}

// Measure is how tall a line of text will be, without drawing it.
func (c *Context) Measure(f Font, markup string, wrapWidth int) int {
	w, h := c.MeasureSize(f, markup, wrapWidth)
	_ = w
	return h
}

// MeasureSize is both dimensions of a laid-out run of text.
func (c *Context) MeasureSize(f Font, markup string, wrapWidth int) (int, int) {
	l := a.createLayout(c.h)
	defer a.objectUnref(l)
	a.layoutFontDesc(l, f.desc)
	a.layoutSetMarkup(l, markup, -1)
	if wrapWidth > 0 {
		a.layoutSetWidth(l, wrapWidth*pangoScale)
		a.layoutSetWrap(l, WrapWordChar)
	}
	var w, h int
	a.layoutPixelSize(l, &w, &h)
	return w, h
}

// TextLine draws ONE line, cut off with an ellipsis rather than wrapped or left
// to run off the edge. It is what a tile value and a hostname need: a name long
// enough to push the layout wider than the screen says nothing useful, while an
// ellipsis says that something is there.
//
// Pango needs three things together for this -- a width, an ellipsize mode, and
// a height of zero, which means exactly one line.
func (c *Context) TextLine(f Font, markup string, x, y, width int) int {
	l := a.createLayout(c.h)
	defer a.objectUnref(l)
	a.layoutFontDesc(l, f.desc)
	a.layoutSetMarkup(l, markup, -1)
	a.layoutSetWidth(l, width*pangoScale)
	a.layoutSetWrap(l, WrapWordChar)
	a.layoutSetEllipsize(l, EllipsizeEnd)
	a.layoutSetHeight(l, 0)
	var w, h int
	a.layoutPixelSize(l, &w, &h)
	a.moveTo(c.h, float64(x), float64(y))
	a.showLayout(c.h, l)
	return h
}

// DrawSurface places an image (the wordmark) inside a box, scaling it without
// distorting it.
//
// Scaling is done with the drawing transform, NOT by giving the source a
// destination rectangle: cairo_set_source_surface lays the image down at its own
// size, so a 2658-pixel-wide wordmark drawn into a 161-pixel box shows only its
// top-left corner -- which, on a logo, is transparent. That is a blank header
// that looks like a missing file rather than a missing transform.
func (c *Context) DrawSurface(s Surface, x, y, w, h float64) {
	if !s.Valid() {
		return
	}
	sw, sh := float64(s.Width()), float64(s.Height())
	if sw <= 0 || sh <= 0 {
		return
	}
	scale := w / sw
	if t := h / sh; t < scale {
		scale = t
	}
	a.save(c.h)
	a.translate(c.h, x, y)
	a.scale(c.h, scale, scale)
	a.setSourceSurface(c.h, s.h, 0, 0)
	a.paint(c.h)
	a.restore(c.h)
}
