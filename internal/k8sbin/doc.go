// Package k8sbin knows where the Kubernetes binaries live and how to name them.
//
// It is a package of its own, rather than a couple of functions inside the
// schema, because two very different places need the same rule: the schema
// (vatescfg) computes the URL to validate it, and the launcher computes it to
// fetch. Two copies of a naming rule is two chances for them to disagree, and
// the failure would be a node that cannot start with a message about a 404.
//
// The layout is Kubernetes' own, so the public value needs no explanation and an
// internal mirror only has to reproduce a directory tree:
//
//	{base}/{version}/bin/linux/{arch}/{name}
//	{base}/{version}/bin/linux/{arch}/{name}.sha256
package k8sbin
