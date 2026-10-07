#!/bin/bash
# Prove the control plane's virtual IP moves when its holder dies.
#
#   test/vip-failover.sh
#
# The VIP is the only address in the cluster that is not a node: everything
# points at it -- the kubelet's kubeconfig, the API server's endpoint in
# vates-node.yaml, the console dashboard. So "does it follow the leader" is not a
# detail, and it is not something to assume from the fact that kube-vip is
# deployed. It was wrong once: every joining control plane had a kube-vip
# manifest mounting a kubeconfig that does not exist on it, so kube-vip ran on
# one node only and the VIP had exactly one possible owner. Nothing looked broken
# until that node was the one to die.
#
# This runs entirely FROM THE HOST: it asks the API through the VIP with the
# kubeconfig the cluster handed over, and finds the holder by its MAC on the
# libvirt bridge. There is no SSH and no login on a node, and nothing is run
# inside one -- which is the point of the system, so the test obeys it too.
#
# The holder is killed with a power cut (`virsh destroy`) and started again at
# the end; it is not waited for, because a test that hangs to be tidy is a test
# nobody runs.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}" || exit

CONN="qemu:///system"
VIP="${VIP:-192.168.122.200}"
WORK="${WORK:-/var/tmp/vates-os/cluster}"
KUBECONFIG_LOCAL="${WORK}/kubeconfig"
TIMEOUT="${TIMEOUT:-90}"
CONTROLS=(vates-cp-1 vates-cp-2 vates-cp-3)

say() { printf '%s\n' "$*"; }
die() { printf 'FATAL: %s\n' "$*" >&2; exit 1; }

# api_through_vip asks the cluster health endpoint THROUGH THE VIP, from the
# host. Through the VIP and not through a node: the question is whether the
# cluster is reachable at the address everything is configured with.
api_through_vip() {
  [ "$(kubectl --kubeconfig "${KUBECONFIG_LOCAL}" --server="https://${VIP}:6443" \
        --request-timeout=3s get --raw=/healthz 2>/dev/null)" = "ok" ]
}

# mac_of <node> is the node's MAC on the libvirt bridge.
mac_of() {
  virsh -c "${CONN}" domiflist "$1" 2>/dev/null | awk '/network|bridge/ {print $5; exit}'
}

# vip_mac is the MAC currently answering for the VIP, after nudging the ARP table.
vip_mac() {
  ping -c1 -W2 "${VIP}" >/dev/null 2>&1
  ip neigh show "${VIP}" 2>/dev/null | awk '{print $5; exit}'
}

# holder is the control plane whose MAC answers for the VIP, or empty.
holder() {
  local m n; m="$(vip_mac)"
  [ -n "${m}" ] || return 0
  for n in "${CONTROLS[@]}"; do
    [ "$(mac_of "${n}")" = "${m}" ] && { echo "${n}"; return 0; }
  done
}

[ -f "${KUBECONFIG_LOCAL}" ] || die "no kubeconfig at ${KUBECONFIG_LOCAL}: run 'make cluster' first"

# --- find the holder ---------------------------------------------------------

say "=== who holds the VIP ==="
H="$(holder)"
[ -n "${H}" ] || die "no control plane holds ${VIP}; the VIP is already down"
for n in "${CONTROLS[@]}"; do
  printf '  %-14s %-18s %s\n' "${n}" "$(mac_of "${n}")" "$([ "${n}" = "${H}" ] && echo HOLDS || echo -)"
done
api_through_vip || die "the API does not answer through ${VIP} even on ${H}; fix that first"
say "  the API answers through ${VIP}"

# --- pull the plug -----------------------------------------------------------

say ""
say "=== killing ${H} (power cut) ==="
start=$(date +%s)
virsh -c "${CONN}" destroy "${H}" >/dev/null 2>&1 || die "could not destroy ${H}"
say "  [  0s] ${H} is down"

# --- wait for the move -------------------------------------------------------

say ""
say "=== watching for the address ==="
moved=""
while :; do
  sleep 2
  elapsed=$(( $(date +%s) - start ))
  [ -z "${moved}" ] && moved="$(holder)"
  if [ -n "${moved}" ] && api_through_vip; then
    say "  [$(printf '%3d' "${elapsed}")s] the API answers through ${VIP} again (on ${moved})"
    break
  fi
  if [ "${elapsed}" -ge "${TIMEOUT}" ]; then
    say ""
    say "FAILED: ${VIP} did not come back within ${TIMEOUT}s."
    say "  The address is not merely late: nothing else can take it unless"
    say "  kube-vip is RUNNING on the other control planes."
    virsh -c "${CONN}" start "${H}" >/dev/null 2>&1
    exit 1
  fi
  [ $((elapsed % 10)) -eq 0 ] && say "  [$(printf '%3d' "${elapsed}")s] not yet (VIP on: ${moved:-none})"
done

# --- put it back -------------------------------------------------------------

say ""
say "=== starting ${H} again ==="
virsh -c "${CONN}" start "${H}" >/dev/null 2>&1 && say "  ${H} started; it rejoins on its own"
say ""
say "RESULT: the VIP moved from ${H} to ${moved} in $(( $(date +%s) - start ))s,"
say "        and the API answered through it there."
