# util-linux 2.41.5 -- libblkid/libmount/libuuid/libfdisk/libsmartcols and the
# programs built on them. The long --disable-* list keeps only what the node
# needs: mount/umount come from busybox, not from here.
build() {
	autotools_build util-linux \
		--exec-prefix=/usr --program-prefix= \
		--disable-gtk-doc --disable-doc --disable-docs --disable-documentation \
		--with-xmlto=no --with-fop=no --disable-dependency-tracking \
		--enable-ipv6 --disable-nls --disable-static --enable-shared \
		--disable-asciidoc --disable-makeinstall-chown --disable-poman \
		--disable-rpath --disable-year2038 \
		--without-systemd --with-systemdsystemunitdir=no --without-udev \
		--enable-widechar --without-ncursesw --without-ncurses --without-selinux \
		--enable-all-programs \
		--disable-agetty --disable-bfs --disable-cal --disable-chfn-chsh \
		--disable-chmem --disable-cramfs --disable-eject --disable-fallocate \
		--disable-fdformat --disable-fsck --disable-hardlink --disable-hwclock \
		--disable-ipcmk --disable-ipcrm --disable-ipcs --disable-irqtop \
		--disable-kill --disable-last --enable-libblkid --enable-libfdisk \
		--enable-libmount --enable-libsmartcols --enable-libuuid --disable-line \
		--disable-logger --disable-login --disable-losetup --disable-lsfd \
		--disable-lslogins --disable-lsmem --disable-mesg --disable-minix \
		--disable-more --disable-mount --disable-mountpoint --disable-newgrp \
		--disable-nologin --disable-nsenter --disable-partx --disable-pg \
		--disable-pivot_root --disable-raw --disable-rename --disable-rfkill \
		--disable-runuser --disable-schedutils --disable-setpriv --disable-setterm \
		--disable-su --disable-sulogin --disable-switch_root --disable-tunelp \
		--disable-ul --disable-unshare --disable-utmpdump --disable-uuidd \
		--disable-vipw --disable-waitpid --disable-wall --disable-wdctl \
		--disable-wipefs --disable-write --disable-zramctl \
		--without-python --without-readline --without-audit --without-libmagic
}
