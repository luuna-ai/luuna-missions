package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

var issueFormats = []string{"markdown", "compact", "plain", "ids", "json"}
var issueSorts = []string{"priority", "created", "updated", "id"}

type issueWithPlan struct {
	model.Issue
	PlanStatus   string    `json:"plan_status,omitempty"`
	PlanChildren *[]string `json:"plan_children,omitempty"`
}

// Issue has a custom marshaler that preserves unknown record fields. Embedding
// it promotes that marshaler, so append plan context explicitly to its output.
func (entry issueWithPlan) MarshalJSON() ([]byte, error) {
	issueJSON, err := entry.Issue.MarshalJSON()
	if err != nil || (entry.PlanStatus == "" && entry.PlanChildren == nil) {
		return issueJSON, err
	}
	if len(issueJSON) < 2 || issueJSON[0] != '{' || issueJSON[len(issueJSON)-1] != '}' {
		return nil, fmt.Errorf("issue JSON not an object")
	}
	// A stored extension could already use one of these names. Match the
	// TypeScript object spread by replacing it rather than emitting duplicate keys.
	var existing map[string]json.RawMessage
	if err := json.Unmarshal(issueJSON, &existing); err != nil {
		return nil, err
	}
	_, hasStatus := existing["plan_status"]
	_, hasChildren := existing["plan_children"]
	if hasStatus || hasChildren {
		if entry.PlanStatus != "" {
			existing["plan_status"], err = json.Marshal(entry.PlanStatus)
			if err != nil {
				return nil, err
			}
		}
		if entry.PlanChildren != nil {
			existing["plan_children"], err = json.Marshal(entry.PlanChildren)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(existing)
	}
	var out bytes.Buffer
	out.Write(issueJSON[:len(issueJSON)-1])
	if len(issueJSON) > 2 {
		out.WriteByte(',')
	}
	if entry.PlanStatus != "" {
		status, err := json.Marshal(entry.PlanStatus)
		if err != nil {
			return nil, err
		}
		out.WriteString(`"plan_status":`)
		out.Write(status)
	}
	if entry.PlanChildren != nil {
		if entry.PlanStatus != "" {
			out.WriteByte(',')
		}
		children, err := json.Marshal(entry.PlanChildren)
		if err != nil {
			return nil, err
		}
		out.WriteString(`"plan_children":`)
		out.Write(children)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

type planIndex struct {
	byID   map[string]model.Plan
	bySeed map[string]model.Plan
}

func loadPlanIndex(dir string) (planIndex, error) {
	plans, err := store.ReadPlans(dir)
	if err != nil {
		return planIndex{}, err
	}
	index := planIndex{byID: map[string]model.Plan{}, bySeed: map[string]model.Plan{}}
	for _, plan := range plans {
		index.byID[plan.ID] = plan
		old, found := index.bySeed[plan.Seed]
		if !found || old.UpdatedAt < plan.UpdatedAt {
			index.bySeed[plan.Seed] = plan
		}
	}
	return index, nil
}

func (index planIndex) forIssue(issue model.Issue) (model.Plan, bool) {
	if issue.PlanID == "" {
		return model.Plan{}, false
	}
	plan, found := index.byID[issue.PlanID]
	return plan, found
}

func annotateIssue(issue model.Issue, plans planIndex) issueWithPlan {
	entry := issueWithPlan{Issue: issue}
	if plan, found := plans.forIssue(issue); found {
		entry.PlanStatus = plan.Status
		children := plan.Children
		if children == nil {
			children = []string{}
		}
		entry.PlanChildren = &children
	}
	return entry
}

func formatMode(options Options) (string, error) {
	mode := options.Value("format")
	if mode == "" && options.Has("json") {
		mode = "json"
	}
	if mode == "" {
		mode = "markdown"
	}
	for _, valid := range issueFormats {
		if mode == valid {
			return mode, nil
		}
	}
	return "", fmt.Errorf("Invalid --format value: %s. Valid: markdown|compact|plain|ids|json", mode)
}

func queryOptions(args []string, extraValues, extraSwitches []string) (Options, error) {
	values := append([]string{"type", "assignee", "label", "label-any", "priority", "priority-max", "limit", "sort", "format"}, extraValues...)
	switches := append([]string{"json", "unlabeled"}, extraSwitches...)
	return ParseOptions(args, values, switches)
}

func validateType(value string) error {
	if value == "" {
		return nil
	}
	for _, valid := range model.ValidTypes {
		if value == valid {
			return nil
		}
	}
	return fmt.Errorf("Invalid --type value: %s. Valid: task|bug|feature|epic", value)
}

func validateStatus(value string) error {
	if value == "" {
		return nil
	}
	for _, valid := range model.ValidStatuses {
		if value == valid {
			return nil
		}
	}
	return fmt.Errorf("Invalid --status value: %s. Valid: open|in_progress|closed", value)
}

func parseLimit(value string) (int, error) {
	if value == "" {
		return 50, nil
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("Invalid --limit %q: must be a non-negative integer", value)
		}
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("Invalid --limit %q: must be a non-negative integer", value)
	}
	return limit, nil
}

func parseFilterPriority(value string) (int, error) {
	token := strings.TrimSpace(value)
	digits := token
	if strings.HasPrefix(strings.ToUpper(digits), "P") {
		digits = digits[1:]
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 || n > 4 || strconv.Itoa(n) != digits {
		return 0, fmt.Errorf("Invalid priority %q: must be 0-4 or P0-P4", token)
	}
	return n, nil
}

func splitLabels(value string) []string {
	var labels []string
	for _, label := range strings.Split(value, ",") {
		if trimmed := strings.ToLower(strings.TrimSpace(label)); trimmed != "" {
			labels = append(labels, trimmed)
		}
	}
	return labels
}

func filterIssues(issues []model.Issue, opts Options) ([]model.Issue, error) {
	priority := map[int]bool{}
	if opts.Value("priority") != "" {
		for _, value := range strings.Split(opts.Value("priority"), ",") {
			if strings.TrimSpace(value) == "" {
				continue
			}
			n, err := parseFilterPriority(value)
			if err != nil {
				return nil, err
			}
			priority[n] = true
		}
	}
	priorityMax := -1
	if opts.Has("priority-max") {
		var err error
		priorityMax, err = parseFilterPriority(opts.Value("priority-max"))
		if err != nil {
			return nil, err
		}
	}
	required := splitLabels(opts.Value("label"))
	any := splitLabels(opts.Value("label-any"))
	filtered := make([]model.Issue, 0, len(issues))
	for _, issue := range issues {
		if value := opts.Value("type"); value != "" && issue.Type != value {
			continue
		}
		if value := opts.Value("assignee"); value != "" && issue.Assignee != value {
			continue
		}
		if opts.Has("unlabeled") && len(issue.Labels) > 0 {
			continue
		}
		labels := map[string]bool{}
		for _, label := range issue.Labels {
			labels[label] = true
		}
		matches := true
		for _, label := range required {
			if !labels[label] {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if len(any) > 0 {
			found := false
			for _, label := range any {
				if labels[label] {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if len(priority) > 0 && !priority[issue.Priority] {
			continue
		}
		if priorityMax >= 0 && issue.Priority > priorityMax {
			continue
		}
		filtered = append(filtered, issue)
	}
	return filtered, nil
}

func sortIssues(issues []model.Issue, mode string) error {
	valid := false
	for _, item := range issueSorts {
		if item == mode {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("Invalid --sort value: %s. Valid: priority|created|updated|id", mode)
	}
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		switch mode {
		case "priority":
			if a.Priority != b.Priority {
				return a.Priority < b.Priority
			}
			return a.CreatedAt > b.CreatedAt
		case "created":
			return a.CreatedAt > b.CreatedAt
		case "updated":
			return a.UpdatedAt > b.UpdatedAt
		default:
			return a.ID < b.ID
		}
	})
	return nil
}

func closedIDs(issues []model.Issue) map[string]bool {
	closed := map[string]bool{}
	for _, issue := range issues {
		if issue.Status == "closed" {
			closed[issue.ID] = true
		}
	}
	return closed
}

func effectivelyBlocked(issue model.Issue, closed map[string]bool) bool {
	for _, id := range issue.BlockedBy {
		if !closed[id] {
			return true
		}
	}
	return false
}

func planSuffix(issue model.Issue, plans planIndex) string {
	plan, found := plans.forIssue(issue)
	if !found {
		return ""
	}
	if plan.Status == "draft" {
		return " [plan in draft — run luuna-missions plan submit]"
	}
	return " [plan " + plan.Status + "]"
}

func compactIssue(issue model.Issue, closed map[string]bool) string {
	status := issue.Status
	if effectivelyBlocked(issue, closed) {
		status = "blocked"
	}
	priority := model.PriorityLabels[issue.Priority]
	if priority == "" {
		priority = strconv.Itoa(issue.Priority)
	}
	return fmt.Sprintf("%s %s %s %s", issue.ID, priority, status, issue.Title)
}

func oneLineIssue(issue model.Issue, closed map[string]bool) string {
	blocked := effectivelyBlocked(issue, closed)
	icon := "-"
	if issue.Status == "closed" {
		icon = "x"
	} else if issue.Status == "in_progress" {
		icon = ">"
	} else if blocked {
		icon = "!"
	}
	priority := model.PriorityLabels[issue.Priority]
	if priority == "" {
		priority = strconv.Itoa(issue.Priority)
	}
	line := fmt.Sprintf("%s %s · %s   [%s · %s]", icon, issue.ID, issue.Title, priority, issue.Type)
	if issue.Assignee != "" {
		line += " · @" + issue.Assignee
	}
	if blocked {
		line += " [blocked]"
	}
	if len(issue.Labels) > 0 {
		line += " {" + strings.Join(issue.Labels, ", ") + "}"
	}
	return line
}

func renderIssues(ctx *Context, command, mode string, issues []model.Issue, all []model.Issue, plans planIndex) error {
	closed := closedIDs(all)
	if mode == "json" {
		annotated := make([]issueWithPlan, 0, len(issues))
		for _, issue := range issues {
			annotated = append(annotated, annotateIssue(issue, plans))
		}
		return ctx.JSON(struct {
			Success bool            `json:"success"`
			Command string          `json:"command"`
			Issues  []issueWithPlan `json:"issues"`
			Count   int             `json:"count"`
		}{true, command, annotated, len(issues)})
	}
	for _, issue := range issues {
		switch mode {
		case "ids":
			if _, err := fmt.Fprintln(ctx.Stdout, issue.ID); err != nil {
				return err
			}
		case "compact":
			if _, err := fmt.Fprintln(ctx.Stdout, compactIssue(issue, closed)); err != nil {
				return err
			}
		default:
			if _, err := fmt.Fprintln(ctx.Stdout, oneLineIssue(issue, closed)+planSuffix(issue, plans)); err != nil {
				return err
			}
		}
	}
	if mode == "ids" || mode == "compact" {
		return nil
	}
	if len(issues) == 0 {
		if command == "ready" {
			_, err := fmt.Fprintln(ctx.Stdout, "No ready issues.")
			return err
		}
		_, err := fmt.Fprintln(ctx.Stdout, "No issues found.")
		return err
	}
	if command == "ready" {
		_, err := fmt.Fprintf(ctx.Stdout, "\n%d ready issue(s)\n", len(issues))
		return err
	}
	_, err := fmt.Fprintf(ctx.Stdout, "\n%d issue(s)\n", len(issues))
	return err
}

func runList(ctx *Context, args []string) error {
	opts, err := queryOptions(args, []string{"status"}, []string{"all"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'list'")
	}
	mode, err := formatMode(opts)
	if err != nil {
		return err
	}
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
	candidates := make([]model.Issue, 0, len(all))
	for _, issue := range all {
		if status := opts.Value("status"); status != "" && issue.Status != status {
			continue
		}
		if opts.Value("status") == "" && !opts.Has("all") && issue.Status == "closed" {
			continue
		}
		candidates = append(candidates, issue)
	}
	issues, err := filterIssues(candidates, opts)
	if err != nil {
		return err
	}
	modeSort := opts.Value("sort")
	if modeSort == "" {
		modeSort = "priority"
	}
	if err := sortIssues(issues, modeSort); err != nil {
		return err
	}
	if limit < len(issues) {
		issues = issues[:limit]
	}
	return renderIssues(ctx, "list", mode, issues, all, plans)
}

func scheduledOut(issue model.Issue, now time.Time) bool {
	if issue.Extensions == nil {
		return false
	}
	if queued, ok := issue.Extensions["queued"].(bool); ok && queued {
		return true
	}
	if value, ok := issue.Extensions["scheduledFor"].(string); ok {
		value = strings.TrimSpace(value)
		when, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			// JavaScript Date.parse interprets ISO date-only values as UTC.
			when, err = time.Parse(time.DateOnly, value)
		}
		if err == nil && when.After(now) {
			return true
		}
	}
	return false
}

func runReady(ctx *Context, args []string) error {
	opts, err := queryOptions(args, nil, []string{"respect-schedule"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'ready'")
	}
	mode, err := formatMode(opts)
	if err != nil {
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
	closed := closedIDs(all)
	candidates := []model.Issue{}
	for _, issue := range all {
		if issue.Status != "open" {
			continue
		}
		if issue.RequiresPlan {
			plan, found := plans.bySeed[issue.ID]
			if !found || plan.Status == "draft" {
				continue
			}
		} else if plan, found := plans.forIssue(issue); found && plan.Status == "draft" {
			candidates = append(candidates, issue)
			continue
		}
		if !effectivelyBlocked(issue, closed) {
			candidates = append(candidates, issue)
		}
	}
	issues, err := filterIssues(candidates, opts)
	if err != nil {
		return err
	}
	if opts.Has("respect-schedule") {
		filtered := make([]model.Issue, 0, len(issues))
		now := time.Now()
		for _, issue := range issues {
			if !scheduledOut(issue, now) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}
	modeSort := opts.Value("sort")
	if modeSort == "" {
		modeSort = "priority"
	}
	if err := sortIssues(issues, modeSort); err != nil {
		return err
	}
	if limit < len(issues) {
		issues = issues[:limit]
	}
	return renderIssues(ctx, "ready", mode, issues, all, plans)
}

func formatIssueFull(issue model.Issue) string {
	priority := model.PriorityLabels[issue.Priority]
	if priority == "" {
		priority = strconv.Itoa(issue.Priority)
	}
	lines := []string{issue.ID + "  " + issue.Status,
		"Title:    " + issue.Title,
		"Type:     " + issue.Type + "   Priority: " + priority}
	if issue.Assignee != "" {
		lines = append(lines, "Assignee: "+issue.Assignee)
	}
	if len(issue.Labels) > 0 {
		lines = append(lines, "Labels:   "+strings.Join(issue.Labels, ", "))
	}
	if issue.DiscoveredFrom != "" {
		lines = append(lines, "From:     "+issue.DiscoveredFrom)
	}
	if issue.Intent != nil {
		intent := fmt.Sprint(issue.Intent)
		if ids, ok := issue.Intent.([]any); ok {
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				parts = append(parts, fmt.Sprint(id))
			}
			intent = strings.Join(parts, ", ")
		} else if ids, ok := issue.Intent.([]string); ok {
			intent = strings.Join(ids, ", ")
		}
		lines = append(lines, "Intent:   "+intent)
	}
	if len(issue.Extensions) > 0 {
		keys := make([]string, 0, len(issue.Extensions))
		for key := range issue.Extensions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			var encoded bytes.Buffer
			encoder := json.NewEncoder(&encoded)
			encoder.SetEscapeHTML(false)
			if err := encoder.Encode(issue.Extensions[key]); err == nil {
				parts = append(parts, key+"="+strings.TrimSuffix(encoded.String(), "\n"))
			}
		}
		lines = append(lines, "Extensions: "+strings.Join(parts, " "))
	}
	if issue.Description != "" {
		lines = append(lines, "\n"+issue.Description)
	}
	if len(issue.BlockedBy) > 0 {
		lines = append(lines, "Blocked by: "+strings.Join(issue.BlockedBy, ", "))
	}
	if len(issue.Blocks) > 0 {
		lines = append(lines, "Blocks:     "+strings.Join(issue.Blocks, ", "))
	}
	if issue.Convoy != "" {
		lines = append(lines, "Convoy:   "+issue.Convoy)
	}
	if issue.CloseReason != "" {
		lines = append(lines, "Reason:   "+issue.CloseReason)
	}
	lines = append(lines, "Created:  "+issue.CreatedAt, "Updated:  "+issue.UpdatedAt)
	if issue.ClosedAt != "" {
		lines = append(lines, "Closed:   "+issue.ClosedAt)
	}
	return strings.Join(lines, "\n")
}

func summarizeChildren(plan model.Plan, lookup map[string]model.Issue) []map[string]any {
	children := make([]map[string]any, 0, len(plan.Children))
	adopted := map[string]bool{}
	for _, id := range plan.AdoptedChildren {
		adopted[id] = true
	}
	for _, id := range plan.Children {
		entry := map[string]any{"id": id, "title": "(missing)", "status": "missing", "adopted": adopted[id]}
		if child, ok := lookup[id]; ok {
			entry["title"], entry["status"] = child.Title, child.Status
		}
		children = append(children, entry)
	}
	return children
}

func summarizePlan(plan model.Plan) any {
	return struct {
		ID       string   `json:"id"`
		Status   string   `json:"status"`
		Revision int      `json:"revision"`
		Template string   `json:"template"`
		Children []string `json:"children"`
	}{plan.ID, plan.Status, plan.Revision, plan.Template, plan.Children}
}

func renderIssuePlanBlock(plan model.Plan, children []map[string]any) string {
	lines := []string{"", "Plan: " + plan.ID + "  [" + plan.Status + "]"}
	if plan.Status == "draft" {
		lines = append(lines, "plan in draft — run luuna-missions plan submit")
	} else if len(children) > 0 {
		lines = append(lines, fmt.Sprintf("Plan steps (%d):", len(children)))
		for _, child := range children {
			tag := ""
			if child["adopted"] == true {
				tag = " (adopted)"
			}
			lines = append(lines, fmt.Sprintf("  %s  [%s]  %s%s", child["id"], child["status"], child["title"], tag))
		}
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}

func formatIssueWithPlan(issue model.Issue, plans planIndex, lookup map[string]model.Issue) string {
	text := formatIssueFull(issue)
	if plan, found := plans.forIssue(issue); found {
		text += renderIssuePlanBlock(plan, summarizeChildren(plan, lookup))
	}
	return text
}

func runShow(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, []string{"format"}, []string{"json"})
	if err != nil {
		return err
	}
	mode, err := formatMode(opts)
	if err != nil {
		return err
	}
	if len(opts.Positionals) == 0 {
		return fmt.Errorf("Usage: luuna-missions show <id> [ids...]")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	plans, err := loadPlanIndex(dir)
	if err != nil {
		return err
	}
	lookup := map[string]model.Issue{}
	for _, issue := range issues {
		lookup[issue.ID] = issue
	}
	if len(opts.Positionals) == 1 {
		id := opts.Positionals[0]
		issue, found := lookup[id]
		if !found {
			if strings.HasPrefix(id, "pl-") {
				if mode != "markdown" && mode != "json" {
					ctx.ExitCode = 1
					_, err := fmt.Fprintf(ctx.Stderr, "✗ luuna-missions show %s: --format %s is not supported for plan ids; pass --json or use 'luuna-missions plan show %s'\n", id, mode, id)
					return err
				}
				planArgs := []string{"plan", "show", id}
				if mode == "json" {
					planArgs = append(planArgs, "--json")
				}
				ctx.ExitCode = ctx.App.Run(planArgs)
				return nil
			}
			return fmt.Errorf("Issue not found: %s", id)
		}
		switch mode {
		case "json":
			if plan, found := plans.forIssue(issue); found {
				children := summarizeChildren(plan, lookup)
				return ctx.JSON(struct {
					Success      bool        `json:"success"`
					Command      string      `json:"command"`
					Issue        model.Issue `json:"issue"`
					Plan         any         `json:"plan"`
					PlanChildren any         `json:"plan_children"`
				}{true, "show", issue, summarizePlan(plan), children})
			}
			return ctx.JSON(struct {
				Success bool        `json:"success"`
				Command string      `json:"command"`
				Issue   model.Issue `json:"issue"`
			}{true, "show", issue})
		case "ids":
			_, err := fmt.Fprintln(ctx.Stdout, issue.ID)
			return err
		case "compact":
			_, err := fmt.Fprintln(ctx.Stdout, compactIssue(issue, closedIDs(issues)))
			return err
		default:
			if _, err := fmt.Fprintln(ctx.Stdout, formatIssueFull(issue)); err != nil {
				return err
			}
			if plan, found := plans.forIssue(issue); found {
				_, err = fmt.Fprint(ctx.Stdout, renderIssuePlanBlock(plan, summarizeChildren(plan, lookup)))
			}
			return err
		}
	}
	// Multi-ID show keeps successful entries and reports every missing ID.
	found, failures := []model.Issue{}, []map[string]string{}
	for _, id := range opts.Positionals {
		if issue, ok := lookup[id]; ok {
			found = append(found, issue)
		} else {
			message := "Issue not found: " + id
			if strings.HasPrefix(id, "pl-") {
				message = fmt.Sprintf("Plan id %s not supported in multi-id 'luuna-missions show'; run 'luuna-missions plan show %s' instead", id, id)
			}
			failures = append(failures, map[string]string{"id": id, "error": message})
		}
	}
	if len(failures) > 0 {
		ctx.ExitCode = 1
	}
	if mode == "json" {
		results := make([]map[string]any, 0, len(found))
		for _, issue := range found {
			item := map[string]any{"issue": issue}
			if plan, ok := plans.forIssue(issue); ok {
				item["plan"] = summarizePlan(plan)
				item["plan_children"] = summarizeChildren(plan, lookup)
			}
			results = append(results, item)
		}
		payload := map[string]any{"success": len(failures) == 0, "command": "show", "issues": found, "results": results}
		if len(failures) > 0 {
			payload["errors"] = failures
		}
		return ctx.JSON(payload)
	}
	closed := closedIDs(issues)
	for i, issue := range found {
		if mode == "markdown" && i > 0 {
			fmt.Fprint(ctx.Stdout, "\n"+strings.Repeat("─", 60)+"\n\n")
		}
		line := formatIssueWithPlan(issue, plans, lookup)
		if mode == "ids" {
			line = issue.ID
		} else if mode == "compact" {
			line = compactIssue(issue, closed)
		}
		if _, err := fmt.Fprintln(ctx.Stdout, line); err != nil {
			return err
		}
		if mode == "plain" && i < len(found)-1 {
			fmt.Fprintln(ctx.Stdout)
		}
	}
	for _, missing := range failures {
		fmt.Fprintf(ctx.Stderr, "✗ %s: %s\n", missing["id"], missing["error"])
	}
	return nil
}
