# vates-sysinit -- PID 1 and the boot sequence (this repository's Go source,
# staged at /src by build.sh). The sysroot flags common.sh exports are for C;
# Go ignores them under CGO_ENABLED=0, which is what makes these binaries static.
build() {
	install -d "${SYSROOT}/usr/local/bin"
	CGO_ENABLED=0 go build -C /src -trimpath -ldflags '-s -w' \
		-o "${SYSROOT}/usr/local/bin/vates-sysinit" ./cmd/vates-sysinit
}
