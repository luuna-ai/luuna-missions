package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
)

const onboardStart = "<!-- missions:start -->"
const onboardEnd = "<!-- missions:end -->"
const onboardSchema = "<!-- missions-onboard-schema:8 -->"

func onboardSnippet(version string) string {
	content := fmt.Sprintf(`## Issue Tracking (Missions)
<!-- missions-onboard:v%s -->
%s

This project uses [Missions](https://github.com/luuna-ai/luuna-missions) v%s (~lm~) for git-native issue tracking. Run ~luuna-missions prime~ at session start for the workflow and live state (in-progress and ready issues), or run ~luuna-missions setup claude~ once to do it automatically.

- ~luuna-missions ready~ — Find unblocked work
- ~luuna-missions update <id> --claim~ — Claim work (atomic)
- ~luuna-missions create --title "..." [--from <id>]~ — File work
- ~luuna-missions close <id>~ — Complete work
- ~luuna-missions plan prompt <id>~ — Decompose large or ambiguous work
- ~luuna-missions sync~ — Commit ~.luuna/missions/~ changes

Never hand-edit ~.luuna/missions/*.jsonl~; ~luuna-missions~ locks and validates the store.`, version, onboardSchema, version)
	return strings.ReplaceAll(content, "~", "`")
}
func wrapOnboard(snippet string) string { return onboardStart + "\n" + snippet + "\n" + onboardEnd }
func upgradeOnboardMarkers(content string) string {
	return strings.ReplaceAll(content, "<!-- seeds", "<!-- missions")
}
func onboardStatus(content string) string {
	content = upgradeOnboardMarkers(content)
	if !strings.Contains(content, onboardStart) || !strings.Contains(content, onboardEnd) {
		return "missing"
	}
	if strings.Contains(content, "<!-- missions-onboard-v:") {
		return "outdated"
	}
	if strings.Contains(content, onboardSchema) {
		return "current"
	}
	return "outdated"
}
func replaceOnboardSection(content, snippet string) (string, bool) {
	content = upgradeOnboardMarkers(content)
	start, end := strings.Index(content, onboardStart), strings.Index(content, onboardEnd)
	if start < 0 || end < start {
		return "", false
	}
	return content[:start] + wrapOnboard(snippet) + content[end+len(onboardEnd):], true
}
func onboardTarget(root string) (string, bool, error) {
	for _, name := range []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md"), "AGENTS.md"} {
		path := filepath.Join(root, name)
		_, err := os.Stat(path)
		if err == nil {
			return path, true, nil
		}
		if !os.IsNotExist(err) {
			return "", false, err
		}
	}
	return filepath.Join(root, "CLAUDE.md"), false, nil
}

func runOnboard(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"stdout", "check", "json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'onboard'")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	root := config.ProjectRoot(dir)
	path, exists, err := onboardTarget(root)
	if err != nil {
		return err
	}
	snippet := onboardSnippet(ctx.App.Version)
	if opts.Has("check") {
		status := "missing"
		if exists {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			status = onboardStatus(string(content))
		}
		if opts.Has("json") {
			var file any = nil
			if exists {
				file = path
			}
			return ctx.JSON(struct {
				Success bool   `json:"success"`
				Command string `json:"command"`
				Status  string `json:"status"`
				File    any    `json:"file"`
			}{true, "onboard", status, file})
		}
		if !exists {
			_, err = fmt.Fprintln(ctx.Stdout, "Status: missing (no CLAUDE.md found)")
			return err
		}
		_, err = fmt.Fprintf(ctx.Stdout, "Status: %s (%s)\n", status, path)
		return err
	}
	if opts.Has("stdout") {
		_, err = fmt.Fprintln(ctx.Stdout, wrapOnboard(snippet))
		return err
	}
	wrapped := wrapOnboard(snippet)
	action := ""
	if !exists {
		if err = os.WriteFile(path, []byte(wrapped+"\n"), 0644); err != nil {
			return err
		}
		action = "created"
	} else {
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(contentBytes)
		switch onboardStatus(content) {
		case "current":
			action = "unchanged"
		case "outdated":
			updated, ok := replaceOnboardSection(content, snippet)
			if !ok {
				message := fmt.Sprintf("Found missions markers in %s but could not replace the section (markers may be malformed, out of order, or duplicated). Fix the file manually or remove the missions:start/missions:end block and re-run.", path)
				if opts.Has("json") {
					ctx.ExitCode = 1
					return ctx.JSON(struct {
						Success bool   `json:"success"`
						Command string `json:"command"`
						Action  string `json:"action"`
						File    string `json:"file"`
						Error   string `json:"error"`
					}{false, "onboard", "failed", path, message})
				}
				return fmt.Errorf("%s", message)
			}
			if err = os.WriteFile(path, []byte(updated), 0644); err != nil {
				return err
			}
			action = "updated"
		default:
			separator := "\n\n"
			if strings.HasSuffix(content, "\n") {
				separator = "\n"
			}
			if err = os.WriteFile(path, []byte(content+separator+wrapped+"\n"), 0644); err != nil {
				return err
			}
			action = "appended"
		}
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			Action  string `json:"action"`
			File    string `json:"file"`
		}{true, "onboard", action, path})
	}
	switch action {
	case "created":
		return ctx.Success("Created " + path + " with missions section")
	case "updated":
		return ctx.Success("Updated missions section in " + path)
	case "appended":
		return ctx.Success("Added missions section to " + path)
	default:
		return ctx.Success("Missions section is already up to date")
	}
}
