# Chore: native release packaging (C-missions)

Scoped from `luuna-ai-os/specs/feature-native-cli-distribution.md`, task **C-missions** only. Hub tasks H0–H7 and the other component tasks are owned elsewhere. Nothing is committed, tagged, released or published.

## Scope

- Add `scripts/release/package.sh`:
  - takes `--version V --out DIR [--os darwin|linux] [--arch arm64|amd64]`
  - builds `./cmd/luuna-missions` with `CGO_ENABLED=0 -trimpath -buildvcs=false -ldflags "-s -w -X github.com/luuna-ai/luuna-missions/internal/cli.CurrentVersion=<package.json version>"`
  - copies the binary as `lms`
  - writes a deterministic `luuna-missions_<V>_<os>_<arch>.tar.gz` with exactly `bin/luuna-missions` and `bin/lms` (0755, 0:0, no directory entry)
  - writes `SHA256SUMS`, `provenance.json`, `LICENSE` and `UPSTREAM.md`
- Add `scripts/release/check-archive.sh`. It checks the name, the optional `SHA256SUMS` and the entry allow-list (no links, absolute paths, `..` or wrong modes) before extraction. It then confirms regular executables and, on the host platform, runs `--help`/`--version` through both names.
- Add `scripts/release/test-release-scripts.sh`. These are offline tests with stub archives: argument validation, traversal, absolute path, symlink, directory, extra (`lm`) and missing entries, mode, checksum, foreign platform and corrupt input.
- Add `.github/workflows/release.yml`:
  - `workflow_dispatch` only, with `contents: read`
  - matrix: `macos-14` darwin/arm64, `macos-15-intel` darwin/amd64, `ubuntu-latest` linux/amd64, `ubuntu-24.04-arm` linux/arm64
  - steps: tests → package → check → `upload-artifact`
  - the attest step is commented out
  - no publish or release steps
- Add `docs/RELEASING.md`.
- Add `.tac/runs/` to `.gitignore`.

## Decisions and deviations

- **Runner:** `macos-13` in the parent plan is retired. `macos-15-intel` replaces it for darwin/amd64.
- **Version symbol:** this is the existing `internal/cli.CurrentVersion`, already set by `scripts/native-build.ts`. No new symbol was invented. The binary reports the internal compatibility version from `package.json` (0.6.0). The archive version is recorded separately in the file name and `provenance.json` (`archive_version` and `internal_version`).
- **Alias:** `main.go` uses `os.Args[1:]` only, so `lms` does not depend on argv[0]. It is a byte copy, and `check-archive.sh` asserts identical output.
- **Attribution:** `LICENSE` and `UPSTREAM.md` ship next to the archive rather than inside it, which keeps the strict `bin/` allow-list the hub installer enforces.
- **Toolchain:** `GOTOOLCHAIN=local` is used both locally and in CI. `go.mod` says `go 1.22`, which is a minimum. CI's `setup-go` resolves from it, and the exact version used is recorded in provenance. An exact pin is left to the owner.
- **Unchanged:** `package.json` bins, existing `ci.yml`, and Go/TS runtime sources.

## Validation

```sh
GOTOOLCHAIN=local go test ./...
sh scripts/release/test-release-scripts.sh
out=$(mktemp -d); a=$(sh scripts/release/package.sh --version 0.0.0-local --out "$out"); sh scripts/release/check-archive.sh "$a"
grep -nE 'gh release|action-gh-release|npm publish|cargo publish|attest-build-provenance' .github/workflows/release.yml   # only commented lines
```

`bun run verify` needs Bun 1.4.3. If Bun is absent, report it rather than installing it.
