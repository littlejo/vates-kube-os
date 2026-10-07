# libseccomp 2.6.0 -- containerd/runc restrict syscalls with it. autotools, and
# it generates its syscall table with gperf (installed in the builder image).
build() {
	autotools_build libseccomp
}
