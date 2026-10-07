# e2fsprogs 1.47.4 -- resize2fs grows /var into the space the hypervisor adds.
# Its install target is "install install-libs", and ac_cv_path_LDCONFIG=true
# keeps configure from calling the host's ldconfig, so it is built by hand.
build() {
	local d
	d=$(unpack e2fsprogs | tail -n1)
	(
		cd "${d}"
		CFLAGS="${CFLAGS} -Os" ac_cv_path_LDCONFIG=true ./configure \
			--host="${HOST}" --build="${BUILD}" \
			--prefix=/usr --exec-prefix=/usr \
			--sysconfdir=/etc --localstatedir=/var \
			--bindir=/bin --sbindir=/sbin --enable-elf-shlibs \
			--disable-debugfs --disable-imager --disable-defrag \
			--enable-fsck --enable-resizer --disable-uuidd \
			--disable-libblkid --disable-libuuid --disable-e2initrd-helper \
			--disable-testio-debug --disable-rpath --enable-symlink-install \
			--disable-fuse2fs --disable-nls --disable-static --enable-shared
		make -j"$(nproc)"
		make DESTDIR="${SYSROOT}" E2SCRUB_DIR= install install-libs
	)
}
