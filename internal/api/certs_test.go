package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestCA creates a self-signed CA on disk, the way kubeadm's cluster CA
// looks, and returns its certificate and the two file paths.
func writeTestCA(t *testing.T, dir string) (*x509.Certificate, string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating the CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kubernetes"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating the CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the CA certificate: %v", err)
	}

	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return cert, certPath, keyPath
}

func TestEnsureServerCertIsSignedByTheClusterCA(t *testing.T) {
	dir := t.TempDir()
	ca, caCert, caKey := writeTestCA(t, dir)
	certPath := filepath.Join(dir, "api", "tls.crt")
	keyPath := filepath.Join(dir, "api", "tls.key")

	hosts := []string{"localhost", "10.0.0.7", "vates-cp-1"}
	if err := EnsureServerCert(caCert, caKey, certPath, keyPath, hosts); err != nil {
		t.Fatalf("EnsureServerCert: %v", err)
	}

	raw, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("reading the minted certificate: %v", err)
	}
	block, _ := pem.Decode(raw)
	srv, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parsing the minted certificate: %v", err)
	}

	if err := srv.CheckSignatureFrom(ca); err != nil {
		t.Errorf("the server certificate is not signed by the cluster CA: %v", err)
	}
	var sawIP, sawDNS bool
	for _, ip := range srv.IPAddresses {
		if ip.Equal(net.ParseIP("10.0.0.7")) {
			sawIP = true
		}
	}
	for _, d := range srv.DNSNames {
		if d == "localhost" || d == "vates-cp-1" {
			sawDNS = true
		}
	}
	if !sawIP {
		t.Errorf("the certificate has no SAN for 10.0.0.7: %v", srv.IPAddresses)
	}
	if !sawDNS {
		t.Errorf("the certificate has no SAN for the node name: %v", srv.DNSNames)
	}
}

func TestEnsureServerCertKeepsAnExistingCertificate(t *testing.T) {
	dir := t.TempDir()
	_, caCert, caKey := writeTestCA(t, dir)
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	if err := EnsureServerCert(caCert, caKey, certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(certPath)
	if err != nil {
		t.Fatal(err)
	}

	// A second call must not regenerate: the certificate is kept.
	if err := EnsureServerCert(caCert, caKey, certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Error("EnsureServerCert rewrote an existing certificate")
	}
}

// The virtual IP is not on the node when the certificate is minted, so a node
// that already has a certificate must pick the new name up when it appears --
// otherwise the VIP never enters the SAN and the operator cannot reach the
// cluster by the one address they know.
func TestEnsureServerCertRegeneratesWhenAHostIsMissing(t *testing.T) {
	dir := t.TempDir()
	_, caCert, caKey := writeTestCA(t, dir)
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	if err := EnsureServerCert(caCert, caKey, certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureServerCert(caCert, caKey, certPath, keyPath, []string{"localhost", "192.168.122.99"}); err != nil {
		t.Fatal(err)
	}

	if c := readCert(t, certPath); !certHasIP(c, net.ParseIP("192.168.122.99")) {
		t.Errorf("the certificate was kept without the new host: %v", c.IPAddresses)
	}
}

// A certificate signed by the wrong authority is useless, key or no key.
func TestEnsureServerCertRegeneratesUnderANewCA(t *testing.T) {
	dir := t.TempDir()
	_, caCert1, caKey1 := writeTestCA(t, t.TempDir())
	ca2, caCert2, caKey2 := writeTestCA(t, t.TempDir())
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	if err := EnsureServerCert(caCert1, caKey1, certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureServerCert(caCert2, caKey2, certPath, keyPath, []string{"localhost"}); err != nil {
		t.Fatal(err)
	}

	if err := readCert(t, certPath).CheckSignatureFrom(ca2); err != nil {
		t.Errorf("the certificate was not re-signed by the new CA: %v", err)
	}
}

func TestExtraHostsReadsOneNamePerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-hosts")
	if err := os.WriteFile(path, []byte("192.168.122.99\n\n  extra.example  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extraHosts(path)
	want := []string{"192.168.122.99", "extra.example"}
	if len(got) != len(want) {
		t.Fatalf("extraHosts() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extraHosts() = %q, want %q", got, want)
		}
	}
}

func TestExtraHostsMissingFileIsNotAnError(t *testing.T) {
	if got := extraHosts(filepath.Join(t.TempDir(), "absent")); got != nil {
		t.Errorf("extraHosts() = %q, want nothing", got)
	}
}

func readCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("%s holds no PEM certificate", path)
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
