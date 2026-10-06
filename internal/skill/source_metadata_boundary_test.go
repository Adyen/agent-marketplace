package skill

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
)

// spec: registry-symlink-containment/legacy-metadata-readers-refuse-foreign-source-payloads
func TestDiscoverLegacySourceMetadataRefusesForeignPayload(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(dir, "SKILL.md")
	payload := []byte("---\nname: probe\ndescription: native metadata control\n---\n\ninert body\n")
	if err := os.WriteFile(primary, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	got, malformed, err := DiscoverSkills(root)
	if err != nil || len(got) != 1 || len(malformed) != 0 || got[0].Name != "probe" {
		t.Fatalf("legacy metadata positive control failed: %+v %+v %v", got, malformed, err)
	}
	foreign := filepath.Join(outside, "foreign.md")
	if err := os.Rename(primary, foreign); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, primary); err != nil {
		t.Fatal(err)
	}
	got, malformed, err = DiscoverSkills(root)
	if len(got) != 0 || len(malformed) != 1 || !errors.Is(malformed[0].Error, fsboundary.ErrUnsafePath) {
		t.Fatalf("foreign legacy metadata imported or typed refusal lost: %+v %+v %v", got, malformed, err)
	}
	if name := DeclaredNameFromFile(primary); name != "" {
		t.Fatalf("foreign payload supplied best-effort authored name: %q", name)
	}
	if after, err := os.Stat(foreign); err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("legacy source read changed foreign metadata: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != string(payload) {
		t.Fatalf("legacy source read changed foreign bytes: %q %v", data, err)
	}
}

func TestLegacySourceMetadataContainedAliasAndHardLink(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "SKILL.md")
	authored := filepath.Join(root, "authored.txt")
	if err := os.WriteFile(authored, []byte("---\nname: authored-probe\ndescription: prefix control\n---\n\ninert body\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("authored.txt", primary); err != nil {
		t.Fatal(err)
	}
	name, description, _, err := parseFrontmatter(primary)
	if err != nil || name != "authored-probe" || description != "prefix control" || DeclaredNameFromSource(root, primary) != "authored-probe" {
		t.Fatalf("contained metadata alias lost authored control: %q %q %v", name, description, err)
	}
	if err := os.Link(authored, filepath.Join(t.TempDir(), "retained.txt")); err != nil {
		t.Fatal(err)
	}
	if name, _, _, err := parseFrontmatter(primary); !errors.Is(err, fsboundary.ErrUnsafeFile) || name != "" {
		t.Fatalf("metadata hard link accepted: %q %v", name, err)
	}
	if name := DeclaredNameFromSource(root, primary); name != "" {
		t.Fatalf("hard link supplied best-effort name: %q", name)
	}
}
