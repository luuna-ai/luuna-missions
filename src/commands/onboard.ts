import { existsSync } from "node:fs";
import { join } from "node:path";
import type { Command } from "commander";
import { findMissionsDir, projectRootFromMissionsDir } from "../config.ts";
import { upgradeLegacyMarkers } from "../legacy-layout.ts";
import { hasMarkerSection, replaceMarkerSection, wrapInMarkers } from "../markers.ts";
import { outputJson, printError, printSuccess } from "../output.ts";
import { VERSION } from "../version.ts";

// Schema version drives outdated-snippet detection. Bump when the snippet body changes
// in a way agents should re-render. Independent of the package version so patch releases
// don't mark every existing snippet as outdated.
const ONBOARD_SCHEMA = 8;
const SCHEMA_MARKER = `<!-- missions-onboard-schema:${String(ONBOARD_SCHEMA)} -->`;
const VERSION_MARKER = `<!-- missions-onboard:v${VERSION} -->`;
const LEGACY_VERSION_MARKER_PREFIX = "<!-- missions-onboard-v:";

const CANDIDATE_FILES = ["CLAUDE.md", ".claude/CLAUDE.md", "AGENTS.md"] as const;

function onboardSnippet(): string {
	return `## Issue Tracking (Missions)
${VERSION_MARKER}
${SCHEMA_MARKER}

This project uses [Missions](https://github.com/luuna-ai/luuna-missions) v${VERSION} (\`lm\`) for git-native issue tracking. Run \`luuna-missions prime\` at session start for the workflow and live state (in-progress and ready issues), or run \`luuna-missions setup claude\` once to do it automatically.

- \`luuna-missions ready\` — Find unblocked work
- \`luuna-missions update <id> --claim\` — Claim work (atomic)
- \`luuna-missions create --title "..." [--from <id>]\` — File work
- \`luuna-missions close <id>\` — Complete work
- \`luuna-missions plan prompt <id>\` — Decompose large or ambiguous work
- \`luuna-missions sync\` — Commit \`.luuna/missions/\` changes

Never hand-edit \`.luuna/missions/*.jsonl\`; \`luuna-missions\` locks and validates the store.`;
}

function findTargetFile(projectRoot: string): string | null {
	for (const candidate of CANDIDATE_FILES) {
		const fullPath = join(projectRoot, candidate);
		if (existsSync(fullPath)) {
			return fullPath;
		}
	}
	return null;
}

function detectStatus(rawContent: string): "missing" | "current" | "outdated" {
	const content = upgradeLegacyMarkers(rawContent);
	if (!hasMarkerSection(content)) return "missing";
	// Legacy snippets used `missions-onboard-v:N`. Always treat them as outdated so
	// the next run upgrades them to the new schema/version markers.
	if (content.includes(LEGACY_VERSION_MARKER_PREFIX)) return "outdated";
	if (content.includes(SCHEMA_MARKER)) return "current";
	return "outdated";
}

export async function run(args: string[]): Promise<void> {
	const jsonMode = args.includes("--json");
	const stdoutMode = args.includes("--stdout");
	const checkMode = args.includes("--check");

	const missionsDir = await findMissionsDir();
	const projectRoot = projectRootFromMissionsDir(missionsDir);

	const targetPath = findTargetFile(projectRoot);
	const snippet = onboardSnippet();

	// --check mode: report status only
	if (checkMode) {
		if (!targetPath) {
			if (jsonMode) {
				await outputJson({ success: true, command: "onboard", status: "missing", file: null });
			} else {
				console.log("Status: missing (no CLAUDE.md found)");
			}
			return;
		}
		const content = await Bun.file(targetPath).text();
		const status = detectStatus(content);
		if (jsonMode) {
			await outputJson({ success: true, command: "onboard", status, file: targetPath });
		} else {
			console.log(`Status: ${status} (${targetPath})`);
		}
		return;
	}

	// --stdout mode: print what would be written
	if (stdoutMode) {
		process.stdout.write(wrapInMarkers(snippet));
		process.stdout.write("\n");
		return;
	}

	// Default mode: write to file
	const filePath = targetPath ?? join(projectRoot, "CLAUDE.md");
	const fileExists = existsSync(filePath);
	const wrappedSnippet = wrapInMarkers(snippet);

	if (!fileExists) {
		await Bun.write(filePath, `${wrappedSnippet}\n`);
		if (jsonMode) {
			await outputJson({ success: true, command: "onboard", action: "created", file: filePath });
		} else {
			printSuccess(`Created ${filePath} with missions section`);
		}
		return;
	}

	const content = await Bun.file(filePath).text();
	const status = detectStatus(content);

	if (status === "current") {
		if (jsonMode) {
			await outputJson({
				success: true,
				command: "onboard",
				action: "unchanged",
				file: filePath,
			});
		} else {
			printSuccess("Missions section is already up to date");
		}
		return;
	}

	if (status === "outdated") {
		const updated = replaceMarkerSection(content, snippet);
		if (!updated) {
			// Defensive: detectStatus saw markers but replaceMarkerSection refused
			// (e.g. markers out of order, truncated, or duplicated). Surface this
			// instead of silently no-op'ing — agents need to know their snippet is stale.
			const msg = `Found missions markers in ${filePath} but could not replace the section (markers may be malformed, out of order, or duplicated). Fix the file manually or remove the missions:start/missions:end block and re-run.`;
			if (jsonMode) {
				await outputJson({
					success: false,
					command: "onboard",
					action: "failed",
					file: filePath,
					error: msg,
				});
			} else {
				printError(msg);
			}
			process.exitCode = 1;
			return;
		}
		await Bun.write(filePath, updated);
		if (jsonMode) {
			await outputJson({
				success: true,
				command: "onboard",
				action: "updated",
				file: filePath,
			});
		} else {
			printSuccess(`Updated missions section in ${filePath}`);
		}
		return;
	}

	// status === "missing": append
	const separator = content.endsWith("\n") ? "\n" : "\n\n";
	await Bun.write(filePath, `${content}${separator}${wrappedSnippet}\n`);
	if (jsonMode) {
		await outputJson({ success: true, command: "onboard", action: "appended", file: filePath });
	} else {
		printSuccess(`Added missions section to ${filePath}`);
	}
}

export function register(program: Command): void {
	program
		.command("onboard")
		.description("Add missions section to CLAUDE.md / AGENTS.md")
		.option("--stdout", "Print what would be written to stdout")
		.option("--check", "Check status without modifying files")
		.option("--json", "Output as JSON")
		.action(async (opts: { stdout?: boolean; check?: boolean; json?: boolean }) => {
			const args: string[] = [];
			if (opts.stdout) args.push("--stdout");
			if (opts.check) args.push("--check");
			if (opts.json) args.push("--json");
			await run(args);
		});
}
