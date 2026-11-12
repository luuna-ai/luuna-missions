package regression

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

type result struct {
	Stdout, Stderr string
	Exit           int
}
type fixture struct {
	Name          string
	Args          []string
	Input, Root   string
	Before, After map[string]string
	Result        result
	IDs           map[string]string
	Env           map[string]string
}

var iso = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z\b`)

// TestCompatibility replays independently captured public CLI contracts. Every
// case restores real files and checks streams, exit status and all resulting files.
func TestCompatibility(t *testing.T) {
	goBin := os.Getenv("LUUNA_MISSIONS_GO")
	if goBin == "" {
		goBin = "go"
	}
	bin := filepath.Join(t.TempDir(), "luuna-missions"+exeSuffix())
	build := exec.Command(goBin, "build", "-buildvcs=false", "-o", bin, "../../cmd/luuna-missions")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	atlasDir := t.TempDir()
	build = exec.Command(goBin, "build", "-buildvcs=false", "-o", filepath.Join(atlasDir, "luuna-atlas"+exeSuffix()), "./testdata/atlas")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("Atlas fixture: %v\n%s", err, out)
	}
	paths, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 194 {
		t.Fatalf("expected 194 compatibility cases, found %d", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f fixture
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(path)+"/"+f.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "project")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			replaceRoot := func(s string) string {
				s = strings.ReplaceAll(s, strings.ReplaceAll(f.Root, `\`, `\\`), strings.ReplaceAll(root, `\`, `\\`))
				s = strings.ReplaceAll(s, strings.ReplaceAll(f.Root, `\`, `/`), filepath.ToSlash(root))
				return strings.ReplaceAll(s, f.Root, root)
			}
			for name, content := range f.Before {
				target := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte(replaceRoot(content)), 0644); err != nil {
					t.Fatal(err)
				}
			}
			git := exec.Command("git", "init", "-q", root)
			if out, err := git.CombinedOutput(); err != nil {
				t.Fatalf("git: %v %s", err, out)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, f.Args...)
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(replaceRoot(f.Input))
			for _, e := range os.Environ() {
				key, _, _ := strings.Cut(e, "=")
				if !strings.HasPrefix(key, "LUUNA_MISSIONS_") && key != "GIT_DIR" && key != "GIT_WORK_TREE" && key != "GIT_INDEX_FILE" && key != "PATH" {
					cmd.Env = append(cmd.Env, e)
				}
			}
			pathEnv := os.Getenv("PATH")
			if p := f.Env["PATH"]; p != "" {
				if strings.Contains(p, "no-atlas-bin") {
					pathEnv = t.TempDir()
				} else {
					pathEnv = atlasDir + string(os.PathListSeparator) + pathEnv
				}
			}
			cmd.Env = append(cmd.Env, "PATH="+pathEnv, "NO_COLOR=1", "FORCE_COLOR=0", "CI=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			exit := 0
			if err := cmd.Run(); err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					exit = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != f.Result.Exit {
				t.Fatalf("exit %d, want %d: %s", exit, f.Result.Exit, stderr.String())
			}
			ids := map[string]string{}
			for _, v := range f.IDs {
				ids[v] = v
			}
			var old, new any
			_ = json.Unmarshal([]byte(f.Result.Stdout), &old)
			_ = json.Unmarshal(stdout.Bytes(), &new)
			bind(old, new, ids)
			if f.Name == "version json" {
				expected := old.(map[string]any)
				osName, arch := runtime.GOOS, runtime.GOARCH
				if osName == "windows" {
					osName = "win32"
				}
				if arch == "amd64" {
					arch = "x64"
				}
				expected["platform"] = osName + "-" + arch
				data, _ := json.Marshal(expected)
				f.Result.Stdout = string(data)
			}
			if f.Name == "list sort" {
				m := new.(map[string]any)
				issues := m["issues"].([]any)
				for i := 1; i < len(issues); i++ {
					if fmt.Sprint(issues[i-1].(map[string]any)["id"]) > fmt.Sprint(issues[i].(map[string]any)["id"]) {
						t.Fatal("list --sort id returned unsorted IDs")
					}
				}
			}

			norm := func(s string, actual bool) string {
				s = strings.ReplaceAll(s, "\r\n", "\n")
				for _, p := range []string{f.Root, root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`), strings.ReplaceAll(f.Root, `\`, `\\`), strings.ReplaceAll(f.Root, `\`, `/`)} {
					s = strings.ReplaceAll(s, p, "<PROJECT>")
				}
				keys := make([]string, 0, len(f.IDs))
				for k := range f.IDs {
					keys = append(keys, k)
				}
				sort.Slice(keys, func(i, j int) bool { return len(f.IDs[keys[i]]) > len(f.IDs[keys[j]]) })
				for _, k := range keys {
					v := f.IDs[k]
					if actual {
						v = ids[v]
					}
					s = strings.ReplaceAll(s, v, "<"+k+">")
				}
				return iso.ReplaceAllString(s, "<ISO>")
			}
			compare := func(label, want, got string) {
				t.Helper()
				want, got = norm(want, false), norm(got, true)
				var a, b any
				if json.Unmarshal([]byte(want), &a) == nil && json.Unmarshal([]byte(got), &b) == nil {
					if f.Name == "list sort" && label == "stdout" {
						sortIssues(a)
						sortIssues(b)
					}
					if reflect.DeepEqual(portablePaths(a), portablePaths(b)) {
						return
					}
				} else if want == got {
					return
				}
				t.Errorf("%s differs\nwant: %.1500s\n got: %.1500s", label, want, got)
			}
			compare("stdout", f.Result.Stdout, stdout.String())
			compare("stderr", f.Result.Stderr, stderr.String())
			found := map[string]bool{}
			err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if d.Name() == ".git" {
						return filepath.SkipDir
					}
					return nil
				}
				rel, _ := filepath.Rel(root, path)
				name := filepath.ToSlash(rel)
				found[name] = true
				want, ok := f.After[name]
				if !ok {
					t.Errorf("unexpected file %s", name)
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if strings.HasSuffix(name, ".jsonl") {
					a, b := strings.Split(strings.TrimSpace(want), "\n"), strings.Split(strings.TrimSpace(string(data)), "\n")
					if len(a) != len(b) {
						t.Errorf("%s record count: %d want %d", name, len(b), len(a))
						return nil
					}
					for i := range a {
						compare(fmt.Sprintf("%s:%d", name, i+1), a[i], b[i])
					}
				} else {
					compare(name, want, string(data))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for name := range f.After {
				if !found[name] {
					t.Errorf("missing file %s", name)
				}
			}
		})
	}
}
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
func bind(a, b any, ids map[string]string) {
	switch v := a.(type) {
	case string:
		if _, ok := ids[v]; ok {
			if s, ok := b.(string); ok {
				ids[v] = s
			}
		}
	case map[string]any:
		if w, ok := b.(map[string]any); ok {
			for k, x := range v {
				bind(x, w[k], ids)
			}
		}
	case []any:
		if w, ok := b.([]any); ok {
			for i := 0; i < len(v) && i < len(w); i++ {
				bind(v[i], w[i], ids)
			}
		}
	}
}
func sortIssues(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	a, ok := m["issues"].([]any)
	if !ok {
		return
	}
	sort.Slice(a, func(i, j int) bool {
		return fmt.Sprint(a[i].(map[string]any)["id"]) < fmt.Sprint(a[j].(map[string]any)["id"])
	})
}

// Only absolute fixture paths may vary in path separator across host systems.
func portablePaths(v any) any {
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "<PROJECT>") {
			return strings.ReplaceAll(x, `\`, "/")
		}
	case map[string]any:
		for k, value := range x {
			x[k] = portablePaths(value)
		}
	case []any:
		for i, value := range x {
			x[i] = portablePaths(value)
		}
	}
	return v
}
