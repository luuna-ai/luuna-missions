package cli

import (
	"fmt"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func runTpl(ctx *Context, args []string) error {
	path, err := nestedPath(args, 1)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return fmt.Errorf("Usage: luuna-missions tpl <create|step|list|show|pour|status>")
	}
	var valueFlags []string
	switch path[0] {
	case "create":
		valueFlags = []string{"name"}
	case "step":
		stepPath, err := nestedPath(args, 2)
		if err != nil {
			return err
		}
		if len(stepPath) > 1 && stepPath[1] == "add" {
			valueFlags = []string{"title", "type", "priority"}
		}
	case "pour":
		valueFlags = []string{"prefix"}
	}
	opts, err := ParseOptions(args, valueFlags, []string{"json"})
	if err != nil {
		return err
	}
	p := opts.Positionals
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	switch p[0] {
	case "create":
		name := opts.Value("name")
		if name == "" {
			return fmt.Errorf("--name is required")
		}
		id := ""
		err = store.WithLock(store.TemplatesPath(dir), func() error {
			templates, err := store.ReadTemplates(dir)
			if err != nil {
				return err
			}
			ids := map[string]bool{}
			for _, tpl := range templates {
				ids[tpl.ID] = true
			}
			id, err = model.GenerateID("tpl", ids)
			if err != nil {
				return err
			}
			return store.AppendTemplate(dir, model.Template{ID: id, Name: name, Steps: []model.TemplateStep{}})
		})
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				ID      string `json:"id"`
			}{true, "tpl create", id})
		}
		return ctx.Success("Created template " + id + ": " + name)
	case "step":
		if len(p) < 2 || p[1] != "add" {
			return fmt.Errorf("Usage: luuna-missions tpl step add <id> --title ...")
		}
		if len(p) < 3 {
			return fmt.Errorf("Template ID is required")
		}
		id := p[2]
		title := opts.Value("title")
		if title == "" {
			return fmt.Errorf("--title is required")
		}
		issueType := opts.Value("type")
		if issueType == "" {
			issueType = "task"
		}
		if err = validateType(issueType); err != nil {
			return err
		}
		priority, err := parsePriority(opts.Value("priority"))
		if err != nil {
			return err
		}
		count := 0
		err = store.WithLock(store.TemplatesPath(dir), func() error {
			templates, err := store.ReadTemplates(dir)
			if err != nil {
				return err
			}
			idx := -1
			for i := range templates {
				if templates[i].ID == id {
					idx = i
					break
				}
			}
			if idx < 0 {
				return fmt.Errorf("Template not found: %s", id)
			}
			templates[idx].Steps = append(templates[idx].Steps, model.TemplateStep{Title: title, Type: issueType, Priority: model.IntPtr(priority)})
			count = len(templates[idx].Steps)
			return store.WriteTemplates(dir, templates)
		})
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success   bool   `json:"success"`
				Command   string `json:"command"`
				ID        string `json:"id"`
				StepCount int    `json:"stepCount"`
			}{true, "tpl step add", id, count})
		}
		return ctx.Success(fmt.Sprintf("Added step %d to %s: %q", count, id, title))
	case "list":
		templates, err := store.ReadTemplates(dir)
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success   bool             `json:"success"`
				Command   string           `json:"command"`
				Templates []model.Template `json:"templates"`
				Count     int              `json:"count"`
			}{true, "tpl list", templates, len(templates)})
		}
		if len(templates) == 0 {
			_, err = fmt.Fprintln(ctx.Stdout, "No templates.")
			return err
		}
		for _, tpl := range templates {
			if _, err = fmt.Fprintf(ctx.Stdout, "%s  %s  (%d steps)\n", tpl.ID, tpl.Name, len(tpl.Steps)); err != nil {
				return err
			}
		}
		return nil
	case "show":
		if len(p) < 2 {
			return fmt.Errorf("Usage: luuna-missions tpl show <id>")
		}
		templates, err := store.ReadTemplates(dir)
		if err != nil {
			return err
		}
		var tpl *model.Template
		for i := range templates {
			if templates[i].ID == p[1] {
				tpl = &templates[i]
				break
			}
		}
		if tpl == nil {
			return fmt.Errorf("Template not found: %s", p[1])
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success  bool            `json:"success"`
				Command  string          `json:"command"`
				Template *model.Template `json:"template"`
			}{true, "tpl show", tpl})
		}
		if _, err = fmt.Fprintf(ctx.Stdout, "%s  %s\nSteps (%d):\n", tpl.ID, tpl.Name, len(tpl.Steps)); err != nil {
			return err
		}
		for i, step := range tpl.Steps {
			issueType := step.Type
			if issueType == "" {
				issueType = "task"
			}
			priority := 2
			if step.Priority != nil {
				priority = *step.Priority
			}
			if _, err = fmt.Fprintf(ctx.Stdout, "  %d. %s  [%s P%d]\n", i+1, step.Title, issueType, priority); err != nil {
				return err
			}
		}
		return nil
	case "pour":
		if len(p) < 2 {
			return fmt.Errorf("Usage: luuna-missions tpl pour <id> --prefix ...")
		}
		if !opts.Has("prefix") {
			return fmt.Errorf("--prefix is required")
		}
		id, prefix := p[1], opts.Value("prefix")
		templates, err := store.ReadTemplates(dir)
		if err != nil {
			return err
		}
		var tpl *model.Template
		for i := range templates {
			if templates[i].ID == id {
				tpl = &templates[i]
				break
			}
		}
		if tpl == nil {
			return fmt.Errorf("Template not found: %s", id)
		}
		if len(tpl.Steps) == 0 {
			return fmt.Errorf("Template %s has no steps", id)
		}
		project, err := config.ReadConfig(dir)
		if err != nil {
			return err
		}
		created := []string{}
		err = store.WithLock(store.IssuesPath(dir), func() error {
			issues, err := store.ReadIssues(dir)
			if err != nil {
				return err
			}
			ids := map[string]bool{}
			for _, issue := range issues {
				ids[issue.ID] = true
			}
			now := model.NowISO()
			fresh := make([]model.Issue, 0, len(tpl.Steps))
			for _, step := range tpl.Steps {
				newID, err := model.GenerateID(project.Project, ids)
				if err != nil {
					return err
				}
				ids[newID] = true
				priority := 2
				if step.Priority != nil {
					priority = *step.Priority
				}
				issueType := step.Type
				if issueType == "" {
					issueType = "task"
				}
				fresh = append(fresh, model.Issue{ID: newID, Title: strings.ReplaceAll(step.Title, "{prefix}", prefix), Status: "open", Type: issueType, Priority: priority, CreatedAt: now, UpdatedAt: now, Convoy: id})
				created = append(created, newID)
			}
			for i := 1; i < len(fresh); i++ {
				fresh[i].BlockedBy = []string{fresh[i-1].ID}
				fresh[i-1].Blocks = []string{fresh[i].ID}
			}
			return store.WriteIssues(dir, append(issues, fresh...))
		})
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool     `json:"success"`
				Command string   `json:"command"`
				IDs     []string `json:"ids"`
			}{true, "tpl pour", created})
		}
		if err := ctx.Success(fmt.Sprintf("Poured template %s — created %d issues", id, len(created))); err != nil {
			return err
		}
		for _, createdID := range created {
			if _, err = fmt.Fprintln(ctx.Stdout, "  "+createdID); err != nil {
				return err
			}
		}
		return nil
	case "status":
		if len(p) < 2 {
			return fmt.Errorf("Usage: luuna-missions tpl status <id>")
		}
		id := p[1]
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		convoy := []model.Issue{}
		for _, issue := range issues {
			if issue.Convoy == id {
				convoy = append(convoy, issue)
			}
		}
		if len(convoy) == 0 {
			if opts.Has("json") {
				return ctx.JSON(struct {
					Success    bool     `json:"success"`
					Command    string   `json:"command"`
					TemplateID string   `json:"templateId"`
					Total      int      `json:"total"`
					Issues     []string `json:"issues"`
				}{true, "tpl status", id, 0, []string{}})
			}
			_, err = fmt.Fprintf(ctx.Stdout, "No issues found for convoy %s\n", id)
			return err
		}
		closed := closedIDs(issues)
		completed, inProgress, blocked := 0, 0, 0
		ids := []string{}
		for _, issue := range convoy {
			ids = append(ids, issue.ID)
			if issue.Status == "closed" {
				completed++
			} else if issue.Status == "in_progress" {
				inProgress++
			}
			if issue.Status != "closed" && effectivelyBlocked(issue, closed) {
				blocked++
			}
		}
		status := struct {
			TemplateID string   `json:"templateId"`
			Total      int      `json:"total"`
			Completed  int      `json:"completed"`
			InProgress int      `json:"inProgress"`
			Blocked    int      `json:"blocked"`
			Issues     []string `json:"issues"`
		}{id, len(convoy), completed, inProgress, blocked, ids}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				Status  any    `json:"status"`
			}{true, "tpl status", status})
		}
		if _, err = fmt.Fprintf(ctx.Stdout, "Convoy: %s\n  Total:       %d\n  Completed:   %d\n  In progress: %d\n  Blocked:     %d\n  Issues:\n", id, len(convoy), completed, inProgress, blocked); err != nil {
			return err
		}
		for _, issue := range convoy {
			if _, err = fmt.Fprintln(ctx.Stdout, "    "+oneLineIssue(issue, closed)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("Unknown tpl subcommand: %s. Use create, step, list, show, pour, or status.", p[0])
	}
}
