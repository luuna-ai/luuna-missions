//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCompletionsClosedStdoutExitsZero(t *testing.T) {
	goBinary := os.Getenv("LUUNA_MISSIONS_GO")
	if goBinary == "" {
		goBinary = "go"
	}
	binary := filepath.Join(t.TempDir(), "luuna-missions")
	build := exec.Command(goBinary, "build", "-buildvcs=false", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native CLI: %v\n%s", err, output)
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := readEnd.Close(); err != nil {
		t.Fatal(err)
	}
	defer writeEnd.Close()
	command := exec.Command(binary, "completions", "bash")
	command.Stdout = writeEnd
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("closed stdout should exit 0, got %v; stderr=%q", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}
