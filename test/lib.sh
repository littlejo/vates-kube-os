#!/bin/bash
# Shared helpers for the VM tests.
#
# Nothing here waits a fixed number of seconds for a guest to be ready. A guest
# is "up" when it has been given a DHCP lease, and PID 1 asks for that only after
# the filesystem is mounted (and grown), the read-only probe has run, and the
# container runtime is on its feet -- long before the checks read anything back.
# Polling for the lease is both faster and a real signal. A fixed sleep is
# neither: it wastes time when the guest is quick, and when it is slow the test
# reads a disk the guest never touched and reports a false failure.
#
# Sourced by ro.sh, grow.sh, reboot.sh and ab.sh. CONN is taken from the caller
# if it set one.

CONN="${CONN:-qemu:///system}"

# ovmf_firmware prints "<code.fd> <vars.fd>", the OVMF firmware pair to boot a
# guest with. The location is a property of the distribution, not of the host
# name: Fedora puts symlinks in edk2/ovmf, Arch ships only the 4 MiB build under
# edk2/x64 with ".4m" names, and Debian uses OVMF/ with an underscore. Hardcoding
# one of them is how a test that worked everywhere it was written fails on the
# next machine with
#   cp: cannot stat '/usr/share/edk2/ovmf/OVMF_VARS.fd': No such file or directory
# The known layouts are tried in order, and an explicit OVMF_CODE/OVMF_VARS pair
# still wins. The two files must come from the SAME directory: a 4 MiB code with
# a 2 MiB variable store, or the reverse, does not boot.
ovmf_firmware() {
	if [ -n "${OVMF_CODE:-}" ] && [ -n "${OVMF_VARS:-}" ]; then
		printf '%s %s\n' "${OVMF_CODE}" "${OVMF_VARS}"
		return 0
	fi
	local dir pair code vars
	for dir in \
		/usr/share/edk2/ovmf /usr/share/OVMF /usr/share/ovmf \
		/usr/share/edk2/x64 /usr/share/edk2-ovmf/x64; do
		for pair in "OVMF_CODE.fd:OVMF_VARS.fd" \
			"OVMF_CODE.4m.fd:OVMF_VARS.4m.fd" \
			"OVMF_CODE_4M.fd:OVMF_VARS_4M.fd"; do
			code="${dir}/${pair%%:*}"
			vars="${dir}/${pair##*:}"
			if [ -f "${code}" ] && [ -f "${vars}" ]; then
				printf '%s %s\n' "${code}" "${vars}"
				return 0
			fi
		done
	done
	return 1
}

# The tests that source this file all boot a UEFI guest, so the pair is resolved
# once, here. A script that set OVMF_CODE/OVMF_VARS itself before sourcing keeps
# its choice; ovmf_firmware honours it.
if [ -z "${OVMF_CODE:-}" ] || [ -z "${OVMF_VARS:-}" ]; then
	_ovmf="$(ovmf_firmware)" || {
		echo "FATAL: cannot find OVMF firmware (OVMF_CODE.fd / OVMF_VARS.fd)." >&2
		echo "  Install it (Fedora/Arch: edk2-ovmf, Debian: ovmf), or set OVMF_CODE= and OVMF_VARS=." >&2
		exit 1
	}
	OVMF_CODE="${_ovmf%% *}"
	OVMF_VARS="${_ovmf##* }"
	unset _ovmf
fi

wait_for_guest() { # <name> [timeout-seconds]
	local name="$1" timeout="${2:-150}" deadline
	deadline=$(( $(date +%s) + timeout ))
	while [ "$(date +%s)" -lt "${deadline}" ]; do
		if virsh -c "${CONN}" domifaddr "${name}" --source lease 2>/dev/null | grep -q ipv4; then
			echo "  node is up (DHCP lease obtained)"
			return 0
		fi
		sleep 2
	done
	echo "  node did not come up within ${timeout}s" >&2
	return 1
}
