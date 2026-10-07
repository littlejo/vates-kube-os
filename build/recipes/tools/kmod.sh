# kmod 34.2 -- modprobe/depmod/insmod, and libkmod. meson package; zlib on (it is
# in the socle), zstd/xz/openssl off, no man pages or completions.
build() {
	meson_build kmod \
		-Dmanpages=false \
		-Dbashcompletiondir=no \
		-Dfishcompletiondir=no \
		-Dzshcompletiondir=no \
		-Ddlopen=all \
		-Dzlib=enabled \
		-Dzstd=disabled \
		-Dxz=disabled \
		-Dopenssl=disabled
}
