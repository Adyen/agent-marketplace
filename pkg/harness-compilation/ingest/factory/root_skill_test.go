package factory

import (
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func rootSkillFixture(extra map[string]*fstest.MapFile) fstest.MapFS {
	fsys := fstest.MapFS{
		factoryPluginManifest: {Data: []byte(`{"name":"demo","version":"1.0.0"}`)},
		"SKILL.md":            {Data: []byte("---\nname: root-skill\ndescription: at root\n---\nBody\n")},
	}
	for path, file := range extra {
		fsys[path] = file
	}
	return fsys
}

// spec: harness-compilation/root-skill-without-a-skills-tree-warns
func TestRootSkillWithoutSkillsTreeWarns(t *testing.T) {
	b, err := New().LoadBundle(rootSkillFixture(nil))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	for _, resource := range b.Resources {
		if resource.Kind() == ir.KindSkill {
			t.Fatalf("loaded a skill from the root SKILL.md: %s", resource.Name())
		}
	}
	if !diag.HasCode(b.Diagnostics, diag.CodeIngestFactoryRootSkillIgnored) {
		t.Fatalf("missing ingest.factory.root-skill-ignored warning; diagnostics = %+v", b.Diagnostics)
	}
}

// spec: harness-compilation/root-skill-beside-a-skills-tree-warns-and-keeps-the-tree
func TestRootSkillBesideSkillsTreeWarnsAndKeepsTree(t *testing.T) {
	b, err := New().LoadBundle(rootSkillFixture(map[string]*fstest.MapFile{
		"skills/kept/SKILL.md": {Data: []byte("---\nname: kept\ndescription: kept skill\n---\nBody\n")},
	}))
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}
	var skills []string
	for _, resource := range b.Resources {
		if resource.Kind() == ir.KindSkill {
			skills = append(skills, resource.Name())
		}
	}
	if len(skills) != 1 || skills[0] != "kept" {
		t.Fatalf("skills = %v, want [kept]", skills)
	}
	if !diag.HasCode(b.Diagnostics, diag.CodeIngestFactoryRootSkillIgnored) {
		t.Fatalf("missing ingest.factory.root-skill-ignored warning; diagnostics = %+v", b.Diagnostics)
	}
}
