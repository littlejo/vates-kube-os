#!/bin/bash
# Verify that a node grows its /var to fill a disk larger than the image.
#
#   test/grow.sh                       # boot the current image on a 20G disk
#   IMAGE=.../vates.qcow2 test/grow.sh # or another image
#   KEEP=1 test/grow.sh                # leave the VM to inspect it
#
# This is the one part of the layout only a real kernel can confirm. The image's
# partition table ends at ~4.4 GiB; the VM's disk is 20 GiB, so at first boot PID
# 1 must extend the /var partition and grow its filesystem to fill the rest.
#
# The check needs nothing from the guest -- no login, no serial capture, nothing
# the node has to cooperate with. The VM boots, PID 1 runs its grow step, the VM
# is powered off, and the result is read straight off the disk with
# virt-filesystems.
set -euo pipefail

CONN="qemu:///system"
VMDIR="${VMDIR:-/var/tmp/vates-os}"
NAME="${NAME:-growtest}"
DISK_SIZE="${DISK_SIZE:-20G}"
WAIT="${WAIT:-70}"
IMAGE="${IMAGE:-${ROOT}/build/out/vates.qcow2}"

# shellcheck source=test/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ -f "${IMAGE}" ] || { echo "FATAL: no image at ${IMAGE}; run make image or set IMAGE=" >&2; exit 1; }

echo "=== image: ${IMAGE}"
echo "=== clearing any previous ${NAME}"
virsh -c "${CONN}" destroy "${NAME}" >/dev/null 2>&1 || true
virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true

mkdir -p "${VMDIR}"
chgrp qemu "${VMDIR}" 2>/dev/null || true
chmod 2775 "${VMDIR}"

BASE="${VMDIR}/base-$(basename "${IMAGE}")"
STAMP="${BASE}.stamp"
SRC_STAMP="$(stat -c '%s-%Y' "${IMAGE}" 2>/dev/null || true)"
if [ -f "${BASE}" ] && [ -n "${SRC_STAMP}" ] && [ "$(cat "${STAMP}" 2>/dev/null)" != "${SRC_STAMP}" ]; then
	echo "=== re-staging the base image (it changed)"
	rm -f "${BASE}"
fi
if [ ! -f "${BASE}" ]; then
	echo "=== staging the base image"
	cp --reflink=auto "${IMAGE}" "${BASE}.tmp"
	mv -f "${BASE}.tmp" "${BASE}"
	[ -n "${SRC_STAMP}" ] && printf '%s\n' "${SRC_STAMP}" > "${STAMP}"
fi
# libvirt chowns a domain's disk images -- and their backing files -- to qemu
# when it starts the VM, so a cached base is qemu-owned on the next run and this
# chmod is not permitted. It is only a courtesy (qemu already owns the file and
# can read it), so it must not abort the test.
chmod 0664 "${BASE}" 2>/dev/null || true

DISK="${VMDIR}/${NAME}.qcow2"
NVRAM="${VMDIR}/${NAME}-efivars.fd"
rm -f "${DISK}" "${NVRAM}"
qemu-img create -q -f qcow2 -F qcow2 -b "${BASE}" "${DISK}" "${DISK_SIZE}"
cp "${OVMF_VARS}" "${NVRAM}"
chmod 0664 "${DISK}" "${NVRAM}"

# The same domain virsh-vm.sh defines, minus the config drive: the grow runs
# before configure, so the node needs nothing to boot into it.
XML="${VMDIR}/${NAME}.xml"
cat > "${XML}" <<XML
<domain type='kvm'>
  <name>${NAME}</name>
  <memory unit='MiB'>2048</memory>
  <vcpu>2</vcpu>
  <os firmware='efi'>
    <type arch='x86_64' machine='q35'>hvm</type>
    <loader readonly='yes' type='pflash'>${OVMF_CODE}</loader>
    <nvram>${NVRAM}</nvram>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough' check='none'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='${DISK}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='network'><source network='default'/><model type='virtio'/></interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='spice' autoport='yes' listen='127.0.0.1'><listen type='address' address='127.0.0.1'/></graphics>
    <video><model type='qxl' ram='32768' heads='1'/></video>
    <memballoon model='virtio'/>
  </devices>
</domain>
XML

echo "=== defining and starting ${NAME} (disk ${DISK_SIZE})"
virsh -c "${CONN}" define "${XML}" >/dev/null
virsh -c "${CONN}" start "${NAME}" >/dev/null

echo "=== waiting ${WAIT}s for PID 1 to run its grow step"
sleep "${WAIT}"
virsh -c "${CONN}" destroy "${NAME}" >/dev/null
sync
sleep 2

echo
echo "=== partitions and filesystems on the VM's disk"
virt-filesystems -a "${DISK}" --all --long -h

# Read the /var filesystem size in bytes BEFORE cleanup. No -h, so the size is an
# integer; with -h it would be human and locale-dependent ("1,9G"). The CSV
# columns are Name,Type,VFS,Label,Size,Parent -- Size is the fifth.
grown_bytes="$(LC_ALL=C virt-filesystems -a "${DISK}" --filesystems --long --csv 2>/dev/null \
	| awk -F, '$0 ~ /vates-var/ {print $5}' | head -1)"

if [ "${KEEP:-0}" != "1" ]; then
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	rm -f "${DISK}" "${NVRAM}" "${XML}"
fi

echo
if [ -z "${grown_bytes}" ]; then
	echo "FAIL: could not read the /var filesystem size" >&2
	exit 1
fi
# The image ships /var at 256M (filesystem ~230M); a grown one is thousands of
# times that, so 1 GiB is a threshold nothing else in this test can reach.
if [ "${grown_bytes}" -gt 1073741824 ]; then
	echo "PASS: /var grew to ${grown_bytes} bytes (> 1 GiB)"
else
	echo "FAIL: /var is still ${grown_bytes} bytes; the grow did not happen" >&2
	exit 1
fi
