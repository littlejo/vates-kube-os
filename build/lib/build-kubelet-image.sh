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
LAUNCHER="${LAUNCHER:-/build/vates-bin/vates-launcher}"
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
# The kubelet's userspace does not need the console's stack, so the cairo/pango/
# harfbuzz/glib family and every static/dev artifact are left out -- copied whole
# it would be ~190 MB of which ~95 MB is harfbuzz alone.
cp -a "${SYSROOT}/lib/." "${stage}/lib/"
cp -a "${SYSROOT}/lib64/." "${stage}/lib64/"
cp -a "${SYSROOT}/usr/lib/." "${stage}/usr/lib/"
[ -d "${SYSROOT}/usr/lib64" ] && cp -a "${SYSROOT}/usr/lib64/." "${stage}/usr/lib64/"
find "${stage}/usr/lib" -maxdepth 1 \( -name '*.a' -o -name '*.la' \) -delete
rm -rf "${stage}/usr/lib/pkgconfig" "${stage}/usr/share/pkgconfig"
for pat in 'libharfbuzz*' 'libcairo*' 'libpango*' 'libpixman*' 'libgio-*' \
	'libglib-*' 'libgobject-*' 'libgmodule-*' 'libfontconfig*' 'libfreetype*' \
	'libpng*' 'libfribidi*' 'libdrm*' 'libexpat*' 'libffi*' 'libpcre2*' 'libpopt*'; do
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
