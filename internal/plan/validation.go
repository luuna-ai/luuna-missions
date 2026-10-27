package plan

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

// ErrorEntry and Diff are the public partial-state validation contract used by
// `plan submit` and `plan validate`. The `current` value is the submitted plan.
type ErrorEntry struct {
	Path string `json:"path"`
	Code string `json:"code"`
	Fix  string `json:"fix"`
}

type Diff struct {
	Errors  []ErrorEntry `json:"errors"`
	Current any          `json:"current"`
}

// Validate mirrors the plan template schema and the step-specific checks in
// src/plan-schema.ts. Unknown keys remain allowed, as in the source schema.
func Validate(template model.PlanTemplate, submitted any) (bool, *Diff) {
	errors := make([]ErrorEntry, 0)
	data, ok := submitted.(map[string]any)
	if !ok {
		errors = append(errors, typeError("", "object"))
		return false, &Diff{Errors: errors, Current: submitted}
	}
	if _, exists := data["template"]; !exists {
		errors = append(errors, requiredError("template"))
	}
	if _, exists := data["sections"]; !exists {
		errors = append(errors, requiredError("sections"))
	}
	if name, exists := data["template"]; exists {
		if name, ok := name.(string); !ok {
			errors = append(errors, typeError("template", "string"))
		} else if name != template.Name {
			errors = append(errors, ErrorEntry{Path: "template", Code: "const", Fix: "'template': const (allowedValue=" + template.Name + ")"})
		}
	}
	rawSections, exists := data["sections"]
	if exists {
		if sections, ok := rawSections.(map[string]any); !ok {
			errors = append(errors, typeError("sections", "object"))
		} else {
			for _, name := range sectionNames(template) {
				if spec := template.Sections[name]; spec.Required {
					if _, present := sections[name]; !present {
						errors = append(errors, requiredError("sections."+name))
					}
				}
			}
			for _, name := range sectionNames(template) {
				spec := template.Sections[name]
				value, present := sections[name]
				path := "sections." + name
				if !present {
					continue
				}
				errors = append(errors, validateSection(spec, value, path)...)
			}
			errors = append(errors, validateSteps(template, sections)...)
		}
	}
	if len(errors) == 0 {
		return true, nil
	}
	return false, &Diff{Errors: errors, Current: submitted}
}

func requiredError(path string) ErrorEntry {
	parts := strings.Split(path, ".")
	field := parts[len(parts)-1]
	return ErrorEntry{Path: path, Code: "required", Fix: "add a '" + field + "' field"}
}

func typeError(path, expected string) ErrorEntry {
	where := path
	if where == "" {
		where = "value"
	}
	return ErrorEntry{Path: path, Code: "type", Fix: "'" + where + "' must be a " + expected}
}

func minLengthError(path string, minimum int) ErrorEntry {
	return ErrorEntry{Path: path, Code: "minLength", Fix: fmt.Sprintf("expand '%s' to at least %d characters", path, minimum)}
}

func minItemsError(path string, minimum, got int) ErrorEntry {
	need := minimum - got
	if need < 1 {
		need = 1
	}
	noun := "entry"
	if need != 1 {
		noun = "entries"
	}
	return ErrorEntry{Path: path, Code: "min", Fix: fmt.Sprintf("add at least %d more %s", need, noun)}
}

func validateSection(spec model.SectionSpec, value any, path string) []ErrorEntry {
	if kind, ok := spec.Kind.(string); ok {
		switch kind {
		case "text":
			text, ok := value.(string)
			if !ok {
				return []ErrorEntry{typeError(path, "string")}
			}
			minimum := 1
			if spec.MinLength != nil {
				minimum = *spec.MinLength
			}
			if len(utf16.Encode([]rune(text))) < minimum {
				return []ErrorEntry{minLengthError(path, minimum)}
			}
			return nil
		case "list", "steps":
			items, ok := value.([]any)
			if !ok {
				return []ErrorEntry{typeError(path, "array")}
			}
			var errors []ErrorEntry
			if spec.Min != nil && len(items) < *spec.Min {
				errors = append(errors, minItemsError(path, *spec.Min, len(items)))
			}
			for index, item := range items {
				itemPath := path + "." + strconv.Itoa(index)
				if kind == "steps" {
					errors = append(errors, validateStepFields(item, itemPath)...)
				} else {
					errors = append(errors, validateListItem(spec.Item, item, itemPath)...)
				}
			}
			return errors
		}
	}
	return validateObject(spec.Kind, value, path)
}

func validateListItem(itemSpec any, value any, path string) []ErrorEntry {
	if itemSpec == nil || itemSpec == "text" {
		text, ok := value.(string)
		if !ok {
			return []ErrorEntry{typeError(path, "string")}
		}
		if text == "" {
			return []ErrorEntry{minLengthError(path, 1)}
		}
		return nil
	}
	return validateObject(itemSpec, value, path)
}

func validateObject(spec any, value any, path string) []ErrorEntry {
	object, ok := value.(map[string]any)
	if !ok {
		return []ErrorEntry{typeError(path, "object")}
	}
	fields, ok := spec.(map[string]model.SectionSpec)
	if !ok {
		return nil
	}
	var errors []ErrorEntry
	for _, name := range sortedKeys(fields) {
		field := fields[name]
		if _, present := object[name]; !present && field.Required {
			errors = append(errors, requiredError(path+"."+name))
		}
	}
	for _, name := range sortedKeys(fields) {
		field := fields[name]
		fieldPath := path + "." + name
		fieldValue, present := object[name]
		if !present {
		} else {
			errors = append(errors, validateSection(field, fieldValue, fieldPath)...)
		}
	}
	return errors
}

func validateStepFields(value any, path string) []ErrorEntry {
	step, ok := value.(map[string]any)
	if !ok {
		return []ErrorEntry{typeError(path, "object")}
	}
	var errors []ErrorEntry
	for _, name := range []string{"title", "type", "priority", "blocks", "plan_template", "existing_seed", "labels"} {
		value, exists := step[name]
		if !exists {
			continue
		}
		fieldPath := path + "." + name
		switch name {
		case "title", "plan_template", "existing_seed":
			text, ok := value.(string)
			if !ok {
				errors = append(errors, typeError(fieldPath, "string"))
			} else if name != "plan_template" && text == "" {
				errors = append(errors, minLengthError(fieldPath, 1))
			}
		case "type":
			text, ok := value.(string)
			if !ok {
				errors = append(errors, typeError(fieldPath, "string"))
			} else if !contains(model.ValidTypes, text) {
				errors = append(errors, ErrorEntry{Path: fieldPath, Code: "enum", Fix: "'" + fieldPath + "': enum (allowedValues=task,bug,feature,epic)"})
			}
		case "priority":
			number, ok := number(value)
			if !ok || math.Trunc(number) != number {
				errors = append(errors, typeError(fieldPath, "integer"))
			} else if number < 0 {
				errors = append(errors, ErrorEntry{Path: fieldPath, Code: "minimum", Fix: "'" + fieldPath + "': minimum (comparison=>=, limit=0)"})
			} else if number > 4 {
				errors = append(errors, ErrorEntry{Path: fieldPath, Code: "maximum", Fix: "'" + fieldPath + "': maximum (comparison=<=, limit=4)"})
			}
		case "blocks", "labels":
			items, ok := value.([]any)
			if !ok {
				errors = append(errors, typeError(fieldPath, "array"))
				continue
			}
			for index, item := range items {
				itemPath := fieldPath + "." + strconv.Itoa(index)
				if name == "blocks" {
					n, ok := number(item)
					if !ok || math.Trunc(n) != n {
						errors = append(errors, typeError(itemPath, "integer"))
					}
				} else {
					label, ok := item.(string)
					if !ok {
						errors = append(errors, typeError(itemPath, "string"))
					} else if label == "" {
						errors = append(errors, minLengthError(itemPath, 1))
					} else if strings.TrimSpace(label) == "" {
						errors = append(errors, ErrorEntry{Path: itemPath, Code: "pattern", Fix: "'" + itemPath + "': pattern (pattern=\\S)"})
					}
				}
			}
		}
	}
	return errors
}

func validateSteps(template model.PlanTemplate, sections map[string]any) []ErrorEntry {
	var errors []ErrorEntry
	for _, name := range sectionNames(template) {
		spec := template.Sections[name]
		kind, isLiteral := spec.Kind.(string)
		if !isLiteral || kind != "steps" {
			continue
		}
		steps, ok := sections[name].([]any)
		if !ok {
			continue
		}
		for index, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				continue
			}
			base := "sections." + name + "." + strconv.Itoa(index)
			title, titleOK := step["title"].(string)
			adopt, adoptOK := step["existing_seed"].(string)
			if (!titleOK || title == "") && (!adoptOK || adopt == "") {
				errors = append(errors, ErrorEntry{Path: base, Code: "missing-title", Fix: fmt.Sprintf("step %d must declare either 'title' (fresh spawn) or 'existing_seed' (adoption)", index+1)})
			}
			blocks, ok := step["blocks"].([]any)
			if !ok {
				continue
			}
			for _, raw := range blocks {
				n, valid := number(raw)
				if !valid || math.Trunc(n) != n {
					continue
				}
				path := base + ".blocks"
				if int(n) == index+1 {
					errors = append(errors, ErrorEntry{Path: path, Code: "self-reference", Fix: fmt.Sprintf("step %d cannot block itself; remove %d from blocks", index+1, int(n))})
				} else if n < 1 || n > float64(len(steps)) {
					errors = append(errors, ErrorEntry{Path: path, Code: "out-of-range", Fix: fmt.Sprintf("step index %d is out of range (step indices are 1-based; valid range 1..%d)", int(n), len(steps))})
				}
			}
		}
	}
	return errors
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func sectionNames(template model.PlanTemplate) []string {
	result := make([]string, 0, len(template.Sections))
	seen := make(map[string]bool, len(template.Sections))
	for _, name := range template.SectionOrder {
		if _, exists := template.Sections[name]; exists && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	remaining := make([]string, 0)
	for name := range template.Sections {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	return append(result, remaining...)
}

func sortedKeys(fields map[string]model.SectionSpec) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
