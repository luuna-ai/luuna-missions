# Luuna Missions CLI and system map

The sole CLI is Go. `cmd/luuna-missions/main.go` registers handlers from `internal/cli` and `internal/plan`. `internal/model`, `internal/store` and `internal/config` own public records, storage and project discovery. `cmd/dev` provides native build and verification commands.

Build with `go run ./cmd/dev build`; executables and aliases appear under `dist/`. Arguments, standard streams, signals and exit codes are handled directly by the native executable. Version JSON reports runtime `go`. Platform labels retain their established spellings for compatibility. Embedded help, shell completions and workflow guidance are part of the public contract.

Missions owns tickets and plans, Atlas expertise, Beacon prompts and Luuna runtime policy. Consumers must update their executable paths and exact component pins in a separate integration.

## Project data and invariants

`init` creates `.luuna/missions/config.yaml`, `issues.jsonl`, `plans.jsonl`, `templates.jsonl`, and a local `.gitignore` for `*.lock`. It adds three `merge=union` lines for the JSONL files to the project's `.gitattributes`. Commands discover the nearest initialized project by walking up from the current directory; a linked Git worktree resolves its main checkout's store. The first access to a legacy `.seeds/` layout may move it to `.luuna/missions/` through `internal/config/legacy.go` and rewrite relevant `.gitattributes`/`.gitignore` references. That migration refuses to merge two stores or follow a linked old/new directory.

| File | Record or purpose | Main owners |
| --- | --- | --- |
| `issues.jsonl` | One issue per line: ID, title, status, type, priority, optional assignee/description/labels/dependencies/plan links/intent/extensions, timestamps | `create`, `update`, `close`, `dep`, `label`, `block`, `unblock`, template pour, plan child operations, Beads migration |
| `plans.jsonl` | Plan ID, parent seed, template, status, revision, sections, ordered children, adopted-child subset, optional outcome/reviewer, timestamps | `plan` family; child status transitions from `update` and `close` |
| `templates.jsonl` | Named issue template with ordered steps; pouring creates a convoy of issues | `tpl` family |
| `config.yaml` | Project name, schema version, nested plan display depth, optional custom plan templates | `init`, `config`, plan validation and prompts |

An issue has required `id`, `title`, `status`, `type`, `priority`, `createdAt`, and `updatedAt`. Optional fields are `assignee`, `description`, `closeReason`, `closedAt`, `blocks`, `blockedBy`, `labels`, `convoy`, `plan_id`, `plan_step_index`, `requires_plan`, `discoveredFrom`, Roots `intent`, and `extensions`. Status is `open`, `in_progress`, or `closed`; type is `task`, `bug`, `feature`, or `epic`; priority is 0 (critical) through 4 (backlog). `blocks` and `blockedBy` are reciprocal issue ID relationships. `discoveredFrom` is provenance, not a blocker. Unknown record fields are preserved. A plan stores `id`, parent `seed`, `template`, `name`, lifecycle `status`, `revision`, validated `sections`, ordered `children`, optional `adoptedChildren`, `outcome`, `outcomeNote`, `reviewedBy`, and timestamps. A template stores `id`, `name`, and ordered `steps`; each step has a title and optional type, priority, and `plan_template`. Config has `project`, `version`, optional `max_plan_depth`, and optional `plan_templates`; `config schema` emits the full JSON Schema.

Writes use advisory per-file `.lock` files, atomic replacement, and JSONL deduplication by ID. Multi-file plan operations acquire the plans lock before the issues lock. JSONL is meant to be mutated through commands, not by hand. IDs, timestamps, human output, JSON field names, and exit behavior are command contracts.

Plan status starts at `approved` after submission or adopt-only creation. A child entering progress makes the plan `active`; all children closed makes it `done`; reopening a child returns it to `active`. A `draft` stays draft, and `outcome` records an assessment without changing status. `plan show` can recurse into child plans up to `max_plan_depth` (default 3). Built-in plan templates are `feature`, `bug`, and `refactor`; custom templates in YAML may override them.

## Full command inventory

Options in the table are command-specific. `--json` is available on most data and mutation commands and yields a stable object with `success` and `command`; list/show/ready/search/blocked/stats also support `--format markdown|compact|plain|ids|json`. `--format` is not a universal option. Top-level flags are `-h/--help`, `-v/--version`, `-q/--quiet` (suppress human status output), `--verbose` (extra diagnostics where supported), and `--timing` (elapsed time on stderr after success); `--version --json` emits version metadata. A command failure returns a nonzero exit and either a JSON error object in JSON mode or an `Error:` line on stderr. Run `<command> --help` (and `<command> <subcommand> --help`) for exact argument ordering and option text.

| Environment variable | Use |
| --- | --- |
| `LUUNA_MISSIONS_GO` | Go executable used by the native developer command and release packaging; defaults to `go` on `PATH` |
| `LUUNA_MISSIONS_DEBUG=1` | Enables debug records for top-level command errors unless a valid explicit log level takes precedence |
| `LUUNA_MISSIONS_LOG_LEVEL` | `trace`, `debug`, `info`, `warn`, `error`, `fatal`, or `silent`; controls those error diagnostics |
| `USER` | Default identity for `update <id> --claim` if `--as` is omitted |

| Command | Use and notable options | Writes / side effects |
| --- | --- | --- |
| `init` | Initialize current project; `--json` | Creates data files and updates `.gitattributes` |
| `create` | Add a ticket; required `--title`; `--type task|bug|feature|epic` (default task), `--priority 0..4` or `P0..P4` (default 2), `--assignee`, `--description`/`--desc`/`--body`, comma-separated `--labels`, `--from`, repeatable `--intent`, `--json` | Appends issue |
| `show <id> [ids...]` | Inspect one or more issues (and linked plan details); `--format`, `--json` | Read only |
| `list` | List/filter issues; `--status`, `--type`, `--assignee`, `--all` (include closed), `--label` (all), `--label-any`, `--unlabeled`, `--priority`, `--priority-max`, `--limit` (default 50), `--sort priority|created|updated|id`, `--format`/`--json` | Read only |
| `ready` | Unblocked open work; the type/assignee/label/priority/limit/sort/format filters from `list`; `--respect-schedule` excludes queued or future scheduled extensions | Read only |
| `search <query>` | Case-insensitive title/description search; status/type/assignee/label/priority/limit/sort/format filters from `list`, without `--all` | Read only |
| `update <id>` | `--status`, `--title`, `--assignee`, `--description`/`--desc`/`--body`, `--type`, `--priority`, `--add-label`, `--remove-label`, `--set-labels`, `--extensions <json>` (shallow merge), `--clear-extensions`, repeatable replacing `--intent`, `--json`; `--claim --as <agent>` atomically claims open work (`USER` fallback) | Rewrites issue; status may transition parent plan |
| `close <id> [ids...]` | Close one or more issues; optional `--reason`, `--json` | Rewrites issues; may transition parent plan |
| `dep add <issue> <depends-on>` | Add reciprocal dependency links; `--json` | Rewrites issues |
| `dep remove <issue> <depends-on>` | Remove reciprocal dependency links; `--json` | Rewrites issues |
| `dep list <issue>` | Show dependencies; `--json` | Read only |
| `label add <issue> <labels...>` | Normalize and append one or more labels; `--json` | Rewrites issue |
| `label remove <issue> <labels...>` | Remove one or more labels; `--json` | Rewrites issue |
| `label list <issue>` | Show labels on one issue; `--json` | Read only |
| `label list-all` | Show labels across issues; `--json` | Read only |
| `blocked` | List blocked issues; `--format`, `--json` | Read only |
| `stats` | Aggregate project issue counts; `--format`, `--json` | Read only |
| `sync` | `--status` inspects changes, `--dry-run` previews, `--json`; default stages `.luuna/missions/` and runs `git commit`; skips commit from a Git worktree | Default commits the current index, including unrelated paths already staged |
| `doctor` | Check config, issue/template JSONL integrity and duplicates, issue schema, dependencies, labels, extensions, closed metadata, locks, and Git attributes; `--fix`, `--verbose`, `--json` | `--fix` may remove malformed JSONL lines, repair issue/template records and `.gitattributes`, and remove stale locks |
| `tpl create` | Create an issue template; required `--name`, optional `--json` | Appends template |
| `tpl step add <id>` | Add ordered template step; required `--title`, optional `--type`/`--priority`, `--json` | Rewrites template |
| `tpl list` | List issue templates; `--json` | Read only |
| `tpl show <id>` | Inspect one issue template; `--json` | Read only |
| `tpl pour <id>` | Instantiate template steps as linked issues; required `--prefix`, optional `--json` | Creates issues/convoy |
| `tpl status <id>` | Inspect convoy status; `--json` | Read only |
| `migrate-from-beads` | Import `.beads/issues.jsonl` and skip already-present IDs; `--json` | Adds issues |
| `prime` | Emit agent workflow context and live work state; `--compact`, `--export`, `--json` | Read only |
| `onboard` | Add/update a marked Missions snippet in `CLAUDE.md`, `.claude/CLAUDE.md`, or `AGENTS.md`; `--check`, `--stdout`, `--json` | Default edits one guidance file |
| `upgrade` | `--check`, `--json`; self-upgrade is disabled while unpublished and returns an unavailable error | No registry update |
| `completions <shell>` | Print bash, zsh, or fish completion script | Read only |
| `block <id> --by <blocker-id>` | Add one blocker; `--json` | Rewrites issues |
| `unblock <id>` | `--from <blocker-id>` or `--all` closed blockers; `--json` | Rewrites issues |
| `plan` | Structured plan subcommands below | Depends on subcommand |
| `config schema` | Emit full config JSON Schema; `--json` selects compact one-line output (default pretty JSON) | Read only |
| `config show` | Show all YAML config or `--path <dot.path>`; `--json` | Read only |
| `config set <path> <value>` | YAML-parse and schema-validate the value; `--json` | Atomically writes `config.yaml` |
| `config unset <path>` | Remove a config field after validation; `--json` | Atomically writes `config.yaml` |
| `setup claude` | Install or `--remove` Missions-owned Claude hooks; `--json` | Edits `.claude/settings.json` |
| `guard` | Claude `PreToolUse` hook; reads JSON from stdin and denies supported direct edits to managed JSONL | No project write; hook decision on stdout |

### Plan subcommands

Every plan leaf accepts `--json`; commands that take `<id>` generally accept either a plan ID or its parent seed ID. The leaf help text remains the exact option and argument reference.

| Command | Use and important options | Writes / effects |
| --- | --- | --- |
| `plan templates` | List built-in plus configured plan templates | Read only |
| `plan prompt <seed-id>` | Emit a template-shaped planning request; `--template`, `--domain`; optional Atlas prior art | Read only; optional Atlas queries |
| `plan submit <seed-id>` | Validate plan JSON via required `--plan <file|->` (stdin with `-`); `--overwrite`, `--name`, `--record-decision`, `--domain` | Writes plan, parent and child issues; optional Atlas decision write after success |
| `plan show <id>` | Show plan by plan ID or seed ID with child summaries and nested plans | Read only |
| `plan validate <id>` | Recheck stored sections against current template; emits structured diff on stderr when invalid | Read only |
| `plan outcome <id>` | Required `--result success|partial|failure`, optional `--note`; warns about open children | Updates plan assessment only |
| `plan review <id>` | Required `--by <name>`; reviewer is informational | Updates plan |
| `plan edit <id>` | `--name`, text `--section <name> <text>`, or `--step <i>` with `--title`/`--priority`/`--type`; bumps revision and propagates step metadata | Rewrites plan and possibly child issue |
| `plan create <seed-id>` | Adopt-only approved plan with zero children; `--name`, `--template` | Writes plan and parent link |
| `plan adopt <plan-id> <seed-ids...>` | Attach existing open missions; optional `--step`, one of `--at`/`--before`/`--after` | Writes plan and issues; keeps seed status/type/priority |
| `plan reorder <plan-id> <seed-ids...>` | Require an exact permutation of existing children | Rewrites plan order only |
| `plan release <plan-id> <seed-ids...>` | Detach children without closing them | Rewrites plan and issues |
| `plan list` | Filter by `--seed`, `--status`, `--outcome`, `--template` | Read only |

The plan submission shape is `{ "template": "feature", "name": "optional label", "sections": { ... } }`. `plan prompt` wraps the form in a `plan_request` envelope; remove that envelope when submitting. Submit validates text/list/step shapes and step dependencies before writing. A step may spawn a child from `title` or adopt an existing child with `existing_seed`. `blocks` contains 1-based step indices, while stored `plan_step_index` is 0-based. Step `labels` are normalized and merged onto adopted children. Child descriptions carry a marked plan back-reference; releasing a child strips that marked block while preserving manual description text.

## Typical workflow and integrations

Run ticket commands from an initialized project, not from this component's own checkout. For example:

```bash
luuna-missions init
luuna-missions create --title "Add import validation" --type feature --priority P1 --json
luuna-missions ready --json
luuna-missions show <issue-id> --json
luuna-missions update <issue-id> --claim --as <agent> --json
luuna-missions close <issue-id> --reason "Verified" --json
```

1. `luuna-missions init` in a Git project, then `create --title ...` to capture work.
2. `ready --json` or `list --json` to select work. `update <id> --claim --as <agent>` claims it atomically; `close <id>` finishes it. Dependencies and plan drafts affect readiness.
3. For larger work, create a parent issue, run `plan prompt <id> --json`, fill the resulting plan template, then `plan submit <id> --plan plan.json --json`. Use `plan show`, `validate`, `edit`, `adopt`, `reorder`, `release`, `review`, and `outcome` as the work evolves.
4. `prime` emits context for agents. `setup claude` installs a SessionStart `luuna-missions prime --compact` hook and a PreToolUse `luuna-missions guard` hook with a `Write|Edit|MultiEdit|NotebookEdit` matcher. The guard reads Claude's hook JSON on stdin and emits a deny decision for direct edits to `.luuna/missions/*.jsonl`; other tools and targets pass through. `onboard` writes a marked Missions block in a project instruction file without owning the rest of that file.
5. `sync --status` or `sync --dry-run` shows Missions changes; plain `sync` stages Missions files and commits the current index. Run `git diff --cached --name-only` to inspect unrelated staged paths first. Luuna's policy and approval mechanism is outside this component.

Atlas enrichment is optional: `plan prompt` can query `luuna-atlas` for matching domain records; `plan submit --record-decision` may write an Atlas decision after the local plan is saved. Failures do not roll back the local plan. Beacon has no direct data store integration in this component. `migrate-from-beads` is an explicit import from a prior issue tracker; legacy `.seeds/` layout movement is a separate automatic compatibility path.

## Development and evidence boundaries

Verification uses `go run ./cmd/dev verify`: formatting, static analysis, Go tests, 194 captured compatibility cases and native builds. Cases normalize generated IDs, fixture paths, line endings and millisecond timestamps; JSON values and ordered JSONL records are compared semantically. Other output and files are compared exactly. The corpus covers representative success, failure, mutation and help paths, not every possible input. Optional Atlas tests use a native test executable.

CI declares the same surface on Linux, Windows and macOS. Platform claims require actual runner results. Native release artifact checks are separate; see [RELEASING.md](RELEASING.md).
