package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

type destinationBoundary struct {
	machineScope    bool
	repoRoot        string
	homeDir         string
	roots           []string
	legacySkillsDir string
}

func (b destinationBoundary) directory(dir string, create bool) error {
	originalDir := dir
	scope := b.repoRoot
	if b.machineScope {
		scope = b.homeDir
	}
	absoluteScope, err := filepath.Abs(scope)
	if err != nil {
		return err
	}
	canonicalScope, err := filepath.EvalSymlinks(absoluteScope)
	if err != nil {
		return err
	}
	normalize := func(path string) string {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		if rel, err := filepath.Rel(absoluteScope, absolute); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return filepath.Join(canonicalScope, rel)
		}
		return absolute
	}
	dir = normalize(dir)
	root := ""
	for _, candidate := range b.roots {
		candidate = normalize(candidate)
		if rel, err := filepath.Rel(candidate, dir); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) && len(candidate) > len(root) {
			root = candidate
		}
	}
	if root == "" && dir == canonicalScope {
		root = dir
	}
	if root == "" {
		return fmt.Errorf("%w: output directory outside admitted harness roots", fsboundary.ErrUnsafePath)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	if originalDir == b.legacySkillsDir {
		if text, err := os.Readlink(originalDir); err == nil && text == "../.agents/skills" {
			if err := harness.ValidateDestinationPath(b.machineScope, b.repoRoot, b.homeDir, root, filepath.Dir(rel)); err != nil {
				return err
			}
			if !create {
				return nil
			}
			return ensureDirectoryReplacingSymlink(originalDir)
		}
	}
	if create {
		return harness.EnsureDestinationPath(b.machineScope, b.repoRoot, b.homeDir, root, rel)
	}
	return harness.ValidateDestinationPath(b.machineScope, b.repoRoot, b.homeDir, root, rel)
}

func (b destinationBoundary) validate(dirs []string) error {
	for _, root := range b.roots {
		if err := harness.ValidateDestinationRoot(b.machineScope, b.repoRoot, b.homeDir, root); err != nil {
			return err
		}
	}
	for _, dir := range dirs {
		if err := b.directory(dir, false); err != nil {
			return err
		}
	}
	return nil
}
