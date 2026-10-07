// Package sysinit brings the machine up: it is the image's PID 1, and the work
// around it that happens once, at boot -- mounting and growing /var, mounting
// the CNI overlays, starting NTP, blessing the booted A/B slot, and drawing the
// boot screen.
//
// There is no init system on purpose: the node carries a single Go binary whose
// only job is to run Kubernetes, so it can be the init process itself. Pid1 is
// that process: it mounts the pseudo filesystems a container host needs, applies
// sysctls, starts containerd and the console, and reaps.
//
// It is reached as `/sbin/init` (a symlink to the vates binary), so it is
// dispatched by argv[0]. `vates-init` remains the first-boot configuration and
// is a different verb on purpose.
package sysinit
