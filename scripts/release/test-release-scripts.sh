#!/bin/sh
# Offline tests for package.sh and check-archive.sh.
# Uses generated #!/bin/sh stub archives and a stub `go`; needs neither network access nor
# the Go compiler. Needs git for the provenance cases. Everything happens in a
# private temporary directory; the real working tree is never modified.
#
#   sh scripts/release/test-release-scripts.sh
set -eu

here=$(cd "$(dirname "$0")" && pwd -P)
repo_root=$(cd "$here/../.." && pwd -P)
pkg="$here/package.sh"
check="$here/check-archive.sh"
sandbox=$(mktemp -d "${TMPDIR:-/tmp}/luuna-missions-release-test.XXXXXX")
trap 'rm -rf "$sandbox"' EXIT HUP INT TERM
sandbox=$(cd "$sandbox" && pwd -P)
failures=0
count=0

pass() { count=$((count + 1)); printf 'ok %s - %s\n' "$count" "$1"; }
fail() { count=$((count + 1)); failures=$((failures + 1)); printf 'not ok %s - %s\n' "$count" "$1"; }
# check_that DESCRIPTION CMD...: pass when CMD succeeds.
check_that() {
    desc=$1
    shift
    if "$@"; then pass "$desc"; else fail "$desc"; fi
}

# expect_exit CODE DESCRIPTION CMD...
expect_exit() {
    want=$1
    desc=$2
    shift 2
    set +e
    "$@" >"$sandbox/out" 2>&1
    got=$?
    set -e
    if [ "$got" = "$want" ]; then pass "$desc"; else
        fail "$desc (exit $got, want $want)"
        sed 's/^/#   /' "$sandbox/out"
    fi
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
    else shasum -a 256 "$1" | awk '{print $1}'; fi
}

host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64 | aarch64) host_arch=arm64 ;; *) host_arch=amd64 ;; esac
other_os=linux
[ "$host_os" = linux ] && other_os=darwin

is_gnu_tar() { tar --version 2>/dev/null | grep -q 'GNU tar'; }

# make_stub DIR: bin/luuna-missions and bin/lms print their arguments.
make_stub() {
    mkdir -p "$1/bin"
    for exe in luuna-missions lms; do
        cat >"$1/bin/$exe" <<'EOF'
#!/bin/sh
for a in "$@"; do printf '[%s]' "$a"; done
echo
EOF
        chmod 0755 "$1/bin/$exe"
    done
}

# sums DIR NAME: write DIR/SHA256SUMS with one matching entry for NAME.
sums() {
    (cd "$1" && printf '%s  %s\n' "$(sha256_of "$2")" "$2" >SHA256SUMS)
}

# pack NAME SRC_DIR ENTRIES... : write NAME (a tar.gz) under $sandbox/a-NAME/
# together with a matching SHA256SUMS.
pack() {
    name=$1
    src=$2
    shift 2
    d=$(mktemp -d "$sandbox/a-XXXXXX")
    (cd "$src" && COPYFILE_DISABLE=1 tar -cf - "$@") | gzip -n >"$d/$name"
    sums "$d" "$name"
    printf '%s\n' "$d/$name"
}

v=0.0.0-test
good_name="luuna-missions_${v}_${host_os}_${host_arch}.tar.gz"
zero=0000000000000000000000000000000000000000000000000000000000000000

# --- package.sh argument validation (all fail before any build) ---
expect_exit 0 "package.sh --help" sh "$pkg" --help
expect_exit 2 "package.sh rejects missing --version" sh "$pkg" --out "$sandbox/o1"
expect_exit 2 "package.sh rejects bad --version" sh "$pkg" --version 'v1.0' --out "$sandbox/o2"
expect_exit 2 "package.sh rejects shell metachar --version" sh "$pkg" --version '1.0.0;rm' --out "$sandbox/o3"
expect_exit 2 "package.sh rejects missing --out" sh "$pkg" --version 1.0.0
expect_exit 2 "package.sh rejects option-like --out" sh "$pkg" --version 1.0.0 --out -x
expect_exit 2 "package.sh rejects --os windows" sh "$pkg" --version 1.0.0 --out "$sandbox/o4" --os windows
expect_exit 2 "package.sh rejects --arch riscv64" sh "$pkg" --version 1.0.0 --out "$sandbox/o5" --arch riscv64
expect_exit 2 "package.sh rejects unknown argument" sh "$pkg" --version 1.0.0 --out "$sandbox/o6" --publish
: >"$sandbox/afile"
expect_exit 2 "package.sh rejects --out that is a file" sh "$pkg" --version 1.0.0 --out "$sandbox/afile"
mkdir -p "$sandbox/nonempty" && : >"$sandbox/nonempty/x"
expect_exit 2 "package.sh rejects non-empty --out" sh "$pkg" --version 1.0.0 --out "$sandbox/nonempty"
check_that "rejected non-empty --out left untouched" test "$(ls -A "$sandbox/nonempty")" = x
for d in o1 o2 o3 o4 o5 o6; do
    if [ -e "$sandbox/$d" ]; then fail "rejected run created $d"; fi
done

# --- check-archive.sh ---
make_stub "$sandbox/good"
a=$(pack "$good_name" "$sandbox/good" bin/luuna-missions bin/lms)
good=$a
expect_exit 0 "good host archive verifies and runs" sh "$check" "$a"
check_that "good archive was executed" grep -q 'executed on host' "$sandbox/out"
expect_exit 0 "--require-run on good host archive" sh "$check" --require-run "$a"
check_that "--require-run output says executed on host" grep -q 'executed on host' "$sandbox/out"
expect_exit 2 "--require-run with --no-run is a usage error" sh "$check" --require-run --no-run "$a"
expect_exit 2 "--no-run with --require-run is a usage error" sh "$check" --no-run --require-run "$a"
expect_exit 0 "--no-run verifies without executing" sh "$check" --no-run "$a"
check_that "--no-run output says not executed" grep -q 'not executed' "$sandbox/out"

# alias must preserve arguments (including spaces) identically to the primary
out_p=$("$sandbox/good/bin/luuna-missions" 'a b' c)
out_a=$("$sandbox/good/bin/lms" 'a b' c)
if [ "$out_p" = "[a b][c]" ] && [ "$out_a" = "$out_p" ]; then pass "stub alias preserves args"; else fail "stub alias preserves args"; fi

gd=$(dirname "$good")
good_sha=$(sha256_of "$good")
printf '%s *%s\n' "$good_sha" "$good_name" >"$gd/SHA256SUMS"
expect_exit 0 "binary-mode '*' SHA256SUMS entry accepted" sh "$check" "$good"
printf '%s  %s\n%s  %s\n' "$zero" LICENSE "$good_sha" "$good_name" >"$gd/SHA256SUMS"
expect_exit 0 "SHA256SUMS with other entries accepted" sh "$check" "$good"
printf '%s  %s\n' "$zero" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "mismatching SHA256SUMS rejected" sh "$check" "$good"
rm "$gd/SHA256SUMS"
expect_exit 4 "missing SHA256SUMS rejected" sh "$check" "$good"
mkdir -p "$sandbox/sums-real"
printf '%s  %s\n' "$good_sha" "$good_name" >"$sandbox/sums-real/SHA256SUMS"
ln -s "$sandbox/sums-real/SHA256SUMS" "$gd/SHA256SUMS"
expect_exit 4 "symlinked SHA256SUMS rejected" sh "$check" "$good"
rm "$gd/SHA256SUMS"
mkdir "$gd/SHA256SUMS"
expect_exit 4 "directory SHA256SUMS rejected" sh "$check" "$good"
rmdir "$gd/SHA256SUMS"
printf '%s  %s\n%s  %s\n' "$good_sha" "$good_name" "$good_sha" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "duplicate identical SHA256SUMS entries rejected" sh "$check" "$good"
printf '%s  %s\n%s *%s\n' "$good_sha" "$good_name" "$good_sha" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "duplicate entries with and without '*' rejected" sh "$check" "$good"
printf '%s  %s\n' "$zero" "other.tar.gz" >"$gd/SHA256SUMS"
expect_exit 4 "SHA256SUMS without an entry for the archive rejected" sh "$check" "$good"
printf '%s  %s\n' "$(printf '%s' "$good_sha" | cut -c1-63)" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "short SHA256SUMS digest rejected" sh "$check" "$good"
printf '%s  %s\n' "$(printf '%s' "$good_sha" | cut -c1-63)g" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "non-hex SHA256SUMS digest rejected" sh "$check" "$good"
printf '%s  %s\n' "$(printf '%s' "$good_sha" | tr 'a-f' 'A-F')" "$good_name" >"$gd/SHA256SUMS"
if [ "$(printf '%s' "$good_sha" | tr 'a-f' 'A-F')" != "$good_sha" ]; then
    expect_exit 4 "uppercase SHA256SUMS digest rejected" sh "$check" "$good"
fi
printf '%s  %s extra\n' "$good_sha" "$good_name" >"$gd/SHA256SUMS"
expect_exit 4 "SHA256SUMS entry with extra fields rejected" sh "$check" "$good"
sums "$gd" "$good_name"

other="luuna-missions_${v}_${other_os}_${host_arch}.tar.gz"
a=$(pack "$other" "$sandbox/good" bin/luuna-missions bin/lms)
expect_exit 0 "foreign-platform archive verified without running" sh "$check" "$a"
check_that "foreign archive not executed" grep -q 'not executed' "$sandbox/out"
expect_exit 6 "--require-run on foreign-platform archive fails" sh "$check" --require-run "$a"

a=$(pack "luuna-missions_${v}_windows_amd64.tar.gz" "$sandbox/good" bin/luuna-missions bin/lms)
expect_exit 3 "bad archive name rejected" sh "$check" "$a"

mkdir -p "$sandbox/extra" && make_stub "$sandbox/extra" && cp "$sandbox/extra/bin/lms" "$sandbox/extra/bin/lm"
a=$(pack "$good_name" "$sandbox/extra" bin/luuna-missions bin/lms bin/lm)
expect_exit 5 "extra entry (lm) rejected" sh "$check" "$a"

a=$(pack "$good_name" "$sandbox/good" bin/luuna-missions)
expect_exit 5 "missing alias rejected" sh "$check" "$a"

a=$(pack "$good_name" "$sandbox/good" bin)
expect_exit 5 "directory entry rejected" sh "$check" "$a"

mkdir -p "$sandbox/link/bin" && make_stub "$sandbox/link" && rm "$sandbox/link/bin/lms" && ln -s luuna-missions "$sandbox/link/bin/lms"
a=$(pack "$good_name" "$sandbox/link" bin/luuna-missions bin/lms)
expect_exit 5 "symlink entry rejected" sh "$check" "$a"

mkdir -p "$sandbox/mode" && make_stub "$sandbox/mode" && chmod 0644 "$sandbox/mode/bin/lms"
a=$(pack "$good_name" "$sandbox/mode" bin/luuna-missions bin/lms)
expect_exit 5 "non-executable entry rejected" sh "$check" "$a"

mkdir -p "$sandbox/ww" && make_stub "$sandbox/ww" && chmod 0777 "$sandbox/ww/bin/lms"
a=$(pack "$good_name" "$sandbox/ww" bin/luuna-missions bin/lms)
expect_exit 5 "world-writable entry rejected" sh "$check" "$a"

# traversal and absolute entries (built with tar name rewriting)
d="$sandbox/a-trav" && mkdir -p "$d"
if is_gnu_tar; then
    (cd "$sandbox/good" && tar -cPf - --transform 's,^bin/lms$,bin/../lms,' bin/luuna-missions bin/lms) | gzip -n >"$d/$good_name"
else
    (cd "$sandbox/good" && COPYFILE_DISABLE=1 tar -cPf - -s ',^bin/lms$,bin/../lms,' bin/luuna-missions bin/lms) | gzip -n >"$d/$good_name"
fi
sums "$d" "$good_name"
if tar -tzf "$d/$good_name" | grep -q '\.\./'; then
    expect_exit 5 "path traversal entry rejected" sh "$check" "$d/$good_name"
else
    fail "could not build traversal fixture"
fi

d="$sandbox/a-abs" && mkdir -p "$d"
(cd "$sandbox/good" && COPYFILE_DISABLE=1 tar -cPf - bin/luuna-missions "$sandbox/good/bin/lms") | gzip -n >"$d/$good_name"
sums "$d" "$good_name"
if tar -tzf "$d/$good_name" | grep -q '^/'; then
    expect_exit 5 "absolute path entry rejected" sh "$check" "$d/$good_name"
else
    fail "could not build absolute-path fixture"
fi
[ ! -e "$sandbox/lms" ] || fail "traversal fixture escaped into sandbox"

mkdir -p "$sandbox/a-bad" && printf 'garbage' >"$sandbox/a-bad/$good_name"
sums "$sandbox/a-bad" "$good_name"
expect_exit 5 "corrupt archive (matching SHA256SUMS) rejected" sh "$check" "$sandbox/a-bad/$good_name"

expect_exit 2 "check-archive requires an argument" sh "$check"
expect_exit 2 "check-archive rejects missing file" sh "$check" "$sandbox/nope.tar.gz"

# --- package.sh end to end with a stub go (no Go toolchain needed) ---
# Isolate git from the caller's configuration and repository discovery.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CEILING_DIRECTORIES="$sandbox"
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GITHUB_REF 2>/dev/null || true

mkdir -p "$sandbox/gostub"
stub_go="$sandbox/gostub/go"
cat >"$stub_go" <<'EOF'
#!/bin/sh
# Stub go: `build ... -o PATH ...` writes a tiny executable; `env GOVERSION`.
case "${1:-}" in
build)
    while [ $# -gt 0 ]; do
        if [ "$1" = -o ]; then
            printf '#!/bin/sh\necho luuna-missions-stub\n' >"$2"
            exit 0
        fi
        shift
    done
    exit 1
    ;;
env)
    [ "${2:-}" = GOVERSION ] && echo go0.0-stub && exit 0
    exit 1
    ;;
esac
exit 1
EOF
chmod 0755 "$stub_go"

# make_src DIR: minimal source tree that package.sh accepts as its root.
make_src() {
    mkdir -p "$1/scripts/release" "$1/cmd/luuna-missions" "$1/internal/cli"
    for f in go.mod internal/cli/app.go LICENSE UPSTREAM.md cmd/luuna-missions/main.go scripts/release/package.sh; do
        cp "$repo_root/$f" "$1/$f"
    done
    printf 'dist/\n' >"$1/.gitignore"
}

# run_pkg SRC OUT [ENV...]: package with the stub go and print the archive.
run_pkg() {
    src=$1
    o=$2
    shift 2
    env LUUNA_MISSIONS_GO="$stub_go" "$@" sh "$src/scripts/release/package.sh" --version "$v" --out "$o"
}

# tar_mtime ARCHIVE OFFSET: the octal mtime field of the ustar header at OFFSET.
tar_mtime() {
    gzip -dc "$1" | dd bs=1 skip=$(($2 + 136)) count=11 2>/dev/null
}

nogit="$sandbox/src-nogit"
make_src "$nogit"
if a1=$(run_pkg "$nogit" "$sandbox/tz1" TZ=UTC) && a2=$(run_pkg "$nogit" "$sandbox/tz2" TZ=Pacific/Kiritimati); then
    pass "package.sh with stub go succeeds"
    check_that "archives are byte-identical across TZ=UTC and TZ=Pacific/Kiritimati" cmp -s "$a1" "$a2"
    want_mtime=$(printf '%011o' 946684800)
    check_that "first entry mtime is 946684800" test "$(tar_mtime "$a1" 0)" = "$want_mtime"
    check_that "second entry mtime is 946684800" test "$(tar_mtime "$a1" 1024)" = "$want_mtime"
    p="$sandbox/tz1/provenance.json"
    check_that "no git: commit is null" grep -q '"commit": null,' "$p"
    check_that "no git: ref is null" grep -q '"ref": null,' "$p"
    check_that "no git: source_tree_dirty is null" grep -q '"source_tree_dirty": null,' "$p"
    check_that "toolchain go recorded" grep -q '"go": "go0.0-stub"' "$p"
    check_that "no tracked_tree_dirty key" sh -c '! grep -q tracked_tree_dirty "$1"' sh "$p"
    expect_exit 0 "check-archive --require-run accepts packaged archive" sh "$check" --require-run "$a1"
    check_that "SHA256SUMS lists four files" test "$(wc -l <"$sandbox/tz1/SHA256SUMS" | tr -d ' ')" = 4
else
    fail "package.sh with stub go succeeds"
fi

if command -v git >/dev/null 2>&1; then
    gitsrc="$sandbox/src-git"
    make_src "$gitsrc"
    git -C "$gitsrc" init -q
    git -C "$gitsrc" add -A
    git -C "$gitsrc" -c user.name=test -c user.email=test@example.invalid -c commit.gpgsign=false \
        commit -q --no-verify -m fixture
    mkdir -p "$gitsrc/dist" && : >"$gitsrc/dist/ignored"
    if run_pkg "$gitsrc" "$sandbox/g1" >/dev/null; then
        p="$sandbox/g1/provenance.json"
        check_that "clean tree (ignored file only): source_tree_dirty is false" grep -q '"source_tree_dirty": false,' "$p"
        check_that "git: commit is a 40-hex string" grep -Eq '"commit": "[0-9a-f]{40}",' "$p"
        check_that "git: ref is the branch" grep -Eq '"ref": "refs/heads/[^"]+",' "$p"
    else
        fail "package.sh in clean git tree"
    fi
    mkdir -p "$gitsrc/internal" && printf 'package x\n' >"$gitsrc/internal/x.go"
    if run_pkg "$gitsrc" "$sandbox/g2" >/dev/null; then
        check_that "untracked non-ignored file: source_tree_dirty is true" \
            grep -q '"source_tree_dirty": true,' "$sandbox/g2/provenance.json"
    else
        fail "package.sh in git tree with untracked file"
    fi
    if run_pkg "$gitsrc" "$sandbox/g3" GITHUB_REF=refs/tags/v0.0.0 >/dev/null; then
        check_that "GITHUB_REF overrides ref" grep -q '"ref": "refs/tags/v0.0.0",' "$sandbox/g3/provenance.json"
    else
        fail "package.sh with GITHUB_REF"
    fi
else
    fail "git is required for provenance tests"
fi

# Failing tar or gzip must fail package.sh and leave no partial output.
for tool in tar gzip; do
    shim="$sandbox/shim-$tool"
    mkdir -p "$shim"
    printf '#!/bin/sh\nexit 1\n' >"$shim/$tool"
    chmod 0755 "$shim/$tool"
    expect_exit 1 "failing $tool fails package.sh" run_pkg "$nogit" "$sandbox/fail-$tool" PATH="$shim:$PATH"
    check_that "failing $tool: created --out removed" test ! -e "$sandbox/fail-$tool"
    mkdir "$sandbox/fail-$tool-existing"
    expect_exit 1 "failing $tool fails package.sh (existing --out)" \
        run_pkg "$nogit" "$sandbox/fail-$tool-existing" PATH="$shim:$PATH"
    check_that "failing $tool: existing --out kept and empty" \
        test -d "$sandbox/fail-$tool-existing" -a -z "$(ls -A "$sandbox/fail-$tool-existing")"
done

printf '1..%s\n' "$count"
if [ "$failures" -ne 0 ]; then
    printf '%s test(s) failed\n' "$failures" >&2
    exit 1
fi
