package plan

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

type editSection struct {
	name string
	text string
}

type editStep struct {
	index     int
	title     *string
	priority  *int
	issueType *string
}

// Commander captures every non-option argument after --section. Keep the
// complete capture so the public two-argument validation also rejects an
// unquoted multiword value instead of silently joining it.
func extractEditSection(args []string) ([]string, []string, bool) {
	rest := make([]string, 0, len(args))
	var captured []string
	provided := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg != "--section" && !strings.HasPrefix(arg, "--section=") {
			rest = append(rest, arg)
			continue
		}
		provided = true
		if strings.HasPrefix(arg, "--section=") {
			captured = append(captured, strings.TrimPrefix(arg, "--section="))
		}
		for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			captured = append(captured, args[i])
		}
	}
	return rest, captured, provided
}

func parseEditSection(raw []string, provided bool) (*editSection, error) {
	if !provided {
		return nil, nil
	}
	if len(raw) < 2 {
		return nil, fmt.Errorf("--section requires two arguments: --section <name> <text> (quote the text).")
	}
	if len(raw) > 2 {
		return nil, fmt.Errorf("--section received more than two arguments. Quote the text: --section <name> \"<text>\".")
	}
	if strings.TrimSpace(raw[0]) == "" {
		return nil, fmt.Errorf("--section name must be a non-empty string.")
	}
	return &editSection{name: raw[0], text: raw[1]}, nil
}

var leadingInt = regexp.MustCompile(`^\s*[+-]?\d+`)

func parseEditPriority(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToUpper(value), "P") {
		value = value[1:]
	}
	match := leadingInt.FindString(value)
	n, err := strconv.Atoi(strings.TrimSpace(match))
	if match == "" || err != nil || n < 0 || n > 4 {
		return 0, fmt.Errorf("--priority must be 0-4 or P0-P4 (got: %s).", raw)
	}
	return n, nil
}

func parseEditStep(opts cli.Options) (*editStep, error) {
	provided := opts.Has("step")
	withTitle, withPriority, withType := opts.Has("title"), opts.Has("priority"), opts.Has("type")
	anyField := withTitle || withPriority || withType
	if !provided {
		if anyField {
			return nil, fmt.Errorf("--title/--priority/--type require --step <i> (the step index to edit).")
		}
		return nil, nil
	}
	raw := opts.Value("step")
	trimmed := strings.TrimSpace(raw)
	oneBased, err := strconv.Atoi(trimmed)
	if err != nil || oneBased < 1 || strconv.Itoa(oneBased) != trimmed {
		return nil, fmt.Errorf("--step must be a positive integer (got: %s).", raw)
	}
	if !anyField {
		return nil, fmt.Errorf("--step requires at least one of --title, --priority, --type.")
	}
	patch := &editStep{index: oneBased - 1}
	if withTitle {
		title := strings.TrimSpace(opts.Value("title"))
		if title == "" {
			return nil, fmt.Errorf("--title must be a non-empty string.")
		}
		patch.title = &title
	}
	if withPriority {
		priority, err := parseEditPriority(opts.Value("priority"))
		if err != nil {
			return nil, err
		}
		patch.priority = &priority
	}
	if withType {
		issueType := opts.Value("type")
		valid := false
		for _, value := range model.ValidTypes {
			if issueType == value {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("Invalid --type value: %s. Valid: %s", issueType, strings.Join(model.ValidTypes, "|"))
		}
		patch.issueType = &issueType
	}
	return patch, nil
}

func jsStringLength(value string) int { return len(utf16.Encode([]rune(value))) }

func editTemplateNames(templates map[string]model.PlanTemplate) string {
	// Built-ins are registered in this order in the TypeScript object literal.
	names := make([]string, 0, len(templates))
	seen := map[string]bool{}
	for _, name := range []string{"feature", "bug", "refactor"} {
		if _, ok := templates[name]; ok {
			names = append(names, name)
			seen[name] = true
		}
	}
	extra := []string{}
	for name := range templates {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return strings.Join(append(names, extra...), ", ")
}

func editSectionNames(template model.PlanTemplate) string {
	if len(template.SectionOrder) > 0 {
		return strings.Join(template.SectionOrder, ", ")
	}
	names := make([]string, 0, len(template.Sections))
	for name := range template.Sections {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func cloneSectionMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func runEdit(ctx *cli.Context, args []string) error {
	withoutSection, capturedSection, sectionProvided := extractEditSection(args)
	opts, err := parse(withoutSection, []string{"name", "step", "title", "priority", "type"}, nil)
	if err != nil {
		return err
	}
	idArg, err := needArg(opts, "edit <id> [options]")
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	planID, err := resolvePlanID(idArg, dir)
	if err != nil {
		return err
	}
	section, err := parseEditSection(capturedSection, sectionProvided)
	if err != nil {
		return err
	}
	stepPatch, err := parseEditStep(opts)
	if err != nil {
		return err
	}

	edited := []string{}
	if opts.Has("name") {
		edited = append(edited, "name")
	}
	if section != nil {
		edited = append(edited, "section:"+section.name)
	}
	if stepPatch != nil {
		prefix := fmt.Sprintf("step:%d:", stepPatch.index+1)
		if stepPatch.title != nil {
			edited = append(edited, prefix+"title")
		}
		if stepPatch.priority != nil {
			edited = append(edited, prefix+"priority")
		}
		if stepPatch.issueType != nil {
			edited = append(edited, prefix+"type")
		}
	}
	if len(edited) == 0 {
		return fmt.Errorf("No fields to edit. Pass at least one of: --name <text>, --section <name> <text>, --step <i> --title/--priority/--type.")
	}
	nextName := ""
	if opts.Has("name") {
		nextName = strings.TrimSpace(opts.Value("name"))
		if nextName == "" {
			return fmt.Errorf("--name must be a non-empty string.")
		}
	}

	var updated model.Plan
	approachChanged := false
	propagated := []string{}
	err = store.WithLock(store.PlansPath(dir), func() error {
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		index := planIndex(plans, planID)
		if index < 0 {
			return planNotFound(planID)
		}
		plan := plans[index]
		nextSections := plan.Sections
		if section != nil {
			templates, err := config.LoadPlanTemplates(dir)
			if err != nil {
				return err
			}
			template, ok := templates[plan.Template]
			if !ok {
				return fmt.Errorf("Plan %s references unknown template '%s'. Available: %s.", planID, plan.Template, editTemplateNames(templates))
			}
			spec, ok := template.Sections[section.name]
			if !ok {
				return fmt.Errorf("Unknown section '%s' for template '%s'. Known: %s.", section.name, plan.Template, editSectionNames(template))
			}
			kind, isText := spec.Kind.(string)
			if !isText || kind != "text" {
				if !isText {
					kind = "object"
				}
				return fmt.Errorf("--section editing supports kind=text only (V1). Section '%s' is kind=%s. Use 'luuna-missions plan submit --overwrite' for structural edits.", section.name, kind)
			}
			if spec.Required && strings.TrimSpace(section.text) == "" {
				return fmt.Errorf("Section '%s' is required and cannot be empty.", section.name)
			}
			minimum := 0
			if spec.MinLength != nil {
				minimum = *spec.MinLength
			}
			if minimum > 0 && jsStringLength(section.text) < minimum {
				return fmt.Errorf("Section '%s' must be at least %d characters (got %d).", section.name, minimum, jsStringLength(section.text))
			}
			prior, existed := plan.Sections[section.name].(string)
			nextSections = cloneSectionMap(plan.Sections)
			nextSections[section.name] = section.text
			approachChanged = section.name == "approach" && (!existed || prior != section.text)
		}
		if stepPatch != nil {
			rawSteps, ok := nextSections["steps"].([]any)
			if !ok {
				return fmt.Errorf("Plan %s has no steps section to edit. Use 'luuna-missions plan submit --overwrite' to add steps.", planID)
			}
			if stepPatch.index >= len(rawSteps) {
				plural := "s"
				if len(rawSteps) == 1 {
					plural = ""
				}
				return fmt.Errorf("--step %d is out of range (plan %s has %d step%s).", stepPatch.index+1, planID, len(rawSteps), plural)
			}
			existing, _ := rawSteps[stepPatch.index].(map[string]any)
			nextStep := cloneSectionMap(existing)
			if stepPatch.title != nil {
				nextStep["title"] = *stepPatch.title
			}
			if stepPatch.priority != nil {
				nextStep["priority"] = *stepPatch.priority
			}
			if stepPatch.issueType != nil {
				nextStep["type"] = *stepPatch.issueType
			}
			nextSteps := append([]any(nil), rawSteps...)
			nextSteps[stepPatch.index] = nextStep
			nextSections = cloneSectionMap(nextSections)
			nextSections["steps"] = nextSteps
		}
		now := model.NowISO()
		updated = plan
		updated.Sections = nextSections
		updated.Revision++
		updated.UpdatedAt = now
		if opts.Has("name") {
			updated.Name = nextName
		}
		plans[index] = updated
		if err := store.WritePlans(dir, plans); err != nil {
			return err
		}
		if !approachChanged && stepPatch == nil {
			return nil
		}
		return store.WithLock(store.IssuesPath(dir), func() error {
			issues, err := store.ReadIssues(dir)
			if err != nil {
				return err
			}
			parentIndex := issueIndex(issues, updated.Seed)
			childIDs := make(map[string]bool, len(updated.Children))
			for _, childID := range updated.Children {
				childIDs[childID] = true
			}
			dirty := false
			if approachChanged && parentIndex >= 0 {
				parent := issues[parentIndex]
				for _, childID := range updated.Children {
					childIndex := issueIndex(issues, childID)
					if childIndex < 0 {
						continue
					}
					child := issues[childIndex]
					child.Description = submitApplyBackref(child.Description, child.PlanStepIndex, updated.ID, parent.ID, parent.Title, updated.Template, updated.Sections["approach"])
					child.UpdatedAt = now
					issues[childIndex] = child
					dirty = true
				}
			}
			if stepPatch != nil {
				for i := range issues {
					child := issues[i]
					if !childIDs[child.ID] || child.PlanStepIndex == nil || *child.PlanStepIndex != stepPatch.index {
						continue
					}
					child.UpdatedAt = now
					if stepPatch.title != nil {
						child.Title = *stepPatch.title
					}
					if stepPatch.priority != nil {
						child.Priority = *stepPatch.priority
					}
					if stepPatch.issueType != nil {
						child.Type = *stepPatch.issueType
					}
					issues[i] = child
					propagated = append(propagated, child.ID)
					dirty = true
				}
			}
			if dirty {
				return store.WriteIssues(dir, issues)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	refreshed := 0
	if approachChanged {
		refreshed = len(updated.Children)
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success            bool     `json:"success"`
			Command            string   `json:"command"`
			PlanID             string   `json:"plan_id"`
			Revision           int      `json:"revision"`
			Edited             []string `json:"edited"`
			Name               string   `json:"name,omitempty"`
			BackrefsRefreshed  int      `json:"backrefs_refreshed"`
			PropagatedChildren []string `json:"propagated_children"`
		}{true, "plan edit", updated.ID, updated.Revision, edited, updated.Name, refreshed, propagated})
	}
	if err := ctx.Success(fmt.Sprintf("plan %s edited (%s); revision %d", updated.ID, strings.Join(edited, ", "), updated.Revision)); err != nil {
		return err
	}
	if approachChanged {
		if err := ctx.Success(fmt.Sprintf("refreshed backrefs on %d child seed(s)", len(updated.Children))); err != nil {
			return err
		}
	}
	if len(propagated) > 0 {
		return ctx.Success(fmt.Sprintf("propagated step metadata to %d child seed(s): %s", len(propagated), strings.Join(propagated, ", ")))
	}
	return nil
}
