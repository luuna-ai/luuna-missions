package main

import (
	"os"

	"github.com/luuna-ai/luuna-missions/internal/cli"
	"github.com/luuna-ai/luuna-missions/internal/plan"
)

func main() {
	ignoreBrokenPipeSignal()
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--luuna-args-stdin" {
		var err error
		args, err = readNativeArgs(os.Stdin)
		if err != nil {
			_, _ = os.Stderr.WriteString("error: invalid native argument request\n")
			os.Exit(2)
		}
	}
	app := cli.New()
	cli.RegisterBuiltins(app)
	plan.Register(app)
	os.Exit(app.Run(args))
}
