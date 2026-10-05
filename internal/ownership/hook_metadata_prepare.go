//go:build unix

package ownership

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func (s *Store) Prepare(m Mutation, satisfactions *[]HookSatisfaction) (dependencytxn.DesiredFile, error) {
	if err := validateMutation(m); err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	if err := s.ensureRelNamesAbsFile(m); err != nil {
		return dependencytxn.DesiredFile{}, err
	}
	for _, e := range m.Entries {
		if err := validateEntry(e); err != nil {
			return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
		}
	}

	raw, readErr := ReadAggregateFileAt(s.canonicalRoot, m.Rel)
	if readErr != nil && !os.IsNotExist(readErr) {
		return dependencytxn.DesiredFile{}, fmt.Errorf("ownership: read aggregate for prepare: %w", readErr)
	}
	liveDigest := ""
	if readErr == nil {
		liveDigest = Digest(raw)
	}
	if liveDigest != m.ExpectPriorDigest {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipGenerationConflict, m.Rel,
			fmt.Errorf("%w: expected prior digest %s, found %s", ErrGenerationConflict, m.ExpectPriorDigest, liveDigest))
	}

	stateBytes, stateGeneration, stateMode, snapErr := dependencytxn.ReadSnapshot(s.StateFile())
	if snapErr != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("ownership: snapshot state record for prepare: %w", snapErr)
	}
	var current *Record
	if stateGeneration.Exists {
		rec, decErr := s.decodeStateRecord(s.StateFile(), stateBytes)
		if decErr != nil {
			return dependencytxn.DesiredFile{}, decErr
		}
		current = rec
	}
	var currentGeneration uint64
	if current != nil {
		currentGeneration = current.Generation
	}
	if currentGeneration != m.ExpectPriorGeneration {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipGenerationConflict, m.Rel,
			fmt.Errorf("%w: expected prior generation %d, found %d", ErrGenerationConflict, m.ExpectPriorGeneration, currentGeneration))
	}

	next, err := s.nextRecordMeta(current, m.Rel, Digest(m.NextBytes), m.Entries, satisfactions)
	if err != nil {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
	}
	nextFile, nextPresent := next.Files[m.Rel]
	if current == nil {
		if !nextPresent {
			return dependencytxn.DesiredFile{}, nil
		}
	} else {
		currentFile, currentPresent := current.Files[m.Rel]
		if fileStateSemanticallyEqual(currentFile, currentPresent, nextFile, nextPresent) {
			return dependencytxn.DesiredFile{}, nil
		}
	}
	if err := validateRecord(next, s.canonicalRoot, s.rootKey); err != nil {
		return dependencytxn.DesiredFile{}, coded(diag.CodeOwnershipStateUnsafeValue, m.Rel, err)
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return dependencytxn.DesiredFile{}, fmt.Errorf("ownership: marshal record: %w", err)
	}
	data = append(data, '\n')
	return dependencytxn.DesiredFile{
		Path:         s.StateFile(),
		Expected:     stateGeneration,
		ExpectedMode: stateMode,
		Bytes:        data,
		Mode:         0o600,
	}, nil
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
