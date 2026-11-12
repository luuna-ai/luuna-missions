# Captured CLI contracts

The 194 JSON fixtures were captured while running the previous differential corpus against both implementations; all 194 comparisons passed before capture was accepted. Tests now replay those contracts directly against a native Go executable, with no second implementation required.

Each fixture contains arguments/stdin, initial project files, expected stdout/stderr/exit code, expected final files and generated-ID bindings. Captured machine paths and process environments have been removed. Every case runs in its own temporary Git project. The Atlas test dependency is another Go executable under `testdata/atlas`.

Intentional migration updates to the baseline are limited to native runtime metadata, updated upgrade help and Go-project verification guidance. Maintain assertions deliberately; do not regenerate expected output from the implementation as part of verification. Native path separators are normalized only in absolute fixture paths; platform metadata is checked against the executing host. JSONL record order is significant. The explicit `list --sort id` case compares its issue set after normalization because generated IDs differ; its ordering is checked separately.

Run `go test ./internal/regression -count=1` or the full `go run ./cmd/dev verify`.
