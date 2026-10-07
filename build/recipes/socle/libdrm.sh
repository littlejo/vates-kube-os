# libdrm 2.4.134 -- the console's DRM/KMS. meson package. Every hardware driver
# and udev are off: the console only needs the generic ioctl
# layer.
build() {
	meson_build libdrm \
		-Dcairo-tests=disabled \
		-Dman-pages=disabled \
		-Dtests=false \
		-Dudev=false \
		-Dintel=disabled \
		-Dradeon=disabled \
		-Damdgpu=disabled \
		-Dnouveau=disabled \
		-Dvmwgfx=disabled \
		-Domap=disabled \
		-Detnaviv=disabled \
		-Dexynos=disabled \
		-Dfreedreno=disabled \
		-Dtegra=disabled \
		-Dvc4=disabled \
		-Dvalgrind=disabled
}
