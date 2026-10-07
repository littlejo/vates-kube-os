package configdrive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newDrive builds a Drive over a temporary directory. Open() is used rather
// than Find()/Mount() so the test needs no privileges and no real device.
func newDrive(t *testing.T, files map[string]string) *Drive {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		// Parent directories are created: some drive content is nested (pki/),
		// and os.WriteFile would not create them.
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Open(dir)
}

func TestFileReadsExactName(t *testing.T) {
	d := newDrive(t, map[string]string{"vates-node.yaml": "role: worker\n"})

	b, err := d.File("vates-node.yaml")
	if err != nil {
		t.Fatalf("File() failed: %v", err)
	}
	if string(b) != "role: worker\n" {
		t.Errorf("File() = %q, want the file's content", b)
	}
}

func TestFileFallsBackToUpperCase(t *testing.T) {
	// Some NoCloud producers write the name in the ISO9660 8.3 form, which for
	// vates-node.yaml is VATES-NO.YAM: eight characters, then the extension.
	d := newDrive(t, map[string]string{"VATES-NO.YAM": "role: worker\n"})

	// Guard: the exact-named file must really be absent, otherwise this test
	// would pass by reading it and prove nothing about the fallback.
	if _, err := os.Stat(filepath.Join(d.Path, "vates-node.yaml")); err == nil {
		t.Fatal("test setup is wrong: vates-node.yaml exists, so the fallback is not exercised")
	}

	b, err := d.File("vates-node.yaml")
	if err != nil {
		t.Fatalf("File() did not fall back to the upper-cased name: %v", err)
	}
	if string(b) != "role: worker\n" {
		t.Errorf("File() = %q, want the file's content", b)
	}
}

func TestFileReportsMissing(t *testing.T) {
	d := newDrive(t, map[string]string{"other": "x"})

	_, err := d.File("vates-node.yaml")
	if err == nil {
		t.Fatal("File() succeeded for an absent file")
	}
	if !strings.Contains(err.Error(), "not found on the config drive") {
		t.Errorf("error was %q, want it to say the file was not found", err)
	}
}

func TestHas(t *testing.T) {
	d := newDrive(t, map[string]string{"pki/ca.crt": "cert"})

	// Has takes a path relative to the drive, and directories are fine.
	if !d.Has("pki/ca.crt") {
		t.Error("Has() = false for a file that exists")
	}
	if d.Has("pki/ca.key") {
		t.Error("Has() = true for a file that does not exist")
	}
}

func TestFindWithoutDriveIsNotFound(t *testing.T) {
	// On this machine there is no /dev/disk/by-label/cidata, so Find must
	// report ErrNotFound rather than something cryptic.
	_, err := Find()
	if err == nil {
		t.Skip("a config drive happens to be attached; skipping the not-found case")
	}
	if !strings.Contains(err.Error(), "config drive") {
		t.Errorf("error was %q, want it to mention the config drive", err)
	}
}
