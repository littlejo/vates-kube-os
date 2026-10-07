#!/bin/bash
# Build the operator CLI, vateskctl, into build/bin/vateskctl.
#
# vateskctl runs OFF the node -- it is what drives the management API from the
# operator's machine -- so it is not part of the image and the from-scratch
# builder does not produce it. It is an artifact of its own: `make cli` builds
# it, and `make cluster` builds it too, because the bring-up drives the API
# through it.
#
# It is built in a pinned Go container when a runtime is present, so a machine
# with podman/docker and no Go can still build it; a host Go is used when there
# is no runtime at all. The binary is streamed out over stdout rather than
# written through a bind mount: a bind-mounted output comes back owned by the
# container's user (root under a rootful runtime), and a 19 MiB binary in the
# repository owned by root is a trap. The redirect belongs to the caller.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${VATESKCTL_BIN:-${ROOT}/build/bin/vateskctl}"
# Overridable; the tag must satisfy the `go` directive in go.mod (1.25.0).
IMAGE="${VATESKCTL_IMAGE:-docker.io/library/golang:1.25}"

mkdir -p "$(dirname "${BIN}")"

rt=""
for c in podman docker; do
  command -v "${c}" >/dev/null 2>&1 && { rt="${c}"; break; }
done

if [ -z "${rt}" ]; then
  command -v go >/dev/null 2>&1 || {
    echo "FATAL: cannot build vateskctl: neither podman/docker nor go is available." >&2
    echo "  Install a container runtime (podman) or a Go toolchain." >&2
    exit 1
  }
  ( cd "${ROOT}" && go build -o "${BIN}" ./cmd/vateskctl )
  echo "built ${BIN} with the host go"
  exit 0
fi

# The Go caches live beside the build, not in an anonymous container layer, so
# the first build pays the module download once and the next ones do not.
cache="${ROOT}/build/go-cache"
mkdir -p "${cache}"

# SELinux: the repository is user_home_t, which a container cannot read, and
# relabelling it (:z) would touch every file in the checkout. Disable labelling
# for this one container instead -- it builds local, trusted source.
label=()
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" != "Disabled" ]; then
  label=(--security-opt label=disable)
fi

tmp="${BIN}.tmp"
rm -f "${tmp}"
if ! "${rt}" run --rm "${label[@]}" \
    -v "${ROOT}:/src:ro" \
    -v "${cache}:/gocache" \
    -w /src \
    -e CGO_ENABLED=0 \
    -e GOCACHE=/gocache/build \
    -e GOMODCACHE=/gocache/mod \
    -e GOTOOLCHAIN=local \
    "${IMAGE}" \
    sh -c 'go build -o /tmp/vateskctl ./cmd/vateskctl && cat /tmp/vateskctl' \
    > "${tmp}"; then
  rm -f "${tmp}"
  echo "FATAL: building vateskctl in ${IMAGE} failed (see the errors above)" >&2
  exit 1
fi
chmod 0755 "${tmp}"
mv -f "${tmp}" "${BIN}"
echo "built ${BIN} with ${rt} (${IMAGE})"
