package config

// All pre-Luuna names and migration rules are confined to this file.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luuna-ai/luuna-missions/internal/model"
)

const legacyDir = ".seeds"

var (
	legacyIgnorePattern = regexp.MustCompile(`(?m)(^|[\s!])\.seeds(/|\s|$)`)
	LegacyHookCommandRE = regexp.MustCompile(`^\s*(?:sd|seeds)\s+(prime|guard)(\s|$)`)
)

func lstatExists(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return info, true, nil
}

// MigrateLegacyLayout moves the old directory only when the target is absent.
// It never merges two stores or follows a linked old/new directory.
func MigrateLegacyLayout(projectRoot string) (bool, error) {
	legacy := filepath.Join(projectRoot, legacyDir)
	legacyInfo, found, err := lstatExists(legacy)
	if err != nil || !found {
		return false, err
	}
	luuna := filepath.Join(projectRoot, ".luuna")
	target := filepath.Join(projectRoot, model.MissionsDir)
	if _, exists, err := lstatExists(target); err != nil {
		return false, err
	} else if exists {
		return false, fmt.Errorf("Both %s/ and %s/ exist in %s. Resolve manually: keep %s/ and remove %s/, or the reverse.", legacyDir, model.MissionsDir, projectRoot, model.MissionsDir, legacyDir)
	}
	luunaInfo, luunaExists, err := lstatExists(luuna)
	if err != nil {
		return false, err
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 || (luunaExists && luunaInfo.Mode()&os.ModeSymlink != 0) {
		return false, fmt.Errorf("Refusing to migrate %s/ in %s: %s or .luuna is a symlink. Move it to %s/ manually.", legacyDir, projectRoot, legacyDir, model.MissionsDir)
	}
	if !legacyInfo.IsDir() {
		return false, nil
	}
	if err := os.MkdirAll(luuna, 0755); err != nil {
		return false, err
	}
	if err := os.Rename(legacy, target); err != nil {
		return false, err
	}
	if err := rewriteIgnoreFiles(projectRoot); err != nil {
		return false, err
	}
	fmt.Fprintf(os.Stderr, "Migrated legacy %s/ to %s/ in %s\n", legacyDir, model.MissionsDir, projectRoot)
	return true, nil
}

func rewriteIgnoreFiles(root string) error {
	for _, name := range []string{".gitattributes", ".gitignore"} {
		path := filepath.Join(root, name)
		info, exists, err := lstatExists(path)
		if err != nil {
			return err
		}
		if !exists || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		before := string(content)
		after := legacyIgnorePattern.ReplaceAllString(before, "${1}"+model.MissionsDir+"${2}")
		if after != before {
			if err := os.WriteFile(path, []byte(after), info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func UpgradeLegacyMarkers(text string) string {
	text = strings.ReplaceAll(text, "<!-- seeds:", "<!-- missions:")
	return strings.ReplaceAll(text, "<!-- seeds-", "<!-- missions-")
}

// UpgradeLegacyConfigKeys preserves the existing new key when both forms are
// present and recursively normalizes user-defined plan templates.
func UpgradeLegacyConfigKeys(value any) any {
	switch typed := value.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = UpgradeLegacyConfigKeys(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		_, hasNew := typed["atlas_source"]
		for key, item := range typed {
			if key == "mulch_source" {
				if !hasNew {
					out["atlas_source"] = UpgradeLegacyConfigKeys(item)
				}
				continue
			}
			out[key] = UpgradeLegacyConfigKeys(item)
		}
		return out
	default:
		return value
	}
}
