#!/bin/bash
# Assemble the bootable disk: from /sysroot (the built userspace), the kernel
# (bzImage + modules, copied in from the kernel stage) and the image's own
# content (context/), produce rootfs.ext4 and the ESP, then lay down the GPT disk
# with genimage.
#
# Layout and PARTUUIDs are in genimage.cfg; the boot entries here must name the
# same PARTUUIDs. A/B is real: rootA active (sort-key 0), rootB reserved
# (sort-key 1), /var last. See docs/IMMUTABILITY.md.
set -euo pipefail

: "${SCRATCH:=/build}"
# shellcheck source=/dev/null
source "${SCRATCH}/lib/common.sh"
CTX="${SCRATCH}/context"
OUT="${OUT:-/out}"
GENIMAGE="${GENIMAGE:-/opt/host/bin/genimage}"
# The vates binaries and the Xen guest agent are already in ${SYSROOT}, installed
# by the recipes (the `vates` and `guest` stages, copied in by `disk`); assembly
# does not copy binaries.
KUBELET_TAR="${KUBELET_TAR:-/build/kubelet.tar}"

ROOT_A_PARTUUID="d3e1f3a0-1b2c-4d5e-8f90-a1b2c3d4e5f6"
ROOT_B_PARTUUID="d3e1f3a0-1b2c-4d5e-8f90-a1b2c3d4e5f7"
COMMON_OPTS="rootwait ro rootfstype=ext4 panic=10 init=/usr/local/bin/vates-sysinit console=tty0 console=ttyS0 console=hvc0 quiet loglevel=4"

mkdir -p "${OUT}"

echo "=== [assemble] skeleton ==="
# What a distribution's base filesystem provides and PID 1 needs before it can
# mount anything: the mount points themselves. `mount proc /proc` fails ENOENT
# without /proc, and Pid1 returns that error and exits, which is a kernel panic
# because it is PID 1.
install -d -m 0755 "${SYSROOT}/proc" "${SYSROOT}/sys" "${SYSROOT}/sys/fs/bpf" \
	"${SYSROOT}/dev" "${SYSROOT}/dev/pts" "${SYSROOT}/dev/shm" \
	"${SYSROOT}/run" "${SYSROOT}/mnt" "${SYSROOT}/media" \
	"${SYSROOT}/opt" "${SYSROOT}/root" "${SYSROOT}/srv" "${SYSROOT}/home" \
	"${SYSROOT}/var/cache" "${SYSROOT}/var/lib" "${SYSROOT}/var/log" "${SYSROOT}/var/tmp"
install -d -m 1777 "${SYSROOT}/tmp" "${SYSROOT}/var/tmp"
# Minimal identity files: containerd, the kubelet and a shell all read them.
if [ ! -f "${SYSROOT}/etc/passwd" ]; then
	cat >"${SYSROOT}/etc/passwd" <<'EOF'
root:x:0:0:root:/root:/bin/sh
EOF
fi
if [ ! -f "${SYSROOT}/etc/group" ]; then
	cat >"${SYSROOT}/etc/group" <<'EOF'
root:x:0:
EOF
fi
[ -f "${SYSROOT}/etc/hostname" ] || echo vates >"${SYSROOT}/etc/hostname"
if [ ! -f "${SYSROOT}/etc/hosts" ]; then
	printf '127.0.0.1\tlocalhost\n::1\tlocalhost\n' >"${SYSROOT}/etc/hosts"
fi
# resolv.conf is a symlink onto /run: the root is read-only and udhcpc writes it.
[ -e "${SYSROOT}/etc/resolv.conf" ] || ln -s ../run/resolv.conf "${SYSROOT}/etc/resolv.conf"
chmod 0755 "${SYSROOT}/usr/share/udhcpc/default.script" 2>/dev/null || true

echo "=== [assemble] the vates faces ==="
# The binaries themselves are already in ${SYSROOT}/usr/local/bin (installed by
# the recipes); assemble only adds the argv[0] faces, which are assembly.
ln -sf vates-sysinit "${SYSROOT}/usr/local/bin/vates-init"
ln -sf vates-sysinit "${SYSROOT}/usr/local/bin/vates-splash"
ln -sf vates-console "${SYSROOT}/usr/local/bin/vates-dashboard"

echo "=== [assemble] overlay, /etc, assets, kubelet image ==="
cp -a "${CTX}/overlay/." "${SYSROOT}/"
install -d "${SYSROOT}/etc/modules-load.d" "${SYSROOT}/etc/sysctl.d" \
	"${SYSROOT}/usr/share/vates/cni/net.d" "${SYSROOT}/usr/share/vates/cni/bin" \
	"${SYSROOT}/opt/cni/bin" "${SYSROOT}/etc/cni/net.d"
[ -d "${CTX}/config/cni" ] && cp -a "${CTX}/config/cni/." "${SYSROOT}/usr/share/vates/cni/net.d/"
[ -d "${CTX}/config/modules-load.d" ] && cp -a "${CTX}/config/modules-load.d/." "${SYSROOT}/etc/modules-load.d/"
[ -d "${CTX}/config/sysctl.d" ] && cp -a "${CTX}/config/sysctl.d/." "${SYSROOT}/etc/sysctl.d/"
install -d "${SYSROOT}/usr/share/vates/assets"
[ -d "${CTX}/assets" ] && cp -a "${CTX}/assets/." "${SYSROOT}/usr/share/vates/assets/"
if [ -f "${KUBELET_TAR}" ]; then
	cp "${KUBELET_TAR}" "${SYSROOT}/usr/share/vates/kubelet.tar"
	echo "  kubelet image staged"
fi

echo "=== [assemble] iptables defaults to nftables ==="
# The legacy xtables need the kernel's ip_tables/nat tables, which this kernel
# does not carry; nftables it does. Point the iptables
# names at the nft multi-call. Without it flannel and kube-proxy fail with
# "can't initialize iptables table `nat'" and no pod ever gets networking.
if [ -x "${SYSROOT}/usr/sbin/xtables-nft-multi" ]; then
	for a in iptables iptables-restore iptables-save \
		ip6tables ip6tables-restore ip6tables-save; do
		ln -sf xtables-nft-multi "${SYSROOT}/usr/sbin/${a}"
	done
fi

echo "=== [assemble] trim ==="
bash "${SCRATCH}/lib/trim.sh"

echo "=== [assemble] depmod ==="
KVER="$(find "${SYSROOT}/lib/modules" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null | head -1 || true)"
if [ -n "${KVER}" ]; then
	for dp in "${SYSROOT}/usr/sbin/depmod" "${SYSROOT}/sbin/depmod" \
		"${SYSROOT}/usr/bin/depmod" "${SYSROOT}/bin/depmod"; do
		if [ -x "${dp}" ]; then
			PATH="${SYSROOT}/usr/sbin:${SYSROOT}/sbin:${SYSROOT}/usr/bin:${SYSROOT}/bin:${PATH}" \
				"${dp}" -b "${SYSROOT}" "${KVER}" && echo "  depmod ${KVER} ok (${dp#"${SYSROOT}"})"
			break
		fi
	done
fi

echo "=== [assemble] remove development files ==="
rm -rf "${SYSROOT}/usr/include" "${SYSROOT}/usr/share/man" "${SYSROOT}/usr/share/doc" \
	"${SYSROOT}/usr/share/info" "${SYSROOT}/usr/share/aclocal" \
	"${SYSROOT}/usr/share/gettext" "${SYSROOT}/usr/share/locale" \
	"${SYSROOT}/usr/share/gtk-doc" "${SYSROOT}/usr/share/gdb" \
	"${SYSROOT}/usr/lib/pkgconfig" "${SYSROOT}/usr/share/pkgconfig" \
	"${SYSROOT}/usr/lib/cmake" "${SYSROOT}/usr/lib64/pkgconfig"
find "${SYSROOT}/usr/lib" "${SYSROOT}/usr/lib64" -maxdepth 1 -name '*.a' -delete 2>/dev/null || true
find "${SYSROOT}" -name '*.la' -delete 2>/dev/null || true

echo "=== [assemble] ldconfig ==="
# Build /etc/ld.so.cache with the image's OWN ldconfig (our glibc's). Without it
# the loader does not search /lib, and every binary linking a library util-linux
# installed there (libblkid, libmount, ...) dies with "cannot open shared object
# file" -- which is how blkid exits 127, /var never mounts and the config drive
# is never found. The cache from glibc's own install is stale: it predates every
# other library.
cat >"${SYSROOT}/etc/ld.so.conf" <<'EOF'
/lib
/usr/lib
/lib64
/usr/lib64
EOF
"${SYSROOT}/lib64/ld-linux-x86-64.so.2" "${SYSROOT}/sbin/ldconfig" -r "${SYSROOT}"
ls -l "${SYSROOT}/etc/ld.so.cache"

echo "=== [assemble] font cache ==="
# Best effort: fontconfig scans /usr/share/fonts at runtime if the cache is
# absent, but building it here makes the first console frame fast.
if [ -x "${SYSROOT}/usr/bin/fc-cache" ]; then
	"${SYSROOT}/lib64/ld-linux-x86-64.so.2" \
		--library-path "${SYSROOT}/lib64:${SYSROOT}/lib:${SYSROOT}/usr/lib" \
		"${SYSROOT}/usr/bin/fc-cache" -f -y "${SYSROOT}" >/dev/null 2>&1 || true
fi
ls "${SYSROOT}/usr/share/fonts" 2>/dev/null

echo "=== [assemble] rootfs.ext4 ==="
truncate -s 2G "${OUT}/rootfs.ext4"
mke2fs -q -t ext4 -L vates-root -d "${SYSROOT}" -F "${OUT}/rootfs.ext4"

echo "=== [assemble] the ESP ==="
ESP="${SCRATCH}/esp"
rm -rf "${ESP}"
install -d "${ESP}/EFI/BOOT" "${ESP}/loader/entries"
install -m 0644 "${SYSROOT}/boot/systemd-bootx64.efi" "${ESP}/EFI/BOOT/BOOTX64.EFI"
install -m 0644 "${SYSROOT}/boot/bzImage" "${ESP}/bzImage"
printf 'timeout 3\n' >"${ESP}/loader/loader.conf"
cat >"${ESP}/loader/entries/vates-a.conf" <<EOF
title   Vates Kube OS (A)
sort-key 0
linux   /bzImage
options root=PARTUUID=${ROOT_A_PARTUUID} ${COMMON_OPTS}
EOF
cat >"${ESP}/loader/entries/vates-b.conf" <<EOF
title   Vates Kube OS (B)
sort-key 1
linux   /bzImage
options root=PARTUUID=${ROOT_B_PARTUUID} ${COMMON_OPTS}
EOF
truncate -s 128M "${OUT}/boot.vfat"
mkfs.vfat -F 32 -n ESP "${OUT}/boot.vfat" >/dev/null
(cd "${ESP}" && mcopy -s -i "${OUT}/boot.vfat" EFI loader bzImage ::)

echo "=== [assemble] genimage ==="
"${GENIMAGE}" --rootpath "${SYSROOT}" --tmppath /tmp/genimage \
	--config "${SCRATCH}/genimage.cfg" \
	--inputpath "${OUT}" --outputpath "${OUT}"
ls -lh "${OUT}/vates.img"
