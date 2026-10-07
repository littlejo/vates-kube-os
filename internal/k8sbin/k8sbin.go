package k8sbin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// DefaultBase is where Kubernetes publishes its release binaries.
const DefaultBase = "https://dl.k8s.io/release"

// archAMD64 and archARM64 are the architectures this project builds for and the
// ones the URL rules are exercised against. Named so that the allow-list below
// and the tests cannot drift apart on the spelling.
const (
	archAMD64 = "amd64"
	archARM64 = "arm64"
)

// The components fetched this way. Only these are ever needed, and naming them
// here means a typo in a wrapper script fails at the launcher rather than as a
// download of something that does not exist.
//
// mounter is among them and is easy to overlook: it is a genuine Kubernetes
// release artifact (1.6 MB against the kubelet's 77), published on the same path,
// and it is what the kubelet execs to perform mounts. Leaving it out of the image
// without providing it here would produce a node whose pods never get a volume.
const (
	Kubelet = "kubelet"
	Kubeadm = "kubeadm"
	Kubectl = "kubectl"
	Mounter = "mounter"
)

// Components is the allow-list, in the order a node needs them.
var Components = []string{Kubelet, Kubeadm, Kubectl, Mounter}

// Known reports whether a name is one of the components.
func Known(name string) bool {
	return slices.Contains(Components, name)
}

// URL is where a component's binary is published.
func URL(base, version, arch, name string) string {
	return strings.Join([]string{strings.TrimRight(base, "/"), version, "bin", "linux", arch, name}, "/")
}

// ChecksumURL is where its checksum is published: beside the binary, with
// ".sha256" appended.
//
// Worth being honest about what this proves. The checksum comes from the same
// host as the binary, so it detects a truncated or corrupted download and does
// NOT defend against a malicious mirror. That is the same guarantee the image
// build has always had -- it fetches the .sha256 from dl.k8s.io exactly like
// this -- so moving the fetch to first boot changes nothing about the trust
// model. Stronger provenance would mean signature verification, which is a
// separate improvement rather than a prerequisite.
func ChecksumURL(base, version, arch, name string) string {
	return URL(base, version, arch, name) + ".sha256"
}

// Arch maps a Go architecture to the name Kubernetes publishes.
//
// They agree for the architectures this project builds for, but the mapping is
// explicit rather than assumed: passing "x86_64" or an empty string through
// would produce a URL that 404s, and the error would name the download rather
// than the architecture.
func Arch(goarch string) (string, error) {
	switch goarch {
	case archAMD64, archARM64, "ppc64le", "s390x":
		return goarch, nil
	default:
		return "", fmt.Errorf("no Kubernetes release binaries for architecture %q", goarch)
	}
}

// CachePath is where a fetched binary is kept.
//
// Under the version and architecture, so several versions can coexist on one
// machine during an upgrade, and a fetch never overwrites a binary another
// component is executing.
func CachePath(root, version, arch, name string) string {
	return strings.Join([]string{strings.TrimRight(root, "/"), version, arch, name}, "/")
}

// ParseChecksum reads the published checksum.
//
// The file holds the bare hash: no file name, no trailing newline. It is
// validated as 64 hex characters rather than trusted, because comparing a
// download against a garbage string would fail in a way that points at the
// binary instead of at the checksum.
func ParseChecksum(body []byte) (string, error) {
	sum := strings.ToLower(strings.TrimSpace(string(body)))
	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("the published checksum is %d characters, not %d", len(sum), sha256.Size*2)
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("the published checksum is not hexadecimal: %w", err)
	}
	return sum, nil
}

// Verify compares a downloaded binary against a checksum.
func Verify(content []byte, want string) error {
	got := hex.EncodeToString(hash(content))
	if got != want {
		return fmt.Errorf("the downloaded binary does not match its published checksum (want %s, got %s)", want, got)
	}
	return nil
}

func hash(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
