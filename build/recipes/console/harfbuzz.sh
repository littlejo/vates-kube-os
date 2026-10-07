# harfbuzz 14.3.1 -- text shaping. C++ internally. cairo, freetype and glib on;
# gobject/introspection, graphite, icu and the platform backends off.
build() {
	meson_build harfbuzz \
		-Dgdi=disabled \
		-Ddirectwrite=disabled \
		-Dcoretext=disabled \
		-Dtests=disabled \
		-Ddocs=disabled \
		-Dbenchmark=disabled \
		-Dicu_builtin=false \
		-Dexperimental_api=false \
		-Dfuzzer_ldflags= \
		-Dcairo=enabled \
		-Dfreetype=enabled \
		-Dgobject=disabled \
		-Dintrospection=disabled \
		-Dgraphite=disabled \
		-Dglib=enabled \
		-Dicu=disabled
}
