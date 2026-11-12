package cli

// RegisterBuiltins installs the public command order and Go handlers.
// Domain packages replace their placeholder entry through Register. The
// public command inventory is implemented by the native handlers.
func RegisterBuiltins(app *App) {
	for _, command := range []*Command{
		{Name: "init", Description: "Initialize .luuna/missions/ in current directory", Run: runInit},
		{Name: "create", Description: "Create a new issue", Run: runCreate},
		{Name: "show", Arguments: "<id> [ids]", Description: "Show one or more issues", Run: runShow},
		{Name: "list", Description: "List issues with filters", Run: runList},
		{Name: "ready", Description: "Show open issues with no unresolved blockers", Run: runReady},
		{Name: "search", Arguments: "<query>", Description: "Full-text search title + description", Run: runSearch},
		{Name: "update", Arguments: "<id>", Description: "Update issue fields", Run: runUpdate},
		{Name: "close", Arguments: "<id> [ids]", Description: "Close one or more issues", Run: runClose},
		{Name: "dep", Description: "Manage issue dependencies", Nested: true, Run: runDep},
		{Name: "label", Description: "Manage issue labels", Nested: true, Run: runLabel},
		{Name: "blocked", Description: "Show all blocked issues", Run: runBlocked},
		{Name: "stats", Description: "Project statistics", Run: runStats},
		{Name: "sync", Description: "Stage and commit .luuna/missions/ changes", Run: runSync},
		{Name: "doctor", Description: "Check project health and data integrity", Run: runDoctor},
		{Name: "tpl", Description: "Manage issue templates (molecules)", Nested: true, Run: runTpl},
		{Name: "migrate-from-beads", Description: "Migrate issues from beads issue tracker", Run: runMigrateBeads},
		{Name: "prime", Description: "Output AI agent context", Run: runPrime},
		{Name: "onboard", Description: "Add missions section to CLAUDE.md / AGENTS.md", Run: runOnboard},
		{Name: "upgrade", Description: "Update Luuna Missions through a reviewed checkout", Run: runUpgrade},
		{Name: "completions", Arguments: "<shell>", Description: "Output shell completion script", Run: runCompletions},
		{Name: "block", Arguments: "<id>", Description: "Add a blocker to an issue", Run: runBlock},
		{Name: "unblock", Arguments: "<id>", Description: "Remove blockers from an issue", Run: runUnblock},
		{Name: "plan", Description: "Plan management", Nested: true},
		{Name: "config", Description: "Read, write, and inspect .luuna/missions/config.yaml", Nested: true, Run: runConfig},
		{Name: "setup", Arguments: "<target>", Description: "Install Claude Code hooks: prime on session start, guard .luuna/missions/*.jsonl edits", Nested: true, Run: runSetup},
		{Name: "guard", Description: "PreToolUse hook: deny hand edits to .luuna/missions/*.jsonl (reads hook JSON on stdin)", Run: runGuard},
	} {
		app.Register(command)
	}
}
