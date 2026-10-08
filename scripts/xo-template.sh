#!/usr/bin/env bash
#
# Create a Xen Orchestra VM template from the built Vates Kube OS disk, and
# replace the previous template of the same name.
#
# WHY THIS IS NOT PURE REST: every step here is the public REST API (/rest/v0)
# EXCEPT the final "convert to template". XO exposes that conversion only
# through its internal JSON-RPC API (vm.convertToTemplate); the REST API refuses
# is_a_template with HTTP 422, and create_vm requires a VM-template (a plain VM
# is rejected with "no such VM-template"), so the conversion cannot be skipped.
# xo-cli speaks that JSON-RPC. The rest is curl.
#
# The steps mirror what one does by hand in the UI: import the disk, create a
# VM without booting it (UEFI), attach the disk, convert to a template, and
# remove the previous template so they do not pile up.
#
# Usage:
#   XO_URL=https://xo.example.org XO_TOKEN=<token> \
#   XO_POOL=<pool-uuid> XO_SR=<sr-uuid> \
#   ./scripts/xo-template.sh
#
# Environment:
#   XO_URL, XO_TOKEN   XO endpoint and an authentication token   (required)
#   XO_POOL, XO_SR     pool and storage repository UUIDs         (required)
#   VHD                disk to import          (default build/out/vates.vhd)
#   NAME               template name           (default "Vates Kube OS")
#   BASE_TEMPLATE      bare UUID of the base template for the intermediate VM
#                      (default: the built-in "Other install media")
#   XOCLI              xo-cli command          (default: xo-cli, else npx -y xo-cli)
#   KEEP_OLD           set to keep the previous template of the same name
#   UEFI               "false" for a BIOS VM   (default true -> UEFI)
#
# Prints the new template UUID on success.
set -euo pipefail

say() { printf '  %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# Values for this XO, so they do not have to be typed or looked up: a gitignored
# .env beside the Makefile. An explicit environment variable WINS over the file,
# so `XO_SR=... make template` still overrides it.
ENV_FILE="${ENV_FILE:-.env}"
if [ -f "$ENV_FILE" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|'#'*) continue ;; esac
    key="${line%%=*}"
    [ "$key" = "$line" ] && continue                 # no '='
    case "$key" in ''|*[!A-Za-z0-9_]*) continue ;; esac # not a shell name
    [ -n "${!key+x}" ] && continue                   # environment wins
    val="${line#*=}"
    case "$val" in
      \"*\") val="${val#\"}"; val="${val%\"}" ;;
      \'*\') val="${val#\'}"; val="${val%\'}" ;;
    esac
    export "$key=$val"
  done < "$ENV_FILE"
  say "loaded $ENV_FILE"
fi

: "${XO_URL:?set XO_URL in $ENV_FILE or the environment}"
: "${XO_TOKEN:?set XO_TOKEN in $ENV_FILE or the environment}"
: "${XO_POOL:?set XO_POOL in $ENV_FILE or the environment}"
: "${XO_SR:?set XO_SR in $ENV_FILE or the environment}"
VHD="${VHD:-build/out/vates.vhd}"
NAME="${NAME:-Vates Kube OS}"
UEFI="${UEFI:-true}"
BASE_TEMPLATE="${BASE_TEMPLATE:-}"

if [ -n "${DRY_RUN:-}" ]; then
  say "DRY_RUN: xo=$XO_URL pool=$XO_POOL sr=$XO_SR name='$NAME' vhd=$VHD uefi=$UEFI base=${BASE_TEMPLATE:-<auto>}"
  exit 0
fi

[ -f "$VHD" ] || die "no disk at $VHD (build it first: make image)"

command -v curl >/dev/null || die "curl is required"
command -v python3 >/dev/null || die "python3 is required"

# xo-cli: installed, or fetched on the fly. It is only needed for the two
# JSON-RPC calls (attachDisk, convertToTemplate).
if [ -n "${XOCLI:-}" ]; then
  xocli() { $XOCLI --allowUnauthorized --url "https://${XO_TOKEN}@${XO_HOST}/" "$@"; }
elif command -v xo-cli >/dev/null; then
  xocli() { xo-cli --allowUnauthorized --url "https://${XO_TOKEN}@${XO_HOST}/" "$@"; }
else
  command -v npx >/dev/null || die "xo-cli (or npx) is required for the JSON-RPC calls"
  xocli() { npx -y xo-cli --allowUnauthorized --url "https://${XO_TOKEN}@${XO_HOST}/" "$@"; }
fi

XO_HOST="${XO_URL#*://}"; XO_HOST="${XO_HOST%%/*}"
API="$XO_URL/rest/v0"

# api <curl args...>  — authenticated REST call.
api() { curl -fsSk -b "authenticationToken=${XO_TOKEN}" "$@"; }

# json <expr>  — read stdin JSON with a tiny python expression over `d`.
jget() { python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"; }

# vhd_virtual_size <file>  — the "current size" field of the VHD footer (512
# bytes at the end of the file, big-endian, offset 48).
vhd_virtual_size() {
  python3 - "$1" <<'PY'
import struct, sys
with open(sys.argv[1], "rb") as f:
    f.seek(-512, 2)
    footer = f.read(512)
if footer[:8] != b"conectix":
    sys.exit("not a VHD (missing conectix footer): " + sys.argv[1])
print(struct.unpack(">Q", footer[48:56])[0])
PY
}

say "XO $XO_URL (pool $XO_POOL, SR $XO_SR)"

# 0. connectivity
api "$API/vms?limit=1" >/dev/null || die "cannot reach $API (check XO_URL/XO_TOKEN)"

# 1. base template: create_vm needs one. "Other install media" is the generic
#    base; only its bare UUID is accepted, not the composite id the listing
#    returns for built-in templates.
if [ -z "$BASE_TEMPLATE" ]; then
  say "resolving the base template (Other install media)"
  BASE_TEMPLATE=$(api "$API/vm-templates?fields=id,name_label" | jget \
    "next((t['id'][-36:] for t in d if t.get('name_label')=='Other install media'), '')")
  [ -n "$BASE_TEMPLATE" ] || die "no 'Other install media' template; set BASE_TEMPLATE=<uuid>"
fi
say "base template $BASE_TEMPLATE"

# 2. import the disk as a VDI in the given SR
SIZE=$(vhd_virtual_size "$VHD") || die "cannot read the VHD virtual size"
say "creating a VDI in $XO_SR ($SIZE bytes)"
VDI=$(api -X POST -H 'Content-Type: application/json' \
  -d "{\"srId\":\"$XO_SR\",\"virtual_size\":$SIZE,\"name_label\":\"$NAME\"}" \
  "$API/vdis" | jget "d['id']")
say "VDI $VDI; uploading $VHD"
api -X PUT -H 'Content-Type: application/octet-stream' --data-binary "@$VHD" "$API/vdis/${VDI}.vhd" >/dev/null
say "disk uploaded"

# The VM and VDI are removed if anything below fails, so a half-built template
# does not sit in the SR.
VM=""
cleanup() {
  [ -n "$VM" ] && api -X DELETE "$API/vms/$VM" >/dev/null 2>&1 || true
  [ -n "$VDI" ] && api -X DELETE "$API/vdis/$VDI" >/dev/null 2>&1 || true
}
trap 'rc=$?; if [ $rc -ne 0 ]; then say "failed (rc=$rc); cleaning up"; cleanup; fi' EXIT

# 3. create the VM, UEFI, without booting
say "creating the VM (UEFI=$UEFI, no boot)"
FIRMWARE='"hvmBootFirmware":"uefi"'
[ "$UEFI" = "false" ] && FIRMWARE='"hvmBootFirmware":"bios"'
VM=$(api -X POST -H 'Content-Type: application/json' \
  -d "{\"name_label\":\"$NAME\",\"template\":\"$BASE_TEMPLATE\",\"boot\":false,$FIRMWARE}" \
  "$API/pools/$XO_POOL/actions/create_vm?sync=true" | jget "d['id']")
say "VM $VM"

# 4. attach the disk (bootable)
say "attaching the disk"
xocli vm.attachDisk "vm=$VM" "vdi=$VDI" bootable=true >/dev/null

# 5. convert to a template (JSON-RPC: the REST API cannot do this)
say "converting to a template"
xocli vm.convertToTemplate "id=$VM" >/dev/null
trap - EXIT # it is a template now; not something to delete on error

# 6. remove the previous templates of the same name (the new one is $VM)
if [ -z "${KEEP_OLD:-}" ]; then
  say "removing previous templates named '$NAME'"
  for old in $(api "$API/vm-templates?fields=id,name_label" | jget \
      "' '.join(t['id'] for t in d if t.get('name_label')=='$NAME' and t['id'][-36:]!='$VM')"); do
    api -X DELETE "$API/vm-templates/$old" >/dev/null && say "deleted $old"
  done
fi

printf '%s\n' "$VM"
say "done: template $VM"
