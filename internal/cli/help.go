package cli

import (
	"embed"
	"strings"
)

// Command help is generated from the command registry. The
// snapshots keep option names, descriptions, line wrapping, and nested usage
// stable while the Go handlers adopt the same public surface.
//
//go:embed help/*.txt
var commandHelp embed.FS

func helpSnapshot(args []string) (string, bool) {
	parts := []string{}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		parts = append(parts, arg)
		if len(parts) == 3 {
			break
		}
	}
	for len(parts) > 0 {
		path := "help/" + strings.Join(parts, "__") + ".txt"
		content, err := commandHelp.ReadFile(path)
		if err == nil {
			return string(content), true
		}
		parts = parts[:len(parts)-1]
	}
	return "", false
}
