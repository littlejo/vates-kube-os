# libconfuse 3.3 -- the config-file parser genimage is built against. This is a
# HOST tool (built for the builder, installed under /opt/host); it does not go
# into the image.
build() {
	host_autotools_build libconfuse --disable-rpath --disable-static --enable-shared
}
