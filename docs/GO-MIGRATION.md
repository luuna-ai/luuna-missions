# Native migration review

The CLI, development commands and automated CLI regression suite are implemented in Go. The previous implementation, source launcher, dependency manifests, dependency installation directory, compiler/linter configuration and script-based quality tooling have been retired. Documentation, agent instructions, development-container configuration and CI now use native commands.

| Area | Result |
| --- | --- |
| CLI commands | Native handlers retain ticket, dependency, label, template, config, import, setup, guard, completion, sync and plan operations |
| Executables | Local build writes `luuna-missions`, `lms`, `lm`, and `missions`; compiled executables run directly |
| Records and storage | Existing JSONL/YAML schemas, unknown-field preservation, lock order, atomic writes, deduplication and legacy layout migration retained |
| Compatibility contracts | All 194 differential steps passed before retirement; captured cases now replay as native tests and compare streams, exits and final files |
| Intentional output updates | Version metadata reports runtime `go`; upgrade help points to reviewed source updates; Go projects receive `go test ./...` workflow guidance |
| Build and verification | `go run ./cmd/dev build` and `go run ./cmd/dev verify`; the latter checks formatting, static analysis, all tests and build |
| Releases | Packaging reads the internal version from Go source; existing deterministic archives, attribution, checksums and provenance retained |
| Consumers | Parent application executable paths and exact submodule pins need a separate integration; they were not changed here |

Verification performed during migration:

- Windows amd64, Go 1.27.0: complete developer verification, including all 194 CLI cases and native executable/alias builds.
- Linux amd64 under WSL, Go 1.22.12: complete developer verification, including the Linux broken-pipe regression and all 194 CLI cases.
- Linux shell packaging: all 73 offline release-script checks passed.
- Linux native archive: built and passed `check-archive.sh --require-run`, including checksums, entry validation and host execution through both archive executable names.
- Repository source/configuration/documentation scan found no retired runtime or launcher references. Markdown local links resolve.

The final corpus is a captured compatibility baseline, not a second live implementation. It does not prove every possible CLI input. macOS execution and Windows release archives were not verified. Component CI is configured for Windows, Linux and macOS; no remote workflow was triggered. No commit, push, publication, installed application change or parent pin update was performed. The provided folder has no Git metadata, so no Git diff was available.
