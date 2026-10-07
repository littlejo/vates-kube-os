package api

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
	"time"
)

// GenerateClientPKI creates the material an operator needs to call a node's
// management API without CAPI and without SSH: a small certificate authority of
// its own, one client certificate, and a kubeconfig ready for vateskctl.
//
// It returns the kubeconfig path. The CA certificate (api-ca.crt) AND its key
// (api-ca.key) are what the caller injects into the node's config drive, so the
// node mints its server certificate from this authority; the operator keeps the
// same CA to verify that certificate and to issue client certificates -- for
// rotation, or for CI -- without touching the node again.
//
// The cluster's own CA is not involved: the node generated it (kubeadm) and
// never hands it out. This is a second, operator-owned authority the node
// adopts for its management API.
func GenerateClientPKI(dir, name string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if name == "" {
		name = "vateskctl"
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          mustSerial(),
		Subject:               pkix.Name{CommonName: "vates-api-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return "", fmt.Errorf("api: creating the client CA: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return "", err
	}

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	// O=vates:admin is the group the server's authorizer accepts; it is what
	// makes this a management client rather than an ordinary node.
	clientTmpl := &x509.Certificate{
		SerialNumber: mustSerial(),
		Subject:      pkix.Name{CommonName: name, Organization: []string{"vates:admin"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientDER, err := x509.CreateCertificate(rand.Reader, clientTmpl, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return "", fmt.Errorf("api: signing the client certificate: %w", err)
	}

	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return "", err
	}
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return "", err
	}

	write := func(file, block string, der []byte, mode os.FileMode) error {
		return writePEM(filepath.Join(dir, file), block, der, mode)
	}
	if err := write("api-ca.crt", "CERTIFICATE", caDER, 0o644); err != nil {
		return "", err
	}
	if err := write("api-ca.key", "EC PRIVATE KEY", caKeyDER, 0o600); err != nil {
		return "", err
	}
	if err := write("client.crt", "CERTIFICATE", clientDER, 0o644); err != nil {
		return "", err
	}
	if err := write("client.key", "EC PRIVATE KEY", clientKeyDER, 0o600); err != nil {
		return "", err
	}

	// A kubeconfig so vateskctl needs nothing else. Its server is a placeholder:
	// --node gives the real address, and the certificate is verified against it.
	b64 := base64.StdEncoding.EncodeToString
	kc := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: vates
  cluster:
    certificate-authority-data: %s
    server: https://node:50000
users:
- name: vateskctl
  user:
    client-certificate-data: %s
    client-key-data: %s
contexts:
- name: vates
  context: {cluster: vates, user: vateskctl}
current-context: vates
`, b64(pemBytes(caDER, "CERTIFICATE")), b64(pemBytes(clientDER, "CERTIFICATE")), b64(pemBytes(clientKeyDER, "EC PRIVATE KEY")))
	kcPath := filepath.Join(dir, "client.kubeconfig")
	if err := os.WriteFile(kcPath, []byte(kc), 0o600); err != nil {
		return "", err
	}
	return kcPath, nil
}

func mustSerial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}
	return n
}

func pemBytes(der []byte, block string) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: block, Bytes: der})
}
