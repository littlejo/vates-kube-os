# libffi 3.8.0 -- foreign function interface, pulled in by glib (GObject).
build() {
	autotools_build libffi \
		--disable-multi-os-directory \
		--disable-exec-static-tramp
}
