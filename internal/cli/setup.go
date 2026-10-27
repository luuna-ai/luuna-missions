package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
)

var missionsHookPattern = regexp.MustCompile(`^\s*(?:luuna-missions|lm|sd|seeds)\s+(?:prime|guard)(?:\s|$)`)

type hookSpec struct{ event, matcher, command string }

var hookSpecs = []hookSpec{{"SessionStart", "", "luuna-missions prime --compact"}, {"PreToolUse", "Write|Edit|MultiEdit|NotebookEdit", "luuna-missions guard"}}

func isObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}
func isMissionsHook(value any) bool {
	object, ok := isObject(value)
	if !ok {
		return false
	}
	command, ok := object["command"].(string)
	return ok && missionsHookPattern.MatchString(command)
}
func groupHooks(value any) []any {
	object, ok := isObject(value)
	if !ok {
		return nil
	}
	hooks, _ := object["hooks"].([]any)
	return hooks
}
func makeHookGroup(spec hookSpec) map[string]any {
	group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": spec.command}}}
	if spec.matcher != "" {
		group["matcher"] = spec.matcher
	}
	return group
}
func stripMissionsHooks(groups []any) []any {
	out := []any{}
	for _, group := range groups {
		hooks := groupHooks(group)
		kept := []any{}
		for _, hook := range hooks {
			if !isMissionsHook(hook) {
				kept = append(kept, hook)
			}
		}
		if len(kept) == len(hooks) {
			out = append(out, group)
			continue
		}
		if len(kept) > 0 {
			copy := map[string]any{}
			object, _ := isObject(group)
			for k, v := range object {
				copy[k] = v
			}
			copy["hooks"] = kept
			out = append(out, copy)
		}
	}
	return out
}
func installedHooks(groups []any, spec hookSpec) bool {
	ours := []any{}
	for _, group := range groups {
		for _, hook := range groupHooks(group) {
			if isMissionsHook(hook) {
				ours = append(ours, group)
				break
			}
		}
	}
	return len(ours) == 1 && reflect.DeepEqual(ours[0], makeHookGroup(spec))
}
func syncHookEvent(current any, exists bool, spec hookSpec, remove bool) ([]any, bool, error) {
	groups := []any{}
	if exists {
		var ok bool
		groups, ok = current.([]any)
		if !ok {
			return nil, false, fmt.Errorf("`hooks.%s` in Claude settings is not an array", spec.event)
		}
	}
	if !remove && installedHooks(groups, spec) {
		return nil, false, nil
	}
	updated := stripMissionsHooks(groups)
	if !remove {
		updated = append(updated, makeHookGroup(spec))
	}
	if reflect.DeepEqual(updated, groups) {
		return nil, false, nil
	}
	return updated, true, nil
}
func syncMissionsHooks(settings map[string]any, remove bool) (map[string]any, bool, error) {
	next := map[string]any{}
	for k, v := range settings {
		next[k] = v
	}
	hooks := map[string]any{}
	if raw, exists := next["hooks"]; exists {
		object, ok := isObject(raw)
		if !ok {
			return nil, false, fmt.Errorf("`hooks` in Claude settings is not an object; nothing was written")
		}
		for k, v := range object {
			hooks[k] = v
		}
	}
	changed := false
	for _, spec := range hookSpecs {
		current, exists := hooks[spec.event]
		updated, eventChanged, err := syncHookEvent(current, exists, spec, remove)
		if err != nil {
			return nil, false, err
		}
		if !eventChanged {
			continue
		}
		changed = true
		if len(updated) == 0 {
			delete(hooks, spec.event)
		} else {
			hooks[spec.event] = updated
		}
	}
	if !changed {
		return settings, false, nil
	}
	if len(hooks) == 0 {
		delete(next, "hooks")
	} else {
		next["hooks"] = hooks
	}
	return next, true, nil
}
func readClaudeSettings(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(content)) == "" {
		return map[string]any{}, nil
	}
	var settings any
	if err = json.Unmarshal(content, &settings); err != nil {
		return nil, fmt.Errorf("Cannot parse %s (%s); fix it by hand, nothing was written", path, err)
	}
	object, ok := isObject(settings)
	if !ok {
		return nil, fmt.Errorf("%s is not a JSON object; nothing was written", path)
	}
	return object, nil
}

func runSetup(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"remove", "json"})
	if err != nil {
		return err
	}
	target := ""
	if len(opts.Positionals) > 0 {
		target = opts.Positionals[0]
	}
	if target != "claude" {
		return fmt.Errorf("Unknown setup target %q; supported: claude", target)
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	path := filepath.Join(config.ProjectRoot(dir), ".claude", "settings.json")
	settings, err := readClaudeSettings(path)
	if err != nil {
		return err
	}
	result, changed, err := syncMissionsHooks(settings, opts.Has("remove"))
	if err != nil {
		return err
	}
	if changed {
		if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		var output bytes.Buffer
		output.Write(data)
		output.WriteByte('\n')
		if err = os.WriteFile(path, output.Bytes(), 0644); err != nil {
			return err
		}
	}
	action := "install"
	if opts.Has("remove") {
		action = "remove"
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			Target  string `json:"target"`
			Action  string `json:"action"`
			Changed bool   `json:"changed"`
			File    string `json:"file"`
		}{true, "setup", target, action, changed, path})
	}
	if !changed {
		return ctx.Success(path + ": already up to date")
	}
	if opts.Has("remove") {
		return ctx.Success("Removed missions hooks from " + path)
	}
	return ctx.Success("Installed missions hooks in " + path)
}
