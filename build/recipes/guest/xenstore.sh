# libxenstore (and the two tiny libs it needs: libxentoolcore, libxentoollog),
# from the Xen source. This is what the Rust xen-guest-agent links to reach
# XenStore.
#
# We do NOT run Xen's configure: it insists on iasl and a full tools build for a
# library that is, in the end, three C files. The build below is the whole of it,
# the same way the project rewrites a recipe rather than borrowing a downstream
# patch. The public headers are included as <xen/...>, so reproduce the symlink
# the Xen build makes (tools/include/xen -> the public headers).
#
# SHARED, not static: libxenstore is LGPL-2.1+, and the agent (AGPL) links it at
# runtime like any distro library. The .so files go to /usr/lib, the loader's
# default path, so xenstore-rs's dlopen("libxenstore.so.4") finds them.
#
# The .pc points at /usr/lib/vates, which holds *only* dev symlinks: pkg-config
# then puts that directory on the link line without dragging in /usr/lib's
# libc.so linker script -- whose absolute paths only resolve under --sysroot and
# otherwise break a native Rust link.
build() {
	local d inc objs ver
	d=$(unpack xen | tail -n1)
	ver=$(pin_field xen 2)
	ln -sfn "${d}/xen/include/public" "${d}/tools/include/xen"
	inc="${d}/tools/include"
	objs=$(mktemp -d)
	local cf="${CFLAGS} -fPIC -I${inc} -DUSE_PTHREAD -DUSE_DLSYM -D_GNU_SOURCE"
	cf="${cf} -DXEN_RUN_STORED=\"/var/run/xenstored\""
	gcc ${cf} -c "${d}/tools/libs/store/xs.c"                 -o "${objs}/xs.o"
	gcc ${cf} -c "${d}/tools/libs/toolcore/handlereg.c"       -o "${objs}/handlereg.o"
	gcc ${cf} -c "${d}/tools/libs/toollog/xtl_core.c"         -o "${objs}/xtl_core.o"
	gcc ${cf} -c "${d}/tools/libs/toollog/xtl_logger_stdio.c" -o "${objs}/xtl_logger_stdio.o"

	# Shared libraries, against our glibc, with the upstream sonames.
	gcc ${CFLAGS} -shared -Wl,-soname,libxentoollog.so.1 \
		-o "${objs}/libxentoollog.so.1.0" "${objs}/xtl_core.o" "${objs}/xtl_logger_stdio.o"
	gcc ${CFLAGS} -shared -Wl,-soname,libxentoolcore.so.1 \
		-o "${objs}/libxentoolcore.so.1.0" "${objs}/handlereg.o"
	gcc ${CFLAGS} -shared -Wl,-soname,libxenstore.so.4 \
		-o "${objs}/libxenstore.so.4.1" "${objs}/xs.o" "${objs}/libxentoolcore.so.1.0"

	install -d "${SYSROOT}/usr/lib" "${SYSROOT}/usr/lib/vates" \
		"${SYSROOT}/usr/include/xen" "${SYSROOT}/usr/lib/pkgconfig"
	install -m 0755 "${objs}/libxenstore.so.4.1"    "${SYSROOT}/usr/lib/libxenstore.so.4.1"
	install -m 0755 "${objs}/libxentoolcore.so.1.0" "${SYSROOT}/usr/lib/libxentoolcore.so.1.0"
	install -m 0755 "${objs}/libxentoollog.so.1.0"  "${SYSROOT}/usr/lib/libxentoollog.so.1.0"
	ln -sf libxenstore.so.4.1    "${SYSROOT}/usr/lib/libxenstore.so.4"
	ln -sf libxentoolcore.so.1.0 "${SYSROOT}/usr/lib/libxentoolcore.so.1"
	ln -sf libxentoollog.so.1.0  "${SYSROOT}/usr/lib/libxentoollog.so.1"
	ln -sf libxenstore.so.4    "${SYSROOT}/usr/lib/libxenstore.so"
	ln -sf libxentoolcore.so.1 "${SYSROOT}/usr/lib/libxentoolcore.so"
	ln -sf ../libxenstore.so    "${SYSROOT}/usr/lib/vates/libxenstore.so"
	ln -sf ../libxentoolcore.so "${SYSROOT}/usr/lib/vates/libxentoolcore.so"

	cp -a "${d}/xen/include/public/." "${SYSROOT}/usr/include/xen/"
	install -m 0644 "${d}/tools/include/xenstore.h" \
		"${d}/tools/include/xenstore_lib.h" "${SYSROOT}/usr/include/"

	# License (LGPL-2.1-or-later): the text, where a recipient expects it.
	install -d "${SYSROOT}/usr/share/licenses/libxenstore"
	install -m 0644 "${d}/COPYING" "${SYSROOT}/usr/share/licenses/libxenstore/COPYING"
	install -m 0644 "${d}/LICENSES/LGPL-2.1" \
		"${SYSROOT}/usr/share/licenses/libxenstore/LGPL-2.1"

	cat > "${SYSROOT}/usr/lib/pkgconfig/xenstore.pc" <<EOF
prefix=/usr
includedir=\${prefix}/include
libdir=\${prefix}/lib/vates
Name: xenstore
Description: Xenstore client library (Vates Kube OS)
Version: ${ver}
Libs: -L\${libdir} -lxenstore -lxentoolcore
Libs.private: -lpthread -ldl
Cflags: -I\${includedir}
EOF
	rm -rf "${objs}"
}
