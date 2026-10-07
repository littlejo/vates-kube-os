package vatescfg

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// UserData encodes the configuration as the user-data document a CAPI bootstrap
// provider hands to the infrastructure provider, which passes it to the
// hypervisor unchanged.
//
// The document IS the vates-node.yaml schema: `user-data` is the NoCloud slot
// for a machine's configuration, not a cloud-init script, and nothing wraps it.
// A hypervisor passes it through without reading it. This OS does not run
// cloud-init, so no cloud-seeming envelope is needed or wanted.
func (c *Config) UserData() ([]byte, error) {
	// Refused here rather than on the node: a provider that emits an incoherent
	// document should learn it now, not three steps later in a kubeadm error.
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to encode an invalid configuration: %w", err)
	}
	doc, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("encoding the configuration as user-data: %w", err)
	}
	return doc, nil
}

// FromUserData returns the user-data document, and false when there is none.
//
// There is no envelope to look for: user-data carries the configuration as-is,
// so there is nothing to extract and nothing to strip. What makes it absent is
// having no content at all -- blank lines and comments only, which is what a
// NoCloud placeholder such as "#cloud-config" leaves behind. That is the case
// the caller falls back to vates-node.yaml for.
//
// The bytes are returned as they were, never re-encoded, so a decoding error
// names a line in the real document. Whether they are a valid configuration is
// not decided here: Load is what enforces that, strictly.
func FromUserData(raw []byte) (doc []byte, ok bool) {
	if !hasContent(raw) {
		return nil, false
	}
	return raw, true
}

// hasContent reports whether a document has anything but blank lines and
// comments.
//
// It walks the lines rather than parsing YAML on purpose: the question here is
// only "is there something to read", and a whitespace quirk (a stray tab, say)
// must not turn an empty placeholder into a document the node then fails to
// parse. Whether what is there is valid YAML is Load's business.
func hasContent(raw []byte) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return true
	}
	return false
}
