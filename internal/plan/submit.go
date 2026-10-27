package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

type submittedStep struct {
	Title        string
	Type         string
	Priority     *int
	Blocks       []int
	PlanTemplate string
	ExistingSeed string
	Labels       []string
}

type adoption struct {
	id    string
	index int
}

type submitResult struct {
	planID     string
	children   []string
	revision   int
	obsolete   []model.Issue
	seed       model.Issue
	status     string
	reviewedBy string
}

func runSubmit(ctx *cli.Context, args []string) error {
	opts, err := parse(args, []string{"plan", "domain", "name"}, []string{"overwrite", "record-decision"})
	if err != nil {
		return err
	}
	seedID, err := needArg(opts, "submit [options] <seed-id>")
	if err != nil {
		return err
	}
	planFile := opts.Value("plan")
	if planFile == "" {
		return errors.New("required option '--plan <file>' not specified")
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	raw, err := readSubmittedPlan(ctx, planFile)
	if err != nil {
		return err
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("Invalid JSON in plan file: %w", err)
	}
	templateName := "feature"
	if obj, ok := parsed.(map[string]any); ok {
		if name, ok := obj["template"].(string); ok {
			templateName = name
		}
	}
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	template, ok := templates[templateName]
	if !ok {
		return fmt.Errorf("Unknown template in plan: %s. Available: %s", templateName, submitTemplateNames(templates))
	}
	if valid, diff := Validate(template, parsed); !valid {
		if err := jsonPretty(ctx.Stderr, diff); err != nil {
			return err
		}
		ctx.ExitCode = 1
		return nil
	}
	data := parsed.(map[string]any)
	sections := data["sections"].(map[string]any)
	sectionOrder := submitSectionKeys(raw)
	steps, err := parseSubmittedSteps(sections)
	if err != nil {
		return err
	}
	for i, step := range steps {
		if step.PlanTemplate != "" {
			if _, ok := templates[step.PlanTemplate]; !ok {
				label := step.Title
				if label == "" {
					label = "untitled"
				}
				fmt.Fprintf(ctx.Stderr, "step %d (%s): plan_template '%s' is not defined. Available: %s. Add it under plan_templates: in .luuna/missions/config.yaml.\n", i+1, label, step.PlanTemplate, submitTemplateNames(templates))
				ctx.ExitCode = 1
				return nil
			}
		}
	}
	projectConfig, err := config.ReadConfig(dir)
	if err != nil {
		return err
	}
	explicitName := normalizeSubmittedName(opts.Value("name"))
	if explicitName == "" {
		explicitName = normalizeSubmittedName(data["name"])
	}
	result := submitResult{revision: 1, status: "approved"}
	aborted := false
	err = store.WithLocks([]string{store.PlansPath(dir), store.IssuesPath(dir)}, func() error {
		issues, err := store.ReadIssues(dir)
		if err != nil {
			return err
		}
		plans, err := store.ReadPlans(dir)
		if err != nil {
			return err
		}
		seedIndex := issueIndex(issues, seedID)
		if seedIndex < 0 {
			return fmt.Errorf("Seed not found: %s", seedID)
		}
		seed := issues[seedIndex]
		result.seed = seed
		var existing *model.Plan
		for i := range plans {
			if plans[i].Seed == seedID && plans[i].Status != "draft" {
				existing = &plans[i]
				break
			}
		}
		if existing != nil && !opts.Has("overwrite") {
			fmt.Fprintf(ctx.Stderr, "✗ plan %s already exists for %s (status: %s, revision: %d)\n  Use --overwrite to replace it.\n", existing.ID, seedID, existing.Status, existing.Revision)
			ctx.ExitCode, aborted = 1, true
			return nil
		}
		now := model.NowISO()
		if existing != nil {
			result, err = overwriteSubmittedPlan(ctx, dir, seedIndex, &issues, &plans, *existing, steps, projectConfig.Project, templateName, sections, sectionOrder, explicitName, now)
			if err != nil {
				return err
			}
			if err := store.WriteIssues(dir, issues); err != nil {
				return err
			}
			return store.WritePlans(dir, plans)
		}
		result, err = createSubmittedPlan(ctx, dir, seedIndex, &issues, &plans, steps, projectConfig.Project, templateName, sections, sectionOrder, explicitName, now)
		return err
	})
	if err != nil {
		return err
	}
	if aborted {
		return nil
	}
	recordedAtlasID := ""
	if opts.Has("record-decision") {
		recordedAtlasID = recordSubmittedDecision(ctx, dir, result.seed, result.planID, sections["approach"], opts.Value("domain"))
	}
	for _, issue := range result.obsolete {
		fmt.Fprintf(ctx.Stderr, "luuna-missions close %s --reason \"obsoleted by plan %s revision %d\"\n", issue.ID, result.planID, result.revision)
	}
	if opts.Has("json") {
		obsolete := make([]string, 0, len(result.obsolete))
		for _, issue := range result.obsolete {
			obsolete = append(obsolete, issue.ID)
		}
		return ctx.JSON(struct {
			Success     bool     `json:"success"`
			Command     string   `json:"command"`
			PlanID      string   `json:"plan_id"`
			Children    []string `json:"children"`
			ParentSeed  string   `json:"parent_seed"`
			Revision    int      `json:"revision"`
			Obsolete    []string `json:"obsolete"`
			Overwritten bool     `json:"overwritten"`
		}{true, "plan submit", result.planID, result.children, seedID, result.revision, obsolete, result.revision > 1})
	}
	if result.revision > 1 {
		if err := ctx.Success(fmt.Sprintf("plan %s overwritten (revision %d, status: approved)", result.planID, result.revision)); err != nil {
			return err
		}
	} else {
		if err := ctx.Success(fmt.Sprintf("plan %s created (status: approved)", result.planID)); err != nil {
			return err
		}
	}
	noun := "child seeds"
	if len(result.children) == 1 {
		noun = "child seed"
	}
	if err := ctx.Success(fmt.Sprintf("%d %s: %s", len(result.children), noun, strings.Join(result.children, ", "))); err != nil {
		return err
	}
	if len(result.obsolete) > 0 {
		noun = "obsolete child seeds"
		if len(result.obsolete) == 1 {
			noun = "obsolete child seed"
		}
		if err := ctx.Success(fmt.Sprintf("%d %s flagged (see stderr for close suggestions)", len(result.obsolete), noun)); err != nil {
			return err
		}
	}
	if err := ctx.Success(fmt.Sprintf("%s now blocked by %d children", seedID, len(result.children))); err != nil {
		return err
	}
	if recordedAtlasID != "" {
		if err := ctx.Success("recorded luuna-atlas decision " + recordedAtlasID); err != nil {
			return err
		}
	}
	writeSubmitNextHints(ctx, result.planID, result.reviewedBy == "" && (result.status == "approved" || result.status == "active"))
	return nil
}

func readSubmittedPlan(ctx *cli.Context, planFile string) ([]byte, error) {
	if planFile == "-" {
		return io.ReadAll(ctx.Stdin)
	}
	path := planFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(ctx.Cwd, path)
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("Plan file not found: %s", planFile)
	}
	return content, err
}

func submitSectionKeys(raw []byte) []string {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	sectionJSON := envelope["sections"]
	decoder := json.NewDecoder(bytes.NewReader(sectionJSON))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	order := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil
		}
		order = append(order, key)
	}
	return order
}

func submitTemplateNames(templates map[string]model.PlanTemplate) string {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func parseSubmittedSteps(sections map[string]any) ([]submittedStep, error) {
	raw, ok := sections["steps"].([]any)
	if !ok {
		return nil, errors.New("Validated plan has no steps array")
	}
	steps := make([]submittedStep, 0, len(raw))
	for _, value := range raw {
		m, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Validated plan contains a non-object step")
		}
		step := submittedStep{}
		step.Title, _ = m["title"].(string)
		step.Type, _ = m["type"].(string)
		step.PlanTemplate, _ = m["plan_template"].(string)
		step.ExistingSeed, _ = m["existing_seed"].(string)
		if value, ok := m["priority"].(float64); ok {
			n := int(value)
			step.Priority = &n
		}
		if values, ok := m["blocks"].([]any); ok {
			for _, value := range values {
				step.Blocks = append(step.Blocks, int(value.(float64)))
			}
		}
		if values, ok := m["labels"].([]any); ok {
			for _, value := range values {
				step.Labels = append(step.Labels, value.(string))
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func normalizeSubmittedName(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func issueIndex(issues []model.Issue, id string) int {
	for i := range issues {
		if issues[i].ID == id {
			return i
		}
	}
	return -1
}

func planIndex(plans []model.Plan, id string) int {
	for i := range plans {
		if plans[i].ID == id {
			return i
		}
	}
	return -1
}

func submitIDs(issues []model.Issue, plans []model.Plan) (map[string]bool, map[string]bool) {
	issueIDs, planIDs := map[string]bool{}, map[string]bool{}
	for _, issue := range issues {
		issueIDs[issue.ID] = true
	}
	for _, plan := range plans {
		planIDs[plan.ID] = true
	}
	return issueIDs, planIDs
}

func uniqueAppend(list []string, value string) ([]string, bool) {
	for _, item := range list {
		if item == value {
			return list, false
		}
	}
	return append(list, value), true
}

func normalizeStepLabels(input []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range input {
		label := strings.ToLower(strings.TrimSpace(raw))
		if label != "" && !seen[label] {
			out, seen[label] = append(out, label), true
		}
	}
	return out
}

func mergeStepLabels(existing, stepLabels []string) []string {
	merged := append([]string(nil), existing...)
	for _, label := range normalizeStepLabels(stepLabels) {
		merged, _ = uniqueAppend(merged, label)
	}
	return merged
}

func validateSubmitAdoptions(ctx *cli.Context, steps []submittedStep, parentID string, issues []model.Issue, allowedPlan string) (map[int]adoption, error) {
	out, seen := map[int]adoption{}, map[string]bool{}
	for i, step := range steps {
		id := step.ExistingSeed
		if id == "" {
			continue
		}
		label := fmt.Sprintf("step %d (%s)", i+1, step.Title)
		if step.Title == "" {
			label = fmt.Sprintf("step %d (adopt %s)", i+1, id)
		}
		if step.PlanTemplate != "" {
			return nil, fmt.Errorf("%s: existing_seed and plan_template are mutually exclusive — adoption replaces spawning, so a sub-plan template cannot apply.", label)
		}
		if id == parentID {
			return nil, fmt.Errorf("%s: cannot adopt the parent seed %s into its own plan.", label, parentID)
		}
		if seen[id] {
			return nil, fmt.Errorf("%s: existing_seed %s is already adopted by an earlier step in this plan.", label, id)
		}
		index := issueIndex(issues, id)
		if index < 0 {
			return nil, fmt.Errorf("%s: existing_seed %s not found.", label, id)
		}
		seed := issues[index]
		if seed.Status == "closed" {
			return nil, fmt.Errorf("%s: existing_seed %s is closed; only open or in-progress missions can be adopted.", label, id)
		}
		if seed.PlanID != "" && seed.PlanID != allowedPlan {
			return nil, fmt.Errorf("%s: existing_seed %s is already attached to plan %s.", label, id, seed.PlanID)
		}
		if step.Title != "" && step.Title != seed.Title {
			fmt.Fprintf(ctx.Stderr, "⚠ step %d: existing_seed %s title \"%s\" differs from step.title \"%s\"; seed title is preserved.\n", i+1, id, seed.Title, step.Title)
		}
		seen[id], out[i] = true, adoption{id, index}
	}
	return out, nil
}

func createSubmittedPlan(ctx *cli.Context, dir string, seedIndex int, issues *[]model.Issue, plans *[]model.Plan, steps []submittedStep, projectName, templateName string, sections map[string]any, sectionOrder []string, explicitName, now string) (submitResult, error) {
	seed := (*issues)[seedIndex]
	adoptions, err := validateSubmitAdoptions(ctx, steps, seed.ID, *issues, "")
	if err != nil {
		return submitResult{}, err
	}
	issueIDs, planIDs := submitIDs(*issues, *plans)
	planID, err := model.GenerateID("pl", planIDs)
	if err != nil {
		return submitResult{}, err
	}
	children := make([]string, len(steps))
	for i := range steps {
		if adopted, ok := adoptions[i]; ok {
			children[i] = adopted.id
			continue
		}
		id, err := model.GenerateID(projectName, issueIDs)
		if err != nil {
			return submitResult{}, err
		}
		children[i], issueIDs[id] = id, true
	}
	newIssues := make([]model.Issue, 0, len(steps)-len(adoptions))
	for i, step := range steps {
		index := i
		if adopted, ok := adoptions[i]; ok {
			matched := &(*issues)[adopted.index]
			matched.PlanID = planID
			matched.PlanStepIndex = &index
			matched.Description = submitApplyBackref(matched.Description, &index, planID, seed.ID, seed.Title, templateName, sections["approach"])
			matched.Labels = mergeStepLabels(matched.Labels, step.Labels)
			matched.UpdatedAt = now
			continue
		}
		issue := spawnSubmittedChild(step, children[i], i, planID, seed, templateName, sections["approach"], now)
		newIssues = append(newIssues, issue)
	}
	addChildEdge := func(index int, field, id string) {
		if adopted, ok := adoptions[index]; ok {
			issue := &(*issues)[adopted.index]
			if field == "blocks" {
				issue.Blocks, _ = uniqueAppend(issue.Blocks, id)
			} else {
				issue.BlockedBy, _ = uniqueAppend(issue.BlockedBy, id)
			}
			issue.UpdatedAt = now
			return
		}
		for i := range newIssues {
			if newIssues[i].ID != children[index] {
				continue
			}
			if field == "blocks" {
				newIssues[i].Blocks, _ = uniqueAppend(newIssues[i].Blocks, id)
			} else {
				newIssues[i].BlockedBy, _ = uniqueAppend(newIssues[i].BlockedBy, id)
			}
			return
		}
	}
	for i, step := range steps {
		for _, targetIndex := range step.Blocks {
			addChildEdge(i, "blocks", children[targetIndex-1])
			addChildEdge(targetIndex-1, "blockedBy", children[i])
		}
	}
	for i := range steps {
		addChildEdge(i, "blocks", seed.ID)
	}
	for _, id := range children {
		seed.BlockedBy, _ = uniqueAppend(seed.BlockedBy, id)
	}
	seed.PlanID, seed.UpdatedAt = planID, now
	(*issues)[seedIndex] = seed
	name := explicitName
	if name == "" {
		name = normalizeSubmittedName(seed.Title)
	}
	plan := model.Plan{ID: planID, Seed: seed.ID, Template: templateName, Name: name, Status: "approved", Revision: 1, Sections: sections, Children: children, CreatedAt: now, UpdatedAt: now}
	plan.SetSectionOrder(sectionOrder)
	for i := range steps {
		if adopted, ok := adoptions[i]; ok {
			plan.AdoptedChildren = append(plan.AdoptedChildren, adopted.id)
		}
	}
	*issues = append(*issues, newIssues...)
	if err := store.WriteIssues(dir, *issues); err != nil {
		return submitResult{}, err
	}
	draftIndex := -1
	for i := range *plans {
		if (*plans)[i].Seed == seed.ID && (*plans)[i].Status == "draft" {
			draftIndex = i
			break
		}
	}
	if draftIndex >= 0 {
		(*plans)[draftIndex] = plan
		err = store.WritePlans(dir, *plans)
	} else {
		err = store.AppendPlan(dir, plan)
	}
	if err != nil {
		return submitResult{}, err
	}
	return submitResult{planID: planID, children: children, revision: 1, seed: seed, status: "approved"}, nil
}

func spawnSubmittedChild(step submittedStep, id string, index int, planID string, parent model.Issue, templateName string, approach any, now string) model.Issue {
	issueType := step.Type
	if issueType == "" {
		issueType = "task"
	}
	priority := 2
	if step.Priority != nil {
		priority = *step.Priority
	}
	issue := model.Issue{
		ID: id, Title: step.Title, Status: "open", Type: issueType,
		Priority: priority, PlanStepIndex: model.IntPtr(index),
		Description: submitBuildBackref(model.IntPtr(index), planID, parent.ID, parent.Title, templateName, approach),
		CreatedAt:   now, UpdatedAt: now,
	}
	issue.Labels = normalizeStepLabels(step.Labels)
	if step.PlanTemplate != "" {
		issue.RequiresPlan = true
	} else {
		issue.PlanID = planID
	}
	return issue
}

func overwriteSubmittedPlan(ctx *cli.Context, dir string, seedIndex int, issues *[]model.Issue, plans *[]model.Plan, existing model.Plan, steps []submittedStep, projectName, templateName string, sections map[string]any, sectionOrder []string, explicitName, now string) (submitResult, error) {
	_ = dir
	seed := (*issues)[seedIndex]
	adoptions, err := validateSubmitAdoptions(ctx, steps, seed.ID, *issues, existing.ID)
	if err != nil {
		return submitResult{}, err
	}
	oldChildren, oldIDSet := make([]model.Issue, 0, len(existing.Children)), map[string]bool{}
	for _, id := range existing.Children {
		if index := issueIndex(*issues, id); index >= 0 {
			oldChildren = append(oldChildren, (*issues)[index])
			oldIDSet[id] = true
		}
	}
	usedOld, externalAdopted := map[string]bool{}, map[string]bool{}
	issueIDs, _ := submitIDs(*issues, *plans)
	children := make([]string, len(steps))
	for i, step := range steps {
		if adopted, ok := adoptions[i]; ok {
			children[i] = adopted.id
			if oldIDSet[adopted.id] {
				usedOld[adopted.id] = true
			} else {
				externalAdopted[adopted.id] = true
			}
			continue
		}
		for _, old := range oldChildren {
			if !usedOld[old.ID] && old.Title == step.Title {
				children[i], usedOld[old.ID] = old.ID, true
				break
			}
		}
		if children[i] == "" {
			id, err := model.GenerateID(projectName, issueIDs)
			if err != nil {
				return submitResult{}, err
			}
			children[i], issueIDs[id] = id, true
		}
	}
	newIssues := []model.Issue{}
	for i, step := range steps {
		id := children[i]
		if usedOld[id] {
			index := issueIndex(*issues, id)
			if index >= 0 {
				matched := &(*issues)[index]
				matched.Description = submitApplyBackref(matched.Description, model.IntPtr(i), existing.ID, seed.ID, seed.Title, templateName, sections["approach"])
				matched.Labels = mergeStepLabels(matched.Labels, step.Labels)
				matched.UpdatedAt = now
			}
			continue
		}
		if externalAdopted[id] {
			index := issueIndex(*issues, id)
			if index >= 0 {
				matched := &(*issues)[index]
				matched.PlanID, matched.PlanStepIndex = existing.ID, model.IntPtr(i)
				matched.Description = submitApplyBackref(matched.Description, model.IntPtr(i), existing.ID, seed.ID, seed.Title, templateName, sections["approach"])
				matched.Labels = mergeStepLabels(matched.Labels, step.Labels)
				matched.UpdatedAt = now
			}
			continue
		}
		issue := spawnSubmittedChild(step, id, i, existing.ID, seed, templateName, sections["approach"], now)
		issue.Blocks = []string{seed.ID}
		newIssues = append(newIssues, issue)
	}
	addEdge := func(id, field, value string) {
		for i := range newIssues {
			if newIssues[i].ID == id {
				if field == "blocks" {
					newIssues[i].Blocks, _ = uniqueAppend(newIssues[i].Blocks, value)
				} else {
					newIssues[i].BlockedBy, _ = uniqueAppend(newIssues[i].BlockedBy, value)
				}
				return
			}
		}
		index := issueIndex(*issues, id)
		if index < 0 {
			return
		}
		matched := &(*issues)[index]
		var changed bool
		if field == "blocks" {
			matched.Blocks, changed = uniqueAppend(matched.Blocks, value)
		} else {
			matched.BlockedBy, changed = uniqueAppend(matched.BlockedBy, value)
		}
		if changed {
			matched.UpdatedAt = now
		}
	}
	for i, step := range steps {
		for _, target := range step.Blocks {
			addEdge(children[i], "blocks", children[target-1])
			addEdge(children[target-1], "blockedBy", children[i])
		}
	}
	for id := range externalAdopted {
		addEdge(id, "blocks", seed.ID)
	}
	obsolete := []model.Issue{}
	for _, old := range oldChildren {
		if !usedOld[old.ID] {
			obsolete = append(obsolete, old)
		}
	}
	finalSet := map[string]bool{}
	for _, id := range children {
		finalSet[id] = true
	}
	blockedBy := []string{}
	for _, id := range seed.BlockedBy {
		if !oldIDSet[id] && !finalSet[id] {
			blockedBy = append(blockedBy, id)
		}
	}
	seed.BlockedBy = append(blockedBy, children...)
	seed.PlanID, seed.UpdatedAt = existing.ID, now
	(*issues)[seedIndex] = seed
	updated := existing
	updated.Template, updated.Sections, updated.Children = templateName, sections, children
	updated.SetSectionOrder(sectionOrder)
	updated.Revision, updated.UpdatedAt = existing.Revision+1, now
	if explicitName != "" {
		updated.Name = explicitName
	} else if updated.Name == "" {
		updated.Name = normalizeSubmittedName(seed.Title)
	}
	updated.AdoptedChildren = nil
	for _, id := range existing.AdoptedChildren {
		if finalSet[id] {
			updated.AdoptedChildren, _ = uniqueAppend(updated.AdoptedChildren, id)
		}
	}
	// Map iteration would scramble order; append new adoptions in step order.
	for _, id := range children {
		if externalAdopted[id] {
			updated.AdoptedChildren, _ = uniqueAppend(updated.AdoptedChildren, id)
		}
	}
	if index := planIndex(*plans, existing.ID); index >= 0 {
		(*plans)[index] = updated
	}
	*issues = append(*issues, newIssues...)
	return submitResult{planID: existing.ID, children: children, revision: updated.Revision, obsolete: obsolete, seed: seed, status: existing.Status, reviewedBy: existing.ReviewedBy}, nil
}

const (
	submitBackrefStart = "<!-- missions:plan-backref:start -->"
	submitBackrefEnd   = "<!-- missions:plan-backref:end -->"
)

func submitBuildBackref(stepIndex *int, planID, parentID, parentTitle, templateName string, approach any) string {
	lines := []string{}
	if stepIndex == nil {
		lines = append(lines, "Adopted into plan "+planID+".")
	} else {
		lines = append(lines, fmt.Sprintf("Step %d of plan %s.", *stepIndex+1, planID))
	}
	lines = append(lines, "", "Parent seed: "+parentID+" — "+parentTitle, "Plan template: "+templateName)
	if text, ok := approach.(string); ok {
		collapsed := strings.Join(strings.Fields(text), " ")
		if collapsed != "" {
			if utf8.RuneCountInString(collapsed) > 240 {
				chars := []rune(collapsed)
				cut := string(chars[:240])
				if lastSpace := strings.LastIndex(cut, " "); lastSpace > 120 {
					cut = cut[:lastSpace]
				}
				collapsed = strings.TrimRightFunc(cut, unicode.IsSpace) + "…"
			}
			lines = append(lines, "Plan approach: "+collapsed)
		}
	}
	lines = append(lines, "", "Run `luuna-missions plan show "+planID+"` for the full plan (context, alternatives, sibling steps, acceptance criteria).")
	return submitBackrefStart + "\n" + strings.Join(lines, "\n") + "\n" + submitBackrefEnd
}

// submitApplyBackref and submitStripBackref are shared with the plan link
// commands. Their marker boundaries preserve manual text around the block.
func submitApplyBackref(existing string, stepIndex *int, planID, parentID, parentTitle, templateName string, approach any) string {
	block := submitBuildBackref(stepIndex, planID, parentID, parentTitle, templateName, approach)
	prior := config.UpgradeLegacyMarkers(existing)
	start, end := strings.Index(prior, submitBackrefStart), strings.Index(prior, submitBackrefEnd)
	if start >= 0 && end >= 0 && end >= start {
		return prior[:start] + block + prior[end+len(submitBackrefEnd):]
	}
	if strings.TrimSpace(prior) == "" {
		return block
	}
	return block + "\n\n" + prior
}

func submitStripBackref(existing string) string {
	prior := config.UpgradeLegacyMarkers(existing)
	start, end := strings.Index(prior, submitBackrefStart), strings.Index(prior, submitBackrefEnd)
	if start < 0 || end < start {
		return prior
	}
	before := strings.TrimRightFunc(prior[:start], unicode.IsSpace)
	after := strings.TrimLeftFunc(prior[end+len(submitBackrefEnd):], unicode.IsSpace)
	if before == "" {
		return after
	}
	if after == "" {
		return before
	}
	return before + "\n\n" + after
}

func writeSubmitNextHints(ctx *cli.Context, planID string, reviewable bool) {
	fmt.Fprintln(ctx.Stderr)
	fmt.Fprintln(ctx.Stderr, "Next:")
	fmt.Fprintf(ctx.Stderr, "  luuna-missions plan show %s          # review the plan as a unit\n", planID)
	fmt.Fprintln(ctx.Stderr, "  luuna-missions ready                      # pick up the first child step")
	if reviewable {
		fmt.Fprintf(ctx.Stderr, "  luuna-missions plan review %s --by <name>   # record approval (optional)\n", planID)
	}
}

func recordSubmittedDecision(ctx *cli.Context, dir string, seed model.Issue, planID string, approach any, forcedDomain string) string {
	atlas, err := exec.LookPath("luuna-atlas")
	if err != nil {
		fmt.Fprintln(ctx.Stderr, "⚠ --record-decision: luuna-atlas not found on PATH; skipping")
		return ""
	}
	projectRoot := config.ProjectRoot(dir)
	domain := forcedDomain
	if domain == "" {
		domain = inferSubmittedDomain(atlas, projectRoot, seed)
	}
	if domain == "" {
		fmt.Fprintln(ctx.Stderr, "⚠ --record-decision: no luuna-atlas domain inferred (skipping)")
		return ""
	}
	text, _ := approach.(string)
	command := exec.Command(atlas, "record", domain, "--type", "decision", "--title", seed.Title, "--rationale", text, "--evidence-missions", planID, "--json")
	command.Dir = projectRoot
	output, err := command.Output()
	if err != nil {
		detail := ""
		if exit, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exit.Stderr))
			if stderr != "" {
				detail = ": " + strings.Split(stderr, "\n")[0]
			}
		}
		fmt.Fprintln(ctx.Stderr, "⚠ --record-decision: luuna-atlas record failed"+detail)
		return ""
	}
	var response struct {
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
	}
	if json.Unmarshal(output, &response) == nil {
		return response.Record.ID
	}
	return ""
}

func inferSubmittedDomain(atlas, projectRoot string, seed model.Issue) string {
	command := exec.Command(atlas, "--json", "status")
	command.Dir = projectRoot
	output, err := command.Output()
	if err != nil {
		return ""
	}
	var response struct {
		Domains []struct {
			Domain string `json:"domain"`
		} `json:"domains"`
	}
	if json.Unmarshal(output, &response) != nil {
		return ""
	}
	set := map[string]bool{}
	for _, domain := range response.Domains {
		set[domain.Domain] = true
	}
	for _, label := range seed.Labels {
		if set[label] {
			return label
		}
	}
	for _, token := range strings.Fields(seed.Description) {
		token = strings.TrimRight(token, ".,;:)]")
		if !strings.Contains(token, "/") || !strings.Contains(filepath.Base(token), ".") {
			continue
		}
		for _, segment := range strings.Split(token, "/") {
			if set[segment] {
				return segment
			}
		}
	}
	return ""
}
