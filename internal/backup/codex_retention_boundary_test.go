package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

func TestCodexRetentionClaimsRefuseReplacedManifest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	partitions := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents")}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	root, err := fsboundary.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	sets, err := loadCompleteCodexSetsAt(root)
	if err != nil || len(sets) != 1 {
		t.Fatalf("complete set claims: %v %v", sets, err)
	}
	name := operation + ".set.json"
	if err := os.Rename(filepath.Join(dir, name), filepath.Join(t.TempDir(), "retained-manifest")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("inert replacement metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectRetentionMember(root, name, &sets[0].observed); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("claims authorized by replacement generation: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != "inert replacement metadata" {
		t.Fatalf("replacement changed: %q %v", data, err)
	}
}

func TestRotateCodexSetRefusesSharedArchiveClaims(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	partitions := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents")}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	first, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	forged := first + "-1"
	raw, err := os.ReadFile(filepath.Join(dir, first+".set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest codexSetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Operation = forged
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, forged+".set.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	before := make(map[string][]byte)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); !errors.Is(err, fsboundary.ErrGenerationConflict) {
		t.Fatalf("overlapping set claims removed archives: %v", err)
	}
	for path, data := range before {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(data) {
			t.Fatalf("set overlap refusal changed %s: %v", path, err)
		}
	}
	if err := os.Remove(filepath.Join(dir, forged+".set.json")); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreCodexSet(partitions); err != nil || restored != first {
		t.Fatalf("healthy complete set lost after overlap was removed: %q %v", restored, err)
	}
}

// spec: backup-recovery/codex-retention-refuses-hardlinked-member-before-set-removal
func TestRotateCodexSetRefusesHardlinkedMember(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	partitions := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents")}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	first, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-marketplace", "backups", "codex")
	oldManifest := filepath.Join(dir, first+".set.json")
	raw, err := os.ReadFile(oldManifest)
	if err != nil {
		t.Fatal(err)
	}
	var manifest codexSetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var members []string
	original := first
	first = "2026-01-01-000000"
	for i := range manifest.Partitions {
		partition := &manifest.Partitions[i]
		newName := first + partition.Archive[len(original):]
		if err := os.Rename(filepath.Join(dir, partition.Archive), filepath.Join(dir, newName)); err != nil {
			t.Fatal(err)
		}
		partition.Archive = newName
		members = append(members, filepath.Join(dir, newName))
	}
	if len(members) != 2 {
		t.Fatalf("initial set members: %v", members)
	}
	manifest.Operation = first
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldManifest, filepath.Join(dir, first+".set.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, first+".set.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign-archive")
	if err := os.Link(members[0], foreign); err != nil {
		t.Fatal(err)
	}
	before := make(map[string][]byte)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = data
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("hardlinked set member deleted: %v", err)
	}
	for name, data := range before {
		if got, err := os.ReadFile(name); err != nil || string(got) != string(data) {
			t.Fatalf("set refusal changed %s: %v", name, err)
		}
	}
	if got, err := os.ReadFile(foreign); err != nil || string(got) != string(before[members[0]]) {
		t.Fatalf("foreign archive changed: %v", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(Config{MaxDays: 1, MaxPerDay: 1}); err != nil {
		t.Fatal(err)
	}
	for _, name := range append(members, filepath.Join(dir, first+".set.json")) {
		if _, err := os.Lstat(name); !os.IsNotExist(err) {
			t.Fatalf("old set retained %s: %v", name, err)
		}
	}
	if restored, err := RestoreCodexSet(partitions); err != nil || restored != second {
		t.Fatalf("newest complete set lost: %q %v", restored, err)
	}
}
