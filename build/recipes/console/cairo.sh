# cairo 1.18.4 -- the console's drawing library. No X (no xlib/xcb), no dwrite or
# quartz: the DRM backend and the PNG surface are what the console uses.
build() {
	meson_build cairo \
		-Ddwrite=disabled \
		-Dfontconfig=enabled \
		-Dquartz=disabled \
		-Dtests=disabled \
		-Dspectre=disabled \
		-Dsymbol-lookup=disabled \
		-Dgtk_doc=false \
		-Dfreetype=enabled \
		-Dglib=enabled \
		-Dxcb=disabled \
		-Dxlib=disabled \
		-Dxlib-xcb=disabled \
		-Dpng=enabled \
		-Dtee=disabled \
		-Dzlib=enabled \
		-Dlzo=disabled
}
