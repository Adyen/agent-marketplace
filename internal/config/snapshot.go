package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// FileSnapshot is a byte-for-byte snapshot of one config file. It follows an
// existing symlink to its regular target so restoring an edit preserves the
// link rather than replacing it with a regular file.
type FileSnapshot struct {
	path    string
	target  string
	existed bool
	mode    os.FileMode
	data    []byte
}

// ConfigFileCommit identifies the exact bytes committed by one config edit.
// Generation is a digest of Bytes and is retained as a cheap compare-and-swap
// token for rollback guards.
type ConfigFileCommit struct {
	Bytes      []byte
	Generation string
}

func newConfigFileCommit(data []byte) ConfigFileCommit {
	sum := sha256.Sum256(data)
	return ConfigFileCommit{Bytes: bytes.Clone(data), Generation: "sha256:" + hex.EncodeToString(sum[:])}
}

func (c ConfigFileCommit) matches(data []byte) bool {
	return c.Generation == newConfigFileCommit(data).Generation && bytes.Equal(c.Bytes, data)
}

// SnapshotConfigFile snapshots path when it is absent or a regular file. An
// existing symlink is allowed only when it resolves to a regular file. Other
// file types are rejected before a caller can mutate the config.
func SnapshotConfigFile(path string) (*FileSnapshot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config snapshot path %q: %w", path, err)
	}
	snapshot := &FileSnapshot{path: filepath.Clean(absolute), target: filepath.Clean(absolute)}
	info, err := os.Lstat(snapshot.path)
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot, nil
		}
		return nil, fmt.Errorf("stat config snapshot %q: %w", path, err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(snapshot.path)
	if err != nil {
		return nil, fmt.Errorf("resolve config snapshot %q: %w", path, err)
	}
	snapshot.target = filepath.Clean(resolvedTarget)
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Lstat(snapshot.target)
		if err != nil {
			return nil, fmt.Errorf("stat config snapshot target %q: %w", snapshot.target, err)
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config snapshot %q is not a regular file", path)
	}
	data, err := os.ReadFile(snapshot.target)
	if err != nil {
		return nil, fmt.Errorf("read config snapshot %q: %w", path, err)
	}
	snapshot.existed = true
	snapshot.mode = info.Mode().Perm()
	snapshot.data = data
	return snapshot, nil
}

// Restore restores the exact bytes and permissions captured by SnapshotConfigFile.
// It fails closed if the path was changed to a symlink or another file type
// since the snapshot, rather than replacing an unrelated user file.
func (s *FileSnapshot) Restore() error {
	if s == nil {
		return nil
	}
	return s.restore()
}

// RestoreIfCurrent performs a compare-and-swap rollback. It refuses to restore
// when another writer changed the committed bytes, preserving that writer's
// update instead of overwriting it.
func (s *FileSnapshot) RestoreIfCurrent(commit ConfigFileCommit) error {
	if s == nil {
		return nil
	}
	return withConfigPathLock(s.path, func() error {
		current, err := os.ReadFile(s.path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("config rollback conflict: config %q disappeared", s.path)
			}
			return fmt.Errorf("read config %q during rollback: %w", s.path, err)
		}
		if !commit.matches(current) {
			return fmt.Errorf("config rollback conflict: config %q changed after Marketplace install; refusing to overwrite concurrent writer", s.path)
		}
		return s.restore()
	})
}

func (s *FileSnapshot) restore() error {
	if !s.existed {
		info, err := os.Lstat(s.path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("stat new config %q during rollback: %w", s.path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to remove changed config %q during rollback", s.path)
		}
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("remove new config %q during rollback: %w", s.path, err)
		}
		return nil
	}

	currentTarget, err := filepath.EvalSymlinks(s.path)
	if err != nil {
		return fmt.Errorf("resolve config %q during rollback: %w", s.path, err)
	}
	currentTarget, err = filepath.Abs(currentTarget)
	if err != nil {
		return fmt.Errorf("resolve config target %q during rollback: %w", s.path, err)
	}
	if filepath.Clean(currentTarget) != filepath.Clean(s.target) {
		return fmt.Errorf("refusing to replace changed config target %q during rollback", s.path)
	}
	info, err := os.Lstat(s.target)
	if err != nil {
		return fmt.Errorf("stat config target %q during rollback: %w", s.target, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular config target %q during rollback", s.target)
	}
	if err := writeSnapshotFile(s.target, s.data, s.mode); err != nil {
		return fmt.Errorf("restore config %q: %w", s.path, err)
	}
	return nil
}

func writeSnapshotFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-rollback-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
