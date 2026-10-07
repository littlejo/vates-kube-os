package kubeletrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/core/containers"
	"github.com/containerd/containerd/v2/defaults"
	"github.com/containerd/containerd/v2/pkg/cio"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"github.com/containerd/containerd/v2/pkg/oci"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Where containerd listens, which namespace the system's own containers live in,
// and the image the build imports. These match build/provision.sh and the unit.
const (
	address     = "/run/containerd/containerd.sock"
	namespace   = "vates"
	imageRef    = "localhost/vates/kubelet:current"
	containerID = "vates-kubelet"
	// snapshotID names the writable rootfs the container runs from. Fixed, like
	// the container id: a leftover from a hard kill is removed before the next
	// create rather than accumulating.
	snapshotID = "vates-kubelet-snapshot"
)

// mounts are the host paths the kubelet container needs, the same list the
// unit used to carry. /var/lib/kubelet is the one that must be rshared.
//
// /etc/resolv.conf and /etc/hosts are here for the same reason podman mounted
// them: the container has no resolver of its own, and without the host's the
// launcher cannot resolve dl.k8s.io to fetch the Kubernetes binaries. Measured:
// without it, Go falls back to ::1:53 and every fetch fails with "connection
// refused" -- the kubelet never starts, and nothing says why.
var mounts = []specs.Mount{
	{Type: "bind", Source: "/etc/resolv.conf", Destination: "/etc/resolv.conf", Options: []string{"rbind", "ro"}},
	{Type: "bind", Source: "/etc/hosts", Destination: "/etc/hosts", Options: []string{"rbind", "ro"}},
	// The node's operating-system identity. The kubelet is a FROM-scratch
	// container with no os-release of its own; without the host's file it would
	// report nothing as the node's osImage in `kubectl get nodes`.
	{Type: "bind", Source: "/etc/os-release", Destination: "/etc/os-release", Options: []string{"rbind", "ro"}},
	// /dev/kmsg: the kubelet opens it at startup, and the container's /dev is a
	// fresh tmpfs that has no such node. Without it the kubelet dies with
	// "failed to create kubelet: open /dev/kmsg: no such file or directory" --
	// podman provided it, the OCI default devices do not.
	{Type: "bind", Source: "/dev/kmsg", Destination: "/dev/kmsg", Options: []string{"rbind"}},
	{Type: "bind", Source: "/var/lib/kubelet", Destination: "/var/lib/kubelet", Options: []string{"rbind", "rshared"}},
	// Read-WRITE, unlike the others: with TLS bootstrap the kubelet writes its
	// own /etc/kubernetes/kubelet.conf here, from the certificate the API server
	// issues it. A read-only mount leaves the kubelet unable to record the
	// credential it earned, and it restarts forever.
	{Type: "bind", Source: "/etc/kubernetes", Destination: "/etc/kubernetes", Options: []string{"rbind"}},
	{Type: "bind", Source: "/etc/kubelet", Destination: "/etc/kubelet", Options: []string{"rbind", "ro"}},
	{Type: "bind", Source: "/sys/fs/cgroup", Destination: "/sys/fs/cgroup", Options: []string{"rbind"}},
	{Type: "bind", Source: "/var/log", Destination: "/var/log", Options: []string{"rbind"}},
	{Type: "bind", Source: "/run/containerd", Destination: "/run/containerd", Options: []string{"rbind"}},
	// The host's /run. The kubelet creates hostPath files under it -- the
	// /run/xtables.lock the CNI plugins lock, /run/flannel/... -- and it is a
	// container, so without this it writes into its OWN tmpfs and the host path
	// stays missing. containerd then mounts a directory where a file was
	// expected: "Fatal: can't open lock file /run/xtables.lock: Is a directory".
	{Type: "bind", Source: "/run", Destination: "/run", Options: []string{"rbind"}},
	{Type: "bind", Source: "/var/lib/containerd", Destination: "/var/lib/containerd", Options: []string{"rbind"}},
	{Type: "bind", Source: "/run/systemd/system", Destination: "/run/systemd/system", Options: []string{"rbind"}},
	{Type: "bind", Source: "/run/systemd/private", Destination: "/run/systemd/private", Options: []string{"rbind"}},
	{Type: "bind", Source: "/etc/machine-id", Destination: "/etc/machine-id", Options: []string{"rbind", "ro"}},
	{Type: "bind", Source: "/var/lib/vates/kubernetes", Destination: "/var/lib/vates/kubernetes", Options: []string{"rbind"}},
}

// optionalMounts are the sources that exist on a systemd machine and not
// otherwise. runc fails a bind mount whose source is missing ("failed to fulfil
// mount request: open /run/systemd/system: no such file or directory"), so they
// are included only when their source is there.
var optionalMounts = map[string]bool{
	"/run/systemd/system":  true,
	"/run/systemd/private": true,
	"/etc/machine-id":      true,
	"/etc/os-release":      true,
}

// containerMounts is mounts, minus the optional ones whose source is absent.
func containerMounts() []specs.Mount {
	out := make([]specs.Mount, 0, len(mounts))
	for _, m := range mounts {
		if optionalMounts[m.Source] {
			if _, err := os.Stat(m.Source); err != nil {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// Run starts the container with args as the kubelet's command line and waits for
// it, returning when it exits. The unit restarts it (Restart=always), so a
// kubelet that dies is a container that is started again.
func Run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: vates-kubelet-run <command> [args...]")
	}
	ctx := namespaces.WithNamespace(context.Background(), namespace)

	client, err := containerd.New(address)
	if err != nil {
		return fmt.Errorf("connecting to containerd: %w", err)
	}
	defer func() { _ = client.Close() }()

	image, err := client.GetImage(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("image %s is not in containerd's %q namespace: %w", imageRef, namespace, err)
	}

	// A container of the same name left by a kill that skipped the cleanup would
	// make the create below fail. Remove it, task first, and drop its snapshot.
	if old, err := client.LoadContainer(ctx, containerID); err == nil {
		if task, err := old.Task(ctx, nil); err == nil {
			_ = task.Kill(ctx, syscall.SIGKILL)
			_, _ = task.Delete(ctx, containerd.WithProcessKill)
		}
		_ = old.Delete(ctx)
	}
	_ = client.SnapshotService(defaults.DefaultSnapshotter).Remove(ctx, snapshotID)

	opts := []oci.SpecOpts{
		oci.WithDefaultSpec(),
		oci.WithImageConfig(image),
		oci.WithProcessArgs(args...),
		oci.WithEnv([]string{
			"KUBERNETES_VERSION=" + os.Getenv("KUBERNETES_VERSION"),
			"KUBERNETES_BINARY_BASE=" + os.Getenv("KUBERNETES_BINARY_BASE"),
		}),
		oci.WithMounts(containerMounts()),
		oci.WithPrivileged,
		// WithPrivileged sets capabilities, masked paths, seccomp and the like,
		// but NOT the device cgroup allow-list -- podman's --privileged does.
		// Without this the kubelet's open("/dev/kmsg") fails with EPERM even
		// though the node is mounted and the process is root.
		oci.WithAllDevicesAllowed,
		oci.WithHostNamespace(specs.NetworkNamespace),
		oci.WithHostNamespace(specs.PIDNamespace),
		// The whole point: without this the rootfs is rslave and nothing the
		// kubelet mounts reaches the host. See the package comment.
		withRootfsPropagation,
	}

	// On an SELinux system the kubelet container must carry a label, as podman
	// gave it: the files it reads -- /etc/kubernetes, /var/lib/kubelet, the
	// fetched binaries -- are labelled container_file_t by vates-init, and an
	// unlabelled process under Enforcing is denied them. A privileged container
	// runs as spc_t, which is what Kubernetes and podman use for exactly this
	// kind of system agent.
	//
	// The test is a mounted selinuxfs, not the /sys/fs/selinux directory: the
	// directory exists as soon as the kernel is built with SELinux, even with it
	// disabled at boot, and runc then refuses the label ("selinux label is
	// specified in config, but selinux is disabled"). /sys/fs/selinux/enforce
	// exists only when selinuxfs is mounted -- the real "SELinux is on".
	if _, err := os.Stat("/sys/fs/selinux/enforce"); err == nil {
		opts = append(opts, oci.WithSelinuxLabel("system_u:system_r:spc_t:s0"))
	}

	container, err := client.NewContainer(ctx, containerID,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(snapshotID, image),
		containerd.WithNewSpec(opts...),
	)
	if err != nil {
		return fmt.Errorf("creating the kubelet container: %w", err)
	}
	defer func() { _ = container.Delete(ctx) }()

	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("creating the kubelet task: %w", err)
	}
	defer func() { _, _ = task.Delete(ctx, containerd.WithProcessKill) }()

	// systemd stops this unit with SIGTERM; forward it so the container's
	// process is told to stop rather than outliving the runner.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)
	go func() {
		for range sigs {
			_ = task.Kill(ctx, syscall.SIGTERM)
		}
	}()

	if err := task.Start(ctx); err != nil {
		return fmt.Errorf("starting the kubelet: %w", err)
	}

	status, err := task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting for the kubelet: %w", err)
	}
	code := <-status
	if code.ExitCode() != 0 {
		return fmt.Errorf("the kubelet exited with status %d", code.ExitCode())
	}
	return nil
}

// withRootfsPropagation sets the one field `ctr` cannot.
//
// See the package comment for the upstream issues (containerd#5381, runc#5390)
// and when this can be revisited.
func withRootfsPropagation(_ context.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
	if s.Linux == nil {
		s.Linux = &specs.Linux{}
	}
	s.Linux.RootfsPropagation = "rshared"
	return nil
}
