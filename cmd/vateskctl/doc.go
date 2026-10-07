// Command vateskctl is the operator's CLI for Vates Kube OS.
//
// It talks to a node's management API over mutual TLS. It never opens a shell:
// every verb maps to one API method. This is why the API exists -- the CLI is
// the second consumer, beside the CAPI provider.
package main
