package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/firstboot"
)

// firstboot.ExtraHostsPath lists, one host per line, the names the server
// certificate must also be valid for. It is written by firstboot, which owns the
// path along with the file it writes.

// EnsureServerCert keeps or mints the API's server certificate, signed by the
// given CA.
//
// It needs the CA's PRIVATE key, so it works on a control plane (which kubeadm
// gave ca.key); a worker holds only ca.crt and receives its certificate from the
// provider instead, further down the line.
//
// An existing certificate is KEPT when it still covers everything it must --
// same CA, unexpired, every host in the SAN. Regenerating blindly would change
// the certificate under the operator's feet; keeping it blindly would leave it
// naming addresses the node no longer has, or does not have YET. That second
// case is how the virtual IP came to be missing from the SAN.
func EnsureServerCert(caCertPath, caKeyPath, certPath, keyPath string, hosts []string) error {
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return fmt.Errorf("api: reading the cluster CA: %w", err)
	}
	caCert, err := parseCertificate(caCertPEM)
	if err != nil {
		return err
	}

	if fileExists(keyPath) && certCovers(certPath, caCert, hosts) {
		return nil
	}

	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return fmt.Errorf("api: reading the cluster CA key (only a control plane has one): %w", err)
	}
	caKey, err := parsePrivateKey(caKeyPEM)
	if err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("api: generating the server key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("api: serial number: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "vates-api"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("api: signing the server certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("api: encoding the server key: %w", err)
	}

	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return err
	}
	return writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600)
}

// serverHosts is what the certificate must be valid for: the node's name, every
// local address, loopback, and the names vates-init recorded -- the operator may
// reach it by any of them.
func serverHosts() []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if name, err := os.Hostname(); err == nil {
		hosts = append(hosts, name)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				hosts = append(hosts, ipnet.IP.String())
			}
		}
	}
	return append(hosts, extraHosts(firstboot.ExtraHostsPath)...)
}

// extraHosts reads the additional names vates-init wrote from vates-node.yaml.
// A missing file is the normal cluster-CAPI case, not an error.
func extraHosts(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var hosts []string
	for line := range strings.SplitSeq(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			hosts = append(hosts, line)
		}
	}
	return hosts
}

// certCovers reports whether the certificate on disk is still the right one: it
// parses, is signed by ca, has not expired, and is valid for every host.
func certCovers(certPath string, ca *x509.Certificate, hosts []string) bool {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	cert, err := parseCertificate(raw)
	if err != nil {
		return false
	}
	if err := cert.CheckSignatureFrom(ca); err != nil {
		return false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			if !certHasIP(cert, ip) {
				return false
			}
		} else if h != "" && !certHasDNS(cert, h) {
			return false
		}
	}
	return true
}

func certHasIP(cert *x509.Certificate, ip net.IP) bool {
	for _, have := range cert.IPAddresses {
		if have.Equal(ip) {
			return true
		}
	}
	return false
}

func certHasDNS(cert *x509.Certificate, name string) bool {
	for _, have := range cert.DNSNames {
		if strings.EqualFold(have, name) {
			return true
		}
	}
	return false
}

func writePEM(path, blockType string, der []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("api: %w", err)
	}
	buf := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	if err := os.WriteFile(path, buf, mode); err != nil {
		return fmt.Errorf("api: writing %s: %w", path, err)
	}
	return nil
}

func parseCertificate(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("api: no PEM certificate found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("api: parsing the CA certificate: %w", err)
	}
	return cert, nil
}

// parsePrivateKey accepts the three spellings a CA key may have been written in:
// PKCS#8, PKCS#1 (RSA) and SEC 1 (EC). kubeadm uses PKCS#1 for its RSA CA.
func parsePrivateKey(pemBytes []byte) (any, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("api: no PEM private key found")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, fmt.Errorf("api: unknown private key format")
}

// Compile-time reminder that the CA key is one of the types above.
var _ = []any{(*rsa.PrivateKey)(nil), (*ecdsa.PrivateKey)(nil)}
