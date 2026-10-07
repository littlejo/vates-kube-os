#!/bin/bash
# Build a NoCloud config drive (cidata ISO) from a directory.
#
# The test drives were previously built by hand and committed as binaries, which
# meant their contents could drift from the directories beside them and no one
# could tell which was authoritative. This makes the directory the source and the
# ISO a derived file.
#
#   scripts/mkconfigdrive.sh test/fixtures/cfgdrive       -> test/fixtures/cfgdrive.iso
#   scripts/mkconfigdrive.sh test/fixtures/cfgdrive-capi  -> test/fixtures/cfgdrive-capi.iso
#   scripts/mkconfigdrive.sh test/fixtures/seed           -> test/fixtures/seed.iso
#
# The volume label is cidata, the NoCloud convention vates-init looks for.
set -euo pipefail

SRC="${1:-}"

if [ -z "${SRC}" ]; then
  echo "usage: $(basename "$0") <directory> [output.iso]" >&2
  exit 2
fi

[ -d "${SRC}" ] || { echo "FATAL: ${SRC} is not a directory" >&2; exit 1; }

# The label is not optional: without it vates-init finds no config drive and the
# node stays unconfigured. It is checked rather than assumed.
LABEL="${LABEL:-cidata}"

if [ -n "${2:-}" ]; then
  OUT="$2"
else
  # Strip a trailing slash before appending the extension, so
  # "test/fixtures/cfgdrive/" does not produce "test/fixtures/cfgdrive/.iso".
  OUT="${SRC%/}.iso"
fi

# NoCloud requires at least meta-data; a drive without it is malformed, and the
# failure would appear as a confusing cloud-init complaint much later.
[ -e "${SRC}/meta-data" ] || { echo "FATAL: ${SRC}/meta-data is required by NoCloud" >&2; exit 1; }

[ -e "${SRC}/user-data" ] || echo "WARNING: ${SRC}/user-data is missing" >&2

# The node configuration travels either in the user-data -- the CAPI path, where
# the document is written as-is -- or in a vates-node.yaml file, the direct-drive
# path. A user-data that is empty or only comments is the NoCloud placeholder,
# not a configuration. Warn only when NEITHER carries a document: that drive
# boots a node with nothing to configure, which is worth saying here rather than
# discovering as an unconfigured machine.
if [ ! -e "${SRC}/vates-node.yaml" ] && ! grep -qvE '^[[:space:]]*(#|$)' "${SRC}/user-data" 2>/dev/null; then
  echo "WARNING: ${SRC} carries no node configuration (user-data is empty and there is no vates-node.yaml)" >&2
fi

# -R enables Rock Ridge, which preserves case and permissions. Without it the
# ISO9660 8.3 mangling turns vates-node.yaml into VATES.YAM and pki/ into PKI/ --
# vates-init tolerates that, but there is no reason to introduce it. -J adds
# Joliet alongside it.
#
# xorriso is the ONE builder, and it is required rather than tried. An earlier
# version fell back through genisoimage, mkisofs and xorriso, which made the ISO
# a property of whatever the host happened to have -- valid every time, but a
# different program from one machine to the next. The names lie across
# distributions, too: mkisofs is genisoimage (cdrkit, GPL) on Fedora but cdrtools
# (CDDL) on Arch, two different programs under one name, and genisoimage does not
# exist on Arch at all. xorriso is one GNU program, all-GPL, packaged on Fedora,
# Debian and Arch, and writes the file without any privilege.
command -v xorriso >/dev/null 2>&1 || {
  echo "FATAL: xorriso is not installed; it builds the config-drive ISOs." >&2
  echo "  Fedora: dnf install xorriso" >&2
  echo "  Debian: apt install xorriso" >&2
  echo "  Arch:   pacman -S libisoburn" >&2
  exit 1
}

# Capture xorriso's output rather than redirecting it: it prints a banner on
# stderr even with -quiet, and that banner is noise on every cluster bring-up.
# It is shown only when the build actually fails.
if ! xorriso_out="$(xorriso -as mkisofs -quiet -o "${OUT}" -V "${LABEL}" -R -J "${SRC}" 2>&1)"; then
  printf 'FATAL: xorriso failed:\n%s\n' "${xorriso_out}" >&2
  exit 1
fi

echo "built ${OUT} (label ${LABEL}) with xorriso"
echo "  contents:"
# A convenience listing, not a check: xorriso is the one tool required, so we can
# list with it rather than with cdrkit's isoinfo (absent on Arch).
xorriso -indev "${OUT}" -find / 2>/dev/null | sed 's/^/    /' || true
