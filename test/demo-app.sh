#!/bin/bash
# Deploy a namespace and a small application, and prove it serves traffic.
#
#   test/demo-app.sh
#
# "The pod is Running" is not the same as "the application answers". A Service
# with no endpoints, a DNS entry that does not resolve, or a CNI that never
# programmed the route all leave pods looking perfectly healthy while nothing can
# reach them. So this script ends by asking a DIFFERENT pod to fetch the
# application BY ITS SERVICE NAME, which exercises the parts that a pod's status
# says nothing about: the Service, the endpoints controller, CoreDNS, kube-proxy
# -- or the CNI that replaced it -- and the datapath.
#
# The application is deliberately trivial -- http-echo printing a fixed line --
# so that what is being tested is the cluster, not the application.
#
# kubectl runs on the HOST, against the kubeconfig `make cluster` fetched from
# the node's management API. The image has no SSH, no login and no host kubectl,
# so reaching into a node is not an option: the node's API is the only door, and
# this script uses it the way an operator would.
#
# It needs a schedulable node: a worker, or a single control plane whose
# NoSchedule taint was removed. A bare `make cluster CP=1` has neither, and the
# deployment stays Pending by design.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}" || exit

WORK="${WORK:-/var/tmp/vates-os/cluster}"
CLUSTER="${CLUSTER:-vates-test}"
KUBECONFIG_LOCAL="${WORK}/kubeconfig"
MANIFEST="${ROOT}/test/demo-app.yaml"
NAMESPACE="demo"

say() { printf '%s\n' "$*"; }
die() { printf 'FATAL: %s\n' "$*" >&2; exit 1; }
vm_ip() { virsh -c qemu:///system domifaddr "$1" --source lease 2>/dev/null | awk '/ipv4/ {print $4}' | cut -d/ -f1 | head -1; }

[ -f "${KUBECONFIG_LOCAL}" ] || die "no kubeconfig at ${KUBECONFIG_LOCAL}; run make cluster first"

# --- pick a node to talk to --------------------------------------------------
#
# The kubeconfig names the control-plane VIP, which this libvirt network does
# not route from the host, so the chosen node's own address is used instead --
# the same detour test/cluster.sh's host_kubectl takes. Any vates node answers:
# a worker serves the API the same as a control plane.
NODE="" NODE_IP=""
for node in vates-cp-1 vates-cp-2 vates-cp-3 vates-worker-1 vates-worker-2 vates-worker-3; do
	ip="$(vm_ip "${node}")"
	[ -n "${ip}" ] || continue
	NODE="${node}"; NODE_IP="${ip}"; break
done
[ -n "${NODE_IP}" ] || die "no vates node is reachable on the libvirt network"

k() { kubectl --kubeconfig "${KUBECONFIG_LOCAL}" --server="https://${NODE_IP}:6443" "$@"; }

say "=== kubectl talks to ${NODE} (${NODE_IP}), kubeconfig ${KUBECONFIG_LOCAL} ==="
say ""

# --- apply -------------------------------------------------------------------

say "=== applying ${MANIFEST} ==="
k apply -f "${MANIFEST}" | sed 's/^/  /' || die "apply failed"

say ""
say "=== waiting for the deployment ==="
k -n "${NAMESPACE}" rollout status deploy/hello --timeout=180s | sed 's/^/  /' \
	|| die "the deployment did not become ready"

# --- what exists -------------------------------------------------------------

say ""
say "=== namespace, pods and service ==="
k get ns "${NAMESPACE}" | sed 's/^/  /'
k -n "${NAMESPACE}" get pods -o wide | sed 's/^/  /'
k -n "${NAMESPACE}" get svc,endpoints | sed 's/^/  /'

# A Service with no endpoints is the classic silent failure, and it looks fine in
# `get svc` alone: the port is there, the ClusterIP is there, and nothing is
# behind it.
endpoints=$(k -n "${NAMESPACE}" get endpoints hello -o jsonpath='{.subsets[*].addresses[*].ip}')
[ -n "${endpoints}" ] || die "the Service has no endpoints; nothing is behind it"
say "  endpoints: ${endpoints}"

# --- and it actually answers -------------------------------------------------

say ""
say "=== fetching the application from another pod, by service name ==="
k -n "${NAMESPACE}" delete pod hello-probe --ignore-not-found >/dev/null

# curl, not wget: this is the check that DNS, the ClusterIP, the Service rules
# and the datapath all work together, and curl reports the failure reason rather
# than just a status.
k -n "${NAMESPACE}" run hello-probe --image=curlimages/curl:8.11.0 --restart=Never \
	--command -- curl -sS --max-time 20 http://hello.${NAMESPACE}.svc.cluster.local:80/ \
	| sed 's/^/  /'

k -n "${NAMESPACE}" wait --for=jsonpath='{.status.phase}'=Succeeded pod/hello-probe --timeout=120s \
	| sed 's/^/  /' || {
		say ""
		say "  the probe pod did not succeed; its log:"
		k -n "${NAMESPACE}" logs hello-probe | sed 's/^/    /'
		k -n "${NAMESPACE}" describe pod hello-probe | tail -20 | sed 's/^/    /'
		exit 1
	}

answer=$(k -n "${NAMESPACE}" logs hello-probe)
say ""
say "  answer from the application:"
printf '    %s\n' "${answer}"

k -n "${NAMESPACE}" delete pod hello-probe --ignore-not-found >/dev/null

case "${answer}" in
	*"bonjour depuis Vates Kube OS"*) say ""; say "RESULT: the Service resolved and the application answered." ;;
	*) die "the application answered something unexpected: ${answer}" ;;
esac
