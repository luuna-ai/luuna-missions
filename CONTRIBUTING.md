# Contributing

Use Go 1.22+ and Git. Read [AGENTS.md](AGENTS.md) before editing.

Run `go mod download`, make a focused change, format it with `gofmt -w`, then run `go run ./cmd/dev verify`. Tests use real temporary files and Git repositories. CLI changes need public-boundary tests, including regression fixture updates where output changes intentionally.

Preserve record formats, lock order, atomic writes, deduplication, secret redaction, and command contracts. Keep optional Atlas subprocess behavior graceful when Atlas is unavailable. Do not change runtime policy or component pins in the parent project. Do not install Git hooks automatically.

Build with `go run ./cmd/dev build`; native executables and aliases appear in `dist/`. See [docs/RELEASING.md](docs/RELEASING.md) for release artifact checks. Do not commit, push or publish without explicit authorization.
