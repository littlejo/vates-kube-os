#!/bin/bash
# Verify that a node's identity survives a reboot -- the property a CSI will not
# forgive losing, and the half of step 1 the write probe does not cover.
#
#   test/reboot.sh
#   IMAGE=.../vates.qcow2 test/reboot.sh
#   KEEP=1 test/reboot.sh
#
# Two boots of the SAME disk:
#
#   1. WITH a config drive, so vates-init configure runs and writes the node's
#      identity -- the cluster CA, its kubelet config, its hostname -- through the
#      /etc symlinks, onto /var;
#   2. WITHOUT the config drive, so configure cannot run at all. Anything still
#      present is present because /var persisted, not because it was written
#      again.
#
# The check is read off the disk with libguestfs: the identity tree is byte for
# byte the same, and the /var filesystem's UUID is unchanged (it was not
# recreated). No guest cooperation.
set -euo pipefail

CONN="qemu:///system"
VMDIR="${VMDIR:-/var/tmp/vates-os}"
NAME="${NAME:-reboottest}"
DISK_SIZE="${DISK_SIZE:-6G}"
WAIT="${WAIT:-45}"
IMAGE="${IMAGE:-${ROOT}/build/out/vates.qcow2}"

# shellcheck source=test/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

[ -f "${IMAGE}" ] || { echo "FATAL: no image at ${IMAGE}; run make image or set IMAGE=" >&2; exit 1; }

fail=0
SEED="${VMDIR}/${NAME}-seed"
SEED_ISO="${VMDIR}/${NAME}-seed.iso"

echo "=== building a config drive (a worker, with a freshly made CA)"
rm -rf "${SEED}"; mkdir -p "${SEED}/pki"
cat > "${SEED}/meta-data" <<EOF
instance-id: vates-reboot
local-hostname: vates-reboot
EOF
: > "${SEED}/user-data"
cat > "${SEED}/vates-node.yaml" <<EOF
role: worker
kubernetes:
  version: v1.31.0
cluster:
  controlPlaneEndpoint: "192.0.2.10:6443"
  token: "abcdef.0123456789abcdef"
network:
  iface: eth0
  mode: dhcp
cni:
  plugin: flannel
  cidr: "10.244.0.0/16"
EOF
openssl req -x509 -newkey rsa:2048 -nodes -keyout /dev/null -out "${SEED}/pki/ca.crt" \
	-subj "/CN=vates-reboot-test-ca" -days 1 >/dev/null 2>&1
"$(dirname "${BASH_SOURCE[0]}")/../scripts/mkconfigdrive.sh" "${SEED}" "${SEED_ISO}" >/dev/null

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
	cp --reflink=auto "${IMAGE}" "${BASE}.tmp"
	mv -f "${BASE}.tmp" "${BASE}"
	[ -n "${SRC_STAMP}" ] && printf '%s\n' "${SRC_STAMP}" > "${STAMP}"
fi
chmod 0664 "${BASE}" 2>/dev/null || true

DISK="${VMDIR}/${NAME}.qcow2"
NVRAM="${VMDIR}/${NAME}-efivars.fd"
rm -f "${DISK}" "${NVRAM}"
qemu-img create -q -f qcow2 -F qcow2 -b "${BASE}" "${DISK}" "${DISK_SIZE}"
cp "${OVMF_VARS}" "${NVRAM}"
chmod 0664 "${DISK}" "${NVRAM}" 2>/dev/null || true

# define_and_boot <with-seed: yes|no>: define the domain (with or without the
# config drive) and start it once. The disk is the same every time.
define_and_boot() {
	local with_seed="$1"
	local cdrom=""
	if [ "${with_seed}" = "yes" ]; then
		cdrom="<disk type='file' device='cdrom'><driver name='qemu' type='raw'/>
      <source file='${SEED_ISO}'/><target dev='sda' bus='sata'/><readonly/></disk>"
	fi
	local xml="${VMDIR}/${NAME}-${with_seed}.xml"
	cat > "${xml}" <<XML
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
  <on_poweroff>destroy</on_poweroff><on_reboot>restart</on_reboot><on_crash>destroy</on_crash>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='${DISK}'/><target dev='vda' bus='virtio'/>
    </disk>
    ${cdrom}
    <interface type='network'><source network='default'/><model type='virtio'/></interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='spice' autoport='yes' listen='127.0.0.1'><listen type='address' address='127.0.0.1'/></graphics>
    <video><model type='qxl' ram='32768' heads='1'/></video>
    <memballoon model='virtio'/>
  </devices>
</domain>
XML
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	virsh -c "${CONN}" define "${xml}" >/dev/null
	virsh -c "${CONN}" start "${NAME}" >/dev/null
}

# identity reads the node's identity off the disk: the /var filesystem's UUID,
# PID 1's log length, the cluster CA's hash, and the hostname configure wrote.
identity() {
	local uuid lines ca host
	uuid="$(guestfish -a "${DISK}" run : vfs-uuid /dev/sda4 2>/dev/null)"
	lines="$(guestfish -a "${DISK}" -m /dev/sda4:/ cat /lib/vates/pid1.log 2>/dev/null | wc -l)"
	ca="$(guestfish -a "${DISK}" -m /dev/sda4:/ cat /lib/vates/etc/kubernetes/pki/ca.crt 2>/dev/null | sha256sum | cut -d' ' -f1)"
	host="$(guestfish -a "${DISK}" -m /dev/sda4:/ cat /lib/vates/etc/hostname 2>/dev/null | tr -d '[:space:]')"
	printf '%s|%s|%s|%s\n' "${uuid}" "${lines}" "${ca}" "${host}"
}

echo
echo "=== boot 1 (with the config drive): configure writes the identity"
define_and_boot yes
sleep "${WAIT}"
virsh -c "${CONN}" destroy "${NAME}" >/dev/null; sync; sleep 2
IFS='|' read -r uuid1 lines1 ca1 host1 <<<"$(identity)"
echo "  /var fs uuid   : ${uuid1}"
echo "  pid1.log lines : ${lines1}"
echo "  CA sha256      : ${ca1}"
echo "  hostname       : ${host1}"

echo
echo "=== boot 2 (WITHOUT the config drive): configure cannot run"
define_and_boot no
sleep "${WAIT}"
virsh -c "${CONN}" destroy "${NAME}" >/dev/null; sync; sleep 2
IFS='|' read -r uuid2 lines2 ca2 host2 <<<"$(identity)"
echo "  /var fs uuid   : ${uuid2}"
echo "  pid1.log lines : ${lines2}"
echo "  CA sha256      : ${ca2}"
echo "  hostname       : ${host2}"

echo
empty_hash="$(printf '' | sha256sum | cut -d' ' -f1)"
if [ "${ca1}" = "${empty_hash}" ] || [ "${host1}" != "vates-reboot" ]; then
	echo "FAIL: configure wrote no identity on boot 1 (CA or hostname absent)" >&2
	fail=1
elif [ "${ca1}" = "${ca2}" ] && [ "${host1}" = "${host2}" ]; then
	echo "PASS: the CA and the hostname are identical across the reboot"
else
	echo "FAIL: the identity changed across the reboot" >&2
	fail=1
fi

if [ -n "${uuid1}" ] && [ "${uuid1}" = "${uuid2}" ]; then
	echo "PASS: /var was not recreated (filesystem UUID unchanged)"
else
	echo "FAIL: the /var filesystem changed UUID (${uuid1} -> ${uuid2})" >&2
	fail=1
fi

if [ "${lines2:-0}" -gt "${lines1:-0}" ]; then
	echo "PASS: PID 1's log grew across the reboot, so /var persisted and appended"
else
	echo "FAIL: PID 1's log did not grow (${lines1} -> ${lines2})" >&2
	fail=1
fi

if [ "${KEEP:-0}" != "1" ]; then
	virsh -c "${CONN}" undefine "${NAME}" --nvram >/dev/null 2>&1 || true
	rm -f "${DISK}" "${NVRAM}" "${VMDIR}/${NAME}-yes.xml" "${VMDIR}/${NAME}-no.xml"
	rm -rf "${SEED}" "${SEED_ISO}"
fi

echo
[ "${fail}" = 0 ] && echo "PASS" || echo "FAIL"
exit "${fail}"
