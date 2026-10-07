package gui

// The cursor. There is no compositor and no display server: nothing draws a
// pointer unless this does, and nothing holds its position unless this keeps it.
//
// It is a SOFTWARE cursor, which means it must put back what it covers. That is
// not a detail -- it is the whole problem. cage, which used to draw one for us,
// painted the arrow without repainting the spot it left, and the result was a
// line of arrows across the tiles and the feed: reported twice as a display
// glitch before anyone recognised a stale cursor. The fix there was to change
// the compositor's renderer. Here the fix is to save the pixels under the
// pointer and restore them before drawing it somewhere else.
//
// And it must be restored before ANY drawing that touches its rectangle --
// a scroll moves the feed underneath it, and a cursor drawn on top would be
// carried along with the text.

const (
	cursorW = 12
	cursorH = 19
)

// cursorPoint is one corner of the arrow, in pixels from its tip. It is the
// usual pointer shape: a left edge, a tail, and the notch that makes it read as
// an arrow rather than a triangle at any size.
var cursorShape = [...][2]float64{
	{0, 0}, {0, 15}, {3.5, 12}, {6.5, 18.5}, {8.5, 17.5}, {5.5, 11}, {10, 11},
}

type cursor struct {
	x, y    int
	visible bool

	bg    []byte // the pixels the arrow covers
	bx    int    // where they were taken from
	by    int
	saved bool
}

// restoreCursor puts back the pixels the arrow covers.
func (s *screen) restoreCursor() {
	c := s.cursors
	if c == nil || !c.saved {
		return
	}
	s.blit(c.bg, c.bx, c.by, cursorW, cursorH, false)
	c.saved = false
	s.ctx.MarkDirty(s.surface)
}

// drawCursor saves what is under the arrow and draws it.
func (s *screen) drawCursor() {
	c := s.cursors
	if c == nil || !c.visible {
		return
	}
	s.blit(c.bg, c.x, c.y, cursorW, cursorH, true)
	c.bx, c.by, c.saved = c.x, c.y, true

	s.ctx.MoveTo(float64(c.x)+cursorShape[0][0], float64(c.y)+cursorShape[0][1])
	for _, p := range cursorShape[1:] {
		s.ctx.LineTo(float64(c.x)+p[0], float64(c.y)+p[1])
	}
	s.ctx.ClosePath()
	// Cream inside, black edge: readable over a dark tile, a coloured pill or a
	// line of text, without a shadow to blend.
	s.setColour(colourCream)
	s.ctx.Fill()
	s.setColour(colourBlack)
	s.ctx.Stroke(1)
	s.ctx.MarkDirty(s.surface)
}

// moveCursor takes the pointer where the mouse says it is. It reports nothing:
// what has to be redrawn is decided by what actually changed.
func (s *screen) moveCursor() {
	if s.cursors == nil || s.wheel == nil {
		return
	}
	x, y, ok := s.wheel.Position()
	if !ok {
		return
	}
	// Clamped so the arrow is always WHOLLY on screen: a cursor half off the
	// edge would make the save-and-restore a partial rectangle, and the part
	// left behind would never be cleaned.
	if x > s.w-cursorW {
		x = s.w - cursorW
	}
	if y > s.h-cursorH {
		y = s.h - cursorH
	}
	c := s.cursors
	oldX, oldY := c.x, c.y
	if x == oldX && y == oldY && c.visible {
		return
	}
	c.x, c.y, c.visible = x, y, true

	s.restoreCursor()
	s.drawCursor()

	// Only where the arrow was and where it now is: everything else is unchanged.
	x0 := min(oldX, x)
	y0 := min(oldY, y)
	x1 := max(oldX+cursorW, x+cursorW)
	y1 := max(oldY+cursorH, y+cursorH)
	s.display.PresentRect(x0, y0, x1-x0, y1-y0)
}

// blit copies a rectangle between the screen and a buffer, one scanline at a
// time because the stride is the screen's, not the rectangle's.
func (s *screen) blit(buf []byte, x, y, w, h int, fromScreen bool) {
	mem := s.display.Pixels()
	stride := s.display.Stride()
	rowBytes := w * 4
	for row := range h {
		off := (y+row)*stride + x*4
		if off < 0 || off+rowBytes > len(mem) {
			continue
		}
		if fromScreen {
			copy(buf[row*rowBytes:(row+1)*rowBytes], mem[off:off+rowBytes])
		} else {
			copy(mem[off:off+rowBytes], buf[row*rowBytes:(row+1)*rowBytes])
		}
	}
}
