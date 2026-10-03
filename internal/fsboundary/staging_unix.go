//go:build unix

package fsboundary

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// stagedCreateFlags creates a brand-new staged file: it must not exist, it
// is never a symlink, and it starts owner-only until the caller chmods it.
const stagedCreateFlags = unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_NOFOLLOW | unix.O_CLOEXEC

// CreateTemp creates a new regular file directly below the captured
// directory and returns it open for writing together with its leaf name.
// Pattern follows os.CreateTemp: a "*" is replaced with random text, or
// random text is appended when the pattern has none. The file starts with
// mode 0o600; the caller chmods, writes, and syncs it before publication.
//
// Creation is descriptor-relative: renaming or replacing the directory's
// path afterwards cannot redirect the file into a replacement tree; it
// stays below the captured object.
func (d *Dir) CreateTemp(pattern string) (*os.File, string, error) {
	if pattern == "" || strings.ContainsAny(pattern, `/\`) {
		return nil, "", fmt.Errorf("%w: invalid staged name pattern %q", ErrUnsafePath, pattern)
	}
	prefix, suffix, _ := strings.Cut(pattern, "*")
	var token [16]byte
	for attempts := 0; attempts < 64; attempts++ {
		if _, err := rand.Read(token[:]); err != nil {
			return nil, "", fmt.Errorf("generate staged name: %w", err)
		}
		name := prefix + hex.EncodeToString(token[:]) + suffix
		fd, err := unix.Openat(int(d.file.Fd()), name, stagedCreateFlags, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("create staged file below %s: %w", d.path, err)
		}
		file := os.NewFile(uintptr(fd), name)
		return file, name, nil
	}
	return nil, "", fmt.Errorf("create staged file below %s: name collision exhaustion", d.path)
}

// Mkdir creates the directory name directly below the captured directory.
// Creation is descriptor-relative, so a renamed or replaced path cannot
// redirect it into a replacement tree. An existing name reports
// fs.ErrExist, including a symlink; it is never followed.
func (d *Dir) Mkdir(name string, mode os.FileMode) error {
	if err := validLeafName(name); err != nil {
		return err
	}
	if err := unix.Mkdirat(int(d.file.Fd()), name, uint32(mode.Perm())); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("create directory %s below %s: %w", name, d.path, fs.ErrExist)
		}
		return fmt.Errorf("create directory %s below %s: %w", name, d.path, err)
	}
	return nil
}

// Unlink removes the leaf name below the captured directory without
// following it. Removal is descriptor-relative, so it can never delete a
// leaf inside a replacement tree. A missing name reports fs.ErrNotExist.
func (d *Dir) Unlink(name string) error {
	if err := validLeafName(name); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(d.file.Fd()), name, 0); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("remove %s below %s: %w", name, d.path, fs.ErrNotExist)
		}
		return fmt.Errorf("remove %s below %s: %w", name, d.path, err)
	}
	return nil
}

// validLeafName rejects anything that is not a single relative path
// component.
func validLeafName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: invalid leaf name %q", ErrUnsafePath, name)
	}
	return nil
}

// StagingDir returns a captured Dir for the effective target parent,
// re-verifying the logical and target ancestor chains against capture. A
// renamed, removed, or replaced component is a generation conflict, so a
// staged file can never be created below a replacement tree.
func (d *Destination) StagingDir() (*Dir, error) {
	parent, err := d.verifyTarget()
	if err != nil {
		return nil, err
	}
	path := filepath.Dir(d.Path())
	id, err := identityOf(parent)
	if err != nil {
		_ = parent.Close()
		return nil, fmt.Errorf("inspect staging directory %s: %w", path, err)
	}
	return &Dir{file: parent, path: path, id: id}, nil
}

// ReadTarget reads the effective destination content below the captured
// boundary. The destination is re-verified against its captured state
// (ancestor chains, honored link text, and target identity), the leaf is
// opened descriptor-relatively, and the content is read with the shared
// bounded-read contract, so the returned bytes and identity describe one
// unchanged object. An absent destination reports fs.ErrNotExist.
func (d *Destination) ReadTarget(limit int64) ([]byte, Identity, error) {
	return d.readTarget(limit, nil)
}

func (d *Destination) ReadTargetWith(read func(io.Reader) error) (err error) {
	parent, err := d.verifyTarget()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := d.verifyLeaf(parent); err != nil {
		return err
	}
	if !d.exists {
		return fmt.Errorf("read target %s: %w", d.leafName, fs.ErrNotExist)
	}
	file, before, err := openLeaf(parent, d.effectiveName())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if !before.SameState(d.targetID) {
		return fmt.Errorf("%w: read target changed since capture", ErrGenerationConflict)
	}
	err = read(io.LimitReader(file, before.Size))
	after, statErr := identityOf(file)
	if statErr != nil {
		return errors.Join(err, statErr)
	}
	if !after.SameState(before) {
		return errors.Join(err, fmt.Errorf("%w: target changed during prefix read", ErrGenerationConflict))
	}
	checkedParent, checkErr := d.verifyTarget()
	if checkErr != nil {
		return errors.Join(err, checkErr)
	}
	if leafErr := d.verifyLeaf(checkedParent); leafErr != nil {
		err = errors.Join(err, leafErr)
	}
	if closeErr := checkedParent.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	return err
}

func (d *Destination) readTarget(limit int64, beforeOpen func()) ([]byte, Identity, error) {
	parent, err := d.verifyTarget()
	if err != nil {
		return nil, Identity{}, err
	}
	defer func() { _ = parent.Close() }()
	if err := d.verifyLeaf(parent); err != nil {
		return nil, Identity{}, err
	}
	if !d.exists {
		return nil, Identity{}, fmt.Errorf("read target %s: %w", d.leafName, fs.ErrNotExist)
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	file, _, err := openLeaf(parent, d.effectiveName())
	if err != nil {
		return nil, Identity{}, err
	}
	defer func() { _ = file.Close() }()
	data, id, err := ReadFile(file, limit)
	if err != nil {
		return nil, Identity{}, err
	}
	if !id.SameState(d.targetID) {
		return nil, Identity{}, fmt.Errorf("%w: read target changed since capture", ErrGenerationConflict)
	}
	return data, id, nil
}
