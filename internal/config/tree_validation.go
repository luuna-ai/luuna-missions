package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
	"gopkg.in/yaml.v3"
)

// DisplayTree emits the config tree in source declaration order, as the Bun
// command's parse/stringify cycle does for human-readable `config show`.
func DisplayTree(dir string, tree map[string]any) (string, error) {
	content, err := os.ReadFile(filepath.Join(dir, model.ConfigFile))
	if err != nil {
		return "", err
	}
	orders := map[string][]string{}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return "", err
	}
	if len(document.Content) > 0 {
		collectKeyOrder(document.Content[0], "", orders)
	}
	return StringifyTree(tree, orders)
}

// ValidateTree enforces the complete config schema before config set/unset
// writes. Its error format is the public config command's validation contract.
func ValidateTree(tree map[string]any) error {
	errors := []string{}
	requireKeys(tree, "", []string{"project", "version"}, &errors)
	for _, key := range sortedTreeKeys(tree) {
		value := tree[key]
		switch key {
		case "project":
			text, ok := value.(string)
			if !ok {
				addTypeError(&errors, key, "string")
			} else if text == "" {
				addMinLengthError(&errors, key, 1)
			}
		case "version":
			if _, ok := value.(string); !ok {
				addTypeError(&errors, key, "string")
			}
		case "max_plan_depth":
			n, ok := wholeNumber(value)
			if !ok {
				addTypeError(&errors, key, "integer")
			} else if n < 1 {
				addRuleError(&errors, key, "minimum", "comparison=>=, limit=1")
			}
		case "plan_templates":
			definitions, ok := value.(map[string]any)
			if !ok {
				addTypeError(&errors, key, "object")
				continue
			}
			for _, name := range sortedTreeKeys(definitions) {
				validatePlanTemplateTree(definitions[name], "plan_templates."+name, &errors)
			}
		default:
			addRuleError(&errors, "", "additionalProperties", "additionalProperty="+key)
		}
	}
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("Config validation failed:\n%s", strings.Join(errors, "\n"))
}

func validatePlanTemplateTree(value any, path string, errors *[]string) {
	object, ok := value.(map[string]any)
	if !ok {
		addTypeError(errors, path, "object")
		return
	}
	requireKeys(object, path, []string{"sections"}, errors)
	for _, key := range sortedTreeKeys(object) {
		fieldPath := path + "." + key
		switch key {
		case "description":
			if _, ok := object[key].(string); !ok {
				addTypeError(errors, fieldPath, "string")
			}
		case "sections":
			sections, ok := object[key].(map[string]any)
			if !ok {
				addTypeError(errors, fieldPath, "object")
				continue
			}
			for _, name := range sortedTreeKeys(sections) {
				validateSectionSpecTree(sections[name], fieldPath+"."+name, errors)
			}
		default:
			addRuleError(errors, path, "additionalProperties", "additionalProperty="+key)
		}
	}
}

func validateSectionSpecTree(value any, path string, errors *[]string) {
	object, ok := value.(map[string]any)
	if !ok {
		addTypeError(errors, path, "object")
		return
	}
	requireKeys(object, path, []string{"required", "kind", "prompt"}, errors)
	for _, key := range sortedTreeKeys(object) {
		fieldPath := path + "." + key
		field := object[key]
		switch key {
		case "required":
			if _, ok := field.(bool); !ok {
				addTypeError(errors, fieldPath, "boolean")
			}
		case "prompt", "atlas_source":
			if _, ok := field.(string); !ok {
				addTypeError(errors, fieldPath, "string")
			}
		case "min", "min_length":
			n, ok := wholeNumber(field)
			if !ok {
				addTypeError(errors, fieldPath, "integer")
			} else if n < 0 {
				addRuleError(errors, fieldPath, "minimum", "comparison=>=, limit=0")
			}
		case "kind":
			validateSpecShape(field, fieldPath, []string{"text", "list", "steps"}, errors)
		case "item":
			validateSpecShape(field, fieldPath, []string{"text"}, errors)
		default:
			addRuleError(errors, path, "additionalProperties", "additionalProperty="+key)
		}
	}
}

func validateSpecShape(value any, path string, literals []string, errors *[]string) {
	if text, ok := value.(string); ok {
		for _, literal := range literals {
			if text == literal {
				return
			}
		}
		addRuleError(errors, path, "oneOf", "passingSchemas=null")
		return
	}
	object, ok := value.(map[string]any)
	if !ok {
		addRuleError(errors, path, "oneOf", "passingSchemas=null")
		return
	}
	for _, name := range sortedTreeKeys(object) {
		validateSectionSpecTree(object[name], path+"."+name, errors)
	}
}

func requireKeys(object map[string]any, path string, keys []string, errors *[]string) {
	for _, key := range keys {
		if _, exists := object[key]; exists {
			continue
		}
		fieldPath := key
		if path != "" {
			fieldPath = path + "." + key
		}
		*errors = append(*errors, fmt.Sprintf("  %s: add a '%s' field", fieldPath, key))
	}
}

func addTypeError(errors *[]string, path, expected string) {
	*errors = append(*errors, fmt.Sprintf("  %s: '%s' must be a %s", path, path, expected))
}

func addMinLengthError(errors *[]string, path string, minimum int) {
	*errors = append(*errors, fmt.Sprintf("  %s: expand '%s' to at least %d characters", path, path, minimum))
}

func addRuleError(errors *[]string, path, rule, detail string) {
	location := path
	if location == "" {
		location = "(root)"
	}
	where := ""
	if path != "" {
		where = "'" + path + "': "
	}
	*errors = append(*errors, fmt.Sprintf("  %s: %s%s (%s)", location, where, rule, detail))
}

func sortedTreeKeys(tree map[string]any) []string {
	keys := make([]string, 0, len(tree))
	for key := range tree {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func wholeNumber(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if float64(int(n)) == n {
			return int(n), true
		}
	}
	return 0, false
}
