package fsboundary

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
)

type SourceCheckpoint struct {
	entries map[string]Identity
}

func (s *SourceFS) CaptureCheckpoint() (checkpoint SourceCheckpoint, err error) {
	if err := s.VerifyRoot(); err != nil {
		return SourceCheckpoint{}, err
	}
	root, err := s.root.NativeRoot()
	if err != nil {
		return SourceCheckpoint{}, fmt.Errorf("%w: open source checkpoint root: %w", ErrGenerationConflict, err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return SourceCheckpoint{}, fmt.Errorf("%w: inspect source root: %w", ErrGenerationConflict, err)
	}
	rootIdentity, err := FileInfoIdentity(rootInfo)
	if err != nil {
		return SourceCheckpoint{}, fmt.Errorf("%w: inspect source root: %w", ErrGenerationConflict, err)
	}
	checkpoint.entries = map[string]Identity{".": rootIdentity}
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("%w: walk source entry %q: %w", ErrGenerationConflict, name, walkErr)
		}
		if name == "." {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("%w: inspect source entry %q: %w", ErrGenerationConflict, name, err)
		}
		identity, err := FileInfoIdentity(info)
		if err != nil {
			return fmt.Errorf("%w: inspect source entry %q: %w", ErrGenerationConflict, name, err)
		}
		checkpoint.entries[path.Clean(name)] = identity
		return nil
	})
	if err != nil {
		return SourceCheckpoint{}, err
	}
	afterInfo, err := root.Stat(".")
	if err != nil {
		return SourceCheckpoint{}, fmt.Errorf("%w: inspect source root: %w", ErrGenerationConflict, err)
	}
	afterIdentity, err := FileInfoIdentity(afterInfo)
	if err != nil || !rootIdentity.SameState(afterIdentity) {
		if err != nil {
			return SourceCheckpoint{}, fmt.Errorf("%w: inspect source root: %w", ErrGenerationConflict, err)
		}
		return SourceCheckpoint{}, fmt.Errorf("%w: source root changed while checkpointing", ErrGenerationConflict)
	}
	if err := s.VerifyRoot(); err != nil {
		return SourceCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *SourceFS) VerifyCheckpoint(expected SourceCheckpoint) error {
	current, err := s.CaptureCheckpoint()
	if err != nil {
		return err
	}
	if len(current.entries) != len(expected.entries) {
		return fmt.Errorf("%w: source tree membership changed", ErrGenerationConflict)
	}
	for name, identity := range expected.entries {
		if currentIdentity, ok := current.entries[name]; !ok || !currentIdentity.SameState(identity) {
			return fmt.Errorf("%w: source tree entry %q changed", ErrGenerationConflict, name)
		}
	}
	return nil
}
