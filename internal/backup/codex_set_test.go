package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

func TestCodexBackupSetPublishesBothPartitionsAndRestoresManagedContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentsRoot, codexRoot := filepath.Join(home, ".agents"), filepath.Join(home, "custom-codex")
	if err := os.MkdirAll(filepath.Join(agentsRoot, "skills", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(agentsRoot, "skills", "demo", "SKILL.md")
	if err := os.WriteFile(managed, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	partitions := []CodexPartition{
		{ID: "machine-agents", Root: agentsRoot, RelPaths: []string{"skills"}},
		{ID: "machine-codex", Root: codexRoot, RelPaths: []string{"agents", "hooks.json", "config.toml"}},
	}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := filepath.Glob(filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+"-*.tar.gz"))
	if err != nil || len(archives) != 2 {
		t.Fatalf("archives = %v, err=%v", archives, err)
	}
	for _, id := range []string{"machine-agents", "machine-codex"} {
		found := false
		for _, archive := range archives {
			found = found || strings.Contains(filepath.Base(archive), "-"+id+"-")
		}
		if !found {
			t.Errorf("missing %s archive in %v", id, archives)
		}
	}
	listed, err := ListBackups()
	codex := hcc.Target("codex")
	if err != nil || len(listed[codex]) != 1 || listed[codex][0] != operation {
		t.Fatalf("ListBackups codex = %v, err=%v", listed[codex], err)
	}
	if err := os.WriteFile(managed, []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(agentsRoot, "notes.txt")
	if err := os.WriteFile(unmanaged, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreCodexSet(partitions)
	if err != nil || restored != operation {
		t.Fatalf("RestoreCodexSet = %q, %v", restored, err)
	}
	if got, _ := os.ReadFile(managed); string(got) != "before" {
		t.Fatalf("managed content = %q", got)
	}
	if got, _ := os.ReadFile(unmanaged); string(got) != "keep" {
		t.Fatalf("unmanaged content = %q", got)
	}
}

func TestRestoreCodexSetRejectsUnsafeManifestManagedPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	partitions := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents")}, {ID: "machine-codex", Root: filepath.Join(home, ".codex")}}
	operation, err := CreateCodexSet(partitions)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(home, ".agent-marketplace", "backups", "codex", operation+".set.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest codexSetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Partitions[0].RelPaths = []string{"."}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreCodexSet(partitions); err == nil {
		t.Fatal("unsafe managed_paths manifest was accepted")
	}
}

func TestRestoreCodexSetRecoversManagedPathMissingFromCurrentEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	agentsRoot, codexRoot := filepath.Join(home, ".agents"), filepath.Join(home, ".codex")
	managed := filepath.Join(agentsRoot, "skills", "demo", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("recover"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := []CodexPartition{{ID: "machine-agents", Root: agentsRoot, RelPaths: []string{"skills/demo"}}, {ID: "machine-codex", Root: codexRoot}}
	if _, err := CreateCodexSet(created); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(agentsRoot, "skills", "demo")); err != nil {
		t.Fatal(err)
	}
	current := []CodexPartition{{ID: "machine-agents", Root: agentsRoot}, {ID: "machine-codex", Root: codexRoot}}
	if _, err := RestoreCodexSet(current); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(managed); err != nil || string(got) != "recover" {
		t.Fatalf("restored content = %q, err=%v", got, err)
	}
}

func TestCodexBackupSetRootMismatchRefusesBeforeMutation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	original := []CodexPartition{{ID: "machine-agents", Root: filepath.Join(home, ".agents"), RelPaths: []string{"skills"}}, {ID: "machine-codex", Root: filepath.Join(home, ".codex"), RelPaths: []string{"agents"}}}
	if _, err := CreateCodexSet(original); err != nil {
		t.Fatal(err)
	}
	changedRoot := filepath.Join(home, "other-codex")
	marker := filepath.Join(changedRoot, "agents", "marker")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("unchanged"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed := []CodexPartition{original[0], {ID: "machine-codex", Root: changedRoot, RelPaths: []string{"agents"}}}
	if _, err := RestoreCodexSet(changed); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("RestoreCodexSet error = %v", err)
	}
	if got, _ := os.ReadFile(marker); string(got) != "unchanged" {
		t.Fatalf("root mismatch mutated destination: %q", got)
	}
}
