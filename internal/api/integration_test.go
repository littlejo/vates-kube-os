package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/vatesfr/vates-kube-os/proto/vates/api/v1"
)

// TestGetStatusOverMutualTLS exercises the whole path in one process: a server
// on an ephemeral port, a client built from a kubeconfig, mutual TLS, the group
// authorization, and the RPC itself. No VM, no network reachability games.
func TestGetStatusOverMutualTLS(t *testing.T) {
	dir := t.TempDir()
	ca, caCert, caKey := writeTestCA(t, dir)
	caPEM, err := os.ReadFile(caCert)
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{
		CAPath:    caCert,
		CAKeyPath: caKey,
		CertPath:  filepath.Join(dir, "api", "tls.crt"),
		KeyPath:   filepath.Join(dir, "api", "tls.key"),
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	go func() { _ = ServeListener(lis, opts) }()

	// A management client -- O=system:masters, as kubeadm's admin kubeconfig
	// carries -- is accepted.
	certPEM, keyPEM := signClient(t, ca, caKey, []string{"system:masters"})
	kc := writeKubeconfig(t, filepath.Join(dir, "good"), caPEM, certPEM, keyPEM)

	conn, err := Dial(lis.Addr().String(), kc)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := apiv1.NewVatesAPIClient(conn).GetStatus(ctx, &apiv1.GetStatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if resp.GetNodeName() == "" {
		t.Error("GetStatus returned an empty node name")
	}

	// A node certificate -- same CA, wrong group -- is refused. This is the rule
	// that stops a kubelet's own certificate from calling the management API.
	badCert, badKey := signClient(t, ca, caKey, []string{"system:nodes"})
	badKC := writeKubeconfig(t, filepath.Join(dir, "bad"), caPEM, badCert, badKey)
	badConn, err := Dial(lis.Addr().String(), badKC)
	if err != nil {
		t.Fatalf("Dial (bad client): %v", err)
	}
	defer badConn.Close()

	_, err = apiv1.NewVatesAPIClient(badConn).GetStatus(ctx, &apiv1.GetStatusRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a node certificate was not refused: err = %v", err)
	}
}

// signClient mints a client certificate signed by the cluster CA, with the given
// organizations.
func signClient(t *testing.T, ca *x509.Certificate, caKeyPath string, orgs []string) (certPEM, keyPEM []byte) {
	t.Helper()
	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	caKey, err := parsePrivateKey(caKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "vateskctl", Organization: orgs},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writeKubeconfig(t *testing.T, dir string, caPEM, certPEM, keyPEM []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString
	cfg := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: vates
  cluster:
    certificate-authority-data: %s
    server: https://example.invalid:50000
users:
- name: vateskctl
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: vates
  context: {cluster: vates, user: vateskctl}
current-context: vates
`, b64(caPEM), b64(certPEM), b64(keyPEM))
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetJoinMaterialOverMutualTLS(t *testing.T) {
	dir := t.TempDir()
	ca, caCert, caKey := writeTestCA(t, dir)
	caPEM, err := os.ReadFile(caCert)
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{
		CAPath:    caCert,
		CAKeyPath: caKey,
		CertPath:  filepath.Join(dir, "api", "tls.crt"),
		KeyPath:   filepath.Join(dir, "api", "tls.key"),
		JoinMaterial: func(context.Context) (JoinMaterial, error) {
			return JoinMaterial{
				Token:          "abcdef.0123456789abcdef",
				CertificateKey: "0123456789abcdef",
				CACertHash:     "deadbeef",
			}, nil
		},
	}
	conn := serveWithClient(t, opts, ca, caPEM, caKey)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := apiv1.NewVatesAPIClient(conn).GetJoinMaterial(ctx, &apiv1.GetJoinMaterialRequest{})
	if err != nil {
		t.Fatalf("GetJoinMaterial: %v", err)
	}
	if resp.GetToken() != "abcdef.0123456789abcdef" {
		t.Errorf("token = %q", resp.GetToken())
	}
	if resp.GetCertificateKey() != "0123456789abcdef" {
		t.Errorf("certificate key = %q", resp.GetCertificateKey())
	}
	if resp.GetCaCertHash() != "deadbeef" {
		t.Errorf("CA hash = %q", resp.GetCaCertHash())
	}
}

// A node that cannot issue join material must say so, not return an empty token
// that a provider would hand to a machine and watch fail to join.
func TestGetJoinMaterialWithoutAProviderFails(t *testing.T) {
	dir := t.TempDir()
	ca, caCert, caKey := writeTestCA(t, dir)
	caPEM, err := os.ReadFile(caCert)
	if err != nil {
		t.Fatal(err)
	}
	conn := serveWithClient(t, Options{
		CAPath:    caCert,
		CAKeyPath: caKey,
		CertPath:  filepath.Join(dir, "api", "tls.crt"),
		KeyPath:   filepath.Join(dir, "api", "tls.key"),
	}, ca, caPEM, caKey)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = apiv1.NewVatesAPIClient(conn).GetJoinMaterial(ctx, &apiv1.GetJoinMaterialRequest{})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("GetJoinMaterial error = %v, want FailedPrecondition", err)
	}
}

// serveWithClient starts a server and dials it as a system:masters client signed
// by the server's own CA.
func serveWithClient(t *testing.T, opts Options, ca *x509.Certificate, caPEM []byte, caKey string) *grpc.ClientConn {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	go func() { _ = ServeListener(lis, opts) }()

	certPEM, keyPEM := signClient(t, ca, caKey, []string{"system:masters"})
	kc := writeKubeconfig(t, filepath.Join(t.TempDir(), "good"), caPEM, certPEM, keyPEM)
	conn, err := Dial(lis.Addr().String(), kc)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	return conn
}
