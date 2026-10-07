# fontconfig 2.17.1 -- font matching, on top of freetype and expat. meson.
# The cache lives under /var, which is the writable partition.
build() {
	meson_build fontconfig \
		-Dcache-dir=/var/cache/fontconfig \
		-Dtests=disabled \
		-Ddoc=disabled
}
