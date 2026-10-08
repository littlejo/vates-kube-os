# DejaVu fonts 2.37 -- the cairo + pango console has no built-in faces. DejaVu
# is the "DejaVu Sans Mono" the event feed is drawn in, and the fallback for
# whatever the Poppins faces (see poppins.sh) do not carry. Without any font
# under /usr/share/fonts, fontconfig resolves nothing and pango draws boxes.
# Pure data: no build, just install the TTFs; fontconfig picks them up from
# /usr/share/fonts.
build() {
	local d
	d=$(unpack dejavu | tail -n1)
	install -d "${SYSROOT}/usr/share/fonts/dejavu"
	find "${d}" -name '*.ttf' -exec install -m 0644 {} "${SYSROOT}/usr/share/fonts/dejavu/" \;
}
