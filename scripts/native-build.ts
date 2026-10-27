import { createHash, randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, renameSync, rmSync } from "node:fs";
import { basename, dirname, join, relative } from "node:path";

const root = join(import.meta.dir, "..");

function buildInputs(directory: string): string[] {
	const files: string[] = [];
	for (const entry of readdirSync(directory, { withFileTypes: true })) {
		const path = join(directory, entry.name);
		if (entry.isDirectory()) files.push(...buildInputs(path));
		else if (entry.isFile()) files.push(path);
	}
	return files.sort();
}

function sourceVersion(sourceRoot = root): string {
	const pkg = JSON.parse(readFileSync(join(sourceRoot, "package.json"), "utf8")) as { version: string };
	return pkg.version;
}

export function sourceDigest(sourceRoot = root): string {
	const hash = createHash("sha256");
	hash.update(sourceVersion(sourceRoot));
	for (const path of [join(sourceRoot, "go.mod"), join(sourceRoot, "go.sum"), ...buildInputs(join(sourceRoot, "cmd")), ...buildInputs(join(sourceRoot, "internal"))]) {
		hash.update(relative(sourceRoot, path).replaceAll("\\", "/"));
		hash.update("\0");
		hash.update(readFileSync(path));
		hash.update("\0");
	}
	return hash.digest("hex").slice(0, 16);
}

/** Build once per source revision; never replace a running executable. */
export function ensureNativeBinary(): string {
	const executable = process.platform === "win32" ? "luuna-missions.exe" : "luuna-missions";
	const target = join(root, "dist", `${process.platform}-${process.arch}`, sourceDigest(), executable);
	if (existsSync(target)) return target;
	mkdirSync(dirname(target), { recursive: true });
	const temporary = join(dirname(target), `.building-${process.pid}-${randomBytes(4).toString("hex")}-${basename(target)}`);
	const go = process.env.LUUNA_MISSIONS_GO || "go";
	const goOS = process.platform === "win32" ? "windows" : process.platform;
	const goArch = process.arch === "x64" ? "amd64" : process.arch === "ia32" ? "386" : process.arch;
	try {
		const result = Bun.spawnSync(
			[go, "build", "-trimpath", "-ldflags", `-X github.com/luuna-ai/luuna-missions/internal/cli.CurrentVersion=${sourceVersion()}`, "-o", temporary, "./cmd/luuna-missions"],
			{ cwd: root, stdout: "pipe", stderr: "pipe", env: { ...process.env, GOOS: goOS, GOARCH: goArch } },
		);
		if (result.exitCode !== 0) {
			const detail = new TextDecoder().decode(result.stderr).trim();
			throw new Error(`Go build failed${detail ? `: ${detail}` : ""}`);
		}
		try {
			renameSync(temporary, target);
		} catch (error) {
			if (!existsSync(target)) throw error;
		}
		return target;
	} catch (error) {
		if (error instanceof Error && (error.message.includes("ENOENT") || error.message.includes("not found"))) {
			throw new Error("Go is required to build Luuna Missions. Install Go 1.22+ or run `bun run build:native` on a build host.", { cause: error });
		}
		throw error;
	} finally {
		rmSync(temporary, { force: true });
	}
}

if (import.meta.main) console.log(ensureNativeBinary());
