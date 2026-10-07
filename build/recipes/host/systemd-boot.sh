# systemd-boot -- the EFI boot loader, built from the upstream systemd source
# and installed into the image's /boot. It is built here, in the build, and not
# consumed as a host artifact: the bootloader is part of the OS.
#
# Only the bootloader is built: every other systemd feature is disabled below, so
# systemd itself is never installed -- only systemd-bootx64.efi comes out.
#
# The source URL/sha/version come from the build (ARGs), so this stage does not
# disturb pins.conf. The sysroot cross flags common.sh exports are cleared: this
# is a host-native EFI build.
build() {
	local url sha ver tar work target
	url="${SYSTEMD_URL:?SYSTEMD_URL unset}"
	sha="${SYSTEMD_SHA256:?SYSTEMD_SHA256 unset}"
	ver="${SYSTEMD_VERSION:?SYSTEMD_VERSION unset}"

	tar="${DL}/systemd-${ver}.tar.gz"
	if [ ! -f "${tar}" ]; then
		curl -fL --retry 3 -o "${tar}.part" "${url}"
		mv "${tar}.part" "${tar}"
	fi
	echo "${sha}  ${tar}" | sha256sum -c --quiet -

	work="${WORK}/systemd-boot"
	rm -rf "${work}"
	mkdir -p "${work}"
	tar -xf "${tar}" -C "${work}" --strip-components=1

	(
		cd "${work}"
		unset CFLAGS CXXFLAGS CPPFLAGS LDFLAGS
		env -u PKG_CONFIG_SYSROOT_DIR -u PKG_CONFIG_LIBDIR \
			meson setup build \
			-Dmode=release -Dbootloader=enabled -Defi=true \
			-Dman=disabled -Dtests=false -Dfuzz-tests=false -Dinstall-tests=false \
			-Ddbus=disabled -Dglib=disabled -Dlibarchive=disabled -Dacl=disabled \
			-Dapparmor=disabled -Daudit=disabled -Dlibcryptsetup=disabled \
			-Delfutils=disabled -Dlibiptc=disabled -Dlibidn=disabled -Dlibidn2=disabled \
			-Dseccomp=disabled -Dxkbcommon=disabled -Dbzip2=disabled -Dzstd=disabled \
			-Dlz4=disabled -Dpam=disabled -Dfdisk=disabled -Dkmod=disabled -Dxz=disabled \
			-Dzlib=disabled -Dlibcurl=disabled -Dp11kit=disabled -Dpcre2=disabled \
			-Dblkid=disabled -Dlibmount=disabled -Dhwdb=false -Dbinfmt=false \
			-Dutmp=false -Dvconsole=false -Dquotacheck=false \
			-Dbpf-framework=disabled -Dukify=disabled -Dxenctrl=disabled -Dima=false \
			-Dfirst-boot-full-preset=false -Dcreate-log-dirs=false
		target=$(ninja -C build -t targets all | grep -oE '[^ ]*systemd-bootx64\.efi' | head -1)
		[ -n "${target}" ] || {
			echo "no systemd-bootx64.efi target; efi targets:" >&2
			ninja -C build -t targets all | grep -i efi | head -20 >&2
			exit 1
		}
		ninja -C build "${target}"
		install -D -m 0644 "build/${target}" "${SYSROOT}/boot/systemd-bootx64.efi"
	)
}
