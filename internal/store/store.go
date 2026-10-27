// Package store implements the Missions JSONL record and advisory lock format.
// Mutations are atomic per file; callers coordinate multi-file operations by
// acquiring the relevant locks in a consistent order.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

const (
	lockStale   = 30 * time.Second
	lockTimeout = 30 * time.Second
	lockRetry   = 100 * time.Millisecond
)

func IssuesPath(dir string) string    { return filepath.Join(dir, model.IssuesFile) }
func PlansPath(dir string) string     { return filepath.Join(dir, model.PlansFile) }
func TemplatesPath(dir string) string { return filepath.Join(dir, model.TemplatesFile) }

// WithLock mirrors the existing per-file .lock protocol. In particular, a
// stale lock is moved to a unique sidecar before deletion so a contender
// cannot unlink a different process's new lock after a stat/rename race.
func WithLock(dataPath string, fn func() error) error {
	lock := dataPath + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		file, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lock)
				return closeErr
			}
			defer func() { _ = os.Remove(lock) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			// Windows may report sharing/access denial for an existing exclusive
			// lock. Treat that as contention only when the lock path exists.
			if _, statErr := os.Lstat(lock); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) && errors.Is(err, os.ErrPermission) {
					continue // owner may have removed its lock during the probe
				}
				return err
			}
		}
		if st, statErr := os.Stat(lock); statErr == nil && time.Since(st.ModTime()) > lockStale {
			ReclaimStaleLock(lock, st)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Timeout acquiring lock for %s", dataPath)
		}
		time.Sleep(lockRetry + randomJitter(lockRetry))
	}
}

// WithLocks holds locks in the caller's order. Plan/issue operations should
// use PlansPath first and IssuesPath second, matching the original commands.
func WithLocks(paths []string, fn func() error) error {
	if len(paths) == 0 {
		return fn()
	}
	return WithLock(paths[0], func() error { return WithLocks(paths[1:], fn) })
}

// ReclaimStaleLock removes only the same lock entry the caller observed.
// Callers must check the age of observed before attempting reclamation.
func ReclaimStaleLock(lock string, observed os.FileInfo) bool {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return false
	}
	sidecar := lock + ".stale." + hex.EncodeToString(random)
	if err := os.Rename(lock, sidecar); err != nil {
		return false
	}
	moved, err := os.Stat(sidecar)
	if err != nil {
		_ = os.Remove(sidecar)
		return false
	}
	// Some Windows filesystems report a different file identity after rename.
	// In that case, retain the stale-age/metadata check before unlinking.
	sameObserved := os.SameFile(observed, moved) ||
		(runtime.GOOS == "windows" && observed.Size() == moved.Size() && observed.Mode() == moved.Mode())
	if sameObserved && observed.ModTime().Equal(moved.ModTime()) {
		return os.Remove(sidecar) == nil
	}
	if err := os.Rename(sidecar, lock); err != nil {
		_ = os.Remove(sidecar)
	}
	return false
}

func randomJitter(max time.Duration) time.Duration {
	buf := []byte{0, 0}
	if _, err := rand.Read(buf); err != nil {
		return 0
	}
	return time.Duration(int(buf[0])<<8|int(buf[1])) % max
}

type identified interface{ GetID() string }

func readRecords[T identified](path string) ([]T, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := []T{}
	indexes := map[string]int{}
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			continue
		}
		if at, found := indexes[item.GetID()]; found {
			items[at] = item
		} else {
			indexes[item.GetID()] = len(items)
			items = append(items, item)
		}
	}
	return items, nil
}

func writeRecords[T any](path string, items []T) error {
	var out bytes.Buffer
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		out.Write(encoded)
		out.WriteByte('\n')
	}
	return atomicReplace(path, out.Bytes())
}

// Append preserves existing lines, including malformed ones, exactly as the
// TypeScript append helpers do. The caller must hold WithLock for this path.
func appendRecord(path string, item any) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		content = nil
	} else if err != nil {
		return err
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, encoded...)
	content = append(content, '\n')
	return atomicReplace(path, content)
}

func atomicReplace(path string, content []byte) error {
	dir := filepath.Dir(path)
	previous, statErr := os.Stat(path)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if statErr == nil {
		if err := tmp.Chmod(previous.Mode().Perm()); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ReadIssues(dir string) ([]model.Issue, error) { return readRecords[model.Issue](IssuesPath(dir)) }
func ReadPlans(dir string) ([]model.Plan, error)   { return readRecords[model.Plan](PlansPath(dir)) }
func ReadTemplates(dir string) ([]model.Template, error) {
	return readRecords[model.Template](TemplatesPath(dir))
}

func WriteIssues(dir string, items []model.Issue) error { return writeRecords(IssuesPath(dir), items) }
func WritePlans(dir string, items []model.Plan) error   { return writeRecords(PlansPath(dir), items) }
func WriteTemplates(dir string, items []model.Template) error {
	return writeRecords(TemplatesPath(dir), items)
}

func AppendIssue(dir string, item model.Issue) error { return appendRecord(IssuesPath(dir), item) }
func AppendPlan(dir string, item model.Plan) error   { return appendRecord(PlansPath(dir), item) }
func AppendTemplate(dir string, item model.Template) error {
	return appendRecord(TemplatesPath(dir), item)
}
