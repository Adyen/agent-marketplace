package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/ownership"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Inspect Git's path metadata without launching Git during root resolution.
func validateClaudeGitRoot(repoRoot, root string) error {
	physical, err := ownership.CanonicalRoot(root)
	if err != nil {
		return err
	}
	for _, path := range []string{root, physical} {
		for _, part := range strings.Split(filepath.ToSlash(path), "/") {
			if strings.EqualFold(part, ".git") {
				return fmt.Errorf("claude configuration root is a Git administrative path: %s", root)
			}
		}
	}
	resolve := func(base, path string) string {
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		return filepath.Clean(path)
	}
	var gitDirs, admins []string
	if path := os.Getenv("GIT_DIR"); path != "" {
		gitDirs = append(gitDirs, resolve(repoRoot, path))
	}
	if path := os.Getenv("GIT_COMMON_DIR"); path != "" {
		admins = append(admins, resolve(repoRoot, path))
	}
	if repoRoot != "" {
		base, err := filepath.Abs(repoRoot)
		if err != nil {
			return err
		}
		for {
			marker := filepath.Join(base, ".git")
			info, err := os.Stat(marker)
			if err == nil {
				if info.IsDir() {
					gitDirs = append(gitDirs, marker)
				} else {
					text, readErr := claudeGitPathFile(marker)
					if readErr != nil {
						return readErr
					}
					path, ok := strings.CutPrefix(text, "gitdir:")
					if !ok || strings.TrimSpace(path) == "" {
						return fmt.Errorf("cannot resolve Git administrative path from %s", marker)
					}
					gitDirs = append(gitDirs, resolve(base, strings.TrimSpace(path)))
				}
				break
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	admins = append(admins, gitDirs...)
	for _, dir := range gitDirs {
		common, err := claudeGitPathFile(filepath.Join(dir, "commondir"))
		if err == nil {
			if common == "" {
				return fmt.Errorf("empty Git administrative commondir in %s", dir)
			}
			admins = append(admins, resolve(dir, common))
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	fold := cases.Fold()
	physical = norm.NFC.String(fold.String(norm.NFD.String(physical)))
	for _, admin := range admins {
		canonical, err := ownership.CanonicalRoot(admin)
		if err != nil {
			return err
		}
		canonical = norm.NFC.String(fold.String(norm.NFD.String(canonical)))
		rel, err := filepath.Rel(canonical, physical)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return fmt.Errorf("claude configuration root is within Git administrative path %s: %s", admin, root)
		}
	}
	return nil
}

func claudeGitPathFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("git administrative path metadata is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	return strings.TrimSpace(string(data)), err
}
