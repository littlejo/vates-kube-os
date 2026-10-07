#!/bin/bash
# Common environment and helpers for the from-scratch builder.
#
# Sourced by lib/run-recipe.sh inside the build image. It sets the native
# environment and provides fetch (download + sha256 verify), unpack, and the two
# build-system drivers this engine uses: autotools and meson.
#
# We do NOT cross-compile. The build host and the target are both x86_64, and
# the compiler is the seed gcc of the builder image (Fedora). What we own is the
# C library: it is built from GNU source in the libc pass and installed into
# /sysroot, which is the staging tree every recipe installs into and every
# compile points at with --sysroot. Nothing but our glibc and the compiler's own
# runtime (crtbegin.o, libgcc, libstdc++, from the seed gcc) lives there.
set -euo pipefail

: "${SCRATCH:=/build}"
: "${DL:=/src/dl}"
: "${WORK:=/work}"
: "${SYSROOT:=/sysroot}"

PINS="${SCRATCH}/pins.conf"

# --- native environment, pointed at our sysroot -----------------------------

export CC="gcc"
export CXX="g++"
export AR="ar"
export AS="as"
export LD="ld"
export NM="nm"
export RANLIB="ranlib"
export STRIP="strip"
export OBJCOPY="objcopy"
export OBJDUMP="objdump"
export READELF="readelf"

export CFLAGS="--sysroot=${SYSROOT} -O2 -pipe"
export CXXFLAGS="${CFLAGS}"
export CPPFLAGS="--sysroot=${SYSROOT}"
export LDFLAGS="--sysroot=${SYSROOT}"

export PKG_CONFIG_SYSROOT_DIR="${SYSROOT}"
export PKG_CONFIG_LIBDIR="${SYSROOT}/usr/lib/pkgconfig:${SYSROOT}/usr/share/pkgconfig:${SYSROOT}/usr/lib64/pkgconfig"

# Same triplet for host and target: a native build that happens to be rooted in
# our sysroot.
HOST="x86_64-pc-linux-gnu"
BUILD="x86_64-pc-linux-gnu"
export HOST BUILD

# --- pins -------------------------------------------------------------------

pin_line() { grep -E "^$1\|" "${PINS}"; }
pin_field() { pin_line "$1" | cut -d'|' -f"$2"; }

# --- fetch / unpack ---------------------------------------------------------

# fetch NAME -> prints the path of the verified tarball.
fetch() {
	local name="$1" source sha url dest
	source=$(pin_field "$name" 3)
	sha=$(pin_field "$name" 4)
	url=$(pin_field "$name" 5)
	dest="${DL}/${source}"
	if [ ! -f "${dest}" ]; then
		echo ">> fetching ${source}" >&2
		mkdir -p "${DL}"
		# Same guards as build.sh's fetch: fail fast on a dead or
		# stalled mirror instead of hanging the layer.
		curl -fL --retry 3 --connect-timeout 20 \
			--speed-limit 1024 --speed-time 60 \
			-o "${dest}.part" "${url}"
		mv "${dest}.part" "${dest}"
	fi
	echo "${sha}  ${dest}" | sha256sum -c --quiet -
	echo "${dest}"
}

# unpack NAME -> extracts into $WORK/NAME and prints that directory.
unpack() {
	local name="$1" tar
	tar=$(fetch "$name" | tail -n1)
	rm -rf "${WORK:?}/${name}"
	mkdir -p "${WORK}/${name}"
	tar -xf "${tar}" -C "${WORK}/${name}" --strip-components=1
	echo "${WORK}/${name}"
}

# --- build-system drivers ---------------------------------------------------

# autotools_build NAME [configure args...]
autotools_build() {
	local name="$1"; shift
	local d
	d=$(unpack "$name" | tail -n1)
	(
		cd "${d}"
		./configure \
			--host="${HOST}" --build="${BUILD}" \
			--prefix=/usr --sysconfdir=/etc --localstatedir=/var \
			"$@"
		make -j"$(nproc)"
		make DESTDIR="${SYSROOT}" install
	)
}

# meson_build NAME [meson args...]
meson_build() {
	local name="$1"; shift
	local d
	d=$(unpack "$name" | tail -n1)
	(
		cd "${d}"
		meson setup _build --cross-file "${SCRATCH}/lib/cross.ini" \
			--prefix=/usr --sysconfdir=/etc --localstatedir=/var \
			"$@"
		ninja -C _build
		DESTDIR="${SYSROOT}" ninja -C _build install
	)
}

# host_autotools_build NAME [configure args...]
# For tools that run on the builder (genimage), not in the image: native, no
# sysroot, installed under /opt/host. The sysroot flags common.sh exports are
# cleared so it links against the builder's own libc, not the image's.
host_autotools_build() {
	local name="$1"; shift
	local d
	d=$(unpack "$name" | tail -n1)
	(
		cd "${d}"
		env -u PKG_CONFIG_SYSROOT_DIR -u PKG_CONFIG_LIBDIR \
			CFLAGS= CXXFLAGS= CPPFLAGS= LDFLAGS= \
			./configure --prefix=/opt/host "$@"
		make -j"$(nproc)"
		make install
	)
}
