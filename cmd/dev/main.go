// Command dev provides repository development tasks using the Go toolchain.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func run(name string, args ...string) {
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/dev verify|build")
		os.Exit(2)
	}
	goBin := os.Getenv("LUUNA_MISSIONS_GO")
	if goBin == "" {
		goBin = "go"
	}
	switch os.Args[1] {
	case "verify":
		c := exec.Command("gofmt", "-l", "cmd", "internal")
		output, err := c.CombinedOutput()
		if err != nil || len(output) != 0 {
			fmt.Fprintf(os.Stderr, "Go formatting check failed: %s %v\n", output, err)
			os.Exit(1)
		}
		run(goBin, "vet", "./...")
		run(goBin, "test", "-count=1", "-timeout=5m", "./...")
		build(goBin)
	case "build":
		build(goBin)
	default:
		fmt.Fprintln(os.Stderr, "Unknown task:", os.Args[1])
		os.Exit(2)
	}
}
func build(goBin string) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	if err := os.MkdirAll("dist", 0755); err != nil {
		panic(err)
	}
	target := filepath.Join("dist", "luuna-missions"+ext)
	run(goBin, "build", "-buildvcs=false", "-trimpath", "-o", target, "./cmd/luuna-missions")
	data, err := os.ReadFile(target)
	if err != nil {
		panic(err)
	}
	for _, alias := range []string{"lms", "lm", "missions"} {
		if err := os.WriteFile(filepath.Join("dist", alias+ext), data, 0755); err != nil {
			panic(err)
		}
	}
	fmt.Println("Built native executables in dist/")
}
