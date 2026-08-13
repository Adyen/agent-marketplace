package ownership

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// classify is a probe helper: it reports which absence flavour Load produced.
func classify(t *testing.T, st *Store) (code string, uninitialized bool, missing bool, corrupt bool) {
	t.Helper()
	rec, err := st.Load()
	if err == nil {
		t.Fatalf("Load unexpectedly succeeded: %+v", rec)
	}
	if rec != nil {
		t.Fatalf("Load returned a record alongside an error: %+v", rec)
	}
	if c, ok := diag.AsCodedError(err); ok {
		code = c.Code
	}
	return code, errors.Is(err, ErrStateUninitialized), errors.Is(err, ErrStateMissing), errors.Is(err, ErrStateCorrupt)
}

// TestAbsenceFlavoursAreNeverMisclassifiedAsFirstUse walks every way a record
// can be absent or unusable and pins that ONLY a genuine first use is downgraded
// to ErrStateUninitialized. Anything that had, or could have had, a record must
// keep a user-facing report.
func TestAbsenceFlavoursAreNeverMisclassifiedAsFirstUse(t *testing.T) {
	// damage runs after one committed generation, unless firstUse is set.
	cases := []struct {
		name          string
		firstUse      bool
		damage        func(t *testing.T, f *fixture, st *Store)
		wantFirstUse  bool
		wantCode      string
		skipOnWindows bool
		skipWhenRoot  bool
	}{
		{
			name:         "pristine first use",
			firstUse:     true,
			damage:       func(*testing.T, *fixture, *Store) {},
			wantFirstUse: true,
			wantCode:     diag.CodeOwnershipStateMissing,
		},
		{
			name: "state.json deleted, record dir left",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.Remove(st.StateFile()); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateMissing,
		},
		{
			name: "record dir deleted, safety backups left",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.RemoveAll(st.recordDir()); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateMissing,
		},
		{
			name: "safety backups deleted, record dir left",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.RemoveAll(st.safetyRootDir()); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(st.StateFile()); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateMissing,
		},
		{
			name: "state.json truncated to zero bytes",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.WriteFile(st.StateFile(), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "state.json replaced by a directory",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.Remove(st.StateFile()); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(st.StateFile(), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "record dir replaced by a file",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.RemoveAll(st.recordDir()); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(st.recordDir(), []byte("not a dir"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name:          "record dir unreadable",
			skipOnWindows: true,
			skipWhenRoot:  true,
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.Chmod(st.recordDir(), 0o000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(st.recordDir(), 0o700) })
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name:          "record dir is a symlink loop",
			skipOnWindows: true,
			damage: func(t *testing.T, _ *fixture, st *Store) {
				if err := os.RemoveAll(st.recordDir()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(st.recordDir(), st.recordDir()); err != nil {
					t.Fatal(err)
				}
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "record with the wrong canonical root",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				rewriteRecord(t, st, func(r *Record) { r.CanonicalRoot = r.CanonicalRoot + "-elsewhere" })
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateRootMismatch,
		},
		{
			name: "record with an unknown schema version",
			damage: func(t *testing.T, _ *fixture, st *Store) {
				rewriteRecord(t, st, func(r *Record) { r.Version = 99 })
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "record with a structurally invalid entry",
			damage: func(t *testing.T, f *fixture, st *Store) {
				rewriteRecord(t, st, func(r *Record) {
					fs := r.Files[f.rel]
					fs.Entries[0].ValueDigest = "not-a-digest"
					r.Files[f.rel] = fs
				})
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
		{
			name: "record with an absolute file key",
			damage: func(t *testing.T, f *fixture, st *Store) {
				rewriteRecord(t, st, func(r *Record) {
					r.Files["/etc/passwd"] = r.Files[f.rel]
				})
			},
			wantFirstUse: false,
			wantCode:     diag.CodeOwnershipStateCorrupt,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("permission and symlink semantics differ on windows")
			}
			if tc.skipWhenRoot && os.Geteuid() == 0 {
				t.Skip("root ignores directory read permissions")
			}
			f := newFixture(t)
			f.writeAggregate(priorContent)
			st := f.store()
			if !tc.firstUse {
				entry := mcpEntry("generated", "generated-server", `{"command":"new"}`)
				mustApply(t, st, Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent),
					Entries: []Entry{entry}, Reason: ReasonSync})
			}
			tc.damage(t, f, st)

			code, uninit, missing, _ := classify(t, st)
			if uninit != tc.wantFirstUse {
				t.Fatalf("ErrStateUninitialized = %v, want %v (code %q)", uninit, tc.wantFirstUse, code)
			}
			if code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
			if tc.wantFirstUse && !missing {
				t.Fatalf("a first-use absence stopped matching ErrStateMissing")
			}

			// Fail-closed, in every flavour.
			proof, _ := st.Proof(f.rel)
			if proof.OwnedCount() != 0 {
				t.Fatalf("proof claims %d owned entries when state is unusable", proof.OwnedCount())
			}
			id := EntryID{Kind: KindMcpServer, Container: "mcpServers", Key: "generated",
				IdentityDigest: McpIdentityDigest("generated")}
			if proof.Classify(id, Digest([]byte(`{"command":"new"}`))) != StatusUnmanaged {
				t.Fatal("an entry was adopted while ownership state was unusable")
			}
		})
	}
}

func rewriteRecord(t *testing.T, st *Store, mutate func(*Record)) {
	t.Helper()
	data, err := os.ReadFile(st.StateFile())
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	mutate(&rec)
	out, err := json.Marshal(&rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.StateFile(), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHostileStateRootIsNeverAFirstUse covers the state ROOT itself being
// unusable rather than the record under it: a state root that is a regular
// file, and one that cannot be traversed. Neither is a first use, and neither
// may be silent.
func TestHostileStateRootIsNeverAFirstUse(t *testing.T) {
	t.Run("state root is a file", func(t *testing.T) {
		base := t.TempDir()
		targetRoot := filepath.Join(base, "repo", ".devin")
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		stateRoot := filepath.Join(base, "state-file")
		if err := os.WriteFile(stateRoot, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := Open(targetRoot, WithStateRoot(stateRoot))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, loadErr := st.Load(); !errors.Is(loadErr, ErrStateCorrupt) {
			t.Fatalf("a state root that is a FILE must be corrupt, not a first use: %v", loadErr)
		}
	})

	t.Run("state root cannot be traversed", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("permission semantics")
		}
		base := t.TempDir()
		targetRoot := filepath.Join(base, "repo", ".devin")
		if err := os.MkdirAll(targetRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		stateRoot := filepath.Join(base, "state")
		if err := os.MkdirAll(filepath.Join(stateRoot, ownershipDirName), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(stateRoot, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(stateRoot, 0o700) })

		st, err := Open(targetRoot, WithStateRoot(stateRoot))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, loadErr := st.Load(); errors.Is(loadErr, ErrStateUninitialized) {
			t.Fatalf("an unreadable state root was reported as a first use: %v", loadErr)
		}
	})
}

// TestStateRootIgnoresUnusableXdgValues pins that an unset, empty or relative
// XDG_STATE_HOME falls back to the home default rather than inventing a new
// (and therefore always-first-use) state root next to the process CWD.
func TestStateRootIgnoresUnusableXdgValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".local", "state", stateDirName)
	for _, xdg := range []string{"", "relative/state", "./state"} {
		t.Setenv("XDG_STATE_HOME", xdg)
		got, err := StateRoot()
		if err != nil {
			t.Fatalf("StateRoot with XDG_STATE_HOME=%q: %v", xdg, err)
		}
		if got != want {
			t.Fatalf("StateRoot with XDG_STATE_HOME=%q = %q, want %q", xdg, got, want)
		}
	}
}

// TestEverRecordedSurvivesAnEmptiedSafetyRoot probes the second evidence source
// when a user (or a cleanup script) deletes backup FILES but leaves the
// directories.
func TestEverRecordedSurvivesAnEmptiedSafetyRoot(t *testing.T) {
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	if _, err := st.BackupOnly(f.rel, f.absFile, ReasonSync); err != nil {
		t.Fatalf("BackupOnly: %v", err)
	}
	if err := os.RemoveAll(st.recordDir()); err != nil {
		t.Fatal(err)
	}
	// Delete every backup FILE, keep the per-file directory.
	dir := st.SafetyBackupDir(f.rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	if !st.EverRecorded() {
		t.Fatal("an emptied but present safety-backup directory stopped counting as durable evidence")
	}
}

// TestEverRecordedWithAnUnreadableSafetyRoot pins what happens when the safety
// root exists with entries but cannot be read.
func TestEverRecordedWithAnUnreadableSafetyRoot(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission semantics")
	}
	f := newFixture(t)
	f.writeAggregate(priorContent)
	st := f.store()
	if _, err := st.BackupOnly(f.rel, f.absFile, ReasonSync); err != nil {
		t.Fatalf("BackupOnly: %v", err)
	}
	if err := os.RemoveAll(st.recordDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(st.safetyRootDir(), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(st.safetyRootDir(), 0o700) })

	if !st.EverRecorded() {
		t.Fatal("a safety-backup root that exists but cannot be read was read as 'never recorded'; " +
			"a root that WAS managed is now reported as a first use and its lost record is silent")
	}
}
