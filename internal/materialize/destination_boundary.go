package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

type destinationBoundary struct {
	machineScope            bool
	repoRoot                string
	homeDir                 string
	roots                   []string
	legacySkillsDir         string
	linkPublicationHook     func(string)
	pruneClassificationHook func(string)
	pruneEnumerationHook    func(string)
	manifestPublicationHook func(string)
}

func (b destinationBoundary) directory(dir string, create bool) error {
	captured, err := b.captureDirectory(dir, create)
	if err != nil || captured == nil {
		return err
	}
	return captured.Close()
}

func (b destinationBoundary) captureDirectory(dir string, create bool) (*fsboundary.Dir, error) {
	originalDir := dir
	scope := b.repoRoot
	if b.machineScope {
		scope = b.homeDir
	}
	absoluteScope, err := filepath.Abs(scope)
	if err != nil {
		return nil, err
	}
	canonicalScope, err := filepath.EvalSymlinks(absoluteScope)
	if errors.Is(err, fs.ErrNotExist) {
		canonicalScope = absoluteScope
	} else if err != nil {
		return nil, fmt.Errorf("%w: resolve destination scope %s: %w", fsboundary.ErrUnsafePath, absoluteScope, err)
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
		return nil, fmt.Errorf("%w: output directory outside admitted harness roots", fsboundary.ErrUnsafePath)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return nil, err
	}
	if originalDir == b.legacySkillsDir {
		if text, err := os.Readlink(originalDir); err == nil && text == "../.agents/skills" {
			if err := harness.ValidateDestinationPath(b.machineScope, b.repoRoot, b.homeDir, root, filepath.Dir(rel)); err != nil {
				return nil, err
			}
			if !create {
				return nil, nil
			}
			if err := ensureDirectoryReplacingSymlink(originalDir); err != nil {
				return nil, err
			}
		}
	}
	return harness.CaptureDestinationPath(b.machineScope, b.repoRoot, b.homeDir, root, rel, create)
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
