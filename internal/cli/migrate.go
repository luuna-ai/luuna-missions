package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func stringField(record map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := record[name].(string); ok {
			return value
		}
	}
	return ""
}
func stringsField(record map[string]any, names ...string) []string {
	for _, name := range names {
		if values, ok := record[name].([]any); ok {
			out := []string{}
			for _, v := range values {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}
func beadsStatus(value string) string {
	if value == "in_progress" || value == "in-progress" {
		return "in_progress"
	}
	if value == "closed" || value == "done" || value == "complete" {
		return "closed"
	}
	return "open"
}
func beadsType(value string) string {
	for _, candidate := range []string{"bug", "feature", "epic"} {
		if value == candidate {
			return value
		}
	}
	return "task"
}

func mapBeadsIssue(record map[string]any) (model.Issue, bool) {
	id, title := stringField(record, "id"), stringField(record, "title")
	if id == "" || title == "" {
		return model.Issue{}, false
	}
	now := model.NowISO()
	priority := 2
	if value, ok := record["priority"].(float64); ok {
		priority = int(value)
	}
	created := stringField(record, "created_at", "createdAt")
	if created == "" {
		created = now
	}
	updated := stringField(record, "updated_at", "updatedAt")
	if updated == "" {
		updated = now
	}
	return model.Issue{ID: id, Title: title, Status: beadsStatus(stringField(record, "status")), Type: beadsType(stringField(record, "issue_type", "type")), Priority: priority, CreatedAt: created, UpdatedAt: updated, Assignee: stringField(record, "owner", "assignee"), Description: stringField(record, "description"), CloseReason: stringField(record, "close_reason", "closeReason"), BlockedBy: stringsField(record, "blocked_by", "blockedBy"), Blocks: stringsField(record, "blocks"), ClosedAt: stringField(record, "closed_at", "closedAt")}, true
}

func runMigrateBeads(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'migrate-from-beads'")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	path := filepath.Join(config.ProjectRoot(dir), ".beads", "issues.jsonl")
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("Beads issues not found at: %s", path)
	}
	if err != nil {
		return err
	}
	mapped := []model.Issue{}
	skipped := 0
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		issue, valid := mapBeadsIssue(record)
		if valid {
			mapped = append(mapped, issue)
		} else {
			skipped++
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	written := 0
	err = store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, issue := range issues {
			ids[issue.ID] = true
		}
		for _, issue := range mapped {
			if !ids[issue.ID] {
				issues = append(issues, issue)
				written++
			}
		}
		return store.WriteIssues(dir, issues)
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			Written int    `json:"written"`
			Skipped int    `json:"skipped"`
		}{true, "migrate-from-beads", written, skipped})
	}
	if _, err = fmt.Fprintf(ctx.Stdout, "Migrated %d issues from beads.\n", written); err != nil {
		return err
	}
	if skipped > 0 {
		_, err = fmt.Fprintf(ctx.Stdout, "Skipped %d malformed issues.\n", skipped)
	}
	return err
}
