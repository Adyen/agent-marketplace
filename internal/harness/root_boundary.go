package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func ValidateDestinationRoot(machineScope bool, repoRoot, homeDir, root string) error {
	return destinationPath(machineScope, repoRoot, homeDir, root, ".", false)
}

func ValidateDestinationPath(machineScope bool, repoRoot, homeDir, root, sub string) error {
	return destinationPath(machineScope, repoRoot, homeDir, root, sub, false)
}

func destinationPath(machineScope bool, repoRoot, homeDir, root, sub string, create bool) error {
	directory, err := CaptureDestinationPath(machineScope, repoRoot, homeDir, root, sub, create)
	if err != nil || directory == nil {
		return err
	}
	return directory.Close()
}

func CaptureDestinationPath(machineScope bool, repoRoot, homeDir, root, sub string, create bool) (*fsboundary.Dir, error) {
	scope := repoRoot
	if machineScope {
		if canonicalHome, err := filepath.EvalSymlinks(homeDir); err == nil {
			if rel, err := filepath.Rel(homeDir, root); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				root = filepath.Join(canonicalHome, rel)
			}
			homeDir = canonicalHome
		}
		if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
			canonical, err := filepath.EvalSymlinks(root)
			if err != nil {
				return nil, fmt.Errorf("%w: resolve machine harness alias: %w", fsboundary.ErrUnsafePath, err)
			}
			root = canonical
		}
		scope = homeDir
		if rel, err := filepath.Rel(homeDir, root); err == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)) {
			scope = filepath.Dir(root)
		}
	}
	if scope == "" {
		scope = "."
	}
	absolute, err := filepath.Abs(scope)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	base, err := fsboundary.OpenDir(absolute)
	for create && machineScope && errors.Is(err, fs.ErrNotExist) && filepath.Dir(absolute) != absolute {
		absolute = filepath.Dir(absolute)
		base, err = fsboundary.OpenDir(absolute)
	}
	if !create && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: capture destination scope: %w", fsboundary.ErrUnsafePath, err)
	}
	defer func() { _ = base.Close() }()
	rel, err := filepath.Rel(absolute, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		rel, err = filepath.Rel(base.Path(), root)
	}
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("%w: harness root escapes its authorized scope", fsboundary.ErrUnsafePath)
	}
	var directory *fsboundary.Dir
	if create {
		directory, err = base.EnsureRoot(rel)
	} else {
		directory, err = base.OpenRoot(rel)
	}
	if !create && errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fsboundary.ErrUnsafePath) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	var child *fsboundary.Dir
	if create {
		child, err = directory.EnsurePath(sub)
	} else {
		child, err = directory.OpenPath(sub)
	}
	if !create && errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return child, nil
}
