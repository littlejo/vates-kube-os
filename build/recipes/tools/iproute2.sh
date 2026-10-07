# iproute2 7.1.0 -- ip. Plain Makefile package driven by ./configure, which only
# writes config.mk. The xtables "tc" support is
# off (TC_CONFIG_XT:=n), so it does not need iptables.
build() {
	local d
	d=$(unpack iproute2 | tail -n1)
	(
		cd "${d}"
		CC=gcc ./configure --libbpf_force off
		echo "TC_CONFIG_XT:=n" >> config.mk
		# CFLAGS/LDFLAGS come from the environment (common.sh): iproute2's top
		# Makefile does "CFLAGS := ... -I../include ... $(CFLAGS)" and re-exports
		# the result, so passing CFLAGS on the command line would erase the -I
		# flags and break the build.
		make -j"$(nproc)" \
			LIBDB_LIBS=-lpthread \
			DBM_INCLUDE="${SYSROOT}/usr/include" \
			SHARED_LIBS=y
		make DESTDIR="${SYSROOT}" install
	)
}
