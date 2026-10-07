# libpng 1.6.58 -- pulled in by cairo and freetype. x86_64 uses intel-sse.
build() {
	autotools_build libpng \
		--disable-tools \
		--disable-arm-neon \
		--enable-intel-sse
}
