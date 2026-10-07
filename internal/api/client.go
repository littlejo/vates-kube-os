package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
)

// Dial connects to the management API at addr using the credentials in the
// kubeconfig at path. That file already holds the cluster CA and a client
// certificate signed by it, so vateskctl needs nothing else -- no token, no
// password, no SSH.
func Dial(addr, kubeconfigPath string) (*grpc.ClientConn, error) {
	tlsConf, err := TLSFromKubeconfig(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(tlsConf)))
}

// TLSFromKubeconfig builds a client TLS configuration from a kubeconfig. Both
// the embedded (-data) and the file-referencing spellings are accepted: the
// operator's ~/.kube/config uses files, the one clusterctl hands out uses data.
func TLSFromKubeconfig(path string) (*tls.Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var kc struct {
		Clusters []struct {
			Cluster struct {
				CAData string `yaml:"certificate-authority-data"`
				CA     string `yaml:"certificate-authority"`
			} `yaml:"cluster"`
		} `yaml:"clusters"`
		Users []struct {
			User struct {
				CertData string `yaml:"client-certificate-data"`
				Cert     string `yaml:"client-certificate"`
				KeyData  string `yaml:"client-key-data"`
				Key      string `yaml:"client-key"`
			} `yaml:"user"`
		} `yaml:"users"`
	}
	if err := yaml.Unmarshal(raw, &kc); err != nil {
		return nil, fmt.Errorf("api: parsing %s: %w", path, err)
	}
	if len(kc.Clusters) == 0 || len(kc.Users) == 0 {
		return nil, fmt.Errorf("api: %s names no cluster or no user", path)
	}
	base := filepath.Dir(path)

	caPEM, err := material(kc.Clusters[0].Cluster.CAData, kc.Clusters[0].Cluster.CA, base)
	if err != nil {
		return nil, fmt.Errorf("api: cluster CA: %w", err)
	}
	certPEM, err := material(kc.Users[0].User.CertData, kc.Users[0].User.Cert, base)
	if err != nil {
		return nil, fmt.Errorf("api: client certificate: %w", err)
	}
	keyPEM, err := material(kc.Users[0].User.KeyData, kc.Users[0].User.Key, base)
	if err != nil {
		return nil, fmt.Errorf("api: client key: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("api: client key pair: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("api: %s holds no CA certificate", path)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// material prefers the embedded data, then a path relative to the kubeconfig.
func material(data, ref, base string) ([]byte, error) {
	if data != "" {
		return base64.StdEncoding.DecodeString(data)
	}
	if ref == "" {
		return nil, fmt.Errorf("neither data nor a path is set")
	}
	if !filepath.IsAbs(ref) {
		ref = filepath.Join(base, ref)
	}
	return os.ReadFile(ref)
}
