import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { runCli } from "../test-harness.ts";
import { VERSION } from "../version.ts";

let tmpDir: string;

async function run(
	args: string[],
	cwd: string,
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
	return runCli(args, cwd);
}

async function initMissions(cwd: string): Promise<void> {
	await run(["init"], cwd);
}

beforeEach(async () => {
	tmpDir = await mkdtemp(join(tmpdir(), "missions-onboard-test-"));
});

afterEach(async () => {
	await rm(tmpDir, { recursive: true, force: true });
});

describe("luuna-missions onboard", () => {
	test("fails without .luuna/missions/ initialized", async () => {
		const { exitCode, stderr } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(1);
		expect(stderr).toContain("Not in a missions project");
	});

	test("creates CLAUDE.md when no target file exists", async () => {
		await initMissions(tmpDir);
		const { exitCode } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(0);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("<!-- missions:start -->");
		expect(content).toContain("<!-- missions:end -->");
		expect(content).toContain("Issue Tracking (Missions)");
		expect(content).toContain("luuna-missions prime");
	});

	test("appends to existing CLAUDE.md", async () => {
		await initMissions(tmpDir);
		await Bun.write(join(tmpDir, "CLAUDE.md"), "# My Project\n\nExisting content.\n");
		const { exitCode } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(0);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("# My Project");
		expect(content).toContain("Existing content.");
		expect(content).toContain("<!-- missions:start -->");
		expect(content).toContain("Issue Tracking (Missions)");
	});

	test("is idempotent — second onboard does not duplicate", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const first = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		await run(["onboard"], tmpDir);
		const second = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(second).toBe(first);
	});

	test("--check reports missing when no file exists", async () => {
		await initMissions(tmpDir);
		const { stdout, exitCode } = await run(["onboard", "--check"], tmpDir);
		expect(exitCode).toBe(0);
		expect(stdout).toContain("missing");
	});

	test("--check reports current after onboard", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const { stdout, exitCode } = await run(["onboard", "--check"], tmpDir);
		expect(exitCode).toBe(0);
		expect(stdout).toContain("current");
	});

	test("--check with --json returns structured output", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const { stdout, exitCode } = await run(["onboard", "--check", "--json"], tmpDir);
		expect(exitCode).toBe(0);
		const result = JSON.parse(stdout) as { success: boolean; status: string };
		expect(result.success).toBe(true);
		expect(result.status).toBe("current");
	});

	test("--stdout prints snippet without writing", async () => {
		await initMissions(tmpDir);
		const { stdout, exitCode } = await run(["onboard", "--stdout"], tmpDir);
		expect(exitCode).toBe(0);
		expect(stdout).toContain("<!-- missions:start -->");
		expect(stdout).toContain("Issue Tracking (Missions)");
		// Should not have created the file
		const exists = await Bun.file(join(tmpDir, "CLAUDE.md")).exists();
		expect(exists).toBe(false);
	});

	test("detects existing CLAUDE.md in .claude/ subdirectory", async () => {
		await initMissions(tmpDir);
		const claudeDir = join(tmpDir, ".claude");
		await Bun.write(join(claudeDir, "CLAUDE.md"), "# Agent Instructions\n");
		const { exitCode } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(0);
		const content = await Bun.file(join(claudeDir, "CLAUDE.md")).text();
		expect(content).toContain("<!-- missions:start -->");
		// Root CLAUDE.md should NOT have been created
		const rootExists = await Bun.file(join(tmpDir, "CLAUDE.md")).exists();
		expect(rootExists).toBe(false);
	});

	test("upgrades legacy missions-onboard-v:N marker to schema + version markers", async () => {
		await initMissions(tmpDir);
		// Write a missions section with the legacy version marker
		const oldContent =
			"# Project\n\n<!-- missions:start -->\n## Old Missions Section\n<!-- missions-onboard-v:0 -->\nold content\n<!-- missions:end -->\n";
		await Bun.write(join(tmpDir, "CLAUDE.md"), oldContent);
		const { exitCode } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(0);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("# Project");
		expect(content).toContain("missions-onboard-schema:");
		expect(content).toContain(`missions-onboard:v${VERSION}`);
		expect(content).not.toContain("missions-onboard-v:0");
		expect(content).not.toContain("Old Missions Section");
	});

	test("updates outdated section when schema version changes", async () => {
		await initMissions(tmpDir);
		// Write a missions section with a stale schema marker
		const oldContent =
			"# Project\n\n<!-- missions:start -->\n## Old Missions Section\n<!-- missions-onboard-schema:1 -->\nold content\n<!-- missions:end -->\n";
		await Bun.write(join(tmpDir, "CLAUDE.md"), oldContent);
		const { exitCode } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(0);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("# Project");
		expect(content).not.toContain("missions-onboard-schema:1");
		expect(content).not.toContain("Old Missions Section");
		expect(content).toContain("Issue Tracking (Missions)");
	});

	test("--json output on create", async () => {
		await initMissions(tmpDir);
		const { stdout, exitCode } = await run(["onboard", "--json"], tmpDir);
		expect(exitCode).toBe(0);
		const result = JSON.parse(stdout) as { success: boolean; action: string };
		expect(result.success).toBe(true);
		expect(result.action).toBe("created");
	});

	test("includes schema marker in output", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("missions-onboard-schema:");
	});

	test("includes package version in marker and body text", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain(`<!-- missions-onboard:v${VERSION} -->`);
		expect(content).toContain(
			`Missions](https://github.com/luuna-ai/luuna-missions) v${VERSION}`,
		);
	});

	test("snippet stays short: claim, plan entry point, setup hook, no hand edits", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("luuna-missions update <id> --claim");
		expect(content).not.toContain("--status in_progress");
		expect(content).toContain("luuna-missions plan prompt");
		expect(content).toContain("luuna-missions setup claude");
		expect(content).toContain(".luuna/missions/*.jsonl");
		expect(content.split("\n").length).toBeLessThan(25);
	});

	test("surfaces error when markers are out of order (replaceMarkerSection returns null)", async () => {
		await initMissions(tmpDir);
		// Hand-edited broken state: END_MARKER appears before START_MARKER, plus a
		// legacy version marker so detectStatus returns "outdated" and we hit the
		// replaceMarkerSection branch.
		const broken =
			"# Project\n\n<!-- missions:end -->\nbogus\n<!-- missions:start -->\n<!-- missions-onboard-v:0 -->\n";
		await Bun.write(join(tmpDir, "CLAUDE.md"), broken);
		const { exitCode, stderr } = await run(["onboard"], tmpDir);
		expect(exitCode).toBe(1);
		expect(stderr).toContain("could not replace");
		// File must remain untouched.
		const after = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(after).toBe(broken);
	});

	test("surfaces error via --json when markers are out of order", async () => {
		await initMissions(tmpDir);
		const broken =
			"# Project\n\n<!-- missions:end -->\nbogus\n<!-- missions:start -->\n<!-- missions-onboard-v:0 -->\n";
		await Bun.write(join(tmpDir, "CLAUDE.md"), broken);
		const { exitCode, stdout } = await run(["onboard", "--json"], tmpDir);
		expect(exitCode).toBe(1);
		const result = JSON.parse(stdout) as { success: boolean; action: string; error: string };
		expect(result.success).toBe(false);
		expect(result.action).toBe("failed");
		expect(result.error).toContain("could not replace");
	});

	test("schema marker is at version 7 (pi extension removed)", async () => {
		await initMissions(tmpDir);
		await run(["onboard"], tmpDir);
		const content = await Bun.file(join(tmpDir, "CLAUDE.md")).text();
		expect(content).toContain("<!-- missions-onboard-schema:8 -->");
	});
});
