package plan

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

func positiveFlag(value, name string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 1 || strconv.Itoa(n) != strings.TrimSpace(value) {
		return 0, fmt.Errorf("--%s must be a positive integer (got: %s).", name, value)
	}
	return n, nil
}
func appendUnique(items []string, id string) []string {
	for _, v := range items {
		if v == id {
			return items
		}
	}
	return append(items, id)
}
func removeValue(items []string, id string) []string {
	out := []string{}
	for _, v := range items {
		if v != id {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func containsString(items []string, id string) bool {
	for _, v := range items {
		if v == id {
			return true
		}
	}
	return false
}

func runAdopt(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"step", "at", "before", "after"}, nil)
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
	ids := opts.Positionals[1:]
	if dup := duplicates(ids); len(dup) > 0 {
		noun := ""
		if len(dup) > 1 {
			noun = "s"
		}
		return fmt.Errorf("Duplicate seed id%s in args: %s.", noun, strings.Join(dup, ", "))
	}
	var stepIndex *int
	if opts.Has("step") {
		n, err := positiveFlag(opts.Value("step"), "step")
		if err != nil {
			return err
		}
		stepIndex = model.IntPtr(n - 1)
	}
	positionFlags := []string{}
	for _, name := range []string{"at", "before", "after"} {
		if opts.Has(name) {
			positionFlags = append(positionFlags, "--"+name)
		}
	}
	if len(positionFlags) > 1 {
		return fmt.Errorf("--at, --before, and --after are mutually exclusive (got: %s).", strings.Join(positionFlags, ", "))
	}
	at := 0
	if opts.Has("at") {
		at, err = positiveFlag(opts.Value("at"), "at")
		if err != nil {
			return err
		}
	}
	if opts.Has("before") && strings.TrimSpace(opts.Value("before")) == "" {
		return fmt.Errorf("--before requires a seed id.")
	}
	if opts.Has("after") && strings.TrimSpace(opts.Value("after")) == "" {
		return fmt.Errorf("--after requires a seed id.")
	}
	var next model.Plan
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		p, err := lookupPlan(plans, planID)
		if err != nil {
			return err
		}
		steps, _ := p.Sections["steps"].([]any)
		if stepIndex != nil && (*stepIndex < 0 || *stepIndex >= len(steps)) {
			noun := "s"
			if len(steps) == 1 {
				noun = ""
			}
			return fmt.Errorf("--step %d is out of range (plan %s has %d step%s).", *stepIndex+1, planID, len(steps), noun)
		}
		parentIndex := -1
		byID := map[string]int{}
		for i, issue := range issues {
			byID[issue.ID] = i
			if issue.ID == p.Seed {
				parentIndex = i
			}
		}
		if parentIndex < 0 {
			return fmt.Errorf("Plan %s references parent seed %s which no longer exists.", planID, p.Seed)
		}
		parent := issues[parentIndex]
		for _, id := range ids {
			if id == p.Seed {
				return fmt.Errorf("cannot adopt the parent seed %s into its own plan %s.", id, planID)
			}
			index, ok := byID[id]
			if !ok {
				return fmt.Errorf("seed %s not found.", id)
			}
			seed := issues[index]
			if seed.Status == "closed" {
				return fmt.Errorf("seed %s is closed; only open or in-progress missions can be adopted.", id)
			}
			if seed.PlanID != "" {
				return fmt.Errorf("seed %s is already attached to plan %s; release it first.", id, seed.PlanID)
			}
		}
		insertion := len(p.Children)
		if at > 0 {
			insertion = at - 1
			if insertion > len(p.Children) {
				noun := "ren"
				if len(p.Children) == 1 {
					noun = ""
				}
				return fmt.Errorf("--at %d is out of range (plan %s has %d child%s; valid range 1..%d).", at, planID, len(p.Children), noun, len(p.Children)+1)
			}
		}
		if anchor := opts.Value("before"); opts.Has("before") {
			insertion = -1
			for i, id := range p.Children {
				if id == anchor {
					insertion = i
					break
				}
			}
			if insertion < 0 {
				return fmt.Errorf("--before %s is not a child of plan %s.", anchor, planID)
			}
		}
		if anchor := opts.Value("after"); opts.Has("after") {
			insertion = -1
			for i, id := range p.Children {
				if id == anchor {
					insertion = i + 1
					break
				}
			}
			if insertion < 0 {
				return fmt.Errorf("--after %s is not a child of plan %s.", anchor, planID)
			}
		}
		now := model.NowISO()
		approach := p.Sections["approach"]
		var stepLabels []any
		if stepIndex != nil {
			if step, ok := steps[*stepIndex].(map[string]any); ok {
				stepLabels, _ = step["labels"].([]any)
			}
		}
		for _, id := range ids {
			index := byID[id]
			seed := issues[index]
			seed.PlanID = planID
			seed.PlanStepIndex = stepIndex
			seed.Description = submitApplyBackref(seed.Description, stepIndex, planID, parent.ID, parent.Title, p.Template, approach)
			seed.Blocks = appendUnique(seed.Blocks, parent.ID)
			for _, raw := range stepLabels {
				if label, ok := raw.(string); ok {
					label = strings.ToLower(strings.TrimSpace(label))
					if label != "" {
						seed.Labels = appendUnique(seed.Labels, label)
					}
				}
			}
			seed.UpdatedAt = now
			issues[index] = seed
			parent.BlockedBy = appendUnique(parent.BlockedBy, id)
		}
		parent.UpdatedAt = now
		issues[parentIndex] = parent
		next = *p
		children := append([]string{}, p.Children[:insertion]...)
		children = append(children, ids...)
		children = append(children, p.Children[insertion:]...)
		next.Children = children
		adopted := append([]string{}, p.AdoptedChildren...)
		for _, id := range ids {
			adopted = appendUnique(adopted, id)
		}
		next.AdoptedChildren = adopted
		next.Revision++
		next.UpdatedAt = now
		for i := range plans {
			if plans[i].ID == planID {
				plans[i] = next
				break
			}
		}
		if err := store.WriteIssues(dir, issues); err != nil {
			return err
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
			Adopted  []string `json:"adopted"`
			Revision int      `json:"revision"`
		}{true, "plan adopt", planID, ids, next.Revision})
	}
	for _, id := range ids {
		if err := ctx.Success(id + " adopted into plan " + planID); err != nil {
			return err
		}
	}
	return ctx.Success(fmt.Sprintf("plan %s revision bumped to %d", planID, next.Revision))
}

func runRelease(ctx *cli.Context, args []string) error {
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
	ids := opts.Positionals[1:]
	if dup := duplicates(ids); len(dup) > 0 {
		noun := ""
		if len(dup) > 1 {
			noun = "s"
		}
		return fmt.Errorf("Duplicate seed id%s in args: %s.", noun, strings.Join(dup, ", "))
	}
	var next model.Plan
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		p, err := lookupPlan(plans, planID)
		if err != nil {
			return err
		}
		byID := map[string]int{}
		for i, issue := range issues {
			byID[issue.ID] = i
		}
		parentIndex, ok := byID[p.Seed]
		if !ok {
			return fmt.Errorf("Plan %s references parent seed %s which no longer exists.", planID, p.Seed)
		}
		parent := issues[parentIndex]
		for _, id := range ids {
			if id == p.Seed {
				return fmt.Errorf("cannot release the parent seed %s from its own plan %s.", id, planID)
			}
			index, ok := byID[id]
			if !ok {
				return fmt.Errorf("seed %s not found.", id)
			}
			seed := issues[index]
			if seed.PlanID != planID {
				if seed.PlanID != "" {
					return fmt.Errorf("seed %s is attached to plan %s, not %s.", id, seed.PlanID, planID)
				}
				return fmt.Errorf("seed %s is not attached to plan %s.", id, planID)
			}
		}
		now := model.NowISO()
		for _, id := range ids {
			index := byID[id]
			seed := issues[index]
			seed.PlanID = ""
			seed.PlanStepIndex = nil
			seed.Description = submitStripBackref(seed.Description)
			seed.Blocks = removeValue(seed.Blocks, parent.ID)
			seed.UpdatedAt = now
			issues[index] = seed
			parent.BlockedBy = removeValue(parent.BlockedBy, id)
		}
		parent.UpdatedAt = now
		issues[parentIndex] = parent
		next = *p
		children := append([]string{}, p.Children...)
		adopted := append([]string{}, p.AdoptedChildren...)
		for _, id := range ids {
			children = removeValue(children, id)
			adopted = removeValue(adopted, id)
		}
		if children == nil {
			children = []string{}
		}
		next.Children = children
		next.AdoptedChildren = adopted
		next.Revision++
		next.UpdatedAt = now
		for i := range plans {
			if plans[i].ID == planID {
				plans[i] = next
				break
			}
		}
		if err := store.WriteIssues(dir, issues); err != nil {
			return err
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
			Released []string `json:"released"`
			Revision int      `json:"revision"`
		}{true, "plan release", planID, ids, next.Revision})
	}
	for _, id := range ids {
		if err := ctx.Success(id + " released from plan " + planID); err != nil {
			return err
		}
	}
	return ctx.Success(fmt.Sprintf("plan %s revision bumped to %d", planID, next.Revision))
}
