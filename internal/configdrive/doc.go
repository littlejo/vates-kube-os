// Package configdrive locates and reads the NoCloud config drive that carries
// the node configuration (vates-node.yaml) and the node's PKI.
//
// The node configuration may be delivered two ways: as the user-data document
// itself -- the CAPI path, since a bootstrap provider's payload reaches the
// hypervisor as user-data with no channel for an extra file -- or as a
// vates-node.yaml file, the direct-drive path. NodeConfig reads whichever is
// present. The drive is otherwise a plain NoCloud drive.
//
// The CAPI provider attaches it as a small read-only ISO labelled "cidata" (the
// NoCloud convention, upper-cased "CIDATA" by some producers). Nothing about the
// node can be configured without it, so the lookup is explicit and the failure
// is loud rather than a default that quietly does the wrong thing.
package configdrive
