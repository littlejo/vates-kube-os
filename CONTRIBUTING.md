# Contributing

Thanks for looking at Vates Kube OS. This file covers the day-to-day: how to
build, how to test, and the few conventions the repository follows.

## Build and run

```bash
make image                    # the disk: build/out/vates.qcow2 (needs podman)
make cluster CP=1 WORKERS=0   # boot it under libvirt (needs libvirt + the qemu/libvirt groups)
```

`make cluster` is the real test: one control plane, or three and three. See
[`docs/USAGE.md`](docs/USAGE.md) for the prerequisites, and [`docs/BUILD.md`](docs/BUILD.md)
to change what goes into the image.

## Before you push

```bash
make check        # gofmt, go vet, errcheck, go test
```

`make check` is what CI runs. `errcheck` must be on your `PATH`
(`go install github.com/kisielk/errcheck@v1.20.0`); the exclusions the project
chose are in [`.errcheck-exclude`](.errcheck-exclude).

## Documentation

- **`docs/`** is the public documentation: short, diagram-driven, aimed at
  someone discovering the project.
- **`notes/`** holds the longer development notes (the design records). It is
  **gitignored** and not part of the repository; keep it that way.

If a change needs a paragraph of design rationale, it belongs in `notes/`; what a
user needs to *do* belongs in `docs/`.

## Commits

One subject per commit, with a short `area: summary` line — for example
`cluster: keep rootless podman's storage out of the scratch tree`. The body
explains **why**, not what the diff already shows.

## Generated code

`proto/vates/api/v1/*.pb.go` is **committed on purpose**, so that `go build` and
the image build never need `buf` or `protoc`. After editing the `.proto`,
regenerate it — see the end of [`docs/API.md`](docs/API.md).

## License

By contributing, you agree that your contributions are licensed under the
[Apache License, Version 2.0](LICENSE).
