# Repository extraction, October 21, 2026

At extraction time, the maintainer was brodynelly and the initial destination was https://github.com/brodynelly/luuna-missions. That destination is historical; the current canonical repository is https://github.com/luuna-ai/luuna-missions.

- Source: brodynelly/luuna at `fd42d59c5f608b9120d9cdc2e2354b66f945a5ed`, path `components/missions`.
- Component-scoped history: `git subtree split --prefix=components/missions fd42d59c5f608b9120d9cdc2e2354b66f945a5ed` produced `30f32af42223e8fab46f6e3e0e9272bfe11354cc`.
- The extracted baseline tree exactly matches the source subtree before independent-repository metadata changes.
- Original upstream provenance and pre-branding import revision remain in UPSTREAM.md; the complete earlier monorepo history stays in Luuna.
- Source, data formats, aliases, dependencies and lockfile are unchanged by extraction. LICENSE SHA-256: `9d36b754124311e7363b9f800e147f95b4dcaea008aa3565b70e15b8a7462f0a`.
- This commit adds standalone ownership, documentation and CI. Luuna consumes the exact reviewed Git commit as a submodule at the existing path.
- No registry package is published. GitHub access supplies source retrieval; npm release credentials are intentionally unnecessary.

Visibility: public, explicitly authorized by the owner on October 21, 2026. Luuna remains private. No private authoring records or credential files are included in the extracted history.

The owner requested the explicit luuna-* repository names on October 21, 2026. GitHub repository identity, history and CI are preserved by rename; the original canonical destination was brodynelly/luuna-missions; the current canonical destination is luuna-ai/luuna-missions.

# Layout move to .luuna/missions, October 22, 2026

- Project data directory: `.seeds/` is now `.luuna/missions/`. File names inside (`issues.jsonl`, `plans.jsonl`, `templates.jsonl`, `config.yaml`, `.gitignore`) and the JSONL record format are unchanged.
- Automatic migration: when a command needs the store (including `init`) and `.luuna/missions/` is absent while `.seeds/` exists at the project root, the directory is renamed and a single notice is written to stderr (stdout stays machine-readable). If both exist the command fails with an error asking you to resolve it by hand. A symlinked `.luuna` or `.seeds` is refused.
- Managed `.gitattributes` and `.gitignore` lines that named `.seeds/` are rewritten to `.luuna/missions/`. Commit the rename together with those files.
- Removed bin aliases `sd` and `seeds`; use `luuna-missions` or `lms` (`lm` remains available). Re-run `luuna-missions setup claude` to refresh hook commands (older `sd prime` / `sd guard` hooks are replaced).
- Environment variables: `SEEDS_DEBUG` and `SEEDS_LOG_LEVEL` are now `LUUNA_MISSIONS_DEBUG` and `LUUNA_MISSIONS_LOG_LEVEL`. The old names are no longer read.
- Onboard and plan-backref markers changed from `seeds:*` / `seeds-onboard-*` to `missions:*` / `missions-onboard-*`; existing sections are re-written by `onboard`.
- Plan-template section key `mulch_source` in `config.yaml` is now `atlas_source`; the old key is still accepted and is rewritten when the config is saved with `luuna-missions config set`. Prior-art enrichment and `--record-decision` call `luuna-atlas` instead of `ml`.
