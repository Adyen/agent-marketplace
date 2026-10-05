package materialize

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestOwnedPortableRemovalProofRefusal(t *testing.T) {
	for _, refusal := range []string{"absent proof", "wrong proof", "modified bytes", "linked payload"} {
		t.Run(refusal, func(t *testing.T) {
			parent, destination, state := installedReadonlyRemovalFixture(t)
			proof := state.entries[portableManifestKey(hcc.TargetFactory, "skills/probe")].Digest
			observed, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			external := t.TempDir()
			file := filepath.Join(destination, "SKILL.md")
			switch refusal {
			case "absent proof":
				proof = ""
			case "wrong proof":
				proof = "sha256:untrusted"
			case "modified bytes":
				changePortableModeSourceBody(t, destination, "handwritten replacement")
			case "linked payload":
				if err := os.Link(file, filepath.Join(external, "linked.md")); err != nil {
					t.Fatal(err)
				}
			}
			before := portableModeSnapshot(t, destination)
			dir, err := fsboundary.OpenDir(filepath.Dir(destination))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := dir.Close(); err != nil {
					t.Error(err)
				}
			}()
			native, err := dir.NativeRoot()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := native.Close(); err != nil {
					t.Error(err)
				}
			}()
			err = removeVerifiedPortableOutput(dir, native, filepath.Base(destination), observed, proof)
			want := fsboundary.ErrGenerationConflict
			if refusal == "linked payload" {
				want = fsboundary.ErrUnsafeFile
			}
			if !errors.Is(err, want) {
				t.Fatalf("unproven removal accepted: %v", err)
			}
			assertPortableModeSnapshot(t, destination, before)
			if refusal == "modified bytes" {
				assertCleanupBytes(t, file, "handwritten replacement")
			} else {
				assertCleanupBytes(t, file, "original payload")
			}
			assertNoPortableTreeStage(t, filepath.Join(parent, "skills"))
		})
	}
}

func TestOwnedPortableRemovalRevalidatesAndRestoresModes(t *testing.T) {
	for _, mutation := range []string{"root before", "root after", "nested alias after", "nested directory after"} {
		t.Run(mutation, func(t *testing.T) {
			_, destination, state := installedReadonlyRemovalFixture(t)
			proof := state.entries[portableManifestKey(hcc.TargetFactory, "skills/probe")].Digest
			observed, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			before := portableModeSnapshot(t, destination)
			external := portableModeSource(t, 0o510, 0o555)
			externalBefore := portableModeSnapshot(t, external)
			retained := filepath.Join(filepath.Dir(destination), "retained")
			resetPortableModeTreeOnCleanup(t, retained)
			dir, err := fsboundary.OpenDir(filepath.Dir(destination))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := dir.Close(); err != nil {
					t.Error(err)
				}
			}()
			native, err := dir.NativeRoot()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := native.Close(); err != nil {
					t.Error(err)
				}
			}()
			var replacementBefore map[string]os.FileInfo
			hit := false
			mutate := func() {
				hit = true
				if mutation == "root before" || mutation == "root after" {
					if err := os.Rename(destination, retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, destination); err != nil {
						t.Fatal(err)
					}
					return
				}
				if err := os.Rename(filepath.Join(destination, "nested"), filepath.Join(filepath.Dir(destination), "retained-nested")); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					for _, name := range []string{"retained-nested", "retained-nested/deeper"} {
						if err := os.Chmod(filepath.Join(filepath.Dir(destination), filepath.FromSlash(name)), 0o700); err != nil {
							t.Error(err)
						}
					}
				})
				if mutation == "nested alias after" {
					if err := os.Symlink(external, filepath.Join(destination, "nested")); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Mkdir(filepath.Join(destination, "nested"), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(destination, "nested", "user.md"), []byte("foreign subtree"), 0o444); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(filepath.Join(destination, "nested"), 0o510); err != nil {
						t.Fatal(err)
					}
					replacementBefore = portableModeSnapshot(t, filepath.Join(destination, "nested"))
					t.Cleanup(func() {
						if err := os.Chmod(filepath.Join(destination, "nested"), 0o700); err != nil {
							t.Error(err)
						}
					})
				}
			}
			hooks := []func(){func() {}, mutate}
			if mutation == "root before" {
				hooks = []func(){mutate}
			}
			err = removeVerifiedPortableOutput(dir, native, filepath.Base(destination), observed, proof, hooks...)
			if !hit || !errors.Is(err, fsboundary.ErrGenerationConflict) {
				t.Fatalf("generation mutation did not refuse: %v", err)
			}
			assertPortableModeSnapshot(t, external, externalBefore)
			if mutation == "root before" || mutation == "root after" {
				assertPortableModeSnapshot(t, retained, before)
				assertPortableModeTree(t, retained, 0o500, 0o500, "original payload")
				if err := os.Remove(destination); err != nil {
					t.Fatal(err)
				}
			} else {
				info, err := os.Stat(destination)
				if err != nil || info.Mode().Perm() != 0o500 {
					t.Fatalf("failed removal kept temporary root mode: %v %v", info, err)
				}
				assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "original payload")
				if mutation == "nested directory after" {
					assertPortableModeSnapshot(t, filepath.Join(destination, "nested"), replacementBefore)
					assertCleanupBytes(t, filepath.Join(destination, "nested", "user.md"), "foreign subtree")
				} else {
					if err := os.Chmod(destination, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(filepath.Join(destination, "nested")); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestReadOnlyPortablePrunePreservesUnownedAndReplacement(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "modified", true: "replaced parent"}[replacement], func(t *testing.T) {
			parent, destination, state := installedReadonlyRemovalFixture(t)
			boundary := &destinationBoundary{repoRoot: parent, roots: []string{parent}}
			external := portableModeSource(t, 0o510, 0o500)
			externalBefore := portableModeSnapshot(t, external)
			retained := filepath.Join(parent, "retained-skills")
			if replacement {
				resetPortableModeTreeOnCleanup(t, filepath.Join(retained, "probe"))
				boundary.linkPublicationHook = func(string) {
					if err := os.Rename(filepath.Dir(destination), retained); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(external, filepath.Dir(destination)); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				changePortableModeSourceBody(t, destination, "user-owned bytes")
			}
			before := portableModeSnapshot(t, destination)
			reports, diags, err := prunePortableEntries(state, outputKey{target: hcc.TargetFactory}, parent, parent, make(map[string]bool), false, boundary)
			if replacement {
				if !errors.Is(err, fsboundary.ErrUnsafePath) && !errors.Is(err, fsboundary.ErrGenerationConflict) {
					t.Fatalf("parent replacement not refused: %v", err)
				}
				assertPortableModeSnapshot(t, filepath.Join(retained, "probe"), before)
				if err := os.Remove(filepath.Dir(destination)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err != nil || !hasDiagCode(diags, "materialize.collision.user-owned") {
					t.Fatalf("modified ownership not preserved: %+v %v", diags, err)
				}
				assertPortableModeSnapshot(t, destination, before)
				assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "user-owned bytes")
			}
			if len(reports) != 0 || len(state.entries) != 1 {
				t.Fatalf("refusal reported successful removal: %+v %+v", reports, state.entries)
			}
			assertPortableModeSnapshot(t, external, externalBefore)
		})
	}
}

func TestOwnedPortableRemovalRefusalPreservesSpecialDirectoryMode(t *testing.T) {
	_, destination, state := installedReadonlyRemovalFixture(t)
	if err := os.Chmod(destination, 0o500|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	before := portableModeSnapshot(t, destination)
	observed := before["."]
	dir, err := fsboundary.OpenDir(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	}()
	native, err := dir.NativeRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	}()
	proof := state.entries[portableManifestKey(hcc.TargetFactory, "skills/probe")].Digest
	err = removeVerifiedPortableOutput(dir, native, filepath.Base(destination), observed, proof, func() {}, func() {
		if err := os.WriteFile(filepath.Join(destination, "appeared.md"), []byte("uncooperative change"), 0o444); err != nil {
			t.Fatal(err)
		}
	})
	if !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("changed owned generation was removed: %v", err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode() != observed.Mode() {
		t.Fatalf("refusal lost original directory mode: %v %v", info, err)
	}
	assertCleanupBytes(t, filepath.Join(destination, "SKILL.md"), "original payload")
	assertCleanupBytes(t, filepath.Join(destination, "appeared.md"), "uncooperative change")
}

func TestOwnedPortableRemovalStrictAndPruneMissingSemantics(t *testing.T) {
	parent := t.TempDir()
	dir, err := fsboundary.OpenDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := dir.Close(); err != nil {
			t.Error(err)
		}
	}()
	native, err := dir.NativeRoot()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := native.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := removeVerifiedPortableOutput(dir, native, "absent", nil, ""); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("strict removal silently accepted missing evidence: %v", err)
	}
	path := filepath.Join(parent, "absent")
	boundary := &destinationBoundary{repoRoot: parent, roots: []string{parent}}
	if err := removePortableOutput(path, nil, "", boundary); err != nil {
		t.Fatalf("prune-only absence control: %v", err)
	}
	boundary.linkPublicationHook = func(string) {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "user.md"), []byte("appeared user bytes"), 0o444); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o500); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Error(err)
		}
	})
	if err := removePortableOutput(path, nil, "", boundary); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("prune absence overwrote an appeared tree: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o500 {
		t.Fatalf("appeared tree mode changed: %v %v", info, err)
	}
	assertCleanupBytes(t, filepath.Join(path, "user.md"), "appeared user bytes")
}

func installedReadonlyRemovalFixture(t *testing.T) (string, string, *portableState) {
	t.Helper()
	source := portableModeSource(t, 0o500, 0o500)
	parent := t.TempDir()
	destination := filepath.Join(parent, "skills", "probe")
	resetPortableModeTreeOnCleanup(t, destination)
	state := &portableState{entries: make(map[string]portableManifestEntry)}
	if _, err := syncPortableSkillTrees(filepath.Dir(destination), []skillLink{{name: "probe", target: source}}, false, state, hcc.TargetFactory, "skills", make(map[string]bool)); err != nil {
		t.Fatal(err)
	}
	return parent, destination, state
}
