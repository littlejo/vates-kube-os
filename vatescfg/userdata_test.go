package vatescfg

import (
	"bytes"
	"strings"
	"testing"
)

func loadForTest(t *testing.T, doc string) *Config {
	t.Helper()
	c, err := Load([]byte(doc))
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	return c
}

func TestUserDataRoundTrip(t *testing.T) {
	// The contract is a round trip: what a CABP encodes with UserData, a node
	// reads back with FromUserData and Load.
	want := loadForTest(t, baseWorker)

	raw, err := want.UserData()
	if err != nil {
		t.Fatalf("UserData() failed: %v", err)
	}
	// No envelope: the document is the schema, not a cloud-init script.
	if strings.Contains(string(raw), "#cloud-config") {
		t.Errorf("UserData() carries a cloud-init header it does not honour:\n%s", raw)
	}

	doc, ok := FromUserData(raw)
	if !ok {
		t.Fatal("FromUserData() did not recognise the document UserData() produced")
	}
	got := loadForTest(t, string(doc))
	if got.Role != want.Role ||
		got.Kubernetes.Version != want.Kubernetes.Version ||
		got.Cluster.ControlPlaneEndpoint != want.Cluster.ControlPlaneEndpoint {
		t.Errorf("the round trip lost data:\n got %+v\nwant %+v", got, want)
	}
}

func TestUserDataRefusesInvalid(t *testing.T) {
	// An incoherent document must not leave the provider at all: it is refused
	// here, where the provider can still name the field, rather than on the node.
	var c Config
	if _, err := c.UserData(); err == nil {
		t.Fatal("UserData() encoded a configuration with no role")
	}
}

func TestFromUserDataAbsent(t *testing.T) {
	// An empty user-data, or one that is nothing but comments and blank lines,
	// carries no configuration. "#cloud-config" is the NoCloud placeholder: it is
	// a comment, not a document, and it must not be mistaken for one.
	cases := map[string]string{
		"empty":             "",
		"blank":             "   \n\t\n",
		"comment only":      "#cloud-config\n",
		"comments and crlf": "# a note\r\n\r\n# another\r\n",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			doc, ok := FromUserData([]byte(raw))
			if ok {
				t.Errorf("FromUserData(%q) reported a document: %q", raw, doc)
			}
			if doc != nil {
				t.Errorf("FromUserData(%q) = %q, want nil", raw, doc)
			}
		})
	}
}

func TestFromUserDataPresentIsReturnedUnchanged(t *testing.T) {
	// The bytes are handed back as they came, never re-encoded: that is what
	// keeps a decoding error's line numbers pointing into the real document.
	raw := []byte(baseWorker)
	doc, ok := FromUserData(raw)
	if !ok {
		t.Fatal("FromUserData() did not see the document")
	}
	if !bytes.Equal(doc, raw) {
		t.Errorf("FromUserData() altered the document:\n got %q\nwant %q", doc, raw)
	}
}

func TestFromUserDataMalformedIsStillPresent(t *testing.T) {
	// Not YAML, but not empty either. It is the drive's user-data, so it is
	// handed on and Load reports the parse error; FromUserData must not swallow
	// it as "absent" and let a configured node look unconfigured.
	raw := []byte("role: worker\n  bad: [indent\n")
	doc, ok := FromUserData(raw)
	if !ok {
		t.Fatal("FromUserData() treated malformed but non-empty user-data as absent")
	}
	if !bytes.Equal(doc, raw) {
		t.Errorf("FromUserData() altered the document: %q", doc)
	}
	if _, err := Load(doc); err == nil {
		t.Fatal("Load() accepted malformed YAML; the fixture no longer exercises the case")
	}
}
