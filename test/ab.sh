#!/bin/bash
# Verify A/B: two roots, a switchover, and an automatic rollback.
#
#   test/ab.sh
#   IMAGE=.../vates.qcow2 test/ab.sh
#   KEEP=1 test/ab.sh
#
# The disk ships two boot entries, vates-a (rootA) and vates-b (rootB, empty for
# now). There is no `default` in loader.conf on purpose: systemd-boot sorts the
# entries -- by sort-key, with a "bad" entry (its boot counter run down) pushed
# last -- and boots the first. A switch flips the sort-keys and makes the target a
# TRIAL; if it does not come up, boot counting marks it bad and the other slot
# sorts first. The sort IS the selection, and the rollback.
#
# Each phase runs on a FRESH disk: a disk whose counter has already run down is
# not the same disk.
#
# Which slot booted is read off the disk: PID 1 records the root PARTUUID it was
# started with in /var/lib/vates/pid1.log, which persists across reboots.
set -euo pipefail

CONN="qemu:///system"
VMDIR="${VMDIR:-/var/tmp/vates-os}"
NAME="${NAME:-abtest}"
DISK_SIZE="${DISK_SIZE:-12G}"
TIMEOUT="${TIMEOUT:-150}"
IMAGE="${IMAGE:-${ROOT}/build/out/vates.qcow2}"
ROOT_A_UUID="d3e1f3a0-1b2c-4d5e-8f90-a1b2c3d4e5f6"
ROOT_B_UUID="d3e1f3a0-1b2c-4d5e-8f90-a1b2c3d4e5f7"

# shellcheck source=test/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ -f "${IMAGE}" ] || { echo "FATAL: no image at ${IMAGE}; run make image or set IMAGE=" >&2; exit 1; }

fail=0
DISK="${VMDIR}/${NAME}.qcow2"
NVRAM="${VMDIR}/${NAME}-efivars.fd"
XML="${VMDIR}/${NAME}.xml"

# esp_write uploads content with REAL newlines (guestfish's `write` does not
# expand \n).
esp_write() { printf '%b' "$2" | guestfish -a "${DISK}" -m /dev/sda1:/ upload /dev/stdin "$1" >/dev/null; }
esp_rm() { guestfish -a "${DISK}" -m /dev/sda1:/ rm "$1" 2>/dev/null || true; }

# entries rewrites both entries: sort keys, and a trial counter on the target.
# This is what a switch does (internal/ab): no `default`, sort-key decides.
entries() { # <sortkeyA> <nameA> <sortkeyB> <nameB>
	esp_rm /loader/entries/vates-a.conf
	esp_rm /loader/entries/vates-b.conf
	esp_write "/loader/entries/$2.conf" \
		"title Vates Kube OS (A)\nsort-key $1\nlinux /bzImage\noptions root=PARTUUID=${ROOT_A_UUID} rootwait ro rootfstype=ext4 panic=5 console=ttyS0\n"
	esp_write "/loader/entries/$4.conf" \
		"title Vates Kube OS (B)\nsort-key $3\nlinux /bzImage\noptions root=PARTUUID=${ROOT_B_UUID} rootwait ro rootfstype=ext4 panic=5 console=ttyS0\n"
}

last_boot_uuid() {
	guestfish -a "${DISK}" -m /dev/sda4:/ cat /lib/vates/pid1.log 2>/dev/null \
		| grep '^root: PARTUUID ' | tail -1 | awk '{print $3}'
}

virsh -c "${CONN}" destroy "${NAME}" >/dev/null 2>&1 || true
virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true

mkdir -p "${VMDIR}"; chgrp qemu "${VMDIR}" 2>/dev/null || true; chmod 2775 "${VMDIR}"
BASE="${VMDIR}/base-$(basename "${IMAGE}")"
STAMP="${BASE}.stamp"
SRC_STAMP="$(stat -c '%s-%Y' "${IMAGE}" 2>/dev/null || true)"
if [ -f "${BASE}" ] && [ -n "${SRC_STAMP}" ] && [ "$(cat "${STAMP}" 2>/dev/null)" != "${SRC_STAMP}" ]; then rm -f "${BASE}"; fi
if [ ! -f "${BASE}" ]; then
	cp --reflink=auto "${IMAGE}" "${BASE}.tmp"; mv -f "${BASE}.tmp" "${BASE}"
	[ -n "${SRC_STAMP}" ] && printf '%s\n' "${SRC_STAMP}" > "${STAMP}"
fi
chmod 0664 "${BASE}" 2>/dev/null || true

fresh_disk() {
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	rm -f "${DISK}" "${NVRAM}"
	qemu-img create -q -f qcow2 -F qcow2 -b "${BASE}" "${DISK}" "${DISK_SIZE}"
	cp "${OVMF_VARS}" "${NVRAM}"
	chmod 0664 "${DISK}" "${NVRAM}" 2>/dev/null || true
	cat > "${XML}" <<XML
<domain type='kvm'>
  <name>${NAME}</name>
  <memory unit='MiB'>2048</memory><vcpu>2</vcpu>
  <os firmware='efi'>
    <type arch='x86_64' machine='q35'>hvm</type>
    <loader readonly='yes' type='pflash'>${OVMF_CODE}</loader>
    <nvram>${NVRAM}</nvram><boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough' check='none'/><clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff><on_reboot>restart</on_reboot><on_crash>destroy</on_crash>
  <devices>
    <disk type='file' device='disk'><driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='${DISK}'/><target dev='vda' bus='virtio'/></disk>
    <interface type='network'><source network='default'/><model type='virtio'/></interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='spice' autoport='yes' listen='127.0.0.1'><listen type='address' address='127.0.0.1'/></graphics>
    <video><model type='qxl' ram='32768' heads='1'/></video>
    <memballoon model='virtio'/>
  </devices>
</domain>
XML
	virsh -c "${CONN}" define "${XML}" >/dev/null
}

# boot waits for the node to come up (DHCP lease) then powers it off.
boot_wait() {
	virsh -c "${CONN}" start "${NAME}" >/dev/null
	wait_for_guest "${NAME}" "${TIMEOUT}" || fail=1
	virsh -c "${CONN}" destroy "${NAME}" >/dev/null
	sync; sleep 2
}

echo
echo "=== 1. ROLLBACK (fresh disk): B is a trial, its root is empty, expect A"
fresh_disk
esp_write /loader/loader.conf "timeout 3\n"
entries 1 vates-a 0 vates-b+3     # A sorts second; B sorts first and is a trial
echo "  booting: B is tried, fails, boot counting marks it bad, A must win"
boot_wait
booted="$(last_boot_uuid)"
echo "  last boot root PARTUUID: ${booted}"
if [ "${booted}" = "${ROOT_A_UUID}" ]; then
	echo "PASS: vates-b could not boot and the sort fell back to vates-a"
else
	echo "FAIL: expected the node on A (${ROOT_A_UUID}), got ${booted}" >&2
	fail=1
fi

echo
echo "=== 2. SWITCHOVER (fresh disk): copy rootA into rootB, expect B"
fresh_disk
guestfish -a "${DISK}" run : copy-device-to-device /dev/sda2 /dev/sda3 2>/dev/null
if guestfish -a "${DISK}" -m /dev/sda3:/ stat /usr/local/bin/vates-sysinit >/dev/null 2>&1; then
	echo "  rootB now carries the system (copied from rootA)"
else
	echo "FAIL: the copy into rootB did not take" >&2; fail=1
fi
esp_write /loader/loader.conf "timeout 3\n"
entries 1 vates-a 0 vates-b        # A sorts second; B sorts first and is good
boot_wait
booted="$(last_boot_uuid)"
echo "  last boot root PARTUUID: ${booted}"
if [ "${booted}" = "${ROOT_B_UUID}" ]; then
	echo "PASS: the node came up on the other root, vates-b"
else
	echo "FAIL: expected the node on B (${ROOT_B_UUID}), got ${booted}" >&2
	fail=1
fi

if [ "${KEEP:-0}" != "1" ]; then
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	rm -f "${DISK}" "${NVRAM}" "${XML}"
fi

echo
[ "${fail}" = 0 ] && echo "PASS" || echo "FAIL"
exit "${fail}"
