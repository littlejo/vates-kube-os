package firstboot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vatesfr/vates-kube-os/internal/configdrive"
	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// driveWith writes a set of files into a temporary directory and opens it as a
// config drive, the way the other firstboot tests build their drives.
func driveWith(t *testing.T, files map[string]string) *configdrive.Drive {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return configdrive.Open(dir)
}

func TestReadConfigFromUserData(t *testing.T) {
	// The CAPI path: only meta-data and user-data are on the drive, which is all
	// a hypervisor builds from a bootstrap provider's payload. There is no
	// vates-node.yaml for the provider to place.
	d := driveWith(t, map[string]string{
		"meta-data": "instance-id: i-1\nlocal-hostname: vates-worker-1\n",
		"user-data": `role: worker
kubernetes:
  version: v1.31.0
cluster:
  controlPlaneEndpoint: "192.168.1.10:6443"
  token: "abcdef.0123456789abcdef"
network:
  iface: eth0
  mode: dhcp
cni:
  plugin: flannel
  cidr: "10.244.0.0/16"
`,
	})

	cfg, name, source, err := readConfig(d)
	if err != nil {
		t.Fatalf("readConfig() failed: %v", err)
	}
	if cfg.Role != vatescfg.RoleWorker {
		t.Errorf("role = %q, want worker", cfg.Role)
	}
	if cfg.Kubernetes.Version != "v1.31.0" {
		t.Errorf("version = %q, want v1.31.0 (from user-data)", cfg.Kubernetes.Version)
	}
	if name != "vates-worker-1" {
		t.Errorf("name = %q, want it from meta-data", name)
	}
	if source != "user-data" {
		t.Errorf("source = %q, want user-data", source)
	}
}

// TestReadConfigNodeName covers where the node name comes from: the document
// states it when the bootstrap provider knows it (CAPI), and the drive's
// meta-data is the fallback for a hand-built drive. A hypervisor that writes
// meta-data without local-hostname -- Xen Orchestra writes instance-id only --
// is no longer fatal when the document names the node.
func TestReadConfigNodeName(t *testing.T) {
	const workerBody = `kubernetes:
  version: v1.31.0
cluster:
  controlPlaneEndpoint: "192.168.1.10:6443"
  token: "abcdef.0123456789abcdef"
network:
  iface: eth0
  mode: dhcp
cni:
  plugin: flannel
  cidr: "10.244.0.0/16"
`
	tests := []struct {
		name     string
		metaData string
		userData string
		wantName string
		wantErr  string
	}{
		{
			name:     "the document's name wins over meta-data",
			metaData: "instance-id: i-1\nlocal-hostname: from-metadata\n",
			userData: "role: worker\nnode:\n  name: from-document\n" + workerBody,
			wantName: "from-document",
		},
		{
			name:     "meta-data local-hostname is the fallback when the document states none",
			metaData: "instance-id: i-1\nlocal-hostname: from-metadata\n",
			userData: "role: worker\n" + workerBody,
			wantName: "from-metadata",
		},
		{
			name:     "meta-data without local-hostname is fine when the document names the node",
			metaData: "instance-id: i-1\n",
			userData: "role: worker\nnode:\n  name: vates-cp-0\n" + workerBody,
			wantName: "vates-cp-0",
		},
		{
			name:     "neither source names the node",
			metaData: "instance-id: i-1\n",
			userData: "role: worker\n" + workerBody,
			wantErr:  "local-hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := driveWith(t, map[string]string{"meta-data": tt.metaData, "user-data": tt.userData})
			_, gotName, _, err := readConfig(d)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("readConfig() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("readConfig() error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("readConfig() unexpected error = %v", err)
			}
			if gotName != tt.wantName {
				t.Errorf("name = %q, want %q", gotName, tt.wantName)
			}
		})
	}
}

func TestReadConfigIsStrictInsideUserData(t *testing.T) {
	// The strict decoding guarantee applies to a document read from user-data
	// exactly as to the file: a typo in a field name is a hard error, named,
	// rather than a half-configured node.
	d := driveWith(t, map[string]string{
		"meta-data": "local-hostname: vates-worker-1\n",
		"user-data": "role: worker\nrol: worker\n",
	})

	_, _, _, err := readConfig(d)
	if err == nil {
		t.Fatal("readConfig() accepted a misspelled key under vates:")
	}
	if got := err.Error(); !strings.Contains(got, "field rol not found") {
		t.Errorf("error was %q, want it to name the unknown field", got)
	}
	if got := err.Error(); !strings.Contains(got, "user-data") {
		t.Errorf("error was %q, want it to name user-data as the source", got)
	}
}
