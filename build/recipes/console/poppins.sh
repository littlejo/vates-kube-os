# Poppins -- the Vates typeface, an open-source font by the Indian Type Foundry
# under the SIL Open Font License. Two things are installed:
#
#   - the full Poppins 4.003 family (Google Fonts build): the text faces. The
#     dashboard asks for Poppins Bold / Semi-Bold / regular; without them,
#     fontconfig falls back to DejaVu and the console is not the brand's.
#   - "Poppins Vates", the custom logotype variant Vates publishes. It is a
#     SUBSET -- uppercase A-Z, a space and an icon, no lowercase, no digits --
#     so it is fit for the all-caps labels only (tile labels, EVENTS, the state
#     pill), never for text. Kept beside the family, not merged into it.
#
# Pure data: no build, just install the TTFs; fontconfig picks them up from
# /usr/share/fonts. The URLs and hashes arrive as build ARGs set in the
# Containerfile rather than as pins.conf entries, so that adding the font does
# not invalidate the expensive libc layer.
build() {
	local zip="${WORK}/poppins.zip" ttf="${WORK}/poppins-vates.ttf" d

	echo "  fetching $(basename "${POPPINS_URL}")"
	curl -fL --retry 3 --connect-timeout 20 --speed-limit 1024 --speed-time 60 \
		-o "${zip}" "${POPPINS_URL}"
	echo "${POPPINS_SHA256}  ${zip}" | sha256sum -c --quiet -

	echo "  fetching $(basename "${POPPINS_VATES_URL}")"
	curl -fL --retry 3 --connect-timeout 20 --speed-limit 1024 --speed-time 60 \
		-o "${ttf}" "${POPPINS_VATES_URL}"
	echo "${POPPINS_VATES_SHA256}  ${ttf}" | sha256sum -c --quiet -

	# GNU tar does not read a zip; python3 is in the build base.
	d="${WORK}/poppins"
	rm -rf "${d}"
	mkdir -p "${d}"
	python3 -m zipfile -e "${zip}" "${d}"

	install -d "${SYSROOT}/usr/share/fonts/poppins"
	find "${d}" -name '*.ttf' -exec install -m 0644 {} "${SYSROOT}/usr/share/fonts/poppins/" \;

	install -d "${SYSROOT}/usr/share/fonts/poppins-vates"
	install -m 0644 "${ttf}" "${SYSROOT}/usr/share/fonts/poppins-vates/poppins_vates-webfont.ttf"
}
