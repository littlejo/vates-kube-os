package firstboot

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// JoinCredentials is what a machine that joins an existing cluster needs from
// the cluster: the bootstrap token, the certificate key a joining control plane
// fetches the shared certificates with, and the CA hash it verifies the cluster
// with.
//
// They are produced on demand rather than stored, because they expire -- the
// token by its TTL, the certificate key by kubeadm's two-hour rule -- so a value
// written down during bootstrap is worthless to a node created later.
type JoinCredentials struct {
	Token          string
	CertificateKey string
	CACertHash     string
}

// Polling for the cluster-info signature. Variables, not constants, so a test
// can run the loop without waiting.
var (
	tokenSignAttempts = 30
	tokenSignInterval = 2 * time.Second
)

// KubeletEnvironment reads the Kubernetes version and binary mirror vates-init
// recorded for this node's kubelet.
//
// A joining-machine's helper needs them to fetch kubeadm through the launcher,
// and it cannot read the config drive the way vates-init did -- the drive may be
// gone, and this runs on demand, long after boot. The drop-in is the record.
func KubeletEnvironment(path string) (version, binaryBase string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "Environment=KUBERNETES_VERSION="); ok {
			version = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "Environment=KUBERNETES_BINARY_BASE="); ok {
			binaryBase = strings.TrimSpace(v)
		}
	}
	if version == "" {
		return "", "", fmt.Errorf("%s names no KUBERNETES_VERSION", path)
	}
	return version, binaryBase, nil
}

// CollectJoinCredentials obtains fresh join credentials from a bootstrapped
// control plane. It runs kubeadm and kubectl through the launcher, because
// neither binary is in the image -- the launcher fetches the version this node
// runs -- and reads the cluster's CA from the PKI kubeadm wrote.
//
// It runs in the management API, a different process from vates-init, so it puts
// the version and the mirror into its own environment first: that is what the
// launcher reads.
func CollectJoinCredentials(paths Paths, version, binaryBase string, r Runner) (JoinCredentials, error) {
	setBinarySource(version, binaryBase)
	caPath := filepath.Join(paths.Kubernetes, "pki", "ca.crt")
	caHash, err := caCertHash(caPath)
	if err != nil {
		return JoinCredentials{}, err
	}
	// The certificate key is DERIVED from the CA, so every upload uses the same
	// one. kubeadm re-encrypts the uploaded certificates with the key it is
	// given, so a fresh key per call would invalidate the keys already handed to
	// machines about to join -- which is exactly what a joining control plane
	// decodes them with.
	certKey, err := joinCertificateKey(caPath)
	if err != nil {
		return JoinCredentials{}, err
	}
	token, err := createBootstrapToken(r)
	if err != nil {
		return JoinCredentials{}, err
	}
	if _, err := uploadCertificateKey(certKey, r); err != nil {
		return JoinCredentials{}, err
	}
	return JoinCredentials{Token: token, CertificateKey: certKey, CACertHash: caHash}, nil
}

// createBootstrapToken creates a token and returns only once the cluster-info
// ConfigMap carries its signature.
//
// The wait is not optional. A token exists the moment it is created, but a
// joining node validates the cluster against a JWS signature the
// controller-manager publishes afterwards; handing the token out early makes the
// node's discovery retry forever with a message that names a token ID and never
// the fact that it was too early.
func createBootstrapToken(r Runner) (string, error) {
	created, err := run(r, kubeadmArgs("token", "create", "--ttl", "24h"))
	if err != nil {
		return "", fmt.Errorf("creating a bootstrap token: %w", err)
	}
	token := lastLine(created)
	id, _, ok := strings.Cut(token, ".")
	if !ok || id == "" {
		return "", fmt.Errorf("kubeadm did not return a bootstrap token (got %q)", token)
	}

	base := kubectlJoinArgs()
	for i := 0; i < tokenSignAttempts; i++ {
		data, err := run(r, append(base, "-n", "kube-public", "get", "configmap", "cluster-info", "-o", "jsonpath={.data}"))
		if err == nil && strings.Contains(string(data), "jws-kubeconfig-"+id) {
			return token, nil
		}
		time.Sleep(tokenSignInterval)
	}
	return "", fmt.Errorf("the cluster-info ConfigMap never published a signature for token %s: "+
		"the controller-manager's cluster-info signer may not be running", id)
}

// uploadCertificateKey re-uploads the cluster certificates with the given key.
//
// The key is passed rather than generated: kubeadm's upload-certs re-encrypts
// the uploaded certificates with the key it is given, so re-running with a new
// key each time would invalidate every key already handed out. See
// joinCertificateKey -- the key is stable, so the upload is idempotent.
func uploadCertificateKey(key string, r Runner) (string, error) {
	if _, err := run(r, kubeadmArgs("init", "phase", "upload-certs", "--upload-certs", "--certificate-key", key)); err != nil {
		return "", fmt.Errorf("uploading the cluster certificates: %w", err)
	}
	return key, nil
}

// joinCertificateKey is the certificate key every upload uses, derived from the
// cluster CA so it is stable for the cluster's whole life.
//
// It has to be stable: any control plane can answer GetJoinMaterial, and each
// answer re-uploads the certificates; a key that changed per call would make the
// ones already given to machines about to join undecryptable ("cipher: message
// authentication failed" at download-certs). Deriving it from the CA ties it to
// the cluster, not to a call, so every machine gets the same key and the secret
// always decrypts with it.
func joinCertificateKey(caCertPath string) (string, error) {
	raw, err := os.ReadFile(caCertPath)
	if err != nil {
		return "", fmt.Errorf("reading the cluster CA: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// caCertHash is kubeadm's --discovery-token-ca-cert-hash: the sha256 of the CA
// certificate's public key, DER-encoded.
func caCertHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading the cluster CA: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fmt.Errorf("%s holds no PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

// setBinarySource puts the Kubernetes version and the binary mirror into this
// process's environment, where the launcher reads them.
//
// vates-init does the same at configure time (exposeBinarySource); the
// management API is a different process and cannot rely on that, so it sets them
// from the drop-in when it answers a join-material request.
func setBinarySource(version, binaryBase string) {
	_ = os.Setenv("KUBERNETES_VERSION", version)
	_ = os.Setenv("KUBERNETES_BINARY_BASE", binaryBase)
}

// kubeadmArgs runs the node's kubeadm, through the launcher.
func kubeadmArgs(args ...string) []string {
	return append([]string{KubeadmPath}, args...)
}

// kubectlJoinArgs runs kubectl against the cluster's super-admin kubeconfig --
// the credential that can create bootstrap tokens and read the cluster-info
// ConfigMap while the cluster is bootstrapping.
func kubectlJoinArgs(args ...string) []string {
	return append([]string{KubectlPath, "--kubeconfig", SuperAdminKubeconfig}, args...)
}

// run executes a whole argv built above.
func run(r Runner, argv []string) ([]byte, error) {
	return r.Run(argv[0], argv[1:]...)
}

func lastLine(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
