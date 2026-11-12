# Native CLI specification

Luuna Missions is a Go command-line tool for Git-native tickets and structured work plans. Go 1.22+ builds the executable; `gopkg.in/yaml.v3` supplies YAML parsing. A compiled executable does not need a language runtime.

The entrypoint registers ticket and project handlers in `internal/cli` and plan handlers in `internal/plan`. `internal/model` defines public records, `internal/store` provides JSONL I/O and advisory locks, and `internal/config` discovers projects, parses configuration and migrates legacy layouts.

Public interfaces comprise command arguments, help, exit codes, stdout/stderr, JSON responses and persisted records. Preserve existing ticket and plan schemas. Version metadata identifies the native runtime as `go`; platform identifiers retain their existing spellings. Project workflow guidance recognizes `go.mod` and recommends `go test ./...`.

All mutations use advisory locks and atomic writes. Multi-file plan/issue mutations acquire the plans lock first. JSONL deduplication and unknown extension fields must survive round trips. Diagnostics redact secrets. Project discovery supports ancestor directories and linked Git worktrees. Legacy `.seeds` migration refuses ambiguous dual stores and linked directories.

See [PLAN_SPEC.md](PLAN_SPEC.md) for plan contracts and [docs/CLI-SYSTEM-MAP.md](docs/CLI-SYSTEM-MAP.md) for commands and integrations. `go run ./cmd/dev verify` checks formatting, static analysis, unit/integration tests, 194 captured public CLI cases and the native build. Release artifact tests are a separate shell-based packaging check.
