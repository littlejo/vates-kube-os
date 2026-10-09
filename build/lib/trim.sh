#!/bin/bash
# Remove binaries the node never calls, after the rootfs is assembled.
#
# cairo + pango pull in harfbuzz, fontconfig, glib and their command-line tools.
# They are used to BUILD the console's libraries, not to run it: the console
# draws through cairo/libdrm and never execs hb-*, fc-*, pango-* or gio. This
# trims them, and every other real binary the node never calls -- the whole tool
# sets of util-linux, iproute2, fdisk and pango (DEAD_BINS below). The libraries
# themselves stay: only programs go.
set -euo pipefail

: "${SYSROOT:=/sysroot}"
TARGET_DIR="${SYSROOT}"
BIN="${TARGET_DIR}/usr/bin"

# Console build-time tools.
rm -f "${BIN}"/hb-* "${BIN}"/fc-* "${BIN}"/pango-*
# glib/gi tools.
rm -f "${BIN}/gio" "${BIN}/gio-querymodules" "${BIN}/gsettings" \
	"${BIN}/gapplication" "${BIN}/gdbus" "${BIN}/gresource" \
	"${BIN}"/gi-inspect-typelib "${BIN}"/gi-decompile-typelib \
	"${BIN}"/gi-compile-repository
# pcre2 command-line tools.
rm -f "${BIN}/pcre2test" "${BIN}/pcre2grep"
# glibc's own tools and locale data -- the image runs UTC and has no locale.
# ldconfig stays: the assembly runs it to build /etc/ld.so.cache.
rm -f "${BIN}/gencat" "${BIN}/getconf" "${BIN}/getent" "${BIN}/iconv" \
	"${BIN}/locale" "${BIN}/localedef" "${BIN}/makedb" "${BIN}/pldd" \
	"${BIN}/sprof" "${BIN}/zdump" "${BIN}/pcprofiledump"
rm -f "${TARGET_DIR}/usr/sbin/iconvconfig" "${TARGET_DIR}/usr/sbin/nscd" \
	"${TARGET_DIR}/sbin/sln"
rm -rf "${TARGET_DIR}/usr/libexec/getconf"
# glibc's locale source and compiled locales: the image is C-locale only and runs
# UTC. This is ~17 MB of i18n source data nothing on the node reads.
rm -rf "${TARGET_DIR}/usr/share/i18n" "${TARGET_DIR}/usr/share/locale" \
	"${TARGET_DIR}/usr/lib/locale"
# glib's build-time tools; the console links the library, never execs these.
rm -f "${BIN}/glib-compile-resources" "${BIN}/glib-compile-schemas" \
	"${BIN}/gobject-query" "${BIN}/gtester" \
	"${TARGET_DIR}/usr/libexec/gio-launch-desktop"
# iproute2's extra tools; `ip` alone is what the node calls.
for b in dcb devlink dpll netshaper rdma tipc vdpa; do
	rm -f "${TARGET_DIR}/sbin/$b" "${TARGET_DIR}/usr/sbin/$b" "${TARGET_DIR}/usr/bin/$b"
done
# util-linux odds and ends the node does not call; the load-bearing ones --
# mount, umount, blkid -- stay, and DEAD_BINS below sweeps every directory.
rm -f "${BIN}/lscpu" "${BIN}/lsns" "${BIN}/lslocks" "${BIN}/lsirq" \
	"${BIN}/lsipc" "${BIN}/lsclocks" "${BIN}/fincore" "${BIN}/hexdump" \
	"${BIN}/script" "${BIN}/scriptreplay" "${BIN}/scriptlive" \
	"${BIN}/namei" "${BIN}/rev" "${BIN}/uuidgen" "${BIN}/uuidparse" \
	"${BIN}/setsid" "${BIN}/setpgid" "${BIN}/setarch" "${BIN}/rtcwake" \
	"${BIN}/readprofile" "${BIN}/ldattach" "${BIN}/mcookie" \
	"${BIN}/whereis" "${BIN}/look" "${BIN}/isosize" "${BIN}/renice" \
	"${BIN}/prlimit" "${BIN}/getopt"

# The real (non-symlink) binaries nothing on the node calls: `vates` and the
# container runtime are the only code that runs, and every name below was checked
# against the exec calls in cmd/ and internal/ and the embedded firstboot
# templates. util-linux, iproute2, fdisk and pango ship
# their whole tool set, so they are removed here, from every directory.
#
# Kept, and the reason: blkid (config drive, growing /var), mount/umount (config
# drive), ip (lo/eth0, CNI), sgdisk and resize2fs (growing
# /var). The xtables multi-calls stay too: the CNI plugins exec one
# of them as `iptables`.
DEAD_BINS='
	blkdiscard blkpr blkzone blockdev bridge bits chcpu choom col colcrt
	colrm column ctrlaltdel dmesg enosys exch fadvise fdisk findfs findmnt
	flock fribidi fsfreeze fstrim genl ifstat ldattach lnstat lsblk mkswap
	mountpoint nstat pipesz pivot_root readprofile rtacct rtcwake rtmon
	scmp_sys_resolver sfdisk ss swaplabel swapoff swapon tc
'
for d in "${TARGET_DIR}/bin" "${TARGET_DIR}/sbin" \
	"${TARGET_DIR}/usr/bin" "${TARGET_DIR}/usr/sbin"; do
	for b in ${DEAD_BINS}; do
		rm -f "$d/$b"
	done
done

# Guard: the config drive is located by volume label -- blkid -L cidata
# (internal/configdrive) -- and /var is grown through the same lookup
# (internal/sysinit/grow.go). busybox's blkid applet does NOT accept -L and lands
# as a SYMLINK at /sbin/blkid. A real util-linux blkid must be there instead, or
# the node boots and never finds its config drive -- so fail the build.
if [ -L "${TARGET_DIR}/sbin/blkid" ] || [ ! -x "${TARGET_DIR}/sbin/blkid" ]; then
	echo "ERROR: /sbin/blkid is not a real util-linux blkid (busybox applet?)." >&2
	echo "  The config drive and /var are found with 'blkid -L', which the applet" >&2
	echo "  does not support." >&2
	exit 1
fi

# Links a removal leaves behind (relative, so they point at nothing once the
# target is gone).
rm -f "${TARGET_DIR}/sbin/e2label" "${TARGET_DIR}/sbin/e2mmpstatus"
rm -f "${TARGET_DIR}/usr/bin/i386" "${TARGET_DIR}/usr/bin/linux32" \
	"${TARGET_DIR}/usr/bin/linux64" "${TARGET_DIR}/usr/bin/uname26" \
	"${TARGET_DIR}/usr/bin/x86_64"

# Shell tools for a prompt that does not exist.
rm -f "${TARGET_DIR}/bin/compile_et" "${TARGET_DIR}/bin/mk_cmds" \
	"${TARGET_DIR}/sbin/routel" \
	"${TARGET_DIR}/usr/sbin/iptables-apply" "${TARGET_DIR}/usr/sbin/ip6tables-apply"

# e2fsprogs ships a whole filesystem-recovery suite; the image uses exactly one
# program from it: resize2fs, to grow /var into the space the hypervisor adds.
for d in "${TARGET_DIR}/bin" "${TARGET_DIR}/sbin" "${TARGET_DIR}/usr/bin" "${TARGET_DIR}/usr/sbin"; do
	rm -f "$d"/mke2fs "$d"/e2fsck "$d"/fsck "$d"/fsck.ext2 "$d"/fsck.ext3 "$d"/fsck.ext4 \
		"$d"/mkfs "$d"/mkfs.ext2 "$d"/mkfs.ext3 "$d"/mkfs.ext4 \
		"$d"/tune2fs "$d"/dumpe2fs "$d"/badblocks "$d"/filefrag "$d"/e2freefrag \
		"$d"/e2undo "$d"/e4crypt "$d"/chattr "$d"/lsattr "$d"/logsave "$d"/mklost+found
done

# kbd: one font and setfont, nothing else. The text dashboard's frames are drawn
# with box-drawing characters, and the kernel's built-in VGA font carries none;
# PID 1 loads eurlatgr with setfont before the console (internal/console/tty).
CONSOLE_FONTS="${TARGET_DIR}/usr/share/consolefonts"
if [ -d "${CONSOLE_FONTS}" ]; then
	find "${CONSOLE_FONTS}" -type f ! -name 'eurlatgr.psf*' -delete
fi
# Keep the one font uncompressed: setfont must read it without zlib.
if [ -f "${CONSOLE_FONTS}/eurlatgr.psfu.gz" ]; then
	gzip -dc "${CONSOLE_FONTS}/eurlatgr.psfu.gz" >"${CONSOLE_FONTS}/eurlatgr.psfu"
	rm -f "${CONSOLE_FONTS}/eurlatgr.psfu.gz"
fi
rm -rf "${TARGET_DIR}/usr/share/consolefonts/partialfonts" \
	"${TARGET_DIR}/usr/share/keymaps" \
	"${TARGET_DIR}/usr/share/consoletrans" \
	"${TARGET_DIR}/usr/share/unimaps"
for b in chvt deallocvt dumpkeys fgconsole getkeycodes kbd_mode kbdinfo \
	kbdrate loadkeys loadunimap mapscrn openvt psfaddtable psfgettable \
	psfstriptable psfxtable resizecons setkeycodes setleds setmetamode \
	setvtrgb showconsolefont showkey unicode_start unicode_stop; do
	rm -f "${TARGET_DIR}/bin/$b" "${TARGET_DIR}/sbin/$b" \
		"${TARGET_DIR}/usr/bin/$b" "${TARGET_DIR}/usr/sbin/$b"
done

# busybox's ntpd is the node's NTP client. Its configuration is written to the
# writable /var, so the image carries only the link.
ln -sfn /var/lib/vates/etc/ntp.conf "${TARGET_DIR}/etc/ntp.conf"

# /opt/cni/bin and /etc/cni/net.d are overlay MOUNT POINTS: the plugins live in
# /usr/share/vates/cni, the lower layer, and PID 1 mounts the overlay over them
# (internal/sysinit/cni.go). The directories themselves stay.
rm -rf "${TARGET_DIR}/opt/cni/bin"/* "${TARGET_DIR}/etc/cni/net.d"/*

# Strip every ELF binary and shared library. The components ship with debug info,
# which is most of their size, and a distribution's packages are stripped for you
# -- nothing here does that, so without this the image is far larger than it
# needs to be (measured: the console libraries alone were the bulk of it).
STRIP="$(command -v strip || true)"
if [ -n "${STRIP}" ]; then
	find "${TARGET_DIR}" -xdev -type f \
		\( -perm -u+x -o -name '*.so' -o -name '*.so.*' \) \
		! -path "${TARGET_DIR}/lib/modules/*" \
		! -path "${TARGET_DIR}/usr/lib/modules/*" -print0 2>/dev/null |
		xargs -0 file 2>/dev/null | grep -a 'ELF' | cut -d: -f1 |
		while IFS= read -r f; do
			"${STRIP}" --strip-unneeded "$f" 2>/dev/null || true
		done
	echo "  stripped ELF binaries and libraries"
fi

echo "  trimmed console and utility tools"

# No login, ever. There is no sshd, and the console is the vates dashboard, not a
# prompt -- so the paths that could present a password prompt are taken away: the
# busybox getty and login applets, and the root account itself.
rm -f "${TARGET_DIR}/bin/login" "${TARGET_DIR}/sbin/getty"
if [ -f "${TARGET_DIR}/etc/shadow" ]; then
	sed -i 's|^root:[^:]*:|root:!:|' "${TARGET_DIR}/etc/shadow"
fi
