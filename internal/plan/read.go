package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

const instructions = "Fill every section. Required fields are marked. Use prior_art entries to ground decisions. Reply with JSON shaped { \"template\": \"<name>\", \"name\": \"<short label>\", \"sections\": { \"<section-name>\": <value>, ... } } — drop the plan_request wrapper, and sections in your reply is an object keyed by name (not the array of section metadata above). The top-level `name` field is an optional short human-readable label (e.g. \"Schema-driven config editor\"); if you omit it, luuna-missions plan submit derives one from the parent seed title. Each step is shaped { title?, type?, priority?, blocks?: number[], labels?: string[], plan_template?, existing_seed? }. In each step, `blocks` lists 1-based step indices that this step blocks (step 1 is the first step, step N is the last); e.g. step 1 with `blocks: [2]` means step 1 must finish before step 2 starts. Leave empty if nothing depends on it. Optional `labels` is an array of non-empty strings applied to the spawned (or adopted) child seed; values are normalized (lowercased, trimmed, deduped) and merged additively on adoption — they never clobber existing labels."

type promptSection struct {
	Name      string `json:"name"`
	Required  bool   `json:"required"`
	Kind      any    `json:"kind"`
	Prompt    string `json:"prompt"`
	PriorArt  []any  `json:"prior_art"`
	MinLength *int   `json:"min_length,omitempty"`
	Min       *int   `json:"min,omitempty"`
	Item      any    `json:"item,omitempty"`
}

func runPrompt(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"template", "domain"}, nil)
	if err != nil {
		return err
	}
	seedID, err := needArg(opts, "prompt <seed-id> [options]")
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	var seed *model.Issue
	for i := range issues {
		if issues[i].ID == seedID {
			seed = &issues[i]
			break
		}
	}
	if seed == nil {
		return fmt.Errorf("Seed not found: %s", seedID)
	}
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	templateName := opts.Value("template")
	if templateName == "" {
		inherited, err := inheritedTemplate(*seed, dir)
		if err != nil {
			return err
		}
		templateName = inherited
		if templateName == "" {
			templateName = config.DefaultTemplateForType(seed.Type)
		}
	}
	template, ok := templates[templateName]
	if !ok {
		return fmt.Errorf("Unknown template: %s. Available: %s", templateName, strings.Join(templateNames(templates), ", "))
	}
	sections := make([]promptSection, 0, len(template.Sections))
	for _, name := range sectionNames(template) {
		spec := template.Sections[name]
		sections = append(sections, promptSection{Name: name, Required: spec.Required, Kind: spec.Kind, Prompt: spec.Prompt, PriorArt: []any{}, MinLength: spec.MinLength, Min: spec.Min, Item: spec.Item})
	}
	domain := inferAtlasDomain(*seed, opts.Value("domain"), ctx.Cwd)
	for i := range sections {
		sections[i].PriorArt = priorArtForSection(domain, sections[i].Name, template.Sections[sections[i].Name].AtlasSource, ctx.Cwd)
	}
	minSteps, minAcceptance := 0, 0
	if spec, ok := template.Sections["steps"]; ok && spec.Min != nil {
		minSteps = *spec.Min
	}
	if spec, ok := template.Sections["acceptance"]; ok && spec.Min != nil {
		minAcceptance = *spec.Min
	}
	request := struct {
		Seed         string          `json:"seed"`
		Template     string          `json:"template"`
		Instructions string          `json:"instructions"`
		Sections     []promptSection `json:"sections"`
		Validation   struct {
			AllRequiredPresent bool `json:"all_required_present"`
			MinSteps           int  `json:"min_steps"`
			MinAcceptance      int  `json:"min_acceptance"`
		} `json:"validation"`
	}{Seed: seedID, Template: templateName, Instructions: instructions, Sections: sections}
	request.Validation.AllRequiredPresent = true
	request.Validation.MinSteps = minSteps
	request.Validation.MinAcceptance = minAcceptance
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success     bool   `json:"success"`
			Command     string `json:"command"`
			PlanRequest any    `json:"plan_request"`
		}{true, "plan prompt", request})
	}
	fmt.Fprintf(ctx.Stdout, "Plan prompt for %s\nTemplate: %s\nSeed title: %s\n\n%s\n\n", seedID, templateName, seed.Title, instructions)
	for _, s := range sections {
		tag := "optional"
		if s.Required {
			tag = "required"
		}
		kind, ok := s.Kind.(string)
		if !ok {
			kind = "object"
		}
		fmt.Fprintf(ctx.Stdout, "  %s (%s) %s\n    %s\n", s.Name, kind, tag, s.Prompt)
		if s.MinLength != nil {
			fmt.Fprintf(ctx.Stdout, "    min_length: %d\n", *s.MinLength)
		}
		if s.Min != nil {
			fmt.Fprintf(ctx.Stdout, "    min entries: %d\n", *s.Min)
		}
	}
	_, err = fmt.Fprintf(ctx.Stdout, "\nPipe --json into a file the LLM fills, then run:\n  luuna-missions plan submit %s --plan <file>\n", seedID)
	return err
}

func templateNames(templates map[string]model.PlanTemplate) []string {
	// Object.keys of the built-ins precedes custom entries in the TypeScript implementation.
	names := []string{}
	for _, name := range []string{"feature", "bug", "refactor"} {
		if _, ok := templates[name]; ok {
			names = append(names, name)
		}
	}
	custom := []string{}
	for name := range templates {
		if name != "feature" && name != "bug" && name != "refactor" {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	names = append(names, custom...)
	return names
}

func runList(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"seed", "status", "outcome", "template"}, nil)
	if err != nil {
		return err
	}
	status, outcome := opts.Value("status"), opts.Value("outcome")
	if status != "" && status != "draft" && status != "approved" && status != "active" && status != "done" {
		return fmt.Errorf("Invalid --status value: %s. Valid: draft|approved|active|done", status)
	}
	if outcome != "" && outcome != "success" && outcome != "partial" && outcome != "failure" {
		return fmt.Errorf("Invalid --outcome value: %s. Valid: success|partial|failure", outcome)
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	plans, err := store.ReadPlans(dir)
	if err != nil {
		return err
	}
	filtered := []model.Plan{}
	for _, p := range plans {
		if seed := opts.Value("seed"); seed != "" && p.Seed != seed {
			continue
		}
		if status != "" && p.Status != status {
			continue
		}
		if outcome != "" && p.Outcome != outcome {
			continue
		}
		if template := opts.Value("template"); template != "" && p.Template != template {
			continue
		}
		filtered = append(filtered, p)
	}
	// stable newest-first; ties retain JSONL order.
	sortPlansNewest(filtered)
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool         `json:"success"`
			Command string       `json:"command"`
			Plans   []model.Plan `json:"plans"`
			Count   int          `json:"count"`
		}{true, "plan list", filtered, len(filtered)})
	}
	if len(filtered) == 0 {
		_, err = fmt.Fprintln(ctx.Stdout, "No plans match.")
		return err
	}
	for _, p := range filtered {
		name := "(unnamed)"
		if p.Name != "" {
			name = p.Name
		}
		if len(utf16.Encode([]rune(name))) > 40 {
			name = string([]rune(name)[:39]) + "…"
		}
		pad := 40 - len(utf16.Encode([]rune(name)))
		if pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		outcomeText := ""
		if p.Outcome != "" {
			outcomeText = " (" + p.Outcome + ")"
		}
		fmt.Fprintf(ctx.Stdout, "%s  %s  rev %d  %s  %s  seed=%s  children=%d%s  %s\n", p.ID, p.Status, p.Revision, name, p.Template, p.Seed, len(p.Children), outcomeText, p.CreatedAt)
	}
	return nil
}

func runShow(ctx *cli.Context, args []string) error {
	opts, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	id, err := needArg(opts, "show <id> [options]")
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(id, dir)
	if err != nil {
		return err
	}
	settings, err := config.ReadConfig(dir)
	if err != nil {
		return err
	}
	plans, err := store.ReadPlans(dir)
	if err != nil {
		return err
	}
	plan, err := lookupPlan(plans, planID)
	if err != nil {
		return err
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return err
	}
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	bySeed := map[string]model.Plan{}
	for _, p := range plans {
		if old, ok := bySeed[p.Seed]; !ok || old.UpdatedAt < p.UpdatedAt {
			bySeed[p.Seed] = p
		}
	}
	maxDepth := config.MaxPlanDepth(settings)
	tree := buildTree(*plan, issues, bySeed, 1, maxDepth)
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success       bool       `json:"success"`
			Command       string     `json:"command"`
			Plan          model.Plan `json:"plan"`
			Children      []any      `json:"children"`
			ChildrenPlans []any      `json:"children_plans"`
		}{true, "plan show", *plan, tree.Children, tree.ChildrenPlans})
	}
	fmt.Fprintf(ctx.Stdout, "%s  %s  rev %d\n", plan.ID, plan.Status, plan.Revision)
	if plan.Name != "" {
		fmt.Fprintf(ctx.Stdout, "Name:     %s\n", plan.Name)
	}
	fmt.Fprintf(ctx.Stdout, "Seed:     %s\nTemplate: %s\nCreated:  %s\nUpdated:  %s\n", plan.Seed, plan.Template, plan.CreatedAt, plan.UpdatedAt)
	if plan.Outcome != "" {
		note := ""
		if plan.OutcomeNote != "" {
			note = " — " + plan.OutcomeNote
		}
		fmt.Fprintf(ctx.Stdout, "Outcome:  %s%s\n", plan.Outcome, note)
	}
	if plan.ReviewedBy == "" && (plan.Status == "approved" || plan.Status == "active") {
		fmt.Fprintln(ctx.Stdout, "Review suggested (no reviewer recorded yet)")
	}
	if plan.ReviewedBy != "" {
		fmt.Fprintf(ctx.Stdout, "Reviewed: %s\n", plan.ReviewedBy)
	}
	fmt.Fprintln(ctx.Stdout, "\nSections:")
	renderSections(ctx, *plan, templates[plan.Template])
	fmt.Fprintf(ctx.Stdout, "\nChildren (%d):\n", len(tree.Children))
	if len(tree.Children) == 0 {
		fmt.Fprintln(ctx.Stdout, "  (none)")
	} else {
		for _, child := range tree.Children {
			renderChild(ctx, child, "  ")
		}
	}
	for _, nested := range tree.ChildrenPlans {
		fmt.Fprintln(ctx.Stdout)
		renderNested(ctx, nested, "  ")
	}
	return nil
}

type planTree struct {
	Plan          model.Plan `json:"plan"`
	Children      []any      `json:"children"`
	ChildrenPlans []any      `json:"children_plans"`
}

func buildTree(plan model.Plan, issues []model.Issue, bySeed map[string]model.Plan, depth, maxDepth int) planTree {
	children := childSummaries(plan, issues)
	nested := []any{}
	for _, id := range plan.Children {
		sub, ok := bySeed[id]
		if !ok {
			continue
		}
		if depth+1 > maxDepth {
			nested = append(nested, struct {
				PlanID    string `json:"plan_id"`
				Truncated bool   `json:"truncated"`
				Hint      string `json:"hint"`
			}{sub.ID, true, "depth limit reached — use `luuna-missions plan show " + sub.ID + "` to drill in"})
		} else {
			nested = append(nested, buildTree(sub, issues, bySeed, depth+1, maxDepth))
		}
	}
	return planTree{plan, children, nested}
}

func renderChild(ctx *cli.Context, value any, indent string) {
	data, _ := json.Marshal(value)
	var item struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Status  string `json:"status"`
		Adopted bool   `json:"adopted"`
	}
	_ = json.Unmarshal(data, &item)
	tag := ""
	if item.Adopted {
		tag = " (adopted)"
	}
	fmt.Fprintf(ctx.Stdout, "%s%s  [%s]  %s%s\n", indent, item.ID, item.Status, item.Title, tag)
}
func renderNested(ctx *cli.Context, value any, indent string) {
	if truncated, ok := value.(struct {
		PlanID    string `json:"plan_id"`
		Truncated bool   `json:"truncated"`
		Hint      string `json:"hint"`
	}); ok {
		fmt.Fprintf(ctx.Stdout, "%s%s\n", indent, truncated.Hint)
		return
	}
	tree, ok := value.(planTree)
	if !ok {
		return
	}
	name := ""
	if tree.Plan.Name != "" {
		name = "  " + tree.Plan.Name
	}
	fmt.Fprintf(ctx.Stdout, "%sSub-plan: %s%s  [%s]  rev %d  seed=%s\n", indent, tree.Plan.ID, name, tree.Plan.Status, tree.Plan.Revision, tree.Plan.Seed)
	fmt.Fprintf(ctx.Stdout, "%sChildren (%d):\n", indent, len(tree.Children))
	childIndent := indent + "  "
	if len(tree.Children) == 0 {
		fmt.Fprintf(ctx.Stdout, "%s(none)\n", childIndent)
	} else {
		for _, child := range tree.Children {
			renderChild(ctx, child, childIndent)
		}
	}
	for _, sub := range tree.ChildrenPlans {
		fmt.Fprintln(ctx.Stdout)
		renderNested(ctx, sub, childIndent)
	}
}

func renderSections(ctx *cli.Context, plan model.Plan, template model.PlanTemplate) {
	names := []string{}
	known := map[string]bool{}
	for _, name := range []string{"context", "approach", "alternatives", "steps", "risks", "acceptance"} {
		known[name] = true
		if _, ok := plan.Sections[name]; ok {
			names = append(names, name)
		}
	}
	others := []string{}
	for _, name := range plan.SectionKeys() {
		if !known[name] {
			others = append(others, name)
		}
	}
	names = append(names, others...)
	for _, name := range names {
		value := plan.Sections[name]
		fmt.Fprintf(ctx.Stdout, "  %s\n", name)
		if value == nil {
			fmt.Fprintln(ctx.Stdout, "    (empty)")
			continue
		}
		if s, ok := value.(string); ok {
			for _, line := range strings.Split(s, "\n") {
				fmt.Fprintf(ctx.Stdout, "    %s\n", line)
			}
			continue
		}
		if entries, ok := value.([]any); ok {
			if len(entries) == 0 {
				fmt.Fprintln(ctx.Stdout, "    (none)")
				continue
			}
			for i, entry := range entries {
				renderSectionEntry(ctx, i+1, entry, template.Sections[name])
			}
			continue
		}
		fmt.Fprintf(ctx.Stdout, "    %s\n", compactJSON(value))
	}
}
func renderSectionEntry(ctx *cli.Context, number int, entry any, spec model.SectionSpec) {
	marker := fmt.Sprintf("    %d.", number)
	if s, ok := entry.(string); ok {
		fmt.Fprintf(ctx.Stdout, "%s %s\n", marker, s)
		return
	}
	data, ok := entry.(map[string]any)
	if !ok {
		fmt.Fprintf(ctx.Stdout, "%s %s\n", marker, compactJSON(entry))
		return
	}
	if kind, ok := spec.Kind.(string); ok && kind == "steps" {
		title, _ := data["title"].(string)
		if title == "" {
			if adopt, ok := data["existing_seed"].(string); ok {
				title = "(adopt " + adopt + ")"
			} else {
				title = compactJSON(entry)
			}
		}
		fmt.Fprintf(ctx.Stdout, "%s %s\n", marker, title)
		indent := strings.Repeat(" ", len(marker)+1)
		if blocks, ok := data["blocks"].([]any); ok && len(blocks) > 0 {
			values := []string{}
			for _, v := range blocks {
				values = append(values, fmt.Sprint(v))
			}
			fmt.Fprintf(ctx.Stdout, "%sblocks: %s\n", indent, strings.Join(values, ", "))
		}
		for _, field := range []string{"requires_plan", "plan_template", "type", "priority", "labels", "existing_seed"} {
			value, exists := data[field]
			if !exists {
				continue
			}
			if field == "existing_seed" && data["title"] == nil {
				continue
			}
			if field == "requires_plan" && value != true {
				continue
			}
			if text, ok := value.(string); ok && text == "" {
				continue
			}
			rendered := fmt.Sprint(value)
			if field == "labels" {
				if list, ok := value.([]any); ok {
					entries := []string{}
					for _, v := range list {
						entries = append(entries, fmt.Sprint(v))
					}
					rendered = strings.Join(entries, ", ")
				}
				if rendered == "" {
					continue
				}
			}
			fmt.Fprintf(ctx.Stdout, "%s%s: %s\n", indent, field, rendered)
		}
		return
	}
	fmt.Fprintf(ctx.Stdout, "%s %s\n", marker, compactJSON(entry))
}
func compactJSON(v any) string { data, _ := json.Marshal(v); return string(data) }

func sortPlansNewest(plans []model.Plan) {
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].CreatedAt > plans[j].CreatedAt })
}

func runValidate(ctx *cli.Context, args []string) error {
	opts, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	id, err := needArg(opts, "validate <id> [options]")
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(id, dir)
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
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	template, ok := templates[p.Template]
	if !ok {
		return fmt.Errorf("Plan %s references unknown template '%s'. Available: %s.", planID, p.Template, strings.Join(templateNames(templates), ", "))
	}
	valid, diff := Validate(template, map[string]any{"template": p.Template, "sections": p.Sections})
	if valid {
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				Valid   bool   `json:"valid"`
				PlanID  string `json:"plan_id"`
			}{true, "plan validate", true, planID})
		}
		return ctx.Success("plan " + planID + " valid")
	}
	if err := jsonPretty(ctx.Stderr, diff); err != nil {
		return err
	}
	ctx.ExitCode = 1
	return nil
}
