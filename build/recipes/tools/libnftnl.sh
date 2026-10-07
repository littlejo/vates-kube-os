# libnftnl 1.3.0 -- nftables netlink library, used by iptables' nft backend.
build() {
	autotools_build libnftnl --disable-static --enable-shared --disable-nls
}
