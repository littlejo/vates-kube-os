package firstboot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKubeletEnvironmentReadsVersionAndBase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "10-node.conf")
	doc := "[Service]\n" +
		"Environment=NODE_NAME=vates-cp-1\n" +
		"Environment=KUBERNETES_VERSION=v1.31.0\n" +
		"Environment=KUBERNETES_BINARY_BASE=https://mirror.example/k8s\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	version, base, err := KubeletEnvironment(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v1.31.0" {
		t.Errorf("version = %q, want v1.31.0", version)
	}
	if base != "https://mirror.example/k8s" {
		t.Errorf("base = %q, want the mirror", base)
	}
}

func TestKubeletEnvironmentRefusesWithoutAVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "10-node.conf")
	if err := os.WriteFile(path, []byte("Environment=NODE_NAME=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := KubeletEnvironment(path); err == nil {
		t.Fatal("KubeletEnvironment() accepted a drop-in with no version")
	}
}

// joinFake answers the two kubeadm calls and the configmap read the way a
// bootstrapped control plane does, and can withhold the signature once to check
// that the wait actually waits.
type joinFake struct {
	commands      []string
	unsignedOnce  bool
	unsignedCalls int
}

func (f *joinFake) MkdirAll(string, os.FileMode) error { return nil }
func (f *joinFake) WriteFile(string, os.FileMode, []byte) error {
	return nil
}
func (f *joinFake) Stat(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
func (f *joinFake) Logf(string, ...any)              {}
func (f *joinFake) Progress(string)                  {}

func (f *joinFake) Run(name string, args ...string) ([]byte, error) {
	joined := strings.Join(append([]string{name}, args...), " ")
	f.commands = append(f.commands, joined)

	switch {
	case strings.Contains(joined, "token create"):
		return []byte("abcdef.0123456789abcdef\n"), nil
	case strings.Contains(joined, "get configmap cluster-info"):
		if f.unsignedOnce && f.unsignedCalls == 0 {
			f.unsignedCalls++
			return []byte("{}\n"), nil
		}
		return []byte("jws-kubeconfig-abcdef=...\n"), nil
	case strings.Contains(joined, "upload-certs"):
		return []byte(strings.Repeat("a", 64) + "\n"), nil
	}
	return nil, nil
}

func TestCollectJoinCredentials(t *testing.T) {
	tests := []struct {
		name  string
		calls int
	}{
		{
			// The nominal path: a token, the CA hash and a certificate key
			// derived from the CA.
			name:  "returns the token, the CA hash and a CA-derived certificate key",
			calls: 1,
		},
		{
			// Regression: kubeadm's upload-certs re-encrypts the uploaded
			// certificates with the key it is given, so every call has to hand
			// it the SAME key -- a fresh key would invalidate the one a joining
			// control plane is about to decode the certificates with.
			name:  "hands every call the same certificate key",
			calls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := testPaths(t)
			caPath := filepath.Join(paths.Kubernetes, "pki", "ca.crt")
			caHash := writeCACert(t, caPath)
			wantKey := sha256Hex(t, caPath)

			oldAttempts, oldInterval := tokenSignAttempts, tokenSignInterval
			tokenSignAttempts, tokenSignInterval = 2, 0
			defer func() { tokenSignAttempts, tokenSignInterval = oldAttempts, oldInterval }()

			var first string
			for i := 0; i < tt.calls; i++ {
				r := &joinFake{}
				c, err := CollectJoinCredentials(paths, "v1.31.0", "", r)
				if err != nil {
					t.Fatalf("CollectJoinCredentials() call %d failed: %v", i+1, err)
				}
				if c.Token != "abcdef.0123456789abcdef" {
					t.Errorf("token = %q, want abcdef.0123456789abcdef", c.Token)
				}
				if c.CACertHash != caHash {
					t.Errorf("CA hash = %q, want %q", c.CACertHash, caHash)
				}
				if c.CertificateKey != wantKey {
					t.Errorf("certificate key = %q, want the CA digest %q", c.CertificateKey, wantKey)
				}
				if i == 0 {
					first = c.CertificateKey
				} else if c.CertificateKey != first {
					t.Errorf("certificate key changed between calls: %q then %q", first, c.CertificateKey)
				}
				// The key is meaningless unless kubeadm encrypts the uploaded
				// certificates with it.
				if !anyCommandHas(r.commands, "upload-certs", "--certificate-key", c.CertificateKey) {
					t.Errorf("no upload-certs command carried --certificate-key %q (commands: %q)", c.CertificateKey, r.commands)
				}
			}
		})
	}
}

// sha256Hex is the digest of a file, the way joinCertificateKey derives the
// certificate key from the CA.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// anyCommandHas reports whether one recorded command contains all the given
// fragments -- enough to pin the argv a helper actually ran.
func anyCommandHas(commands []string, fragments ...string) bool {
	for _, cmd := range commands {
		ok := true
		for _, fragment := range fragments {
			if !strings.Contains(cmd, fragment) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// A token is useless until the cluster-info signature is published; a helper
// that returned the first token it saw would hand a node a token it retries on
// forever.
func TestCollectJoinCredentialsWaitsForTheSignature(t *testing.T) {
	paths := testPaths(t)
	writeCACert(t, filepath.Join(paths.Kubernetes, "pki", "ca.crt"))

	oldAttempts, oldInterval := tokenSignAttempts, tokenSignInterval
	tokenSignAttempts, tokenSignInterval = 3, 0
	defer func() { tokenSignAttempts, tokenSignInterval = oldAttempts, oldInterval }()

	r := &joinFake{unsignedOnce: true}
	if _, err := CollectJoinCredentials(paths, "v1.31.0", "", r); err != nil {
		t.Fatalf("CollectJoinCredentials() failed while the signature was pending: %v", err)
	}
	if r.unsignedCalls != 1 {
		t.Errorf("the helper did not retry the unsigned configmap")
	}
}

func TestCollectJoinCredentialsFailsWithNoSignature(t *testing.T) {
	paths := testPaths(t)
	writeCACert(t, filepath.Join(paths.Kubernetes, "pki", "ca.crt"))

	oldAttempts, oldInterval := tokenSignAttempts, tokenSignInterval
	tokenSignAttempts, tokenSignInterval = 1, 0
	defer func() { tokenSignAttempts, tokenSignInterval = oldAttempts, oldInterval }()

	r := &joinFake{}
	// Never publish the signature.
	r2 := &alwaysUnsigned{joinFake: r}
	if _, err := CollectJoinCredentials(paths, "v1.31.0", "", r2); err == nil {
		t.Fatal("CollectJoinCredentials() succeeded with no signature")
	}
}

type alwaysUnsigned struct{ *joinFake }

func (a *alwaysUnsigned) Run(name string, args ...string) ([]byte, error) {
	if strings.Contains(strings.Join(append([]string{name}, args...), " "), "get configmap cluster-info") {
		return []byte("{}\n"), nil
	}
	return a.joinFake.Run(name, args...)
}

// writeCACert writes a small self-signed CA and returns kubeadm's
// --discovery-token-ca-cert-hash for it.
func writeCACert(t *testing.T, path string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:])
}
