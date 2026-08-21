package ownership

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const stagedRandomBytes = 16

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

func newStagingTransaction(canonicalRoot, absFile, dirPath, filePath string) (*stagingTransaction, error) {
	parentPath := filepath.Dir(absFile)
	if filepath.Dir(dirPath) != parentPath || filepath.Base(filePath) != filepath.Base(absFile) || filepath.Dir(filePath) != dirPath {
		return nil, fmt.Errorf("ownership: staging paths are not siblings of aggregate %s", absFile)
	}
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
	parent := root
	if relativeParent != "." {
		parent, err = root.OpenRoot(relativeParent)
		_ = root.Close()
		if err != nil {
			return nil, fmt.Errorf("ownership: open aggregate directory %s: %w", canonicalParent, err)
		}
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

func (s *stagingTransaction) write(data []byte, mode os.FileMode) (*os.File, error) {
	if s.dir == nil {
		return nil, fmt.Errorf("ownership: staging directory is not open")
	}
	f, err := s.dir.OpenFile(s.fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, fmt.Errorf("ownership: create staged file %s: %w", s.filePath, err)
	}
	cleanup := func(cause error) (*os.File, error) {
		_ = f.Close()
		return nil, cause
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

func (s *stagingTransaction) publish(staged *os.File, aggregateName string) error {
	if err := verifyStagedFile(staged, s.filePath); err != nil {
		return err
	}
	if err := s.parent.Rename(filepath.Join(s.dirName, s.fileName), aggregateName); err != nil {
		return fmt.Errorf("ownership: publish %s: %w", filepath.Join(s.parent.Name(), aggregateName), err)
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
	_ = f.Sync()
	return nil
}

// verifyStagedFile confirms that the path still names the exclusively-created
// file. The directory-relative publish operation performs this check again at
// its own boundary.
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

// writeStaged is retained as the narrow helper seam for the Unix symlink test.
// Production mutations use stagingTransaction so publication and cleanup are
// rooted in open directory handles.
func writeStaged(path string, data []byte, mode os.FileMode) (*os.File, error) {
	f, err := openStagedFile(path, mode)
	if err != nil {
		return nil, fmt.Errorf("ownership: create staged file %s: %w", path, err)
	}
	cleanup := func(cause error) (*os.File, error) {
		_ = f.Close()
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("%w; remove staged file %s: %v", cause, path, removeErr)
		}
		return nil, cause
	}
	if err := f.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("ownership: chmod staged file %s: %w", path, err))
	}
	if n, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("ownership: write staged file %s: %w", path, err))
	} else if n != len(data) {
		return cleanup(fmt.Errorf("ownership: short write to staged file %s: wrote %d of %d bytes", path, n, len(data)))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staged file %s: %w", path, err))
	}
	if err := fsyncDir(filepath.Dir(path)); err != nil {
		return cleanup(fmt.Errorf("ownership: fsync staging directory %s: %w", filepath.Dir(path), err))
	}
	return f, nil
}
