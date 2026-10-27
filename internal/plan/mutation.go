package plan

import (
	"fmt"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func runCreate(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"name", "template"}, nil)
	if err != nil {
		return err
	}
	seedID, err := needArg(opts, "create <seed-id> [options]")
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	createdID, templateName, aborted := "", "", false
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		seedIndex := -1
		for i := range issues {
			if issues[i].ID == seedID {
				seedIndex = i
				break
			}
		}
		if seedIndex < 0 {
			return fmt.Errorf("Seed not found: %s", seedID)
		}
		seed := issues[seedIndex]
		templateName = opts.Value("template")
		if templateName == "" {
			templateName = config.DefaultTemplateForType(seed.Type)
		}
		if _, ok := templates[templateName]; !ok {
			return fmt.Errorf("Unknown template: %s. Available: %s", templateName, strings.Join(templateNames(templates), ", "))
		}
		for _, p := range plans {
			if p.Seed == seedID && p.Status != "draft" {
				fmt.Fprintf(ctx.Stderr, "✗ plan %s already exists for %s (status: %s, revision: %d)\n  Adopt missions into it with 'luuna-missions plan adopt %s <seed-ids...>'.\n", p.ID, seedID, p.Status, p.Revision, p.ID)
				ctx.ExitCode = 1
				aborted = true
				return nil
			}
		}
		ids := map[string]bool{}
		for _, p := range plans {
			ids[p.ID] = true
		}
		planID, err := model.GenerateID("pl", ids)
		if err != nil {
			return err
		}
		now := model.NowISO()
		name := strings.TrimSpace(opts.Value("name"))
		if name == "" {
			name = strings.TrimSpace(seed.Title)
		}
		next := model.Plan{ID: planID, Seed: seedID, Template: templateName, Name: name, Status: "approved", Revision: 1, Sections: map[string]any{"steps": []any{}}, Children: []string{}, CreatedAt: now, UpdatedAt: now}
		issues[seedIndex].PlanID = planID
		issues[seedIndex].UpdatedAt = now
		if err := store.WriteIssues(dir, issues); err != nil {
			return err
		}
		draftIndex := -1
		for i, p := range plans {
			if p.Seed == seedID && p.Status == "draft" {
				draftIndex = i
				break
			}
		}
		if draftIndex >= 0 {
			plans[draftIndex] = next
			err = store.WritePlans(dir, plans)
		} else {
			err = store.AppendPlan(dir, next)
		}
		if err != nil {
			return err
		}
		createdID = planID
		return nil
	})
	if err != nil {
		return err
	}
	if aborted {
		return nil
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success    bool     `json:"success"`
			Command    string   `json:"command"`
			PlanID     string   `json:"plan_id"`
			ParentSeed string   `json:"parent_seed"`
			Template   string   `json:"template"`
			Children   []string `json:"children"`
		}{true, "plan create", createdID, seedID, templateName, []string{}})
	}
	if err := ctx.Success(fmt.Sprintf("plan %s created (status: approved, adopt-only)", createdID)); err != nil {
		return err
	}
	_, err = fmt.Fprintf(ctx.Stderr, "\nNext:\n  luuna-missions plan adopt %s <seed-ids...>   # populate children in order\n  luuna-missions plan reorder %s <seed-ids...> # set the exact children order\n", createdID, createdID)
	return err
}

func runOutcome(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"result", "note"}, nil)
	if err != nil {
		return err
	}
	id, err := needArg(opts, "outcome <id> --result <value> [options]")
	if err != nil {
		return err
	}
	result := opts.Value("result")
	if result == "" {
		return fmt.Errorf("required option '--result <value>' not specified")
	}
	if result != "success" && result != "partial" && result != "failure" {
		return fmt.Errorf("Invalid --result value: %s. Valid: success|partial|failure", result)
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(id, dir)
	if err != nil {
		return err
	}
	var next model.Plan
	openChildren := 0
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		p, err := lookupPlan(plans, planID)
		if err != nil {
			return err
		}
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		for _, childID := range p.Children {
			for _, issue := range issues {
				if issue.ID == childID && issue.Status != "closed" {
					openChildren++
					break
				}
			}
		}
		next = *p
		next.Outcome = result
		next.UpdatedAt = model.NowISO()
		if opts.Has("note") {
			next.OutcomeNote = opts.Value("note")
		}
		for i := range plans {
			if plans[i].ID == planID {
				plans[i] = next
				break
			}
		}
		return store.WritePlans(dir, plans)
	})
	if err != nil {
		return err
	}
	if openChildren > 0 {
		noun := "child"
		if openChildren != 1 {
			noun = "children"
		}
		fmt.Fprintf(ctx.Stderr, "⚠ plan %s has %d open %s\n", next.ID, openChildren, noun)
	}
	if opts.Has("json") {
		var note *string
		if opts.Has("note") || next.OutcomeNote != "" {
			note = &next.OutcomeNote
		}
		return ctx.JSON(struct {
			Success      bool    `json:"success"`
			Command      string  `json:"command"`
			PlanID       string  `json:"plan_id"`
			Outcome      string  `json:"outcome"`
			OutcomeNote  *string `json:"outcomeNote,omitempty"`
			OpenChildren int     `json:"open_children"`
		}{true, "plan outcome", next.ID, next.Outcome, note, openChildren})
	}
	suffix := ""
	if next.OutcomeNote != "" {
		suffix = " — " + next.OutcomeNote
	}
	return ctx.Success("plan " + next.ID + " outcome recorded: " + next.Outcome + suffix)
}

func runReview(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"by"}, nil)
	if err != nil {
		return err
	}
	id, err := needArg(opts, "review <id> --by <name> [options]")
	if err != nil {
		return err
	}
	by := opts.Value("by")
	if by == "" {
		return fmt.Errorf("required option '--by <name>' not specified")
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(id, dir)
	if err != nil {
		return err
	}
	err = store.WithLock(store.PlansPath(dir), func() error {
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		p, err := lookupPlan(plans, planID)
		if err != nil {
			return err
		}
		p.ReviewedBy = by
		p.UpdatedAt = model.NowISO()
		return store.WritePlans(dir, plans)
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success    bool   `json:"success"`
			Command    string `json:"command"`
			PlanID     string `json:"plan_id"`
			ReviewedBy string `json:"reviewedBy"`
		}{true, "plan review", planID, by})
	}
	return ctx.Success("plan " + planID + " reviewed by " + by)
}

func runReorder(ctx *cli.Context, args []string) error {
	opts, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	if len(opts.Positionals) < 2 {
		return fmt.Errorf("At least one seed id is required.")
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(opts.Positionals[0], dir)
	if err != nil {
		return err
	}
	seedIDs := opts.Positionals[1:]
	if d := duplicates(seedIDs); len(d) > 0 {
		noun := ""
		if len(d) > 1 {
			noun = "s"
		}
		return fmt.Errorf("Duplicate seed id%s in args: %s.", noun, strings.Join(d, ", "))
	}
	var next model.Plan
	err = store.WithLock(store.PlansPath(dir), func() error {
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		p, err := lookupPlan(plans, planID)
		if err != nil {
			return err
		}
		current, provided := map[string]bool{}, map[string]bool{}
		for _, id := range p.Children {
			current[id] = true
		}
		for _, id := range seedIDs {
			provided[id] = true
		}
		missing, extra := []string{}, []string{}
		for _, id := range p.Children {
			if !provided[id] {
				missing = append(missing, id)
			}
		}
		for _, id := range seedIDs {
			if !current[id] {
				extra = append(extra, id)
			}
		}
		if len(extra) > 0 {
			verb, noun := "is", "a child"
			if len(extra) > 1 {
				verb, noun = "are", "children"
			}
			return fmt.Errorf("%s %s not %s of plan %s. Adopt first with 'luuna-missions plan adopt'.", strings.Join(extra, ", "), verb, noun, planID)
		}
		if len(missing) > 0 {
			return fmt.Errorf("reorder must list every child exactly once; missing: %s. Use 'luuna-missions plan release' to drop a child.", strings.Join(missing, ", "))
		}
		next = *p
		next.Children = append([]string{}, seedIDs...)
		next.Revision++
		next.UpdatedAt = model.NowISO()
		for i := range plans {
			if plans[i].ID == planID {
				plans[i] = next
				break
			}
		}
		return store.WritePlans(dir, plans)
	})
	if err != nil {
		return err
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success  bool     `json:"success"`
			Command  string   `json:"command"`
			PlanID   string   `json:"plan_id"`
			Children []string `json:"children"`
			Revision int      `json:"revision"`
		}{true, "plan reorder", planID, next.Children, next.Revision})
	}
	if err := ctx.Success(fmt.Sprintf("plan %s children reordered (revision %d)", planID, next.Revision)); err != nil {
		return err
	}
	return ctx.Success("order: " + strings.Join(next.Children, ", "))
}

func duplicates(ids []string) []string {
	seen, dupe := map[string]bool{}, map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if seen[id] && !dupe[id] {
			out = append(out, id)
			dupe[id] = true
		}
		seen[id] = true
	}
	return out
}
