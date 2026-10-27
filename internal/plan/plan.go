package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/config"
	"github.com/luuna-ai/luuna-missions/internal/model"
	"github.com/luuna-ai/luuna-missions/internal/store"
)

const topHelp = "Usage: luuna-missions plan [options] [command]\n\nPlan management\n\nOptions:\n  -h, --help                                 display help for command\n\nCommands:\n  templates [options]                        List available plan templates\n  prompt [options] <seed-id>                 Emit structured planning prompt JSON for a seed\n  submit [options] <seed-id>                 Validate a plan, spawn children, write plans.jsonl row\n  show [options] <id>                        Show a plan with sections, children, and status (accepts plan id or seed id)\n  validate [options] <id>                    Re-run validation against the current template definition (accepts plan id or seed id)\n  outcome [options] <id>                     Record a plan outcome (storage-only; not a state transition; accepts plan id or seed id)\n  review [options] <id>                      Record a reviewer (informational; not a state transition; accepts plan id or seed id)\n  edit [options] <id>                        Edit plan fields in place (accepts plan id or seed id); bumps revision\n  create [options] <seed-id>                 Create an adopt-only plan with zero spawned children (populate via 'luuna-missions plan adopt')\n  adopt [options] <plan-id> <seed-ids...>    Adopt existing open missions into a plan (link-only; bumps plan revision)\n  reorder [options] <plan-id> <seed-ids...>  Set the exact order of plan.children (must be a permutation of current children)\n  release [options] <plan-id> <seed-ids...>  Release missions from a plan (link-only; missions stay open; bumps plan revision)\n  list [options]                             List plans with optional filters\n"

func Register(app *cli.App) {
	app.Register(&cli.Command{Name: "plan", Description: "Plan management", Help: topHelp, Nested: true, Run: run})
	cli.SetPlanTransitionHook(ApplyTransitions)
}

func run(ctx *cli.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, err := io.WriteString(ctx.Stdout, topHelp)
		if len(args) == 0 {
			ctx.ExitCode = 1
		}
		return err
	}
	for _, arg := range args[1:] {
		if arg == "--help" || arg == "-h" {
			if help, ok := leafHelp[args[0]]; ok {
				_, err := io.WriteString(ctx.Stdout, help)
				return err
			}
		}
	}
	switch args[0] {
	case "templates":
		return runTemplates(ctx, args[1:])
	case "prompt":
		return runPrompt(ctx, args[1:])
	case "list":
		return runList(ctx, args[1:])
	case "show":
		return runShow(ctx, args[1:])
	case "validate":
		return runValidate(ctx, args[1:])
	case "create":
		return runCreate(ctx, args[1:])
	case "outcome":
		return runOutcome(ctx, args[1:])
	case "review":
		return runReview(ctx, args[1:])
	case "submit":
		return runSubmit(ctx, args[1:])
	case "edit":
		return runEdit(ctx, args[1:])
	case "adopt":
		return runAdopt(ctx, args[1:])
	case "reorder":
		return runReorder(ctx, args[1:])
	case "release":
		return runRelease(ctx, args[1:])
	default:
		return fmt.Errorf("error: unknown command '%s'", args[0])
	}
}

func parse(args []string, valueFlags, switches []string) (cli.Options, error) {
	switches = append(switches, "json")
	return cli.ParseOptions(args, valueFlags, switches)
}

func needArg(options cli.Options, usage string) (string, error) {
	if len(options.Positionals) == 0 {
		return "", fmt.Errorf("Usage: luuna-missions plan %s", usage)
	}
	return options.Positionals[0], nil
}

func findDir(ctx *cli.Context) (string, error) { return config.FindMissionsDir(ctx.Cwd) }

func runTemplates(ctx *cli.Context, args []string) error {
	opts, err := parse(args, nil, nil)
	if err != nil {
		return err
	}
	dir, err := findDir(ctx)
	if err != nil {
		return err
	}
	templates, err := config.LoadPlanTemplates(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	type entry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	entries := make([]entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, entry{name, templates[name].Description})
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success   bool    `json:"success"`
			Command   string  `json:"command"`
			Templates []entry `json:"templates"`
			Count     int     `json:"count"`
		}{true, "plan templates", entries, len(entries)})
	}
	if _, err := fmt.Fprintln(ctx.Stdout, "Available templates:"); err != nil {
		return err
	}
	for _, item := range entries {
		line := "  " + item.Name
		if item.Description != "" {
			line += "  " + item.Description
		}
		if _, err := fmt.Fprintln(ctx.Stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func resolvePlanID(arg, dir string) (string, error) {
	if strings.HasPrefix(arg, "pl-") {
		return arg, nil
	}
	issues, err := store.ReadIssues(dir)
	if err != nil {
		return "", err
	}
	for _, issue := range issues {
		if issue.ID == arg {
			if issue.PlanID == "" {
				return "", fmt.Errorf("Seed %s has no plan. Submit one with 'luuna-missions plan submit %s --plan <file>'.", arg, arg)
			}
			return issue.PlanID, nil
		}
	}
	return "", planNotFound(arg)
}

func planNotFound(id string) error {
	return fmt.Errorf("Plan not found: %s. Run 'luuna-missions plan list' to see available plans.", id)
}

func lookupPlan(plans []model.Plan, id string) (*model.Plan, error) {
	for index := range plans {
		if plans[index].ID == id {
			return &plans[index], nil
		}
	}
	return nil, planNotFound(id)
}

func childSummaries(plan model.Plan, issues []model.Issue) []any {
	out := make([]any, 0, len(plan.Children))
	adopted := make(map[string]bool, len(plan.AdoptedChildren))
	for _, id := range plan.AdoptedChildren {
		adopted[id] = true
	}
	for _, id := range plan.Children {
		title, status := "(missing)", "missing"
		for _, issue := range issues {
			if issue.ID == id {
				title, status = issue.Title, issue.Status
				break
			}
		}
		out = append(out, struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Status  string `json:"status"`
			Adopted bool   `json:"adopted"`
		}{id, title, status, adopted[id]})
	}
	return out
}

func jsonPretty(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func parsePositiveInt(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("expected a positive integer, got %q", raw)
	}
	return value, nil
}
