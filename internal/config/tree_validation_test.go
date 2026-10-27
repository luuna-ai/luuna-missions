package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

func TestConfigTreeSchemaAndOrder(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, model.MissionsDir)
	tree, err := ReadTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTree(tree); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(ConfigSchemaJSON()) || ConfigSchema()["title"] != "Missions project config" {
		t.Fatal("embedded schema invalid")
	}
	before, err := DisplayTree(dir, tree)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(before, "project:") || !strings.Contains(before, "\nversion:") {
		t.Fatalf("source order lost: %q", before)
	}
	tree["max_plan_depth"] = 5
	if err := WriteTree(dir, tree); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, model.ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(content, []byte("max_plan_depth: 5\n")) {
		t.Fatalf("depth not emitted: %q", content)
	}
	got, err := ReadTree(dir)
	if err != nil || got["max_plan_depth"] != 5 {
		t.Fatalf("round trip: %v, %v", got, err)
	}
}

func TestValidateConfigTreeRejectsNestedPartialAndRootExtras(t *testing.T) {
	base := map[string]any{"project": "test", "version": "1"}
	base["plan_templates"] = map[string]any{"spike": map[string]any{"sections": map[string]any{"context": map[string]any{"kind": "text"}}}}
	if err := ValidateTree(base); err == nil || !strings.Contains(err.Error(), "context.required") || !strings.Contains(err.Error(), "context.prompt") {
		t.Fatalf("partial section accepted or unclear: %v", err)
	}
	base["plan_templates"] = map[string]any{}
	base["extra"] = 1
	if err := ValidateTree(base); err == nil || !strings.Contains(err.Error(), "additionalProperties") {
		t.Fatalf("root extra accepted: %v", err)
	}
}
