# Luuna Missions development

This component is a maintained Go fork of Missions, with provenance in `UPSTREAM.md` and the original MIT notice in `LICENSE`.

Use Go 1.22 or newer and Git. From this directory:

```sh
go mod download
go run ./cmd/dev verify
go run ./cmd/dev build
```

The CLI lives under `cmd/luuna-missions/`, `internal/cli/`, and `internal/plan/`. The developer command under `cmd/dev/` checks formatting, runs static analysis and all Go tests, then builds native executables. `internal/regression/` replays 194 captured CLI contracts with real temporary Git repositories, checking exit codes, both output streams, JSON values, ordered JSONL records, and resulting files. Update relevant public-boundary regression cases when behavior changes. Use Go formatting and module conventions.

Preserve command names, help, exit codes, JSON output, record schemas and public types unless a migration is explicitly designed and tested. Preserve advisory locks, atomic writes, JSONL deduplication and secret redaction. Multi-file plan and issue mutations take the plans lock before the issues lock. Do not hand-edit project JSONL.

Primary executable: `luuna-missions` (`lms`; compatibility aliases `lm` and `missions`). Missions owns Git-native tickets and structured plans. Atlas owns structured expertise and project memory; Beacon owns reusable prompts. Project data lives in `.luuna/missions/` (created by `init`); environment variables use the `LUUNA_MISSIONS_` prefix. Legacy layout migration is confined to `internal/config/legacy.go`. Runtime policy and approval enforcement belong in Luuna.

Do not load imported dogfooding stores, session histories or upstream agent skills as Luuna work records. Do not create them as a development ritual. Installations must not alter root Git hook configuration. This is a public component maintained in luuna-ai/luuna-missions. Registry publication and self-upgrades require a separate reviewed release process. Luuna consumes exact Git commits through submodules; changes here do not authorize changes to Luuna runtime policy or its pin.

Do not commit or push unless explicitly requested. Report remaining work without automatically creating issues or synchronizing component state.
