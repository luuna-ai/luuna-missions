# Upstream provenance

- Original project: Seeds
- Original package: @os-eco/seeds-cli
- Upstream repository: https://github.com/jayminwest/seeds
- Imported upstream version: 0.6.0
- Import source: the tracked seeds/ tree in the original Luuna core repository at commit 532048d1839d003d724e55e8a47dbfdc44145004 (see repository history for the full tree).
- Luuna identity: @luuna/missions-cli, in components/missions/.
- Upstream copyright holder and author: Jaymin West, as retained in LICENSE and package authorship.

The original MIT LICENSE is preserved verbatim. Luuna modifications are also licensed under MIT by Luuna contributors. Original version numbers remain the compatibility baseline; a distinct Luuna release version must be chosen before publication.

Luuna changes include branding, private package metadata, safe installation without Git-hook mutation, current agent guidance, and removal of imported authoring/session records. Storage formats, historical command aliases, exported type names, environment variables, schema fields, and integration markers retain upstream compatibility. No rewrite of the implementation is implied.

This maintained distribution is currently published at https://github.com/luuna-ai/luuna-missions. Luuna consumes it as an exact-commit Git submodule. The package remains private and unpublished. The component-scoped history was extracted with git subtree split; the original pre-branding history remains in the original Luuna core repository at the import source recorded above. See MIGRATION.md.

Imported agent instructions and authoring metadata were preserved locally before removal from the working tree. They must not be loaded as Luuna project state. Historical documentation remains reference material; current component development starts with this repository's AGENTS.md. Luuna runtime policy remains in the Luuna repository.

Layout note (October 22, 2026): the on-disk project directory, bin aliases and environment-variable prefix were later renamed to the Luuna layout (`.luuna/missions/`, `luuna-missions`/`lm`, `LUUNA_MISSIONS_*`). The JSONL record format remains compatible with upstream; see MIGRATION.md and CHANGELOG.md.
