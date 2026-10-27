package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
	"gopkg.in/yaml.v3"
)

//go:embed schema.json
var schemaJSON []byte

// ConfigSchema returns the wire schema exposed by `config schema`.
func ConfigSchema() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		panic("embedded config schema: " + err.Error())
	}
	return schema
}

// ConfigSchemaJSON preserves the source schema's JSON property order.
func ConfigSchemaJSON() []byte { return append([]byte(nil), schemaJSON...) }

// ReadTree reads all config keys, including user-defined plan templates.
func ReadTree(dir string) (map[string]any, error) {
	path := filepath.Join(dir, model.ConfigFile)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("Config not found at %s", path)
	}
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := yaml.Unmarshal(content, &tree); err != nil {
		return nil, err
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return UpgradeLegacyConfigKeys(tree).(map[string]any), nil
}

// WriteTree writes a validated tree with the same atomic replacement strategy
// as the Bun config command. The caller holds the config.yaml advisory lock.
// Existing key declaration order is retained where possible.
func WriteTree(dir string, tree map[string]any) error {
	path := filepath.Join(dir, model.ConfigFile)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	orders := map[string][]string{}
	var document yaml.Node
	if len(old) > 0 && yaml.Unmarshal(old, &document) == nil && len(document.Content) > 0 {
		collectKeyOrder(document.Content[0], "", orders)
	}
	content, err := StringifyTree(tree, orders)
	if err != nil {
		return err
	}
	previous, statErr := os.Stat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	tmp, err := os.CreateTemp(dir, model.ConfigFile+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write([]byte(content)); err != nil {
		tmp.Close()
		return err
	}
	if statErr == nil {
		if err := tmp.Chmod(previous.Mode().Perm()); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func collectKeyOrder(node *yaml.Node, path string, orders map[string][]string) {
	if node.Kind == yaml.SequenceNode {
		for i, item := range node.Content {
			collectKeyOrder(item, path+"/"+strconv.Itoa(i), orders)
		}
		return
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		orders[path] = append(orders[path], key)
		collectKeyOrder(node.Content[i+1], path+"/"+key, orders)
	}
}

// StringifyTree emits the minimal YAML subset used by the prior CLI. An empty
// order map is suitable when displaying a value independent of a source file.
func StringifyTree(tree map[string]any, orders map[string][]string) (string, error) {
	if orders == nil {
		orders = map[string][]string{}
	}
	var out strings.Builder
	if err := emitMap(&out, tree, 0, "", orders); err != nil {
		return "", err
	}
	if out.Len() == 0 {
		out.WriteByte('\n')
	}
	return out.String(), nil
}

func mapKeys(m map[string]any, declared []string) []string {
	out := make([]string, 0, len(m))
	seen := map[string]bool{}
	for _, key := range declared {
		if _, ok := m[key]; ok && !seen[key] {
			out, seen[key] = append(out, key), true
		}
	}
	if _, isSpec := m["required"]; isSpec {
		for _, key := range []string{"required", "kind", "prompt", "min_length", "min", "item", "atlas_source"} {
			if _, ok := m[key]; ok && !seen[key] {
				out, seen[key] = append(out, key), true
			}
		}
	} else if _, isTemplate := m["sections"]; isTemplate {
		for _, key := range []string{"sections", "description"} {
			if _, ok := m[key]; ok && !seen[key] {
				out, seen[key] = append(out, key), true
			}
		}
	}
	rest := make([]string, 0, len(m)-len(out))
	for key := range m {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func emitMap(out *strings.Builder, data map[string]any, indent int, path string, orders map[string][]string) error {
	for _, key := range mapKeys(data, orders[path]) {
		value := data[key]
		out.WriteString(strings.Repeat(" ", indent))
		out.WriteString(formatKey(key))
		out.WriteByte(':')
		switch v := value.(type) {
		case nil:
			out.WriteByte('\n')
		case map[string]any:
			if len(v) == 0 {
				out.WriteString(" {}\n")
			} else {
				out.WriteByte('\n')
				if err := emitMap(out, v, indent+2, path+"/"+key, orders); err != nil {
					return err
				}
			}
		case []any:
			if len(v) == 0 {
				out.WriteString(" []\n")
			} else {
				out.WriteByte('\n')
				if err := emitSeq(out, v, indent, path+"/"+key, orders); err != nil {
					return err
				}
			}
		default:
			formatted, err := formatScalar(v)
			if err != nil {
				return err
			}
			out.WriteByte(' ')
			out.WriteString(formatted)
			out.WriteByte('\n')
		}
	}
	return nil
}

func emitSeq(out *strings.Builder, values []any, indent int, path string, orders map[string][]string) error {
	for index, value := range values {
		out.WriteString(strings.Repeat(" ", indent))
		out.WriteByte('-')
		switch v := value.(type) {
		case nil:
			out.WriteByte('\n')
		case map[string]any:
			if len(v) == 0 {
				out.WriteString(" {}\n")
				continue
			}
			itemPath := path + "/" + strconv.Itoa(index)
			keys := mapKeys(v, orders[itemPath])
			if len(keys) > 0 && isSimpleTreeValue(v[keys[0]]) {
				allSimple := true
				for _, key := range keys[1:] {
					if !isSimpleTreeValue(v[key]) {
						allSimple = false
						break
					}
				}
				if allSimple {
					first := keys[0]
					formatted, err := formatScalar(v[first])
					if err != nil {
						return err
					}
					out.WriteString(" " + formatKey(first) + ": " + formatted + "\n")
					if len(keys) > 1 {
						rest := make(map[string]any, len(keys)-1)
						for _, key := range keys[1:] {
							rest[key] = v[key]
						}
						if err := emitMap(out, rest, indent+2, itemPath, orders); err != nil {
							return err
						}
					}
					continue
				}
			}
			out.WriteByte('\n')
			if err := emitMap(out, v, indent+2, itemPath, orders); err != nil {
				return err
			}
		case []any:
			if len(v) == 0 {
				out.WriteString(" []\n")
				continue
			}
			out.WriteByte('\n')
			if err := emitSeq(out, v, indent+2, path, orders); err != nil {
				return err
			}
		default:
			formatted, err := formatScalar(v)
			if err != nil {
				return err
			}
			out.WriteByte(' ')
			out.WriteString(formatted)
			out.WriteByte('\n')
		}
	}
	return nil
}

func isSimpleTreeValue(value any) bool {
	if value == nil {
		return false
	}
	switch value.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

var (
	quoteScalar   = regexp.MustCompile("[:#\"'\\[\\]{}\\n,&*!|>%@`]")
	quoteKey      = regexp.MustCompile("[\\s:#\"'\\[\\]{},&*!|>%@`]")
	numericScalar = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
)

func formatKey(value string) string {
	if value == "" || quoteKey.MatchString(value) {
		return strconv.Quote(value)
	}
	return value
}

func formatScalar(value any) (string, error) {
	switch v := value.(type) {
	case string:
		if v == "" || v == "true" || v == "false" || v == "null" || v == "~" || numericScalar.MatchString(v) || quoteScalar.MatchString(v) || strings.TrimSpace(v) != v {
			return strconv.Quote(v), nil
		}
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported config value %T", value)
	}
}
