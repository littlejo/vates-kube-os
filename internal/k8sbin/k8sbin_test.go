package k8sbin

import "testing"

func TestURLIsTheLayoutKubernetesPublishes(t *testing.T) {
	const base = "https://dl.k8s.io/release"
	cases := []struct {
		version, arch, name, want string
	}{
		{"v1.31.4", "amd64", "kubelet", "https://dl.k8s.io/release/v1.31.4/bin/linux/amd64/kubelet"},
		{"v1.32.0", "arm64", "kubeadm", "https://dl.k8s.io/release/v1.32.0/bin/linux/arm64/kubeadm"},
	}
	for _, c := range cases {
		if got := URL(base, c.version, c.arch, c.name); got != c.want {
			t.Errorf("URL() = %q, want %q", got, c.want)
		}
	}

	// The checksum is published beside the binary, with the suffix appended.
	if got := ChecksumURL(base, "v1.31.4", "amd64", "kubelet"); got != cases[0].want+".sha256" {
		t.Errorf("ChecksumURL() = %q", got)
	}

	// A base with a trailing slash must not produce a double slash: a mirror is
	// configured by a person, and a URL with // in it 404s on some servers.
	if got := URL(base+"/", "v1.31.4", "amd64", "kubelet"); got != cases[0].want {
		t.Errorf("URL() with a trailing slash = %q, want %q", got, cases[0].want)
	}
}

func TestArchRefusesWhatKubernetesDoesNotPublish(t *testing.T) {
	for _, ok := range []string{"amd64", "arm64", "ppc64le", "s390x"} {
		if got, err := Arch(ok); err != nil || got != ok {
			t.Errorf("Arch(%q) = %q, %v", ok, got, err)
		}
	}
	// The uname spelling and the empty string are the two a careless caller
	// produces, and both would build a URL that 404s -- naming the download
	// rather than the architecture.
	for _, bad := range []string{"x86_64", "aarch64", "", "riscv64"} {
		if got, err := Arch(bad); err == nil {
			t.Errorf("Arch(%q) was accepted as %q", bad, got)
		}
	}
}

func TestCachePathSeparatesVersionsAndArchitectures(t *testing.T) {
	// Two versions on one machine during an upgrade must not overwrite each
	// other: the older kubelet is still running while the newer one is fetched.
	a := CachePath("/var/lib/vates/kubernetes", "v1.31.4", "amd64", "kubelet")
	b := CachePath("/var/lib/vates/kubernetes", "v1.32.0", "amd64", "kubelet")
	c := CachePath("/var/lib/vates/kubernetes", "v1.31.4", "arm64", "kubelet")
	if a == b || a == c || b == c {
		t.Errorf("cache paths collide: %q %q %q", a, b, c)
	}
	if a != "/var/lib/vates/kubernetes/v1.31.4/amd64/kubelet" {
		t.Errorf("CachePath() = %q", a)
	}
}

func TestParseChecksumRejectsWhatIsNotAChecksum(t *testing.T) {
	// The published file is a bare hash. A wrong-length or non-hex value would
	// make the comparison fail in a way that points at the binary rather than at
	// the checksum, so it is refused here.
	good := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got, err := ParseChecksum([]byte(good + "\n")); err != nil || got != good {
		t.Errorf("ParseChecksum(valid) = %q, %v", got, err)
	}
	for _, bad := range []string{"", "abc", good + "00", "zz" + good[2:]} {
		if _, err := ParseChecksum([]byte(bad)); err == nil {
			t.Errorf("ParseChecksum(%q) was accepted", bad)
		}
	}
}

func TestVerifyDetectsAMismatch(t *testing.T) {
	content := []byte("a binary")
	good := "4b3f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"
	if err := Verify(content, good); err == nil {
		t.Error("Verify accepted a checksum that does not match")
	}
}

func TestKnownRefusesAnythingElse(t *testing.T) {
	for _, c := range Components {
		if !Known(c) {
			t.Errorf("%q should be known", c)
		}
	}
	for _, bad := range []string{"", "sh", "kubelet ", "KUBELET"} {
		if Known(bad) {
			t.Errorf("%q should not be known", bad)
		}
	}
}
