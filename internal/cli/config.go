package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
	"gopkg.in/yaml.v3"
)

var configNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func configPathParts(path string) ([]string, error) {
	parts := []string{}
	for _, part := range strings.Split(path, ".") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("Config path must be non-empty")
	}
	return parts, nil
}

func configValueAt(tree map[string]any, parts []string) (any, bool) {
	var current any = tree
	for _, part := range parts {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func describeConfigType(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case bool:
		return "boolean"
	case int, int64, float64:
		return "number"
	case string:
		return "string"
	}
	return "object"
}

func setConfigValue(tree map[string]any, parts []string, value any) error {
	current := tree
	for i, part := range parts[:len(parts)-1] {
		next, exists := current[part]
		if !exists || next == nil {
			object := map[string]any{}
			current[part] = object
			current = object
			continue
		}
		object, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("Cannot set: '%s' is not an object (got: %s)", strings.Join(parts[:i+1], "."), describeConfigType(next))
		}
		current = object
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func unsetConfigValue(tree map[string]any, parts []string) bool {
	current := tree
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part]
		if !ok {
			return false
		}
		object, ok := next.(map[string]any)
		if !ok {
			return false
		}
		current = object
	}
	last := parts[len(parts)-1]
	if _, ok := current[last]; !ok {
		return false
	}
	delete(current, last)
	return true
}

func parseConfigValue(raw string) (any, error) {
	if raw == "" || raw == "null" || raw == "~" {
		return nil, nil
	}
	if raw == "true" {
		return true, nil
	}
	if raw == "false" {
		return false, nil
	}
	if configNumber.MatchString(raw) {
		if strings.Contains(raw, ".") {
			value, err := strconv.ParseFloat(raw, 64)
			return value, err
		}
		value, err := strconv.Atoi(raw)
		return value, err
	}
	if strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "{") {
		if strings.HasPrefix(raw, "[") && !strings.HasSuffix(raw, "]") {
			return nil, fmt.Errorf("Unclosed flow sequence: %s", raw)
		}
		if strings.HasPrefix(raw, "{") && !strings.HasSuffix(raw, "}") {
			return nil, fmt.Errorf("Unclosed flow mapping: %s", raw)
		}
		var value any
		if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if strings.HasPrefix(raw, "\"") {
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return value, nil
		}
		if len(raw) > 1 && strings.HasSuffix(raw, "\"") {
			return raw[1 : len(raw)-1], nil
		}
	}
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) > 1 {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'"), nil
	}
	return raw, nil
}

func writeJSONValue(ctx *Context, value any) error {
	encoder := json.NewEncoder(ctx.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func runConfig(ctx *Context, args []string) error {
	path, err := nestedPath(args, 1)
	if err != nil {
		return err
	}
	if len(path) == 0 {
		return fmt.Errorf("Usage: luuna-missions config <schema|show|set|unset>")
	}
	cmd := path[0]
	var valueFlags []string
	if cmd == "show" {
		valueFlags = []string{"path"}
	}
	opts, err := ParseOptions(args, valueFlags, []string{"json"})
	if err != nil {
		return err
	}
	if cmd == "schema" {
		data := config.ConfigSchemaJSON()
		if !opts.Has("json") {
			var indented bytes.Buffer
			if err := json.Indent(&indented, data, "", "  "); err != nil {
				return err
			}
			data = indented.Bytes()
		}
		_, err := ctx.Stdout.Write(append(data, '\n'))
		return err
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	if cmd == "show" {
		tree, err := config.ReadTree(dir)
		if err != nil {
			return err
		}
		if opts.Has("path") {
			path := opts.Value("path")
			parts, err := configPathParts(path)
			if err != nil {
				return err
			}
			value, found := configValueAt(tree, parts)
			if !found {
				return fmt.Errorf("Path not found: %s", path)
			}
			if opts.Has("json") {
				return ctx.JSON(struct {
					Success bool   `json:"success"`
					Command string `json:"command"`
					Path    string `json:"path"`
					Value   any    `json:"value"`
				}{true, "config show", path, value})
			}
			if object, ok := value.(map[string]any); ok {
				display, err := config.StringifyTree(object, nil)
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(ctx.Stdout, display)
				return err
			}
			if str, ok := value.(string); ok {
				_, err = fmt.Fprintln(ctx.Stdout, str)
				return err
			}
			return writeJSONValue(ctx, value)
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool           `json:"success"`
				Command string         `json:"command"`
				Config  map[string]any `json:"config"`
			}{true, "config show", tree})
		}
		display, err := config.DisplayTree(dir, tree)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(ctx.Stdout, display)
		return err
	}
	if cmd == "set" {
		if len(opts.Positionals) < 3 {
			return fmt.Errorf("Usage: luuna-missions config set <path> <value>")
		}
		path := opts.Positionals[1]
		parts, err := configPathParts(path)
		if err != nil {
			return err
		}
		value, err := parseConfigValue(opts.Positionals[2])
		if err != nil {
			return err
		}
		err = store.WithLock(filepath.Join(dir, model.ConfigFile), func() error {
			tree, err := config.ReadTree(dir)
			if err != nil {
				return err
			}
			if err = setConfigValue(tree, parts, value); err != nil {
				return err
			}
			if err = config.ValidateTree(tree); err != nil {
				return err
			}
			return config.WriteTree(dir, tree)
		})
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				Path    string `json:"path"`
				Value   any    `json:"value"`
			}{true, "config set", path, value})
		}
		valueDisplay := fmt.Sprint(value)
		if value == nil {
			valueDisplay = "null"
		} else if _, ok := value.(string); !ok {
			b, _ := json.Marshal(value)
			valueDisplay = string(b)
		}
		return ctx.Success("Set " + path + " = " + valueDisplay)
	}
	if cmd == "unset" {
		if len(opts.Positionals) < 2 {
			return fmt.Errorf("Usage: luuna-missions config unset <path>")
		}
		path := opts.Positionals[1]
		parts, err := configPathParts(path)
		if err != nil {
			return err
		}
		removed := false
		err = store.WithLock(filepath.Join(dir, model.ConfigFile), func() error {
			tree, err := config.ReadTree(dir)
			if err != nil {
				return err
			}
			removed = unsetConfigValue(tree, parts)
			if !removed {
				return nil
			}
			if err = config.ValidateTree(tree); err != nil {
				return err
			}
			return config.WriteTree(dir, tree)
		})
		if err != nil {
			return err
		}
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				Path    string `json:"path"`
				Removed bool   `json:"removed"`
			}{true, "config unset", path, removed})
		}
		if removed {
			return ctx.Success("Unset " + path)
		}
		_, err = fmt.Fprintln(ctx.Stdout, "No such path: "+path)
		return err
	}
	return fmt.Errorf("Unknown config subcommand: %s", cmd)
}
