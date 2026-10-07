package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A single-cluster operator should not have to name the cluster they only have:
// export XDG_DATA_HOME and go. This is the case that was previously answered
// with a puzzling error about the machine's Kubernetes kubeconfig.
func TestClientKubeconfigSingleClusterIsChosen(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	want := writeCredential(t, xdg, "demo")

	got, err := clientKubeconfig("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("clientKubeconfig() = %q, want %q", got, want)
	}
}

// With a choice to make, the tool asks rather than guessing.
func TestClientKubeconfigManyClustersNeedsAName(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	writeCredential(t, xdg, "alpha")
	writeCredential(t, xdg, "beta")

	_, err := clientKubeconfig("", "")
	if err == nil {
		t.Fatal("clientKubeconfig() guessed between two clusters")
	}
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not name %q: %v", want, err)
		}
	}
}

func TestClientKubeconfigNoClustersSaysSo(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := clientKubeconfig("", "")
	if err == nil {
		t.Fatal("clientKubeconfig() succeeded with no cluster at all")
	}
	if !strings.Contains(err.Error(), "--cluster") {
		t.Fatalf("error does not suggest --cluster: %v", err)
	}
}

func TestClientKubeconfigExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := clientKubeconfig("", path)
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("clientKubeconfig() = %q, want %q", got, path)
	}
}

// A missing credential is not a mystery: the error has to say which cluster it
// looked for and how to make it.
func TestClientKubeconfigMissingNamesTheCluster(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := clientKubeconfig("demo", "")
	if err == nil {
		t.Fatal("clientKubeconfig() succeeded with no credential on disk")
	}
	if !strings.Contains(err.Error(), "gen --cluster demo") {
		t.Fatalf("error does not say how to create it: %v", err)
	}
}

func writeCredential(t *testing.T, xdg, cluster string) string {
	t.Helper()
	dir := filepath.Join(xdg, "vates", "kube-clusters", cluster)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.kubeconfig")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The operator knows the cluster by its virtual IP, so that -- not a node's own
// address -- is what a default call must aim at.
func TestNodeAddressUsesTheRecordedEndpoint(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	writeCredential(t, xdg, "demo")
	writeFile(t, filepath.Join(xdg, "vates", "kube-clusters", "demo", "endpoint"), "192.168.122.99:6443\n")

	got, err := nodeAddress("demo")
	if err != nil {
		t.Fatal(err)
	}
	if want := "192.168.122.99:50000"; got != want {
		t.Fatalf("nodeAddress() = %q, want %q", got, want)
	}
}

// A cluster that handed over its kubeconfig has already named its endpoint; the
// file is written by the kubeconfig fetch, so --endpoint is not the only source.
func TestNodeAddressFallsBackToTheStoredKubeconfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	writeCredential(t, xdg, "demo")
	writeFile(t, filepath.Join(xdg, "vates", "kube-clusters", "demo", "kubeconfig"),
		"apiVersion: v1\nclusters:\n- cluster:\n    server: https://192.168.122.99:6443\n")

	got, err := nodeAddress("demo")
	if err != nil {
		t.Fatal(err)
	}
	if want := "192.168.122.99:50000"; got != want {
		t.Fatalf("nodeAddress() = %q, want %q", got, want)
	}
}

func TestNodeAddressWithoutAnEndpointSaysHowToRecordIt(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	writeCredential(t, xdg, "demo")

	_, err := nodeAddress("demo")
	if err == nil {
		t.Fatal("nodeAddress() succeeded with no endpoint anywhere")
	}
	if !strings.Contains(err.Error(), "--node") || !strings.Contains(err.Error(), "--endpoint") {
		t.Fatalf("error does not offer --node and --endpoint: %v", err)
	}
}
