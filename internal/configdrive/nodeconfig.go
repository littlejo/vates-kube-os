package configdrive

import (
	"fmt"

	"github.com/vatesfr/vates-kube-os/vatescfg"
)

// NodeConfig returns the node configuration document and the drive entry it was
// read from.
//
// It may be delivered two ways, and the user-data slot wins when both are
// present:
//
//   - the user-data slot, holding the vates-node.yaml document as-is. This is
//     the CAPI path: the bootstrap provider's payload is passed through
//     unchanged to the hypervisor, which builds the config drive from it, so
//     there is no separate file for the provider to place.
//   - the vates-node.yaml file, the direct-drive path (mkconfigdrive.sh, a
//     hand-built libvirt drive).
//
// A user-data that is empty or only comments (the NoCloud placeholder) carries
// no configuration and is not an error: it falls back to the file. Whatever
// document is found, the caller decodes it strictly with vatescfg.Load.
func (d *Drive) NodeConfig() ([]byte, string, error) {
	if raw, err := d.File("user-data"); err == nil {
		if doc, ok := vatescfg.FromUserData(raw); ok {
			return doc, "user-data", nil
		}
	}
	if raw, err := d.File("vates-node.yaml"); err == nil {
		return raw, "vates-node.yaml", nil
	}
	return nil, "", fmt.Errorf(
		"no vates configuration: user-data is empty or absent and there is no vates-node.yaml file")
}
