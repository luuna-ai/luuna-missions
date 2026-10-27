package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func runEditFixture(t *testing.T, root string, args ...string) (int, string, string) {
	t.Helper()
	app := cli.New()
	app.Cwd = root
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	Register(app)
	code := app.Run(append([]string{"plan", "edit"}, args...))
	return code, stdout.String(), stderr.String()
}

func TestEditThroughCLIUpdatesPlanAndChildMetadata(t *testing.T) {
	root, dir := submitFixture(t)
	file := writeSubmitFixture(t, root, []any{
		map[string]any{"title": "First child", "type": "task", "priority": 2, "blocks": []int{2}},
		map[string]any{"title": "Second child", "type": "task", "priority": 2},
	})
	code, stdout, stderr := runSubmitFixture(t, root, "test-parent", "--plan", file, "--json")
	if code != 0 {
		t.Fatalf("submit exit=%d stderr=%s", code, stderr)
	}
	var submitted struct {
		PlanID   string   `json:"plan_id"`
		Children []string `json:"children"`
	}
	if err := json.Unmarshal([]byte(stdout), &submitted); err != nil {
		t.Fatal(err)
	}
	if len(submitted.Children) != 2 {
		t.Fatalf("submitted children: %+v", submitted)
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	firstIndex := issueIndex(issues, submitted.Children[0])
	issues[firstIndex].Description += "\n\nHand-written note"
	if err := store.WriteIssues(dir, issues); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = runEditFixture(t, root, "test-parent", "--name", "  Revised plan  ", "--section", "approach", "Use a revised and testable approach.", "--step", "1", "--title", "  Renamed child  ", "--priority", "P0", "--type", "bug", "--json")
	if code != 0 {
		t.Fatalf("edit exit=%d stderr=%s", code, stderr)
	}
	var edited struct {
		Command            string   `json:"command"`
		PlanID             string   `json:"plan_id"`
		Revision           int      `json:"revision"`
		Edited             []string `json:"edited"`
		Name               string   `json:"name"`
		BackrefsRefreshed  int      `json:"backrefs_refreshed"`
		PropagatedChildren []string `json:"propagated_children"`
	}
	if err := json.Unmarshal([]byte(stdout), &edited); err != nil {
		t.Fatal(err)
	}
	if edited.Command != "plan edit" || edited.PlanID != submitted.PlanID || edited.Revision != 2 || edited.Name != "Revised plan" || edited.BackrefsRefreshed != 2 {
		t.Fatalf("edit result: %+v", edited)
	}
	if strings.Join(edited.Edited, ",") != "name,section:approach,step:1:title,step:1:priority,step:1:type" || strings.Join(edited.PropagatedChildren, ",") != submitted.Children[0] {
		t.Fatalf("edit details: %+v", edited)
	}
	plans, err := store.ReadPlans(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].Name != "Revised plan" || plans[0].Revision != 2 || plans[0].Sections["approach"] != "Use a revised and testable approach." {
		t.Fatalf("plan record: %+v", plans)
	}
	steps, ok := plans[0].Sections["steps"].([]any)
	if !ok {
		t.Fatalf("plan steps: %#v", plans[0].Sections["steps"])
	}
	firstStep := steps[0].(map[string]any)
	if firstStep["title"] != "Renamed child" || firstStep["priority"] != float64(0) || firstStep["type"] != "bug" {
		t.Fatalf("first plan step: %#v", firstStep)
	}
	issues, err = store.ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := issues[issueIndex(issues, submitted.Children[0])]
	second := issues[issueIndex(issues, submitted.Children[1])]
	if first.Title != "Renamed child" || first.Priority != 0 || first.Type != "bug" || !strings.Contains(first.Description, "Use a revised and testable approach.") || !strings.Contains(first.Description, "Hand-written note") {
		t.Fatalf("first child: %+v", first)
	}
	if second.Title != "Second child" || !strings.Contains(second.Description, "Use a revised and testable approach.") {
		t.Fatalf("second child: %+v", second)
	}
}

func TestEditRejectsInvalidFieldsWithoutWriting(t *testing.T) {
	root, dir := submitFixture(t)
	file := writeSubmitFixture(t, root, []any{map[string]any{"title": "First child"}, map[string]any{"title": "Second child"}})
	code, _, stderr := runSubmitFixture(t, root, "test-parent", "--plan", file, "--json")
	if code != 0 {
		t.Fatalf("submit exit=%d stderr=%s", code, stderr)
	}
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"no fields", []string{"test-parent", "--json"}, "No fields to edit"},
		{"step without patch", []string{"test-parent", "--step", "1", "--json"}, "--step requires at least one"},
		{"title without step", []string{"test-parent", "--title", "Bad", "--json"}, "require --step"},
		{"out of range", []string{"test-parent", "--step", "3", "--title", "Bad", "--json"}, "out of range"},
		{"nontext section", []string{"test-parent", "--section", "steps", "bad", "--json"}, "kind=text only"},
		{"short context", []string{"test-parent", "--section", "context", "short", "--json"}, "at least 50 characters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			beforePlans, err := os.ReadFile(store.PlansPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			beforeIssues, err := os.ReadFile(store.IssuesPath(dir))
			if err != nil {
				t.Fatal(err)
			}
			code, stdout, _ := runEditFixture(t, root, test.args...)
			if code != 1 || !strings.Contains(stdout, test.want) {
				t.Fatalf("edit exit=%d stdout=%q, want %q", code, stdout, test.want)
			}
			afterPlans, _ := os.ReadFile(store.PlansPath(dir))
			afterIssues, _ := os.ReadFile(store.IssuesPath(dir))
			if !bytes.Equal(beforePlans, afterPlans) || !bytes.Equal(beforeIssues, afterIssues) {
				t.Fatal("invalid edit changed store")
			}
		})
	}
}
