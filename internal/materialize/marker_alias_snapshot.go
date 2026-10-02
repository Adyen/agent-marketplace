package materialize

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func markerAliasSnapshot(outDir, path string, boundary *destinationBoundary) ([]byte, fs.FileMode, error) {
	parentPath := filepath.Dir(path)
	var parent *fsboundary.Dir
	var err error
	if boundary != nil {
		parent, err = boundary.captureDirectory(parentPath, false)
	} else {
		absolute, absErr := filepath.Abs(parentPath)
		if absErr != nil {
			return nil, 0, absErr
		}
		parent, err = fsboundary.OpenDir(absolute)
	}
	if err != nil {
		return nil, 0, err
	}
	if parent == nil {
		return nil, 0, fmt.Errorf("%w: marker alias parent missing", fsboundary.ErrGenerationConflict)
	}
	defer func() { _ = parent.Close() }()
	destination, err := fsboundary.CaptureDestination(parent, filepath.Base(path), fsboundary.LinkPolicyUser)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = destination.Close() }()
	scope := outDir
	if boundary != nil {
		scope = boundary.repoRoot
		if boundary.machineScope {
			scope = boundary.homeDir
		}
	}
	approved := []string{scope, cache.CacheDir()}
	if boundary != nil && boundary.machineScope {
		approved = append(approved, parent.Path())
	}
	contained := false
	for _, root := range approved {
		canonical, err := ownership.CanonicalRoot(root)
		if err != nil {
			return nil, 0, err
		}
		if rel, err := filepath.Rel(canonical, destination.Path()); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			contained = true
			break
		}
	}
	if !contained {
		return nil, 0, fmt.Errorf("%w: marker alias target outside admitted scope", fsboundary.ErrUnsafePath)
	}
	bytes, identity, err := destination.ReadTarget(destination.Identity().Size)
	if err != nil {
		return nil, 0, err
	}
	return bytes, identity.Mode.Perm(), nil
}
