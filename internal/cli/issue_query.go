package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func runSearch(ctx *Context, args []string) error {
	opts, err := queryOptions(args, []string{"status"}, nil)
	if err != nil {
		return err
	}
	mode, err := formatMode(opts)
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 {
		return fmt.Errorf("Usage: luuna-missions search <query> [filters]")
	}
	query := opts.Positionals[0]
	if err := validateStatus(opts.Value("status")); err != nil {
		return err
	}
	if err := validateType(opts.Value("type")); err != nil {
		return err
	}
	limit, err := parseLimit(opts.Value("limit"))
	if err != nil {
		return err
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	all, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	plans, err := loadPlanIndex(dir)
	if err != nil {
		return err
	}
	needle := strings.ToLower(query)
	candidates := make([]model.Issue, 0)
	for _, issue := range all {
		if status := opts.Value("status"); status != "" && issue.Status != status {
			continue
		}
		if strings.Contains(strings.ToLower(issue.Title), needle) || strings.Contains(strings.ToLower(issue.Description), needle) {
			candidates = append(candidates, issue)
		}
	}
	issues, err := filterIssues(candidates, opts)
	if err != nil {
		return err
	}
	sortMode := opts.Value("sort")
	if sortMode == "" {
		sortMode = "priority"
	}
	if err := sortIssues(issues, sortMode); err != nil {
		return err
	}
	if limit < len(issues) {
		issues = issues[:limit]
	}
	if mode == "json" {
		annotated := make([]issueWithPlan, 0, len(issues))
		for _, issue := range issues {
			annotated = append(annotated, annotateIssue(issue, plans))
		}
		return ctx.JSON(struct {
			Success bool            `json:"success"`
			Command string          `json:"command"`
			Query   string          `json:"query"`
			Issues  []issueWithPlan `json:"issues"`
			Count   int             `json:"count"`
		}{true, "search", query, annotated, len(issues)})
	}
	closed := closedIDs(all)
	if len(issues) == 0 && mode != "ids" && mode != "compact" {
		_, err = fmt.Fprintf(ctx.Stdout, "No issues match %q.\n", query)
		return err
	}
	for _, issue := range issues {
		line := oneLineIssue(issue, closed) + planSuffix(issue, plans)
		if mode == "ids" {
			line = issue.ID
		} else if mode == "compact" {
			line = compactIssue(issue, closed)
		}
		if _, err = fmt.Fprintln(ctx.Stdout, line); err != nil {
			return err
		}
	}
	if mode == "ids" || mode == "compact" {
		return nil
	}
	_, err = fmt.Fprintf(ctx.Stdout, "\n%d match(es)\n", len(issues))
	return err
}

func runBlocked(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"format"}, []string{"json"})
	if err != nil {
		return err
	}
	mode, err := formatMode(opts)
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'blocked'")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	all, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	plans, err := loadPlanIndex(dir)
	if err != nil {
		return err
	}
	closed := closedIDs(all)
	blocked := []model.Issue{}
	for _, issue := range all {
		if issue.Status != "closed" && effectivelyBlocked(issue, closed) {
			blocked = append(blocked, issue)
		}
	}
	if mode == "json" {
		return renderIssues(ctx, "blocked", mode, blocked, all, plans)
	}
	if len(blocked) == 0 && mode != "ids" && mode != "compact" {
		_, err = fmt.Fprintln(ctx.Stdout, "No blocked issues.")
		return err
	}
	for _, issue := range blocked {
		line := oneLineIssue(issue, closed) + planSuffix(issue, plans)
		if mode == "ids" {
			line = issue.ID
		} else if mode == "compact" {
			line = compactIssue(issue, closed)
		}
		if _, err = fmt.Fprintln(ctx.Stdout, line); err != nil {
			return err
		}
	}
	if mode == "ids" || mode == "compact" {
		return nil
	}
	_, err = fmt.Fprintf(ctx.Stdout, "\n%d blocked issue(s)\n", len(blocked))
	return err
}

type statsData struct {
	Total      int            `json:"total"`
	Open       int            `json:"open"`
	InProgress int            `json:"inProgress"`
	Closed     int            `json:"closed"`
	Blocked    int            `json:"blocked"`
	ByType     map[string]int `json:"byType"`
	ByPriority map[int]int    `json:"byPriority"`
	ByLabel    map[string]int `json:"byLabel"`
}

func runStats(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"format"}, []string{"json"})
	if err != nil {
		return err
	}
	mode, err := formatMode(opts)
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'stats'")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	data := statsData{Total: len(issues), ByType: map[string]int{}, ByPriority: map[int]int{}, ByLabel: map[string]int{}}
	typeOrder, labelOrder := []string{}, []string{}
	closed := closedIDs(issues)
	for _, issue := range issues {
		switch issue.Status {
		case "open":
			data.Open++
		case "in_progress":
			data.InProgress++
		case "closed":
			data.Closed++
		}
		if issue.Status != "closed" && effectivelyBlocked(issue, closed) {
			data.Blocked++
		}
		if data.ByType[issue.Type] == 0 {
			typeOrder = append(typeOrder, issue.Type)
		}
		data.ByType[issue.Type]++
		data.ByPriority[issue.Priority]++
		for _, label := range issue.Labels {
			if data.ByLabel[label] == 0 {
				labelOrder = append(labelOrder, label)
			}
			data.ByLabel[label]++
		}
	}
	labelOrder = sortedLabelsByCount(data.ByLabel, labelOrder)
	if mode == "json" {
		return ctx.JSON(struct {
			Success bool      `json:"success"`
			Command string    `json:"command"`
			Stats   statsData `json:"stats"`
		}{true, "stats", data})
	}
	if mode == "ids" {
		return nil
	}
	if mode == "compact" {
		if _, err = fmt.Fprintf(ctx.Stdout, "total=%d open=%d in_progress=%d closed=%d blocked=%d\n", data.Total, data.Open, data.InProgress, data.Closed, data.Blocked); err != nil {
			return err
		}
		if len(typeOrder) > 0 {
			parts := []string{}
			for _, k := range typeOrder {
				parts = append(parts, k+"="+strconv.Itoa(data.ByType[k]))
			}
			if _, err = fmt.Fprintln(ctx.Stdout, "by_type: "+strings.Join(parts, " ")); err != nil {
				return err
			}
		}
		priorities := make([]int, 0, len(data.ByPriority))
		for k := range data.ByPriority {
			priorities = append(priorities, k)
		}
		sort.Ints(priorities)
		if len(priorities) > 0 {
			parts := []string{}
			for _, k := range priorities {
				parts = append(parts, fmt.Sprintf("P%d=%d", k, data.ByPriority[k]))
			}
			if _, err = fmt.Fprintln(ctx.Stdout, "by_priority: "+strings.Join(parts, " ")); err != nil {
				return err
			}
		}
		if len(data.ByLabel) > 0 {
			parts := []string{}
			for _, k := range labelOrder {
				parts = append(parts, k+"="+strconv.Itoa(data.ByLabel[k]))
			}
			_, err = fmt.Fprintln(ctx.Stdout, "by_label: "+strings.Join(parts, " "))
			return err
		}
		return nil
	}
	lines := []string{"Project Statistics", fmt.Sprintf("  Total:       %d", data.Total), fmt.Sprintf("  Open:        %d", data.Open), fmt.Sprintf("  In progress: %d", data.InProgress), fmt.Sprintf("  Closed:      %d", data.Closed), fmt.Sprintf("  Blocked:     %d", data.Blocked), "", "By Type"}
	for _, k := range typeOrder {
		lines = append(lines, fmt.Sprintf("  %-10s %d", k, data.ByType[k]))
	}
	if len(data.ByPriority) > 0 {
		lines = append(lines, "", "By Priority")
		priorities := make([]int, 0, len(data.ByPriority))
		for k := range data.ByPriority {
			priorities = append(priorities, k)
		}
		sort.Ints(priorities)
		for _, k := range priorities {
			lines = append(lines, fmt.Sprintf("  P%d %-10s %d", k, model.PriorityLabels[k], data.ByPriority[k]))
		}
	}
	if len(data.ByLabel) > 0 {
		lines = append(lines, "", "By Label")
		for _, k := range labelOrder {
			lines = append(lines, fmt.Sprintf("  %-15s %d", k, data.ByLabel[k]))
		}
	}
	_, err = fmt.Fprintln(ctx.Stdout, strings.Join(lines, "\n"))
	return err
}

func sortedLabelsByCount(counts map[string]int, insertion []string) []string {
	keys := append([]string(nil), insertion...)
	sort.SliceStable(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	return keys
}
