package launcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// fakeRelease serves one component the way Kubernetes publishes it: the binary,
// and its checksum beside it.
type fakeRelease struct {
	version   string
	arch      string
	component string
	binary    []byte
	// corrupt makes the published checksum wrong, which is what a corrupted
	// download or a meddling mirror looks like from here.
	corrupt bool
	hits    atomic.Int32
}

func (f *fakeRelease) start(t *testing.T) *httptest.Server {
	t.Helper()
	prefix := fmt.Sprintf("/%s/bin/linux/%s/%s", f.version, f.arch, f.component)
	sum := sha256.Sum256(f.binary)
	hexsum := hex.EncodeToString(sum[:])
	if f.corrupt {
		hexsum = "00" + hexsum[2:]
	}

	mux := http.NewServeMux()
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		_, _ = w.Write(f.binary)
	})
	mux.HandleFunc(prefix+".sha256", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		_, _ = fmt.Fprint(w, hexsum)
	})
	return httptest.NewServer(mux)
}

func TestEnsureFetchesVerifiesAndCaches(t *testing.T) {
	release := &fakeRelease{
		version:   "v1.31.4",
		arch:      "amd64",
		component: "kubelet",
		binary:    []byte("#!/bin/sh\necho kubelet\n"),
	}
	server := release.start(t)
	defer server.Close()

	cache := t.TempDir()

	got, err := ensure(server.URL, release.version, release.arch, release.component, cache)
	if err != nil {
		t.Fatalf("ensure() failed: %v", err)
	}
	if want := filepath.Join(cache, release.version, release.arch, release.component); got != want {
		t.Errorf("ensure() = %q, want %q", got, want)
	}

	// It has to be runnable: this is what syscall.Exec is handed next.
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("the binary was not installed: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("the installed binary is not executable (mode %v)", info.Mode())
	}

	// Second call: served from the cache, so the mirror is not asked again.
	// `Restart=always` makes restarts routine, and re-fetching on each of them
	// would put the network back on the critical path of every boot.
	before := release.hits.Load()
	if _, err := ensure(server.URL, release.version, release.arch, release.component, cache); err != nil {
		t.Fatalf("the cached binary was not reused: %v", err)
	}
	if after := release.hits.Load(); after != before {
		t.Errorf("a cached binary was fetched again (%d new requests)", after-before)
	}
}

func TestEnsureRefusesABinaryThatDoesNotMatchItsChecksum(t *testing.T) {
	// The whole point of the checksum. A privileged agent that runs "something"
	// because a download half-succeeded is worse than one that refuses to start:
	// the first is a machine behaving unpredictably, the second is a message.
	release := &fakeRelease{
		version:   "v1.31.4",
		arch:      "amd64",
		component: "kubelet",
		binary:    []byte("a binary"),
		corrupt:   true,
	}
	server := release.start(t)
	defer server.Close()

	cache := t.TempDir()
	if _, err := ensure(server.URL, release.version, release.arch, release.component, cache); err == nil {
		t.Fatal("a binary whose checksum does not match was accepted")
	}

	// And nothing was left behind that a later run would execute.
	if _, err := os.Stat(filepath.Join(cache, release.version, release.arch, release.component)); err == nil {
		t.Error("a rejected binary was left in the cache")
	}
	entries, _ := os.ReadDir(filepath.Join(cache, release.version, release.arch))
	for _, e := range entries {
		t.Errorf("the cache is not clean: %s remains", e.Name())
	}
}

func TestEnsureSaysSoWhenTheVersionDoesNotExist(t *testing.T) {
	// A 404 is the shape of "the mirror does not have this version" and of "the
	// version is misspelled". Both want a message, not three retries.
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := ensure(server.URL, "v9.9.9", "amd64", "kubelet", t.TempDir())
	if err == nil {
		t.Fatal("a missing version was treated as success")
	}
	if !contains(err.Error(), "404") && !contains(err.Error(), "not found") {
		t.Errorf("the error does not name the problem: %v", err)
	}
}

func TestEnsureReplacesAHalfWrittenBinaryInOneStep(t *testing.T) {
	// Two containers can race here: the unit starts the kubelet while
	// vates-init runs kubeadm, and both may fetch the same component. Nothing
	// may ever observe a partial file at the destination.
	release := &fakeRelease{
		version:   "v1.31.4",
		arch:      "amd64",
		component: "kubeadm",
		binary:    []byte("a kubeadm"),
	}
	server := release.start(t)
	defer server.Close()

	cache := t.TempDir()
	dest := filepath.Join(cache, release.version, release.arch, release.component)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	// A truncated file already at the destination, as a previous interrupted
	// fetch would leave.
	if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ensure(server.URL, release.version, release.arch, release.component, cache)
	if err != nil {
		t.Fatalf("ensure() did not replace a non-executable cache entry: %v", err)
	}
	body, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(release.binary) {
		t.Errorf("the destination holds %q, want the fetched binary", body)
	}

	// And no temporary files are left for an operator to wonder about.
	entries, _ := os.ReadDir(filepath.Dir(dest))
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
