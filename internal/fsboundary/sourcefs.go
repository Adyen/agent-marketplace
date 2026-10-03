package fsboundary

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type SourceFS struct {
	root     *Dir
	mu       sync.Mutex
	refusal  error
	admitted map[string]struct{}
}

func OpenSourceFS(path string) (*SourceFS, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := OpenDir(absolute)
	if err != nil {
		return nil, err
	}
	return &SourceFS{root: root}, nil
}

func (s *SourceFS) Close() error { return s.root.Close() }

func (s *SourceFS) VerifyRoot() (err error) {
	root, err := OpenDir(s.root.Path())
	if err != nil {
		return fmt.Errorf("%w: revision root is no longer reachable: %w", ErrGenerationConflict, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if !s.root.Identity().SameObject(root.Identity()) {
		return fmt.Errorf("%w: captured source root was replaced", ErrGenerationConflict)
	}
	return nil
}

func (s *SourceFS) Sub(name string) (*SourceFS, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "sub", Path: name, Err: fs.ErrInvalid}
	}
	root, err := s.root.OpenContainedRoot(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	return &SourceFS{root: root}, nil
}

func (s *SourceFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	directory, err := s.root.OpenContainedRoot(filepath.FromSlash(name))
	if err == nil {
		native, err := directory.NativeRoot()
		if err != nil {
			_ = directory.Close()
			return nil, err
		}
		file, err := native.Open(".")
		if err != nil {
			_ = native.Close()
			_ = directory.Close()
			return nil, err
		}
		return &sourceDirectory{File: file, native: native, directory: directory}, nil
	}
	if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, ErrUnsafePath) {
		return nil, err
	}
	data, identity, err := s.read(name)
	if err != nil {
		return nil, err
	}
	return &sourcePayload{Reader: bytes.NewReader(data), info: sourcePayloadInfo{name: filepath.Base(name), identity: identity}}, nil
}

func (s *SourceFS) ReadFile(name string) ([]byte, error) {
	data, _, err := s.read(name)
	return data, err
}

func (s *SourceFS) Refusal() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refusal
}

func (s *SourceFS) WasRead(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, admitted := s.admitted[name]
	return admitted
}

func IsSourceRefusal(err error) bool {
	return errors.Is(err, ErrUnsafePath) || errors.Is(err, ErrUnsafeFile) || errors.Is(err, ErrGenerationConflict) || errors.Is(err, ErrTooLarge)
}

func (s *SourceFS) recordRefusal(err error) {
	if IsSourceRefusal(err) {
		s.mu.Lock()
		if s.refusal == nil {
			s.refusal = err
		}
		s.mu.Unlock()
	}
}

func (s *SourceFS) read(name string) (data []byte, identity Identity, err error) {
	defer func() {
		if err == nil {
			s.mu.Lock()
			if s.admitted == nil {
				s.admitted = make(map[string]struct{})
			}
			s.admitted[name] = struct{}{}
			s.mu.Unlock()
		} else {
			s.recordRefusal(err)
		}
	}()
	if !fs.ValidPath(name) {
		return nil, Identity{}, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	source, err := s.captureSource(name)
	if err != nil {
		return nil, Identity{}, fmt.Errorf("read source %s: %w", name, err)
	}
	defer func() {
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	return source.ReadTarget(source.Identity().Size)
}

func (s *SourceFS) captureSource(name string) (*Destination, error) {
	source, err := CaptureDestination(s.root, filepath.FromSlash(name), LinkPolicyRepository)
	if errors.Is(err, ErrUnsafePath) {
		return s.captureAliasedSource(name)
	}
	return source, err
}

func (s *SourceFS) captureAliasedSource(name string) (source *Destination, err error) {
	parent, err := s.root.OpenContainedRoot(filepath.Dir(filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, parent.Close())
		if err != nil && source != nil {
			err = errors.Join(err, source.Close())
			source = nil
		}
	}()
	parentRel, err := filepath.Rel(s.root.Path(), parent.Path())
	if err != nil {
		return nil, err
	}
	source, err = CaptureDestination(s.root, filepath.Join(parentRel, filepath.Base(name)), LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	parentID := s.root.Identity()
	if len(source.logicalChain) > 0 {
		parentID = source.logicalChain[len(source.logicalChain)-1].id
	}
	if !parentID.SameObject(parent.Identity()) {
		return source, fmt.Errorf("%w: source parent changed during capture", ErrGenerationConflict)
	}
	return source, nil
}

func (s *SourceFS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	file, err := s.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	directory, ok := file.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: ErrUnsafeFile}
	}
	return directory.ReadDir(-1)
}

func (s *SourceFS) Stat(name string) (info fs.FileInfo, err error) {
	defer func() {
		s.recordRefusal(err)
		if err == nil && name != "." && info.Name() != filepath.Base(name) {
			info = sourceStatInfo{FileInfo: info, name: filepath.Base(name)}
		}
	}()
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	directory, err := s.root.OpenContainedRoot(filepath.FromSlash(name))
	if err == nil {
		defer func() { err = errors.Join(err, directory.Close()) }()
		return directory.file.Stat()
	}
	if errors.Is(err, fs.ErrNotExist) && !errors.Is(err, ErrUnsafePath) {
		return nil, err
	}
	source, err := s.captureSource(name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	return statCapturedSource(source)
}

func statCapturedSource(source *Destination) (info fs.FileInfo, err error) {
	parent, err := source.verifyTarget()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	if err := source.verifyLeaf(parent); err != nil {
		return nil, err
	}
	if !source.exists {
		return nil, fs.ErrNotExist
	}
	file, _, err := openLeaf(parent, source.effectiveName())
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	identity, err := FileInfoIdentity(info)
	if err != nil {
		return nil, err
	}
	if !identity.SameState(source.Identity()) {
		return nil, fmt.Errorf("%w: source metadata changed since capture", ErrGenerationConflict)
	}
	return info, nil
}

type sourceStatInfo struct {
	fs.FileInfo
	name string
}

func (i sourceStatInfo) Name() string { return i.name }

type sourceDirectory struct {
	*os.File
	native    *os.Root
	directory *Dir
}

func (d *sourceDirectory) Close() error {
	return errors.Join(d.File.Close(), d.native.Close(), d.directory.Close())
}

type sourcePayload struct {
	*bytes.Reader
	info sourcePayloadInfo
}

func (p *sourcePayload) Stat() (fs.FileInfo, error) { return p.info, nil }
func (p *sourcePayload) Close() error               { return nil }

type sourcePayloadInfo struct {
	name     string
	identity Identity
}

func (i sourcePayloadInfo) Name() string       { return i.name }
func (i sourcePayloadInfo) Size() int64        { return i.identity.Size }
func (i sourcePayloadInfo) Mode() fs.FileMode  { return i.identity.Mode }
func (i sourcePayloadInfo) ModTime() time.Time { return time.Unix(0, i.identity.ModTimeNS) }
func (i sourcePayloadInfo) IsDir() bool        { return i.identity.Mode.IsDir() }
func (i sourcePayloadInfo) Sys() any           { return nil }
