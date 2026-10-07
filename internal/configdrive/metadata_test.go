package configdrive

import (
	"strings"
	"testing"
)

func TestNodeNameFromLocalHostname(t *testing.T) {
	d := newDrive(t, map[string]string{
		"meta-data": "instance-id: i-123\nlocal-hostname: vates-cp-1\n",
	})

	name, err := d.NodeName()
	if err != nil {
		t.Fatalf("NodeName() failed: %v", err)
	}
	if name != "vates-cp-1" {
		t.Errorf("NodeName() = %q, want %q", name, "vates-cp-1")
	}
}

func TestNodeNameRejectsAnAddress(t *testing.T) {
	// An IP as a node name would change with the DHCP lease, and Kubernetes
	// would see a different node.
	d := newDrive(t, map[string]string{
		"meta-data": "instance-id: i-123\nlocal-hostname: 10.0.2.15\n",
	})

	if _, err := d.NodeName(); err == nil {
		t.Fatal("NodeName() accepted an IP address as a node name")
	} else if !strings.Contains(err.Error(), "is an IP address") {
		t.Errorf("error was %q, want it to say the name is an address", err)
	}
}

func TestNodeNameRejects(t *testing.T) {
	cases := map[string]struct{ meta, wantSub string }{
		"missing local-hostname": {
			meta:    "instance-id: i-123\n",
			wantSub: "no local-hostname",
		},
		"uppercase": {
			meta:    "local-hostname: Vates-CP-1\n",
			wantSub: "RFC 1123",
		},
		"underscore": {
			meta:    "local-hostname: vates_cp_1\n",
			wantSub: "RFC 1123",
		},
		"leading dash": {
			meta:    "local-hostname: -vates\n",
			wantSub: "RFC 1123",
		},
		"unknown key": {
			// Strict decoding, for the same reason vates-node.yaml is strict.
			meta:    "local-hostname: vates-cp-1\nlocal_hostname: typo\n",
			wantSub: "field local_hostname not found",
		},
		"no meta-data at all": {
			meta:    "",
			wantSub: "no local-hostname",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDrive(t, map[string]string{"meta-data": tc.meta})
			_, err := d.NodeName()
			if err == nil {
				t.Fatalf("NodeName() accepted invalid meta-data (%s)", name)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error was %q, want it to contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestNodeNameReportsMissingMetaData(t *testing.T) {
	d := newDrive(t, map[string]string{"vates-node.yaml": "role: worker\n"})

	_, err := d.NodeName()
	if err == nil {
		t.Fatal("NodeName() succeeded with no meta-data on the drive")
	}
	if !strings.Contains(err.Error(), "meta-data") {
		t.Errorf("error was %q, want it to name meta-data as the missing file", err)
	}
}
