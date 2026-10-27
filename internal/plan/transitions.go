package plan

import (
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

// ApplyTransitions mirrors src/plan-lifecycle.ts. Its caller holds the plans
// then issues locks and has already persisted the new issue statuses.
func ApplyTransitions(dir string, issueIDs []string, issues []model.Issue, now string) error {
	plans, err := store.ReadPlans(dir)
	if err != nil {
		return err
	}
	targets := make(map[string]bool, len(issueIDs))
	for _, id := range issueIDs {
		targets[id] = true
	}
	issueByID := make(map[string]model.Issue, len(issues))
	for _, issue := range issues {
		issueByID[issue.ID] = issue
	}
	changed := false
	for index := range plans {
		p := &plans[index]
		affected := false
		children := []model.Issue{}
		for _, childID := range p.Children {
			if targets[childID] {
				affected = true
			}
			if child, ok := issueByID[childID]; ok {
				children = append(children, child)
			}
		}
		if !affected || p.Status == "draft" || len(children) == 0 {
			continue
		}
		allClosed, anyProgress := true, false
		for _, child := range children {
			if child.Status != "closed" {
				allClosed = false
			}
			if child.Status == "in_progress" {
				anyProgress = true
			}
		}
		next := p.Status
		if allClosed {
			next = "done"
		} else if p.Status == "active" || p.Status == "done" || anyProgress {
			next = "active"
		} else {
			next = "approved"
		}
		if next != p.Status {
			p.Status = next
			p.UpdatedAt = now
			changed = true
		}
	}
	if changed {
		return store.WritePlans(dir, plans)
	}
	return nil
}
