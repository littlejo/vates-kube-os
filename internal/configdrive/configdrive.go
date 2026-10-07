package configdrive

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Labels are the volume labels accepted, in order. NoCloud uses "cidata";
// some tooling upper-cases it.
var Labels = []string{"cidata", "CIDATA"}

// ErrNotFound means no config drive is attached.
var ErrNotFound = errors.New("no config drive found")

// Drive is a mounted config drive.
type Drive struct {
	// Path is the directory the drive is mounted at. It is exported so callers
	// can report it, and so tests can point a Drive at a plain directory.
	Path string

	mounted bool
}

// Find locates the config drive by volume label and mounts it read-only.
//
// Read-only because the node must never write to it: it is the provider's
// statement of what this machine should be, and re-reading it after a mutation
// would mask exactly the kind of drift this system exists to avoid.
func Find() (*Drive, error) {
	for _, label := range Labels {
		dev := filepath.Join("/dev/disk/by-label", label)
		if _, err := os.Stat(dev); err == nil {
			d, err := Mount(dev)
			if err != nil {
				return nil, fmt.Errorf("config drive %s (label %s): %w", dev, label, err)
			}
			return d, nil
		}
		// /dev/disk/by-label is created by udev. A system without udev (the
		// image, where `vates` is PID 1 and nothing runs udev) has no
		// such directory, so the device is asked for by label instead.
		if dev, err := byBlkid(label); err == nil {
			d, err := Mount(dev)
			if err != nil {
				return nil, fmt.Errorf("config drive %s (label %s): %w", dev, label, err)
			}
			return d, nil
		}
	}
	return nil, fmt.Errorf("%w: no device labelled %s (checked /dev/disk/by-label and blkid)",
		ErrNotFound, strings.Join(Labels, " or "))
}

// byBlkid resolves a device by filesystem label without udev. blkid may not be
// installed; the caller treats any error, including "not found", the same way.
func byBlkid(label string) (string, error) {
	out, err := exec.Command("blkid", "-L", label).Output()
	if err != nil {
		return "", err
	}
	dev := strings.TrimSpace(string(out))
	if dev == "" {
		return "", ErrNotFound
	}
	return dev, nil
}

// Mount mounts dev read-only and returns the Drive for it.
//
// mount(8) is used rather than a syscall so the behaviour matches what an
// operator would get by hand, and so auto-detected filesystem type works
// (the drive is ISO 9660, but a provider testing with a vfat image would still
// work).
func Mount(dev string) (*Drive, error) {
	dir, err := os.MkdirTemp("", "vates-configdrive-")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("mount", "-o", "ro", dev, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		// The mount failed, so the temporary directory is empty and unused.
		// Removing it cannot lose anything, and the mount error is the one
		// worth reporting, so a failure here is deliberately ignored.
		_ = os.Remove(dir)
		return nil, fmt.Errorf("mount %s: %w: %s", dev, err, strings.TrimSpace(string(out)))
	}
	return &Drive{Path: dir, mounted: true}, nil
}

// Open returns a Drive for an already-accessible directory. It does not check
// that the directory looks like a config drive; File reports a missing
// vates-node.yaml plainly enough.
func Open(dir string) *Drive { return &Drive{Path: dir} }

// Close unmounts the drive if this Drive mounted it.
func (d *Drive) Close() error {
	if d == nil || !d.mounted {
		return nil
	}
	if out, err := exec.Command("umount", d.Path).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %w: %s", d.Path, err, strings.TrimSpace(string(out)))
	}
	d.mounted = false
	return os.Remove(d.Path)
}

// File reads a file from the drive.
//
// NoCloud producers flatten names to the ISO9660 8.3 form on some ISOs
// ("VATES.YAM"), so a miss is retried in upper case before giving up: an
// operator should not have to care which producer wrote the image.
func (d *Drive) File(name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(d.Path, name))
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	upper := strings.ToUpper(name)
	if alt, err2 := os.ReadFile(filepath.Join(d.Path, upper)); err2 == nil {
		return alt, nil
	}
	// ISO9660 may also truncate the name; scan for it case-insensitively.
	entries, derr := os.ReadDir(d.Path)
	if derr == nil {
		for _, e := range entries {
			if strings.HasPrefix(strings.ToUpper(e.Name()), upper[:min(len(upper), 8)]) {
				if alt, err3 := os.ReadFile(filepath.Join(d.Path, e.Name())); err3 == nil {
					return alt, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("%s not found on the config drive at %s", name, d.Path)
}

// Has reports whether a file is present, for optional content such as PKI.
func (d *Drive) Has(name string) bool {
	_, err := d.File(name)
	return err == nil
}
