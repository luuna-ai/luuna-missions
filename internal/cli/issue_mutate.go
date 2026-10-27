package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

// PlanTransitionHook is called inside the plans→issues lock after issue status
// changes. The plan package installs it through Register without an import cycle.
type PlanTransitionHook func(dir string, issueIDs []string, issues []model.Issue, now string) error

var planTransitionHook PlanTransitionHook

func SetPlanTransitionHook(hook PlanTransitionHook) { planTransitionHook = hook }

func runClose(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"reason"}, []string{"json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 {
		return fmt.Errorf("Usage: luuna-missions close <id> [ids...] [--reason text]")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	ids := append([]string(nil), opts.Positionals...)
	now := model.NowISO()
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		for _, id := range ids {
			idx := issueIndex(issues, id)
			if idx < 0 {
				return fmt.Errorf("Issue not found: %s", id)
			}
			issue := &issues[idx]
			issue.Status = "closed"
			issue.ClosedAt = now
			issue.UpdatedAt = now
			if reason := opts.Value("reason"); reason != "" {
				issue.CloseReason = reason
			}
			for _, blockedID := range issue.Blocks {
				blockedIdx := issueIndex(issues, blockedID)
				if blockedIdx < 0 {
					continue
				}
				issues[blockedIdx].BlockedBy = withoutString(issues[blockedIdx].BlockedBy, id)
				issues[blockedIdx].UpdatedAt = now
			}
		}
		if err := store.WriteIssues(dir, issues); err != nil {
			return err
		}
		if planTransitionHook != nil {
			return planTransitionHook(dir, ids, issues, now)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool     `json:"success"`
			Command string   `json:"command"`
			Closed  []string `json:"closed"`
		}{true, "close", ids})
	}
	for _, id := range ids {
		if err := ctx.Success("Closed " + id + func() string {
			if r := opts.Value("reason"); r != "" {
				return " — " + r
			}
			return ""
		}()); err != nil {
			return err
		}
	}
	return nil
}

func issueIndex(issues []model.Issue, id string) int {
	for i := range issues {
		if issues[i].ID == id {
			return i
		}
	}
	return -1
}
func withoutString(items []string, target string) []string {
	next := []string{}
	for _, v := range items {
		if v != target {
			next = append(next, v)
		}
	}
	if len(next) == 0 {
		return nil
	}
	return next
}
func uniqueStrings(items []string) []string {
	next := []string{}
	seen := map[string]bool{}
	for _, v := range items {
		if !seen[v] {
			next = append(next, v)
			seen[v] = true
		}
	}
	return next
}

func parseExtensionsPatch(raw string) (map[string]any, error) {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("--extensions must be valid JSON: %s", err)
	}
	patch, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("--extensions must be a JSON object (not array, null, or scalar)")
	}
	return patch, nil
}

func runUpdate(ctx *Context, args []string) error {
	opts, err := ParseOptions(args,
		[]string{"status", "title", "assignee", "description", "desc", "body", "type", "priority", "add-label", "remove-label", "set-labels", "extensions", "as", "intent"},
		[]string{"clear-extensions", "claim", "json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 {
		return fmt.Errorf("Usage: luuna-missions update <id> [flags]")
	}
	if len(opts.Positionals) > 1 {
		return fmt.Errorf("too many arguments for 'update'")
	}
	id := opts.Positionals[0]
	claim := opts.Has("claim")
	claimant := ""
	if claim {
		if opts.Has("status") || opts.Has("assignee") {
			return fmt.Errorf("--claim cannot be combined with --status or --assignee")
		}
		claimant = strings.TrimSpace(opts.Value("as"))
		if claimant == "" {
			claimant = strings.TrimSpace(os.Getenv("USER"))
		}
		if claimant == "" {
			return fmt.Errorf("--claim needs an identity: pass --as <agent>")
		}
	}
	if opts.Has("extensions") && opts.Has("clear-extensions") {
		return fmt.Errorf("--extensions and --clear-extensions are mutually exclusive")
	}
	if opts.Has("status") {
		if err := validateStatus(opts.Value("status")); err != nil {
			return err
		}
	}
	if opts.Has("type") {
		if err := validateType(opts.Value("type")); err != nil {
			return err
		}
	}
	priority := 0
	if opts.Has("priority") {
		priority, err = parsePriority(opts.Value("priority"))
		if err != nil {
			return err
		}
	}
	var intent any
	if opts.Has("intent") {
		intent, err = parseIntent(opts.Repeated("intent"))
		if err != nil {
			return err
		}
	}
	var extensionsPatch map[string]any
	if opts.Has("extensions") {
		extensionsPatch, err = parseExtensionsPatch(opts.Value("extensions"))
		if err != nil {
			return err
		}
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	statusChanging := opts.Has("status") || claim
	lockPaths := []string{store.IssuesPath(dir)}
	if statusChanging {
		lockPaths = []string{store.PlansPath(dir), store.IssuesPath(dir)}
	}
	var updated model.Issue
	err = store.WithLocks(lockPaths, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		idx := issueIndex(issues, id)
		if idx < 0 {
			return fmt.Errorf("Issue not found: %s", id)
		}
		issue := &issues[idx]
		oldStatus := issue.Status
		if claim {
			if issue.Status != "open" {
				by := ""
				if issue.Assignee != "" {
					by = " by " + issue.Assignee
				}
				return fmt.Errorf("Cannot claim %s: status is %s%s", id, issue.Status, by)
			}
			if issue.Assignee != "" && issue.Assignee != claimant {
				return fmt.Errorf("Cannot claim %s: already assigned to %s", id, issue.Assignee)
			}
			issue.Status = "in_progress"
			issue.Assignee = claimant
		}
		if opts.Has("status") {
			issue.Status = opts.Value("status")
			if issue.Status != "closed" {
				issue.ClosedAt = ""
				issue.CloseReason = ""
			}
		}
		if opts.Has("title") {
			title := strings.TrimSpace(opts.Value("title"))
			if title == "" {
				return fmt.Errorf("--title must not be empty")
			}
			issue.Title = title
		}
		if opts.Has("assignee") {
			issue.Assignee = opts.Value("assignee")
		}
		if opts.Has("description") {
			issue.Description = opts.Value("description")
		} else if opts.Has("desc") {
			issue.Description = opts.Value("desc")
		} else if opts.Has("body") {
			issue.Description = opts.Value("body")
		}
		if opts.Has("type") {
			issue.Type = opts.Value("type")
		}
		if opts.Has("priority") {
			issue.Priority = priority
		}
		if opts.Has("intent") {
			issue.Intent = intent
		}
		if opts.Has("clear-extensions") {
			issue.Extensions = nil
		} else if opts.Has("extensions") {
			if issue.Extensions == nil {
				issue.Extensions = map[string]any{}
			}
			for key, value := range extensionsPatch {
				issue.Extensions[key] = value
			}
			if len(issue.Extensions) == 0 {
				issue.Extensions = nil
			}
		}
		if opts.Has("set-labels") {
			issue.Labels = splitLabels(opts.Value("set-labels"))
		}
		if opts.Has("add-label") {
			issue.Labels = uniqueStrings(append(issue.Labels, splitLabels(opts.Value("add-label"))...))
		}
		if opts.Has("remove-label") {
			for _, label := range splitLabels(opts.Value("remove-label")) {
				issue.Labels = withoutString(issue.Labels, label)
			}
		}
		issue.UpdatedAt = model.NowISO()
		updated = *issue
		if err := store.WriteIssues(dir, issues); err != nil {
			return err
		}
		if statusChanging && oldStatus != issue.Status && planTransitionHook != nil {
			return planTransitionHook(dir, []string{id}, issues, issue.UpdatedAt)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool        `json:"success"`
			Command string      `json:"command"`
			Issue   model.Issue `json:"issue"`
		}{true, "update", updated})
	}
	return ctx.Success("Updated " + id)
}
