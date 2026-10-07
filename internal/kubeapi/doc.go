// Package kubeapi is a small Kubernetes client, used by the console dashboard.
//
// It is deliberately not client-go. The dashboard reads a node, its pods and
// recent events -- three GET requests and a handful of fields. client-go would
// bring a dependency tree an order of magnitude larger than the whole rest of
// the project to do that, and this program is baked into the OS image.
//
// What it needs from a kubeconfig is small too: one server URL, one client
// certificate, one key, one CA. That is parsed here rather than pulled in.
package kubeapi
