# Luuna Missions

Git-native tickets and structured work plans. This is Luuna's maintained MIT-licensed fork of Seeds, imported from upstream version **0.6.0**. The Luuna package identity is **@luuna/missions-cli** in the independent public repository [luuna-ai/luuna-missions](https://github.com/luuna-ai/luuna-missions). The package remains private and unpublished.

## Run from a checkout

Use Bun **1.4.3** and Go **1.22 or newer**. Keep `go` on `PATH`, or set `LUUNA_MISSIONS_GO` to the Go executable. The package bin at `src/index.ts` is a Bun compatibility launcher: it builds the matching native executable on first use, caches it under `dist/`, and forwards arguments, standard streams, signals, and the exit code to Go.

```bash
bun install --frozen-lockfile --ignore-scripts
bun src/index.ts --help
bun run verify
```

`bun run build:native` builds the native cache without running a command. A compiled executable can also run directly without Bun; `go run ./cmd/luuna-missions --help` runs it from source. The cache is local and ignored by Git, so a fresh checkout or package install needs Go for the first launch.

The primary command is `luuna-missions`. Package bin aliases are `lms`, `lm` (compatibility), and `missions`; the consuming Luuna launcher exposes `luuna missions <args>`. Its current pinned Bun entrypoint can continue to use the shim. Updating Luuna's exact submodule pin and installer is a separate reviewed integration, and changes in this repository do not update that consumer automatically.

## What it manages

`init` creates `.luuna/missions/` with `issues.jsonl`, `plans.jsonl`, `templates.jsonl`, and `config.yaml`. Use `create`, `ready`, `update --claim`, and `close` for tickets; `dep`, `label`, `block`, and `unblock` manage relationships; `plan` manages structured plans and child tickets. `tpl` creates reusable issue convoys. `config`, `doctor`, `sync`, `prime`, `onboard`, `setup claude`, `guard`, and `completions` provide project and agent tooling. Run `luuna-missions --help` or see the [full CLI and system map](docs/CLI-SYSTEM-MAP.md) for every command, option family, side effect, and integration.

Project discovery walks upward from the working directory. A legacy `.seeds/` directory may be moved to `.luuna/missions/` on first access; see [MIGRATION.md](MIGRATION.md). Environment variables use the `LUUNA_MISSIONS_` prefix. The Go implementation preserves the JSON and JSONL contracts, advisory locks, atomic writes, and existing project schemas. Luuna owns runtime policy and approval enforcement; Atlas supplies optional plan prior art and decision recording, while Beacon owns reusable prompts.

`sync` stages `.luuna/missions/` and commits the current Git index, including unrelated paths that were already staged. Use `git diff --cached --name-only` to inspect other staged paths before committing. Self-upgrade remains disabled while the package is unpublished. Never replace this fork with a similarly named registry package; update through a reviewed commit and an exact Luuna submodule pin. Dependency installation does not alter root Git hooks.

## Development and releases

The runtime is in `cmd/` and `internal/`. `src/index-legacy.ts` and the TypeScript modules under `src/` remain as the compatibility oracle for the differential test corpus. `bun run verify` runs the TypeScript type check, instruction check, Bun tests, Go tests, and Go-versus-TypeScript CLI parity cases. CI declares this surface on Linux and Windows using Bun 1.4.3 and the Go version in `go.mod`. See [AGENTS.md](AGENTS.md) for repository rules and [the system map](docs/CLI-SYSTEM-MAP.md) for validation limits.

The repository is maintained by the luuna-ai organization. Registry publishing is disabled and requires no npm token. Luuna pins a reviewed commit, never a floating branch; update its submodule only after CI and a consumer smoke pass. Read [UPSTREAM.md](UPSTREAM.md) for provenance and [the original README](docs/UPSTREAM-README.md) for inherited command examples. Upstream release/runbook documents are historical references and do not authorize publication.

## Origin and attribution

Luuna Missions is derived from [Seeds](https://github.com/jayminwest/seeds) 0.6.0 by Jaymin West (MIT). The original license is preserved in [LICENSE](LICENSE), and provenance is recorded in [UPSTREAM.md](UPSTREAM.md). Seeds is independent; Luuna Missions is not affiliated with or endorsed by its authors.
