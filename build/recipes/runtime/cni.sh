# CNI plugins 1.7.1 -- the generic containernetworking plugins. They are the
# read-only LOWER layer of the /opt/cni/bin overlay (internal/sysinit/cni.go); a
# chosen CNI adds its own plugin on top at runtime.
build() {
	local t
	t=$(fetch cni | tail -n1)
	install -d "${SYSROOT}/usr/share/vates/cni/bin"
	tar -C "${SYSROOT}/usr/share/vates/cni/bin" -xf "${t}"
}
