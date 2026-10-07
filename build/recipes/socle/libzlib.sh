# zlib 1.3.2 -- plain Makefile package (not autotools), -fPIC shared library.
# The package is named libzlib here; it PROVIDES zlib.
build() {
	local d
	d=$(unpack libzlib | tail -n1)
	(
		cd "${d}"
		CFLAGS="${CFLAGS} -fPIC" ./configure --shared --prefix=/usr
		make -j"$(nproc)"
		make DESTDIR="${SYSROOT}" LDCONFIG=true install
	)
}
