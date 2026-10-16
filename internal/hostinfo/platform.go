package hostinfo

import (
	"os"
	"strings"
)

// Platform is the hypervisor this machine runs on, or the absence of one.
//
// It is what PID 1 uses to decide what to start and how to draw: the Xen guest
// agent runs only on Xen, the console's serial face is the Xen one, and so on.
// The whole point is that a node detects the platform it is on rather than the
// image assuming it -- which is what lets one image run under KVM and XCP-ng.
type Platform string

const (
	PlatformKVM       Platform = "kvm"
	PlatformXen       Platform = "xen"
	PlatformBareMetal Platform = "bare-metal"
	PlatformUnknown   Platform = "unknown"
)

// Name is the human-readable form shown on the console and in the log.
func (p Platform) Name() string {
	switch p {
	case PlatformKVM:
		return "KVM"
	case PlatformXen:
		return "Xen / XCP-ng"
	case PlatformBareMetal:
		return "bare metal"
	default:
		return "unknown"
	}
}

// Detect returns the hypervisor this machine runs on, detected from the
// kernel's own view of itself. It never fails: an unrecognised machine is
// PlatformUnknown, not an error. It must be called after /sys is mounted,
// because it reads /sys/hypervisor and /sys/class/dmi.
func Detect() Platform {
	return detectPlatform(
		readLine("/sys/hypervisor/type"),
		readLine("/sys/class/dmi/id/sys_vendor"),
		readLine("/sys/class/dmi/id/product_name"),
		readLine("/sys/class/dmi/id/bios_vendor"),
		fileExists("/proc/xen"),
	)
}

// detectPlatform is the pure core: given the machine's identity strings, which
// platform is it. The order is deliberate -- Xen is checked first, because a Xen
// guest can also present a QEMU/SeaBIOS identity, and /proc/xen is the kernel's
// own detection of Xen, the most reliable signal there is.
func detectPlatform(xenType, sysVendor, product, biosVendor string, xenProc bool) Platform {
	switch {
	case xenProc || strings.EqualFold(xenType, "xen"):
		return PlatformXen
	case strings.EqualFold(xenType, "kvm"):
		return PlatformKVM
	case strings.EqualFold(sysVendor, "xenserver") || strings.EqualFold(product, "xenserver"):
		return PlatformXen
	case strings.EqualFold(sysVendor, "qemu") || strings.EqualFold(product, "qemu") || strings.EqualFold(biosVendor, "qemu"):
		return PlatformKVM
	case strings.EqualFold(sysVendor, "kvm") || strings.EqualFold(product, "kvm"):
		return PlatformKVM
	default:
		return PlatformBareMetal
	}
}

// readLine returns the trimmed contents of a file, or "" when it is absent.
func readLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
