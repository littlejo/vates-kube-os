// Splash is the boot screen: the Vates planet and wordmark, a progress bar and a
// scrolling log of the bring-up, drawn straight onto the screen with cairo and
// libdrm.
//
// It is Plymouth's job done by the one binary the system already carries, with
// the cairo, pango and libdrm that are already in the image for the dashboard.
// Nothing new is added, and Plymouth -- which drags dracut, procps and cpio
// behind it -- is not needed.
//
// PID 1 rewrites /run/vates/boot-status as it goes; the splash reads it and
// paints it. Each phase carries a share of the bar, so the bar follows what the
// node is actually doing rather than a timer; between two phases it eases and
// creeps toward the next one, never past it. When the file ends with "done|ok",
// the console is taking over and the splash returns, releasing the DRM device
// for the dashboard.
package sysinit

import (
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/console/tty"
	"github.com/vatesfr/vates-kube-os/internal/display/gui/cairo"
	"github.com/vatesfr/vates-kube-os/internal/display/gui/drm"
)

// BootStatusFile is where PID 1 records the bring-up and the splash reads it.
const BootStatusFile = "/run/vates/boot-status"

const (
	splashTitle       = "VATES KUBE OS"
	splashWordmark    = "vates-name-baseline-white.png"
	splashPlanet      = "vates-planete.png"
	splashLineH       = 22
	splashLogLines    = 5
	splashBarH        = 8
	splashFramePeriod = 33 * time.Millisecond // ~30 fps, enough for a smooth bar
	// How long the bar may keep growing after PID 1 says the boot is done, and
	// how fast it covers the remaining distance. The ramp is long enough to
	// read as the bar reaching the end, the hold long enough to see it full,
	// and the whole thing short enough that the DRM device is free well before
	// stopSplash gives up waiting and vates-console starts.
	splashFinishRamp = 450 * time.Millisecond
	splashFinishHold = 750 * time.Millisecond
)

// splashAssetDirs are searched for the artwork, like the dashboard's.
var splashAssetDirs = []string{"image/assets", "/usr/share/vates/assets"}

// The Vates brand palette, from the brand guidelines. Black stays the backdrop
// as asked; everything on top is a named brand colour.
const (
	splashBG    = 0x000000 // pure black, as asked
	splashInk   = 0xfffce4 // Beige Open: the text and the bar's fill
	splashTrack = 0x1a1b38 // Bleu Spatial: the bar's track
	splashOK    = 0x2ca878 // Vert Infrastructure: a step that succeeded
	splashFail  = 0xbe1621 // Rouge Vates: a step that failed
	splashWait  = 0x8f84ff // Extra Blue: a step in progress
)

// A splashPhase is one phase of PID 1's bring-up. The weight is that phase's
// share of the bar, following what it actually costs: configure pulls the
// control plane's images and dominates, starting the runtime is nearly free.
type splashPhase struct {
	key    string
	msg    string
	weight float64
}

// splashPhases is the walk PID 1 takes, in order. The keys match the labels
// bootStep writes; the messages are what the log shows. The weights sum to 1, so
// the bar is full exactly when the last phase is done.
var splashPhases = []splashPhase{
	{"mount", "mounting the filesystems", 0.05},
	{"containerd", "starting the container runtime", 0.15},
	{"network", "bringing up the network", 0.15},
	{"configure", "configuring the node", 0.35},
	{"kubelet", "starting the kubelet", 0.30},
}

// The planet PNG has transparent margins around the artwork (it is 2600 square
// but the ink is 1550 tall, centred). Sizing by the ink rather than the canvas
// keeps the sphere from looking small in its box; the numbers are measured from
// image/assets/vates-planete.png and only matter for that file.
const (
	planetCanvas = 2600.0
	planetInkH   = 1550.0
)

// Splash paints the boot screen until the console takes over.
func Splash(argv []string) error {
	display, err := drm.Open(splashDevice(argv))
	if err != nil {
		// No screen (serial console, headless VM): not an error, nothing to
		// draw. PID 1 keeps booting.
		return nil
	}
	defer display.Close()

	// Before anything is drawn: from here on the kernel text console never
	// repaints, so the hand-off to the dashboard cannot flash boot text.
	tty.HideTextConsole()

	w, h := display.Width(), display.Height()
	surface := cairo.SurfaceOn(display.Memory(), w, h, display.Stride())
	if !surface.Valid() {
		return nil
	}
	defer surface.Destroy()
	ctx := cairo.New(surface)
	defer ctx.Free()

	s := &splashScreen{
		display: display,
		surface: surface,
		ctx:     ctx,
		w:       w,
		h:       h,
		waitFor: map[string]time.Time{},
	}
	if path := splashFind(splashWordmark); path != "" {
		s.wordmark = cairo.PNG(path)
	}
	defer s.wordmark.Destroy()
	if path := splashFind(splashPlanet); path != "" {
		s.planet = cairo.PNG(path)
	}
	defer s.planet.Destroy()
	// The title is the brand's logotype face, the same one the dashboard's
	// all-caps labels use. "Poppins Vates" carries only A-Z and a space, so the
	// title is upper-case on purpose: lower-case would fall back mid-word.
	s.title = cairo.NewFont("Poppins Vates 16")
	defer s.title.Free()
	s.note = cairo.NewFont("Poppins 11")
	defer s.note.Free()
	s.step = cairo.NewFont("DejaVu Sans Mono 11")
	defer s.step.Free()

	s.refresh()
	s.draw()
	s.shown = 0

	// A stop request means the console is ready: finish the bar, then let the
	// device go. Catching the signal keeps the last frame from being cut off
	// half-way through the final growth.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	// Repaint while the boot proceeds; stop when PID 1 says it is done, or after
	// a bounded time so a splash can never hold the screen forever. Generous:
	// configure pulls images, which can take a while, and the splash is the
	// screen while it does.
	deadline := time.Now().Add(30 * time.Minute)
	var finishAt time.Time
	var finishFrom float64
	for time.Now().Before(deadline) {
		if s.refresh() {
			s.draw()
		}
		// A stop request (or the final `done`) means the console is taking over.
		select {
		case <-sigCh:
			s.done = true
		default:
		}
		if s.done && finishAt.IsZero() {
			finishAt = time.Now()
			finishFrom = s.shown
		}

		if finishAt.IsZero() {
			s.target = s.computeTarget(time.Now())
			if math.Abs(s.shown-s.target) > 0.0005 {
				s.shown += (s.target - s.shown) * 0.20
				if math.Abs(s.shown-s.target) < 0.0005 {
					s.shown = s.target
				}
				s.drawBar()
				s.presentBar()
			}
			time.Sleep(splashFramePeriod)
			continue
		}

		// The boot is done. Cover the distance left in the bar over a fixed,
		// short ramp -- so the last step is SEEN to finish even when the last
		// phases completed in a burst -- then keep it full for a moment and
		// release the DRM device. Lingering is not an option: vates-console
		// cannot take the card until the splash is gone.
		frac := time.Since(finishAt).Seconds() / splashFinishRamp.Seconds()
		if frac > 1 {
			frac = 1
		}
		if shown := finishFrom + (1-finishFrom)*frac; shown != s.shown {
			s.shown = shown
			s.drawBar()
			s.presentBar()
		}
		if time.Since(finishAt) >= splashFinishHold {
			return nil
		}
		time.Sleep(splashFramePeriod)
	}
	return nil
}

// splashDevice reads the optional -device flag, defaulting to the first DRM
// card -- the one the firmware leaves the screen on.
func splashDevice(argv []string) string {
	device := "/dev/dri/card0"
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-device" {
			device = argv[i+1]
		}
	}
	return device
}

// splashScreen is the drawing state: the screen, the surface it hands to cairo,
// the faces resolved once, and where the bar and log landed last frame.
type splashScreen struct {
	display  *drm.Display
	surface  cairo.Surface
	ctx      *cairo.Context
	planet   cairo.Surface
	wordmark cairo.Surface
	title    cairo.Font
	note     cairo.Font
	step     cairo.Font
	w, h     int

	steps   []splashStep
	done    bool
	lastRaw string

	shown   float64 // the fill actually drawn, eased toward target
	target  float64 // where the phases say it should be
	waitFor map[string]time.Time

	barX, barY, barW, barH float64
}

// splashStep is one line of the bring-up as PID 1 wrote it.
type splashStep struct {
	label string
	state string // wait, ok, fail
}

// logEntry is one line of the log after the message is resolved.
type logEntry struct {
	msg   string
	state string
}

// refresh rereads the status file and updates the steps. It reports whether
// anything changed, so the loop only repaints the whole screen on a new phase.
func (s *splashScreen) refresh() bool {
	data, err := os.ReadFile(BootStatusFile)
	if err != nil {
		return false
	}
	if string(data) == s.lastRaw {
		return false
	}
	s.lastRaw = string(data)
	s.steps, s.done = parseBootStatus(string(data))
	return true
}

// parseBootStatus reads one "label|state" per line. A "done" line is not a step;
// it is the signal that the console is taking over.
func parseBootStatus(data string) ([]splashStep, bool) {
	var steps []splashStep
	done := false
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		label, state, _ := strings.Cut(line, "|")
		if label == "done" {
			done = true
			continue
		}
		steps = append(steps, splashStep{label: label, state: state})
	}
	return steps, done
}

// computeTarget turns the phases' states into a fill fraction. A finished phase
// counts whole; a running one creeps, asymptotically, toward 85% of its share --
// never past it, so the bar cannot claim a phase that has not finished.
func (s *splashScreen) computeTarget(now time.Time) float64 {
	if s.done {
		return 1
	}
	if len(s.steps) == 0 {
		return 0
	}
	state := map[string]string{}
	for _, st := range s.steps {
		state[st.label] = st.state
	}
	var total, got float64
	for _, ph := range splashPhases {
		total += ph.weight
		switch state[ph.key] {
		case "ok", "fail":
			got += ph.weight
		case "wait":
			start, ok := s.waitFor[ph.key]
			if !ok {
				start = now
				s.waitFor[ph.key] = now
			}
			elapsed := now.Sub(start).Seconds()
			got += ph.weight * 0.85 * (1 - math.Exp(-elapsed/12))
		}
	}
	if total == 0 {
		return 0
	}
	if v := got / total; v < 1 {
		return v
	}
	return 1
}

// entries resolves the steps to log lines, in the order PID 1 walks them: the
// known phases first, then anything unexpected, in the order it was written.
func (s *splashScreen) entries() []logEntry {
	state := map[string]string{}
	for _, st := range s.steps {
		state[st.label] = st.state
	}
	var out []logEntry
	seen := map[string]bool{}
	for _, ph := range splashPhases {
		if st, ok := state[ph.key]; ok {
			out = append(out, logEntry{msg: ph.msg, state: st})
			seen[ph.key] = true
		}
	}
	for _, st := range s.steps {
		if !seen[st.label] {
			out = append(out, logEntry{msg: st.label, state: st.state})
		}
	}
	return out
}

// draw paints one whole frame: backdrop, artwork, title, bar and log.
func (s *splashScreen) draw() {
	s.ctx.RGB(hexRGB(splashBG))
	s.ctx.Rect(0, 0, float64(s.w), float64(s.h))
	s.ctx.Fill()

	entries := s.entries()
	n := len(entries)
	if n > splashLogLines {
		n = splashLogLines
	}
	// Reserve the full log window from the first frame. Sizing the block on the
	// lines present so far would re-centre it every time one arrives, and the
	// artwork would visibly jump upward on each step.
	logH := float64(splashLogLines * splashLineH)

	// The block is one centred column, sized to the screen. The current step is
	// not advertised separately: the log carries it.
	planetH := clampf(float64(s.h)*0.13, 70, 170)
	if !s.planet.Valid() {
		planetH = 0
	}
	wmW := clampf(float64(s.w)*0.26, 170, 520)
	wmH := wmW / 2.88
	if !s.wordmark.Valid() {
		wmW, wmH = 0, 0
	}
	titleH := float64(s.ctx.Measure(s.title, splashTitle, 0))
	contentW := clampf(float64(s.w)*0.46, 260, 680)

	const sep = 30.0
	total := 0.0
	if planetH > 0 {
		total += planetH + sep
	}
	if wmH > 0 {
		total += wmH + sep
	}
	total += titleH + sep + splashBarH
	if logH > 0 {
		total += sep + logH
	}
	y := (float64(s.h) - total) / 2
	if y < 30 {
		y = 30
	}
	x := (float64(s.w) - contentW) / 2

	if planetH > 0 {
		s.drawPlanet(y, planetH)
		y += planetH + sep
	}
	if wmH > 0 {
		s.ctx.DrawSurface(s.wordmark, (float64(s.w)-wmW)/2, y, wmW, wmH)
		y += wmH + sep
	}

	tw, _ := s.ctx.MeasureSize(s.title, splashTitle, 0)
	s.ctx.RGB(hexRGB(splashInk))
	s.ctx.TextLine(s.title, splashTitle, int((float64(s.w)-float64(tw))/2), int(y), s.w)
	y += titleH + sep

	s.barX, s.barY, s.barW, s.barH = x, y, contentW, splashBarH
	s.drawBar()
	y += splashBarH

	if logH > 0 {
		s.drawLog(entries[len(entries)-n:], x, contentW, y+sep)
	}

	s.ctx.Flush(s.surface)
	s.ctx.MarkDirty(s.surface)
	s.display.Present()
}

// drawPlanet draws the red sphere, sized by its ink so the transparent margin
// in the file does not shrink it.
func (s *splashScreen) drawPlanet(y, inkH float64) {
	box := inkH * planetCanvas / planetInkH
	s.ctx.DrawSurface(s.planet, (float64(s.w)-box)/2, y-(box-inkH)/2, box, box)
}

// drawBar paints the bar band: track, then fill. It is separate from draw so an
// animation step only touches this rectangle, not the whole screen.
func (s *splashScreen) drawBar() {
	r := s.barH / 2
	// Clear the band first: an animation step draws over the previous fill.
	s.ctx.RGB(hexRGB(splashBG))
	s.ctx.Rect(s.barX, s.barY, s.barW, s.barH)
	s.ctx.Fill()

	s.ctx.RGB(hexRGB(splashTrack))
	s.ctx.Rounded(s.barX, s.barY, s.barW, s.barH, r)
	s.ctx.Fill()

	if s.shown <= 0 {
		return
	}
	fw := s.shown * s.barW
	if fw < s.barH {
		fw = s.barH // a minimum cap, so the first sliver is still a pill
	}
	s.ctx.RGB(hexRGB(splashInk))
	s.ctx.Rounded(s.barX, s.barY, fw, s.barH, r)
	s.ctx.Fill()
}

// drawLog draws the newest lines, message left, status right in the brand colour
// of the state. Older lines scroll off the top when the window is full.
func (s *splashScreen) drawLog(entries []logEntry, x, contentW, y float64) {
	for i, e := range entries {
		ly := y + float64(i*splashLineH)
		mark, col := statusOf(e.state)
		mw, _ := s.ctx.MeasureSize(s.step, mark, 0)

		s.ctx.RGB(hexRGB(splashInk))
		s.ctx.TextLine(s.note, e.msg, int(x), int(ly), int(contentW-float64(mw)-14))
		s.ctx.RGB(hexRGB(col))
		s.ctx.TextLine(s.step, mark, int(x+contentW-float64(mw)), int(ly), mw+1)
	}
}

// presentBar declares only the bar band changed, so the virtual display does not
// hand the whole screen back to the host for every frame of the animation.
func (s *splashScreen) presentBar() {
	x, y := int(s.barX)-2, int(s.barY)-2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	s.ctx.Flush(s.surface)
	s.ctx.MarkDirty(s.surface)
	s.display.PresentRect(x, y, int(s.barW)+4, int(s.barH)+4)
}

// statusOf is the mark and colour of a step's state.
func statusOf(state string) (string, uint32) {
	switch state {
	case "ok":
		return "OK", splashOK
	case "fail":
		return "FAILED", splashFail
	default:
		return "...", splashWait
	}
}

// splashFind looks for a file in the splash asset directories, in order.
func splashFind(name string) string {
	for _, dir := range splashAssetDirs {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// clampf bounds v to [lo, hi].
func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// hexRGB splits a 0xRRGGBB colour into cairo's 0..1 components.
func hexRGB(c uint32) (float64, float64, float64) {
	return float64((c>>16)&0xff) / 255, float64((c>>8)&0xff) / 255, float64(c&0xff) / 255
}
