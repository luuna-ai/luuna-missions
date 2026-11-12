# Luuna Missions

Git-native tickets and structured work plans, implemented entirely in Go. Derived from Seeds 0.6.0 by Jaymin West, with the original MIT notice preserved in [LICENSE](LICENSE) and provenance in [UPSTREAM.md](UPSTREAM.md).

## Install and run

Build from this checkout with Go 1.22 or newer:

```sh
go mod download
go run ./cmd/dev build
```

The build produces `luuna-missions`, `lms`, `lm`, and `missions` in `dist/` (with `.exe` on Windows). Add that directory to PATH or copy the executables to your preferred bin directory. Compiled executables need no language runtime. Git is required for project discovery and Git operations; Atlas is optional for plan enrichment.

Alternatively, install the primary executable directly:

```sh
go install ./cmd/luuna-missions
luuna-missions --help
```

For a source-only invocation: `go run ./cmd/luuna-missions --help`. In exported source folders without Git metadata, use `go run -buildvcs=false ./cmd/luuna-missions --help` or the developer build above.

## Work with tickets and plans

```sh
luuna-missions init
luuna-missions create --title "Implement feature" --type feature --json
luuna-missions ready --json
luuna-missions show <id> --json
luuna-missions update <id> --claim
luuna-missions plan prompt <id> --json
luuna-missions close <id>
```

`init` creates `.luuna/missions/` with issues, plans, templates and YAML configuration. Commands preserve locking, atomic writes, deduplication and existing record formats. Project discovery walks upward; legacy `.seeds/` migration is described in [MIGRATION.md](MIGRATION.md). Environment variables use `LUUNA_MISSIONS_`.

Run `--help` on any command for its options. See [the CLI system map](docs/CLI-SYSTEM-MAP.md) for command families and side effects. `sync` stages component data and commits the current Git index, including unrelated paths already staged; inspect `git diff --cached --name-only` first. Self-upgrade is disabled; update through a reviewed source revision.

## Development

```sh
go run ./cmd/dev verify
```

Verification checks formatting, runs `go vet`, all Go tests (including 194 public CLI compatibility cases), and builds the native executables and aliases. See [AGENTS.md](AGENTS.md) and [CONTRIBUTING.md](CONTRIBUTING.md). CI runs this command on Linux, Windows and macOS; a workflow declaration alone does not establish a platform test result.

The native CLI reports `runtime: "go"` in version JSON. Workflow guidance discovers `go.mod` and recommends `go test ./...`. These are intentional updates to runtime metadata and generated development guidance; ticket and plan schemas remain unchanged.

Luuna owns runtime policy; Atlas owns expertise and Beacon owns prompts. Consumers must invoke the compiled executable. Updating the parent Luuna installer and exact submodule pin is a separate integration; this repository does not update them automatically. Publication remains a separate reviewed release process. See [release documentation](docs/RELEASING.md).
