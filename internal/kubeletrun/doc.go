// Package kubeletrun starts the kubelet container under containerd.
//
// It exists because `ctr run` cannot do one thing the kubelet needs: set
// linux.rootfsPropagation. Without it the container's rootfs is `rslave`, and a
// mount created inside the container never propagates to the host -- measured:
// the /var/lib/kubelet bind lands in a NEW shared peer group even when the host
// path is already shared, so pods never see the tmpfs the kubelet mounts for
// their service-account tokens. `ctr` and `nerdctl` expose no flag for it, but
// the container spec does. This program builds that spec through containerd's
// own client and runs the task.
//
// It is the `vates-kubelet-run` face of cmd/vates, so the image still carries a
// single binary.
//
// Upstream references, to revisit when they change:
//
//   - containerd#5381 "ctr run/create needs a flag to set
//     spec.Linux.RootfsPropagation" -- the missing flag, and the report that
//     setting the field makes propagation work:
//     https://github.com/containerd/containerd/issues/5381
//   - runc#5390 "rshared propagation ... silently results in private mounts"
//     -- the same symptom reached through the runtime; if this is fixed and
//     containerd exposes the field (or a flag), this runner could be replaced
//     by a plain `ctr run`:
//     https://github.com/opencontainers/runc/issues/5390
//
// Until then, keep this runner: without rootfsPropagation=rshared, pod volumes
// do not reach the host and no pod can read its token.
package kubeletrun
