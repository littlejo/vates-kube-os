package configdrive

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// assertConfig decodes the document NodeConfig returned, confirming it is the
// vates-node.yaml schema and not some wrapper around it.
func assertConfig(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the returned document is not YAML: %v", err)
	}
	return doc
}

func TestNodeConfigFromUserData(t *testing.T) {
	// The CAPI path: user-data is the only slot a bootstrap provider's payload
	// reaches the hypervisor through, and it holds the document as-is.
	d := newDrive(t, map[string]string{
		"user-data": "role: worker\nkubernetes:\n  version: v1.31.0\n",
	})

	raw, source, err := d.NodeConfig()
	if err != nil {
		t.Fatalf("NodeConfig() failed: %v", err)
	}
	if source != "user-data" {
		t.Errorf("source = %q, want %q", source, "user-data")
	}
	if doc := assertConfig(t, raw); doc["role"] != "worker" {
		t.Errorf("role = %v, want worker", doc["role"])
	}
}

func TestNodeConfigUserDataWinsOverTheFile(t *testing.T) {
	// When a drive carries both, user-data is the CAPI statement and wins; the
	// file is the direct-drive convenience.
	d := newDrive(t, map[string]string{
		"user-data":       "role: worker\n",
		"vates-node.yaml": "role: master\n",
	})

	raw, source, err := d.NodeConfig()
	if err != nil {
		t.Fatalf("NodeConfig() failed: %v", err)
	}
	if source != "user-data" {
		t.Errorf("source = %q, want user-data to win", source)
	}
	if doc := assertConfig(t, raw); doc["role"] != "worker" {
		t.Errorf("role = %v, want worker (from user-data)", doc["role"])
	}
}

func TestNodeConfigFallsBack(t *testing.T) {
	// A user-data that carries no document -- the NoCloud placeholder, or an
	// empty file -- falls back to vates-node.yaml rather than failing. That is
	// the direct-drive path, where user-data exists only to please NoCloud.
	cases := map[string]struct {
		userData string
	}{
		"cloud-init placeholder": {userData: "#cloud-config\n"},
		"empty":                  {userData: ""},
		"comments only":          {userData: "# nothing here\n"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDrive(t, map[string]string{
				"user-data":       tc.userData,
				"vates-node.yaml": "role: master\n",
			})

			raw, source, err := d.NodeConfig()
			if err != nil {
				t.Fatalf("NodeConfig() failed: %v", err)
			}
			if source != "vates-node.yaml" {
				t.Errorf("source = %q, want the file to be the fallback", source)
			}
			if doc := assertConfig(t, raw); doc["role"] != "master" {
				t.Errorf("role = %v, want master (from the file)", doc["role"])
			}
		})
	}
}

func TestNodeConfigReportsMissing(t *testing.T) {
	// The error names both places that were looked at, so a provider that wrote
	// the wrong thing knows where the node expected it.
	cases := map[string]struct {
		files map[string]string
	}{
		"nothing at all": {
			files: map[string]string{"meta-data": "local-hostname: n\n"},
		},
		"only a placeholder user-data": {
			files: map[string]string{"user-data": "#cloud-config\n"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDrive(t, tc.files)
			_, _, err := d.NodeConfig()
			if err == nil {
				t.Fatalf("NodeConfig() accepted a drive with no configuration (%s)", name)
			}
			for _, want := range []string{"no vates configuration", "user-data", "vates-node.yaml"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error was %q, want it to mention %q", err.Error(), want)
				}
			}
		})
	}
}
