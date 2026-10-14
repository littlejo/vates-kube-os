# vates-launcher -- the Kubernetes binaries' launcher: fetch, verify, cache and
# exec one of them. Installed in the image at /usr/local/bin, where the kubelet,
# kubeadm, kubectl and mounter names are symlinks to it (see lib/assemble.sh).
build() {
	install -d "${SYSROOT}/usr/local/bin"
	CGO_ENABLED=0 go build -C /src -trimpath -ldflags '-s -w' \
		-o "${SYSROOT}/usr/local/bin/vates-launcher" ./cmd/vates-launcher
}
