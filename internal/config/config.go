// Package config discovers and initializes a Missions project and loads its
// YAML configuration and schema-driven plan templates.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
	"gopkg.in/yaml.v3"
)

var mergeUnionLines = []string{
	".luuna/missions/issues.jsonl merge=union",
	".luuna/missions/templates.jsonl merge=union",
	".luuna/missions/plans.jsonl merge=union",
}

// FindDir searches upward, migrating a legacy store at each candidate root as
// the original CLI does. A linked worktree shares the main checkout's store
// when that store is initialized.
func FindDir(start string) (string, error) {
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := MigrateLegacyLayout(dir); err != nil {
			return "", err
		}
		candidate := filepath.Join(dir, model.MissionsDir)
		if _, err := os.Stat(filepath.Join(candidate, model.ConfigFile)); err == nil {
			return resolveWorktreeRoot(candidate), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("Not in a missions project. Run `luuna-missions init` first.")
		}
		dir = parent
	}
}

func FindMissionsDir(start string) (string, error) { return FindDir(start) }
func ProjectRoot(missionsDir string) string        { return filepath.Dir(filepath.Dir(missionsDir)) }

func gitPath(cwd, arg string) string {
	cmd := exec.Command("git", "rev-parse", arg)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Clean(filepath.Join(cwd, filepath.FromSlash(raw)))
}

func IsInsideWorktree(dir string) bool {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	gd, common := gitPath(dir, "--git-dir"), gitPath(dir, "--git-common-dir")
	return gd != "" && common != "" && !samePath(gd, common)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func resolveWorktreeRoot(candidate string) string {
	projectRoot := ProjectRoot(candidate)
	common := gitPath(projectRoot, "--git-common-dir")
	if common == "" {
		return candidate
	}
	mainRoot := filepath.Dir(common)
	if filepath.Base(common) != ".git" {
		mainRoot = filepath.Dir(mainRoot)
	}
	if samePath(mainRoot, projectRoot) {
		return candidate
	}
	mainMissions := filepath.Join(mainRoot, model.MissionsDir)
	if _, err := os.Stat(filepath.Join(mainMissions, model.ConfigFile)); err == nil {
		return mainMissions
	}
	return candidate
}

// Init creates exactly the project-owned files established by the Bun CLI.
// Existing initialized stores are left intact; missing merge attributes are
// backfilled on every call.
func Init(projectRoot string) error {
	if projectRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		projectRoot = cwd
	}
	if _, err := MigrateLegacyLayout(projectRoot); err != nil {
		return err
	}
	dir := filepath.Join(projectRoot, model.MissionsDir)
	if _, err := os.Stat(filepath.Join(dir, model.ConfigFile)); err == nil {
		return ensureGitattributes(projectRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	name := filepath.Base(filepath.Clean(projectRoot))
	content := fmt.Sprintf("project: \"%s\"\nversion: \"1\"\nmax_plan_depth: %d\n", name, model.DefaultMaxPlanDepth)
	if err := os.WriteFile(filepath.Join(dir, model.ConfigFile), []byte(content), 0644); err != nil {
		return err
	}
	for _, filename := range []string{model.IssuesFile, model.TemplatesFile, model.PlansFile} {
		if err := os.WriteFile(filepath.Join(dir, filename), nil, 0644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.lock\n"), 0644); err != nil {
		return err
	}
	return ensureGitattributes(projectRoot)
}

func ensureGitattributes(root string) error {
	path := filepath.Join(root, ".gitattributes")
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(content)
	missing := []string{}
	for _, line := range mergeUnionLines {
		if !strings.Contains(text, line) {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += strings.Join(missing, "\n") + "\n"
	return os.WriteFile(path, []byte(text), 0644)
}

func ReadConfig(missionsDir string) (model.Config, error) {
	content, err := os.ReadFile(filepath.Join(missionsDir, model.ConfigFile))
	if err != nil {
		return model.Config{}, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return model.Config{}, err
	}
	config := model.Config{Project: "missions", Version: "1"}
	if project, ok := data["project"].(string); ok {
		config.Project = project
	}
	if version, ok := data["version"].(string); ok {
		config.Version = version
	}
	if depth, ok := integer(data["max_plan_depth"]); ok {
		config.MaxPlanDepth = model.IntPtr(depth)
	}
	return config, nil
}

func MaxPlanDepth(config model.Config) int {
	if config.MaxPlanDepth != nil {
		return *config.MaxPlanDepth
	}
	return model.DefaultMaxPlanDepth
}

func integer(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if float64(int(x)) == x {
			return int(x), true
		}
	}
	return 0, false
}
