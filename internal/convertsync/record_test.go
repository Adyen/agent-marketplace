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

	record, err := Load(outRoot)
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

	record, err := Load(outRoot)
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

	record, err := Load(filepath.Join(outRoot, ".", "nested", "..", "nested"))
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
	path, err := RecordPath(outRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	record, err := Load(outRoot)
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
	path, err := RecordPath(outRoot)
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

	loaded, err := Load(outRoot)
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
	path, err := RecordPath(outRoot)
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

	if _, err := Load(outRoot); !errors.Is(err, ErrRecordCorrupt) {
		t.Fatalf("Load() error = %v, want ErrRecordCorrupt for an escaping path", err)
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

	record, err := Load(outRoot)
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
