//go:build unix

package ownership

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

type preparedMutationSnapshot struct {
	raw               []byte
	priorAbsent       bool
	stateGeneration   dependencytxn.Generation
	stateMode         fs.FileMode
	current           *Record
	currentGeneration uint64
}

func (s *Store) preparedMutationSnapshot(m Mutation) (preparedMutationSnapshot, error) {
	if err := validateMutation(m); err != nil {
		return preparedMutationSnapshot{}, err
	}
	if err := s.ensureRelNamesAbsFile(m); err != nil {
		return preparedMutationSnapshot{}, err
	}
	for _, entry := range m.Entries {
		if err := validateEntry(entry); err != nil {
			return preparedMutationSnapshot{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
		}
	}

	raw, readErr := ReadAggregateFileAt(s.canonicalRoot, m.Rel)
	if readErr != nil && !os.IsNotExist(readErr) {
		return preparedMutationSnapshot{}, fmt.Errorf("ownership: read aggregate for prepare: %w", readErr)
	}
	priorAbsent := os.IsNotExist(readErr)
	liveDigest := ""
	if !priorAbsent {
		liveDigest = Digest(raw)
	}
	if liveDigest != m.ExpectPriorDigest {
		return preparedMutationSnapshot{}, coded(diag.CodeOwnershipGenerationConflict, m.Rel,
			fmt.Errorf("%w: expected prior digest %s, found %s", ErrGenerationConflict, m.ExpectPriorDigest, liveDigest))
	}

	stateBytes, stateGeneration, stateMode, snapErr := dependencytxn.ReadSnapshot(s.StateFile())
	if snapErr != nil {
		return preparedMutationSnapshot{}, fmt.Errorf("ownership: snapshot state record for prepare: %w", snapErr)
	}
	var current *Record
	if stateGeneration.Exists {
		rec, decErr := s.decodeStateRecord(s.StateFile(), stateBytes)
		if decErr != nil {
			return preparedMutationSnapshot{}, decErr
		}
		current = rec
	}
	var currentGeneration uint64
	if current != nil {
		currentGeneration = current.Generation
	}
	if currentGeneration != m.ExpectPriorGeneration {
		return preparedMutationSnapshot{}, coded(diag.CodeOwnershipGenerationConflict, m.Rel,
			fmt.Errorf("%w: expected prior generation %d, found %d", ErrGenerationConflict, m.ExpectPriorGeneration, currentGeneration))
	}
	return preparedMutationSnapshot{
		raw: raw, priorAbsent: priorAbsent, stateGeneration: stateGeneration, stateMode: stateMode,
		current: current, currentGeneration: currentGeneration,
	}, nil
}

func (s *Store) preparedMutationRecord(m Mutation, snapshot preparedMutationSnapshot, satisfactions *[]HookSatisfaction) (*Record, bool, error) {
	next, err := s.nextRecordMeta(snapshot.current, m.Rel, Digest(m.NextBytes), m.Entries, satisfactions)
	if err != nil {
		return nil, false, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
	}
	nextFile, nextPresent := next.Files[m.Rel]
	if snapshot.current == nil {
		if !nextPresent {
			return next, false, nil
		}
	} else {
		currentFile, currentPresent := snapshot.current.Files[m.Rel]
		if fileStateSemanticallyEqual(currentFile, currentPresent, nextFile, nextPresent) {
			return next, false, nil
		}
	}
	if err := validateRecord(next, s.canonicalRoot, s.rootKey); err != nil {
		return nil, false, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
	}
	return next, true, nil
}

func (s *Store) Prepare(m Mutation, satisfactions *[]HookSatisfaction) (dependencytxn.DesiredFile, error) {
	snapshot, err := s.preparedMutationSnapshot(m)
	if err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	next, changed, err := s.preparedMutationRecord(m, snapshot, satisfactions)
	if err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	if !changed {
		return dependencytxn.DesiredFile{}, nil
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("ownership: marshal record: %w", err)
	}
	data = append(data, '\n')
	return dependencytxn.DesiredFile{
		Path:         s.StateFile(),
		Expected:     snapshot.stateGeneration,
		ExpectedMode: snapshot.stateMode,
		Bytes:        data,
		Mode:         0o600,
	}, nil
}

func (s *Store) BackupPreparedMutation(m Mutation) (string, error) {
	var backupPath string
	err := s.withMutation("backup-prepared-mutation", func() error {
		snapshot, err := s.preparedMutationSnapshot(m)
		if err != nil {
			return err
		}
		emptySatisfactions := []HookSatisfaction{}
		if _, _, err := s.preparedMutationRecord(m, snapshot, &emptySatisfactions); err != nil {
			return err
		}
		backupPath, _, _, _, err = s.writeSafetyBackupSnapshot(m.Rel, m.AbsFile, snapshot.currentGeneration+1, m.Reason, m.Entries, snapshot.raw, snapshot.priorAbsent)
		if err != nil {
			return err
		}
		return s.pruneSafetyBackups(s.SafetyBackupDir(m.Rel))
	})
	return backupPath, err
}

func (s *Store) ensureRelNamesAbsFile(m Mutation) error {
	if err := s.ensureUnderRoot(m.AbsFile); err != nil {
		return err
	}
	canonicalDir, err := CanonicalRoot(filepath.Dir(m.AbsFile))
	if err != nil {
		return err
	}
	actual := filepath.Join(canonicalDir, filepath.Base(m.AbsFile))
	want := filepath.Join(s.canonicalRoot, filepath.FromSlash(m.Rel))
	if actual != want {
		return fmt.Errorf("ownership: mutation file %q does not name %s under the canonical root", m.Rel, m.AbsFile)
	}
	return nil
}
