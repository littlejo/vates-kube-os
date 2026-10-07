package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/vatesfr/vates-kube-os/internal/api"
	apiv1 "github.com/vatesfr/vates-kube-os/proto/vates/api/v1"
)

const (
	// apiPortName is both the CLI flag and the file the port is recorded in:
	// they name the same thing, and keeping them one string is the point.
	apiPortName = "api-port"
	// flagNodeUsage is shared by the commands that reach a specific node.
	flagNodeUsage = "a specific node's management address, host:port (default: the cluster endpoint on the management port)"
	// flagCredUsage is shared by the commands that take an operator credential.
	flagCredUsage = "explicit operator credential file"
)

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "vateskctl: %v\n", err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "vateskctl",
		Short: "Talk to a Vates Kube OS node's management API",
		Long: `vateskctl talks to a running Vates Kube OS node over mutual TLS.

A node carries no shell and no sshd: everything a tool needs -- its status, the
cluster's kubeconfig -- is asked for on the management API. Authentication is a
certificate the operator generated before the node booted.

Per-cluster material lives under a vates data directory, in the XDG sense:

  $XDG_DATA_HOME/vates/kube-clusters/<cluster>/   (~/.local/share by default)

VATES manages more than Kubernetes, so a cluster is one kind of thing under that
directory, not the whole of it. Nothing in there is committed to a repository:
the CA key and the kubeconfig are secrets.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// cobra's Print* methods fall back to STDERR. A CLI whose whole job is to
	// print a status or a kubeconfig must print to stdout, or `vateskctl status >
	// file` writes an empty file and a harness capturing its output gets nothing.
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.AddCommand(newGenCmd(), newStatusCmd(), newKubeconfigCmd(), newJoinMaterialCmd(), newABCmd(), newLogsCmd())
	return root
}

func newGenCmd() *cobra.Command {
	var cluster, out, name, endpoint string
	var apiPort int
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate a client CA and certificate to call a node's API",
		Long: `Generate the credentials vateskctl uses to call a node.

A node generated its own cluster CA (kubeadm) and never hands it out, so there is
nothing to fetch your first credential from. This command creates a second,
operator-owned authority instead: inject its certificate into the node's config
drive (as api-ca.crt and api-ca.key), and the node trusts every certificate this
CA signs.

--endpoint records the cluster's address, the one an operator knows the cluster
by -- its virtual IP. status and kubeconfig then default to it instead of asking
for a node's own address every time.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := out
			if dir == "" {
				d, err := operatorDir(cluster)
				if err != nil {
					return err
				}
				dir = d
			}
			kc, err := api.GenerateClientPKI(dir, name)
			if err != nil {
				return err
			}
			if endpoint != "" {
				if err := os.WriteFile(filepath.Join(dir, "endpoint"), []byte(endpoint+"\n"), 0o644); err != nil {
					return err
				}
			}
			if err := os.WriteFile(filepath.Join(dir, apiPortName), []byte(strconv.Itoa(apiPort)+"\n"), 0o644); err != nil {
				return err
			}
			cmd.Printf("client CA    %s\n", filepath.Join(dir, "api-ca.crt"))
			cmd.Printf("client CA key %s\n", filepath.Join(dir, "api-ca.key"))
			cmd.Printf("kubeconfig   %s\n\n", kc)
			cmd.Println("next:")
			cmd.Printf("  1. inject %s and %s into the node's config drive\n",
				filepath.Join(dir, "api-ca.crt"), filepath.Join(dir, "api-ca.key"))
			cmd.Printf("     (as api-ca.crt and api-ca.key)\n")
			if endpoint != "" {
				cmd.Printf("  2. vateskctl kubeconfig --cluster %s\n", cluster)
			} else {
				cmd.Printf("  2. vateskctl kubeconfig --node <ip>:50000 --cluster %s\n", cluster)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cluster, "cluster", "", "cluster name; material goes under the vates data directory")
	cmd.Flags().StringVar(&out, "out", "", "explicit directory to write into (overrides --cluster)")
	cmd.Flags().StringVar(&name, "name", "vateskctl", "client certificate common name")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "the cluster's endpoint, host:port, to default status and kubeconfig to (e.g. the virtual IP)")
	cmd.Flags().IntVar(&apiPort, apiPortName, api.DefaultPort, "the node's management API port, recorded so status and kubeconfig default to it")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	return cmd
}

func newStatusCmd() *cobra.Command {
	var node, cluster, kubeconfig string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report a node's role, version and readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, err := clientKubeconfig(cluster, kubeconfig)
			if err != nil {
				return err
			}
			addr := node
			if addr == "" {
				if addr, err = nodeAddress(cluster); err != nil {
					return err
				}
			}
			conn, err := api.Dial(addr, kc)
			if err != nil {
				return err
			}
			// The command is over; a failing close of the connection has
			// nowhere useful to go.
			defer func() { _ = conn.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			resp, err := apiv1.NewVatesAPIClient(conn).GetStatus(ctx, &apiv1.GetStatusRequest{})
			if err != nil {
				return err
			}
			role := "worker"
			if resp.GetControlPlane() {
				role = "control plane"
			}
			cmd.Printf("node        %s\n", resp.GetNodeName())
			cmd.Printf("role        %s\n", role)
			cmd.Printf("kubernetes  %s\n", orDash(resp.GetKubernetesVersion()))
			cmd.Printf("endpoint    %s\n", orDash(resp.GetClusterEndpoint()))
			cmd.Printf("ready       %t\n", resp.GetReady())
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", flagNodeUsage)
	cmd.Flags().StringVar(&cluster, "cluster", "", "cluster name, to find the operator credential")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", flagCredUsage)
	_ = cmd.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	return cmd
}

// newLogsCmd fetches a node's own logs over the API. It exists because the node
// has no shell and no sshd: when bring-up fails, this is how the operator, or
// `make cluster`, finds out why.
func newLogsCmd() *cobra.Command {
	var node, cluster, kubeconfig string
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch a node's own logs (PID 1 and its services)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, err := clientKubeconfig(cluster, kubeconfig)
			if err != nil {
				return err
			}
			addr := node
			if addr == "" {
				if addr, err = nodeAddress(cluster); err != nil {
					return err
				}
			}
			conn, err := api.Dial(addr, kc)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := apiv1.NewVatesAPIClient(conn).GetLogs(ctx,
				&apiv1.GetLogsRequest{Lines: int32(lines)})
			if err != nil {
				return err
			}
			cmd.Print(resp.GetLogs())
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", flagNodeUsage)
	cmd.Flags().StringVar(&cluster, "cluster", "", "cluster name, to find the operator credential")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", flagCredUsage)
	cmd.Flags().IntVar(&lines, "lines", 200, "lines per log file")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	return cmd
}

// newABCmd drives the node's A/B state: which root it boots, and moving the next
// boot to the other one. It is the whole interface to the switchover -- the node
// has no shell -- and the write of the inactive slot is a separate step the
// caller does (the provider, or an operator with an image).
func newABCmd() *cobra.Command {
	var node, cluster, kubeconfig string

	cmd := &cobra.Command{
		Use:   "ab",
		Short: "Show or move which root the node boots (A/B)",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show the running slot and the boot entry it defaults to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			conn, err := abConn(node, cluster, kubeconfig)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			resp, err := apiv1.NewVatesAPIClient(conn).GetBootSlots(ctx, &apiv1.GetBootSlotsRequest{})
			if err != nil {
				return err
			}
			cmd.Printf("running  %s\n", orDash(resp.GetRunning()))
			cmd.Printf("default  %s\n", orDash(resp.GetDefaultEntry()))
			for _, s := range []string{"a", "b"} {
				cmd.Printf("  slot %s  %s\n", s, orDash(resp.GetEntries()[s]))
			}
			return nil
		},
	}

	switchCmd := &cobra.Command{
		Use:   "switch <a|b>",
		Short: "Boot the other root next, and roll back if it does not come up",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slot := args[0]
			if slot != "a" && slot != "b" {
				return fmt.Errorf("slot %q is not a or b", slot)
			}
			conn, err := abConn(node, cluster, kubeconfig)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := apiv1.NewVatesAPIClient(conn).SetBootSlot(ctx,
				&apiv1.SetBootSlotRequest{Slot: slot}); err != nil {
				return err
			}
			cmd.Printf("next boot: slot %s (a trial; blessed once the node is up, rolled back otherwise)\n", slot)
			return nil
		},
	}

	for _, c := range []*cobra.Command{statusCmd, switchCmd} {
		c.Flags().StringVar(&node, "node", "", flagNodeUsage)
		c.Flags().StringVar(&cluster, "cluster", "", "cluster name, to find the operator credential")
		c.Flags().StringVar(&kubeconfig, "kubeconfig", "", flagCredUsage)
		_ = c.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	}
	cmd.AddCommand(statusCmd, switchCmd)
	return cmd
}

// abConn dials the node whose A/B state is wanted.
func abConn(node, cluster, kubeconfig string) (*grpc.ClientConn, error) {
	kc, err := clientKubeconfig(cluster, kubeconfig)
	if err != nil {
		return nil, err
	}
	addr := node
	if addr == "" {
		if addr, err = nodeAddress(cluster); err != nil {
			return nil, err
		}
	}
	return api.Dial(addr, kc)
}

func newKubeconfigCmd() *cobra.Command {
	var node, cluster, kubeconfig, out string
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Fetch the cluster's admin kubeconfig",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, err := clientKubeconfig(cluster, kubeconfig)
			if err != nil {
				return err
			}
			addr := node
			if addr == "" {
				if addr, err = nodeAddress(cluster); err != nil {
					return err
				}
			}
			conn, err := api.Dial(addr, kc)
			if err != nil {
				return err
			}
			// The command is over; a failing close of the connection has
			// nowhere useful to go.
			defer func() { _ = conn.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := apiv1.NewVatesAPIClient(conn).GetKubeconfig(ctx, &apiv1.GetKubeconfigRequest{})
			if err != nil {
				return err
			}

			dest := out
			if dest == "" && kubeconfig == "" {
				// No explicit destination and no custom credential: the
				// kubeconfig belongs with the cluster it came from.
				name, err := resolveCluster(cluster)
				if err != nil {
					return err
				}
				dir, err := operatorDir(name)
				if err != nil {
					return err
				}
				dest = filepath.Join(dir, "kubeconfig")
			}
			if dest == "" {
				_, err := os.Stdout.Write(resp.GetKubeconfig())
				return err
			}
			if err := os.WriteFile(dest, resp.GetKubeconfig(), 0o600); err != nil {
				return err
			}
			cmd.Printf("wrote %s\n", dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", flagNodeUsage)
	cmd.Flags().StringVar(&cluster, "cluster", "", "cluster name, to find the operator credential and store the kubeconfig")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", flagCredUsage)
	cmd.Flags().StringVarP(&out, "output", "o", "", "write the kubeconfig here (default: the cluster directory, else stdout)")
	_ = cmd.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	return cmd
}

// vatesDataDir is the root of everything VATES keeps for this user, in the XDG
// data sense: $XDG_DATA_HOME/vates, or ~/.local/share/vates.
func vatesDataDir() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "vates"), nil
}

// kubeClustersDir is where the Kubernetes clusters' material lives, one
// directory per cluster. VATES manages more than Kubernetes, so clusters are one
// kind of thing under the vates data directory, not the whole of it.
func kubeClustersDir() (string, error) {
	dir, err := vatesDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kube-clusters"), nil
}

// operatorDir is where one cluster's operator material lives. It follows XDG, so
// the layout is the conventional one and the secrets sit in a data directory
// (mode 0700), never in a repository.
func operatorDir(cluster string) (string, error) {
	if cluster == "" {
		return "", fmt.Errorf("a --cluster name or an explicit --out is required")
	}
	root, err := kubeClustersDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, cluster), nil
}

// clusterNames lists the Kubernetes clusters with operator material under the
// vates data directory, sorted. Reading the directory is how both shell
// completion and the "only one cluster? use it" default see the same world.
func clusterNames() ([]string, error) {
	root, err := kubeClustersDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		// No material yet is not an error, it is a clusterless machine.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// completeClusterNames lists the clusters that exist, for shell completion of
// --cluster.
func completeClusterNames(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	names, err := clusterNames()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// clientKubeconfig resolves the operator credential. An explicit --kubeconfig
// wins; else the --cluster directory; else, when there is exactly one cluster
// under the vates data directory, that one, so a single-cluster operator need
// not repeat its name. It never falls back to the ambient kubeconfig: that is a
// Kubernetes kubeconfig, and this API authenticates with the operator's own
// certificate, which has nothing to do with it. Reaching for it turned a
// missing --cluster into a confusing error from a file the operator never chose.
//
// The file is checked here, so a missing credential is reported as what it is --
// with how to make one -- rather than as a bare open() error.
func clientKubeconfig(cluster, explicit string) (string, error) {
	if explicit != "" {
		return credential(explicit, "")
	}
	cluster, err := resolveCluster(cluster)
	if err != nil {
		return "", err
	}
	dir, err := operatorDir(cluster)
	if err != nil {
		return "", err
	}
	return credential(filepath.Join(dir, "client.kubeconfig"), cluster)
}

// resolveCluster is the cluster a command acts on: the one named, or the only
// one with material under the vates data directory, so a single-cluster
// operator does not have to repeat its name.
func resolveCluster(cluster string) (string, error) {
	if cluster != "" {
		return cluster, nil
	}
	names, err := clusterNames()
	if err != nil {
		return "", err
	}
	switch len(names) {
	case 0:
		return "", fmt.Errorf("no vates cluster to use: pass --cluster <name>, or --kubeconfig <file>")
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("more than one vates cluster: pass --cluster <name> (one of: %s)", strings.Join(names, ", "))
	}
}

// nodeAddress is where the API is called when --node is not given: the cluster
// endpoint, on the management port. The endpoint is the address an operator
// knows the cluster by -- its virtual IP -- and the API answers there on
// whichever control plane currently holds the VIP.
func nodeAddress(cluster string) (string, error) {
	ep, err := endpointOf(cluster)
	if err != nil {
		return "", err
	}
	host, _, err := net.SplitHostPort(ep)
	if err != nil {
		host = ep
	}
	return net.JoinHostPort(host, strconv.Itoa(apiPortOf(cluster))), nil
}

// apiPortOf is the management API port recorded by `gen --api-port`, or the
// default. Reading it here is what lets a deployment move the API off the
// conventional port without every later command being told.
func apiPortOf(cluster string) int {
	name, err := resolveCluster(cluster)
	if err != nil {
		return api.DefaultPort
	}
	dir, err := operatorDir(name)
	if err != nil {
		return api.DefaultPort
	}
	b, err := os.ReadFile(filepath.Join(dir, apiPortName))
	if err != nil {
		return api.DefaultPort
	}
	p, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || p < 1 || p > 65535 {
		return api.DefaultPort
	}
	return p
}

// endpointOf is the cluster's endpoint, as recorded by gen --endpoint or read
// from the kubeconfig the cluster handed over. Both name the same address.
func endpointOf(cluster string) (string, error) {
	name, err := resolveCluster(cluster)
	if err != nil {
		return "", err
	}
	dir, err := operatorDir(name)
	if err != nil {
		return "", err
	}
	if b, err := os.ReadFile(filepath.Join(dir, "endpoint")); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	}
	if b, err := os.ReadFile(filepath.Join(dir, "kubeconfig")); err == nil {
		for line := range strings.SplitSeq(string(b), "\n") {
			v, ok := strings.CutPrefix(strings.TrimSpace(line), "server:")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			v = strings.TrimPrefix(v, "https://")
			v = strings.TrimPrefix(v, "http://")
			if v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("no endpoint recorded for cluster %q: pass --node <ip>:50000, or re-run `vateskctl gen --cluster %s --endpoint <host>:6443`", name, name)
}

func credential(path, cluster string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if cluster != "" {
			return "", fmt.Errorf("%w\n  hint: generate it with `vateskctl gen --cluster %s`, and inject the CA it prints into the node's config drive", err, cluster)
		}
		return "", err
	}
	return path, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newJoinMaterialCmd() *cobra.Command {
	var node, cluster, kubeconfig string
	cmd := &cobra.Command{
		Use:   "join-material",
		Short: "Fetch fresh credentials for a machine joining the cluster",
		Long: `Fetch what a machine needs to join the cluster.

A joining worker is handed its credentials outright. A joining control plane
fetches the shared certificates itself, with the certificate key this returns.
Both need the bootstrap token, and a control plane needs the CA hash.

The values expire -- the token in 24 hours, the certificate key in two -- so this
is called when a machine is about to be created, never stored. Only a
bootstrapped control plane can answer; under CAPI the provider holds the CA and
issues these itself.

Output is shell-friendly, one NAME=value per line, so a harness can source it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, err := clientKubeconfig(cluster, kubeconfig)
			if err != nil {
				return err
			}
			addr := node
			if addr == "" {
				if addr, err = nodeAddress(cluster); err != nil {
					return err
				}
			}
			conn, err := api.Dial(addr, kc)
			if err != nil {
				return err
			}
			// The command is over; a failing close of the connection has
			// nowhere useful to go.
			defer func() { _ = conn.Close() }()

			// Long enough for kubeadm to run twice and for the cluster-info
			// signature to be published.
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			resp, err := apiv1.NewVatesAPIClient(conn).GetJoinMaterial(ctx, &apiv1.GetJoinMaterialRequest{})
			if err != nil {
				return err
			}
			cmd.Printf("TOKEN=%s\n", resp.GetToken())
			cmd.Printf("CERT_KEY=%s\n", resp.GetCertificateKey())
			cmd.Printf("CA_HASH=%s\n", resp.GetCaCertHash())
			return nil
		},
	}
	cmd.Flags().StringVar(&node, "node", "", flagNodeUsage)
	cmd.Flags().StringVar(&cluster, "cluster", "", "cluster name, to find the operator credential")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", flagCredUsage)
	_ = cmd.RegisterFlagCompletionFunc("cluster", completeClusterNames)
	return cmd
}
