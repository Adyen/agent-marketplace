package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func copyScopedSupport(srcDir, dstDir string, compiled map[string]bool, prefix string, missingOK bool, write func(string, []byte, os.FileMode, *destinationBoundary) error) (err error) {
	if srcDir == "" {
		if missingOK {
			return nil
		}
		return fmt.Errorf("%w: empty support source", fs.ErrNotExist)
	}
	absolute, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}
	root, err := fsboundary.OpenDir(absolute)
	if missingOK && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	dstDir, err = filepath.Abs(dstDir)
	if err != nil {
		return err
	}
	var output *fsboundary.Dir
	defer func() {
		if output != nil {
			if closeErr := output.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
	}()
	var boundary *destinationBoundary
	active := make(map[string]bool)
	var walk func(string, string) error
	walk = func(sourceRel, rel string) error {
		if active[sourceRel] {
			return fmt.Errorf("%w: cyclic support directory alias", fsboundary.ErrUnsafePath)
		}
		active[sourceRel] = true
		defer delete(active, sourceRel)
		entries, err := readSupportSourceDirectory(root, sourceRel)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			child := filepath.Join(rel, entry.Name())
			artifactRel := filepath.ToSlash(filepath.Join(prefix, child))
			if artifactRel == "SKILL.md" || compiled[artifactRel] {
				continue
			}
			// A root skill's source dir is a whole registry checkout, so
			// VCS internals must never become harness-visible support
			// files. Git cannot track a .git directory, so regular skills
			// are unaffected.
			if entry.Name() == ".git" {
				continue
			}
			sourceChild, isDir, err := supportSourceChild(root, sourceRel, entry)
			if errors.Is(err, fs.SkipDir) {
				continue
			}
			if err != nil {
				return err
			}
			if boundary == nil {
				var captureErr error
				output, captureErr = capturePortableParent(filepath.Join(dstDir, ".placeholder"), nil, true)
				if captureErr != nil {
					return captureErr
				}
				boundary = &destinationBoundary{capturedRoot: output, capturedRootPath: dstDir}
				slog.Debug("support output admitted", "path", dstDir, "phase", "support-output")
			}
			destinationPath := filepath.Join(dstDir, child)
			if isDir {
				// A root skill's source dir is a whole registry checkout, so
				// VCS internals must never become harness-visible support
				// files. Git cannot track a .git directory, so regular skills
				// are unaffected.
				if err := boundary.directory(destinationPath, true); err != nil {
					return err
				}
				if err := walk(sourceChild, child); err != nil {
					return err
				}
				continue
			}
			data, identity, err := readSupportSourceSnapshot(root, sourceChild)
			if err != nil {
				return err
			}
			if err := write(destinationPath, data, identity.Mode, boundary); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(".", ".")
}

func supportSourceChild(root *fsboundary.Dir, parent string, entry os.DirEntry) (string, bool, error) {
	rel := filepath.Join(parent, entry.Name())
	if entry.Type()&os.ModeSymlink == 0 {
		return rel, entry.IsDir(), nil
	}
	directory, err := root.OpenContainedRoot(rel)
	if err != nil {
		// Only an alias to a non-directory continues as a payload, where the
		// captured file rules apply. A failed directory alias keeps its own
		// error, so an operational failure is not misreported as an unsafe
		// payload and an unsafe alias keeps its typed refusal.
		if supportAliasTargetsNonDirectory(root, rel) {
			return rel, false, nil
		}
		return "", false, err
	}
	targetRel, err := filepath.Rel(root.Path(), directory.Path())
	if err := errors.Join(err, directory.Close()); err != nil {
		return "", false, err
	}
	for _, component := range strings.Split(targetRel, string(filepath.Separator)) {
		if component == ".git" {
			return "", false, fs.SkipDir
		}
	}
	return targetRel, true, nil
}

// supportAliasTargetsNonDirectory reports whether rel is an alias that the
// captured repository link policy resolves to an existing non-directory.
func supportAliasTargetsNonDirectory(root *fsboundary.Dir, rel string) bool {
	target, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return false
	}
	identity := target.Identity()
	exists := target.Exists()
	if closeErr := target.Close(); closeErr != nil {
		return false
	}
	return exists && !identity.Mode.IsDir()
}

// supportPayloadReadHook observes support payload reads. It is nil in
// production and set only by serial tests in this package.
var supportPayloadReadHook func(rel string)

func readSupportSourceSnapshot(root *fsboundary.Dir, rel string) ([]byte, fsboundary.Identity, error) {
	if supportPayloadReadHook != nil {
		supportPayloadReadHook(rel)
	}
	source, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, fsboundary.Identity{}, err
	}
	data, identity, err := source.ReadTarget(source.Identity().Size)
	return data, identity, errors.Join(err, source.Close())
}

// inspectSupportSourceLeaf applies the payload-shape rules of
// readSupportSourceSnapshot without opening or reading the payload: the leaf
// (following a contained file alias) must be a single-link regular file.
// Names the captured root cannot stat directly fall back to the full captured
// classification so escaping and dangling aliases keep their typed refusal.
func inspectSupportSourceLeaf(root *fsboundary.Dir, rel, _ string) (err error) {
	native, err := root.NativeRoot()
	if err != nil {
		return err
	}
	info, statErr := native.Stat(rel)
	if closeErr := native.Close(); closeErr != nil {
		return closeErr
	}
	var identity fsboundary.Identity
	if statErr == nil {
		identity, err = fsboundary.FileInfoIdentity(info)
		if err != nil {
			return err
		}
	} else {
		source, captureErr := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
		if captureErr != nil {
			return captureErr
		}
		identity = source.Identity()
		if closeErr := source.Close(); closeErr != nil {
			return closeErr
		}
	}
	if !identity.Mode.IsRegular() || identity.Links != 1 {
		return fmt.Errorf("%w: support payload %s must be a single-link regular file", fsboundary.ErrUnsafeFile, rel)
	}
	return nil
}

func readSupportSourceDirectory(root *fsboundary.Dir, rel string) ([]os.DirEntry, error) {
	directory, err := root.OpenPath(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	native, err := directory.NativeRoot()
	if err != nil {
		return nil, err
	}
	defer func() { _ = native.Close() }()
	file, err := native.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := file.ReadDir(-1)
	return entries, errors.Join(err, file.Close())
}

func writeSupportStage(path string, data []byte, mode os.FileMode, boundary *destinationBoundary) (err error) {
	parent, err := boundary.captureDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	file, err := native.OpenFile(filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return errors.Join(err, file.Close())
	}
	_, err = file.Write(data)
	return errors.Join(err, file.Close())
}

func writeSupportSnapshot(path string, data []byte, mode os.FileMode, boundary *destinationBoundary) (err error) {
	parent, err := boundary.captureDirectory(filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	native, err := parent.NativeRoot()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := native.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	info, err := native.Lstat(filepath.Base(path))
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: support destination is a link", fsboundary.ErrUnsafePath)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: support destination is not regular", fsboundary.ErrUnsafeFile)
		}
		if info.Size() == int64(len(data)) {
			existing, identity, err := parent.ReadFile(filepath.Base(path), int64(len(data)))
			if err == nil && bytes.Equal(existing, data) && identity.Mode.Perm() == mode.Perm() {
				return nil
			}
			if err != nil && !errors.Is(err, fsboundary.ErrTooLarge) {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return publishPortableBytes(path, data, mode, info, boundary)
}
