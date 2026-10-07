# genimage 20 -- assembles the bootable disk from a declarative config. A HOST
# tool (installed under /opt/host), built against the libconfuse beside it.
build() {
	export PKG_CONFIG_PATH="/opt/host/lib/pkgconfig:/opt/host/share/pkgconfig"
	host_autotools_build genimage --disable-static --disable-rpath
}
