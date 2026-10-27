package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

func TestInitFindConfigAndBuiltInTemplateOrder(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, model.MissionsDir)
	for _, name := range []string{model.ConfigFile, model.IssuesFile, model.PlansFile, model.TemplatesFile, ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(attrs), ".luuna/missions/plans.jsonl merge=union") {
		t.Fatalf("missing merge attribute: %s", attrs)
	}
	deep := filepath.Join(root, "src", "nested")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	found, err := FindDir(deep)
	if err != nil || found != dir {
		t.Fatalf("found %q, error %v; want %q", found, err, dir)
	}
	config, err := ReadConfig(dir)
	if err != nil || config.Project != filepath.Base(root) || MaxPlanDepth(config) != 3 {
		t.Fatalf("config %#v, error %v", config, err)
	}
	templates, err := LoadPlanTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"context", "approach", "alternatives", "steps", "risks", "acceptance"}
	if !reflect.DeepEqual(templates["feature"].SectionOrder, want) {
		t.Fatalf("feature order: %#v", templates["feature"].SectionOrder)
	}
}

func TestCustomTemplateKeepsDeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	content := "project: demo\nversion: '1'\nplan_templates:\n  custom:\n    sections:\n      zeta: { required: true, kind: text, prompt: z }\n      alpha: { required: false, kind: list, item: text, prompt: a }\n"
	if err := os.WriteFile(filepath.Join(dir, model.ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	templates, err := LoadPlanTemplates(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(templates["custom"].SectionOrder, []string{"zeta", "alpha"}) {
		t.Fatalf("custom order: %#v", templates["custom"].SectionOrder)
	}
}

func TestLegacyMoveRejectsConflictsAndRewritesManagedPaths(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyDir)
	if err := os.Mkdir(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, model.ConfigFile), []byte("project: demo\nversion: '1'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte(".seeds/issues.jsonl merge=union\n"), 0644); err != nil {
		t.Fatal(err)
	}
	moved, err := MigrateLegacyLayout(root)
	if err != nil || !moved {
		t.Fatalf("move=%v, err=%v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(root, model.MissionsDir, model.ConfigFile)); err != nil {
		t.Fatal(err)
	}
	attrs, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if string(attrs) != ".luuna/missions/issues.jsonl merge=union\n" {
		t.Fatalf("attributes not rewritten: %q", attrs)
	}
	if err := os.Mkdir(legacy, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateLegacyLayout(root); err == nil || !strings.Contains(err.Error(), "Both") {
		t.Fatalf("expected collision refusal, got %v", err)
	}
}
