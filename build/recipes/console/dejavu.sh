# DejaVu fonts 2.37 -- the cairo + pango console has no built-in faces and its
# GUI asks for "Poppins" (not shipped) with a DejaVu fallback, and "DejaVu Sans
# Mono" for the event feed. Without any font under /usr/share/fonts, fontconfig
# resolves nothing and pango draws boxes. Pure data: no build, just install the
# TTFs; fontconfig picks them up from /usr/share/fonts.
build() {
	local d
	d=$(unpack dejavu | tail -n1)
	install -d "${SYSROOT}/usr/share/fonts/dejavu"
	find "${d}" -name '*.ttf' -exec install -m 0644 {} "${SYSROOT}/usr/share/fonts/dejavu/" \;
}
