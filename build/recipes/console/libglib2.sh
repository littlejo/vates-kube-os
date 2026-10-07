# glib 2.88.3 -- GObject, which cairo/pango/harfbuzz are built with. meson.
# introspection, selinux and xattr off; libmount on (util-linux, from the tools
# pass); debug, libelf, tests and oss-fuzz off.
build() {
	meson_build libglib2 \
		-Dglib_debug=disabled \
		-Dlibelf=disabled \
		-Dgio_module_dir=/usr/lib/gio/modules \
		-Dtests=false \
		-Doss_fuzz=disabled \
		-Dintrospection=disabled \
		-Dselinux=disabled \
		-Dxattr=false \
		-Dlibmount=enabled
}
