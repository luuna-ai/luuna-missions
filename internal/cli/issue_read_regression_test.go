package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/plan"
)

func fixtureApp(t *testing.T) (*cli.App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	app := cli.New()
	app.Cwd = t.TempDir()
	cli.RegisterBuiltins(app)
	plan.Register(app)
	var stdout, stderr bytes.Buffer
	app.Stdout, app.Stderr = &stdout, &stderr
	return app, &stdout, &stderr
}

func runFixtureJSON(t *testing.T, app *cli.App, stdout, stderr *bytes.Buffer, args ...string) map[string]any {
	t.Helper()
	stdout.Reset()
	stderr.Reset()
	if code := app.Run(args); code != 0 {
		t.Fatalf("%v exited %d: stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("%v returned invalid JSON %q: %v", args, stdout.String(), err)
	}
	return payload
}

func issueByID(t *testing.T, payload map[string]any, id string) map[string]any {
	t.Helper()
	for _, value := range payload["issues"].([]any) {
		issue := value.(map[string]any)
		if issue["id"] == id {
			return issue
		}
	}
	t.Fatalf("issue %s absent from %v", id, payload)
	return nil
}

func assertApprovedPlanContext(t *testing.T, payload map[string]any, id string) {
	t.Helper()
	issue := issueByID(t, payload, id)
	if issue["plan_status"] != "approved" {
		t.Fatalf("missing plan_status on %s: %v", id, issue)
	}
	children, ok := issue["plan_children"].([]any)
	if !ok || len(children) != 0 {
		t.Fatalf("plan_children must be an empty array on %s: %v", id, issue)
	}
}

func TestIssueQueriesIncludePlanContext(t *testing.T) {
	app, stdout, stderr := fixtureApp(t)
	runFixtureJSON(t, app, stdout, stderr, "init", "--json")
	parent := runFixtureJSON(t, app, stdout, stderr, "create", "--title", "Planned parent", "--type", "feature", "--json")["id"].(string)
	runFixtureJSON(t, app, stdout, stderr, "plan", "create", parent, "--json")
	assertApprovedPlanContext(t, runFixtureJSON(t, app, stdout, stderr, "list", "--json"), parent)
	assertApprovedPlanContext(t, runFixtureJSON(t, app, stdout, stderr, "ready", "--json"), parent)
	assertApprovedPlanContext(t, runFixtureJSON(t, app, stdout, stderr, "search", "Planned", "--json"), parent)
	blocker := runFixtureJSON(t, app, stdout, stderr, "create", "--title", "Blocker", "--json")["id"].(string)
	runFixtureJSON(t, app, stdout, stderr, "block", parent, "--by", blocker, "--json")
	assertApprovedPlanContext(t, runFixtureJSON(t, app, stdout, stderr, "blocked", "--json"), parent)
}

func TestReadyRespectScheduleExcludesFutureDateOnly(t *testing.T) {
	app, stdout, stderr := fixtureApp(t)
	runFixtureJSON(t, app, stdout, stderr, "init", "--json")
	id := runFixtureJSON(t, app, stdout, stderr, "create", "--title", "Future task", "--json")["id"].(string)
	runFixtureJSON(t, app, stdout, stderr, "update", id, "--extensions", `{"scheduledFor":"2999-01-01"}`, "--json")
	issueByID(t, runFixtureJSON(t, app, stdout, stderr, "ready", "--json"), id)
	ready := runFixtureJSON(t, app, stdout, stderr, "ready", "--respect-schedule", "--json")
	if ready["count"] != float64(0) {
		t.Fatalf("future date-only task is ready: %v", ready)
	}
}
