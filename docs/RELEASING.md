# Releasing native Luuna Missions archives

Nothing in this repository publishes anything. The scripts below build and verify archives. The workflow uploads them only as GitHub Actions workflow artifacts. Packages, GitHub Releases, tags, signing and attestations are deliberate owner actions.

## What an archive contains

Each archive is `luuna-missions_<version>_<os>_<arch>.tar.gz`, where `os` is `darwin` or `linux` and `arch` is `arm64` or `amd64`. It contains exactly two entries:

- `bin/luuna-missions`
- `bin/lms`

Both are regular files with mode 0755, owner `0:0` and a fixed mtime of 2000-01-01T00:00:00Z (`946684800`), set in UTC so the archive bytes do not depend on the builder's time zone. There is no directory entry. The compatibility aliases `lm` and `missions` are deliberately excluded from public archives, but they remain Bun package bins for source use. `lms` is a byte-identical copy of the primary: `cmd/luuna-missions/main.go` passes `os.Args[1:]` and never inspects argv[0], so behavior and arguments are identical under either name. The help text always says `luuna-missions`.

These files sit next to the archive:

- `LICENSE` (the upstream MIT notice) and `UPSTREAM.md` (provenance). The archive's entry allow-list is strict, so attribution ships next to the archive instead of inside it.
- `SHA256SUMS` lists the archive, `provenance.json`, `LICENSE` and `UPSTREAM.md`.
- `provenance.json` is self-reported metadata. It records repository, commit, ref, `source_tree_dirty` (source-tree dirtiness, including untracked non-ignored files; `null` when unknown), workflow run id, runner, Go version, build env/flags/ldflags, the archive SHA256, and two separate versions:
  - `archive_version`: the `--version` you pass, used in the file name.
  - `internal_version`: the version the binary prints for `--version`. It comes from `package.json`, today the `0.6.0` upstream compatibility baseline, and is injected through the existing symbol `github.com/luuna-ai/luuna-missions/internal/cli.CurrentVersion`, the same one `scripts/native-build.ts` sets.

  Outside a Git work tree, `commit`, `ref` (unless `GITHUB_REF` is set) and `source_tree_dirty` are JSON `null`; an unknown Go version is also `null`. `provenance.json` is not an attestation.

## Local dry run (host platform)

```sh
GOTOOLCHAIN=local go test ./...
sh scripts/release/test-release-scripts.sh          # offline, stub fixtures and a stub go; needs git
out=$(mktemp -d)
a=$(sh scripts/release/package.sh --version 0.0.0-local --out "$out")
sh scripts/release/check-archive.sh --require-run "$a"
```

- `package.sh` defaults to the host `--os`/`--arch`. It can cross-build the other Go targets with `CGO_ENABLED=0`. In that case `check-archive.sh` verifies entries and checksums but does not execute the binaries, and says so. `--require-run` turns that into a failure (exit 6); combining it with `--no-run` is a usage error (exit 2).
- `package.sh` writes the tar stream to a temporary file and gzips it in a separate step, so a failure of either command fails the run. On any failure it removes the files it wrote to `--out`, and removes `--out` itself if the run created it.
- `package.sh` sets `GOTOOLCHAIN=local` unless you override it, so it never downloads a Go toolchain.
- The first build may download the `gopkg.in/yaml.v3` module into the Go module cache.
- Use `LUUNA_MISSIONS_GO` to choose a Go executable.
- `bun run check:agents` treats the ignored `dist/` build output as known-missing, so it passes on a fresh checkout; a local Bun for validation may live in the ignored `.tac/tools/`.

`check-archive.sh` checks these, in order, before it lists or extracts anything (exit 4 on any `SHA256SUMS` problem):

1. The name pattern.
2. The mandatory `SHA256SUMS`, which must be a regular file, not a symlink, with exactly one matching entry. The entry may use the `*` binary-mode prefix, and its digest must be 64 lowercase hex digits equal to the archive's SHA256.

It then checks the listing:

3. The exact entry set, with no links, directories, devices, absolute or `..` paths, and mode 0755 on every entry.

It then extracts into a private temp dir. On the host platform it runs `--help` and `--version` through both names and requires identical output.

## Workflow

`.github/workflows/release.yml` has these properties:

- **Trigger:** `workflow_dispatch` with a `version` input only. There is no tag trigger.
- **Permissions:** `contents: read`.
- **Runner matrix:** host-native, one job per target:

  | Runner | Target |
  | --- | --- |
  | `macos-14` | darwin/arm64 |
  | `macos-15-intel` | darwin/amd64 |
  | `ubuntu-latest` | linux/amd64 |
  | `ubuntu-24.04-arm` | linux/arm64 |

  The parent plan named `macos-13`, which GitHub has retired. `macos-15-intel` replaces it.
- **Steps:** Go tests → release-script tests → `package.sh` → `check-archive.sh --require-run` (fails if the binaries were not executed on the runner) → `actions/upload-artifact`.
- **Not included:** `gh release`, `npm publish` and registry steps. The attestation step is present but commented out.
- **Toolchain:** `actions/setup-go` resolves `go-version-file: go.mod`, whose `go 1.22` directive is a minimum, not an exact pin. `GOTOOLCHAIN=local` stops a second toolchain download. Record the exact Go version from `provenance.json`. Exact pinning (for example `go-version: 1.27.x`) is an owner decision.

Bun is not needed for release builds. The existing `bun run verify` CI is unchanged.

## Deferred owner gates

1. Run `release.yml` through `workflow_dispatch` and download the four artifacts.
2. On each claimed platform, verify `SHA256SUMS`, re-run `check-archive.sh` and review `provenance.json`.
3. Decide on Apple signing/notarization and on GitHub attestations (uncomment the attest step and add `id-token: write`/`attestations: write`).
4. Choose a distinct Luuna release version (see `UPSTREAM.md`), then create the tag and GitHub Release and upload the assets.
5. Add the hub manifest row (`luuna-ai-os/releases/<v>/manifest.tsv`) through a reviewed PR in that repository.

npm publication of `@luuna/missions-cli` remains separate and unpublished.

## Limits

- A local run proves only the host platform.
- Linux binaries are static (`CGO_ENABLED=0`). musl/Alpine is not claimed.
- Unsigned macOS binaries downloaded through a browser may be quarantined by Gatekeeper.
- Windows archives are out of scope.
