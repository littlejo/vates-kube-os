# linux 7.1.13 -- the kernel. Independent of the userspace: it builds alongside
# the socle/console passes and only meets them at assembly.
#
# It is freestanding: the cross flags common.sh exports (--sysroot) do not apply,
# so they are cleared here. The kernel options are board/fragments/, one file per
# platform (00-common, 10-kvm, 20-xen), merged in order -- nftables, cgroup v2,
# VXLAN, DRM, /proc/config.gz, and BTF for Cilium -- which is why pahole/dwarves
# is in the builder image.
build() {
	unset CFLAGS CXXFLAGS CPPFLAGS LDFLAGS
	unset CC CXX LD AR AS NM RANLIB STRIP OBJCOPY OBJDUMP READELF

	local d fragments
	d=$(unpack linux | tail -n1)
	fragments=("${SCRATCH}"/board/fragments/*.fragment)
	(
		cd "${d}"
		make ARCH=x86_64 defconfig
		scripts/kconfig/merge_config.sh -m .config "${fragments[@]}"
		make ARCH=x86_64 olddefconfig
		make -j"$(nproc)" ARCH=x86_64 bzImage modules
		make ARCH=x86_64 INSTALL_MOD_PATH="${SYSROOT}" modules_install
		install -D -m 0644 arch/x86/boot/bzImage "${SYSROOT}/boot/bzImage"
	)
}
