package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

var intentPattern = regexp.MustCompile(`^r-[0-9a-f]{4,8}$`)

func runInit(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'init'")
	}
	if _, err := config.MigrateLegacyLayout(ctx.Cwd); err != nil {
		return err
	}
	dir := filepath.Join(ctx.Cwd, model.MissionsDir)
	_, statErr := os.Stat(filepath.Join(dir, model.ConfigFile))
	already := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if err := config.Init(ctx.Cwd); err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			Dir     string `json:"dir"`
		}{true, "init", dir})
	}
	if already {
		return ctx.Success("Already initialized: " + dir)
	}
	return ctx.Success("Initialized .luuna/missions/ in " + ctx.Cwd)
}

func parsePriority(raw string) (int, error) {
	if raw == "" {
		return 2, nil
	}
	digits := raw
	if strings.HasPrefix(strings.ToUpper(raw), "P") {
		digits = raw[1:]
	}
	if digits == "" {
		return 0, fmt.Errorf("--priority must be 0-4 or P0-P4")
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("--priority must be 0-4 or P0-P4")
		}
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 0 || n > 4 {
		return 0, fmt.Errorf("--priority must be 0-4 or P0-P4")
	}
	return n, nil
}

func parseIntent(values []string) (any, error) {
	ids, seen := []string{}, map[string]bool{}
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			id := strings.ToLower(strings.TrimSpace(token))
			if id == "" {
				continue
			}
			if !intentPattern.MatchString(id) {
				return nil, fmt.Errorf("Invalid --intent value: %s. Expected a roots idea id like r-a1b2", id)
			}
			if !seen[id] {
				ids, seen[id] = append(ids, id), true
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return ids, nil
}

func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func runCreate(ctx *Context, args []string) error {
	opts, err := ParseOptions(args,
		[]string{"title", "type", "priority", "assignee", "description", "desc", "body", "labels", "label", "from", "intent"},
		[]string{"json"},
	)
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'create'")
	}
	title := strings.TrimSpace(opts.Value("title"))
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	issueType := opts.Value("type")
	if issueType == "" {
		issueType = "task"
	}
	validType := false
	for _, candidate := range model.ValidTypes {
		if issueType == candidate {
			validType = true
			break
		}
	}
	if !validType {
		return fmt.Errorf("Invalid --type value: %s. Valid: task|bug|feature|epic", issueType)
	}
	priority, err := parsePriority(opts.Value("priority"))
	if err != nil {
		return err
	}
	labelsValue := opts.Value("labels")
	if !opts.Has("labels") {
		labelsValue = opts.Value("label")
	}
	var labels []string
	if labelsValue != "" {
		for _, label := range strings.Split(labelsValue, ",") {
			if normalized := strings.ToLower(strings.TrimSpace(label)); normalized != "" {
				labels = append(labels, normalized)
			}
		}
	}
	description := opts.Value("description")
	if !opts.Has("description") {
		description = opts.Value("desc")
	}
	if !opts.Has("description") && !opts.Has("desc") {
		description = opts.Value("body")
	}
	intent, err := parseIntent(opts.Repeated("intent"))
	if err != nil {
		return err
	}
	from := strings.TrimSpace(opts.Value("from"))
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	project, err := config.ReadConfig(dir)
	if err != nil {
		return err
	}
	createdID := ""
	err = store.WithLock(store.IssuesPath(dir), func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		ids := make(map[string]bool, len(issues))
		for _, issue := range issues {
			ids[issue.ID] = true
		}
		if from != "" && !ids[from] {
			return fmt.Errorf("--from issue not found: %s", from)
		}
		id, err := model.GenerateID(project.Project, ids)
		if err != nil {
			return err
		}
		now := timestamp()
		issue := model.Issue{ID: id, Title: title, Status: "open", Type: issueType,
			Priority: priority, CreatedAt: now, UpdatedAt: now,
			Assignee: opts.Value("assignee"), Description: description, Labels: labels,
			DiscoveredFrom: from, Intent: intent}
		if err := store.AppendIssue(dir, issue); err != nil {
			return err
		}
		createdID = id
		return nil
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			ID      string `json:"id"`
		}{true, "create", createdID})
	}
	return ctx.Success("Created " + createdID)
}
