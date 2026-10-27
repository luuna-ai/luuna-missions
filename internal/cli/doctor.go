package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

type doctorCheck struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Details []string `json:"details"`
	Fixable bool     `json:"fixable"`
}

type doctorLine struct {
	Number   int
	Text     string
	Parsed   any
	ParseErr error
}

func passCheck(name, message string) doctorCheck {
	return doctorCheck{name, "pass", message, []string{}, false}
}
func issueCheck(name, status, message string, details []string, fixable bool) doctorCheck {
	return doctorCheck{name, status, message, details, fixable}
}

func runDoctor(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"fix", "json", "verbose"})
	if err != nil {
		return err
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	checks, err := collectDoctorChecks(dir)
	if err != nil {
		return err
	}
	var fixed []string
	if opts.Has("fix") {
		for _, check := range checks {
			if check.Status != "pass" && check.Fixable {
				fixed, err = applyDoctorFixes(dir, checks)
				if err != nil {
					return err
				}
				checks, err = collectDoctorChecks(dir)
				if err != nil {
					return err
				}
				break
			}
		}
	}
	pass, warn, fail := 0, 0, 0
	for _, check := range checks {
		switch check.Status {
		case "pass":
			pass++
		case "warn":
			warn++
		case "fail":
			fail++
		}
	}
	if opts.Has("json") {
		payload := struct {
			Success bool          `json:"success"`
			Command string        `json:"command"`
			Checks  []doctorCheck `json:"checks"`
			Summary struct {
				Pass int `json:"pass"`
				Warn int `json:"warn"`
				Fail int `json:"fail"`
			} `json:"summary"`
			Fixed []string `json:"fixed,omitempty"`
		}{Success: fail == 0, Command: "doctor", Checks: checks, Fixed: fixed}
		payload.Summary.Pass, payload.Summary.Warn, payload.Summary.Fail = pass, warn, fail
		if err := ctx.JSON(payload); err != nil {
			return err
		}
	} else {
		fmt.Fprint(ctx.Stdout, "\nMissions Doctor\n\n")
		for _, check := range checks {
			if check.Status == "pass" && !opts.Has("verbose") {
				continue
			}
			icon := "✓"
			if check.Status == "warn" {
				icon = "!"
			} else if check.Status == "fail" {
				icon = "✗"
			}
			fmt.Fprintf(ctx.Stdout, "  %s %s\n", icon, check.Message)
			for _, detail := range check.Details {
				fmt.Fprintf(ctx.Stdout, "      %s\n", detail)
			}
		}
		fmt.Fprintf(ctx.Stdout, "\n%d passed, %d warning(s), %d failure(s)\n", pass, warn, fail)
		if len(fixed) > 0 {
			fmt.Fprint(ctx.Stdout, "\nFixed:\n")
			for _, item := range fixed {
				fmt.Fprintf(ctx.Stdout, "  ✓ %s\n", item)
			}
		}
	}
	if fail > 0 {
		ctx.ExitCode = 1
	}
	return nil
}

func doctorRawLines(path string) ([]doctorLine, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []doctorLine{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines := []doctorLine{}
	for index, raw := range bytes.Split(content, []byte{'\n'}) {
		text := strings.TrimSpace(string(raw))
		if text == "" {
			continue
		}
		line := doctorLine{Number: index + 1, Text: text}
		line.ParseErr = json.Unmarshal([]byte(text), &line.Parsed)
		lines = append(lines, line)
	}
	return lines, nil
}

func doctorObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func doctorID(object map[string]any, line int) string {
	if id, ok := object["id"].(string); ok {
		return id
	}
	return fmt.Sprintf("line %d", line)
}

func doctorString(value any) (string, bool) { text, ok := value.(string); return text, ok }
func doctorTruthyString(value any) bool     { text, ok := value.(string); return ok && text != "" }

func collectDoctorChecks(dir string) ([]doctorCheck, error) {
	checks := []doctorCheck{}
	configCheck := doctorConfigCheck(dir)
	checks = append(checks, configCheck)
	if configCheck.Status == "fail" {
		return checks, nil
	}
	issueLines, err := doctorRawLines(store.IssuesPath(dir))
	if err != nil {
		return nil, err
	}
	templateLines, err := doctorRawLines(store.TemplatesPath(dir))
	if err != nil {
		return nil, err
	}
	checks = append(checks, doctorJSONLCheck(issueLines, templateLines))
	checks = append(checks, doctorSchemaCheck(issueLines))
	checks = append(checks, doctorDuplicatesCheck(issueLines, templateLines))
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return nil, err
	}
	checks = append(checks, doctorReferencesCheck(issues))
	checks = append(checks, doctorBidirectionalCheck(issues))
	checks = append(checks, doctorCyclesCheck(issues))
	checks = append(checks, doctorLabelsCheck(issueLines))
	checks = append(checks, doctorExtensionsCheck(issueLines))
	checks = append(checks, doctorClosedCheck(issueLines))
	checks = append(checks, doctorLocksCheck(dir))
	checks = append(checks, doctorGitattributesCheck(dir))
	return checks, nil
}

func doctorConfigCheck(dir string) doctorCheck {
	if _, err := os.Stat(dir); err != nil {
		return issueCheck("config", "fail", ".luuna/missions/ directory not found", []string{}, false)
	}
	projectConfig, err := config.ReadConfig(dir)
	if err != nil {
		return issueCheck("config", "fail", "config.yaml is missing or unparseable", []string{}, false)
	}
	if projectConfig.Project == "" {
		return issueCheck("config", "fail", "config.yaml missing required 'project' field", []string{}, false)
	}
	return passCheck("config", "Config is valid")
}

func doctorJSONLCheck(issues, templates []doctorLine) doctorCheck {
	details := []string{}
	for _, pair := range []struct {
		Name  string
		Lines []doctorLine
	}{{model.IssuesFile, issues}, {model.TemplatesFile, templates}} {
		for _, line := range pair.Lines {
			if line.ParseErr != nil {
				details = append(details, fmt.Sprintf("%s line %d: %s", pair.Name, line.Number, line.ParseErr))
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("jsonl-integrity", "fail", fmt.Sprintf("%d malformed line(s) in JSONL files", len(details)), details, true)
	}
	return passCheck("jsonl-integrity", "All JSONL lines parse correctly")
}

func doctorSchemaCheck(lines []doctorLine) doctorCheck {
	details := []string{}
	for _, line := range lines {
		if line.ParseErr != nil || line.Parsed == nil {
			continue
		}
		issue := doctorObject(line.Parsed)
		id := doctorID(issue, line.Number)
		for _, key := range []string{"id", "title", "createdAt", "updatedAt"} {
			if !doctorTruthyString(issue[key]) {
				details = append(details, fmt.Sprintf("%s: missing or invalid '%s'", id, key))
			}
		}
		if status, ok := doctorString(issue["status"]); ok && !doctorContains(model.ValidStatuses, status) {
			details = append(details, fmt.Sprintf("%s: invalid status '%s'", id, status))
		}
		if issueType, ok := doctorString(issue["type"]); ok && !doctorContains(model.ValidTypes, issueType) {
			details = append(details, fmt.Sprintf("%s: invalid type '%s'", id, issueType))
		}
		if priority, ok := issue["priority"].(float64); ok && (priority < 0 || priority > 4) {
			details = append(details, fmt.Sprintf("%s: invalid priority %s (must be 0-4)", id, doctorNumber(priority)))
		}
	}
	if len(details) > 0 {
		return issueCheck("schema-validation", "fail", fmt.Sprintf("%d schema violation(s)", len(details)), details, false)
	}
	return passCheck("schema-validation", "All issues have valid schema")
}

func doctorDuplicatesCheck(issues, templates []doctorLine) doctorCheck {
	details := []string{}
	for _, pair := range []struct {
		Name  string
		Lines []doctorLine
	}{{model.IssuesFile, issues}, {model.TemplatesFile, templates}} {
		counts, order := map[string]int{}, []string{}
		for _, line := range pair.Lines {
			if line.ParseErr != nil {
				continue
			}
			if id, ok := doctorObject(line.Parsed)["id"].(string); ok {
				if counts[id] == 0 {
					order = append(order, id)
				}
				counts[id]++
			}
		}
		for _, id := range order {
			if counts[id] > 1 {
				details = append(details, fmt.Sprintf("%s appears %d times in %s", id, counts[id], pair.Name))
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("duplicate-ids", "warn", fmt.Sprintf("%d duplicate ID(s) found", len(details)), details, true)
	}
	return passCheck("duplicate-ids", "No duplicate IDs")
}

func doctorContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
func doctorNumber(value float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", value), "0"), ".")
}

func doctorReferencesCheck(issues []model.Issue) doctorCheck {
	ids := map[string]bool{}
	for _, issue := range issues {
		ids[issue.ID] = true
	}
	details := []string{}
	for _, issue := range issues {
		for _, ref := range issue.BlockedBy {
			if !ids[ref] {
				details = append(details, fmt.Sprintf("%s.blockedBy → %s (not found)", issue.ID, ref))
			}
		}
		for _, ref := range issue.Blocks {
			if !ids[ref] {
				details = append(details, fmt.Sprintf("%s.blocks → %s (not found)", issue.ID, ref))
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("referential-integrity", "warn", fmt.Sprintf("%d dangling dependency reference(s)", len(details)), details, true)
	}
	return passCheck("referential-integrity", "All dependency references are valid")
}

func doctorBidirectionalCheck(issues []model.Issue) doctorCheck {
	byID := map[string]model.Issue{}
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	details := []string{}
	for _, issue := range issues {
		for _, ref := range issue.BlockedBy {
			if target, ok := byID[ref]; ok && !doctorContains(target.Blocks, issue.ID) {
				details = append(details, fmt.Sprintf("%s.blockedBy has %s, but %s.blocks missing %s", issue.ID, ref, ref, issue.ID))
			}
		}
		for _, ref := range issue.Blocks {
			if target, ok := byID[ref]; ok && !doctorContains(target.BlockedBy, issue.ID) {
				details = append(details, fmt.Sprintf("%s.blocks has %s, but %s.blockedBy missing %s", issue.ID, ref, ref, issue.ID))
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("bidirectional-consistency", "warn", fmt.Sprintf("%d bidirectional mismatch(es)", len(details)), details, true)
	}
	return passCheck("bidirectional-consistency", "All dependency links are bidirectional")
}

func doctorCyclesCheck(issues []model.Issue) doctorCheck {
	graph := map[string][]string{}
	for _, issue := range issues {
		graph[issue.ID] = issue.BlockedBy
	}
	visited, inStack, cycles := map[string]bool{}, map[string]bool{}, [][]string{}
	var walk func(string, []string)
	walk = func(node string, path []string) {
		if inStack[node] {
			for i, candidate := range path {
				if candidate == node {
					cycles = append(cycles, append(append([]string(nil), path[i:]...), node))
					break
				}
			}
			return
		}
		if visited[node] {
			return
		}
		visited[node], inStack[node] = true, true
		for _, dep := range graph[node] {
			walk(dep, append(append([]string(nil), path...), node))
		}
		delete(inStack, node)
	}
	for _, issue := range issues {
		walk(issue.ID, nil)
	}
	if len(cycles) > 0 {
		details := make([]string, 0, len(cycles))
		for _, cycle := range cycles {
			details = append(details, strings.Join(cycle, " → "))
		}
		return issueCheck("circular-dependencies", "warn", fmt.Sprintf("%d circular dependency chain(s) found", len(cycles)), details, false)
	}
	return passCheck("circular-dependencies", "No circular dependencies")
}

func doctorLabelsCheck(lines []doctorLine) doctorCheck {
	details := []string{}
	for _, line := range lines {
		if line.ParseErr != nil {
			continue
		}
		issue := doctorObject(line.Parsed)
		labels, present := issue["labels"]
		if !present {
			continue
		}
		id := doctorID(issue, line.Number)
		items, ok := labels.([]any)
		if !ok {
			details = append(details, id+": labels is not an array")
			continue
		}
		for _, item := range items {
			label, ok := item.(string)
			if !ok {
				details = append(details, id+": label is not a string: "+doctorJSON(item))
			} else if strings.TrimSpace(label) == "" {
				details = append(details, id+": empty label string")
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("label-schema", "warn", fmt.Sprintf("%d label schema issue(s)", len(details)), details, true)
	}
	return passCheck("label-schema", "All label arrays are valid")
}

func doctorExtensionsCheck(lines []doctorLine) doctorCheck {
	details := []string{}
	for _, line := range lines {
		if line.ParseErr != nil {
			continue
		}
		issue := doctorObject(line.Parsed)
		ext, present := issue["extensions"]
		if !present {
			continue
		}
		if _, ok := ext.(map[string]any); !ok {
			details = append(details, fmt.Sprintf("%s: extensions must be a plain object: %s", doctorID(issue, line.Number), doctorJSON(ext)))
		}
	}
	if len(details) > 0 {
		return issueCheck("extensions-schema", "warn", fmt.Sprintf("%d malformed extensions field(s)", len(details)), details, true)
	}
	return passCheck("extensions-schema", "All extensions fields are valid")
}

func doctorClosedCheck(lines []doctorLine) doctorCheck {
	failures, warnings := []string{}, []string{}
	for _, line := range lines {
		if line.ParseErr != nil {
			continue
		}
		issue := doctorObject(line.Parsed)
		id := doctorID(issue, line.Number)
		status, statusOK := issue["status"].(string)
		closedAt := doctorTruthyString(issue["closedAt"])
		reason := doctorTruthyString(issue["closeReason"])
		if statusOK && status != "closed" && (closedAt || reason) {
			stale := []string{}
			if closedAt {
				stale = append(stale, "closedAt")
			}
			if reason {
				stale = append(stale, "closeReason")
			}
			failures = append(failures, fmt.Sprintf("%s: status=%s but %s still set", id, status, strings.Join(stale, "+")))
		} else if status == "closed" && !closedAt {
			warnings = append(warnings, id+": status=closed but closedAt missing")
		}
	}
	if len(failures) > 0 {
		return issueCheck("closed-fields-consistency", "fail", fmt.Sprintf("%d status/close-metadata mismatch(es)", len(failures)+len(warnings)), append(failures, warnings...), true)
	}
	if len(warnings) > 0 {
		return issueCheck("closed-fields-consistency", "warn", fmt.Sprintf("%d closed issue(s) missing closedAt", len(warnings)), warnings, true)
	}
	return passCheck("closed-fields-consistency", "All status/close-metadata pairs are consistent")
}

func doctorLocksCheck(dir string) doctorCheck {
	details := []string{}
	for _, file := range []string{model.IssuesFile, model.TemplatesFile} {
		lock := filepath.Join(dir, file+".lock")
		info, err := os.Stat(lock)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			details = append(details, file+".lock exists but cannot stat")
			continue
		}
		age := time.Since(info.ModTime())
		seconds := int(math.Round(age.Seconds()))
		if age > 30*time.Second {
			details = append(details, fmt.Sprintf("%s.lock is stale (%ds old)", file, seconds))
		} else {
			details = append(details, fmt.Sprintf("%s.lock exists (%ds old, may be active)", file, seconds))
		}
	}
	if len(details) > 0 {
		return issueCheck("stale-locks", "warn", fmt.Sprintf("%d lock file(s) found", len(details)), details, true)
	}
	return passCheck("stale-locks", "No stale lock files")
}

var doctorMergeLines = []string{
	".luuna/missions/issues.jsonl merge=union",
	".luuna/missions/templates.jsonl merge=union",
	".luuna/missions/plans.jsonl merge=union",
}

func doctorGitattributesCheck(dir string) doctorCheck {
	path := filepath.Join(config.ProjectRoot(dir), ".gitattributes")
	content, err := os.ReadFile(path)
	details := []string{}
	if errors.Is(err, os.ErrNotExist) {
		details = append(details, ".gitattributes file not found")
	} else if err == nil {
		for _, line := range doctorMergeLines {
			if !bytes.Contains(content, []byte(line)) {
				details = append(details, "Missing: "+line)
			}
		}
	}
	if len(details) > 0 {
		return issueCheck("gitattributes", "warn", "Missing merge=union gitattributes entries", details, true)
	}
	return passCheck("gitattributes", "Gitattributes configured correctly")
}

func doctorJSON(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }

func applyDoctorFixes(dir string, checks []doctorCheck) ([]string, error) {
	fixed := []string{}
	for _, check := range checks {
		if check.Status == "pass" || !check.Fixable {
			continue
		}
		var err error
		switch check.Name {
		case "jsonl-integrity":
			for _, name := range []string{model.IssuesFile, model.TemplatesFile} {
				path := filepath.Join(dir, name)
				err = store.WithLock(path, func() error {
					lines, err := doctorRawLines(path)
					if err != nil {
						return err
					}
					valid := []string{}
					for _, line := range lines {
						if line.ParseErr == nil {
							valid = append(valid, line.Text)
						}
					}
					if len(valid) == len(lines) {
						return nil
					}
					content := ""
					if len(valid) > 0 {
						content = strings.Join(valid, "\n") + "\n"
					}
					if err := doctorAtomicWrite(path, []byte(content)); err != nil {
						return err
					}
					fixed = append(fixed, fmt.Sprintf("Removed %d malformed line(s) from %s", len(lines)-len(valid), name))
					return nil
				})
				if err != nil {
					break
				}
			}
		case "duplicate-ids":
			err = doctorFixDuplicates(dir, &fixed)
		case "referential-integrity":
			err = doctorFixDangling(dir, &fixed)
		case "bidirectional-consistency":
			err = doctorFixBidirectional(dir, &fixed)
		case "stale-locks":
			doctorFixLocks(dir, &fixed)
		case "label-schema":
			err = doctorFixLabels(dir, &fixed)
		case "extensions-schema":
			err = doctorFixExtensions(dir, &fixed)
		case "closed-fields-consistency":
			err = doctorFixClosed(dir, &fixed)
		case "gitattributes":
			err = doctorFixGitattributes(dir, &fixed)
		}
		if err != nil {
			return nil, err
		}
	}
	return fixed, nil
}

func doctorAtomicWrite(path string, content []byte) error {
	previous, statErr := os.Stat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
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
	return os.Rename(tmp.Name(), path)
}

func doctorDedupObjects(lines []doctorLine) ([]map[string]any, int) {
	objects := []map[string]any{}
	index := map[string]int{}
	parsed := 0
	for _, line := range lines {
		if line.ParseErr != nil {
			continue
		}
		parsed++
		object := doctorObject(line.Parsed)
		id, ok := object["id"].(string)
		if !ok {
			continue
		}
		if at, exists := index[id]; exists {
			objects[at] = object
		} else {
			index[id] = len(objects)
			objects = append(objects, object)
		}
	}
	return objects, parsed
}

func doctorWriteObjects(path string, objects []map[string]any) error {
	var content bytes.Buffer
	for _, object := range objects {
		encoded, err := json.Marshal(object)
		if err != nil {
			return err
		}
		content.Write(encoded)
		content.WriteByte('\n')
	}
	return doctorAtomicWrite(path, content.Bytes())
}

func doctorFixDuplicates(dir string, fixed *[]string) error {
	for _, name := range []string{model.IssuesFile, model.TemplatesFile} {
		path := filepath.Join(dir, name)
		if err := store.WithLock(path, func() error {
			lines, err := doctorRawLines(path)
			if err != nil {
				return err
			}
			objects, parsed := doctorDedupObjects(lines)
			if len(objects) >= parsed {
				return nil
			}
			if err := doctorWriteObjects(path, objects); err != nil {
				return err
			}
			*fixed = append(*fixed, "Deduplicated "+name)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func doctorFixIssues(dir string, fixed *[]string, label string, mutate func([]map[string]any) bool) error {
	path := store.IssuesPath(dir)
	return store.WithLock(path, func() error {
		lines, err := doctorRawLines(path)
		if err != nil {
			return err
		}
		objects, _ := doctorDedupObjects(lines)
		if !mutate(objects) {
			return nil
		}
		if err := doctorWriteObjects(path, objects); err != nil {
			return err
		}
		*fixed = append(*fixed, label)
		return nil
	})
}

func doctorFixDangling(dir string, fixed *[]string) error {
	return doctorFixIssues(dir, fixed, "Removed dangling dependency references", func(objects []map[string]any) bool {
		ids := map[string]bool{}
		for _, issue := range objects {
			if id, ok := issue["id"].(string); ok {
				ids[id] = true
			}
		}
		changed := false
		for _, issue := range objects {
			for _, field := range []string{"blockedBy", "blocks"} {
				values, ok := issue[field].([]any)
				if !ok {
					continue
				}
				kept := []any{}
				for _, value := range values {
					if ref, ok := value.(string); ok && ids[ref] {
						kept = append(kept, ref)
					}
				}
				if len(kept) != len(values) {
					changed = true
				}
				if len(kept) > 0 {
					issue[field] = kept
				} else {
					delete(issue, field)
				}
			}
		}
		return changed
	})
}

func doctorArrayHas(values []any, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func doctorFixBidirectional(dir string, fixed *[]string) error {
	return doctorFixIssues(dir, fixed, "Added missing bidirectional dependency links", func(objects []map[string]any) bool {
		byID := map[string]map[string]any{}
		for _, issue := range objects {
			if id, ok := issue["id"].(string); ok {
				byID[id] = issue
			}
		}
		changed := false
		for _, issue := range objects {
			id, _ := issue["id"].(string)
			if refs, ok := issue["blockedBy"].([]any); ok {
				for _, value := range refs {
					ref, _ := value.(string)
					if target := byID[ref]; target != nil {
						back, _ := target["blocks"].([]any)
						if !doctorArrayHas(back, id) {
							target["blocks"] = append(back, id)
							changed = true
						}
					}
				}
			}
			if refs, ok := issue["blocks"].([]any); ok {
				for _, value := range refs {
					ref, _ := value.(string)
					if target := byID[ref]; target != nil {
						back, _ := target["blockedBy"].([]any)
						if !doctorArrayHas(back, id) {
							target["blockedBy"] = append(back, id)
							changed = true
						}
					}
				}
			}
		}
		return changed
	})
}

func doctorFixLocks(dir string, fixed *[]string) {
	for _, name := range []string{model.IssuesFile, model.TemplatesFile} {
		path := filepath.Join(dir, name+".lock")
		info, err := os.Stat(path)
		if err == nil && time.Since(info.ModTime()) > 30*time.Second {
			if store.ReclaimStaleLock(path, info) {
				*fixed = append(*fixed, "Removed stale "+name+".lock")
			}
		}
	}
}

func doctorFixLabels(dir string, fixed *[]string) error {
	return doctorFixIssues(dir, fixed, "Cleaned up invalid label entries", func(objects []map[string]any) bool {
		changed := false
		for _, issue := range objects {
			value, present := issue["labels"]
			if !present {
				continue
			}
			labels, ok := value.([]any)
			if !ok {
				delete(issue, "labels")
				changed = true
				continue
			}
			cleaned := []any{}
			for _, item := range labels {
				if label, ok := item.(string); ok && strings.TrimSpace(label) != "" {
					cleaned = append(cleaned, label)
				}
			}
			if len(cleaned) != len(labels) {
				if len(cleaned) > 0 {
					issue["labels"] = cleaned
				} else {
					delete(issue, "labels")
				}
				changed = true
			}
		}
		return changed
	})
}

func doctorFixExtensions(dir string, fixed *[]string) error {
	return doctorFixIssues(dir, fixed, "Dropped malformed extensions fields", func(objects []map[string]any) bool {
		changed := false
		for _, issue := range objects {
			value, present := issue["extensions"]
			if !present {
				continue
			}
			if _, ok := value.(map[string]any); !ok {
				delete(issue, "extensions")
				changed = true
			}
		}
		return changed
	})
}

func doctorFixClosed(dir string, fixed *[]string) error {
	return doctorFixIssues(dir, fixed, "Repaired status/close-metadata mismatches", func(objects []map[string]any) bool {
		changed := false
		for _, issue := range objects {
			status, _ := issue["status"].(string)
			if status != "closed" {
				if _, ok := issue["closedAt"]; ok {
					delete(issue, "closedAt")
					changed = true
				}
				if _, ok := issue["closeReason"]; ok {
					delete(issue, "closeReason")
					changed = true
				}
			} else if !doctorTruthyString(issue["closedAt"]) {
				if updatedAt, ok := issue["updatedAt"]; ok {
					issue["closedAt"] = updatedAt
				} else {
					delete(issue, "closedAt")
				}
				changed = true
			}
		}
		return changed
	})
}

func doctorFixGitattributes(dir string, fixed *[]string) error {
	path := filepath.Join(config.ProjectRoot(dir), ".gitattributes")
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte(strings.Join(doctorMergeLines, "\n")+"\n"), 0644); err != nil {
			return err
		}
		*fixed = append(*fixed, "Created .gitattributes with merge=union entries")
		return nil
	}
	if err != nil {
		return err
	}
	missing := []string{}
	for _, line := range doctorMergeLines {
		if !bytes.Contains(content, []byte(line)) {
			missing = append(missing, line)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	updated := string(content)
	if updated != "" && !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	updated += strings.Join(missing, "\n") + "\n"
	if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
		return err
	}
	*fixed = append(*fixed, "Added missing merge=union entries to .gitattributes")
	return nil
}
