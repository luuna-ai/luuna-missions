# Maintainer runbook

1. Read [AGENTS.md](AGENTS.md); inspect local work before changes.
2. Run `go run ./cmd/dev verify` from the component root.
3. Build native executables using `go run ./cmd/dev build`.
4. Smoke-test `dist/luuna-missions --help` and `--version --json` (use `.exe` on Windows).
5. For packaging, follow [docs/RELEASING.md](docs/RELEASING.md). Publication requires separate approval.

Do not initialize development work records or synchronize user data as a maintenance ritual. Do not alter root hook configuration or the parent application's component pin. Keep source data and installed/live executables intact.
