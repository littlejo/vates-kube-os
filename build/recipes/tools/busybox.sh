# busybox 1.38.0 -- kept for udhcpc, sh, ntpd and a few helpers. Its applet set
# starts from busybox's own defconfig plus board/busybox.fragment (the NTP client
# vates-init drives). The kernel's init= names vates-sysinit; busybox is not init.
build() {
	local d
	d=$(unpack busybox | tail -n1)
	(
		cd "${d}"
		make defconfig
		cat "${SCRATCH}/board/busybox.fragment" >> .config
		# busybox's tc applet uses CBQ structures that a recent kernel UAPI no
		# longer carries; iproute2 provides tc. Disable it (edit the existing
		# line: an appended "# ... is not set" is ignored by the old kconfig).
		sed -i 's/^CONFIG_TC=y/# CONFIG_TC is not set/' .config
		# start-stop-daemon was only ever called by the /etc/init.d scripts,
		# which PID 1 (vates-sysinit) replaced; nothing calls it now. Disable
		# the applet too (same edit-the-existing-line rule as tc above; the
		# FEATURE_* symbols depend on it and drop off in oldconfig).
		sed -i 's/^CONFIG_START_STOP_DAEMON=y/# CONFIG_START_STOP_DAEMON is not set/' .config
		# busybox's kconfig is old: it has no olddefconfig. oldconfig takes the
		# default for every symbol the fragment exposes. The subshell drops
		# pipefail so yes's SIGPIPE on make finishing is not an error.
		( set +o pipefail; yes "" | make oldconfig )
		make -j"$(nproc)" \
			CC=gcc HOSTCC=gcc CROSS_COMPILE= \
			EXTRA_LDFLAGS="${LDFLAGS}" \
			AR=ar NM=nm RANLIB=ranlib \
			SKIP_STRIP=y
		make CONFIG_PREFIX="${SYSROOT}" PREFIX="${SYSROOT}" install-noclobber
	)
}
