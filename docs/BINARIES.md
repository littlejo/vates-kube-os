# The binaries

The image ships **several small binaries** rather than one busybox-style
multi-call program. The reason is measured, not stylistic: a Go program runs the
`init()` of every package linked into it, so a single binary made *every* process
pay for the heaviest packages linked in, and the kubelet image carried the whole
program to run one launcher.

| Binary | Role | Faces (`argv[0]`) |
|---|---|---|
| `vates-sysinit` | PID 1 and the boot sequence, plus the first-boot configuration | `vates-sysinit`, `init`, `vates-init`, `vates-splash` |
| `vates-console` | the machine's screen | `vates-console`, `vates-dashboard` |
| `vates-api` | the management API | `vates-api` |
| `vates-launcher` | the kubelet image: fetch, verify, cache, exec one Kubernetes binary | `vates-launcher`, `kubelet`, `kubeadm`, `kubectl`, `mounter` |
| `vateskctl` | the operator's CLI (runs off the node) | — |

There is **no kubelet-runner binary**: the kubelet container is started by
containerd's own `ctr`, which now exposes the `--rootfs-propagation` flag the
custom runner existed for (containerd#5381). That removed the only import of
containerd's client library.

`vates-init` and `vates-splash` are symlinks to `vates-sysinit`; `vates-dashboard`
is a symlink to `vates-console`. `firstboot` has no binary of its own: it runs
once, in process, inside `vates-sysinit`.

## Why it matters

On the long-running faces (`vates-sysinit`, `vates-console`, `vates-api`), the
split drops the non-shareable memory from ~3.5 MB to ~1.7 MB per process — about
**1.7 MB each** — and the kubelet image's binary goes from ~20 MB to under 6 MB.
That image is imported into containerd on every node at first boot, so the
saving is paid on every machine.

The split also draws a boundary by construction: PID 1 no longer carries gRPC,
TLS and protobuf; the API no longer carries containerd, the renderers and the boot
sequence. `package main` enforces the dependency split, not discipline.

## Measuring it

Per process, at rest, from `/proc/<pid>/smaps_rollup`:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /tmp/vates-api ./cmd/vates-api
/tmp/vates-api & pid=$!
sleep 1
grep -E '^(Pss|Pss_Anon|Pss_File):' /proc/$pid/smaps_rollup
kill $pid
```

Run it on a **disk-backed** filesystem, not a tmpfs, and with KSM disabled: on a
tmpfs the numbers are meaningless, and with KSM enabled identical pages get merged
and they lie the other way.
