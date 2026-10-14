package gui

import (
	"fmt"

	"github.com/vatesfr/vates-kube-os/internal/dashboard"
	"github.com/vatesfr/vates-kube-os/internal/display/gui/render"
	"github.com/vatesfr/vates-kube-os/internal/hostinfo"
)

// Preview draws one dashboard frame offscreen and writes it as a PNG.
//
// It exists so the picture can be looked at without a DRM device: on a
// workstation, in a test, or to regenerate the documentation's preview image.
// It is the same layout and the same drawing as the live console -- there is no
// second renderer -- so what it writes is what the machine would show.
func Preview(snapshot dashboard.Snapshot, info hostinfo.Info, assets []string, w, h int, path string) error {
	surface := render.NewSurface(w, h)
	if !surface.Valid() {
		return fmt.Errorf("preview: bad size %dx%d", w, h)
	}
	s := &screen{
		surface: surface,
		w:       w,
		h:       h,
		follow:  true,
		heights: make(map[string]int),
		seen:    make(map[string]bool),
	}
	s.scale = scaleFor(w, h)
	s.ctx = render.New(surface)
	defer s.ctx.Free()

	s.loadFonts()
	s.loadLogo(assets)
	s.layout()
	s.snap, s.info = snapshot, info
	s.absorb(snapshot.Events)
	s.refreshFeed()
	s.redraw()
	return surface.SavePNG(path)
}
