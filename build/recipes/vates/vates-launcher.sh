# vates-launcher -- the kubelet image's only binary: fetch, verify, cache and
# exec one Kubernetes binary (this repository's Go source, staged at /src by
# build.sh). Installed on the node too; the kubelet image takes it from
# /sysroot/usr/local/bin (see lib/build-kubelet-image.sh).
build() {
	install -d "${SYSROOT}/usr/local/bin"
	CGO_ENABLED=0 go build -C /src -trimpath -ldflags '-s -w' \
		-o "${SYSROOT}/usr/local/bin/vates-launcher" ./cmd/vates-launcher
}
