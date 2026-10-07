package kubeapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// credentials generates a self-signed certificate and key, so that the client
// under test builds a real tls.Certificate rather than being handed a stub.
func credentials(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "system:node:test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling the key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestNewFromKubeconfigWithEmbeddedCredentials(t *testing.T) {
	// This is the shape the provider writes for a joining control plane: the
	// certificate material is base64 inside the file.
	cert, key := credentials(t)
	dir := t.TempDir()
	path := write(t, dir, "kubelet.conf", fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: vates
  cluster:
    server: https://192.0.2.1:6443
    certificate-authority-data: %s
users:
- name: kubelet
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: vates
  context:
    cluster: vates
    user: kubelet
current-context: vates
`,
		base64.StdEncoding.EncodeToString(cert),
		base64.StdEncoding.EncodeToString(cert),
		base64.StdEncoding.EncodeToString(key)))

	c, err := NewFromKubeconfig(path)
	if err != nil {
		t.Fatalf("NewFromKubeconfig: %v", err)
	}
	if c.Server != "https://192.0.2.1:6443" {
		t.Errorf("server = %q, want the one in the file", c.Server)
	}
}

func TestNewFromKubeconfigWithCredentialFiles(t *testing.T) {
	// And this is the shape vates-init installs for a worker: paths, not data.
	// A reader that understood only the embedded form worked on control planes
	// and failed on every worker.
	cert, key := credentials(t)
	dir := t.TempDir()
	certFile := write(t, dir, "kubelet.crt", string(cert))
	keyFile := write(t, dir, "kubelet.key", string(key))
	caFile := write(t, dir, "ca.crt", string(cert))
	path := write(t, dir, "kubelet.conf", fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: vates
  cluster:
    server: https://192.0.2.2:6443
    certificate-authority: %s
users:
- name: kubelet
  user:
    client-certificate: %s
    client-key: %s
contexts:
- name: vates
  context:
    cluster: vates
    user: kubelet
current-context: vates
`, caFile, certFile, keyFile))

	c, err := NewFromKubeconfig(path)
	if err != nil {
		t.Fatalf("NewFromKubeconfig: %v", err)
	}
	if c.Server != "https://192.0.2.2:6443" {
		t.Errorf("server = %q, want the one in the file", c.Server)
	}
}

func TestNewFromKubeconfigWithoutCredentialsFails(t *testing.T) {
	// A file naming a server but carrying no identity would produce a client
	// that is refused with a 401, which reads like a permissions problem rather
	// than like a kubeconfig that was read wrongly.
	dir := t.TempDir()
	path := write(t, dir, "kubelet.conf", `apiVersion: v1
kind: Config
clusters:
- name: vates
  cluster:
    server: https://192.0.2.3:6443
users:
- name: kubelet
  user: {}
`)
	if _, err := NewFromKubeconfig(path); err == nil {
		t.Fatal("a kubeconfig with no client certificate was accepted")
	}
}

func TestMaterialPrefersDataAndFallsBackToTheFile(t *testing.T) {
	cert, _ := credentials(t)
	dir := t.TempDir()
	file := write(t, dir, "ca.crt", "FROM-FILE")

	if got := material(base64.StdEncoding.EncodeToString(cert), file); string(got) != string(cert) {
		t.Error("material did not prefer the embedded data when both are present")
	}
	if got := material("", file); string(got) != "FROM-FILE" {
		t.Errorf("material from a file = %q, want FROM-FILE", got)
	}
	if got := material("", filepath.Join(dir, "absent")); got != nil {
		t.Errorf("material on a missing file = %q, want nil", got)
	}
}

func TestMustBase64PassesRawPEMThrough(t *testing.T) {
	// A hand-written kubeconfig can put raw PEM in a *-data field. It is not
	// valid base64, so it must survive rather than become emptiness.
	raw := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	if got := string(mustBase64(raw)); got != raw {
		t.Errorf("mustBase64 mangled raw PEM: %q", got)
	}
}

func TestPickClusterAndUserFallBackToTheOnlyEntry(t *testing.T) {
	// A node's kubeconfig has one cluster and one user, and may name neither in
	// a context. Refusing it would be refusing the file this system writes.
	clusters := []namedCluster{{Name: "vates"}}
	users := []namedUser{{Name: "kubelet"}}

	if _, ok := pickCluster(clusters, "absent"); !ok {
		t.Error("pickCluster did not fall back to the only cluster")
	}
	if _, ok := pickUser(users, "absent"); !ok {
		t.Error("pickUser did not fall back to the only user")
	}
	if _, ok := pickCluster([]namedCluster{{Name: "a"}, {Name: "b"}}, "absent"); ok {
		t.Error("pickCluster chose among several when none was named")
	}
}
