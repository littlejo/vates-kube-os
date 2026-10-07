#!/bin/bash
# Pass -1: own the C library.
#
# Builds, from GNU/upstream sources and with the seed gcc of the builder image:
#   1. the kernel UAPI headers (from the pinned linux source) into /sysroot;
#   2. glibc itself, into /sysroot.
# Then it seeds the compiler's own runtime (C++ headers, libgcc_s, libstdc++)
# into the sysroot -- that is compiler runtime, not the libc, and it is the one
# thing we deliberately borrow from the seed gcc.
#
# What ships in the image is exactly this glibc. Its version is our choice
# (pins.conf), not a toolchain vendor's. Nothing in the image comes from a
# prebuilt cross-toolchain.
#
# The self-test at the end links and runs a program against the sysroot, so a
# broken libc fails here, not three passes later.
set -euo pipefail

: "${SCRATCH:=/build}"
# shellcheck source=/dev/null
source "${SCRATCH}/lib/common.sh"

echo "=== [libc] kernel UAPI headers (linux $(pin_field linux 2)) ==="
kdir=$(unpack linux | tail -n1)
make -C "${kdir}" INSTALL_HDR_PATH="${SYSROOT}/usr" headers_install >/dev/null
echo "  installed $(find "${SYSROOT}/usr/include" -type f | wc -l) headers"

echo "=== [libc] glibc $(pin_field glibc 2) ==="
gdir=$(unpack glibc | tail -n1)
mkdir -p "${WORK}/glibc-build"
cd "${WORK}/glibc-build"
"${gdir}/configure" \
	--prefix=/usr \
	--libdir=/usr/lib \
	--host="${HOST}" --build="${BUILD}" \
	--with-headers="${SYSROOT}/usr/include" \
	--enable-bind-now \
	--disable-werror
make -j"$(nproc)"
make DESTDIR="${SYSROOT}" install

echo "=== [libc] seeding the compiler runtime into the sysroot ==="
# C++ standard headers (the seed g++ looks for them under the sysroot).
if [ -d /usr/include/c++ ]; then
	cp -a /usr/include/c++ "${SYSROOT}/usr/include/"
fi
# Runtime and link-time support objects the seed compiler owns: they are the
# compiler's, not the libc's, and the image needs the shared ones at runtime.
# gcc reports them as symlinks (libstdc++.so.6 -> libstdc++.so.6.0.36); copying
# the symlink alone leaves a dangling link and the loader fails, so install the
# real file under its versioned name and recreate the soname symlink.
for f in libgcc_s.so.1 libstdc++.so.6 libatomic.so.1; do
	src=$(gcc -print-file-name="${f}")
	[ -f "${src}" ] || continue
	real=$(readlink -f "${src}")
	cp -a "${real}" "${SYSROOT}/usr/lib/$(basename "${real}")"
	ln -sf "$(basename "${real}")" "${SYSROOT}/usr/lib/${f}"
done

echo "=== [libc] self-test: link and run against the sysroot ==="
printf '#include <stdio.h>\nint main(void){printf("libc ok\\n");return 0;}\n' > /tmp/hello.c
gcc --sysroot="${SYSROOT}" -o /tmp/hello /tmp/hello.c
"${SYSROOT}/lib64/ld-linux-x86-64.so.2" \
	--library-path "${SYSROOT}/lib64:${SYSROOT}/usr/lib" /tmp/hello

echo "=== [libc] sysroot layout ==="
find "${SYSROOT}/lib64" -mindepth 1 -maxdepth 1 -printf '%f\n' | head
