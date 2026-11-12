package cli

import (
	_ "embed"
	"fmt"
)

// Generated from the command registry; include nested command
// names and flag descriptions so shell scripts keep their existing contract.
//
//go:embed completion_bash.txt
var completionBash []byte

//go:embed completion_zsh.txt
var completionZsh []byte

//go:embed completion_fish.txt
var completionFish []byte

func runCompletions(ctx *Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("error: missing required argument 'shell'")
	}
	if len(args) > 1 {
		return fmt.Errorf("error: too many arguments for 'completions'. Expected 1 argument but got %d.", len(args))
	}
	var script []byte
	switch args[0] {
	case "bash":
		script = completionBash
	case "zsh":
		script = completionZsh
	case "fish":
		script = completionFish
	default:
		ctx.ExitCode = 1
		_, err := fmt.Fprintf(ctx.Stderr, "✗ Unknown shell: %s. Supported: bash, zsh, fish\n", args[0])
		return err
	}
	_, err := ctx.Stdout.Write(script)
	return err
}
