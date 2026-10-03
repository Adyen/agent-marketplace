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
	native   *os.Root
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
	native, err := root.NativeRoot()
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	return &SourceFS{root: root, native: native}, nil
}

func (s *SourceFS) Close() error { return errors.Join(s.native.Close(), s.root.Close()) }

func (s *SourceFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	info, err := s.native.Lstat(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		directory, err := s.root.OpenPath(filepath.FromSlash(name))
		if err != nil {
			return nil, err
		}
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

func (s *SourceFS) read(name string) (data []byte, identity Identity, err error) {
	defer func() {
		if err == nil {
			s.mu.Lock()
			if s.admitted == nil {
				s.admitted = make(map[string]struct{})
			}
			s.admitted[name] = struct{}{}
			s.mu.Unlock()
		} else if IsSourceRefusal(err) {
			s.mu.Lock()
			if s.refusal == nil {
				s.refusal = err
			}
			s.mu.Unlock()
		}
	}()
	if !fs.ValidPath(name) {
		return nil, Identity{}, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	source, err := CaptureDestination(s.root, filepath.FromSlash(name), LinkPolicyRepository)
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

func (s *SourceFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	return s.native.Stat(filepath.FromSlash(name))
}

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
