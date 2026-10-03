package sourcerevision

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: registry-symlink-containment/source-revision-hashing-refuses-post-ingest-unsafe-payloads
func TestBuildSourceHashRefusesPostIngestHardLink(t *testing.T) {
	snapshot := t.TempDir()
	primary := filepath.Join(snapshot, "skills", "hash-probe", "SKILL.md")
	writeFixture(t, primary, "---\nname: hash-probe\ndescription: hash control\n---\n\ninert primary body\n")
	before, err := os.Stat(primary)
	if err != nil {
		t.Fatal(err)
	}
	hit := false
	result, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude", beforeHash: func(root string) {
		hit = true
		if err := os.Link(primary, filepath.Join(root, "retained-primary.md")); err != nil {
			t.Fatal(err)
		}
	}})
	if !hit {
		t.Fatal("source revision did not reach actual hash phase")
	}
	if !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("unsafe hash payload did not refuse: %+v %v", result, err)
	}
	if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 {
		t.Fatalf("unsafe hash published a manifest: %+v", result)
	}
	if after, err := os.Stat(primary); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("unsafe hash changed primary payload: %v", err)
	}
}

func TestBuildSourceHashSupportRefusalIsNotOmission(t *testing.T) {
	snapshot := t.TempDir()
	primary := filepath.Join(snapshot, "skills", "hash-probe", "SKILL.md")
	writeFixture(t, primary, "---\nname: hash-probe\ndescription: hash control\n---\n\ninert primary body\n")
	support := filepath.Join(snapshot, "skills", "hash-probe", "reference.txt")
	writeFixture(t, support, "inert support data")
	control, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude"})
	if err != nil || len(control.Manifest.Resources) != 1 || len(control.Manifest.Resources[0].SupportFiles) != 1 {
		t.Fatalf("native revision support positive control failed: %+v %v", control, err)
	}
	hit := false
	result, err := Build(Input{SnapshotRoot: snapshot, SourceHarness: "claude", beforeHash: func(root string) {
		hit = true
		if err := os.Link(support, filepath.Join(root, "retained-support.txt")); err != nil {
			t.Fatal(err)
		}
	}})
	if !hit || !errors.Is(err, fsboundary.ErrUnsafeFile) {
		t.Fatalf("unsafe support hash became omission: %v %+v %v", hit, result, err)
	}
	if len(result.Payload) != 0 || len(result.Manifest.Resources) != 0 {
		t.Fatalf("support refusal published partial revision: %+v", result)
	}
	if data, err := os.ReadFile(support); err != nil || string(data) != "inert support data" {
		t.Fatalf("support refusal changed payload: %q %v", data, err)
	}
}
