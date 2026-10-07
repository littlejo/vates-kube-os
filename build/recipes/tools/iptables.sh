# iptables 1.8.11 -- the nft backend, no legacy/xtables compiler / nfsynproxy.
# --with-kernel points at the sysroot's kernel headers (UAPI).
build() {
	autotools_build iptables \
		--libexecdir=/usr/lib \
		--with-kernel="${SYSROOT}/usr" \
		--disable-static --enable-shared --disable-nls \
		--enable-nftables --disable-bpf-compiler --disable-nfsynproxy
}
