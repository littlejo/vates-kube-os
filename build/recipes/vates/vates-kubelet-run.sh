# vates-kubelet-run -- starts the kubelet container under containerd (this
# repository's Go source, staged at /src by build.sh).
build() {
	install -d "${SYSROOT}/usr/local/bin"
	CGO_ENABLED=0 go build -C /src -trimpath -ldflags '-s -w' \
		-o "${SYSROOT}/usr/local/bin/vates-kubelet-run" ./cmd/vates-kubelet-run
}
