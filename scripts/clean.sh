#!/bin/bash
# Remove build artifacts and runtime scratch.
#
#   clean      what is derived and regenerated automatically: the built Go
#              binary, the generated NoCloud config drives, the staged image
#              content, the disk image and the build log. Cheap.
#
#   distclean  clean, plus the fetched source tarballs (build/dl) and the
#              podman build cache, whose loss costs a full rebuild: glibc, the
#              kernel, the console stack. This is the "start over" switch.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

say() { printf '  %s\n' "$*"; }

do_clean() {
  rm -rf test/run build/bin
  say "removed test/run/ and build/bin/"

  # The produced disk and what the build staged for it. The Containerfile layer
  # cache is untouched: the next build reuses it.
  rm -rf build/out build/context build/build.log
  say "removed build/out/, build/context/ and build/build.log"

  shopt -s nullglob
  local isos=(test/fixtures/*.iso)
  if [ "${#isos[@]}" -gt 0 ]; then
    rm -f "${isos[@]}"
    say "removed ${#isos[@]} generated config drive(s); scripts/mkconfigdrive.sh recreates them"
  fi
  shopt -u nullglob
}

do_distclean() {
  do_clean
  rm -rf build/dl
  say "removed build/dl/ (the fetched sources); build.sh fetches them again"

  rm -rf build/go-cache
  say "removed build/go-cache/ (the Go module cache vateskctl is built with)"

  if command -v podman >/dev/null; then
    podman rmi -f localhost/vates-scratch >/dev/null 2>&1 || true
    say "dropped the localhost/vates-scratch build images; the next build starts cold"
  fi
}

case "${1:-clean}" in
  clean)     do_clean ;;
  distclean) do_distclean ;;
  -h|--help|help)
    sed -n '2,13p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    ;;
  *)
    echo "usage: $(basename "$0") [clean|distclean]" >&2
    exit 2
    ;;
esac

echo "done."
