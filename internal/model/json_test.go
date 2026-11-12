package model

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestIssueRewritePreservesUnknownFieldsAndNull(t *testing.T) {
	original := `{"id":"demo-a1b2","title":"Before","status":"open","type":"task","priority":2,"assignee":null,"custom":{"future":true},"createdAt":"2026-10-22T12:00:00.000Z","updatedAt":"2026-10-22T12:00:00.000Z"}`
	var issue Issue
	if err := json.Unmarshal([]byte(original), &issue); err != nil {
		t.Fatal(err)
	}
	issue.Title = "After"
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	got := string(encoded)
	if !strings.Contains(got, `"assignee":null`) || !strings.Contains(got, `"custom":{"future":true}`) {
		t.Fatalf("lost existing data: %s", got)
	}
	if !strings.Contains(got, `"title":"After"`) {
		t.Fatalf("title not updated: %s", got)
	}
	if !strings.HasPrefix(got, `{"id":"demo-a1b2","title":"After","status":"open"`) {
		t.Fatalf("lost original key ordering: %s", got)
	}
}

func TestIssueRewriteCanRemovePresentOptionalField(t *testing.T) {
	var issue Issue
	if err := json.Unmarshal([]byte(`{"id":"x","title":"t","status":"open","type":"task","priority":2,"assignee":"old","createdAt":"a","updatedAt":"b"}`), &issue); err != nil {
		t.Fatal(err)
	}
	issue.Assignee = ""
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "assignee") {
		t.Fatalf("assignee was not removed: %s", encoded)
	}
}

func TestNowISOHasMillisecondShape(t *testing.T) {
	pattern := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`)
	if got := NowISO(); !pattern.MatchString(got) {
		t.Fatalf("unexpected timestamp: %s", got)
	}
}

func TestTemplateStepKeepsFutureFields(t *testing.T) {
	var template Template
	input := `{"id":"tpl-a","name":"cycle","steps":[{"title":"First","future":{"enabled":true}}]}`
	if err := json.Unmarshal([]byte(input), &template); err != nil {
		t.Fatal(err)
	}
	template.Steps[0].Title = "Changed"
	encoded, err := json.Marshal(template)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"future":{"enabled":true}`) {
		t.Fatalf("nested field lost: %s", encoded)
	}
}

func TestIssueEmptySliceIsDistinctFromAbsent(t *testing.T) {
	var issue Issue
	if err := json.Unmarshal([]byte(`{"id":"x","title":"t","status":"open","type":"task","priority":2,"blockedBy":["y"],"createdAt":"a","updatedAt":"b"}`), &issue); err != nil {
		t.Fatal(err)
	}
	issue.BlockedBy = []string{}
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"blockedBy":[]`) {
		t.Fatalf("explicit empty array lost: %s", encoded)
	}
	issue.BlockedBy = nil
	encoded, err = json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"blockedBy"`) {
		t.Fatalf("removed array retained: %s", encoded)
	}
}

func TestPlanSectionKeysPreserveSourceOrder(t *testing.T) {
	var plan Plan
	input := `{"id":"pl-x","seed":"x","template":"feature","status":"approved","revision":1,"sections":{"steps":[],"context":"x","approach":"y"},"children":[],"createdAt":"a","updatedAt":"b"}`
	if err := json.Unmarshal([]byte(input), &plan); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan.SectionKeys(), ","); got != "steps,context,approach" {
		t.Fatalf("section order: %s", got)
	}
}

func TestNewPlanKeepsAuthorSectionOrder(t *testing.T) {
	plan := Plan{ID: "pl-x", Seed: "x", Template: "feature", Status: "approved", Revision: 1, Sections: map[string]any{"steps": []any{}, "context": "why", "approach": "how"}, Children: []string{}, CreatedAt: "a", UpdatedAt: "b"}
	plan.SetSectionOrder([]string{"context", "steps", "approach"})
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	context, steps, approach := strings.Index(string(encoded), `"context"`), strings.Index(string(encoded), `"steps"`), strings.Index(string(encoded), `"approach"`)
	if context < 0 || steps < context || approach < steps {
		t.Fatalf("new plan section order lost: %s", encoded)
	}
}

func TestIssueReadRetainsRecordWithMalformedOptionalField(t *testing.T) {
	input := `{"id":"x","title":"t","status":"open","type":"task","priority":2,"labels":"wrong type","createdAt":"a","updatedAt":"b"}`
	var issue Issue
	if err := json.Unmarshal([]byte(input), &issue); err != nil {
		t.Fatal(err)
	}
	if issue.ID != "x" || issue.Title != "t" {
		t.Fatalf("valid fields lost: %+v", issue)
	}
	encoded, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"labels":"wrong type"`) {
		t.Fatalf("unmodified malformed field lost: %s", encoded)
	}
	issue.Labels = []string{"fixed"}
	encoded, err = json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"labels":["fixed"]`) {
		t.Fatalf("field change not applied: %s", encoded)
	}
}
