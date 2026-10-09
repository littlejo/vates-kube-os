# xen-guest-agent -- the Rust Xen guest agent (gitlab.com/xen-project/xen-guest-agent),
# the future upstream that replaces the Go xe-guest-utilities. One process:
# detects the guest OS itself and publishes it to XenStore.
#
# Cargo needs the C libxenstore to link and clang (libclang) to run bindgen
# against its headers; both are provided by the stage, and the pkg-config paths
# common.sh exports point at our /sysroot (recipes/guest/xenstore.sh put the
# .pc there). Dynamic: no -F static, so the binary dlopens libxenstore.so.4,
# which the image ships in /usr/lib.
build() {
	local d
	d=$(unpack xen-guest-agent | tail -n1)
	(
		cd "${d}"
		cargo build --release
		install -D -m 0755 target/release/xen-guest-agent \
			"${SYSROOT}/usr/sbin/xen-guest-agent"
		# License (AGPL-3.0-only): the text, where a recipient expects it.
		install -D -m 0644 LICENSE \
			"${SYSROOT}/usr/share/licenses/xen-guest-agent/LICENSE"
	)
}
