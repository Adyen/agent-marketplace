package convertsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolatedState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
}

func TestLoadWithoutRecordReportsMissingAndOwnsNothing(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	record, err := Load(outRoot, "factory")
	if !errors.Is(err, ErrRecordMissing) {
		t.Fatalf("Load() error = %v, want ErrRecordMissing", err)
	}
	if status := record.Classify("skills/demo/SKILL.md", []byte("body")); status != StatusUnmanaged {
		t.Fatalf("Classify() = %v, want StatusUnmanaged for an absent record", status)
	}
}

func TestSaveThenLoadClassifiesOwnedAndDriftedAndUnmanaged(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	owned := []byte("generated body\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "skills/demo/SKILL.md", Digest: DigestOf(owned)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	record, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := record.Classify("skills/demo/SKILL.md", owned); got != StatusOwned {
		t.Fatalf("Classify(owned) = %v, want StatusOwned", got)
	}
	if got := record.Classify("skills/demo/SKILL.md", []byte("edited by hand\n")); got != StatusOwnedDrifted {
		t.Fatalf("Classify(edited) = %v, want StatusOwnedDrifted", got)
	}
	if got := record.Classify("README.md", []byte("handwritten\n")); got != StatusUnmanaged {
		t.Fatalf("Classify(unrecorded) = %v, want StatusUnmanaged", got)
	}
}

func TestSaveIsRelativeToTheCanonicalOutputRootNotItsSpelling(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()
	nested := filepath.Join(outRoot, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	body := []byte("generated\n")
	if err := Save(nested, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	record, err := Load(filepath.Join(outRoot, ".", "nested", "..", "nested"), "factory")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := record.Classify("a.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want StatusOwned through an equivalent path spelling", got)
	}
}

func TestLoadRefusesCorruptRecordAndOwnsNothing(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "skills/demo/SKILL.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := RecordPath(outRoot, "factory")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	record, err := Load(outRoot, "factory")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Load() error = %v, want ErrRecordCorrupt", err)
	}
	if got := record.Classify("skills/demo/SKILL.md", body); got != StatusUnmanaged {
		t.Fatalf("Classify() = %v, want StatusUnmanaged for a corrupt record", got)
	}
}

func TestLoadRefusesRecordWrittenForAnotherRoot(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := RecordPath(outRoot, "factory")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	record.CanonicalRoot = filepath.Join(outRoot, "elsewhere")
	tampered, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(outRoot, "factory")
	if !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Load() error = %v, want ErrRecordCorrupt for a foreign root", err)
	}
	if got := loaded.Classify("a.md", body); got != StatusUnmanaged {
		t.Fatalf("Classify() = %v, want StatusUnmanaged", got)
	}
}

func TestLoadRefusesRecordWithEscapingPath(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := RecordPath(outRoot, "factory")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"a.md"`, `"../a.md"`, 1)
	if tampered == string(raw) {
		t.Fatalf("expected to rewrite the recorded path in %s", raw)
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(outRoot, "factory"); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Load() error = %v, want ErrRecordCorrupt for an escaping path", err)
	}
}

func TestLoadDoesNotSeeAnotherTargetsRecord(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "skills/demo/SKILL.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	record, err := Load(outRoot, "gemini")
	if !errors.Is(err, ErrRecordMissing) {
		t.Fatalf("Load(gemini) error = %v, want ErrRecordMissing", err)
	}
	if got := record.Classify("skills/demo/SKILL.md", body); got != StatusUnmanaged {
		t.Fatalf("Classify() = %v, want StatusUnmanaged for another target's record", got)
	}
	// The record itself is intact: it still owns its own target's output.
	sameTarget, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load(factory) error = %v", err)
	}
	if got := sameTarget.Classify("skills/demo/SKILL.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want StatusOwned for the recorded target", got)
	}
}

func TestLoadRefusesRecordWithoutATarget(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := RecordPath(outRoot, "factory")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"target": "factory"`, `"target": ""`, 1)
	if tampered == string(raw) {
		t.Fatalf("expected to blank the recorded target in %s", raw)
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	// A record naming no target is refused rather than adopted by the target that
	// happens to read it, even though its file name says which target wrote it:
	// the name is a lossy key, and only the body is evidence.
	if _, err := Load(outRoot, "factory"); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Load() error = %v, want ErrRecordCorrupt for a record with no target", err)
	}
}

func TestLoadWithoutATargetRefusesBeforeReadingState(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	if _, err := Load(outRoot, ""); !errors.Is(err, ErrTargetUnusable) {
		t.Fatalf("Load() error = %v, want ErrTargetUnusable", err)
	}
}

// Two targets converted into the same output directory must each keep their own
// record: a single record per root means whichever ran last erases the other's
// evidence, so neither can prune its own output again.
func TestSaveKeepsOneRecordPerTargetForTheSameRoot(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	factoryBody := []byte("factory generated\n")
	geminiBody := []byte("gemini generated\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "skills/demo/SKILL.md", Digest: DigestOf(factoryBody)}}); err != nil {
		t.Fatalf("Save(factory) error = %v", err)
	}
	if err := Save(outRoot, "gemini", []OwnedFile{{Path: ".gemini/skills/demo/SKILL.md", Digest: DigestOf(geminiBody)}}); err != nil {
		t.Fatalf("Save(gemini) error = %v", err)
	}

	factory, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load(factory) error = %v", err)
	}
	if got := factory.Classify("skills/demo/SKILL.md", factoryBody); got != StatusOwned {
		t.Fatalf("Classify(factory output) = %v, want StatusOwned after a gemini sync into the same root", got)
	}
	if got := factory.Classify(".gemini/skills/demo/SKILL.md", geminiBody); got != StatusUnmanaged {
		t.Fatalf("Classify(gemini output) = %v, want StatusUnmanaged under the factory record", got)
	}

	gemini, err := Load(outRoot, "gemini")
	if err != nil {
		t.Fatalf("Load(gemini) error = %v", err)
	}
	if got := gemini.Classify(".gemini/skills/demo/SKILL.md", geminiBody); got != StatusOwned {
		t.Fatalf("Classify(gemini output) = %v, want StatusOwned", got)
	}
	if got := gemini.Classify("skills/demo/SKILL.md", factoryBody); got != StatusUnmanaged {
		t.Fatalf("Classify(factory output) = %v, want StatusUnmanaged under the gemini record", got)
	}
}

// A record written before records carried the target in their name is still
// evidence for the target it names, so it is adopted once and then rewritten
// under its target key.
func TestLoadAdoptsAndSaveRetiresAPreTargetKeyedRecord(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	legacyPath := writeLegacyRecord(t, outRoot, "factory", OwnedFile{Path: "a.md", Digest: DigestOf(body)})

	adopted, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load() error = %v, want the legacy record to be adopted", err)
	}
	if got := adopted.Classify("a.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want StatusOwned from the legacy record", got)
	}

	if err := Save(outRoot, "factory", adopted.Files); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("stat legacy record after migration = %v, want it removed", err)
	}
	migrated, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load() after migration error = %v", err)
	}
	if got := migrated.Classify("a.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want StatusOwned from the migrated record", got)
	}
}

// A pre-target-keyed record names exactly one target. Another target must not
// inherit it, and must not destroy it either: it is that target's only evidence
// of what it owns.
func TestLegacyRecordAuthorizesOnlyItsOwnTarget(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	legacyPath := writeLegacyRecord(t, outRoot, "factory", OwnedFile{Path: "a.md", Digest: DigestOf(body)})

	record, err := Load(outRoot, "gemini")
	if !errors.Is(err, ErrRecordOtherTarget) {
		t.Fatalf("Load(gemini) error = %v, want ErrRecordOtherTarget", err)
	}
	if got := record.Classify("a.md", body); got != StatusUnmanaged {
		t.Fatalf("Classify() = %v, want StatusUnmanaged", got)
	}

	if err := Save(outRoot, "gemini", []OwnedFile{{Path: "b.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save(gemini) error = %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("stat legacy record after an unrelated target's save = %v, want it preserved", err)
	}
	factory, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load(factory) error = %v", err)
	}
	if got := factory.Classify("a.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want the legacy record still usable by its own target", got)
	}
}

func TestTargetSpellingDoesNotSplitTheRecord(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	body := []byte("generated\n")
	if err := Save(outRoot, " Factory ", []OwnedFile{{Path: "a.md", Digest: DigestOf(body)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	record, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := record.Classify("a.md", body); got != StatusOwned {
		t.Fatalf("Classify() = %v, want StatusOwned across equivalent target spellings", got)
	}
}

func TestUnusableTargetNamesAreRefused(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	// A target name becomes part of the record's file name, and that file decides
	// which paths may be deleted, so a name that could redirect it is refused
	// outright instead of sanitized into one that might collide.
	for _, target := range []string{"", "   ", "../factory", "fact/ory", "fact.ory", strings.Repeat("f", 65)} {
		if err := Save(outRoot, target, []OwnedFile{{Path: "a.md", Digest: DigestOf(nil)}}); !errors.Is(err, ErrTargetUnusable) {
			t.Fatalf("Save(%q) error = %v, want ErrTargetUnusable", target, err)
		}
		if _, err := Load(outRoot, target); !errors.Is(err, ErrTargetUnusable) {
			t.Fatalf("Load(%q) error = %v, want ErrTargetUnusable", target, err)
		}
	}
}

// writeLegacyRecord writes a valid record at the pre-target-keyed location by
// relocating a freshly saved one, and returns its path.
func writeLegacyRecord(t *testing.T, outRoot, target string, files ...OwnedFile) string {
	t.Helper()
	if err := Save(outRoot, target, files); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	current, err := RecordPath(outRoot, target)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := LegacyRecordPath(outRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(current, legacy); err != nil {
		t.Fatal(err)
	}
	return legacy
}

func TestSaveRefusesRecordingWithoutATarget(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	if err := Save(outRoot, "  ", []OwnedFile{{Path: "a.md", Digest: DigestOf(nil)}}); err == nil {
		t.Fatal("Save() = nil, want an error without a target harness")
	}
	if _, err := Load(outRoot, "factory"); !errors.Is(err, ErrRecordMissing) {
		t.Fatalf("Load() error = %v, want ErrRecordMissing: a refused Save must write nothing", err)
	}
}

func TestSaveRefusesUnusablePathsAndDigests(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	cases := []struct {
		desc string
		file OwnedFile
	}{
		{desc: "absolute path", file: OwnedFile{Path: "/etc/passwd", Digest: DigestOf(nil)}},
		{desc: "parent traversal", file: OwnedFile{Path: "../escape.md", Digest: DigestOf(nil)}},
		{desc: "empty path", file: OwnedFile{Path: "", Digest: DigestOf(nil)}},
		{desc: "malformed digest", file: OwnedFile{Path: "a.md", Digest: "deadbeef"}},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			if err := Save(outRoot, "factory", []OwnedFile{tc.file}); err == nil {
				t.Fatalf("Save(%+v) = nil, want an error", tc.file)
			}
		})
	}
}

func TestSaveReplacesThePreviousRecord(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	first := []byte("first\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(first)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	second := []byte("second\n")
	if err := Save(outRoot, "factory", []OwnedFile{{Path: "b.md", Digest: DigestOf(second)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	record, err := Load(outRoot, "factory")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := record.Classify("a.md", first); got != StatusUnmanaged {
		t.Fatalf("Classify(a.md) = %v, want StatusUnmanaged after replacement", got)
	}
	if got := record.Classify("b.md", second); got != StatusOwned {
		t.Fatalf("Classify(b.md) = %v, want StatusOwned", got)
	}
}

func TestSaveDoesNotWriteInsideTheOutputRoot(t *testing.T) {
	isolatedState(t)
	outRoot := t.TempDir()

	if err := Save(outRoot, "factory", []OwnedFile{{Path: "a.md", Digest: DigestOf(nil)}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	entries, err := os.ReadDir(outRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("output root gained %d entry/entries; ownership state must live outside the output tree", len(entries))
	}
}
