# Changelog

## Native CLI migration

- Native source is the sole CLI implementation.
- Build, verification, aliases, CI and development-container configuration use Go.
- The 194-case CLI compatibility corpus runs as Go tests with captured expected output and file state.
- Version JSON identifies the runtime as `go`.
- Generated workflow guidance recognizes native Go projects.
- Release tooling reads the compatibility version from Go source.
- Existing ticket/plan schemas, store layout, locking and migration behavior remain unchanged.

See [MIGRATION.md](MIGRATION.md) for extraction and storage-layout history and [UPSTREAM.md](UPSTREAM.md) for provenance.
