package api

import (
	"os"
	"path/filepath"
	"testing"
)

// The bootstrap node has super-admin.conf; a control plane that JOINED has only
// admin.conf. The API has to serve both, because a virtual IP can land on
// either -- reading only super-admin.conf made a joining control plane report
// itself unready and refuse to hand out the kubeconfig.
func TestPickKubeconfigPrefersSuperAdmin(t *testing.T) {
	dir := t.TempDir()
	super := filepath.Join(dir, "super-admin.conf")
	admin := filepath.Join(dir, "admin.conf")
	for _, p := range []string{super, admin} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := pickKubeconfig(super, admin)
	if err != nil || got != super {
		t.Fatalf("pickKubeconfig() = %q, %v; want %q", got, err, super)
	}
}

func TestPickKubeconfigFallsBackToAdmin(t *testing.T) {
	dir := t.TempDir()
	super := filepath.Join(dir, "super-admin.conf") // absent on a joining node
	admin := filepath.Join(dir, "admin.conf")
	if err := os.WriteFile(admin, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := pickKubeconfig(super, admin)
	if err != nil || got != admin {
		t.Fatalf("pickKubeconfig() = %q, %v; want %q", got, err, admin)
	}
}

func TestPickKubeconfigWithNeither(t *testing.T) {
	dir := t.TempDir()
	if _, err := pickKubeconfig(filepath.Join(dir, "s"), filepath.Join(dir, "a")); err == nil {
		t.Fatal("pickKubeconfig() succeeded with no kubeconfig at all")
	}
}
