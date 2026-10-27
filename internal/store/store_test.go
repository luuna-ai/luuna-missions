package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

func TestReadUsesLastIDAndSkipsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	content := `{"id":"a","title":"first","status":"open","type":"task","priority":2,"createdAt":"a","updatedAt":"a"}` + "\n" +
		"not json\n" +
		`{"id":"b","title":"second","status":"open","type":"task","priority":2,"createdAt":"a","updatedAt":"a"}` + "\n" +
		`{"id":"a","title":"last","status":"open","type":"task","priority":2,"createdAt":"a","updatedAt":"a"}` + "\n"
	if err := os.WriteFile(IssuesPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	issues, err := ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].ID != "a" || issues[0].Title != "last" || issues[1].ID != "b" {
		t.Fatalf("unexpected deduplication: %#v", issues)
	}
}

func TestAtomicReplaceOverExistingFileAndAppend(t *testing.T) {
	dir := t.TempDir()
	item := model.Issue{ID: "a", Title: "first", Status: "open", Type: "task", Priority: 2, CreatedAt: "a", UpdatedAt: "a"}
	if err := WriteIssues(dir, []model.Issue{item}); err != nil {
		t.Fatal(err)
	}
	item.Title = "replacement"
	if err := WriteIssues(dir, []model.Issue{item}); err != nil {
		t.Fatalf("same-directory replacement failed: %v", err)
	}
	if err := AppendIssue(dir, model.Issue{ID: "b", Title: "added", Status: "open", Type: "task", Priority: 2, CreatedAt: "a", UpdatedAt: "a"}); err != nil {
		t.Fatal(err)
	}
	issues, err := ReadIssues(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0].Title != "replacement" || issues[1].ID != "b" {
		t.Fatalf("unexpected records: %#v", issues)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp.*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files remain: %v, %v", matches, err)
	}
}

func TestLockSerializesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := IssuesPath(dir)
	var wait sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- WithLock(path, func() error {
				content, err := os.ReadFile(path)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				return os.WriteFile(path, append(content, 'x'), 0644)
			})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != strings.Repeat("x", 12) {
		t.Fatalf("lost concurrent writes: %q", content)
	}
}

func TestReclaimStaleLockKeepsReplacement(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "issues.jsonl.lock")
	if err := os.WriteFile(lock, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, stale, stale); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("fresh"), 0600); err != nil {
		t.Fatal(err)
	}
	if ReclaimStaleLock(lock, observed) {
		t.Fatal("reclaimed a different lock")
	}
	content, err := os.ReadFile(lock)
	if err != nil || string(content) != "fresh" {
		t.Fatalf("replacement lock changed: %q, %v", content, err)
	}
}

func TestReclaimStaleLockRemovesObserved(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "issues.jsonl.lock")
	if err := os.WriteFile(lock, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, stale, stale); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !ReclaimStaleLock(lock, observed) {
		t.Fatal("observed stale lock was not reclaimed")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("stale lock remains: %v", err)
	}
}

func TestAtomicReplacePreservesExistingMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has different file mode semantics")
	}
	dir := t.TempDir()
	path := IssuesPath(dir)
	if err := os.WriteFile(path, nil, 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := WriteIssues(dir, []model.Issue{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("file mode changed: %04o", info.Mode().Perm())
	}
}
