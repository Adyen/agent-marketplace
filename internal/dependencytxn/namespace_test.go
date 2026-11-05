package dependencytxn

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func namespaceObjectID(t *testing.T, path string, follow bool) (uint64, uint64) {
	t.Helper()
	var info os.FileInfo
	var err error
	if follow {
		info, err = os.Stat(path)
	} else {
		info, err = os.Lstat(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat payload for %s", path)
	}
	return uint64(stat.Dev), stat.Ino
}

func namespaceFor(t *testing.T, scope, path string) *NamespaceExpectation {
	t.Helper()
	scopeDevice, scopeInode := namespaceObjectID(t, scope, true)
	parentDevice, parentInode := namespaceObjectID(t, filepath.Dir(path), false)
	return &NamespaceExpectation{
		ScopePath: filepath.Clean(scope), ScopeDevice: scopeDevice, ScopeInode: scopeInode,
		ParentDevice: parentDevice, ParentInode: parentInode,
	}
}

func replaceNamespaceParent(t *testing.T, parent, path string, data []byte, mode os.FileMode) string {
	t.Helper()
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	retained := parent + "-retained"
	if err := os.Rename(parent, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode.Perm()); err != nil {
		t.Fatal(err)
	}
	return retained
}

func replaceNamespaceScope(t *testing.T, scope, path string, data []byte, mode os.FileMode) string {
	t.Helper()
	info, err := os.Stat(scope)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(scope, path)
	if err != nil {
		t.Fatal(err)
	}
	retained := scope + "-retained"
	if err := os.Rename(scope, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(scope, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scope, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(scope, rel)
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clone, data, mode.Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(clone, mode.Perm()); err != nil {
		t.Fatal(err)
	}
	return retained
}

func TestCommitNamespaceExpectationExistingAndAbsentLeaves(t *testing.T) {
	for _, existing := range []bool{true, false} {
		name := "absent-leaf"
		if existing {
			name = "existing-leaf"
		}
		t.Run(name, func(t *testing.T) {
			repo, agents, state := repoLayer(t)
			path := filepath.Join(agents, "materialization-manifest.yaml")
			old := []byte("version: 1\nentries: []\n")
			expectedMode := os.FileMode(0)
			if existing {
				mustWrite(t, path, old, 0o600)
				expectedMode = 0o600
			}
			namespace := namespaceFor(t, repo, path)
			want := []byte("version: 1\nentries:\n  - target: factory\n    path: hooks/scripts/check.sh\n")
			if _, err := Commit(t.Context(), state, []DesiredFile{{
				Path: path, Expected: generation(t, path), ExpectedMode: expectedMode,
				Namespace: namespace, Bytes: want, Mode: 0o600,
			}}); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			assertFile(t, path, string(want), 0o600)
			if err := CheckPending(state); err != nil {
				t.Fatalf("pending journal: %v", err)
			}
		})
	}
}

func TestCommitNamespaceExpectationRejectsParentCloneAfterCAS(t *testing.T) {
	repo, agents, state := repoLayer(t)
	path := filepath.Join(agents, "materialization-manifest.yaml")
	old := []byte("version: 1\nentries: []\n")
	want := []byte("version: 1\nentries:\n  - target: factory\n    path: hooks/scripts/check.sh\n")
	mustWrite(t, path, old, 0o600)
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	namespace := namespaceFor(t, repo, path)
	var retained string
	_, err = Commit(t.Context(), state, []DesiredFile{{
		Path: path, Expected: generation(t, path), ExpectedMode: 0o600,
		Namespace: namespace, Bytes: want, Mode: 0o600,
	}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterCAS {
			retained = replaceNamespaceParent(t, agents, path, old, 0o600)
		}
		return nil
	}))
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Commit after parent clone = %v, want ErrGenerationConflict", err)
	}
	assertFile(t, path, string(old), 0o600)
	assertFile(t, filepath.Join(retained, filepath.Base(path)), string(old), 0o600)
	cloneInfo, err := os.Stat(path)
	if err != nil || os.SameFile(originalInfo, cloneInfo) {
		t.Fatalf("parent replacement did not create a clone: %v", err)
	}
	assertNoStagingResidue(t, agents)
	assertNoStagingResidue(t, retained)
	assertNoTransactionResidue(t, repo, state)
}

func TestCommitNamespaceExpectationRejectsScopeCloneAfterCAS(t *testing.T) {
	repo, _, state := repoLayer(t)
	path := filepath.Join(repo, ".agents", "materialization-manifest.yaml")
	old := []byte("version: 1\nentries: []\n")
	want := []byte("version: 1\nentries: [{target: factory}]\n")
	mustWrite(t, path, old, 0o600)
	namespace := namespaceFor(t, repo, path)
	var retained string
	_, err := Commit(t.Context(), state, []DesiredFile{{
		Path: path, Expected: generation(t, path), ExpectedMode: 0o600,
		Namespace: namespace, Bytes: want, Mode: 0o600,
	}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterCAS {
			retained = replaceNamespaceScope(t, repo, path, old, 0o600)
		}
		return nil
	}))
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Commit after scope clone = %v, want ErrGenerationConflict", err)
	}
	assertFile(t, path, string(old), 0o600)
	assertFile(t, filepath.Join(retained, ".agents", filepath.Base(path)), string(old), 0o600)
	assertNoTransactionResidue(t, repo, state)
}

func TestCommitNamespaceExpectationRejectsInvalidCombinations(t *testing.T) {
	t.Run("zero-scope-inode", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		path := filepath.Join(agents, "manifest.yaml")
		mustWrite(t, path, []byte("old\n"), 0o600)
		namespace := namespaceFor(t, repo, path)
		namespace.ScopeInode = 0
		_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("zero scope inode = %v, want ErrUnsafePath", err)
		}
	})
	t.Run("device-zero-is-not-invalid", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		path := filepath.Join(agents, "manifest.yaml")
		mustWrite(t, path, []byte("old\n"), 0o600)
		device, _ := namespaceObjectID(t, repo, true)
		namespace := namespaceFor(t, repo, path)
		namespace.ScopeDevice = 0
		_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600}})
		if errors.Is(err, ErrUnsafePath) {
			t.Fatalf("zero scope device was rejected as malformed: %v", err)
		}
		if device != 0 && !errors.Is(err, ErrGenerationConflict) {
			t.Fatalf("zero scope device mismatch = %v, want ErrGenerationConflict", err)
		}
	})
	t.Run("scope-does-not-contain-path", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		path := filepath.Join(agents, "manifest.yaml")
		mustWrite(t, path, []byte("old\n"), 0o600)
		namespace := namespaceFor(t, repo, path)
		namespace.ScopePath = t.TempDir()
		_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("out-of-scope participant = %v, want ErrUnsafePath", err)
		}
	})
	t.Run("managed-config", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		path := filepath.Join(agents, "config.yaml")
		mustWrite(t, path, []byte("old\n"), 0o600)
		namespace := namespaceFor(t, repo, path)
		_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("managed config with namespace = %v, want ErrUnsafePath", err)
		}
	})
	t.Run("expected-link-target", func(t *testing.T) {
		repo, state := testDirs(t)
		target := filepath.Join(repo, "target")
		path := filepath.Join(repo, "manifest-link")
		mustWrite(t, target, []byte("old\n"), 0o600)
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		namespace := namespaceFor(t, repo, path)
		_, err := Commit(t.Context(), state, []DesiredFile{{
			Path: path, Expected: Generation{Exists: true, Digest: Digest([]byte(target))}, ExpectedLinkTarget: target,
			Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600,
		}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("link participant with namespace = %v, want ErrUnsafePath", err)
		}
		assertFile(t, target, "old\n", 0o600)
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("link changed after refusal: %v", err)
		}
	})
	t.Run("symlink-parent", func(t *testing.T) {
		repo, agents, state := repoLayer(t)
		retained := agents + "-real"
		if err := os.Rename(agents, retained); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(retained, agents); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(agents, "manifest.yaml")
		mustWrite(t, filepath.Join(retained, "manifest.yaml"), []byte("old\n"), 0o600)
		namespace := namespaceFor(t, repo, path)
		_, err := Commit(t.Context(), state, []DesiredFile{{Path: path, Expected: generation(t, path), Namespace: namespace, Bytes: []byte("new\n"), Mode: 0o600}})
		if !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("symlink parent = %v, want ErrUnsafePath", err)
		}
		assertFile(t, filepath.Join(retained, "manifest.yaml"), "old\n", 0o600)
	})
}

func TestCommitNamespaceAssertionRefusesCloneBeforeOtherPublication(t *testing.T) {
	repo, agents, state := repoLayer(t)
	manifestPath := filepath.Join(agents, "materialization-manifest.yaml")
	outputPath := filepath.Join(repo, "z-output.txt")
	oldManifest := []byte("version: 1\nentries: []\n")
	mustWrite(t, manifestPath, oldManifest, 0o600)
	namespace := namespaceFor(t, repo, manifestPath)
	var retained string
	_, err := Commit(t.Context(), state, []DesiredFile{
		{Path: manifestPath, Expected: generation(t, manifestPath), ExpectedMode: 0o600, Namespace: namespace, Bytes: oldManifest, Mode: 0o600},
		{Path: outputPath, Expected: Generation{}, Bytes: []byte("new output\n"), Mode: 0o600},
	}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultBeforePublish && point.Path == outputPath {
			retained = replaceNamespaceParent(t, agents, manifestPath, oldManifest, 0o600)
		}
		return nil
	}))
	if !errors.Is(err, ErrRecoveryBlocked) && !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Commit after assertion namespace replacement = %v, want refusal", err)
	}
	if _, err := os.Lstat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("other participant published despite namespace refusal: %v", err)
	}
	assertFile(t, manifestPath, string(oldManifest), 0o600)
	assertFile(t, filepath.Join(retained, filepath.Base(manifestPath)), string(oldManifest), 0o600)
	if err := CheckPending(state); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("pending assertion transaction = %v, want ErrRecoveryRequired", err)
	}
	if err := os.RemoveAll(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, agents); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), state); err != nil {
		t.Fatalf("Recover after restoring the namespace: %v", err)
	}
	assertFile(t, outputPath, "new output\n", 0o600)
	assertNoTransactionResidue(t, repo, state)
}

func TestRecoverNamespaceExpectationRefusesParentCloneAndCompletesAfterRestore(t *testing.T) {
	repo, agents, state := repoLayer(t)
	path := filepath.Join(agents, "materialization-manifest.yaml")
	old := []byte("version: 1\nentries: []\n")
	updated := []byte("version: 1\nentries: [{target: factory}]\n")
	mustWrite(t, path, old, 0o600)
	namespace := namespaceFor(t, repo, path)
	interrupted := errors.New("interrupted after journal")
	_, err := Commit(t.Context(), state, []DesiredFile{{
		Path: path, Expected: generation(t, path), ExpectedMode: 0o600,
		Namespace: namespace, Bytes: updated, Mode: 0o600,
	}}, WithFaultHook(func(point FaultPoint) error {
		if point.Stage == FaultAfterJournal {
			return interrupted
		}
		return nil
	}))
	if !errors.Is(err, interrupted) {
		t.Fatalf("Commit interruption = %v, want sentinel", err)
	}
	pending := journals(t, state)
	if len(pending) != 1 {
		t.Fatalf("pending journals = %v, want one", pending)
	}
	journal, err := readJournal(pending[0])
	if err != nil {
		t.Fatal(err)
	}
	if journal.Version != journalVersion || len(journal.Files) != 1 || journal.Files[0].Namespace == nil || *journal.Files[0].Namespace != *namespace {
		t.Errorf("journal namespace = %+v, version=%d", journal.Files, journal.Version)
	}
	retained := replaceNamespaceParent(t, agents, path, old, 0o600)
	stageName := filepath.Base(journal.Files[0].Temp)
	stagedOld := filepath.Join(retained, stageName)
	stagedBytes, err := os.ReadFile(stagedOld)
	if err != nil {
		t.Fatal(err)
	}
	stagedInfo, err := os.Stat(stagedOld)
	if err != nil {
		t.Fatal(err)
	}
	stagedClone := filepath.Join(agents, stageName)
	if err := os.WriteFile(stagedClone, stagedBytes, stagedInfo.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stagedClone, stagedInfo.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	recoverErr := Recover(t.Context(), state)
	if !errors.Is(recoverErr, ErrRecoveryBlocked) && !errors.Is(recoverErr, ErrGenerationConflict) {
		t.Fatalf("Recover after parent clone = %v, want refusal", recoverErr)
	}
	assertFile(t, path, string(old), 0o600)
	assertFile(t, filepath.Join(retained, filepath.Base(path)), string(old), 0o600)
	assertFile(t, stagedClone, string(stagedBytes), stagedInfo.Mode().Perm())
	if len(journals(t, state)) != 1 {
		t.Fatal("namespace refusal removed recovery journal")
	}
	if err := os.RemoveAll(agents); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(retained, agents); err != nil {
		t.Fatal(err)
	}
	if err := Recover(t.Context(), state); err != nil {
		t.Fatalf("Recover after restoring captured parent: %v", err)
	}
	assertFile(t, path, string(updated), 0o600)
	assertNoTransactionResidue(t, repo, state)
}

func TestRecoverNamespaceExpectationCompletesPrefixAndRefusesForeignLaterFile(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		name := "unchanged-namespace"
		if foreign {
			name = "foreign-unpublished-participant"
		}
		t.Run(name, func(t *testing.T) {
			repo, agents, state := repoLayer(t)
			manifestPath := filepath.Join(agents, "materialization-manifest.yaml")
			outputPath := filepath.Join(repo, "z-output.txt")
			oldManifest := []byte("version: 1\nentries: []\n")
			newManifest := []byte("version: 1\nentries: [{target: factory}]\n")
			mustWrite(t, manifestPath, oldManifest, 0o600)
			namespace := namespaceFor(t, repo, manifestPath)
			interrupted := errors.New("interrupted after first publish")
			_, err := Commit(t.Context(), state, []DesiredFile{
				{Path: manifestPath, Expected: generation(t, manifestPath), ExpectedMode: 0o600, Namespace: namespace, Bytes: newManifest, Mode: 0o600},
				{Path: outputPath, Expected: Generation{}, Bytes: []byte("new output\n"), Mode: 0o600},
			}, WithFaultHook(func(point FaultPoint) error {
				if point.Stage == FaultAfterPublish && point.Index == 0 {
					if foreign {
						if err := os.WriteFile(outputPath, []byte("foreign output\n"), 0o600); err != nil {
							return err
						}
						if err := os.Chmod(outputPath, 0o600); err != nil {
							return err
						}
					}
					return interrupted
				}
				return nil
			}))
			if !errors.Is(err, interrupted) {
				t.Fatalf("Commit interruption = %v, want sentinel", err)
			}
			assertFile(t, manifestPath, string(newManifest), 0o600)
			if foreign {
				recoverErr := Recover(t.Context(), state)
				if !errors.Is(recoverErr, ErrRecoveryBlocked) {
					t.Fatalf("Recover with foreign later file = %v, want ErrRecoveryBlocked", recoverErr)
				}
				assertFile(t, outputPath, "foreign output\n", 0o600)
				if err := os.Remove(outputPath); err != nil {
					t.Fatal(err)
				}
			} else if err := Recover(t.Context(), state); err != nil {
				t.Fatalf("Recover published prefix: %v", err)
			}
			if foreign {
				if err := Recover(t.Context(), state); err != nil {
					t.Fatalf("Recover after removing foreign file: %v", err)
				}
			}
			assertFile(t, outputPath, "new output\n", 0o600)
			assertNoTransactionResidue(t, repo, state)
		})
	}
}

func TestNamespaceExpectationDoesNotBumpJournalShape(t *testing.T) {
	j := journal{Version: journalVersion, ID: "old", Files: []journalFile{{Path: "/repo/file", Temp: "/repo/.dependencytxn-temp", Old: Generation{}, NewDigest: Digest([]byte("new")), NewMode: 0o600}}}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var decoded journal
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != journalVersion || decoded.Files[0].Namespace != nil {
		t.Fatalf("old journal fields changed: %+v", decoded)
	}
}
