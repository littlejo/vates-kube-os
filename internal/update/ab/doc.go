// Package ab is the userspace half of the A/B update: which of the two roots
// booted, switching the default between them, and blessing a good boot.
//
// It is a package of its own, because two callers need it and they must not
// depend on each other: PID 1 blesses the running slot at boot, and the
// management API exposes status and switch -- the node has no shell, so the API
// is its only door. Neither imports the other, so neither can hold this logic
// without a cycle; here it is shared.
//
// The two roots are rootA and rootB (genimage.cfg); the ESP carries one boot
// entry for each, and systemd-boot's boot counting is what rolls a bad update
// back. ("Bless" is systemd's word -- sd-bless-boot: mark a boot as good.)
package ab
