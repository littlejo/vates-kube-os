package sysinit

import (
	"fmt"

	"github.com/vatesfr/vates-kube-os/internal/display/gui/render"
)

// SplashPreview draws one boot screen frame offscreen and writes it as a PNG.
//
// The splash owns the DRM device during boot, so the only way to look at what it
// paints -- on a workstation, or in a test -- is to render a frame without a
// screen. status is the content of the boot-status file, exactly as PID 1 would
// have written it. shown is how full the bar is (0..1).
func SplashPreview(w, h int, status string, shown float64, path string) error {
	surface := render.NewSurface(w, h)
	if !surface.Valid() {
		return fmt.Errorf("splash preview: bad size %dx%d", w, h)
	}
	ctx := render.New(surface)
	defer ctx.Free()

	s := newSplashScreen(surface, ctx, w, h)
	s.lastRaw = status
	s.steps, s.done = parseBootStatus(status)
	s.shown = shown
	s.draw()
	return surface.SavePNG(path)
}
