# freetype 2.14.3 -- font rasterizer. harfbuzz off (freetype does not use it);
# zlib and libpng on, the other compressors and brotli off.
build() {
	autotools_build freetype \
		--without-harfbuzz --enable-freetype-config \
		--with-zlib --without-brotli --without-bzip2 --with-png \
		--disable-static --enable-shared --disable-nls
}
