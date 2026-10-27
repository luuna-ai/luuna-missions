package main

import (
	"strings"
	"testing"
)

func TestReadNativeArgsPreservesArgumentBoundaries(t *testing.T) {
	got, err := readNativeArgs(strings.NewReader(`["create","--title","two words","--description","雪\nnew line","--json"]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "--title", "two words", "--description", "雪\nnew line", "--json"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argument %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReadNativeArgsRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"object", `{"args":["list"]}`},
		{"non-string", `["list",3]`},
		{"empty", `[]`},
		{"trailing", `["list"] false`},
		{"nul", `["a\u0000b"]`},
		{"argument too large", `[` + `"` + strings.Repeat("x", maxNativeArgBytes+1) + `"]`},
		{"too many arguments", `[` + strings.TrimSuffix(strings.Repeat(`"x",`, maxNativeArgCount+1), ",") + `]`},
		{"request too large", `[` + `"` + strings.Repeat("x", maxNativeArgsBytes) + `"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := readNativeArgs(strings.NewReader(tt.input)); err == nil {
				t.Fatal("expected invalid request to be rejected")
			}
		})
	}
}
