// Command ggpreview renders the console's pictures -- the boot screen and the
// dashboard -- offscreen, as PNGs, without a DRM device. It is a development
// tool: it is installed by no recipe and never enters the image.
//
// Run it from the repository root so that the assets under image/assets are
// found, with the fonts either installed under /usr/share/fonts or pointed at by
// VATES_FONT_DIR:
//
//	go run ./cmd/ggpreview /tmp/opencode
//
// It writes dashboard.png and splash.png at the reference resolution, plus a
// scaled pair at 800x600, which is what Xen Orchestra's Cirrus VGA offers.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vatesfr/vates-kube-os/internal/console"
	"github.com/vatesfr/vates-kube-os/internal/display/gui"
	"github.com/vatesfr/vates-kube-os/internal/hostinfo"
	"github.com/vatesfr/vates-kube-os/internal/sysinit"
)

// splashStatus is a bring-up in progress: the configure phase running, the ones
// before it done. It is what /run/vates/boot-status holds mid-boot.
const splashStatus = "mount|ok\ncontainerd|ok\nnetwork|ok\nconfigure|wait\n"

var assets = []string{"image/assets", "/usr/share/vates/assets"}

func main() {
	out := "."
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fail(err)
	}

	snapshot := console.MockSnapshot()
	info := console.MockHost(hostinfo.Info{})

	render := func(name string, w, h int) {
		if err := gui.Preview(snapshot, info, assets, w, h, filepath.Join(out, "dashboard-"+name+".png")); err != nil {
			fail(err)
		}
		if err := sysinit.SplashPreview(w, h, splashStatus, 0.62, filepath.Join(out, "splash-"+name+".png")); err != nil {
			fail(err)
		}
		fmt.Fprintf(os.Stderr, "wrote dashboard-%s.png and splash-%s.png\n", name, name)
	}

	render("1024x768", 1024, 768)
	render("800x600", 800, 600)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ggpreview:", err)
	os.Exit(1)
}
