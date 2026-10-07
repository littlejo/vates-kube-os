package firstboot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// NTPConfPath is the node's NTP configuration.
//
// It is a symlink into the writable /var in the image (see the board overlay):
// the root filesystem is read-only, and a node's time source is the machine's,
// not the image's. vates-init writes it from time.servers in vates-node.yaml, and
// busybox's ntpd reads the "server HOST" lines.
const NTPConfPath = "/etc/ntp.conf"

// ntpSyncTimeout bounds the boot-time one-shot synchronization. A node that
// cannot reach its NTP servers must still boot: the clock then comes from the
// hypervisor, which is what it did before this existed.
const ntpSyncTimeout = 5 * time.Second

// SyncClock writes the node's NTP configuration and sets the clock once, before
// anything is signed with it.
//
// kubeadm mints this node's certificates during configure, and a clock off by
// minutes yields certificates that are not yet valid and an etcd that refuses to
// run. The sync is bounded and best effort: a node that cannot reach its servers
// boots on the hypervisor's clock rather than not booting.
func SyncClock(servers []string) error {
	if err := writeNTPConf(servers); err != nil {
		return fmt.Errorf("writing %s: %w", NTPConfPath, err)
	}
	// ntpd -q steps the clock and exits. busybox's has no timeout of its own, so
	// one that cannot reach its servers is killed here rather than left to retry
	// for ever.
	ctx, cancel := context.WithTimeout(context.Background(), ntpSyncTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ntpd", "-q", "-n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ntpd -q: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writeNTPConf renders ntp.conf from the node's servers.
//
// The system runs in UTC, so nothing here names a zone: the clock is kept true,
// and "true" is UTC.
func writeNTPConf(servers []string) error {
	var b strings.Builder
	b.WriteString("# Written by vates-init, from time.servers in vates-node.yaml.\n")
	b.WriteString("# This node runs in UTC; the clock is kept true, not local.\n")
	for _, s := range servers {
		fmt.Fprintf(&b, "server %s\n", s)
	}
	return os.WriteFile(NTPConfPath, []byte(b.String()), 0o644)
}
