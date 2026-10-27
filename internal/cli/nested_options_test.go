package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
)

func runNestedOptions(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	app := New()
	RegisterBuiltins(app)
	app.Cwd = dir
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	return app.Run(args), stdout.String(), stderr.String()
}

func TestNestedLeavesRejectSiblingOptions(t *testing.T) {
	t.Setenv("LUUNA_MISSIONS_DEBUG", "")
	dir := t.TempDir()
	if err := config.Init(dir); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, model.MissionsDir, model.ConfigFile)
	templatePath := filepath.Join(dir, model.MissionsDir, model.TemplatesFile)
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeTemplates, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		flag string
	}{
		{"config schema", []string{"config", "schema", "--path", "project"}, "--path"},
		{"config schema inline", []string{"config", "schema", "--path=project"}, "--path=project"},
		{"config set", []string{"config", "set", "project", "changed", "--path", "project"}, "--path"},
		{"config unset", []string{"config", "unset", "project", "--path", "project"}, "--path"},
		{"config parent", []string{"config", "--json", "schema"}, "--json"},
		{"tpl create", []string{"tpl", "create", "--name", "demo", "--title", "sibling"}, "--title"},
		{"tpl step add", []string{"tpl", "step", "add", "missing", "--title", "step", "--name", "sibling"}, "--name"},
		{"tpl step parent", []string{"tpl", "step", "--title", "step", "add", "missing"}, "--title"},
		{"tpl list", []string{"tpl", "list", "--prefix", "sibling"}, "--prefix"},
		{"tpl list inline", []string{"tpl", "list", "--prefix=sibling"}, "--prefix=sibling"},
		{"tpl show", []string{"tpl", "show", "missing", "--type", "bug"}, "--type"},
		{"tpl pour", []string{"tpl", "pour", "missing", "--prefix", "demo", "--name", "sibling"}, "--name"},
		{"tpl status", []string{"tpl", "status", "missing", "--priority", "1"}, "--priority"},
		{"tpl parent", []string{"tpl", "--json", "list"}, "--json"},
		{"json error", []string{"tpl", "list", "--prefix", "sibling", "--json"}, "--prefix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runNestedOptions(t, dir, tt.args...)
			want := fmt.Sprintf("error: unknown option '%s'\n", tt.flag)
			if code != 1 || stdout != "" || stderr != want {
				t.Fatalf("code=%d stdout=%q stderr=%q; want code=1 empty stdout stderr=%q", code, stdout, stderr, want)
			}
		})
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	afterTemplates, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforeTemplates, afterTemplates) {
		t.Fatal("rejected options modified the project")
	}
}

func TestNestedLeavesKeepTheirOwnOptions(t *testing.T) {
	dir := t.TempDir()
	if err := config.Init(dir); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "schema", "--json"},
		{"config", "show", "--path", "project", "--json"},
		{"config", "set", "max_plan_depth", "4", "--json"},
		{"config", "unset", "max_plan_depth", "--json"},
		{"tpl", "create", "--name", "demo", "--json"},
		{"tpl", "list", "--json"},
	} {
		code, stdout, stderr := runNestedOptions(t, dir, args...)
		if code != 0 || stderr != "" || !json.Valid([]byte(stdout)) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}
