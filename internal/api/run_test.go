package api

import "testing"

// The CA paths must be empty when no flag is given, so that api.Serve can pick
// the operator CA when it was injected. Defaulting them here to the cluster CA
// made the API sign with the wrong authority, and the operator's client refused
// the node: a bug only an end-to-end run exposed.
func TestAPIOptionsLeaveTheAuthorityUnsetByDefault(t *testing.T) {
	opts, err := options(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.CAPath != "" || opts.CAKeyPath != "" {
		t.Fatalf("options() pre-filled the CA: %+v; api.Serve must choose it", opts)
	}
	if opts.CertPath != "" || opts.KeyPath != "" {
		t.Fatalf("options() pre-filled the certificate paths: %+v", opts)
	}
	if opts.Listen != ":50000" {
		t.Errorf("options() listen = %q, want :50000", opts.Listen)
	}
}
