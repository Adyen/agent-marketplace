//go:build unix

package ownership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStagedRefusesPreexistingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "unrelated")
	stage := filepath.Join(dir, "aggregate.json.am-staged-known")
	const original = "must survive"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, stage); err != nil {
		t.Fatal(err)
	}

	if _, err := writeStaged(stage, []byte("replacement"), 0o600); err == nil {
		t.Fatal("writeStaged followed a pre-existing staging symlink")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != original {
		t.Fatalf("unrelated target = %q, %v; want %q", got, err, original)
	}
	if info, err := os.Lstat(stage); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("pre-existing staging symlink was changed: info=%v err=%v", info, err)
	}
}

func TestGenerationConflictDoesNotFollowPlantedStagingSymlink(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	unrelated := filepath.Join(t.TempDir(), "unrelated")
	const original = "do not truncate"
	if err := os.WriteFile(unrelated, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := f.absFile + ".am-staged-1"
	if err := os.Symlink(unrelated, planted); err != nil {
		t.Fatal(err)
	}

	st := f.store()
	_, err := st.Apply(Mutation{
		Rel:                   f.rel,
		AbsFile:               f.absFile,
		NextBytes:             []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:               []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:                ReasonSync,
		ExpectPriorGeneration: 1,
		ExpectPriorDigest:     Digest([]byte(prior)),
	})
	if !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Apply error = %v, want ErrGenerationConflict", err)
	}
	if got, err := os.ReadFile(unrelated); err != nil || string(got) != original {
		t.Fatalf("unrelated target = %q, %v; want %q", got, err, original)
	}
	if info, err := os.Lstat(planted); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("planted staging symlink was changed: info=%v err=%v", info, err)
	}
}
