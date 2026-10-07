# gptfdisk 1.0.10 -- sgdisk, which extends the GPT when the disk grows. Plain
# Makefile package; it links libuuid (util-linux) and, for sgdisk, popt.
build() {
	local d
	d=$(unpack gptfdisk | tail -n1)
	(
		cd "${d}"
		make -j"$(nproc)" \
			CC=gcc CXX=g++ \
			CFLAGS="${CFLAGS}" CXXFLAGS="${CXXFLAGS}" LDFLAGS="${LDFLAGS}" \
			LDLIBS="-luuid" \
			SGDISK_LDLIBS="$(pkg-config --libs popt)" \
			sgdisk
		install -D -m 0755 sgdisk "${SYSROOT}/usr/sbin/sgdisk"
	)
}
