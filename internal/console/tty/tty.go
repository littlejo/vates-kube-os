package tty

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// The VT ioctls from linux/kd.h: KD_GRAPHICS tells fbcon the console is a
// graphics surface and it must stop drawing text. That is what stops the kernel
// text console from flashing its buffer -- including errors already scrolled --
// over the screen the moment the splash releases the DRM device.
const (
	kdSetMode  = 0x4B3A
	kdGraphics = 0x01
	kdText     = 0x00
)

// HasScreen reports whether a DRM card exists: a machine with a screen, where
// the splash and the dashboard draw, as opposed to a headless VM whose only
// console is the serial or Xen one.
func HasScreen() bool {
	_, err := os.Stat("/dev/dri/card0")
	return err == nil
}

// HideTextConsole tells fbcon to stop painting text on /dev/tty0, so it does not
// repaint over a picture drawn through DRM.
func HideTextConsole() {
	setMode(kdGraphics)
}

// ShowTextConsole hands the virtual console back to fbcon, undoing
// HideTextConsole.
//
// The splash leaves the VT in KD_GRAPHICS and never restores it, because the
// graphical console draws through DRM and does not need fbcon. The text console
// is the other way round: it only exists through fbcon, so without the restore
// it runs, writes its frames, and the screen stays black.
func ShowTextConsole() {
	setMode(kdText)
}

func setMode(mode uintptr) {
	f, err := os.OpenFile("/dev/tty0", os.O_RDWR, 0)
	if err != nil {
		return
	}
	// The ioctl is the whole point; closing is a formality whose failure says
	// nothing about it.
	defer func() { _ = f.Close() }()
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), kdSetMode, mode)
}

// consoleFontGlob is the console font the text dashboard wants. eurlatgr, from
// the kbd package, carries the whole box-drawing block -- the kernel's built-in
// VGA font does not -- and keeps the 8x16 cell, so the layout's density is
// unchanged (128x48 on a 1024x768 console). A glob because kbd installs the font
// compressed or not, depending on how it was built.
const consoleFontGlob = "/usr/share/consolefonts/eurlatgr.psf*"

// LoadConsoleFont loads that font on the virtual console, reporting whether it
// did. Best effort by design: an image without kbd, a machine with no TTY, a font
// that will not load -- the console then falls back to ASCII rather than failing
// to appear.
func LoadConsoleFont() bool {
	paths, _ := filepath.Glob(consoleFontGlob)
	if len(paths) == 0 {
		return false
	}
	out, err := exec.Command("setfont", "-C", "/dev/tty1", paths[0]).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "console: setfont %s: %v: %s\n", paths[0], err, strings.TrimSpace(string(out)))
		return false
	}
	fmt.Fprintf(os.Stderr, "console: loaded font %s\n", paths[0])
	return true
}
