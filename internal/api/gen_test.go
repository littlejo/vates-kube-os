package api

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	apiv1 "github.com/vatesfr/vates-kube-os/proto/vates/api/v1"
)

// serve starts a server on an ephemeral port and returns its address.
func serve(t *testing.T, opts Options) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	go func() { _ = ServeListener(lis, opts) }()
	return lis.Addr().String()
}

func callStatus(t *testing.T, addr, kubeconfig string) error {
	t.Helper()
	conn, err := Dial(addr, kubeconfig)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = apiv1.NewVatesAPIClient(conn).GetStatus(ctx, &apiv1.GetStatusRequest{})
	return err
}

// TestOperatorCAGovernsTheAPI is the point of the operator CA: a node that
// generated its own cluster CA (kubeadm) can still be called, because the
// operator injects an authority of its own that signs the server certificate and
// verifies the client. Nothing is fetched from the node; the operator generated
// its credential before booting it.
func TestOperatorCAGovernsTheAPI(t *testing.T) {
	dir := t.TempDir()
	operator := filepath.Join(dir, "operator")
	kubeconfig, err := GenerateClientPKI(operator, "vateskctl")
	if err != nil {
		t.Fatalf("GenerateClientPKI: %v", err)
	}

	addr := serve(t, Options{
		CAPath:    filepath.Join(operator, "api-ca.crt"),
		CAKeyPath: filepath.Join(operator, "api-ca.key"),
		CertPath:  filepath.Join(dir, "api", "tls.crt"),
		KeyPath:   filepath.Join(dir, "api", "tls.key"),
	})
	if err := callStatus(t, addr, kubeconfig); err != nil {
		t.Fatalf("the operator's own CA was not enough: %v", err)
	}
}

// The cluster CA path (CAPI): the node keeps its self-generated cluster CA, and
// a cluster kubeconfig authenticates. The operator's separate CA must NOT be
// accepted then -- trust is opt-in, by what was injected.
func TestOperatorCAIsNotTrustedWhenTheClusterCARules(t *testing.T) {
	dir := t.TempDir()

	// The client trusts the operator CA only (its kubeconfig embeds it), so it
	// cannot verify a server certificate signed by the cluster CA.
	operator := filepath.Join(dir, "operator")
	kubeconfig, err := GenerateClientPKI(operator, "vateskctl")
	if err != nil {
		t.Fatal(err)
	}

	_, clusterCA, clusterCAKey := writeTestCA(t, dir)
	addr := serve(t, Options{
		CAPath:    clusterCA,
		CAKeyPath: clusterCAKey,
		CertPath:  filepath.Join(dir, "api", "tls.crt"),
		KeyPath:   filepath.Join(dir, "api", "tls.key"),
	})
	if err := callStatus(t, addr, kubeconfig); err == nil {
		t.Fatal("an operator credential was accepted although the operator CA was not injected")
	}
}
