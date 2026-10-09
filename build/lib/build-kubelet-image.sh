#!/bin/bash
# Build the kubelet OCI image as a docker-archive, from the image's own /sysroot.
#
# The image is FROM scratch: the vates launcher (under the names kubelet, kubeadm,
# kubectl, mounter) and the small userspace it shells out to, with the shared
# libraries they need, copied from /sysroot so the glibc in the container is
# exactly the node's. No Kubernetes binary inside: the launcher fetches the one
# vates-node.yaml asks for.
#
# ctr's archive format is manifest.json + a config json + one layer tar, each
# named by digest; it is built by hand here so no nested container builder (and
# no privileges) is needed.
set -euo pipefail

: "${SYSROOT:=/sysroot}"
LAUNCHER="${LAUNCHER:-${SYSROOT}/usr/local/bin/vates-launcher}"
OUT="${OUT:-/build/kubelet.tar}"
REF="localhost/vates/kubelet:current"

stage="$(mktemp -d)"
arc="$(mktemp -d)"
trap 'rm -rf "${stage}" "${arc}"' EXIT

mkdir -p "${stage}"/bin "${stage}"/sbin "${stage}"/etc/ssl/certs \
	"${stage}"/etc/kubernetes "${stage}"/etc/kubelet "${stage}"/proc \
	"${stage}"/sys/fs/cgroup "${stage}"/dev "${stage}"/run/containerd \
	"${stage}"/tmp "${stage}"/usr/bin "${stage}"/usr/sbin "${stage}"/usr/local/bin \
	"${stage}"/usr/lib "${stage}"/var/lib/kubelet "${stage}"/var/lib/vates/kubernetes \
	"${stage}"/var/lib/containerd "${stage}"/var/log
# The loader and libc live in /lib64 (glibc's slibdir), and the other libraries
# in /lib and /usr/lib. They must be copied as real directories: the kubelet
# binary is dynamically linked ("interpreter /lib64/ld-linux-x86-64.so.2"), so a
# /lib64 -> lib symlink leaves it with no interpreter and exec fails ENOENT.
mkdir -p "${stage}/lib64" "${stage}/usr/lib64"

# The node's own libraries (our glibc included), so the container runs on it.
#
# Only what the binaries in the image actually load. The launcher, kubelet,
# kubeadm and kubectl need glibc (measured: the published kubelet links libc,
# libresolv and libpthread; kubeadm and kubectl are static), and the small
# userspace needs glibc plus libmnl/libnftnl/libxtables. Copied whole from
# /sysroot the rootfs was 39 MB; the rules below bring it to ~20 MB. The archive
# is imported into containerd on every node at first boot, so the saving is paid
# on every machine.
cp -a "${SYSROOT}/lib/." "${stage}/lib/"
cp -a "${SYSROOT}/lib64/." "${stage}/lib64/"
cp -a "${SYSROOT}/usr/lib/." "${stage}/usr/lib/"
[ -d "${SYSROOT}/usr/lib64" ] && cp -a "${SYSROOT}/usr/lib64/." "${stage}/usr/lib64/"
# 1. the kernel modules (7 MB): the host's, and the container loads none. A
#    kubeadm container inspects the host's tree through a bind mount, not this.
rm -rf "${stage}/lib/modules"
# 2. the static and development artifacts, and the two sub-trees the patterns
#    below do not reach: gconv (8 MB of charset tables) and cairo's trace helper.
find "${stage}/usr/lib" -maxdepth 1 \( -name '*.a' -o -name '*.la' \) -delete
rm -rf "${stage}/usr/lib/pkgconfig" "${stage}/usr/share/pkgconfig" \
	"${stage}/usr/lib/gconv" "${stage}/usr/lib/cairo"
# 3. the console's stack (copied whole ~190 MB, ~95 MB of it harfbuzz), the C++
#    runtime (libstdc++) and GObject introspection: nothing in the kubelet
#    container loads them.
for pat in 'libharfbuzz*' 'libcairo*' 'libpango*' 'libpixman*' 'libgio-*' \
	'libglib-*' 'libgobject-*' 'libgmodule-*' 'libgthread*' 'libgirepository*' \
	'libfontconfig*' 'libfreetype*' 'libstdc++*' 'libpng*' 'libfribidi*' \
	'libdrm*' 'libexpat*' 'libffi*' 'libpcre2*' 'libpopt*'; do
	# The pattern is a glob on purpose: it must expand, not be a literal name.
	# shellcheck disable=SC2086
	rm -f "${stage}"/usr/lib/${pat}
done

# The userspace the kubelet shells out to.
for b in iptables iptables-nft iptables-save iptables-restore \
	ip6tables ip6tables-nft ip6tables-save ip6tables-restore \
	xtables-nft-multi xtables-legacy-multi; do
	[ -e "${SYSROOT}/usr/sbin/${b}" ] && cp -a "${SYSROOT}/usr/sbin/${b}" "${stage}/usr/sbin/${b}"
done
# The container's iptables must be the nft one too (see assemble.sh): the legacy
# tables are not in this kernel.
if [ -x "${stage}/usr/sbin/xtables-nft-multi" ]; then
	for a in iptables iptables-restore iptables-save ip6tables ip6tables-restore ip6tables-save; do
		ln -sf xtables-nft-multi "${stage}/usr/sbin/${a}"
	done
fi
cp -a "${SYSROOT}/sbin/ip" "${stage}/sbin/ip"
cp -a "${SYSROOT}/bin/mount" "${SYSROOT}/bin/umount" "${stage}/bin/"
cp -a "${SYSROOT}/bin/uname" "${SYSROOT}/bin/touch" "${stage}/bin/"
cp -a "${SYSROOT}/bin/busybox" "${stage}/bin/busybox"
ln -sf busybox "${stage}/bin/sh"

cp -a "${SYSROOT}/etc/ssl/certs/ca-certificates.crt" "${stage}/etc/ssl/certs/"
for f in nsswitch.conf passwd group; do
	[ -e "${SYSROOT}/etc/${f}" ] && cp -a "${SYSROOT}/etc/${f}" "${stage}/etc/${f}"
done

install -D -m 0755 "${LAUNCHER}" "${stage}/usr/local/bin/vates-launcher"
for n in kubelet kubeadm kubectl mounter; do
	ln -sf /usr/local/bin/vates-launcher "${stage}/usr/local/bin/${n}"
done

# Guard: every shared object the container's own binaries load must be present,
# so an over-eager rule above fails the build here rather than on a node. It
# walks the libraries too, which is what catches a leftover whose dependency was
# pruned (libgthread without libglib, for instance). The kubelet itself is
# fetched at run time and is not covered; it is the reason the glibc copy cannot
# be trimmed further -- the published binary is dynamic.
if command -v readelf >/dev/null 2>&1; then
	missing="$(
		for exe in $(find "${stage}"/bin "${stage}"/sbin "${stage}"/usr/bin \
			"${stage}"/usr/sbin "${stage}"/usr/local/bin "${stage}"/lib \
			"${stage}"/lib64 "${stage}"/usr/lib "${stage}"/usr/lib64 \
			-type f \( -perm -u+x -o -name '*.so*' \) 2>/dev/null); do
			for lib in $(readelf -d "${exe}" 2>/dev/null | awk '/NEEDED/{print $NF}' | tr -d '[]'); do
				find "${stage}/lib" "${stage}/lib64" "${stage}/usr/lib" "${stage}/usr/lib64" \
					-name "${lib}" -print -quit 2>/dev/null | grep -q . ||
					echo "${lib} (needed by ${exe})"
			done
		done
	)"
	if [ -n "${missing}" ]; then
		echo "kubelet image: shared libraries missing from the rootfs:" >&2
		echo "${missing}" >&2
		exit 1
	fi
fi

# The docker-archive: one layer, its config, and the manifest.
tar --numeric-owner -C "${stage}" -cf "${arc}/layer.tar" .
layer_digest=$(sha256sum "${arc}/layer.tar" | cut -d' ' -f1)

cat >"${arc}/config.json" <<EOF
{"created":"1970-01-01T00:00:00Z","architecture":"amd64","os":"linux","config":{},"rootfs":{"type":"layers","diff_ids":["sha256:${layer_digest}"]},"history":[{"created":"1970-01-01T00:00:00Z","created_by":"vates-scratch"}]}
EOF
config_digest=$(sha256sum "${arc}/config.json" | cut -d' ' -f1)

cat >"${arc}/manifest.json" <<EOF
[{"Config":"${config_digest}.json","RepoTags":["${REF}"],"Layers":["${layer_digest}.tar"]}]
EOF

mv "${arc}/config.json" "${arc}/${config_digest}.json"
mv "${arc}/layer.tar" "${arc}/${layer_digest}.tar"

tar -C "${arc}" -cf "${OUT}" manifest.json "${config_digest}.json" "${layer_digest}.tar"
echo "  kubelet image archive: ${OUT} ($(du -h "${OUT}" | cut -f1))"
