package materialize

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
)

func materializeMarkerAlias(outDir, path string, boundary *destinationBoundary, dryRun bool) (err error) {
	parent, err := capturePortableParent(path, boundary, false)
	if err != nil {
		return err
	}
	if parent == nil {
		return fmt.Errorf("%w: marker alias parent missing", fsboundary.ErrGenerationConflict)
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, native.Close()) }()
	name := filepath.Base(path)
	info, err := native.Lstat(name)
	if err != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fsboundary.ErrGenerationConflict
	}
	link, err := fsboundary.FileInfoIdentity(info)
	if err != nil {
		return err
	}
	destination, err := fsboundary.CaptureDestination(parent, name, fsboundary.LinkPolicyUser)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, destination.Close()) }()
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
			return err
		}
		if rel, err := filepath.Rel(canonical, destination.Path()); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			contained = true
			break
		}
	}
	if !contained {
		return fmt.Errorf("%w: marker alias target outside admitted scope", fsboundary.ErrUnsafePath)
	}
	data, identity, err := destination.ReadTarget(destination.Identity().Size)
	if err != nil {
		return err
	}
	verify := func() error {
		fresh, err := capturePortableParent(path, boundary, false)
		if err != nil {
			return errors.Join(fsboundary.ErrGenerationConflict, err)
		}
		unchanged := fresh != nil && fresh.Identity().SameObject(parent.Identity())
		if fresh != nil {
			if err := fresh.Close(); err != nil {
				return err
			}
		}
		if !unchanged {
			return fsboundary.ErrGenerationConflict
		}
		if err := destination.Verify(); err != nil {
			return err
		}
		current, err := native.Lstat(name)
		if err != nil {
			return errors.Join(fsboundary.ErrGenerationConflict, err)
		}
		observed, err := fsboundary.FileInfoIdentity(current)
		if err != nil {
			return err
		}
		if !observed.SameState(link) {
			return fsboundary.ErrGenerationConflict
		}
		resolved, err := filepath.EvalSymlinks(filepath.Join(parent.Path(), name))
		if err != nil || resolved != destination.Path() {
			return errors.Join(fsboundary.ErrGenerationConflict, err)
		}
		return nil
	}
	slog.Debug("native marker alias input captured", "path", path, "phase", "native-marker-alias-input")
	if err := verify(); err != nil || dryRun {
		return err
	}
	if len(data) == 0 {
		return destination.RemoveLeaf()
	}
	staged, stagedName, err := parent.CreateTemp(".agent-marketplace-*")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, staged.Close()) }()
	var stagedID fsboundary.Identity
	prepared := false
	defer func() {
		current, inspectErr := native.Lstat(stagedName)
		if errors.Is(inspectErr, fs.ErrNotExist) {
			return
		}
		if inspectErr != nil {
			err = errors.Join(err, inspectErr)
			return
		}
		expected := stagedID
		if !prepared {
			expected, inspectErr = fsboundary.FileIdentity(staged)
			if inspectErr != nil {
				err = errors.Join(err, inspectErr)
				return
			}
		}
		currentID, inspectErr := fsboundary.FileInfoIdentity(current)
		if inspectErr != nil || !currentID.SameState(expected) {
			err = errors.Join(err, fsboundary.ErrGenerationConflict, inspectErr)
			return
		}
		err = errors.Join(err, parent.Unlink(stagedName))
	}()
	mode := identity.Mode.Perm()
	if mode == 0 {
		mode = 0o644
	}
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	if _, err := staged.Write(data); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	stagedID, err = fsboundary.FileIdentity(staged)
	if err != nil {
		return err
	}
	prepared = true
	slog.Debug("native marker alias replacement staged", "path", path, "phase", "native-marker-alias-staged")
	if err := verify(); err != nil {
		return err
	}
	current, err := native.Lstat(stagedName)
	if err != nil {
		return errors.Join(fsboundary.ErrGenerationConflict, err)
	}
	currentID, err := fsboundary.FileInfoIdentity(current)
	if err != nil {
		return err
	}
	if !currentID.SameState(stagedID) {
		return fsboundary.ErrGenerationConflict
	}
	return native.Rename(stagedName, name)
}
