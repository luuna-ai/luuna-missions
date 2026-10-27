# Luuna Missions development

This component is a maintained fork of Missions, with provenance in `UPSTREAM.md` and the original MIT notice in `LICENSE`.

Use Bun 1.4.3 and Go 1.22 or newer. Put `go` on `PATH` or set `LUUNA_MISSIONS_GO` to its executable. From this directory:

```bash
bun install --frozen-lockfile --ignore-scripts
bun run typecheck
bun run check:agents
bun test --timeout 30000
bun run verify
```

The live CLI is Go source under `cmd/` and `internal/`. `src/index.ts` is the Bun compatibility launcher for the package bins and Luuna's pinned entry; it builds a source-keyed native binary under ignored `dist/`. `src/index-legacy.ts` and the remaining TypeScript modules are the behavior oracle. Keep strict TypeScript for that oracle and launcher, Go formatting and module conventions for the native implementation, and stable command names, help, exit codes, JSON output, record schemas, and public types. Tests use Bun and Go with real temporary files. Test changes at the appropriate public boundary, including the differential corpus in `scripts/parity.mjs` when command behavior changes.

Preserve advisory locks, atomic writes, JSONL deduplication, and secret redaction. Multi-file plan and issue mutations take the plans lock before the issues lock. Do not hand-edit project JSONL. `bun run verify` is the supported component validation surface: type check, instruction check, Bun tests, Go tests, and the full TypeScript-versus-Go CLI corpus. Inherited `check:all` and quality-budget scripts remain for upstream maintenance comparison; they reference standalone upstream CI and are not the supported CI contract.

Primary executable: `luuna-missions` (`lms`; compatibility alias `lm`). This component owns Git-native tickets and structured plans. Atlas owns structured expertise and project memory; Beacon owns reusable prompts. Project data lives in .luuna/missions/ (created by `init`); environment variables use the `LUUNA_MISSIONS_` prefix. Legacy layout migration is confined to `internal/config/legacy.go` in Go and `src/legacy-layout.ts` in the TypeScript oracle. Preserve configuration keys, record schemas, and public types unless an explicit migration is designed and tested. Runtime policy and approval enforcement belong in Luuna, not in this component.

Do not load imported dogfooding stores, session histories, or upstream agent skills as Luuna work records. Do not create them as a development ritual. Installations must not alter the root Git hook configuration. This is a public Luuna component maintained in the luuna-ai/luuna-missions repository. Packages remain unpublished; registry publication and upstream self-upgrades require a separate reviewed release process. Luuna consumes exact Git commits through submodules; changes here do not authorize changes to Luuna runtime policy or update its pin.

Do not commit or push unless explicitly requested. Report remaining work without automatically creating issues or synchronizing component state.
