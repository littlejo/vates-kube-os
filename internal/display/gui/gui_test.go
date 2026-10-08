package gui

import (
	"math"
	"testing"
)

// TestScaleFor pins the resolution mapping: the reference screen is the
// identity, and every other screen is scaled by the smaller of its width and
// height ratios -- so the fonts never outgrow the tiles.
func TestScaleFor(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		want float64
	}{
		{name: "the reference resolution is the identity", w: refW, h: refH, want: 1},
		{name: "twice the reference", w: 2 * refW, h: 2 * refH, want: 2},
		{name: "half the reference", w: refW / 2, h: refH / 2, want: 0.5},
		{name: "Xen Orchestra's cirrus VGA, 800x600", w: 800, h: 600, want: 0.78125},
		{name: "the smallest cirrus mode, 640x480", w: 640, h: 480, want: 0.625},
		{name: "1080p is limited by its height", w: 1920, h: 1080, want: 1080.0 / refH},
		{name: "an ultrawide is limited by its height", w: 2560, h: 1080, want: 1080.0 / refH},
		{name: "a screen shorter than the reference", w: refW, h: refH / 2, want: 0.5},
		{name: "a degenerate size falls back to the identity", w: 0, h: 0, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := scaleFor(tt.w, tt.h)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("scaleFor(%d,%d) = %v, want %v", tt.w, tt.h, got, tt.want)
			}
		})
	}
}

// TestValueRoomNeverDropsBelowTheReference pins the point of the scaling. At the
// reference resolution a value has room = tileW - 2*inset, in pixels; divided by
// its font size it is the room in "font heights". Scaling both the tile and the
// font by the same factor keeps that number at least what the design was
// approved with, so no value is ellipsised on a screen the scaling knows how to
// size for -- which is the Xen Orchestra bug this replaces fixed tiles for.
func TestValueRoomNeverDropsBelowTheReference(t *testing.T) {
	roomInFontHeights := func(w, h int) float64 {
		k := scaleFor(w, h)
		tileW := (float64(w) - 2*refPadding*k - (cardColumns-1)*refTileGap*k) / cardColumns
		return (tileW - 2*refTileInset*k) / (baseValue * k)
	}
	want := roomInFontHeights(refW, refH)

	tests := []struct {
		name string
		w, h int
	}{
		{name: "the reference 1024x768", w: 1024, h: 768},
		{name: "Xen Orchestra's cirrus 800x600", w: 800, h: 600},
		{name: "the smallest cirrus 640x480", w: 640, h: 480},
		{name: "1080p", w: 1920, h: 1080},
		{name: "1440p", w: 2560, h: 1440},
		{name: "ultrawide", w: 2560, h: 1080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := roomInFontHeights(tt.w, tt.h); got < want-1e-9 {
				t.Fatalf("a value has room for %.2f font heights at %dx%d, below the reference %.2f: it would be ellipsised",
					got, tt.w, tt.h, want)
			}
		})
	}
}
