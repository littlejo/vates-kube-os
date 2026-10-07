#!/bin/bash
# Create, define and start a Vates Kube OS VM under libvirt, so that it appears
# in virt-manager.
#
#   test/virsh-vm.sh <name> <configdrive.iso> [--mem MiB] [--vcpus N] [--no-start]
#
# Why libvirt and not plain qemu: the VMs must be visible in virt-manager, and a
# cluster of six has to have them on one network. `virsh -c qemu:///system` is
# used rather than the session connection because the session connection has NO
# networks at all -- session VMs get QEMU's user-mode networking, each in its own
# isolated 10.0.2.x -- and machines that cannot reach each other cannot form a
# cluster.
#
# The price of the system connection is that qemu runs as the `qemu` user, which
# cannot traverse /home (mode 0710 on the home directory). The disks therefore
# live under /var/tmp/vates-os, which is world-traversable, instead of in the
# repository.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONN="qemu:///system"

# Where the VMs' disks live. On the root filesystem, not /tmp, which is a 16 GiB
# tmpfs here and would hold the 3.5 GiB base image in RAM.
VMDIR="${VMDIR:-/var/tmp/vates-os}"

# OVMF_CODE and OVMF_VARS come from lib.sh, which locates the firmware pair for
# this distribution (edk2/ovmf on Fedora, edk2/x64 with .4m names on Arch).
# shellcheck source=test/lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# The image the machines boot, and the EFI variables they start from. Both are
# The image is the disk `make image` produces (and prints the path to),
# overridable so the same tooling boots another one:
#
#   IMAGE=.../vates.qcow2 make cluster
#
# The EFI variables default to the stock ones: this disk carries its own ESP and
# boots through the removable-media fallback, which is also what an imported disk
# does in Xen Orchestra.
IMAGE="${IMAGE:-${ROOT}/build/out/vates.qcow2}"
EFIVARS="${EFIVARS:-}"

NAME=""
SEED=""
MEM="${MEM:-3072}"
VCPUS="${VCPUS:-2}"
START=1

while [ $# -gt 0 ]; do
  case "$1" in
    --mem)      MEM="$2"; shift 2 ;;
    --vcpus)    VCPUS="$2"; shift 2 ;;
    --no-start) START=0; shift ;;
    -h|--help)
      sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *)
      if [ -z "${NAME}" ]; then NAME="$1"; elif [ -z "${SEED}" ]; then SEED="$1"; else
        echo "usage: $(basename "$0") <name> <configdrive.iso>" >&2; exit 2
      fi
      shift ;;
  esac
done

[ -n "${NAME}" ] && [ -n "${SEED}" ] || {
  echo "usage: $(basename "$0") <name> <configdrive.iso> [--mem MiB] [--vcpus N]" >&2
  exit 2
}

[ -f "${IMAGE}" ] || {
  echo "FATAL: no image at ${IMAGE}. Run: make image, or set IMAGE=" >&2; exit 1; }
[ -f "${SEED}" ] || { echo "FATAL: no config drive at ${SEED}" >&2; exit 1; }
# libvirt resolves disk source paths against its own working directory, not the
# caller's, so a relative path defines successfully and then fails at start with
# "Impossible d'acceder au fichier de stockage".
SEED="$(realpath "${SEED}")"

# vm_ip returns the lease libvirt's DHCP gave the domain, or an empty string.
# The guest is reachable directly at that address -- the host holds the bridge,
# so no port forwarding is needed, unlike the earlier plain-qemu setup.
vm_ip() {
  virsh -c "${CONN}" domifaddr "$1" --source lease 2>/dev/null \
    | awk '/ipv4/ {print $4}' | cut -d/ -f1 | head -1
}

mkdir -p "${VMDIR}"
# qemu runs as its own user, not as the invoking one. It has to be able to walk
# into the directory, and to create and replace the serial log it writes to, so
# the directory is set-group to qemu and setgid -- files created in it then
# inherit that group. Without this the domain defines and then fails to start
# with "Impossible de supprimer le fichier ...-serial.log: Permission non
# accordee", which names the log rather than the real problem.
chgrp qemu "${VMDIR}" 2>/dev/null || true
chmod 2775 "${VMDIR}"

# The base image is staged once. It is readable in the repository, but the
# repository is not reachable from the qemu user, so it is copied here.
#
# Staged, but NOT cached blindly: a base image left over from an earlier build is
# invisible. The domain starts, the guest boots an older system, and the symptom
# is a feature that "does not work" although its code is in the repository --
# observed exactly once, as `vates-init: field caCertHash not found`, against a
# binary that had been in the repository for several builds.
#
# The staging records the source image's size and mtime, and re-copies the base
# when either changes. A stale base would silently leave the VM booting an older
# system -- the symptom is a fix that is in the disk and still "does not work".
IMG_NAME="$(basename "${IMAGE}")"
BASE="${VMDIR}/base-${IMG_NAME}.qcow2"
BASE_STAMP="${VMDIR}/base-${IMG_NAME}.stamp"
STAMP_SRC="$(stat -c '%s-%Y' "${IMAGE}" 2>/dev/null || true)"
if [ -f "${BASE}" ] && [ -n "${STAMP_SRC}" ] && \
   [ "$(cat "${BASE_STAMP}" 2>/dev/null)" != "${STAMP_SRC}" ]; then
  echo "re-staging the base image: ${IMAGE} changed"
  rm -f "${BASE}"
fi
if [ ! -f "${BASE}" ]; then
  echo "staging the base image from ${IMAGE}..."
  cp --reflink=auto "${IMAGE}" "${BASE}.tmp"
  mv -f "${BASE}.tmp" "${BASE}"
  chmod 0664 "${BASE}"
  [ -n "${STAMP_SRC}" ] && printf '%s\n' "${STAMP_SRC}" > "${BASE_STAMP}"
fi

# A per-VM overlay, so the published image is never written to and two VMs can
# run side by side without sharing state.
DISK="${VMDIR}/${NAME}.qcow2"
if [ -f "${DISK}" ]; then
  echo "FATAL: ${DISK} already exists." >&2
  echo "  Remove it to recreate this VM: virsh -c ${CONN} undefine --remove-all-storage ${NAME}" >&2
  exit 1
fi
qemu-img create -q -f qcow2 -F qcow2 -b "${BASE}" "${DISK}" 20G
chmod 0664 "${DISK}"

# UEFI variables, one copy per VM: the systemd-boot entry lives in them, and
# sharing one file would have two VMs writing to it.
NVRAM="${VMDIR}/${NAME}-efivars.fd"
# The disk carries its own ESP, so it boots from a blank NVRAM: the firmware
# finds EFI/BOOT/BOOTX64.EFI through the removable-media fallback, which is also
# what an imported disk does in Xen Orchestra. EFIVARS can be set to reuse a
# prepared NVRAM instead.
if [ -f "${EFIVARS}" ]; then
  cp "${EFIVARS}" "${NVRAM}"
else
  cp "${OVMF_VARS}" "${NVRAM}"
fi
chmod 0664 "${NVRAM}"

XML="${VMDIR}/${NAME}.xml"
cat > "${XML}" <<XML
<domain type='kvm'>
  <name>${NAME}</name>
  <description>Vates Kube OS test node</description>
  <memory unit='MiB'>${MEM}</memory>
  <vcpu>${VCPUS}</vcpu>
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
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='${SEED}'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <source network='default'/>
      <model type='virtio'/>
    </interface>
    <!-- A pty, not a file.
         libvirt relabels a domain's disk images to svirt_image_t when it starts
         it. A serial log is not a disk image, so a file left in /var/tmp keeps
         that directory's label (user_tmp_t) while qemu runs as svirt_t, and the
         domain refuses to start with a permission error naming the log.
         Attaching to the pty with the virsh console command gives the same boot
         output without a file libvirt cannot manage. -->
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='spice' autoport='yes' listen='127.0.0.1'>
      <listen type='address' address='127.0.0.1'/>
    </graphics>
    <video><model type='qxl' ram='65536' heads='1'/></video>
    <channel type='spicevmc'><target type='virtio' name='com.redhat.spice.0'/></channel>
    <rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
    <memballoon model='virtio'/>
  </devices>
</domain>
XML

# qemu runs as its own user and has to walk the whole path to every file the
# domain references. Making that true here, rather than trusting whoever created
# the directories, is deliberate: the failure it prevents is
#   Could not open '<file>': Permission denied
# at domain start, which names the file and never the directory blocking it, and
# it has now cost time three times -- with a serial log, a config drive, and a
# second config drive.
ensure_readable() { # <file>
  local f="$1" d
  for d in "$(dirname "${f}")" "$(dirname "$(dirname "${f}")")"; do
    chgrp qemu "${d}" 2>/dev/null || true
    chmod 2775 "${d}" 2>/dev/null || true
  done
  chmod 0644 "${f}" 2>/dev/null || true
}
ensure_readable "${SEED}"
ensure_readable "${DISK}"

echo "=== defining ${NAME} ==="
virsh -c "${CONN}" define "${XML}" | sed 's/^/  /'

if [ "${START}" = "1" ]; then
  echo "=== starting ${NAME} ==="
  virsh -c "${CONN}" start "${NAME}" | sed 's/^/  /'
  echo
  echo "  console  : virt-manager, or:  virt-viewer -c ${CONN} ${NAME}"
  echo "  boot log : ./test/virsh-console.sh ${NAME}"
  sleep 3
  echo "  address  : $(vm_ip "${NAME}")"
fi
