package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

// spec: marketplace-publication/declared-prerequisites-are-indexed-in-declaration-order
// spec: marketplace-publication/credential-bearing-hints-are-stored-redacted
func TestBuildRegistryRevisionIndexesRequirements(t *testing.T) {
	repoDir := createIngestRepo(t)
	skillPath := filepath.Join(repoDir, ".agents", "skills", "review", "SKILL.md")
	body := `---
name: review
description: Review code
license: MIT
metadata:
  requires:
    - helper
    - command:lint
    - kind: subagent
      name: helper2
      registry: https://user:secret@example.com/registry.git
      path: shared
---
Review the code carefully.
`
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAllIngestRepo(t, repoDir, "requires")
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	source := store.Registry{
		ID: "src-test", Slug: "example-source", Ref: "main",
		Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT",
		Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
	}
	rev, err := BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}
	var skill, collection *store.ResourceRevision
	for i := range rev.Resources {
		switch rev.Resources[i].Coordinate.Kind + "/" + rev.Resources[i].Coordinate.Name {
		case "skill/review":
			skill = &rev.Resources[i]
		case "collection/defaults":
			collection = &rev.Resources[i]
		}
	}
	if skill == nil || collection == nil {
		t.Fatalf("missing revisions: %#v", rev.Resources)
	}
	if collection.Requires != "[]" {
		t.Fatalf("collection requires = %q, want []", collection.Requires)
	}
	var requirements []store.ResourceRequirement
	if err := json.Unmarshal([]byte(skill.Requires), &requirements); err != nil {
		t.Fatalf("requires is not canonical JSON: %q: %v", skill.Requires, err)
	}
	want := []store.ResourceRequirement{
		{Kind: "skill", Name: "helper"},
		{Kind: "command", Name: "lint"},
		{Kind: "subagent", Name: "helper2", Registry: "https://***@example.com/registry.git", Path: "shared"},
	}
	if len(requirements) != len(want) {
		t.Fatalf("requires = %+v, want %+v", requirements, want)
	}
	for i := range want {
		if requirements[i] != want[i] {
			t.Fatalf("requires[%d] = %+v, want %+v", i, requirements[i], want[i])
		}
	}
	if json.Valid([]byte(skill.Requires)) && string(mustMarshal(t, requirements)) != skill.Requires {
		t.Fatalf("requires is not canonical JSON: %q", skill.Requires)
	}
	for _, raw := range []string{"user:secret", "secret@"} {
		if strings.Contains(skill.Requires, raw) {
			t.Fatalf("stored requires leaked credentials: %q", skill.Requires)
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// spec: marketplace-publication/malformed-prerequisite-declaration-does-not-fail-publication
func TestBuildRegistryRevisionKeepsMalformedRequirementParse(t *testing.T) {
	repoDir := createIngestRepo(t)
	skillPath := filepath.Join(repoDir, ".agents", "skills", "review", "SKILL.md")
	body := `---
name: review
description: Review code
license: MIT
metadata:
  requires:
    - helper
    - kind: weird
      name: dropped
---
Review the code carefully.
`
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAllIngestRepo(t, repoDir, "malformed requires")
	commit, err := repoCommit(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	source := store.Registry{
		ID: "src-test", Slug: "example-source", Ref: "main",
		Origins: []string{"file://" + repoDir}, Acquisition: store.AcquisitionMirror, License: "MIT",
		Roots: []store.RegistryRoot{{Path: ".agents", SourceHarness: "factory"}},
	}
	rev, err := BuildRegistryRevision(context.Background(), testConfig(t, repoDir), "mp-test", source, repoDir, commit)
	if err != nil {
		t.Fatalf("BuildRegistryRevision: %v", err)
	}
	var requirements []store.ResourceRequirement
	for i := range rev.Resources {
		if rev.Resources[i].Coordinate.Name == "review" {
			if err := json.Unmarshal([]byte(rev.Resources[i].Requires), &requirements); err != nil {
				t.Fatalf("requires = %q: %v", rev.Resources[i].Requires, err)
			}
		}
	}
	if len(requirements) != 1 || requirements[0] != (store.ResourceRequirement{Kind: "skill", Name: "helper"}) {
		t.Fatalf("requires = %+v, want only the accepted declaration", requirements)
	}
}
