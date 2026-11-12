# Native release quick reference

Build local executables with `go run ./cmd/dev build`. The primary executable and all three aliases are written to `dist/`. Verify with `go run ./cmd/dev verify`.

For deterministic release archives, see [RELEASING.md](RELEASING.md):

```sh
sh scripts/release/test-release-scripts.sh
out=$(mktemp -d)
a=$(sh scripts/release/package.sh --version 0.0.0-local --out "$out")
sh scripts/release/check-archive.sh --require-run "$a"
```

Release archives currently target macOS and Linux, include the primary executable and `lms`, and ship attribution, provenance and checksums alongside the archive. Windows local builds are supported by the Go build command, but this archive workflow has no Windows packaging job. macOS binaries remain unsigned. Cross-compilation is not proof of native execution. The workflow uploads artifacts only; publication, signing and attestations require separate approval.
