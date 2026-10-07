# kbd 2.9.0 -- setfont and the eurlatgr font, for the text dashboard's
# box-drawing glyphs. Only setfont and that font are kept afterwards
# (trim.sh); here the whole package is installed for now.
build() {
	local d
	d=$(unpack kbd | tail -n1)
	(
		cd "${d}"
		./configure \
			--host="${HOST}" --build="${BUILD}" \
			--prefix=/usr --sysconfdir=/etc --localstatedir=/var \
			--disable-vlock --disable-tests \
			--without-bzip2 --without-lzma --with-zlib --without-zstd \
			--disable-nls --disable-static --enable-shared
		make -j"$(nproc)"
		make DESTDIR="${SYSROOT}" \
			MKINSTALLDIRS="${d}/config/mkinstalldirs" install
	)
}
