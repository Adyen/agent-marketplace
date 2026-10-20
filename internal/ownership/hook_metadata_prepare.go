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

type StateSnapshot struct {
	record     *Record
	generation dependencytxn.Generation
	mode       fs.FileMode
	rootKey    string
}

func (s *Store) SnapshotState() (StateSnapshot, error) {
	data, generation, mode, err := dependencytxn.ReadSnapshot(s.StateFile())
	if err != nil {
		return StateSnapshot{}, err
	}
	snapshot := StateSnapshot{generation: generation, mode: mode, rootKey: s.rootKey}
	if generation.Exists {
		record, err := s.decodeStateRecord(s.StateFile(), data)
		if err != nil {
			return StateSnapshot{}, err
		}
		snapshot.record = record
	}
	return snapshot, nil
}

func (s *Store) ProofFromSnapshot(snapshot StateSnapshot, rel string) (Proof, error) {
	if snapshot.rootKey != s.rootKey {
		return Proof{}, ErrStateRootMismatch
	}
	if snapshot.record == nil {
		err := s.missingStateError()
		return Proof{}, coded(diag.CodeOwnershipStateMissing, s.StateFile(), err)
	}
	file, ok := snapshot.record.Files[rel]
	if !ok {
		return Proof{file: rel, generation: snapshot.record.Generation}, nil
	}
	entries := make(map[string]Entry, len(file.Entries))
	for _, entry := range file.Entries {
		entries[entry.ID().mapKey()] = entry
	}
	return Proof{
		file:              rel,
		generation:        snapshot.record.Generation,
		contentDigest:     file.ContentDigest,
		entries:           entries,
		hookSatisfactions: append([]HookSatisfaction(nil), file.HookSatisfactions...),
	}, nil
}

func (s *Store) PrepareBatch(snapshot StateSnapshot, mutations []Mutation, satisfactions map[string][]HookSatisfaction) (dependencytxn.DesiredFile, error) {
	if snapshot.rootKey != s.rootKey {
		return dependencytxn.DesiredFile{}, ErrStateRootMismatch
	}
	if len(mutations) == 0 {
		return dependencytxn.DesiredFile{}, nil
	}
	baseGeneration := uint64(0)
	var current *Record
	if snapshot.record != nil {
		current = cloneRecord(snapshot.record)
		baseGeneration = snapshot.record.Generation
	}
	seen := make(map[string]bool, len(mutations))
	changed := false
	for _, mutation := range mutations {
		if err := validateMutation(mutation); err != nil {
			return dependencytxn.DesiredFile{}, err
		}
		if err := s.ensureRelNamesAbsFile(mutation); err != nil {
			return dependencytxn.DesiredFile{}, err
		}
		if seen[mutation.Rel] {
			return dependencytxn.DesiredFile{}, fmt.Errorf("%w: duplicate aggregate mutation %q", ErrUnsafeStateValue, mutation.Rel)
		}
		seen[mutation.Rel] = true
		if mutation.ExpectPriorGeneration != baseGeneration {
			return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipGenerationConflict, mutation.Rel,
				fmt.Errorf("%w: expected prior generation %d, found %d", ErrGenerationConflict, mutation.ExpectPriorGeneration, baseGeneration))
		}
		for _, entry := range mutation.Entries {
			if err := validateEntry(entry); err != nil {
				return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, mutation.Rel, err)
			}
		}
		var satisfactionList *[]HookSatisfaction
		if values, ok := satisfactions[mutation.Rel]; ok {
			copy := append([]HookSatisfaction(nil), values...)
			satisfactionList = &copy
		}
		next, err := s.nextRecordMeta(current, mutation.Rel, Digest(mutation.NextBytes), mutation.Entries, satisfactionList)
		if err != nil {
			return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, mutation.Rel, err)
		}
		next.Generation = baseGeneration + 1
		if current == nil {
			changed = changed || len(next.Files) > 0
		} else {
			prior, priorPresent := current.Files[mutation.Rel]
			nextFile, nextPresent := next.Files[mutation.Rel]
			changed = changed || !fileStateSemanticallyEqual(prior, priorPresent, nextFile, nextPresent)
		}
		current = next
	}
	if !changed {
		return dependencytxn.DesiredFile{}, nil
	}
	if current == nil {
		return dependencytxn.DesiredFile{}, nil
	}
	current.Generation = baseGeneration + 1
	if err := validateRecord(current, s.canonicalRoot, s.rootKey); err != nil {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, "", err)
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("ownership: marshal batch record: %w", err)
	}
	data = append(data, '\n')
	return dependencytxn.DesiredFile{
		Path:         s.StateFile(),
		Expected:     snapshot.generation,
		ExpectedMode: snapshot.mode,
		Bytes:        data,
		Mode:         0o600,
	}, nil
}

func cloneRecord(record *Record) *Record {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Files = make(map[string]FileState, len(record.Files))
	for rel, file := range record.Files {
		file.Entries = append([]Entry(nil), file.Entries...)
		for i := range file.Entries {
			file.Entries[i].SourceEvidence = append([]HookSourceEvidence(nil), file.Entries[i].SourceEvidence...)
		}
		file.HookSatisfactions = append([]HookSatisfaction(nil), file.HookSatisfactions...)
		for i := range file.HookSatisfactions {
			file.HookSatisfactions[i].Sources = append([]HookSourceEvidence(nil), file.HookSatisfactions[i].Sources...)
		}
		clone.Files[rel] = file
	}
	return &clone
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

func (s *Store) BackupPreparedMutationSnapshot(m Mutation, snapshot StateSnapshot, raw []byte, priorAbsent bool) (string, error) {
	if snapshot.rootKey != s.rootKey {
		return "", ErrStateRootMismatch
	}
	if err := validateMutation(m); err != nil {
		return "", err
	}
	if err := s.ensureRelNamesAbsFile(m); err != nil {
		return "", err
	}
	if snapshot.record != nil && snapshot.record.Generation != m.ExpectPriorGeneration ||
		snapshot.record == nil && m.ExpectPriorGeneration != 0 {
		return "", coded(diag.CodeOwnershipGenerationConflict, m.Rel, ErrGenerationConflict)
	}
	digest := ""
	if !priorAbsent {
		digest = Digest(raw)
	}
	if digest != m.ExpectPriorDigest {
		return "", coded(diag.CodeOwnershipGenerationConflict, m.Rel, ErrGenerationConflict)
	}
	emptySatisfactions := []HookSatisfaction{}
	if _, _, err := s.preparedMutationRecord(m, preparedMutationSnapshot{
		raw: raw, priorAbsent: priorAbsent, stateGeneration: snapshot.generation,
		stateMode: snapshot.mode, current: snapshot.record, currentGeneration: m.ExpectPriorGeneration,
	}, &emptySatisfactions); err != nil {
		return "", err
	}
	var backupPath string
	err := s.withMutation("backup-prepared-mutation", func() error {
		var err error
		backupPath, _, _, _, err = s.writeSafetyBackupSnapshot(
			m.Rel, m.AbsFile, m.ExpectPriorGeneration+1, m.Reason, m.Entries, raw, priorAbsent,
		)
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
