package render

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"unsafe"
)

// Surface is a pixel buffer. There are two kinds:
//
//   - the SCREEN, made over the DRM buffer with SurfaceOn. gg draws into an
//     image.RGBA of its own, and the DRM memory is written from it on Flush:
//     the screen is XRGB8888 in memory (B, G, R, X), an image.RGBA is R, G, B,
//     A, so one channel swap happens on the way out. That copy is the price of
//     not sharing cairo's byte-order-encoded surface, and it is a few
//     megabytes a frame.
//
//   - an IMAGE loaded from a file (PNG), for the wordmark and the planet.
type Surface struct {
	img    *image.RGBA
	dst    []byte // the screen memory, nil for a loaded image
	stride int    // the screen's row pitch (dst); the image's is width*4
}

// SurfaceOn lays a drawing surface over the screen's own memory. It does not
// write there until Flush: gg needs a real image.RGBA to rasterize into.
func SurfaceOn(data unsafe.Pointer, width, height, stride int) Surface {
	if data == nil || width <= 0 || height <= 0 || stride < width*4 {
		return Surface{}
	}
	return Surface{
		img:    image.NewRGBA(image.Rect(0, 0, width, height)),
		dst:    unsafe.Slice((*byte)(data), stride*height),
		stride: stride,
	}
}

// NewSurface is an offscreen surface, not backed by any screen memory. It is
// what the preview renderer draws into.
func NewSurface(width, height int) Surface {
	if width <= 0 || height <= 0 {
		return Surface{}
	}
	return Surface{img: image.NewRGBA(image.Rect(0, 0, width, height))}
}

// SavePNG writes the surface as a PNG. It is for previews and tests.
func (s Surface) SavePNG(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return png.Encode(f, s.img)
}

// PNG loads an image from a file. A missing or unreadable file is not fatal:
// the surface comes back invalid and the caller draws without it.
func PNG(path string) Surface {
	f, err := os.Open(path)
	if err != nil {
		return Surface{}
	}
	defer func() { _ = f.Close() }()
	src, err := png.Decode(f)
	if err != nil {
		return Surface{}
	}
	img := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	return Surface{img: img}
}

func (s Surface) Valid() bool { return s.img != nil }
func (s Surface) Width() int  { return s.img.Bounds().Dx() }
func (s Surface) Height() int { return s.img.Bounds().Dy() }

// Stride is the image's row pitch, which is what a caller copying rectangles
// between buffers needs.
func (s Surface) Stride() int { return s.img.Stride }

// Pix is the image's pixels, R, G, B, A, row by row.
func (s Surface) Pix() []byte { return s.img.Pix }

// Destroy exists for the call sites the cairo surface had. Go's collector owns
// the memory now, so it is a no-op.
func (s Surface) Destroy() {}

// Flush writes the whole image into the screen buffer.
func (s Surface) Flush() { s.FlushRect(0, 0, s.Width(), s.Height()) }

// FlushRect writes one rectangle of the image into the screen buffer, swapping
// the red and blue channels: the screen is XRGB8888 little-endian.
func (s Surface) FlushRect(x, y, w, h int) {
	if s.dst == nil || s.img == nil {
		return
	}
	b := s.img.Bounds()
	x0, y0 := max(x, b.Min.X), max(y, b.Min.Y)
	x1, y1 := min(x+w, b.Max.X), min(y+h, b.Max.Y)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	for row := y0; row < y1; row++ {
		src := s.img.Pix[row*s.img.Stride+x0*4:]
		dst := s.dst[row*s.stride+x0*4:]
		for col := 0; col < x1-x0; col++ {
			dst[col*4+0] = src[col*4+2]
			dst[col*4+1] = src[col*4+1]
			dst[col*4+2] = src[col*4+0]
			dst[col*4+3] = 0
		}
	}
}

// MoveRows copies a band of the image by dy pixels: what is at y arrives at
// y+dy, for h rows, across the x..x+w columns. It is what a scroll costs: the
// text already on the screen is moved, and only the strip the move exposes is
// drawn again.
func (s Surface) MoveRows(x, y, w, h, dy int) {
	if s.img == nil || dy == 0 || w <= 0 || h <= 0 {
		return
	}
	moveRows(s.img.Pix, s.img.Stride, x, y, w, h, dy)
}

// moveRows shifts rows within a byte buffer of the given pitch.
func moveRows(pix []byte, stride, x, y, w, h, dy int) {
	rowBytes := w * 4
	switch {
	case dy > 0:
		// Down: the last row first, or it overwrites what it has not read.
		for i := h - 1; i >= 0; i-- {
			src := (y+i)*stride + x*4
			dst := (y+i+dy)*stride + x*4
			if src < 0 || dst < 0 || dst+rowBytes > len(pix) || src+rowBytes > len(pix) {
				continue
			}
			copy(pix[dst:dst+rowBytes], pix[src:src+rowBytes])
		}
	default:
		for i := 0; i < h; i++ {
			src := (y+i)*stride + x*4
			dst := (y+i+dy)*stride + x*4
			if src < 0 || dst < 0 || dst+rowBytes > len(pix) || src+rowBytes > len(pix) {
				continue
			}
			copy(pix[dst:dst+rowBytes], pix[src:src+rowBytes])
		}
	}
}
