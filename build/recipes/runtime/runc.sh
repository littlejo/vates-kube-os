# runc 1.3.4 -- the OCI runtime containerd's v2 shim execs. Upstream binary;
# it links libseccomp (in the socle) and is glibc-dynamic.
build() {
	install -D -m 0755 "$(fetch runc | tail -n1)" "${SYSROOT}/usr/bin/runc"
}
