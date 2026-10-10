# DejaVu fonts 2.37 -- the console has no built-in faces. DejaVu is the
# "DejaVuSansMono" the event feed is drawn in, and the fallback for whatever the
# Poppins faces (see poppins.sh) do not carry. The console loads the TTFs by
# path (internal/display/gui/render); there is no fontconfig any more.
# Pure data: no build, just install the TTFs.
build() {
	local d
	d=$(unpack dejavu | tail -n1)
	install -d "${SYSROOT}/usr/share/fonts/dejavu"
	find "${d}" -name '*.ttf' -exec install -m 0644 {} "${SYSROOT}/usr/share/fonts/dejavu/" \;
}
