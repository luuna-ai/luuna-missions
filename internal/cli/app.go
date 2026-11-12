package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Handler implements one top-level CLI command. It receives all arguments
// following the command name, including command-specific options.
type Handler func(*Context, []string) error

type Command struct {
	Name        string
	Arguments   string
	Description string
	Help        string
	Nested      bool
	Run         Handler
}

type App struct {
	Commands []*Command
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Cwd      string
	Version  string
}

type Context struct {
	App      *App
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Cwd      string
	Quiet    bool
	Verbose  bool
	Timing   bool
	Command  string
	ExitCode int
}

// CurrentVersion is the native compatibility version; release builds may override it with -ldflags.
var CurrentVersion = "0.6.0"

func New() *App {
	cwd, _ := os.Getwd()
	return &App{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Cwd: cwd, Version: CurrentVersion,
	}
}

// Register replaces an existing command without moving it in the help order.
// This allows a domain package to register its complete implementation after
// the public command inventory is installed by RegisterBuiltins.
func (a *App) Register(command *Command) {
	for i, existing := range a.Commands {
		if existing.Name == command.Name {
			if command.Description == "" {
				command.Description = existing.Description
			}
			if command.Arguments == "" {
				command.Arguments = existing.Arguments
			}
			if !command.Nested {
				command.Nested = existing.Nested
			}
			a.Commands[i] = command
			return
		}
	}
	a.Commands = append(a.Commands, command)
}

func (a *App) find(name string) *Command {
	for _, command := range a.Commands {
		if command.Name == name {
			return command
		}
	}
	return nil
}

func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func (a *App) context(command string, args []string) *Context {
	return &Context{
		App:   a,
		Stdin: a.Stdin, Stdout: a.Stdout, Stderr: a.Stderr, Cwd: a.Cwd,
		Quiet:   contains(args, "--quiet") || contains(args, "-q"),
		Verbose: contains(args, "--verbose"), Timing: contains(args, "--timing"),
		Command: command,
	}
}

// JSON writes one indented JSON value and a newline, matching outputJson in
// the original CLI. HTML escaping is disabled to preserve public JSON output.
func (c *Context) JSON(value any) error {
	encoder := json.NewEncoder(c.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (c *Context) Success(message string) error {
	if c.Quiet {
		return nil
	}
	_, err := fmt.Fprintf(c.Stdout, "✓ %s\n", message)
	return err
}

func (c *Context) Warning(message string) error {
	if c.Quiet {
		return nil
	}
	_, err := fmt.Fprintf(c.Stderr, "! %s\n", message)
	return err
}

// Run is intentionally separate from os.Exit, so command parity can be
// exercised in process as well as through the compiled executable.
func (a *App) Run(args []string) int {
	rawArgs := args
	jsonMode := contains(args, "--json")
	if (contains(args, "--version") || contains(args, "-v")) && jsonMode {
		ctx := a.context("version", args)
		if err := ctx.JSON(struct {
			Success  bool   `json:"success"`
			Command  string `json:"command"`
			Name     string `json:"name"`
			Version  string `json:"version"`
			Runtime  string `json:"runtime"`
			Platform string `json:"platform"`
		}{true, "version", "@luuna/missions-cli", a.Version, "go", goPlatform()}); err != nil {
			return a.outputError(err)
		}
		return 0
	}
	for len(args) > 0 {
		switch args[0] {
		case "-q", "--quiet", "--verbose", "--timing":
			args = args[1:]
		default:
			goto dispatched
		}
	}
dispatched:

	if len(args) == 0 {
		_, _ = io.WriteString(a.Stderr, a.Help())
		return 1
	}
	if (contains(args, "--version") || contains(args, "-v")) &&
		(strings.HasPrefix(args[0], "-") || a.find(args[0]) != nil) {
		_, err := fmt.Fprintln(a.Stdout, a.Version)
		if err != nil {
			return a.outputError(err)
		}
		return 0
	}
	if args[0] == "--help" || args[0] == "-h" ||
		(strings.HasPrefix(args[0], "-") && (contains(args, "--help") || contains(args, "-h"))) {
		_, err := io.WriteString(a.Stdout, a.Help())
		if err != nil {
			return a.outputError(err)
		}
		return 0
	}
	if strings.HasPrefix(args[0], "-") {
		_, err := ParseOptions([]string{args[0]}, nil, nil)
		if err != nil {
			fmt.Fprintf(a.Stderr, "error: %s\n", err)
			return 1
		}
	}

	name := args[0]
	command := a.find(name)
	if command == nil {
		suggestion, message := suggest(name, a.Commands)
		if jsonMode {
			payload := struct {
				Success    bool   `json:"success"`
				Command    string `json:"command"`
				Error      string `json:"error"`
				Suggestion string `json:"suggestion,omitempty"`
			}{false, name, message, suggestion}
			_ = a.context(name, args).JSON(payload)
		} else {
			fmt.Fprintln(a.Stderr, message)
		}
		return 1
	}
	if contains(args[1:], "--help") || contains(args[1:], "-h") {
		if snapshot, found := helpSnapshot(args); found {
			_, _ = io.WriteString(a.Stdout, snapshot)
			return 0
		}
		if command.Help != "" {
			_, _ = io.WriteString(a.Stdout, command.Help)
		} else {
			_, _ = fmt.Fprintf(a.Stdout, "Usage: luuna-missions %s %s\n\n%s\n", command.Name, command.Arguments, command.Description)
		}
		return 0
	}
	ctx := a.context(name, rawArgs)
	start := time.Now()
	if command.Run == nil {
		return a.commandError(name, jsonMode, errors.New("command implementation unavailable"))
	}
	err := command.Run(ctx, args[1:])
	if ctx.Timing && err == nil {
		elapsed := time.Since(start)
		value := fmt.Sprintf("%.0fms", math.Round(elapsed.Seconds()*1000))
		if elapsed >= time.Second {
			value = fmt.Sprintf("%.2fs", elapsed.Seconds())
		}
		fmt.Fprintf(a.Stderr, "⏱ %s\n", value)
	}
	if err != nil {
		if errors.Is(err, syscall.EPIPE) {
			return 0
		}
		var optionErr *unknownOptionError
		if errors.As(err, &optionErr) {
			fmt.Fprintf(a.Stderr, "error: %s\n", optionErr)
			return 1
		}
		return a.commandError(name, jsonMode, err)
	}
	return ctx.ExitCode
}

func (a *App) outputError(err error) int {
	if errors.Is(err, syscall.EPIPE) {
		return 0
	}
	logDebugError(a.Stdout, "", err)
	fmt.Fprintf(a.Stderr, "Error: %s\n", err)
	return 1
}

func (a *App) commandError(name string, jsonMode bool, err error) int {
	logDebugError(a.Stdout, name, err)
	if jsonMode {
		_ = a.context(name, nil).JSON(struct {
			Success bool   `json:"success"`
			Command string `json:"command"`
			Error   string `json:"error"`
		}{false, name, err.Error()})
		return 1
	}
	fmt.Fprintf(a.Stderr, "Error: %s\n", err)
	return 1
}

func (a *App) Help() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Luuna Missions v%s — Git-native issue tracking\n\n", a.Version)
	b.WriteString("Usage: luuna-missions <command> [options]\n\nCommands:\n")
	for _, command := range a.Commands {
		entry := command.Name
		if command.Arguments != "" {
			entry += " " + command.Arguments
		}
		padding := 18 - len([]rune(entry))
		if padding < 2 {
			padding = 2
		}
		fmt.Fprintf(&b, "  %s%s%s\n", entry, strings.Repeat(" ", padding), command.Description)
	}
	b.WriteString("\nOptions:\n")
	for _, option := range [][2]string{
		{"-h, --help", "Show help"},
		{"-v, --version", "Print version"},
		{"--format <mode>", "Output format (markdown|compact|plain|ids|json)"},
		{"--json", "Alias for --format json"},
		{"-q, --quiet", "Suppress non-error output"},
		{"--verbose", "Extra diagnostic output"},
		{"--timing", "Show command execution time"},
	} {
		padding := 18 - len([]rune(option[0]))
		if padding < 2 {
			padding = 2
		}
		fmt.Fprintf(&b, "  %s%s%s\n", option[0], strings.Repeat(" ", padding), option[1])
	}
	b.WriteString("\nRun 'luuna-missions <command> --help' for command-specific help.\n")
	return b.String()
}

func suggest(input string, commands []*Command) (string, string) {
	best, distance := "", int(^uint(0)>>1)
	for _, command := range commands {
		if d := levenshtein(input, command.Name); d < distance {
			best, distance = command.Name, d
		}
	}
	if distance <= 2 {
		return best, fmt.Sprintf("Unknown command: %s. Did you mean %s?", input, best)
	}
	return "", "Unknown command: " + input
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, left := range ar {
		curr := make([]int, len(br)+1)
		curr[0] = i + 1
		for j, right := range br {
			cost := 0
			if left != right {
				cost = 1
			}
			curr[j+1] = min(curr[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev = curr
	}
	return prev[len(br)]
}

// The version JSON platform remains the established spelling for compatibility.
func goPlatform() string {
	osName := runtime.GOOS
	if osName == "windows" {
		osName = "win32"
	}
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return osName + "-" + arch
}
