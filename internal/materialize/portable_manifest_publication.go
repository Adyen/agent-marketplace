package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func (s *portableState) captureManifestParent(create bool) (parent *fsboundary.Dir, err error) {
	anchor, err := filepath.Abs(filepath.Dir(filepath.Dir(s.path)))
	if err != nil {
		return nil, err
	}
	scope, err := fsboundary.OpenDir(anchor)
	if err != nil {
		return nil, fmt.Errorf("capture manifest scope: %w", err)
	}
	defer func() {
		if closeErr := scope.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
			if parent != nil {
				if parentCloseErr := parent.Close(); parentCloseErr != nil {
					err = errors.Join(err, parentCloseErr)
				}
				parent = nil
			}
		}
	}()
	if s.pin.scope != (fsboundary.Identity{}) && !scope.Identity().SameObject(s.pin.scope) {
		return nil, fmt.Errorf("%w: manifest scope replaced", fsboundary.ErrGenerationConflict)
	}
	name := filepath.Base(filepath.Dir(s.path))
	if create && s.pin.parent == (fsboundary.Identity{}) {
		if err := scope.Mkdir(name, 0o755); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil, fmt.Errorf("%w: manifest parent appeared since load", fsboundary.ErrGenerationConflict)
			}
			return nil, err
		}
		if err := scope.Sync(); err != nil {
			return nil, err
		}
		parent, err = scope.OpenRoot(name)
	} else {
		parent, err = scope.OpenRoot(name)
	}
	if err != nil {
		return nil, fmt.Errorf("capture manifest parent: %w", err)
	}
	if s.pin.parent != (fsboundary.Identity{}) && !parent.Identity().SameObject(s.pin.parent) {
		_ = parent.Close()
		parent = nil
		return nil, fmt.Errorf("%w: manifest parent replaced", fsboundary.ErrGenerationConflict)
	}
	return parent, nil
}

func (s *portableState) manifestNamespaceExpectation(parent *fsboundary.Dir) (*dependencytxn.NamespaceExpectation, error) {
	anchor, err := filepath.Abs(filepath.Clean(filepath.Dir(filepath.Dir(s.path))))
	if err != nil {
		return nil, err
	}
	parentID := parent.Identity()
	zero := fsboundary.Identity{}
	if s.pin.scope.Inode == 0 || parentID.Inode == 0 {
		return nil, fmt.Errorf("%w: portable manifest namespace has no captured inode", fsboundary.ErrUnsafePath)
	}
	if s.pin.parent != zero && !parentID.SameObject(s.pin.parent) {
		return nil, fmt.Errorf("%w: manifest parent changed since load", fsboundary.ErrGenerationConflict)
	}
	return &dependencytxn.NamespaceExpectation{
		ScopePath: filepath.Clean(anchor), ScopeDevice: s.pin.scope.Device, ScopeInode: s.pin.scope.Inode,
		ParentDevice: parentID.Device, ParentInode: parentID.Inode,
	}, nil
}

func (s *portableState) prepareManifestParentForOutput(dir string) (err error) {
	manifestParent, err := filepath.Abs(filepath.Clean(filepath.Dir(s.path)))
	if err != nil {
		return err
	}
	outputDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(manifestParent, outputDir)
	if err != nil {
		return err
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	created := s.pin.parent == (fsboundary.Identity{})
	if created && (s.exists || s.pin.data != nil) {
		return fmt.Errorf("%w: absent manifest parent has existing manifest state", dependencytxn.ErrGenerationConflict)
	}
	parent, err := s.captureManifestParent(created)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, parent.Close())
	}()
	dest, err := s.captureManifestDestination(parent)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, dest.Close())
	}()
	if created && dest.Exists() {
		return fmt.Errorf("%w: manifest appeared while preparing its parent", dependencytxn.ErrGenerationConflict)
	}
	if err := s.verifyManifestParent(parent); err != nil {
		return err
	}
	if created {
		parentID := parent.Identity()
		if parentID.Inode == 0 {
			return fmt.Errorf("%w: created manifest parent has no captured inode", fsboundary.ErrUnsafePath)
		}
		s.pin.parent = parentID
	}
	return nil
}

func refreshNativeHookManifestCommit(commit nativeHookManifestCommit) error {
	state := commit.state
	participant := commit.participant
	if state == nil || participant.Path != state.path || participant.Namespace == nil {
		return fmt.Errorf("%w: missing portable manifest commit receipt", dependencytxn.ErrGenerationConflict)
	}
	if state.exists != participant.Expected.Exists || state.exists &&
		(dependencytxn.Digest(state.raw) != participant.Expected.Digest || participant.ExpectedMode != 0 && state.mode.Perm() != participant.ExpectedMode.Perm()) {
		return fmt.Errorf("%w: portable manifest receipt does not match loaded state", dependencytxn.ErrGenerationConflict)
	}
	snapshot, err := readPortableManifestSnapshot(state.path)
	if err != nil {
		return err
	}
	namespace := participant.Namespace
	if snapshot.scope.Device != namespace.ScopeDevice || snapshot.scope.Inode != namespace.ScopeInode ||
		snapshot.parent.Device != namespace.ParentDevice || snapshot.parent.Inode != namespace.ParentInode {
		return fmt.Errorf("%w: committed portable manifest namespace changed", dependencytxn.ErrGenerationConflict)
	}
	zero := fsboundary.Identity{}
	if state.pin.scope != zero && !snapshot.scope.SameObject(state.pin.scope) ||
		state.pin.parent != zero && !snapshot.parent.SameObject(state.pin.parent) {
		return fmt.Errorf("%w: committed portable manifest lost its original namespace", dependencytxn.ErrGenerationConflict)
	}
	if participant.Delete {
		if snapshot.data != nil || snapshot.file != zero || len(participant.Bytes) != 0 || participant.Mode != 0 || !participant.Expected.Exists && participant.ExpectedMode != 0 {
			return fmt.Errorf("%w: invalid portable manifest deletion receipt", dependencytxn.ErrGenerationConflict)
		}
	} else if participant.Expected.Exists || len(participant.Bytes) > 0 || participant.Mode != 0 {
		if snapshot.data == nil || !bytes.Equal(snapshot.data, participant.Bytes) ||
			dependencytxn.Digest(snapshot.data) != dependencytxn.Digest(participant.Bytes) ||
			snapshot.file.Mode.Perm() != participant.Mode.Perm() {
			return fmt.Errorf("%w: committed portable manifest differs from participant", dependencytxn.ErrGenerationConflict)
		}
	} else if snapshot.data != nil || snapshot.file != zero {
		return fmt.Errorf("%w: portable manifest absence assertion no longer holds", dependencytxn.ErrGenerationConflict)
	}
	entries, err := decodePortableManifestEntries(state.path, snapshot.data, state.machineScope)
	if err != nil {
		return err
	}
	state.entries = entries
	state.pin = snapshot
	state.raw = append([]byte(nil), snapshot.data...)
	state.exists = snapshot.data != nil
	state.mode = snapshot.file.Mode.Perm()
	return nil
}

func (s *portableState) captureManifestDestination(parent *fsboundary.Dir) (*fsboundary.Destination, error) {
	dest, err := fsboundary.CaptureDestination(parent, filepath.Base(s.path), fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, err
	}
	if dest.Link() != nil {
		_ = dest.Close()
		return nil, fmt.Errorf("%w: manifest leaf is a link", fsboundary.ErrUnsafePath)
	}
	if dest.Exists() != (s.pin.data != nil) || dest.Exists() && !dest.Identity().SameState(s.pin.file) {
		_ = dest.Close()
		return nil, fmt.Errorf("%w: manifest changed since load", fsboundary.ErrGenerationConflict)
	}
	if dest.Exists() {
		current, _, err := dest.ReadTarget(s.pin.file.Size)
		if err != nil {
			_ = dest.Close()
			return nil, err
		}
		if !bytes.Equal(current, s.pin.data) {
			_ = dest.Close()
			return nil, fmt.Errorf("%w: manifest content changed since load", fsboundary.ErrGenerationConflict)
		}
	}
	return dest, nil
}

func (s *portableState) removeManifest() (err error) {
	if s.pin.data == nil && s.pin.parent == (fsboundary.Identity{}) {
		return nil
	}
	parent, err := s.captureManifestParent(false)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	dest, err := s.captureManifestDestination(parent)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dest.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if err := s.verifyManifestParent(parent); err != nil {
		return err
	}
	if s.pin.data == nil {
		return nil
	}
	namespace, err := s.manifestNamespaceExpectation(parent)
	if err != nil {
		return err
	}
	participant := dependencytxn.DesiredFile{
		Path: s.path, Expected: dependencytxn.Generation{Exists: s.exists, Digest: dependencytxn.Digest(s.raw)},
		ExpectedMode: s.mode, Namespace: namespace, Delete: true,
	}
	if err := dest.RemoveLeaf(); err != nil {
		return err
	}
	return refreshNativeHookManifestCommit(nativeHookManifestCommit{state: s, participant: participant})
}

func (s *portableState) verifyManifestParent(parent *fsboundary.Dir) error {
	fresh, err := s.captureManifestParent(false)
	if err != nil {
		return err
	}
	unchanged := fresh.Identity().SameObject(parent.Identity())
	if err := fresh.Close(); err != nil {
		return err
	}
	if !unchanged {
		return fmt.Errorf("%w: staged manifest parent replaced", fsboundary.ErrGenerationConflict)
	}
	return nil
}

func (s *portableState) publishManifest(data []byte) (err error) {
	parent, err := s.captureManifestParent(true)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := parent.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	dest, err := s.captureManifestDestination(parent)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := dest.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	if bytes.Equal(data, s.pin.data) {
		return s.verifyManifestParent(parent)
	}
	mode := fs.FileMode(0o644)
	if dest.Exists() {
		mode = s.pin.file.Mode.Perm()
	}
	namespace, err := s.manifestNamespaceExpectation(parent)
	if err != nil {
		return err
	}
	participant := dependencytxn.DesiredFile{
		Path: s.path, Expected: dependencytxn.Generation{Exists: s.exists, Digest: dependencytxn.Digest(s.raw)},
		ExpectedMode: s.mode, Namespace: namespace, Bytes: data, Mode: mode,
	}
	staged, name, err := parent.CreateTemp(".materialization-manifest-*")
	if err != nil {
		return err
	}
	defer func() {
		if staged != nil {
			if closeErr := staged.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
		}
		if cleanupErr := parent.Unlink(name); cleanupErr != nil && !errors.Is(cleanupErr, fs.ErrNotExist) {
			err = errors.Join(err, cleanupErr)
		}
	}()
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	if _, err := staged.Write(data); err != nil {
		return err
	}
	closeErr := staged.Close()
	staged = nil
	if closeErr != nil {
		return closeErr
	}
	if s.publicationHook != nil {
		s.publicationHook(s.path)
	}
	if err := s.verifyManifestParent(parent); err != nil {
		return err
	}
	if err := dest.Publish(parent, name); err != nil {
		return err
	}
	return refreshNativeHookManifestCommit(nativeHookManifestCommit{state: s, participant: participant})
}
