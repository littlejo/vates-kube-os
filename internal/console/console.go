package console

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/console/tty"
	"github.com/vatesfr/vates-kube-os/internal/version"
)

// Console is what a machine shows on its console (tty1).
//
// It replaces the shell script that used to do this. Which of the faces appears
// is decided by `dashboard.mode` in vates-node.yaml, which reaches this program
// through a systemd drop-in written by vates-init:
//
//	tui    the text dashboard: no dependency, works on any console, and the only
//	       one that can work on a serial console
//	gui    the graphical dashboard, full screen, drawn straight onto the screen
//
// The whole point of this indirection is that the choice belongs to the machine,
// not to the image: one image serves both.
func Console(argv []string) error {
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	showVer := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *showVer {
		fmt.Printf("vates-console %s\n", version.Version)
		return nil
	}

	// The console cannot be left, in either face. Ignoring the quit signals here
	// covers this process -- in graphical mode it waits on the renderer child
	// while that child owns the screen, and a Ctrl-C delivered to the process
	// group must not kill the parent and leave PID 1 to start a second console
	// on top of the first -- and the ignore disposition is inherited by the
	// renderer it execs.
	ignoreQuitSignals()

	mode := env("DASHBOARD_MODE", "gui")

	switch mode {
	case "tui":
		return textDashboard()
	case "gui":
		// The graphical console draws straight into the screen's own buffer.
		// There is no compositor, no session and no seat: cairo writes where the
		// display controller reads, and scrolling is a memory copy. Measured on
		// a node, against the GTK window that ran under cage before it:
		//
		//   cage + GTK   83 MB, 51 to 67 % of a core to scroll
		//   this         23 MB, 0.5 %
		//
		// Which is why none of what used to be here is here any more.
		//
		// The check is the same as it ever was, and for the same reason: an
		// intention is not a fallback. The program is started, looked at after a
		// moment, and replaced by the text dashboard if it is already gone -- no
		// /dev/dri/card0, a card the kernel refuses, a mode that will not set.
		// A machine that promised a picture and shows nothing is worse than one
		// that shows text.
		//
		// The graphical face is a child process, not a call: this is the shape
		// the shell had (`--gui &`, then a liveness check), and it is the only
		// one that can tell "it drew" from "it exited" -- the renderer returns
		// only when it stops.
		if _, err := os.Stat("/dev/dri/card0"); err == nil {
			if done, ok := startGUI(); ok {
				if err := <-done; err == nil {
					return nil
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "vates-console: no /dev/dri/card0; text dashboard")
		}
		return textDashboard()
	default:
		// The schema refuses an unknown mode before the machine boots, so this
		// is a backstop, not a path: a console that shows the text dashboard is
		// better than one that shows nothing.
		//
		// There is no "no console" branch and no login prompt: see the mask of
		// every getty in build/provision.sh, and the comment on dashboard.mode
		// in vatescfg.
		fmt.Fprintf(os.Stderr, "vates-console: dashboard.mode %q is not one of tui, gui\n", mode)
		return textDashboard()
	}
}

// textDashboard draws the text dashboard, taking the virtual console back from
// the splash first.
//
// The splash leaves the VT in KD_GRAPHICS -- fbcon told to stop painting -- and
// never restores it, because the graphical console draws through DRM and does not
// need fbcon. The text console is the other way round: it only exists through
// fbcon, so without the restore it runs, writes its frames, and the screen stays
// black.
func textDashboard() error {
	tty.ShowTextConsole()
	// A virtual console draws box-drawing characters only if a font that carries
	// them has been loaded; the kernel's built-in one does not. Load it and say
	// so, so the dashboard can frame itself with real lines instead of ASCII.
	if tty.HasScreen() && tty.LoadConsoleFont() {
		_ = os.Setenv("DASHBOARD_UNICODE", "1")
	}
	return Dashboard(nil)
}

// startGUI launches `vates dashboard --gui` as a child and reports whether it
// survived its first two seconds.
//
// ok is false when the child could not even be started. When ok is true, done
// yields the child's exit error: a renderer that has not exited after two
// seconds is the one that matters, and its result is ours.
func startGUI() (done <-chan error, ok bool) {
	self, err := os.Executable()
	if err != nil {
		self = "vates"
	}
	cmd := exec.Command(self, "dashboard", "--gui")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "vates-console: cannot start the graphical console: %v\n", err)
		return nil, false
	}

	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()

	select {
	case err := <-ch:
		// It was gone within the window: no card, a card the kernel refused, a
		// mode that would not set. Say so and fall back to text.
		fmt.Fprintf(os.Stderr, "vates-console: the graphical console did not start: %v\n", err)
		return nil, false
	case <-time.After(2 * time.Second):
		return ch, true
	}
}

// env reads an environment variable, with a fallback when it is empty.
func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
