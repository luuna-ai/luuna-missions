package cli

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

// These templates are generated from the TypeScript `prime --export --json`
// oracle in a project with and without a package.json verify script. Keeping
// the content here preserves the public session context while Go takes over.
//
//go:embed prime_full.json
var primeFull []byte

//go:embed prime_compact.json
var primeCompact []byte

//go:embed prime_full_verify.json
var primeFullVerify []byte

//go:embed prime_compact_verify.json
var primeCompactVerify []byte

type primeFixture struct {
	Sections any    `json:"sections"`
	Content  string `json:"content"`
}
type primeRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Assignee string `json:"assignee,omitempty"`
}
type primeState struct {
	InProgress []primeRef `json:"inProgress"`
	Ready      []primeRef `json:"ready"`
	ReadyCount int        `json:"readyCount"`
}

func primeReference(issue model.Issue) primeRef {
	return primeRef{ID: issue.ID, Title: issue.Title, Priority: issue.Priority, Assignee: issue.Assignee}
}

func projectHasVerify(root string) bool {
	content, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if json.Unmarshal(content, &pkg) != nil {
		return false
	}
	_, ok := pkg.Scripts["verify"].(string)
	return ok
}

func primeTemplate(compact, verify bool) (primeFixture, error) {
	data := primeFull
	if compact {
		data = primeCompact
	}
	if verify {
		if compact {
			data = primeCompactVerify
		} else {
			data = primeFullVerify
		}
	}
	var fixture primeFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return primeFixture{}, err
	}
	return fixture, nil
}

func loadPrimeState(dir string) (*primeState, error) {
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return nil, err
	}
	plans, err := loadPlanIndex(dir)
	if err != nil {
		return nil, err
	}
	closed := closedIDs(issues)
	inProgress, ready := []model.Issue{}, []model.Issue{}
	for _, issue := range issues {
		if issue.Status == "in_progress" {
			inProgress = append(inProgress, issue)
		}
		if issue.Status != "open" {
			continue
		}
		if issue.RequiresPlan {
			plan, found := plans.bySeed[issue.ID]
			if !found || plan.Status == "draft" {
				continue
			}
		} else if plan, found := plans.forIssue(issue); found && plan.Status == "draft" {
			ready = append(ready, issue)
			continue
		}
		if !effectivelyBlocked(issue, closed) {
			ready = append(ready, issue)
		}
	}
	if err = sortIssues(inProgress, "priority"); err != nil {
		return nil, err
	}
	if err = sortIssues(ready, "priority"); err != nil {
		return nil, err
	}
	state := &primeState{InProgress: []primeRef{}, Ready: []primeRef{}, ReadyCount: len(ready)}
	for _, issue := range inProgress {
		state.InProgress = append(state.InProgress, primeReference(issue))
	}
	for i, issue := range ready {
		if i == 5 {
			break
		}
		state.Ready = append(state.Ready, primeReference(issue))
	}
	return state, nil
}

func renderPrimeState(state *primeState) string {
	lines := []string{"## Current State", ""}
	if len(state.InProgress) > 0 {
		lines = append(lines, "In progress:")
		for _, issue := range state.InProgress {
			lines = append(lines, primeRefLine(issue))
		}
		lines = append(lines, "")
	}
	if state.ReadyCount == 0 {
		lines = append(lines, "Ready: none")
	} else {
		more := ""
		if state.ReadyCount > len(state.Ready) {
			more = fmt.Sprintf(" (top %d of %d)", len(state.Ready), state.ReadyCount)
		}
		lines = append(lines, "Ready"+more+":")
		for _, issue := range state.Ready {
			lines = append(lines, primeRefLine(issue))
		}
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}
func primeRefLine(issue primeRef) string {
	who := ""
	if issue.Assignee != "" {
		who = " (@" + issue.Assignee + ")"
	}
	return fmt.Sprintf("- %s P%d %s%s", issue.ID, issue.Priority, issue.Title, who)
}
func primeWithState(content string, state *primeState) string {
	if state == nil {
		return content
	}
	separator := "\n\n"
	if content == "" || strings.HasSuffix(content, "\n\n") {
		separator = ""
	} else if strings.HasSuffix(content, "\n") {
		separator = "\n"
	}
	return content + separator + renderPrimeState(state)
}

func runPrime(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"compact", "export", "json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'prime'")
	}
	dir, findErr := config.FindDir(ctx.Cwd)
	root := ctx.Cwd
	if findErr == nil {
		root = config.ProjectRoot(dir)
	}
	fixture, err := primeTemplate(opts.Has("compact"), projectHasVerify(root))
	if err != nil {
		return err
	}
	if opts.Has("export") {
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success  bool   `json:"success"`
				Command  string `json:"command"`
				Sections any    `json:"sections"`
				Content  string `json:"content"`
			}{true, "prime", fixture.Sections, fixture.Content})
		}
		_, err = fmt.Fprint(ctx.Stdout, fixture.Content)
		return err
	}
	sections := fixture.Sections
	content := fixture.Content
	var state *primeState
	if findErr == nil {
		customPath := filepath.Join(dir, "PRIME.md")
		if custom, err := os.ReadFile(customPath); err == nil {
			sections = nil
			content = string(custom)
		}
		state, _ = loadPrimeState(dir)
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success  bool        `json:"success"`
			Command  string      `json:"command"`
			Sections any         `json:"sections"`
			State    *primeState `json:"state"`
			Content  string      `json:"content"`
		}{true, "prime", sections, state, content})
	}
	_, err = fmt.Fprint(ctx.Stdout, primeWithState(content, state))
	return err
}
