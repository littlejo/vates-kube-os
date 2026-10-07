# pixman 0.46.4 -- pixel manipulation, cairo's raster backend. meson package.
# The SIMD options: x86_64 has MMX/SSE2/SSSE3, everything else
# off. libpng is disabled here (cairo links its own).
build() {
	meson_build pixman \
		-Dmmx=enabled \
		-Dsse2=enabled \
		-Dssse3=enabled \
		-Darm-simd=disabled \
		-Dneon=disabled \
		-Da64-neon=disabled \
		-Drvv=disabled \
		-Dloongson-mmi=disabled \
		-Dvmx=disabled \
		-Dmips-dspr2=disabled \
		-Dopenmp=disabled \
		-Dgtk=disabled \
		-Dlibpng=disabled \
		-Dtests=disabled \
		-Dgnuplot=false
}
