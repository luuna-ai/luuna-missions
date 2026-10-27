package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func addDependency(dir, issueID, blockerID string) error {
	if issueID == blockerID {
		return fmt.Errorf("Cannot add a self-dependency: %s", issueID)
	}
	return store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		i, j := issueIndex(issues, issueID), issueIndex(issues, blockerID)
		if i < 0 {
			return fmt.Errorf("Issue not found: %s", issueID)
		}
		if j < 0 {
			return fmt.Errorf("Issue not found: %s", blockerID)
		}
		now := model.NowISO()
		issues[i].BlockedBy = uniqueStrings(append(issues[i].BlockedBy, blockerID))
		issues[j].Blocks = uniqueStrings(append(issues[j].Blocks, issueID))
		issues[i].UpdatedAt, issues[j].UpdatedAt = now, now
		return store.WriteIssues(dir, issues)
	})
}

func removeDependency(dir, issueID, blockerID string) error {
	return store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		i, j := issueIndex(issues, issueID), issueIndex(issues, blockerID)
		if i < 0 {
			return fmt.Errorf("Issue not found: %s", issueID)
		}
		if j < 0 {
			return fmt.Errorf("Issue not found: %s", blockerID)
		}
		now := model.NowISO()
		issues[i].BlockedBy = withoutString(issues[i].BlockedBy, blockerID)
		issues[j].Blocks = withoutString(issues[j].Blocks, issueID)
		issues[i].UpdatedAt, issues[j].UpdatedAt = now, now
		return store.WriteIssues(dir, issues)
	})
}

func runDep(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	p := opts.Positionals
	if len(p) == 0 {
		return fmt.Errorf("Usage: luuna-missions dep <add|remove|list> <issue> [depends-on]")
	}
	cmd := p[0]
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	if cmd == "list" {
		if len(p) < 2 {
			return fmt.Errorf("Usage: luuna-missions dep list <issue>")
		}
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		idx := issueIndex(issues, p[1])
		if idx < 0 {
			return fmt.Errorf("Issue not found: %s", p[1])
		}
		issue := issues[idx]
		blockedBy := issue.BlockedBy
		if blockedBy == nil {
			blockedBy = []string{}
		}
		blocks := issue.Blocks
		if blocks == nil {
			blocks = []string{}
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success   bool     `json:"success"`
				Command   string   `json:"command"`
				IssueID   string   `json:"issueId"`
				BlockedBy []string `json:"blockedBy"`
				Blocks    []string `json:"blocks"`
			}{true, "dep list", p[1], blockedBy, blocks})
		}
		if _, err = fmt.Fprintf(ctx.Stdout, "%s dependencies:\n", p[1]); err != nil {
			return err
		}
		closed := closedIDs(issues)
		for _, group := range []struct {
			name string
			ids  []string
		}{{"Blocked by", blockedBy}, {"Blocks", blocks}} {
			if len(group.ids) == 0 {
				continue
			}
			if _, err = fmt.Fprintf(ctx.Stdout, "  %s:\n", group.name); err != nil {
				return err
			}
			for _, id := range group.ids {
				line := id + " (not found)"
				if at := issueIndex(issues, id); at >= 0 {
					line = oneLineIssue(issues[at], closed)
				}
				if _, err = fmt.Fprintln(ctx.Stdout, "    "+line); err != nil {
					return err
				}
			}
		}
		if len(blockedBy) == 0 && len(blocks) == 0 {
			_, err = fmt.Fprintln(ctx.Stdout, "  No dependencies.")
		}
		return err
	}
	if cmd != "add" && cmd != "remove" {
		return fmt.Errorf("Unknown dep subcommand: %s. Use add, remove, or list.", cmd)
	}
	if len(p) < 3 {
		return fmt.Errorf("Usage: luuna-missions dep %s <issue> <depends-on>", cmd)
	}
	if cmd == "add" {
		err = addDependency(dir, p[1], p[2])
	} else {
		err = removeDependency(dir, p[1], p[2])
	}
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success     bool   `json:"success"`
			Command     string `json:"command"`
			IssueID     string `json:"issueId"`
			DependsOnID string `json:"dependsOnId"`
		}{true, "dep " + cmd, p[1], p[2]})
	}
	verb := "Added"
	if cmd == "remove" {
		verb = "Removed"
	}
	_, err = fmt.Fprintf(ctx.Stdout, "%s dependency: %s → %s\n", verb, p[1], p[2])
	return err
}

func runBlock(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"by"}, []string{"json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 || !opts.Has("by") || opts.Value("by") == "" {
		return fmt.Errorf("Usage: luuna-missions block <id> --by <blocker-id>")
	}
	id, blocker := opts.Positionals[0], opts.Value("by")
	if id == blocker {
		return fmt.Errorf("Cannot block an issue by itself: %s", id)
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	if err = addDependency(dir, id, blocker); err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success   bool   `json:"success"`
			Command   string `json:"command"`
			IssueID   string `json:"issueId"`
			BlockerID string `json:"blockerId"`
		}{true, "block", id, blocker})
	}
	_, err = fmt.Fprintf(ctx.Stdout, "%s is now blocked by %s\n", id, blocker)
	return err
}

func runUnblock(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"from"}, []string{"all", "json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 || (!opts.Has("all") && !opts.Has("from")) {
		return fmt.Errorf("Usage: luuna-missions unblock <id> [--from <blocker-id> | --all]")
	}
	if opts.Has("from") && opts.Value("from") == "" {
		return fmt.Errorf("--from requires a blocker ID")
	}
	id := opts.Positionals[0]
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	removed := []string{}
	err = store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		i := issueIndex(issues, id)
		if i < 0 {
			return fmt.Errorf("Issue not found: %s", id)
		}
		current := issues[i].BlockedBy
		if opts.Has("all") {
			closed := closedIDs(issues)
			for _, bid := range current {
				if closed[bid] {
					removed = append(removed, bid)
				}
			}
		} else {
			bid := opts.Value("from")
			found := false
			for _, v := range current {
				if v == bid {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s is not blocked by %s", id, bid)
			}
			removed = append(removed, bid)
		}
		now := model.NowISO()
		issues[i].UpdatedAt = now
		for _, bid := range removed {
			issues[i].BlockedBy = withoutString(issues[i].BlockedBy, bid)
			if j := issueIndex(issues, bid); j >= 0 {
				issues[j].Blocks = withoutString(issues[j].Blocks, id)
				issues[j].UpdatedAt = now
			}
		}
		return store.WriteIssues(dir, issues)
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool     `json:"success"`
			Command string   `json:"command"`
			IssueID string   `json:"issueId"`
			Removed []string `json:"removed"`
		}{true, "unblock", id, removed})
	}
	if len(removed) == 0 {
		_, err = fmt.Fprintf(ctx.Stdout, "No closed blockers to remove from %s.\n", id)
		return err
	}
	for _, bid := range removed {
		if _, err = fmt.Fprintf(ctx.Stdout, "%s unblocked from %s\n", id, bid); err != nil {
			return err
		}
	}
	return nil
}

func runLabel(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	p := opts.Positionals
	if len(p) == 0 {
		return fmt.Errorf("Usage: luuna-missions label <add|remove|list|list-all>")
	}
	cmd := p[0]
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	if cmd == "list-all" {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		counts := map[string]int{}
		for _, issue := range issues {
			for _, label := range issue.Labels {
				counts[label]++
			}
		}
		labels := make([]string, 0, len(counts))
		for label := range counts {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool           `json:"success"`
				Command string         `json:"command"`
				Labels  []string       `json:"labels"`
				Counts  map[string]int `json:"counts"`
			}{true, "label list-all", labels, counts})
		}
		if len(labels) == 0 {
			_, err = fmt.Fprintln(ctx.Stdout, "No labels found.")
			return err
		}
		for _, label := range labels {
			if _, err = fmt.Fprintf(ctx.Stdout, "  %-20s %d\n", label, counts[label]); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintf(ctx.Stdout, "\n%d label(s)\n", len(labels))
		return err
	}
	if cmd == "list" {
		if len(p) < 2 {
			return fmt.Errorf("Usage: luuna-missions label list <issue>")
		}
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		i := issueIndex(issues, p[1])
		if i < 0 {
			return fmt.Errorf("Issue not found: %s", p[1])
		}
		labels := issues[i].Labels
		if labels == nil {
			labels = []string{}
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool     `json:"success"`
				Command string   `json:"command"`
				IssueID string   `json:"issueId"`
				Labels  []string `json:"labels"`
			}{true, "label list", p[1], labels})
		}
		if len(labels) == 0 {
			_, err = fmt.Fprintf(ctx.Stdout, "%s has no labels.\n", p[1])
			return err
		}
		if _, err = fmt.Fprintf(ctx.Stdout, "%s labels:\n", p[1]); err != nil {
			return err
		}
		for _, label := range labels {
			if _, err = fmt.Fprintln(ctx.Stdout, "  "+label); err != nil {
				return err
			}
		}
		return nil
	}
	if cmd != "add" && cmd != "remove" {
		return fmt.Errorf("Unknown label subcommand: %s. Use add, remove, list, or list-all.", cmd)
	}
	if len(p) < 3 {
		return fmt.Errorf("Usage: luuna-missions label %s <issue> <label> [<label2> ...]", cmd)
	}
	labels := []string{}
	for _, raw := range p[2:] {
		if label := strings.ToLower(strings.TrimSpace(raw)); label != "" {
			labels = append(labels, label)
		}
	}
	if cmd == "add" && len(labels) == 0 {
		return fmt.Errorf("No valid labels provided")
	}
	labels = uniqueStrings(labels)
	err = store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		i := issueIndex(issues, p[1])
		if i < 0 {
			return fmt.Errorf("Issue not found: %s", p[1])
		}
		if cmd == "add" {
			issues[i].Labels = uniqueStrings(append(issues[i].Labels, labels...))
		} else {
			for _, label := range labels {
				issues[i].Labels = withoutString(issues[i].Labels, label)
			}
		}
		issues[i].UpdatedAt = model.NowISO()
		return store.WriteIssues(dir, issues)
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool     `json:"success"`
			Command string   `json:"command"`
			IssueID string   `json:"issueId"`
			Labels  []string `json:"labels"`
		}{true, "label " + cmd, p[1], labels})
	}
	if cmd == "add" {
		return ctx.Success("Added label(s) " + strings.Join(labels, ", ") + " to " + p[1])
	}
	return ctx.Success("Removed label(s) from " + p[1])
}
