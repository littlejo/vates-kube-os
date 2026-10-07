package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestColumnCountAdaptsToTheConsole(t *testing.T) {
	// The machine's console is 128 columns, but the same binary runs in an 80
	// column serial console and in a resized terminal. The thresholds are the
	// widths at which a column stops holding a label and a value.
	cases := []struct {
		width int
		want  int
	}{
		{40, 1}, {61, 1},
		{62, 2}, {117, 2},
		{118, 3}, {128, 3}, {200, 3},
	}
	for _, c := range cases {
		if got := columnCount(c.width); got != c.want {
			t.Errorf("columnCount(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

func TestPadCountsDisplayColumnsNotBytes(t *testing.T) {
	// A styled string carries escape sequences that occupy no columns. Padding
	// by len() would overflow the panel by the length of the escapes, and on a
	// real console a line one column too long wraps and breaks the frame.
	styled := lipgloss.NewStyle().Bold(true).Render("vates-cp-1")
	if len(styled) == len("vates-cp-1") {
		t.Skip("no escapes emitted in this environment; nothing to assert")
	}
	padded := pad(styled, 20)
	if got := lipgloss.Width(padded); got != 20 {
		t.Errorf("pad produced %d display columns, want 20", got)
	}
}

func TestTruncateCountsRunes(t *testing.T) {
	// Cutting by bytes would split a multi-byte character and print a broken
	// glyph on the console.
	got := truncate("héllo wörld", 5)
	if len([]rune(got)) != 5 {
		t.Errorf("truncate returned %q, which is %d runes", got, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "\u2026") {
		t.Errorf("truncate should mark the cut with an ellipsis: %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate shortened a string that already fit: %q", got)
	}
}

func TestPanelIsASCIIOnly(t *testing.T) {
	// Measured on a node: the framebuffer console's font rendered the vertical
	// bar of a rounded border but NOT the horizontal dashes, so the panels
	// appeared to have sides and no top or bottom. A frame that depends on a
	// glyph the console may not carry is a frame that renders half-drawn, and
	// nothing about it looks like a font problem.
	m := Model{width: 100, height: 30}
	for _, r := range m.panel("title", "content", 0) {
		if r > 127 {
			t.Fatalf("the panel contains %q, which a kernel console font may not have", r)
		}
	}
}

func TestPanelWidthIsExact(t *testing.T) {
	// On a console, a line one column too long wraps and destroys the frame.
	// Every line, border included, must be exactly the terminal's width.
	for _, w := range []int{62, 80, 100, 128} {
		m := Model{width: w, height: 30}
		for i, line := range strings.Split(m.panel("t", "content", 0), "\n") {
			if got := lipgloss.Width(line); got != w {
				t.Errorf("width %d: line %d is %d columns, want %d", w, i, got, w)
			}
		}
	}
}

func TestViewFillsTheScreenExactly(t *testing.T) {
	// The panels together must occupy every row of the console. A frame one row
	// short leaves the row above it showing whatever was displayed before --
	// measured on a node, the panel was 47 rows on a 48-row screen and a stale
	// title stayed in row 0.
	for _, size := range [][2]int{{80, 24}, {100, 30}, {128, 48}} {
		m := Model{width: size[0], height: size[1]}
		if got := lipgloss.Height(m.View()); got != size[1] {
			t.Errorf("View at %dx%d is %d rows tall, want %d", size[0], size[1], got, size[1])
		}
	}
}

func TestStageTellsConfiguredFromConfiguring(t *testing.T) {
	// The two states look identical through the API -- it has answered in
	// neither -- so the console tells them apart from the machine's own state:
	// vates-init's drop-in exists, or it does not. Without that, a node whose
	// endpoint is unreachable shows "Configuring" in the warning colour
	// forever, when it is done and the cluster is the one not talking.
	dropIn := filepath.Join(t.TempDir(), "10-node.conf")
	m := Model{dropIn: dropIn}

	if got := m.grid(120); !strings.Contains(got, "Configuring") {
		t.Errorf("before vates-init runs, STAGE should read Configuring:\n%s", got)
	}

	if err := os.WriteFile(dropIn, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := m.grid(120)
	if !strings.Contains(got, "Configured") || strings.Contains(got, "Configuring") {
		t.Errorf("configured but without an API answer, STAGE should read Configured:\n%s", got)
	}
	if !strings.Contains(got, "Waiting for API") {
		t.Errorf("CLUSTER should say the machine is waiting for the API:\n%s", got)
	}

	// And the API answering still wins: once the cluster has spoken about this
	// node, the machine is Running, whatever the local files say.
	m.snapshot.Node.Metadata.Name = "vates-cp-1"
	if got := m.grid(120); !strings.Contains(got, "Running") {
		t.Errorf("with an API snapshot, STAGE should read Running:\n%s", got)
	}
}

func TestPanelHonoursTheRequestedHeight(t *testing.T) {
	// The panels have to cover the screen exactly: a panel that stops short
	// leaves whatever was displayed before visible underneath, which is what
	// made boot messages look like new kernel traces.
	m := Model{width: 120}
	for _, want := range []int{0, 4, 10, 30} {
		got := lipgloss.Height(m.panel("t", "a\nb", want))
		if want == 0 {
			if got != 5 {
				t.Errorf("panel with no height requested is %d rows, want 5", got)
			}
			continue
		}
		if got != want {
			t.Errorf("panel asked for %d rows is %d", want, got)
		}
	}
}

func TestTheCPUReadingSurvivesTheTripThroughTheModel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the host reading comes from /proc")
	}
	// A CPU percentage is the DIFFERENCE between two readings, so the Reader
	// that holds the first one has to survive the round trip through the model.
	// It did not: collect() worked on a copy and Update stored only the Info, so
	// every reading started without a predecessor and the CPU tile read n/a for
	// the life of the process -- in the DEFAULT console mode.
	//
	// The kubeconfig does not exist, and that is fine: the host reading is taken
	// before the API is ever contacted, so the error is expected and irrelevant
	// to what is asserted here.
	m := New(filepath.Join(t.TempDir(), "absent.conf"), "", "")

	m = runCollection(t, m)
	// /proc/stat is a cumulative counter: the percentage needs an interval to be
	// defined, and 60 ms is several jiffies even on an idle machine.
	time.Sleep(60 * time.Millisecond)
	m = runCollection(t, m)

	if got := m.hostInfo.CPUPercent(); got < 0 {
		t.Fatalf("CPU is still reported as unknown after two collections: the previous sample was not carried")
	}
}

// runCollection executes the collection command synchronously and folds its
// message into the model, the way bubbletea's loop would.
func runCollection(t *testing.T, m Model) Model {
	t.Helper()
	msg := m.collect()()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestNoKeyQuitsTheConsole(t *testing.T) {
	// The console is the machine's face: there is no shell behind it and no
	// login prompt to return to, so q, Ctrl-C and Esc have nowhere to go. A key
	// that returned tea.Quit would end the program and leave the screen dead
	// until PID 1 brought it back.
	m := Model{width: 100, height: 30}
	keys := []tea.KeyMsg{
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune("q")},
	}
	for _, key := range keys {
		_, cmd := m.Update(key)
		if cmd != nil {
			t.Errorf("%q returned a command; the console must not be quittable", key.String())
		}
	}
}

// TestTheFrameIsUnicodeOnlyWhenTheFontIsThere pins the border choice: the
// box-drawing frame needs a console font that carries those glyphs, and the
// kernel's built-in one does not. It must be opt-in, so a serial console or a
// font that did not load keeps the ASCII frame rather than drawing a broken one.
func TestTheFrameIsUnicodeOnlyWhenTheFontIsThere(t *testing.T) {
	if got := frameBorder(true); got != lipgloss.NormalBorder() {
		t.Errorf("frameBorder(true) is not the Unicode normal border")
	}
	if got := frameBorder(false); got != lipgloss.ASCIIBorder() {
		t.Errorf("frameBorder(false) is not the ASCII border")
	}
}
