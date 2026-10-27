package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func submitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := config.Init(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, model.MissionsDir)
	issues := []model.Issue{
		{ID: "test-parent", Title: "Parent mission", Status: "open", Type: "feature", Priority: 2, CreatedAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z"},
		{ID: "test-existing", Title: "Original adopted title", Status: "in_progress", Type: "bug", Priority: 1, Labels: []string{"manual"}, Description: "User notes", CreatedAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z"},
	}
	if err := store.WriteIssues(dir, issues); err != nil {
		t.Fatal(err)
	}
	return root, dir
}

func writeSubmitFixture(t *testing.T, root string, steps []any) string {
	t.Helper()
	data := map[string]any{
		"template": "feature",
		"sections": map[string]any{
			"context":    "The parent mission needs a plan that keeps all work visible and connected to the original goal.",
			"approach":   "Use linked child missions and preserve their existing user metadata.",
			"steps":      steps,
			"acceptance": []string{"The parent has all current children as blockers."},
		},
	}
	content, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "plan.json")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runSubmitFixture(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	app := cli.New()
	app.Cwd = root
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	Register(app)
	code := app.Run(append([]string{"plan", "submit"}, args...))
	return code, stdout.String(), stderr.String()
}

func TestSubmitAdoptionAndOverwritePreserveUserData(t *testing.T) {
	root, dir := submitFixture(t)
	file := writeSubmitFixture(t, root, []any{
		map[string]any{"existing_seed": "test-existing", "labels": []string{" NEW ", "manual"}, "blocks": []int{2}},
		map[string]any{"title": "New child", "type": "task", "priority": 0},
	})
	code, stdout, stderr := runSubmitFixture(t, root, "test-parent", "--plan", file, "--json")
	if code != 0 {
		t.Fatalf("submit exit=%d, stderr=%s", code, stderr)
	}
	var first struct {
		PlanID   string   `json:"plan_id"`
		Children []string `json:"children"`
		Revision int      `json:"revision"`
	}
	if err := json.Unmarshal([]byte(stdout), &first); err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || len(first.Children) != 2 || first.Children[0] != "test-existing" {
		t.Fatalf("result: %+v", first)
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	adopted := issues[issueIndex(issues, "test-existing")]
	if adopted.Title != "Original adopted title" || adopted.Status != "in_progress" || adopted.Priority != 1 {
		t.Fatalf("adopted fields changed: %+v", adopted)
	}
	if adopted.PlanID != first.PlanID || adopted.PlanStepIndex == nil || *adopted.PlanStepIndex != 0 {
		t.Fatalf("adopted link: %+v", adopted)
	}
	if strings.Join(adopted.Labels, ",") != "manual,new" || !strings.Contains(adopted.Description, "User notes") {
		t.Fatalf("adopted data: %+v", adopted)
	}
	if strings.Join(adopted.Blocks, ",") != first.Children[1]+",test-parent" {
		t.Fatalf("adopted blocks: %v", adopted.Blocks)
	}
	spawned := issues[issueIndex(issues, first.Children[1])]
	if spawned.Priority != 0 || strings.Join(spawned.BlockedBy, ",") != "test-existing" {
		t.Fatalf("spawned: %+v", spawned)
	}
	parent := issues[issueIndex(issues, "test-parent")]
	if strings.Join(parent.BlockedBy, ",") != strings.Join(first.Children, ",") {
		t.Fatalf("parent blockers: %v", parent.BlockedBy)
	}

	file = writeSubmitFixture(t, root, []any{
		map[string]any{"existing_seed": "test-existing"},
		map[string]any{"title": "Replacement child"},
	})
	code, stdout, stderr = runSubmitFixture(t, root, "test-parent", "--plan", file, "--overwrite", "--json")
	if code != 0 {
		t.Fatalf("overwrite exit=%d, stderr=%s", code, stderr)
	}
	var second struct {
		Children    []string `json:"children"`
		Revision    int      `json:"revision"`
		Obsolete    []string `json:"obsolete"`
		Overwritten bool     `json:"overwritten"`
	}
	if err := json.Unmarshal([]byte(stdout), &second); err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 || !second.Overwritten || second.Children[0] != "test-existing" || len(second.Obsolete) != 1 || second.Obsolete[0] != first.Children[1] {
		t.Fatalf("overwrite result: %+v", second)
	}
	issues, err = store.ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	parent = issues[issueIndex(issues, "test-parent")]
	if strings.Join(parent.BlockedBy, ",") != strings.Join(second.Children, ",") {
		t.Fatalf("overwrite blockers: %v", parent.BlockedBy)
	}
	obsolete := issues[issueIndex(issues, first.Children[1])]
	if obsolete.Status != "open" {
		t.Fatalf("obsolete child was auto-closed: %+v", obsolete)
	}
	plans, err := store.ReadPlans(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Revision != 2 || strings.Join(plans[0].AdoptedChildren, ",") != "test-existing" {
		t.Fatalf("plan rows: %+v", plans)
	}
}

func TestSubmitValidationFailureDoesNotWrite(t *testing.T) {
	root, dir := submitFixture(t)
	file := writeSubmitFixture(t, root, []any{map[string]any{"blocks": []int{1}}})
	beforeIssues, _ := os.ReadFile(store.IssuesPath(dir))
	beforePlans, _ := os.ReadFile(store.PlansPath(dir))
	code, stdout, stderr := runSubmitFixture(t, root, "test-parent", "--plan", file)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "missing-title") {
		t.Fatalf("validation exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	afterIssues, _ := os.ReadFile(store.IssuesPath(dir))
	afterPlans, _ := os.ReadFile(store.PlansPath(dir))
	if !bytes.Equal(beforeIssues, afterIssues) || !bytes.Equal(beforePlans, afterPlans) {
		t.Fatal("invalid submission changed store")
	}
}

func TestSubmitBackrefPreservesManualText(t *testing.T) {
	index := 0
	first := submitApplyBackref("Before\n\nAfter", &index, "pl-1", "seed-1", "Parent", "feature", "Use the old approach")
	second := submitApplyBackref(first, &index, "pl-1", "seed-1", "Parent", "feature", "Use the new approach")
	if strings.Count(second, submitBackrefStart) != 1 || !strings.Contains(second, "new approach") || strings.Contains(second, "old approach") {
		t.Fatalf("backref not replaced: %q", second)
	}
	if submitStripBackref(second) != "Before\n\nAfter" {
		t.Fatalf("manual text lost: %q", submitStripBackref(second))
	}
}
