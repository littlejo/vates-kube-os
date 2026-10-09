# The Xen guest agent, in Rust

The in-guest agent that makes a VM legible in Xen Orchestra (its IP, its OS, a
clean shutdown) is the Rust
[`xen-project/xen-guest-agent`](https://gitlab.com/xen-project/xen-guest-agent).
It **replaces** the Go `xe-guest-utilities` (`xcp-ng/xe-guest-utilities`) that
the image used to ship. This branch is where that switch is tested.

## Why the Rust one

One process instead of two. The Go pair was `xe-linux-distribution` (writes
`/var/cache/xe-linux-distribution`, the OS cache) then `xe-daemon` (reads it,
publishes to XenStore). The Rust agent detects the guest OS itself and publishes
in a single binary — no cache, no script, one child of PID 1. It is also the
designated upstream: the Go fork is not where the future work happens.

None of this is needed for the CAPI `providerID` — the CCM maps a node by its
SystemUUID — so the agent is best-effort: `startGuestAgent` no-ops off Xen.

## How it is built

Two recipes in the `guest` pass (see `docs/BUILD.md` for the recipe model):

- **`recipes/guest/xenstore.sh`** builds `libxenstore`, `libxentoolcore` and
  `libxentoollog` from the pinned Xen source, as **shared** libraries with the
  upstream sonames (`libxenstore.so.4`, `libxentoolcore.so.1`), plus a
  `xenstore.pc` and the headers. It does **not** run Xen's `configure`: that
  insists on `iasl` and a full tools build for what is, in the end, three C
  files. The recipe compiles them directly and reproduces the one generated
  input, the `tools/include/xen -> xen/include/public` symlink.
- **`recipes/guest/xen-guest-agent.sh`** builds the agent with Cargo (dynamic: no
  `-F static`), with the Rust toolchain and clang (for bindgen) borrowed from the
  base — build tools, never shipped.

Shared, not static, on purpose: `libxenstore` is **LGPL-2.1+** and the agent is
AGPL, so linking it at runtime is the ordinary distro arrangement. The `.so`
files are installed in `/usr/lib`, the loader's default path, so `xenstore-rs`'s
`dlopen("libxenstore.so.4")` finds them without an RPATH.

The one subtlety is the **link** path: `/usr/lib/libc.so` is a linker script
whose absolute paths only resolve under `--sysroot`, and having it on the link
line breaks a native Rust link. So `xenstore.pc`'s `libdir` points at
`/usr/lib/vates`, which holds *only* dev symlinks back to the real libraries in
`/usr/lib`; pkg-config then puts that directory on the link line, not `/usr/lib`.
The link stays host-native, so the binary links the builder's glibc (older) and
runs on the image's (newer) — the same situation as `containerd` and `runc`.

## What is shipped and started

`recipes/guest/xenstore.sh` and `recipes/guest/xen-guest-agent.sh` install into
`/sysroot`; the `disk` stage copies `/usr/sbin/xen-guest-agent` and the
`libxenstore.so.4` / `libxentoolcore.so.1` (and `libxentoollog.so.1`) shared
libraries in. `internal/sysinit/pid1.go` starts the agent (one supervised child),
after the same `/proc/xen` guard as before.

## Licensing

`libxenstore`/`libxentoolcore`/`libxentoollog` are **LGPL-2.1-or-later**; the
agent is **AGPL-3.0-only**. They are linked **dynamically** on purpose: the LGPL
lets a recipient replace the `.so`, so no relinking material is needed — static
linking would trigger the LGPL relink obligation (ship object files and build
scripts). The license texts are installed under `/usr/share/licenses/`
(`libxenstore/`, `xen-guest-agent/`), and a curated `THIRD-PARTY-NOTICES` rides
the overlay at `/usr/share/licenses/vates-kube-os/`.

## Pins

- Xen source `4.21.2`, sha256 `9c5c2ad0b4ab86ddd378f60ecc5b1ed53d36b436249ba27c86831003e1281fa3`.
- `xen-guest-agent` `main` @ `6acc719051364b0d34bde3c06ecc4baf5983a41e`
  (2026-06-01), archive sha256
  `61f09b0ce91906c055d41087870717ea3d1f44e1ed34994678d98703343eb26d` (Cargo.lock
  is committed upstream, so the dependency set is fixed by the commit).

Both live in [`build/pins.conf`](../build/pins.conf).

## Try it

Build only the guest pass:

```sh
podman build --target guest -t localhost/vates-guest build/
```

The full disk is `make image`. A Xen guest is needed to see the agent do
anything: off Xen it exits at once (which is why `startGuestAgent` guards on
`/proc/xen`).
