# vates-console -- the machine's screen, TUI or DRM (this repository's Go source,
# staged at /src by build.sh).
build() {
	install -d "${SYSROOT}/usr/local/bin"
	CGO_ENABLED=0 go build -C /src -trimpath -ldflags '-s -w' \
		-o "${SYSROOT}/usr/local/bin/vates-console" ./cmd/vates-console
}
