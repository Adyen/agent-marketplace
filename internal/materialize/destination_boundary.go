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
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

type destinationBoundary struct {
	capturedRoot            *fsboundary.Dir
	capturedRootPath        string
	machineScope            bool
	repoRoot                string
	homeDir                 string
	roots                   []string
	legacySkillsDir         string
	linkPublicationHook     func(string)
	pruneClassificationHook func(string)
	pruneEnumerationHook    func(string)
	pruneMarkerReadHook     func(string)
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
	if b.capturedRoot != nil {
		info, err := os.Stat(b.capturedRootPath)
		if err != nil {
			return nil, errors.Join(fsboundary.ErrGenerationConflict, err)
		}
		identity, err := fsboundary.FileInfoIdentity(info)
		if err != nil {
			return nil, err
		}
		if !identity.SameObject(b.capturedRoot.Identity()) {
			return nil, fsboundary.ErrGenerationConflict
		}
		rel, err := filepath.Rel(b.capturedRootPath, dir)
		if err != nil {
			return nil, err
		}
		if create {
			return b.capturedRoot.EnsurePath(rel)
		}
		return b.capturedRoot.OpenPath(rel)
	}
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

func (b destinationBoundary) validateDirectories(dirs []string) (string, error) {
	for _, root := range b.roots {
		if err := harness.ValidateDestinationRoot(b.machineScope, b.repoRoot, b.homeDir, root); err != nil {
			return root, err
		}
	}
	for _, dir := range dirs {
		if err := b.directory(dir, false); err != nil {
			return dir, err
		}
	}
	return "", nil
}

func configureDestinationBoundary(boundary *destinationBoundary, key outputKey, outDir, repoRoot string, destinations []harness.Destination) []string {
	if key.target == hcc.TargetFactory {
		boundary.legacySkillsDir = filepath.Join(outDir, "skills")
	}
	var directories []string
	for _, destination := range destinations {
		root := destination.Root
		if key.target == hcc.TargetWindsurf && !key.machineScope {
			root = filepath.Join(repoRoot, windsurfDir)
		}
		boundary.roots = append(boundary.roots, root)
		if key.target == hcc.TargetCodex && (!key.machineScope || destination.Prefix == ".agents" || os.Getenv("CODEX_HOME") == "") {
			boundary.roots = append(boundary.roots, filepath.Join(outDir, destination.Prefix))
		}
		if len(destination.AggregateRoutes) > 0 {
			directories = append(directories, filepath.Dir(filepath.Join(destination.Root, destination.AggregateFile)))
		}
	}
	return directories
}
