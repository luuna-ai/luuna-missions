package plan

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

// Atlas is an optional best-effort source for prior art. Failed subprocesses
// leave sections empty and never prevent local plan creation.
type atlasEntry struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Summary   string  `json:"summary"`
	Relevance float64 `json:"relevance"`
}

func inferAtlasDomain(seed model.Issue, explicit, cwd string) string {
	if explicit != "" {
		return explicit
	}
	binary, err := exec.LookPath("luuna-atlas")
	if err != nil {
		return ""
	}
	cmd := exec.Command(binary, "--json", "status")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	var payload struct {
		Domains []struct {
			Domain string `json:"domain"`
		} `json:"domains"`
	}
	if json.Unmarshal(output, &payload) != nil {
		return ""
	}
	domains := map[string]bool{}
	for _, item := range payload.Domains {
		domains[item.Domain] = true
	}
	for _, label := range seed.Labels {
		if domains[label] {
			return label
		}
	}
	refs := pathReferences(seed.Description)
	if len(refs) == 0 {
		return ""
	}
	diff := exec.Command("git", "diff", "--name-only", "HEAD")
	diff.Dir = cwd
	if out, err := diff.Output(); err == nil {
		changed := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			changed[strings.TrimSpace(line)] = true
		}
		intersection := []string{}
		for _, ref := range refs {
			if changed[ref] {
				intersection = append(intersection, ref)
			}
		}
		if len(intersection) > 0 {
			refs = intersection
		}
	}
	for _, ref := range refs {
		for _, segment := range strings.Split(ref, "/") {
			if domains[segment] {
				return segment
			}
		}
	}
	return ""
}

var atlasPathRegex = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+\.[A-Za-z0-9]+`)

func pathReferences(description string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, match := range atlasPathRegex.FindAllString(description, -1) {
		ref := strings.TrimRight(match, ".,;:)]")
		if !seen[ref] {
			out = append(out, ref)
			seen[ref] = true
		}
	}
	return out
}

func priorArtForSection(domain, name, hint, cwd string) []any {
	entries := []any{}
	if domain == "" {
		return entries
	}
	binary, err := exec.LookPath("luuna-atlas")
	if err != nil {
		return entries
	}
	types := []string{}
	if hint != "" {
		types = []string{hint}
	} else {
		switch name {
		case "approach":
			types = []string{"pattern", "decision"}
		case "risks":
			types = []string{"failure"}
		case "acceptance":
			types = []string{"guide"}
		}
	}
	for _, kind := range types {
		cmd := exec.Command(binary, "--json", "query", domain, "--type", kind)
		cmd.Dir = cwd
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		var payload struct {
			Domains []struct {
				Records []map[string]any `json:"records"`
			} `json:"domains"`
		}
		if json.Unmarshal(output, &payload) != nil {
			continue
		}
		records := []map[string]any{}
		for _, entry := range payload.Domains {
			records = append(records, entry.Records...)
		}
		if len(records) > 5 {
			records = records[:5]
		}
		for index, record := range records {
			id, _ := record["id"].(string)
			if id == "" {
				continue
			}
			recordType, _ := record["type"].(string)
			if recordType == "" {
				recordType = kind
			}
			name, _ := record["name"].(string)
			description, _ := record["description"].(string)
			summary := description
			if name != "" {
				summary = name
				if description != "" {
					summary = name + ": " + description
				}
			}
			if len([]rune(summary)) > 240 {
				runes := []rune(summary)
				summary = strings.TrimRightFunc(string(runes[:239]), func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' }) + "…"
			}
			entries = append(entries, atlasEntry{id, recordType, summary, float64(5-index) / 5})
		}
	}
	if len(entries) > 5 {
		return entries[:5]
	}
	return entries
}

func inheritedTemplate(seed model.Issue, dir string) (string, error) {
	if seed.PlanStepIndex == nil {
		return "", nil
	}
	plans, err := store.ReadPlans(dir)
	if err != nil {
		return "", err
	}
	var parent *model.Plan
	if seed.PlanID != "" {
		for i := range plans {
			if plans[i].ID == seed.PlanID {
				parent = &plans[i]
				break
			}
		}
	}
	if parent == nil {
		for i := range plans {
			if containsString(plans[i].Children, seed.ID) {
				parent = &plans[i]
				break
			}
		}
	}
	if parent == nil {
		return "", nil
	}
	steps, ok := parent.Sections["steps"].([]any)
	if !ok || *seed.PlanStepIndex < 0 || *seed.PlanStepIndex >= len(steps) {
		return "", nil
	}
	step, ok := steps[*seed.PlanStepIndex].(map[string]any)
	if !ok {
		return "", nil
	}
	name, _ := step["plan_template"].(string)
	return name, nil
}
