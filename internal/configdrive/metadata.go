package configdrive

import (
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// nodeNameRe is RFC 1123 subdomain, which is what Kubernetes accepts for an
// object name. Checking it here means a bad hostname is caught at first boot,
// with a message naming the file, rather than as a rejected node registration
// much later.
var nodeNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// Metadata is the NoCloud meta-data document.
//
// Only the two keys this system needs are decoded, but decoding is strict for
// the same reason vates-node.yaml is: a misspelled key must not be ignored. NoCloud
// producers add keys this system does not care about, so the set below is
// deliberately the whole of what is accepted -- anything else is a signal that
// the drive was written for something else.
type Metadata struct {
	InstanceID    string `yaml:"instance-id"`
	LocalHostname string `yaml:"local-hostname"`
}

// NodeName returns the name this machine should be known by in Kubernetes when
// the configuration document states none.
//
// The document's node.name is the first source, and the one a CAPI bootstrap
// provider fills from the Machine's name; this meta-data path is the fallback a
// hand-built drive uses. NoCloud's local-hostname is the machine's hostname,
// which is exactly what the node name is.
func (d *Drive) NodeName() (string, error) {
	raw, err := d.File("meta-data")
	if err != nil {
		return "", fmt.Errorf("reading meta-data for the node name: %w", err)
	}

	var m Metadata
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	// An empty document is not a parse failure, it is simply a meta-data file
	// with nothing in it, and the useful message is the one about the missing
	// local-hostname further down. Reporting "EOF" here would name the decoder
	// rather than the actual problem.
	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("parsing meta-data: %w", err)
	}

	name := strings.TrimSpace(m.LocalHostname)
	if name == "" {
		return "", fmt.Errorf("meta-data has no local-hostname; the node name is taken from it")
	}
	// An address as a node name would change with the DHCP lease, and a renamed
	// node is a different node to Kubernetes -- it would silently orphan every
	// pod bound to the old name.
	//
	// This has to be an explicit check: "10.0.2.15" is a perfectly valid RFC
	// 1123 subdomain, so the pattern below accepts it and the rule has to be
	// stated separately. Verified by test, which is how the gap was found.
	if net.ParseIP(name) != nil {
		return "", fmt.Errorf("meta-data local-hostname %q is an IP address; the node name must be stable while the address may change", name)
	}
	if !nodeNameRe.MatchString(name) {
		return "", fmt.Errorf("meta-data local-hostname %q is not usable as a Kubernetes node name (lowercase RFC 1123 subdomain)", name)
	}
	return name, nil
}
