// Package vatescfg defines the Vates Kube OS node configuration, the single
// vates-node.yaml document that the CAPI provider writes onto the node's config
// drive and that vates-init reads at first boot.
//
// This package is deliberately dependency-free apart from the YAML decoder and
// the small k8sbin helper it uses to validate the binary URLs, and it lives
// outside internal/ for one reason: cluster-api-provider-vates imports exactly
// these types, so the schema cannot drift between the two projects.
package vatescfg
