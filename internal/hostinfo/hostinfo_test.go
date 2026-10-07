package hostinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIPv4FromHexReadsLittleEndian(t *testing.T) {
	// /proc/net/route writes the gateway as a 32-bit little-endian number. The
	// bytes therefore come out reversed if read as written, and 192.168.1.1
	// shows up as 1.1.168.192 -- which is a plausible-looking address, so the
	// mistake survives review and is only caught by looking at a real machine.
	cases := map[string]string{
		"0101A8C0": "192.168.1.1",
		"0101A8FE": "254.168.1.1",
		"00000000": "0.0.0.0",
		"0100000A": "10.0.0.1",
	}
	for in, want := range cases {
		if got := ipv4FromHex(in); got != want {
			t.Errorf("ipv4FromHex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIPv4FromHexRejectsNonsense(t *testing.T) {
	if got := ipv4FromHex("not-hex"); got != "" {
		t.Errorf("ipv4FromHex on a non-hex value = %q, want empty", got)
	}
}

func TestResolveNodeNamePrefersTheWrittenNameToTheHostname(t *testing.T) {
	// The name vates-init writes comes from the config drive's meta-data, which
	// is what Kubernetes registers the node as. The hostname is set from the
	// same meta-data by cloud init and, before it has run, still holds the name
	// of the machine the image was built on -- which is how a console ended up
	// asking the API about a node called "vates-build".
	dropIn := filepath.Join(t.TempDir(), "10-node.conf")
	content := "[Service]\nEnvironment=NODE_NAME=vates-cp-1\nEnvironment=NODE_IP=192.0.2.9\n"
	if err := os.WriteFile(dropIn, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the drop-in: %v", err)
	}

	if got := NodeNameFromDropIn(dropIn); got != "vates-cp-1" {
		t.Errorf("NodeNameFromDropIn = %q, want vates-cp-1", got)
	}
	if got := ResolveNodeName("", dropIn); got != "vates-cp-1" {
		t.Errorf("ResolveNodeName = %q, want the written name", got)
	}
	if got := ResolveNodeName("explicit", dropIn); got != "explicit" {
		t.Errorf("ResolveNodeName ignored the override: %q", got)
	}
}

func TestResolveNodeNameFallsBackToTheHostname(t *testing.T) {
	// Before first boot there is no drop-in, and the console must still have
	// something to call itself -- and must not fail.
	host, err := os.Hostname()
	if err != nil {
		t.Skip("no hostname available")
	}
	missing := filepath.Join(t.TempDir(), "absent.conf")
	if got := NodeNameFromDropIn(missing); got != "" {
		t.Errorf("NodeNameFromDropIn on a missing file = %q, want empty", got)
	}
	if got := ResolveNodeName("", missing); got != host {
		t.Errorf("ResolveNodeName = %q, want the hostname %q", got, host)
	}
}
