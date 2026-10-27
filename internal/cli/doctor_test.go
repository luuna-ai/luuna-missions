package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
)

func doctorFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := config.Init(root); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, model.MissionsDir)
}

func runDoctorFixture(t *testing.T, root string, args ...string) (int, map[string]any) {
	t.Helper()
	app := New()
	app.Cwd = root
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	RegisterBuiltins(app)
	code := app.Run(append([]string{"doctor", "--json"}, args...))
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("doctor output %q (stderr %q): %v", stdout.String(), stderr.String(), err)
	}
	return code, payload
}

func doctorCheckStatus(t *testing.T, payload map[string]any, name string) string {
	t.Helper()
	for _, value := range payload["checks"].([]any) {
		check := value.(map[string]any)
		if check["name"] == name {
			return check["status"].(string)
		}
	}
	t.Fatalf("missing check %s in %v", name, payload)
	return ""
}

func TestDoctorCleanProjectAndMalformedFix(t *testing.T) {
	root, dir := doctorFixture(t)
	code, payload := runDoctorFixture(t, root)
	if code != 0 || payload["success"] != true || len(payload["checks"].([]any)) != 12 {
		t.Fatalf("clean doctor: code=%d payload=%v", code, payload)
	}
	path := filepath.Join(dir, model.IssuesFile)
	valid := `{"id":"test-a","title":"Valid","status":"open","type":"task","priority":2,"createdAt":"2026-01-01T00:00:00.000Z","updatedAt":"2026-01-01T00:00:00.000Z"}`
	if err := os.WriteFile(path, []byte(valid+"\nNOT JSON\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, payload = runDoctorFixture(t, root)
	if code != 1 || doctorCheckStatus(t, payload, "jsonl-integrity") != "fail" {
		t.Fatalf("malformed doctor: code=%d payload=%v", code, payload)
	}
	code, payload = runDoctorFixture(t, root, "--fix")
	if code != 0 || doctorCheckStatus(t, payload, "jsonl-integrity") != "pass" {
		t.Fatalf("fixed doctor: code=%d payload=%v", code, payload)
	}
	content, _ := os.ReadFile(path)
	if string(content) != valid+"\n" {
		t.Fatalf("repair changed valid line: %q", content)
	}
}

func TestDoctorRepairsLabelsExtensionsAndCloseMetadata(t *testing.T) {
	root, dir := doctorFixture(t)
	path := filepath.Join(dir, model.IssuesFile)
	issue := `{"id":"test-a","title":"Reopened","status":"open","type":"task","priority":2,"createdAt":"2026-01-01T00:00:00.000Z","updatedAt":"2026-01-01T00:00:00.000Z","labels":["manual", "", 1],"extensions":[],"closedAt":"2026-01-01T00:00:00.000Z","closeReason":"old"}`
	if err := os.WriteFile(path, []byte(issue+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, payload := runDoctorFixture(t, root)
	if code != 1 || doctorCheckStatus(t, payload, "label-schema") != "warn" || doctorCheckStatus(t, payload, "extensions-schema") != "warn" || doctorCheckStatus(t, payload, "closed-fields-consistency") != "fail" {
		t.Fatalf("before fix: code=%d payload=%v", code, payload)
	}
	code, payload = runDoctorFixture(t, root, "--fix")
	if code != 0 || doctorCheckStatus(t, payload, "closed-fields-consistency") != "pass" {
		t.Fatalf("after fix: code=%d payload=%v", code, payload)
	}
	content, _ := os.ReadFile(path)
	var repaired map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(content), &repaired); err != nil {
		t.Fatal(err)
	}
	if _, ok := repaired["extensions"]; ok {
		t.Fatalf("extensions retained: %v", repaired)
	}
	if _, ok := repaired["closedAt"]; ok {
		t.Fatalf("stale close data retained: %v", repaired)
	}
	if _, ok := repaired["closeReason"]; ok {
		t.Fatalf("stale close reason retained: %v", repaired)
	}
	if labels := repaired["labels"].([]any); len(labels) != 1 || labels[0] != "manual" {
		t.Fatalf("labels: %v", labels)
	}
}

func TestDoctorDependencyFixAndCycleWarning(t *testing.T) {
	root, dir := doctorFixture(t)
	path := filepath.Join(dir, model.IssuesFile)
	issues := []string{
		`{"id":"a","title":"A","status":"open","type":"task","priority":2,"createdAt":"2026-01-01T00:00:00.000Z","updatedAt":"2026-01-01T00:00:00.000Z","blockedBy":["b","missing"]}`,
		`{"id":"b","title":"B","status":"open","type":"task","priority":2,"createdAt":"2026-01-01T00:00:00.000Z","updatedAt":"2026-01-01T00:00:00.000Z"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(issues, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	code, payload := runDoctorFixture(t, root)
	if code != 0 || doctorCheckStatus(t, payload, "referential-integrity") != "warn" || doctorCheckStatus(t, payload, "bidirectional-consistency") != "warn" {
		t.Fatalf("dependency checks: code=%d payload=%v", code, payload)
	}
	code, payload = runDoctorFixture(t, root, "--fix")
	if code != 0 || doctorCheckStatus(t, payload, "referential-integrity") != "pass" || doctorCheckStatus(t, payload, "bidirectional-consistency") != "pass" {
		t.Fatalf("dependency fix: code=%d payload=%v", code, payload)
	}
}

func TestDoctorRemovesOnlyStaleLocks(t *testing.T) {
	root, dir := doctorFixture(t)
	stale := filepath.Join(dir, model.IssuesFile+".lock")
	active := filepath.Join(dir, model.TemplatesFile+".lock")
	for _, path := range []string{stale, active} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-40 * time.Second)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	code, payload := runDoctorFixture(t, root, "--fix")
	if code != 0 || doctorCheckStatus(t, payload, "stale-locks") != "warn" {
		t.Fatalf("lock fix: code=%d payload=%v", code, payload)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale lock remains: %v", err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active lock removed: %v", err)
	}
}
