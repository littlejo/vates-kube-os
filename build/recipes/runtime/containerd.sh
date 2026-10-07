# containerd 2.1.5 -- upstream release binaries (linux-amd64 tarball). Contains
# containerd, the runc v2 shim and ctr. glibc-dynamic, like the image.
build() {
	local t tmp
	t=$(fetch containerd | tail -n1)
	tmp=$(mktemp -d)
	tar -C "${tmp}" -xf "${t}"
	install -D -m 0755 "${tmp}/bin/containerd" "${SYSROOT}/usr/bin/containerd"
	install -D -m 0755 "${tmp}/bin/containerd-shim-runc-v2" "${SYSROOT}/usr/bin/containerd-shim-runc-v2"
	install -D -m 0755 "${tmp}/bin/ctr" "${SYSROOT}/usr/bin/ctr"
	rm -rf "${tmp}"
}
