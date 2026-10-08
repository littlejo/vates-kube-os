package launcher

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/vatesfr/vates-kube-os/internal/k8sbin"
)

// Launcher fetches one Kubernetes binary and runs it.
//
// It is what the kubelet image contains instead of the kubelet. The image is
// built once and serves any supported Kubernetes version; the version is chosen
// when the machine is created, in vates-node.yaml, and reaches this program
// through the environment:
//
//	KUBERNETES_VERSION      v1.31.4
//	KUBERNETES_BINARY_BASE  https://dl.k8s.io/release   (optional)
//	VATES_BINARY_CACHE      /var/lib/vates/kubernetes    (optional)
//
// It is reached by four names, and they are the four the rest of the system
// already expects, so nothing else had to learn a new path:
//
//	/usr/local/bin/kubelet    <- the unit, and kubeadm's own exec of it
//	/usr/local/bin/kubeadm    <- vates-init, for the init and join phases
//	/usr/local/bin/kubectl    <- vates-init and the cluster scripts
//	/usr/local/bin/mounter    <- the kubelet execs it to perform mounts
//
// The name is argv[0] of a symlink to cmd/vates, which is what component is.
//
// The binary is fetched once and cached under the version and architecture, so a
// restart -- and `Restart=always` makes those routine -- costs nothing. The
// published checksum is verified before anything is executed, and a mismatch
// installs nothing and fails loudly: this is a privileged agent on the host, and
// running "something" is not an option.
//
// Defaults for the environment variables that name a source and a cache. They
// match the schema's defaults so that a node configured the old way, without a
// mirror, behaves exactly as before.
const (
	defaultBinaryBase = k8sbin.DefaultBase
	defaultCacheRoot  = "/var/lib/vates/kubernetes"
)

// downloadTimeout is per request, and retries are attempted inside it.
//
// Generous: the kubelet waits for this, and a slow mirror is better than a node
// that never starts. The alternative -- no timeout -- trades a slow start for a
// hung one.
const downloadTimeout = 5 * time.Minute

// Launcher runs one Kubernetes component: it fetches it at the requested
// version, verifies it, caches it and execs it with the given arguments.
func Launcher(component string, args []string) error {
	if component == "" {
		return fmt.Errorf("usage: %s <%s> [args...]", "vates launcher", joinComponents())
	}
	if !k8sbin.Known(component) {
		return fmt.Errorf("%q is not one of %s", component, joinComponents())
	}

	version := env("KUBERNETES_VERSION", "")
	if version == "" {
		return fmt.Errorf("KUBERNETES_VERSION is empty: the node was not told which Kubernetes to run")
	}
	base := env("KUBERNETES_BINARY_BASE", defaultBinaryBase)
	cacheRoot := env("VATES_BINARY_CACHE", defaultCacheRoot)

	arch, err := k8sbin.Arch(runtime.GOARCH)
	if err != nil {
		return err
	}

	binary, err := ensure(base, version, arch, component, cacheRoot)
	if err != nil {
		return err
	}

	// exec, not a child process: the launcher must leave nothing behind. The
	// kubelet, in particular, is PID 1 of its container and expects to be the
	// process systemd and containerd supervise, not a child of one.
	path, err := exec.LookPath(binary)
	if err != nil {
		path = binary
	}
	if err := syscall.Exec(path, append([]string{component}, args...), os.Environ()); err != nil {
		return fmt.Errorf("cannot execute %s: %w", path, err)
	}
	return nil // unreachable: syscall.Exec replaces the process
}

// ensure returns the path to the component, fetching it if it is not cached.
func ensure(base, version, arch, component, cacheRoot string) (string, error) {
	dest := k8sbin.CachePath(cacheRoot, version, arch, component)

	// A cached binary is trusted: it was verified when it was written, and the
	// cache is only writable by root. Re-hashing 120 MB on every kubelet restart
	// would slow every boot to prove something that has not changed.
	if err := executable(dest); err == nil {
		return dest, nil
	}

	url := k8sbin.URL(base, version, arch, component)
	start := time.Now()
	fmt.Fprintf(os.Stderr, "vates-launcher: fetching %s\n", url)

	content, err := fetch(url)
	if err != nil {
		return "", fmt.Errorf("cannot fetch %s: %w "+
			"(the node needs this binary to start; set binaries.base in "+
			"vates-node.yaml to an internal mirror if the public one is not "+
			"reachable -- and note that the control plane's images are pulled "+
			"from a registry too, so a mirror has to serve both)", url, err)
	}

	sumBody, err := fetch(k8sbin.ChecksumURL(base, version, arch, component))
	if err != nil {
		return "", fmt.Errorf("cannot fetch the checksum for %s: %w", url, err)
	}
	want, err := k8sbin.ParseChecksum(sumBody)
	if err != nil {
		return "", err
	}
	if err := k8sbin.Verify(content, want); err != nil {
		return "", err
	}

	if err := install(dest, content); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "vates-launcher: %s %s verified and cached (%d bytes in %s)\n",
		component, version, len(content), time.Since(start).Round(time.Millisecond))
	return dest, nil
}

// fetch downloads a URL, with a bounded number of retries.
func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: downloadTimeout}
	var lastErr error

	for attempt := 1; attempt <= 3; attempt++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
		_ = resp.Body.Close() // the body is read and the status is checked below; a failed Close adds nothing
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			// A 404 is not worth retrying: the version does not exist, or the
			// mirror is laid out differently. Saying so immediately is more
			// useful than three identical failures.
			return nil, fmt.Errorf("%s: %s", resp.Status, url)
		}
		return body, nil
	}
	return nil, lastErr
}

// install writes the binary where it belongs, atomically.
//
// Through a temporary file in the same directory and a rename: two containers
// can race here -- the unit starts the kubelet while vates-init runs kubeadm
// -- and a half-written binary that another process then executed would be a
// failure with no cause to point at.
func install(dest string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".fetch-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // best-effort cleanup: the rename below removes it on success

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

// executable reports whether the path is a file that can be run.
func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func joinComponents() string {
	return strings.Join(k8sbin.Components, ", ")
}
