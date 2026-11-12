package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luuna-ai/luuna-missions/internal/config"
)

func TestPrimeGoProjectVerificationGuidance(t *testing.T) {
	for _, compact := range []bool{false, true} {
		root := t.TempDir()
		if err := config.Init(root); err != nil {
			t.Fatal(err)
		}
		app := New()
		RegisterBuiltins(app)
		app.Cwd = root
		var out, errOut bytes.Buffer
		app.Stdout = &out
		app.Stderr = &errOut
		args := []string{"prime"}
		if compact {
			args = append(args, "--compact")
		}
		if code := app.Run(args); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		if strings.Contains(out.String(), "go test ./...") {
			t.Fatal("suggested Go tests for a project without a Go module")
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/project\n\ngo 1.22\n"), 0644); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		errOut.Reset()
		if code := app.Run(args); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "go test ./...") {
			t.Fatalf("missing native verification guidance: %s", out.String())
		}
	}
}
