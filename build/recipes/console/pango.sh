# pango 1.58.0 -- text layout, the top of the console stack. Built on cairo,
# harfbuzz, fontconfig, freetype, glib and fribidi (its dependencies are the
# other recipes). No introspection, no Xft.
build() {
	meson_build pango \
		-Dfontconfig=enabled \
		-Dintrospection=disabled
}
