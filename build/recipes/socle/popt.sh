# popt 1.19 -- option parser, pulled in by gptfdisk. NLS off (no gettext in the
# image), and the va_copy check is pre-seeded.
build() {
	local d
	d=$(unpack popt | tail -n1)
	(
		cd "${d}"
		ac_cv_va_copy=yes ./configure \
			--host="${HOST}" --build="${BUILD}" \
			--prefix=/usr --disable-nls
		make -j"$(nproc)"
		make DESTDIR="${SYSROOT}" install
	)
}
