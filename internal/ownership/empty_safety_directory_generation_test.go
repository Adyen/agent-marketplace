package ownership

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// spec: owned-entry-reconciliation/empty-safety-directory-cleanup-preserves-replacement-generation
func TestApplyEmptySafetyDirectoryCleanupPreservesReplacementGeneration(t *testing.T) {
	for _, replacement := range []string{"empty-directory", "alias", "nonempty-directory"} {
		t.Run(replacement, func(t *testing.T) {
			f := newFixture(t)
			f.writeAggregate(priorContent)
			store := f.store()
			nativeInfo, err := os.Stat(f.absFile)
			if err != nil {
				t.Fatal(err)
			}
			refused := errors.New("inert late policy refusal")
			checks, hit := 0, false
			retained, external := filepath.Join(t.TempDir(), "retained-empty-attempt-directory"), t.TempDir()
			var original, foreign, memberInfo os.FileInfo
			var directory, foreignPath string
			logger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(logger) })
			slog.SetDefault(slog.New(metadataPublicationObserver{Handler: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), emptySafetyCleanup: func(path string) {
				if hit {
					return
				}
				hit, directory = true, path
				var err error
				original, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
					t.Fatalf("actual attempt members were not cleaned before empty-directory selection: %v %v", entries, err)
				}
				if err := os.Rename(path, retained); err != nil {
					t.Fatal(err)
				}
				foreignPath = path
				if replacement == "alias" {
					foreignPath = external
					if err := os.Symlink(external, path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(path, 0o750); err != nil {
						t.Fatal(err)
					}
				}
				if replacement == "nonempty-directory" {
					member := filepath.Join(path, "foreign.txt")
					if err := os.WriteFile(member, []byte("foreign inert safety directory member"), 0o640); err != nil {
						t.Fatal(err)
					}
					memberInfo, err = os.Stat(member)
					if err != nil {
						t.Fatal(err)
					}
				}
				foreign, err = os.Stat(foreignPath)
				if err != nil {
					t.Fatal(err)
				}
			}}))
			result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent)), ValidatePolicy: func() error {
				checks++
				if checks > 1 {
					return refused
				}
				return nil
			}})
			if info, statErr := os.Stat(foreignPath); statErr != nil || !os.SameFile(foreign, info) || foreign.Mode() != info.Mode() || !foreign.ModTime().Equal(info.ModTime()) {
				t.Fatalf("empty safety cleanup deleted/changed replacement directory: hit=%t result=%+v err=%v stat=%v", hit, result, err, statErr)
			}
			if !hit || checks < 2 || result.Generation != 0 || !errors.Is(err, refused) {
				t.Fatalf("real late policy refusal did not reach cleanup: hit=%t checks=%d result=%+v err=%v", hit, checks, result, err)
			}
			if info, err := os.Stat(retained); err != nil || !os.SameFile(original, info) || original.Mode() != info.Mode() || !original.ModTime().Equal(info.ModTime()) {
				t.Fatalf("original empty attempt changed: %v", err)
			}
			expectedMembers := 0
			if replacement == "nonempty-directory" {
				expectedMembers = 1
			}
			if entries, err := os.ReadDir(foreignPath); err != nil || len(entries) != expectedMembers {
				t.Fatalf("foreign membership changed: %v %v", entries, err)
			}
			if entries, err := os.ReadDir(retained); err != nil || len(entries) != 0 {
				t.Fatalf("original empty membership changed: %v %v", entries, err)
			}
			if memberInfo != nil {
				member := filepath.Join(directory, "foreign.txt")
				if raw, err := os.ReadFile(member); err != nil || string(raw) != "foreign inert safety directory member" {
					t.Fatalf("foreign member bytes changed: %q %v", raw, err)
				}
				if info, err := os.Stat(member); err != nil || !os.SameFile(memberInfo, info) || memberInfo.Mode() != info.Mode() || !memberInfo.ModTime().Equal(info.ModTime()) {
					t.Fatalf("foreign member metadata changed: %v", err)
				}
			}
			if native, _ := f.readAggregate(); native != priorContent {
				t.Fatalf("policy refusal changed real native bytes: %q", native)
			}
			if info, err := os.Stat(f.absFile); err != nil || !os.SameFile(nativeInfo, info) || nativeInfo.Mode() != info.Mode() || !nativeInfo.ModTime().Equal(info.ModTime()) {
				t.Fatalf("policy refusal changed native metadata: %v", err)
			}
			if _, err := os.Lstat(store.JournalFile()); !os.IsNotExist(err) {
				t.Fatalf("real policy refusal left journal residue: %v", err)
			}
			if stages, err := filepath.Glob(f.absFile + ".am-staged-*"); err != nil || len(stages) != 0 {
				t.Fatalf("real policy refusal left stage residue: %v %v", stages, err)
			}
			if replacement == "alias" {
				if link, err := os.Readlink(directory); err != nil || link != external {
					t.Fatalf("foreign empty alias changed: %q %v", link, err)
				}
				if err := os.Remove(directory); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(directory, filepath.Join(t.TempDir(), "retained-foreign-directory")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Rename(retained, directory); err != nil {
				t.Fatal(err)
			}
			checks = 0
			if _, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent)), ValidatePolicy: func() error {
				checks++
				if checks > 1 {
					return refused
				}
				return nil
			}}); !errors.Is(err, refused) {
				t.Fatalf("healthy actual CAS cleanup retry: %v", err)
			}
			if _, err := os.Lstat(directory); !os.IsNotExist(err) {
				t.Fatalf("healthy retry did not genuinely remove original empty safety directory: %v", err)
			}
			if result, err := store.Apply(Mutation{Rel: f.rel, AbsFile: f.absFile, NextBytes: []byte(nextContent), Reason: ReasonSync, ExpectPriorDigest: Digest([]byte(priorContent))}); err != nil || result.Generation != 1 {
				t.Fatalf("genuine healthy Apply after cleanup did not commit: %+v %v", result, err)
			}
			if native, _ := f.readAggregate(); native != nextContent {
				t.Fatalf("healthy Apply missed desired native bytes: %q", native)
			}
		})
	}
}
