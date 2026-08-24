package ownership

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stagedRandomBytes = 16

// defaultAggregateMode is the mode a published aggregate carries when nothing,
// or nothing regular, currently occupies its name.
const defaultAggregateMode = os.FileMode(0o644)

type stagingTransaction struct {
	parent   *os.Root
	dir      *os.Root
	dirName  string
	fileName string
	dirPath  string
	filePath string
}

// stagedPathFor returns an unguessable private staging directory and the file
// it contains. The directory is created separately, after the journal is
// durable, so the journal can describe the paths before replacement bytes are
// written.
func stagedPathFor(absFile string) (dir, path string, err error) {
	suffix := make([]byte, stagedRandomBytes)
	if _, err := rand.Read(suffix); err != nil {
		return "", "", fmt.Errorf("ownership: generate staging directory name: %w", err)
	}
	dir = fmt.Sprintf("%s.am-staged-%s", absFile, hex.EncodeToString(suffix))
	return dir, filepath.Join(dir, filepath.Base(absFile)), nil
}

func validateStagingPaths(j *Journal) error {
	if !filepath.IsAbs(j.AbsFile) || !filepath.IsAbs(j.StagingDir) || !filepath.IsAbs(j.StagedPath) {
		return fmt.Errorf("%w: staging paths must be absolute", ErrStateCorrupt)
	}
	if filepath.Clean(filepath.Dir(j.StagingDir)) != filepath.Clean(filepath.Dir(j.AbsFile)) ||
		filepath.Clean(j.StagedPath) != filepath.Join(filepath.Clean(j.StagingDir), filepath.Base(j.AbsFile)) {
		return fmt.Errorf("%w: staging paths do not belong to aggregate %s", ErrStateCorrupt, j.AbsFile)
	}
	suffix, ok := strings.CutPrefix(j.StagingDir, j.AbsFile+".am-staged-")
	if !ok || len(suffix) != stagedRandomBytes*2 {
		return fmt.Errorf("%w: staging directory name is not tied to aggregate %s", ErrStateCorrupt, j.AbsFile)
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		return fmt.Errorf("%w: staging directory name is not random hex: %v", ErrStateCorrupt, err)
	}
	return nil
}

// validateLegacyStagingPath pins a version-1 journal's staged path to the single
// pathname that version could have written. Recovery DELETES that path, so a
// journal naming anything else would turn recovery into an arbitrary-unlink
// primitive.
func validateLegacyStagingPath(j *Journal) error {
	if j.StagedPath == "" {
		return nil
	}
	if !filepath.IsAbs(j.StagedPath) {
		return fmt.Errorf("%w: legacy staging path must be absolute", ErrStateCorrupt)
	}
	expected := fmt.Sprintf("%s.am-staged-%d", filepath.Clean(j.AbsFile), j.Generation)
	if filepath.Clean(j.StagedPath) != expected {
		return fmt.Errorf("%w: legacy staging path is not tied to aggregate %s", ErrStateCorrupt, j.AbsFile)
	}
	return nil
}

// openAggregateParent opens the aggregate's canonical parent directory as an
// [os.Root], after proving it lies inside the store's canonical root. Every
// staging operation then names files relative to that descriptor, so replacing a
// directory in the pathname afterwards cannot redirect a write, a rename, or an
// unlink.
func openAggregateParent(canonicalRoot, absFile string) (*os.Root, error) {
	parentPath := filepath.Dir(absFile)
	canonicalParent, err := CanonicalRoot(parentPath)
	if err != nil {
		return nil, err
	}
	relativeParent, err := filepath.Rel(canonicalRoot, canonicalParent)
	if err != nil || relativeParent == ".." || filepath.IsAbs(relativeParent) || strings.HasPrefix(relativeParent, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("ownership: aggregate directory %s is outside canonical root %s", parentPath, canonicalRoot)
	}
	root, err := os.OpenRoot(canonicalRoot)
	if err != nil {
		return nil, fmt.Errorf("ownership: open canonical root %s: %w", canonicalRoot, err)
	}
	if relativeParent == "." {
		return root, nil
	}
	parent, err := root.OpenRoot(relativeParent)
	_ = root.Close()
	if err != nil {
		return nil, fmt.Errorf("ownership: open aggregate directory %s: %w", canonicalParent, err)
	}
	return parent, nil
}

func newStagingTransaction(canonicalRoot, absFile, dirPath, filePath string) (*stagingTransaction, error) {
	if filepath.Dir(dirPath) != filepath.Dir(absFile) || filepath.Base(filePath) != filepath.Base(absFile) || filepath.Dir(filePath) != dirPath {
		return nil, fmt.Errorf("ownership: staging paths are not siblings of aggregate %s", absFile)
	}
	parent, err := openAggregateParent(canonicalRoot, absFile)
	if err != nil {
		return nil, err
	}
	return &stagingTransaction{
		parent:   parent,
		dirName:  filepath.Base(dirPath),
		fileName: filepath.Base(filePath),
		dirPath:  dirPath,
		filePath: filePath,
	}, nil
}

func (s *stagingTransaction) close() error {
	if s.dir != nil {
		if err := s.dir.Close(); err != nil {
			_ = s.parent.Close()
			return err
		}
	}
	if s.parent != nil {
		return s.parent.Close()
	}
	return nil
}

func (s *stagingTransaction) create() error {
	if err := s.parent.Mkdir(s.dirName, 0o700); err != nil {
		return fmt.Errorf("ownership: create staging directory %s: %w", s.dirPath, err)
	}
	if err := syncRootDirectory(s.parent); err != nil {
		return fmt.Errorf("ownership: fsync staging parent %s: %w", filepath.Dir(s.dirPath), err)
	}
	dir, err := s.parent.OpenRoot(s.dirName)
	if err != nil {
		return fmt.Errorf("ownership: open staging directory %s: %w", s.dirPath, err)
	}
	s.dir = dir
	return nil
}

// aggregateMode is the mode the published aggregate should carry. Permissions
// are inherited only from a REGULAR file: an [os.Root] follows symlinks that
// stay inside the root, so inheriting from whatever the aggregate name resolves
// to would let a planted link widen the mode of every generation this package
// publishes from then on.
func (s *stagingTransaction) aggregateMode(aggregateName string) os.FileMode {
	info, err := s.parent.Lstat(aggregateName)
	if err != nil || !info.Mode().IsRegular() {
		return defaultAggregateMode
	}
	return info.Mode().Perm()
}

func (s *stagingTransaction) write(data []byte, mode os.FileMode) (*os.File, error) {
	if s.dir == nil {
		return nil, fmt.Errorf("ownership: staging directory is not open")
	}
	// O_EXCL is what makes this write no-follow. An os.Root refuses to escape
	// the root but still follows links that stay inside it, so refusing a
	// pre-existing final component is the property that keeps a planted link
	// from redirecting the staged bytes onto an unrelated file.
	f, err := s.dir.OpenFile(s.fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, fmt.Errorf("ownership: create staged file %s: %w", s.filePath, err)
	}
	cleanup := func(cause error) (*os.File, error) {
		return nil, s.cleanupWrite(f, cause)
	}
	if err := f.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("ownership: chmod staged file %s: %w", s.filePath, err))
	}
	if n, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("ownership: write staged file %s: %w", s.filePath, err))
	} else if n != len(data) {
		return cleanup(fmt.Errorf("ownership: short write to staged file %s: wrote %d of %d bytes", s.filePath, n, len(data)))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staged file %s: %w", s.filePath, err))
	}
	if err := syncRootDirectory(s.dir); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staging directory %s: %w", s.dirPath, err))
	}
	return f, nil
}

// cleanupWrite discards a half-written staged file. The identity check before
// the unlink is not ceremony: a failed write leaves the descriptor open, and
// removing a NAME that no longer belongs to it would delete somebody else's
// file while leaving the partial bytes behind.
func (s *stagingTransaction) cleanupWrite(f *os.File, cause error) error {
	var cleanupErr error
	if opened, statErr := f.Stat(); statErr != nil {
		cleanupErr = fmt.Errorf("ownership: stat staged descriptor %s during cleanup: %w", s.filePath, statErr)
	} else if current, lstatErr := s.dir.Lstat(s.fileName); lstatErr == nil {
		if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
			cleanupErr = fmt.Errorf("ownership: staged path %s changed during cleanup", s.filePath)
		} else if removeErr := s.dir.Remove(s.fileName); removeErr != nil && !errorsIsNotExist(removeErr) {
			cleanupErr = fmt.Errorf("ownership: remove staged file %s during cleanup: %w", s.filePath, removeErr)
		}
	} else if !errorsIsNotExist(lstatErr) {
		cleanupErr = fmt.Errorf("ownership: inspect staged file %s during cleanup: %w", s.filePath, lstatErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("ownership: close staged file %s during cleanup: %w", s.filePath, closeErr))
	}
	return errors.Join(cause, cleanupErr)
}

// publish renames the staged file onto the aggregate name inside the opened
// parent directory.
//
// beforeRename is the crash-injection boundary, and every identity check runs
// AFTER it on purpose: a check that runs before the last point at which the
// process can be descheduled proves nothing about the rename that follows.
//
// The staged identity is then checked twice, because the two checks fail on
// different attacks. verifyStagedFile resolves the recorded absolute pathname,
// so it detects a replaced ancestor directory. verifyStagedEntry resolves the
// final component through the very descriptor the rename will use, so it detects
// a replaced staging entry without trusting any pathname at all. Publishing an
// entry that failed either check would move attacker-chosen bytes — or an
// attacker-chosen symlink — onto the aggregate, which is exactly the exposure a
// post-hoc rollback cannot undo.
func (s *stagingTransaction) publish(staged *os.File, aggregateName string, beforeRename func() error) error {
	if beforeRename != nil {
		if err := beforeRename(); err != nil {
			return err
		}
	}
	if err := verifyStagedFile(staged, s.filePath); err != nil {
		return err
	}
	if err := s.verifyStagedEntry(staged); err != nil {
		return err
	}
	if err := s.parent.Rename(filepath.Join(s.dirName, s.fileName), aggregateName); err != nil {
		return fmt.Errorf("ownership: publish %s: %w", filepath.Join(s.parent.Name(), aggregateName), err)
	}
	// A destination that no longer names the staged inode means something
	// replaced the aggregate between the rename and this check. Nothing is
	// restored over it: bytes this package did not write are not its to
	// overwrite, and the mandatory safety backup plus the retained journal are
	// what recovery needs to report the foreign change.
	if err := verifyPublishedFile(staged, s.parent, aggregateName); err != nil {
		return err
	}
	return syncRootDirectory(s.parent)
}

func (s *stagingTransaction) remove(expectedDigest string) error {
	if s.dir == nil {
		dir, err := s.parent.OpenRoot(s.dirName)
		if err != nil {
			if errorsIsNotExist(err) {
				return nil
			}
			return fmt.Errorf("ownership: open staging directory %s: %w", s.dirPath, err)
		}
		s.dir = dir
	}
	if info, err := s.dir.Lstat(s.fileName); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("ownership: staged path %s is not a regular file", s.filePath)
		}
		data, err := s.dir.ReadFile(s.fileName)
		if err != nil {
			return fmt.Errorf("ownership: read staged file %s: %w", s.filePath, err)
		}
		if expectedDigest != "" && Digest(data) != expectedDigest {
			return fmt.Errorf("ownership: staged file %s changed before cleanup", s.filePath)
		}
		if err := s.dir.Remove(s.fileName); err != nil {
			return fmt.Errorf("ownership: remove staged file %s: %w", s.filePath, err)
		}
	} else if !errorsIsNotExist(err) {
		return fmt.Errorf("ownership: inspect staged file %s: %w", s.filePath, err)
	}
	if err := s.dir.Close(); err != nil {
		return fmt.Errorf("ownership: close staging directory %s: %w", s.dirPath, err)
	}
	s.dir = nil
	if err := s.parent.Remove(s.dirName); err != nil && !errorsIsNotExist(err) {
		return fmt.Errorf("ownership: remove staging directory %s: %w", s.dirPath, err)
	}
	return syncRootDirectory(s.parent)
}

func errorsIsNotExist(err error) bool { return err != nil && os.IsNotExist(err) }

func syncRootDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// Some filesystems refuse fsync on a directory. We still complete the
	// mutation when that happens, but cannot confirm that the directory update
	// survived a power loss.
	_ = f.Sync()
	return nil
}

// verifyStagedFile confirms that the recorded absolute pathname still resolves
// to the exclusively-created file.
func verifyStagedFile(file *os.File, path string) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("ownership: stat staged descriptor: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("ownership: stat staged path %s: %w", path, err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("ownership: staged path %s changed before publication", path)
	}
	return nil
}

// verifyStagedEntry confirms the same thing as [verifyStagedFile] through the
// open staging descriptor, so the answer does not depend on any pathname.
func (s *stagingTransaction) verifyStagedEntry(staged *os.File) error {
	if s.dir == nil {
		return fmt.Errorf("ownership: staging directory is not open")
	}
	opened, err := staged.Stat()
	if err != nil {
		return fmt.Errorf("ownership: stat staged descriptor: %w", err)
	}
	current, err := s.dir.Lstat(s.fileName)
	if err != nil {
		return fmt.Errorf("ownership: stat staged entry %s: %w", s.filePath, err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("ownership: staged entry %s changed before publication", s.filePath)
	}
	return nil
}

func verifyPublishedFile(file *os.File, root *os.Root, name string) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("ownership: stat published descriptor: %w", err)
	}
	current, err := root.Lstat(name)
	if err != nil {
		return fmt.Errorf("ownership: stat published path %s: %w", filepath.Join(root.Name(), name), err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("ownership: publication replaced staged file at %s", filepath.Join(root.Name(), name))
	}
	return nil
}
