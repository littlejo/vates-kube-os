package render

import (
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Font is a face resolved once: a TTF, a size in pixels, and the metrics the
// layout needs. The cairo/pango build asked fontconfig for a family by name;
// there is no fontconfig any more, so a face is a FILE. The callers know the
// files (the image installs them at fixed paths), which is also why a missing
// one degrades to no text rather than to a panic.
type Font struct {
	face   font.Face
	ascent float64
	height float64
	valid  bool
}

// LoadFont reads a TTF and resolves it at a pixel size. Size is in pixels: the
// DPI is 72, so a point is a pixel and the dashboard's numbers mean what they
// look like. A file that cannot be read or parsed yields an invalid Font, whose
// text methods draw nothing.
func LoadFont(path string, size float64) Font {
	if path == "" || size <= 0 {
		return Font{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Font{}
	}
	f, err := truetype.Parse(data)
	if err != nil {
		return Font{}
	}
	face := truetype.NewFace(f, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	m := face.Metrics()
	return Font{
		face:   face,
		ascent: float64(m.Ascent) / 64,
		height: float64(m.Height) / 64,
		valid:  true,
	}
}

// LoadFirstFont loads the first of the candidate paths that exists. The first
// is the intended face; the rest are fallbacks for a machine -- or a checkout
// -- that does not carry it, so that a console still shows something.
func LoadFirstFont(size float64, candidates ...string) Font {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if f := LoadFont(p, size); f.Valid() {
			return f
		}
	}
	return Font{}
}

// FindFont looks for a relative font path under the roots the console knows:
// the image's install location, and the checkout's own asset directory. The
// VATES_FONT_DIR variable (colon-separated) is searched first, so a preview can
// point at fonts fetched outside the image.
func FindFont(name string) string {
	roots := fontRoots
	if extra := os.Getenv("VATES_FONT_DIR"); extra != "" {
		roots = append(strings.Split(extra, ":"), roots...)
	}
	for _, root := range roots {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

var fontRoots = []string{
	"/usr/share/fonts",
	"image/assets/fonts",
	"image/fonts",
}

func (f Font) Valid() bool { return f.valid }

// Free releases the face. The call sites the cairo font had defer it; the
// garbage collector would do it anyway.
func (f Font) Free() {
	if f.face != nil {
		_ = f.face.Close()
	}
}

func (f Font) lineHeight() float64 {
	if f.height <= 0 {
		return 1
	}
	return f.height
}

// measure is the advance width of a run of text, in pixels.
func (f Font) measure(s string) float64 {
	if s == "" {
		return 0
	}
	d := font.Drawer{Face: f.face}
	return float64(d.MeasureString(s)) / 64
}

// --- markup -----------------------------------------------------------------
//
// The dashboard hands text as Pango markup with colour spans -- that is how the
// event feed colours its timestamp, its reason column and its message
// separately. Nothing else parses Pango here: the grammar is the one this
// project emits, decoded into runs of (text, colour).

// span is a run of text with a colour. A colour of useDefault means "the
// context's current colour".
type span struct {
	text string
	col  uint32
}

const useDefault = 0x1_000000 // outside the 24-bit palette

// parseMarkup decodes the markup into spans. It understands &amp; &lt; &gt;
// &quot; &apos; and numeric entities, <span foreground="#rrggbb"> ... </span>,
// and treats everything else as literal text -- the escapes PangoEscape applies
// mean a raw '<' or '&' cannot reach here as anything but a tag or an entity.
func parseMarkup(s string, def uint32) []span {
	var (
		out     []span
		cur     uint32 = useDefault
		buf     strings.Builder
		flush         = func() {
			if buf.Len() == 0 {
				return
			}
			out = append(out, span{text: buf.String(), col: cur})
			buf.Reset()
		}
	)
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "</span>"):
			flush()
			cur = useDefault
			i += len("</span>")
		case strings.HasPrefix(s[i:], "<span foreground=\""):
			flush()
			rest := s[i+len(`<span foreground="`):]
			j := strings.IndexByte(rest, '"')
			if j < 0 {
				// A malformed tag: take the rest as literal text.
				buf.WriteByte(s[i])
				i++
				continue
			}
			cur = parseColour(rest[:j])
			i += len(`<span foreground="`) + j + 1 // + the closing quote
			if strings.HasPrefix(s[i:], ">") {
				i++
			}
		case strings.HasPrefix(s[i:], "<!--"):
			j := strings.Index(s[i:], "-->")
			if j < 0 {
				i = len(s)
			} else {
				i += j + 3
			}
		case s[i] == '&':
			r, n := decodeEntity(s[i:])
			if n == 0 {
				buf.WriteByte('&')
				i++
			} else {
				buf.WriteRune(r)
				i += n
			}
		case s[i] == '<':
			// Any other tag is dropped, as markup consumers do.
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				i = len(s)
			} else {
				i += j + 1
			}
		default:
			buf.WriteByte(s[i])
			i++
		}
	}
	flush()
	if len(out) == 0 {
		out = append(out, span{text: "", col: useDefault})
	}
	return out
}

// parseColour reads "#rrggbb", "rrggbb", "#rgb" or "rgb".
func parseColour(s string) uint32 {
	s = strings.TrimPrefix(s, "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return useDefault
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return useDefault
	}
	return uint32(v)
}

// decodeEntity reads one entity at the start of s and returns the rune and the
// bytes consumed; n == 0 means it was not an entity.
func decodeEntity(s string) (rune, int) {
	switch {
	case strings.HasPrefix(s, "&amp;"):
		return '&', 5
	case strings.HasPrefix(s, "&lt;"):
		return '<', 4
	case strings.HasPrefix(s, "&gt;"):
		return '>', 4
	case strings.HasPrefix(s, "&quot;"):
		return '"', 6
	case strings.HasPrefix(s, "&apos;"):
		return '\'', 6
	case strings.HasPrefix(s, "&#"):
		end := strings.IndexByte(s, ';')
		if end < 0 {
			return 0, 0
		}
		num := s[2:end]
		base := 10
		if strings.HasPrefix(num, "x") || strings.HasPrefix(num, "X") {
			num, base = num[1:], 16
		}
		v, err := strconv.ParseUint(num, base, 32)
		if err != nil {
			return 0, 0
		}
		return rune(v), end + 1
	}
	return 0, 0
}

// runeCell is one character with the colour it is drawn in.
type runeCell struct {
	r   rune
	col uint32
}

// flatten turns spans into per-rune cells, so that wrapping can break anywhere
// the layout needs and still know each character's colour.
func flatten(spans []span) []runeCell {
	var cells []runeCell
	for _, s := range spans {
		for _, r := range s.text {
			cells = append(cells, runeCell{r: r, col: s.col})
		}
	}
	return cells
}

// ellipsize cuts cells to at most maxWidth pixels, ending in an ellipsis.
func (f Font) ellipsize(cells []runeCell, maxWidth float64) []runeCell {
	if maxWidth <= 0 || f.measureCells(cells) <= maxWidth {
		return cells
	}
	limit := maxWidth - f.measure("…")
	acc, cut := 0.0, 0
	for cut < len(cells) {
		cw := f.measure(string(cells[cut].r))
		if acc+cw > limit {
			break
		}
		acc += cw
		cut++
	}
	col := uint32(useDefault)
	if cut > 0 {
		col = cells[cut-1].col
	}
	kept := make([]runeCell, 0, cut+1)
	kept = append(kept, cells[:cut]...)
	kept = append(kept, runeCell{r: '…', col: col})
	return kept
}

// inkBounds is the vertical extent of the drawn glyphs, relative to the
// baseline: top is negative (above it), bottom positive (below it). Measuring
// the ink, not the line box, is what keeps a face with odd vertical metrics --
// the logotype subset's ascent overflows its height -- centred.
func (f Font) inkBounds(cells []runeCell) (top, bottom float64, ok bool) {
	for _, c := range cells {
		if c.r == ' ' {
			continue
		}
		b, _, has := f.face.GlyphBounds(c.r)
		if !has {
			continue
		}
		t := float64(b.Min.Y) / 64
		bt := float64(b.Max.Y) / 64
		if !ok || t < top {
			top = t
		}
		if !ok || bt > bottom {
			bottom = bt
		}
		ok = true
	}
	return top, bottom, ok
}

func (f Font) measureCells(cells []runeCell) float64 {
	var w float64
	for i := 0; i < len(cells); {
		j := i + 1
		for j < len(cells) && cells[j].col == cells[i].col {
			j++
		}
		w += f.measure(cellsText(cells[i:j]))
		i = j
	}
	return w
}

func cellsText(cells []runeCell) string {
	var b strings.Builder
	b.Grow(len(cells))
	for _, c := range cells {
		b.WriteRune(c.r)
	}
	return b.String()
}

// wrap breaks cells into lines no wider than maxWidth, preserving spaces (the
// event feed aligns its reason column with them) and breaking a word that is
// itself wider than the line, character by character.
func (f Font) wrap(cells []runeCell, maxWidth float64) [][]runeCell {
	if maxWidth <= 0 {
		return [][]runeCell{cells}
	}
	var (
		lines [][]runeCell
		cur   []runeCell
		curW  float64
		flush = func() {
			lines = append(lines, cur)
			cur = nil
			curW = 0
		}
		// place fits as much of one word as the line allows, breaking the
		// word when it is wider than a whole line.
		place = func(word []runeCell) {
			if len(cur) == 0 {
				if w := f.measureCells(word); w <= maxWidth {
					cur = append(cur, word...)
					curW = w
					return
				}
			}
			for len(word) > 0 {
				k, w := 0, 0.0
				for k < len(word) {
					cw := f.measure(string(word[k].r))
					if w+cw > maxWidth && k > 0 {
						break
					}
					w += cw
					k++
				}
				cur = append(cur, word[:k]...)
				curW += w
				word = word[k:]
				if len(word) > 0 {
					flush()
				}
			}
		}
	)
	for i := 0; i < len(cells); {
		j := i + 1
		isSpace := cells[i].r == ' '
		for j < len(cells) && (cells[j].r == ' ') == isSpace {
			j++
		}
		tok := cells[i:j]
		tokW := f.measureCells(tok)
		switch {
		case isSpace:
			if len(cur) == 0 {
				// a space at the start of a line is dropped, as a wrappper does
			} else if curW+tokW <= maxWidth {
				cur = append(cur, tok...)
				curW += tokW
			} else {
				flush()
			}
		case len(cur) > 0 && curW+tokW > maxWidth:
			flush()
			place(tok)
		default:
			place(tok)
		}
		i = j
	}
	if len(cur) > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

// drawCells draws one line of cells at x, with the baseline at baseline,
// switching colour where the runs switch.
func (f Font) drawCells(dst *image.RGBA, cells []runeCell, x, baseline float64, def uint32) {
	d := &font.Drawer{Dst: dst, Face: f.face}
	for i := 0; i < len(cells); {
		j := i + 1
		for j < len(cells) && cells[j].col == cells[i].col {
			j++
		}
		col := cells[i].col
		if col == useDefault {
			col = def
		}
		d.Src = image.NewUniform(nrgba(col))
		d.Dot = fixed.P(int(math.Round(x)), int(math.Round(baseline)))
		d.DrawString(cellsText(cells[i:j]))
		x += f.measure(cellsText(cells[i:j]))
		i = j
	}
}

func nrgba(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}
