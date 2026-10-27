package cli

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/config"
)

func gitCapture(root string, args ...string) (string, string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Dir = root
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func runSync(ctx *Context, args []string) error {
	opts, err := ParseOptions(args, nil, []string{"status", "dry-run", "json"})
	if err != nil {
		return err
	}
	if len(opts.Positionals) > 0 {
		return fmt.Errorf("too many arguments for 'sync'")
	}
	dir, err := config.FindDir(ctx.Cwd)
	if err != nil {
		return err
	}
	root := config.ProjectRoot(dir)
	if config.IsInsideWorktree(ctx.Cwd) {
		message := "Inside a git worktree — skipping commit. Issues are stored in the main repo."
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success   bool   `json:"success"`
				Command   string `json:"command"`
				Committed bool   `json:"committed"`
				Worktree  bool   `json:"worktree"`
				Message   string `json:"message"`
			}{true, "sync", false, true, message})
		}
		return ctx.Warning(message)
	}
	stdout, stderr, err := gitCapture(root, "status", "--porcelain", ".luuna/missions/")
	if err != nil {
		return fmt.Errorf("git status failed: %s", strings.TrimSpace(stderr))
	}
	changed := strings.TrimSpace(stdout)
	if opts.Has("status") {
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success    bool   `json:"success"`
				Command    string `json:"command"`
				HasChanges bool   `json:"hasChanges"`
				Changes    string `json:"changes"`
			}{true, "sync", changed != "", changed})
		}
		if changed != "" {
			if err := ctx.Success("Uncommitted .luuna/missions/ changes:"); err != nil {
				return err
			}
			_, err = fmt.Fprintln(ctx.Stdout, changed)
			return err
		}
		return ctx.Success("No uncommitted .luuna/missions/ changes.")
	}
	if changed == "" {
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success   bool   `json:"success"`
				Command   string `json:"command"`
				Committed bool   `json:"committed"`
				Message   string `json:"message"`
			}{true, "sync", false, "Nothing to commit"})
		}
		return ctx.Success("No changes to commit.")
	}
	message := "missions: sync " + time.Now().UTC().Format("2006-01-02")
	if opts.Has("dry-run") {
		if opts.Has("json") {
			return ctx.JSON(struct {
				Success     bool   `json:"success"`
				Command     string `json:"command"`
				DryRun      bool   `json:"dryRun"`
				WouldCommit bool   `json:"wouldCommit"`
				Message     string `json:"message"`
				Changes     string `json:"changes"`
			}{true, "sync", true, true, message, changed})
		}
		if err := ctx.Success("Dry run — would commit:"); err != nil {
			return err
		}
		_, err = fmt.Fprintf(ctx.Stdout, "%s\nCommit message: %s\n", changed, message)
		return err
	}
	_, stderr, err = gitCapture(root, "add", ".luuna/missions/")
	if err != nil {
		return fmt.Errorf("git add failed: %s", stderr)
	}
	// Preserve the existing sync contract: commit the full index after staging
	// Missions files, including any other paths the caller already staged.
	_, stderr, err = gitCapture(root, "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("git commit failed: %s", stderr)
	}
	if opts.Has("json") {
		return ctx.JSON(struct {
			Success   bool   `json:"success"`
			Command   string `json:"command"`
			Committed bool   `json:"committed"`
			Message   string `json:"message"`
		}{true, "sync", true, message})
	}
	return ctx.Success("Committed: " + message)
}
