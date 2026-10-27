package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
	"gopkg.in/yaml.v3"
)

func section(required bool, kind any, prompt string) model.SectionSpec {
	return model.SectionSpec{Required: required, Kind: kind, Prompt: prompt}
}

func BuiltInTemplates() map[string]model.PlanTemplate {
	featureContext := section(true, "text", "Why does this work need to happen? What problem or opportunity drives it?")
	featureContext.MinLength = model.IntPtr(50)
	featureSteps := section(true, "steps", "Decompose into ordered, independent implementation steps. Each becomes a child seed.")
	featureSteps.Min = model.IntPtr(2)
	featureAlternatives := section(false, "list", "What other approaches were considered and rejected?")
	featureAlternatives.Item = map[string]model.SectionSpec{
		"name":             section(true, "text", ""),
		"rejected_because": section(true, "text", ""),
	}
	featureRisks := section(false, "list", "What could go wrong? Known failure modes from prior work are pre-filled when luuna-atlas is available.")
	featureRisks.Item = "text"
	featureRisks.AtlasSource = "failure"
	featureAcceptance := section(true, "list", "Concrete, verifiable conditions for plan completion.")
	featureAcceptance.Item = "text"
	featureAcceptance.Min = model.IntPtr(1)
	feature := model.PlanTemplate{
		Name:         "feature",
		Description:  "New capability or significant change. Default for type: feature.",
		SectionOrder: []string{"context", "approach", "alternatives", "steps", "risks", "acceptance"},
		Sections: map[string]model.SectionSpec{
			"context":      featureContext,
			"approach":     section(true, "text", "What's the chosen approach, and why this over alternatives?"),
			"alternatives": featureAlternatives,
			"steps":        featureSteps,
			"risks":        featureRisks,
			"acceptance":   featureAcceptance,
		},
	}

	bugReproduction := section(true, "text", "Concrete steps to reproduce. Inputs, environment, observed vs. expected.")
	bugReproduction.MinLength = model.IntPtr(50)
	bugRootCause := section(true, "text", "What's actually broken? Trace the defect to its source, not just the symptom.")
	bugRootCause.MinLength = model.IntPtr(50)
	bugSteps := section(true, "steps", "Ordered fix steps. Each becomes a child seed.")
	bugSteps.Min = model.IntPtr(1)
	bugAcceptance := section(true, "list", "Verifiable conditions: regression test, behavior, etc.")
	bugAcceptance.Item = "text"
	bugAcceptance.Min = model.IntPtr(1)
	bug := model.PlanTemplate{
		Name:         "bug",
		Description:  "Defect fix. Adds reproduction and root_cause sections. Default for type: bug.",
		SectionOrder: []string{"context", "reproduction", "root_cause", "approach", "steps", "acceptance"},
		Sections: map[string]model.SectionSpec{
			"context":      section(true, "text", "Why does fixing this matter? Who is affected and how?"),
			"reproduction": bugReproduction,
			"root_cause":   bugRootCause,
			"approach":     section(true, "text", "Chosen fix and the rationale for it over alternatives."),
			"steps":        bugSteps,
			"acceptance":   bugAcceptance,
		},
	}

	refactorInvariant := section(true, "text", "The contract that MUST remain equal across the refactor. Be specific — this is what acceptance tests verify.")
	refactorInvariant.MinLength = model.IntPtr(50)
	refactorSteps := section(true, "steps", "Ordered restructuring steps. Each becomes a child seed.")
	refactorSteps.Min = model.IntPtr(1)
	refactorAcceptance := section(true, "list", "How we'll verify the invariant is preserved.")
	refactorAcceptance.Item = "text"
	refactorAcceptance.Min = model.IntPtr(1)
	refactor := model.PlanTemplate{
		Name:         "refactor",
		Description:  "Internal restructuring. Adds behavior_invariant (must stay equal). Opt-in via --template refactor.",
		SectionOrder: []string{"context", "behavior_invariant", "approach", "steps", "acceptance"},
		Sections: map[string]model.SectionSpec{
			"context":            section(true, "text", "Why this refactor? What pain does it relieve?"),
			"behavior_invariant": refactorInvariant,
			"approach":           section(true, "text", "Chosen restructuring strategy."),
			"steps":              refactorSteps,
			"acceptance":         refactorAcceptance,
		},
	}
	return map[string]model.PlanTemplate{"feature": feature, "bug": bug, "refactor": refactor}
}

func DefaultTemplateForType(issueType string) string {
	if issueType == "bug" {
		return "bug"
	}
	return "feature"
}

func LoadPlanTemplates(missionsDir string) (map[string]model.PlanTemplate, error) {
	result := BuiltInTemplates()
	content, err := os.ReadFile(filepath.Join(missionsDir, model.ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	orders := customTemplateSectionOrder(content)
	data = UpgradeLegacyConfigKeys(data).(map[string]any)
	userBlock, ok := data["plan_templates"].(map[string]any)
	if !ok {
		return result, nil
	}
	for name, raw := range userBlock {
		templateRaw, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plan_templates.%s must be a mapping", name)
		}
		sectionsRaw, ok := templateRaw["sections"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plan_templates.%s.sections must be a mapping", name)
		}
		sections := map[string]model.SectionSpec{}
		for sectionName, value := range sectionsRaw {
			parsed, err := parseSectionSpec(value, "plan_templates."+name+".sections."+sectionName)
			if err != nil {
				return nil, err
			}
			sections[sectionName] = parsed
		}
		template := model.PlanTemplate{Name: name, Sections: sections, SectionOrder: orders[name]}
		if description, ok := templateRaw["description"].(string); ok {
			template.Description = description
		}
		result[name] = template
	}
	return result, nil
}

func customTemplateSectionOrder(content []byte) map[string][]string {
	orders := map[string][]string{}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil || len(document.Content) == 0 {
		return orders
	}
	root := mappingValue(document.Content[0], "plan_templates")
	if root == nil {
		return orders
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		name := root.Content[i].Value
		sections := mappingValue(root.Content[i+1], "sections")
		if sections == nil {
			continue
		}
		order := make([]string, 0, len(sections.Content)/2)
		for j := 0; j+1 < len(sections.Content); j += 2 {
			order = append(order, sections.Content[j].Value)
		}
		orders[name] = order
	}
	return orders
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func parseSectionSpec(raw any, path string) (model.SectionSpec, error) {
	data, ok := raw.(map[string]any)
	if !ok {
		return model.SectionSpec{}, fmt.Errorf("%s: must be a mapping", path)
	}
	required, ok := data["required"].(bool)
	if !ok {
		return model.SectionSpec{}, fmt.Errorf("%s.required: must be a boolean (got: %s)", path, describe(data["required"]))
	}
	prompt, ok := data["prompt"].(string)
	if !ok {
		return model.SectionSpec{}, fmt.Errorf("%s.prompt: must be a string (got: %s)", path, describe(data["prompt"]))
	}
	kind, err := parseKind(data["kind"], path+".kind")
	if err != nil {
		return model.SectionSpec{}, err
	}
	spec := model.SectionSpec{Required: required, Prompt: prompt, Kind: kind}
	if minLength, ok := integer(data["min_length"]); ok {
		spec.MinLength = model.IntPtr(minLength)
	}
	if min, ok := integer(data["min"]); ok {
		spec.Min = model.IntPtr(min)
	}
	if rawItem, exists := data["item"]; exists {
		item, err := parseItem(rawItem, path+".item")
		if err != nil {
			return model.SectionSpec{}, err
		}
		spec.Item = item
	}
	if atlas, ok := data["atlas_source"].(string); ok {
		spec.AtlasSource = atlas
	}
	return spec, nil
}

func parseKind(raw any, path string) (any, error) {
	if kind, ok := raw.(string); ok {
		for _, valid := range []string{"text", "list", "steps"} {
			if kind == valid {
				return kind, nil
			}
		}
		return nil, fmt.Errorf("%s: unknown kind '%s' (expected text|list|steps|object)", path, kind)
	}
	if fields, ok := raw.(map[string]any); ok {
		return parseNested(fields, path)
	}
	return nil, fmt.Errorf("%s: unknown kind '%s' (expected text|list|steps|object)", path, describe(raw))
}

func parseItem(raw any, path string) (any, error) {
	if raw == "text" {
		return "text", nil
	}
	if fields, ok := raw.(map[string]any); ok {
		return parseNested(fields, path)
	}
	return nil, fmt.Errorf("%s: must be 'text' or an object spec (got: %s)", path, describe(raw))
}

func parseNested(fields map[string]any, path string) (map[string]model.SectionSpec, error) {
	result := map[string]model.SectionSpec{}
	for name, raw := range fields {
		parsed, err := parseSectionSpec(raw, path+"."+name)
		if err != nil {
			return nil, err
		}
		result[name] = parsed
	}
	return result, nil
}

func describe(value any) string {
	if value == nil {
		return "null"
	}
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int64, float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return strings.TrimPrefix(fmt.Sprintf("%T", value), "interface {}")
	}
}
