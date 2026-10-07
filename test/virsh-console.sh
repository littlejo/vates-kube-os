#!/bin/bash
# Attach to a VM's serial console and print it.
#
#   test/virsh-console.sh <name>            follow it (Ctrl-] to detach)
#   test/virsh-console.sh <name> --dump     print what can be read, then exit
#
# The console is a pty rather than a file because libvirt relabels a domain's
# disk images but not a serial log, and a log written into /var/tmp keeps the
# label of that directory while qemu runs as svirt_t.
set -euo pipefail
CONN="qemu:///system"
NAME="${1:?usage: $(basename "$0") <name> [--dump]}"
MODE="${2:-follow}"

if [ "${MODE}" = "--dump" ]; then
  # `virsh console` has no non-interactive mode; it is started, given a moment,
  # then stopped, and whatever it printed is what gets shown.
  timeout 5 virsh -c "${CONN}" console --force "${NAME}" 2>&1 || true
else
  exec virsh -c "${CONN}" console --force "${NAME}"
fi
