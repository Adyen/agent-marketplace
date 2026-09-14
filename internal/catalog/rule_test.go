package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/collection"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadDevinRuleSourcePathPrefersWorkspace(t *testing.T) {
	root := t.TempDir()
	directPath := filepath.Join(root, "rules", "foo.md")
	workspacePath := filepath.Join(root, ".devin", "rules", "foo.md")
	for path, body := range map[string]string{
		directPath:    "---\ntrigger: manual\n---\nDirect body\n",
		workspacePath: "---\ntrigger: manual\n---\nWorkspace body\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cat, err := Load(root, HarnessDevin)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, entry := range cat.Entries {
		if entry.Kind != ir.KindRule || entry.Name != "foo" {
			continue
		}
		if entry.Path != workspacePath {
			t.Fatalf("rule source path = %q, want workspace path %q", entry.Path, workspacePath)
		}
		rule, ok := entry.Resource.(*ir.Rule)
		if !ok {
			t.Fatalf("rule resource type = %T", entry.Resource)
		}
		if rule.Body != "Workspace body\n" {
			t.Fatalf("rule body = %q, want workspace body", rule.Body)
		}
		return
	}
	t.Fatalf("catalog entries = %#v, want rule foo", cat.Entries)
}

func TestEntriesFromCollectionsParsesRuleMembers(t *testing.T) {
	entries, err := EntriesFromCollections([]collection.Collection{{
		Name: "coding-standards",
		Resources: []collection.CollectionEntry{{
			Name: "secure-coding",
			Kind: "rule",
		}},
	}})
	if err != nil {
		t.Fatalf("EntriesFromCollections: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	resource, ok := entries[0].Resource.(*ir.Collection)
	if !ok {
		t.Fatalf("resource type = %T, want *ir.Collection", entries[0].Resource)
	}
	if len(resource.Members) != 1 || resource.Members[0].Kind != ir.KindRule {
		t.Fatalf("collection members = %#v, want one rule member", resource.Members)
	}
}
