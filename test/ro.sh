#!/bin/bash
# Verify the root filesystem is read-only -- the enforcement, not the intention.
#
#   test/ro.sh
#   IMAGE=.../vates.qcow2 test/ro.sh
#   KEEP=1 test/ro.sh       # leave the VM to inspect it
#
# Two checks, because either alone is a lie:
#   - the boot entry asks the kernel to mount `/` read-only (the intention);
#   - PID 1, at boot, tries to WRITE to `/` and records what happened (the
#     effect). A mount option can be read as "ro" while a remount, or a command
#     line that lost it, lets the write succeed.
#
# The second check needs no cooperation from the guest: PID 1's own output goes
# to /var/lib/vates/pid1.log on the /var partition, which is read back offline
# with libguestfs.
set -euo pipefail

CONN="qemu:///system"
VMDIR="${VMDIR:-/var/tmp/vates-os}"
NAME="${NAME:-rotest}"
DISK_SIZE="${DISK_SIZE:-6G}"
IMAGE="${IMAGE:-${ROOT}/build/out/vates.qcow2}"
WAIT="${WAIT:-150}"   # a timeout for wait_for_guest, not a sleep

# shellcheck source=test/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ -f "${IMAGE}" ] || { echo "FATAL: no image at ${IMAGE}; run make image or set IMAGE=" >&2; exit 1; }

fail=0

# --- the intention: the boot entry says ro --------------------------------
echo "=== boot entry (the intention) ==="
entry="$(guestfish --ro -a "${IMAGE}" -m /dev/sda1:/ cat /loader/entries/vates-a.conf 2>/dev/null || true)"
echo "${entry}"
options="$(printf '%s\n' "${entry}" | sed -n 's/^options //p')"
if printf '%s' "${options}" | grep -qw ro && ! printf '%s' "${options}" | grep -qw rw; then
	echo "PASS: the kernel command line asks for a read-only root"
else
	echo "FAIL: the command line does not say ro (or still says rw):" >&2
	echo "      ${options}" >&2
	fail=1
fi

# --- the effect: PID 1 tried to write to / --------------------------------
echo
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
	rm -f "${BASE}"
fi
if [ ! -f "${BASE}" ]; then
	echo "=== staging the base image"
	cp --reflink=auto "${IMAGE}" "${BASE}.tmp"
	mv -f "${BASE}.tmp" "${BASE}"
	[ -n "${SRC_STAMP}" ] && printf '%s\n' "${SRC_STAMP}" > "${STAMP}"
fi
# libvirt chowns a domain's images to qemu; a courtesy chmod is not permitted on
# a qemu-owned file and must not abort the test.
chmod 0664 "${BASE}" 2>/dev/null || true

DISK="${VMDIR}/${NAME}.qcow2"
NVRAM="${VMDIR}/${NAME}-efivars.fd"
rm -f "${DISK}" "${NVRAM}"
qemu-img create -q -f qcow2 -F qcow2 -b "${BASE}" "${DISK}" "${DISK_SIZE}"
cp "${OVMF_VARS}" "${NVRAM}"
chmod 0664 "${DISK}" "${NVRAM}" 2>/dev/null || true

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
echo "=== waiting for the node to come up"
wait_for_guest "${NAME}" "${WAIT}" || fail=1
virsh -c "${CONN}" destroy "${NAME}" >/dev/null
sync
sleep 2

# The probe results are on the /var partition; mount it read-write, which also
# replays the journal a hard power-off leaves behind.
echo
echo "=== PID 1's own account (the effect) ==="
log="$(guestfish -a "${DISK}" -m /dev/sda4:/ cat /lib/vates/pid1.log 2>/dev/null || true)"
probes="$(printf '%s\n' "${log}" | grep -E '^(root|cni|flexvol): ' || true)"
echo "${probes:-（no probe lines in pid1.log）}"

if printf '%s' "${probes}" | grep -q 'root: read-only (ok)'; then
	echo "PASS: a write to / was refused (EROFS), so the root is read-only"
else
	echo "FAIL: / is not read-only (or PID 1 reported no probe result)" >&2
	fail=1
fi

# The other half of the property: the paths a CNI installer writes must stay
# writable, or the node can never get its network.
if printf '%s' "${probes}" | grep -q 'cni: /opt/cni/bin writable (ok)'; then
	echo "PASS: /opt/cni/bin is writable, so a CNI installer can run"
else
	echo "FAIL: /opt/cni/bin is not writable -- no CNI could install" >&2
	fail=1
fi

# The same property again, for the path a control-plane pod declares as a
# DirectoryOrCreate hostPath: containerd creates it on the host before the
# container starts. On a read-only root it must resolve into /var, or the
# controller-manager never starts -- measured, with
#   failed to mkdir "/usr/libexec/kubernetes/kubelet-plugins/volume/exec":
#   read-only file system
# and the whole cluster stuck NotReady while etcd alone was fine.
if printf '%s' "${probes}" | grep -q 'flexvol: /usr/libexec/kubernetes/kubelet-plugins/volume/exec writable (ok)'; then
	echo "PASS: the volume plugin path is writable, so containerd can create it"
else
	echo "FAIL: the volume plugin path is not writable -- no control plane pod could start" >&2
	fail=1
fi

if [ "${KEEP:-0}" != "1" ]; then
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	rm -f "${DISK}" "${NVRAM}" "${XML}"
fi

echo
[ "${fail}" = 0 ] && echo "PASS" || echo "FAIL"
exit "${fail}"
