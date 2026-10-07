package api

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/vatesfr/vates-kube-os/internal/firstboot"
)

// Run starts the node's management API. It is a face of the one binary, started
// by vates-api.service.
//
//	vates api [--listen :50000] [--ca <ca.crt>] [--ca-key <ca.key>] [--cert <crt>] [--key <key>]
func Run(args []string) error {
	opts, err := options(args)
	if err != nil {
		return err
	}
	return Serve(opts)
}

// options parses the face's flags into Options.
//
// The CA and certificate paths default to EMPTY on purpose: Serve decides
// between the operator CA (/etc/vates/api-ca.{crt,key}) and the cluster CA. Filling
// them in here would make that decision unreachable -- which it did, until an
// end-to-end test caught a node signing with the wrong authority.
func options(args []string) (Options, error) {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	listen := fs.String("listen", "", fmt.Sprintf("host:port to serve on (default :%d, or $VATES_API_PORT)", DefaultPort))
	ca := fs.String("ca", "", "CA that signs the server certificate and verifies clients (default: operator CA if injected, else the cluster CA)")
	caKey := fs.String("ca-key", "", "that CA's private key (default: same rule)")
	cert := fs.String("cert", "", "server certificate (default "+DefaultCertPath+")")
	key := fs.String("key", "", "server key (default "+DefaultKeyPath+")")
	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	// api.port in vates-node.yaml reaches this process as $VATES_API_PORT, set by
	// the drop-in vates-init writes. The flag wins when it is given.
	addr := *listen
	if addr == "" {
		if p := os.Getenv("VATES_API_PORT"); p != "" {
			addr = ":" + p
		} else {
			addr = fmt.Sprintf(":%d", DefaultPort)
		}
	}
	return Options{
		Listen:       addr,
		CAPath:       *ca,
		CAKeyPath:    *caKey,
		CertPath:     *cert,
		KeyPath:      *key,
		JoinMaterial: joinMaterial,
	}, nil
}

// joinMaterial produces join credentials on demand.
//
// It reads the kubelet drop-in for the Kubernetes version and mirror, because
// this process's environment does not carry them, and runs kubeadm in the same
// container the bootstrap phases used. A worker never reaches this: its API does
// not start (the unit requires the cluster CA key).
func joinMaterial(context.Context) (JoinMaterial, error) {
	version, base, err := firstboot.KubeletEnvironment(firstboot.KubeletDropIn)
	if err != nil {
		return JoinMaterial{}, err
	}
	c, err := firstboot.CollectJoinCredentials(firstboot.DefaultPaths(), version, base, firstboot.OSRunner{})
	if err != nil {
		return JoinMaterial{}, err
	}
	return JoinMaterial{
		Token:          c.Token,
		CertificateKey: c.CertificateKey,
		CACertHash:     c.CACertHash,
	}, nil
}
