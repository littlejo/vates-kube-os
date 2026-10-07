// Package api is the node's management API: the gRPC service a provider or
// vateskctl talks to, authenticated with the cluster's certificate authority.
//
// It is the SSH replacement. This OS carries no shell and no sshd; a provider
// that needs to ask a node something -- is it up, hand me the kubeconfig -- asks
// here, over mutual TLS.
//
// The port is 50000, the immutable-OS convention, configurable per node through
// api.port in vates-node.yaml. The methods are a fixed allow-list, not a shell:
// the surface is what the proto declares and nothing else.
package api
