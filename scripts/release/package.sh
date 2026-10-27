#!/bin/sh
# Build the public Luuna Missions native archive for one platform.
#
#   sh scripts/release/package.sh --version V --out DIR [--os darwin|linux] [--arch arm64|amd64]
#
# Writes into DIR:
#   luuna-missions_<V>_<os>_<arch>.tar.gz   entries: bin/luuna-missions, bin/lms (0755 regular files)
#   SHA256SUMS                              sha256 of every file below
#   provenance.json                         self-reported build metadata (not an attestation)
#   LICENSE, UPSTREAM.md                    attribution accompanying the archive
#
# The archive contains only the two public executables. The compatibility
# aliases `lm` and `missions` are intentionally excluded. Nothing is published.
# On failure the files written to DIR are removed (and DIR, if this run
# created it).
set -eu

usage() {
	cat <<'EOF'
Usage: sh scripts/release/package.sh --version V --out DIR [--os OS] [--arch ARCH]

  --version V   archive version (semver, e.g. 1.2.3 or 0.0.0-local)
  --out DIR     output directory; created if missing, must be empty if present
  --os OS       darwin or linux (default: host)
  --arch ARCH   arm64 or amd64 (default: host)
  --help        show this help

Environment: LUUNA_MISSIONS_GO (go executable, default "go").
EOF
}

die() {
	printf 'package.sh: %s\n' "$1" >&2
	exit "${2:-1}"
}

host_os() {
	case "$(uname -s)" in
	Darwin) echo darwin ;;
	Linux) echo linux ;;
	*) echo unsupported ;;
	esac
}

host_arch() {
	case "$(uname -m)" in
	arm64 | aarch64) echo arm64 ;;
	x86_64 | amd64) echo amd64 ;;
	*) echo unsupported ;;
	esac
}

valid_version() {
	printf '%s\n' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$'
}

json_escape() {
	printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' | tr -d '\n\r\t'
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "need sha256sum or shasum" 1
	fi
}

version=""
out=""
target_os=""
target_arch=""
while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version requires a value" 2
		version=$2
		shift 2
		;;
	--out)
		[ $# -ge 2 ] || die "--out requires a value" 2
		out=$2
		shift 2
		;;
	--os)
		[ $# -ge 2 ] || die "--os requires a value" 2
		target_os=$2
		shift 2
		;;
	--arch)
		[ $# -ge 2 ] || die "--arch requires a value" 2
		target_arch=$2
		shift 2
		;;
	--help | -h)
		usage
		exit 0
		;;
	*) die "unknown argument: $1" 2 ;;
	esac
done

[ -n "$version" ] || die "--version is required" 2
valid_version "$version" || die "invalid --version: $version" 2
[ -n "$out" ] || die "--out is required" 2
case "$out" in
-*) die "invalid --out: $out" 2 ;;
esac
[ -n "$target_os" ] || target_os=$(host_os)
[ -n "$target_arch" ] || target_arch=$(host_arch)
case "$target_os" in darwin | linux) ;; *) die "unsupported --os: $target_os" 2 ;; esac
case "$target_arch" in arm64 | amd64) ;; *) die "unsupported --arch: $target_arch" 2 ;; esac

created_out=1
if [ -e "$out" ] || [ -L "$out" ]; then
	[ -d "$out" ] && [ ! -L "$out" ] || die "--out exists and is not a directory: $out" 2
	[ -z "$(ls -A "$out")" ] || die "--out must be empty: $out" 2
	created_out=0
fi

root=$(cd "$(dirname "$0")/../.." && pwd -P)
[ -f "$root/go.mod" ] && [ -d "$root/cmd/luuna-missions" ] || die "cannot locate repository root" 1
go_bin=${LUUNA_MISSIONS_GO:-go}
command -v "$go_bin" >/dev/null 2>&1 || die "go not found (set LUUNA_MISSIONS_GO)" 1

# The binary reports the component's internal compatibility version (the
# package.json version, same symbol scripts/native-build.ts sets). The archive
# version is recorded separately in the file name and provenance.json.
internal_version=$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)".*/\1/p' "$root/package.json" | head -n 1)
valid_version "$internal_version" || die "cannot read internal version from package.json" 1
version_symbol=github.com/luuna-ai/luuna-missions/internal/cli.CurrentVersion
ldflags="-s -w -X $version_symbol=$internal_version"

archive="luuna-missions_${version}_${target_os}_${target_arch}.tar.gz"
outputs="$archive provenance.json LICENSE UPSTREAM.md SHA256SUMS"

umask 022
work=""
done_ok=0
# On failure remove only the files this run writes ($out was verified empty
# or created by this run) and the directory itself if this run created it.
cleanup() {
	[ -z "$work" ] || rm -rf "$work"
	if [ "$done_ok" != 1 ]; then
		for f in $outputs; do
			rm -f "$out/$f"
		done
		[ "$created_out" != 1 ] || rmdir "$out" 2>/dev/null || true
	fi
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
[ "$created_out" = 0 ] || mkdir -p "$out"
out=$(cd "$out" && pwd -P)
work=$(mktemp -d "${TMPDIR:-/tmp}/luuna-missions-pkg.XXXXXX")
mkdir "$work/bin"

(
	cd "$root"
	CGO_ENABLED=0 GOOS=$target_os GOARCH=$target_arch GOTOOLCHAIN=${GOTOOLCHAIN:-local} \
		"$go_bin" build -trimpath -buildvcs=false -ldflags "$ldflags" \
		-o "$work/bin/luuna-missions" ./cmd/luuna-missions
)
# main() passes os.Args[1:] and never inspects argv[0], so the alias is a
# byte-identical copy of the primary.
cp "$work/bin/luuna-missions" "$work/bin/lms"
chmod 0755 "$work/bin/luuna-missions" "$work/bin/lms"
# Fixed mtime 2000-01-01T00:00:00Z (946684800) for reproducible archive
# bytes, independent of the caller's time zone.
TZ=UTC0 touch -t 200001010000 "$work/bin/luuna-missions" "$work/bin/lms"

# tar and gzip run as separate commands so that, under set -e, a failure of
# either one stops the script (POSIX sh has no pipefail).
if tar --version 2>/dev/null | grep -q 'GNU tar'; then
	(cd "$work" && tar --format=ustar --owner=0 --group=0 --numeric-owner \
		-cf "$work/archive.tar" bin/luuna-missions bin/lms)
else
	(cd "$work" && COPYFILE_DISABLE=1 tar --format ustar --uid 0 --gid 0 --uname root --gname root \
		-cf "$work/archive.tar" bin/luuna-missions bin/lms)
fi
gzip -n -9 <"$work/archive.tar" >"$out/$archive"

cp "$root/LICENSE" "$out/LICENSE"
cp "$root/UPSTREAM.md" "$out/UPSTREAM.md"

# json_str_or_null VALUE: a quoted JSON string, or null when VALUE is empty.
json_str_or_null() {
	if [ -n "$1" ]; then printf '"%s"' "$(json_escape "$1")"; else printf null; fi
}

archive_sha=$(sha256_of "$out/$archive")
go_version=$("$go_bin" env GOVERSION 2>/dev/null) || go_version=""
commit=""
git_ref=""
dirty=null
if git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	commit=$(git -C "$root" rev-parse --verify -q HEAD 2>/dev/null) || commit=""
	git_ref=$(git -C "$root" symbolic-ref -q HEAD 2>/dev/null) || git_ref=""
	# Includes untracked, non-ignored files; .gitignore is respected.
	if status=$(git -C "$root" status --porcelain --untracked-files=normal 2>/dev/null); then
		if [ -n "$status" ]; then dirty=true; else dirty=false; fi
	fi
fi
ref=${GITHUB_REF:-$git_ref}
run_id=null
[ -z "${GITHUB_RUN_ID:-}" ] || run_id="\"$(json_escape "$GITHUB_RUN_ID")\""
repo=${GITHUB_REPOSITORY:-luuna-ai/luuna-missions}

cat >"$out/provenance.json" <<EOF
{
  "schema": "luuna-component-provenance/1",
  "note": "Self-reported build metadata; not a signed or verifiable attestation.",
  "component": "missions",
  "repository": "$(json_escape "$repo")",
  "commit": $(json_str_or_null "$commit"),
  "ref": $(json_str_or_null "$ref"),
  "source_tree_dirty": $dirty,
  "workflow_run_id": $run_id,
  "runner": {"os": "$(json_escape "$(uname -s)")", "arch": "$(json_escape "$(uname -m)")"},
  "archive_version": "$(json_escape "$version")",
  "internal_version": "$(json_escape "$internal_version")",
  "target": {"os": "$target_os", "arch": "$target_arch"},
  "toolchain": {"go": $(json_str_or_null "$go_version")},
  "build": {
    "package": "./cmd/luuna-missions",
    "env": "CGO_ENABLED=0 GOOS=$target_os GOARCH=$target_arch",
    "flags": "-trimpath -buildvcs=false",
    "ldflags": "$(json_escape "$ldflags")"
  },
  "archive": {"name": "$archive", "sha256": "$archive_sha", "executables": ["luuna-missions", "lms"]},
  "attribution": ["LICENSE", "UPSTREAM.md"]
}
EOF

(
	cd "$out"
	for f in "$archive" provenance.json LICENSE UPSTREAM.md; do
		printf '%s  %s\n' "$(sha256_of "$f")" "$f"
	done
) >"$out/SHA256SUMS"

done_ok=1
printf '%s\n' "$out/$archive"
