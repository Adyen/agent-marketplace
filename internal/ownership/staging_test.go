package ownership

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// stagingResidue lists every staging directory or file left beside the
// aggregate. Both the version-1 sibling file and the version-2 private directory
// match this pattern, so one assertion covers both formats.
func stagingResidue(t *testing.T, absFile string) []string {
	t.Helper()
	matches, err := filepath.Glob(absFile + ".am-staged-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestPublishedAggregatePreservesARegularFileMode(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)
	if err := os.Chmod(f.absFile, 0o600); err != nil {
		t.Fatal(err)
	}

	st := f.store()
	mustApply(t, st, Mutation{
		Rel:               f.rel,
		AbsFile:           f.absFile,
		NextBytes:         []byte(`{"alpha":"value-alpha"}` + "\n"),
		Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:            ReasonSync,
		ExpectPriorDigest: Digest([]byte(prior)),
	})

	info, err := os.Lstat(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("published mode = %v; want a regular file with perm 0600", info.Mode())
	}
	if residue := stagingResidue(t, f.absFile); len(residue) != 0 {
		t.Fatalf("staging residue = %v; want none", residue)
	}
}

// A hard link to the aggregate is the attack that a truncate-and-rewrite
// publication cannot survive: the attacker keeps a name for the inode and the
// rewrite lands in it. Rename-based publication allocates a fresh inode, so the
// planted name keeps the bytes it always had.
func TestPublicationNeverWritesThroughAnExistingInode(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)
	planted := filepath.Join(f.targetRoot, "attacker-hard-link")
	if err := os.Link(f.absFile, planted); err != nil {
		t.Skipf("filesystem does not support hard links: %v", err)
	}

	next := `{"alpha":"value-alpha"}` + "\n"
	mustApply(t, f.store(), Mutation{
		Rel:               f.rel,
		AbsFile:           f.absFile,
		NextBytes:         []byte(next),
		Entries:           []Entry{mcpEntry("alpha", "alpha", "value-alpha")},
		Reason:            ReasonSync,
		ExpectPriorDigest: Digest([]byte(prior)),
	})

	if got, exists := f.readAggregate(); !exists || got != next {
		t.Fatalf("aggregate = %q, exists=%v; want %q", got, exists, next)
	}
	if got, err := os.ReadFile(planted); err != nil || string(got) != prior {
		t.Fatalf("hard-linked inode = %q, %v; want the untouched prior %q", got, err, prior)
	}
	published, err := os.Stat(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	plantedInfo, err := os.Stat(planted)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(published, plantedInfo) {
		t.Fatal("publication reused the inode the attacker had a name for")
	}
}

func TestStagingDirectoryIsPrivateAndUnguessable(t *testing.T) {
	f := newFixture(t)
	dirPath, filePath, err := stagedPathFor(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filePath) != dirPath || filepath.Base(filePath) != filepath.Base(f.absFile) {
		t.Fatalf("staged path %q does not live in staging directory %q", filePath, dirPath)
	}
	suffix, ok := strings.CutPrefix(dirPath, f.absFile+".am-staged-")
	if !ok || len(suffix) != stagedRandomBytes*2 {
		t.Fatalf("staging directory %q does not carry a %d-byte random suffix", dirPath, stagedRandomBytes)
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		t.Fatalf("staging suffix %q is not hex: %v", suffix, err)
	}
	other, _, err := stagedPathFor(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	if other == dirPath {
		t.Fatal("two staging directories for the same aggregate collided")
	}

	staging, err := newStagingTransaction(f.store().canonicalRoot, f.absFile, dirPath, filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staging.close() }()
	if err := staging.create(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("staging directory mode = %v; want a 0700 directory", info.Mode())
	}
}

func TestReadJournalRejectsAnAggregateOutsideTheCanonicalRoot(t *testing.T) {
	f := newFixture(t)
	st := f.store()
	if err := os.MkdirAll(st.recordDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(f.targetRoot), "outside.json")
	// The staging paths are internally consistent with the aggregate, so only the
	// canonical-root confinement can reject this journal.
	dirPath, filePath, err := stagedPathFor(outside)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.writeJournal(&Journal{
		Version:       journalVersion,
		RootKey:       st.rootKey,
		CanonicalRoot: st.canonicalRoot,
		Generation:    1,
		Phase:         PhasePrepared,
		File:          f.rel,
		AbsFile:       outside,
		StagingDir:    dirPath,
		StagedPath:    filePath,
		NextDigest:    Digest([]byte("next")),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadJournal(); err == nil {
		t.Fatal("ReadJournal accepted an aggregate outside the canonical root")
	} else if coded, ok := diag.AsCodedError(err); !ok || coded.Code != diag.CodeOwnershipStateCorrupt {
		t.Fatalf("ReadJournal error = %v; want code %q", err, diag.CodeOwnershipStateCorrupt)
	}
	if _, err := f.store().Recover(); !errors.Is(err, ErrStateCorrupt) {
		t.Fatalf("Recover error = %v; want ErrStateCorrupt", err)
	}
	if _, err := os.Stat(st.JournalFile()); err != nil {
		t.Fatalf("journal evidence = %v; want preserved", err)
	}
	if _, err := os.Lstat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside aggregate stat error = %v; want absent", err)
	}
}

// Concurrent writers must never leave a staging directory behind: a leaked one
// is both a disclosure of patched content and a name the next generation would
// have to reason about.
func TestConcurrentApplyLeavesNoStagingResidue(t *testing.T) {
	f := newFixture(t)
	prior := "{}\n"
	f.writeAggregate(prior)

	const writers = 8
	var wg sync.WaitGroup
	results := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := f.store()
			// A missing record is the legitimate first-use answer, and exactly one
			// writer is going to see it.
			proof, proofErr := st.Proof(f.rel)
			if proofErr != nil && !errors.Is(proofErr, ErrStateMissing) {
				results[i] = proofErr
				return
			}
			expect := ""
			if raw, readErr := os.ReadFile(f.absFile); readErr == nil {
				expect = Digest(raw)
			}
			key := "alpha"
			value := "value-" + string(rune('a'+i))
			_, results[i] = st.Apply(Mutation{
				Rel:                   f.rel,
				AbsFile:               f.absFile,
				NextBytes:             []byte(`{"` + key + `":"` + value + `"}` + "\n"),
				Entries:               []Entry{mcpEntry(key, key, value)},
				Reason:                ReasonSync,
				ExpectPriorGeneration: proof.Generation(),
				ExpectPriorDigest:     expect,
			})
		}(i)
	}
	wg.Wait()

	committed := 0
	for i, err := range results {
		switch {
		case err == nil:
			committed++
		case errors.Is(err, ErrGenerationConflict):
		default:
			t.Fatalf("writer %d error = %v; want nil or a generation conflict", i, err)
		}
	}
	if committed == 0 {
		t.Fatal("no concurrent writer committed a generation")
	}
	if residue := stagingResidue(t, f.absFile); len(residue) != 0 {
		t.Fatalf("staging residue = %v; want none", residue)
	}
	if journal, err := f.store().ReadJournal(); err != nil || journal != nil {
		t.Fatalf("journal = %#v, %v; want cleared", journal, err)
	}

	// State must describe the bytes that are actually live, whichever writer won.
	live, err := os.ReadFile(f.absFile)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := f.store().Proof(f.rel)
	if err != nil {
		t.Fatal(err)
	}
	if proof.ContentDigest() != Digest(live) {
		t.Fatalf("recorded content digest = %q; want the live digest %q", proof.ContentDigest(), Digest(live))
	}
}
