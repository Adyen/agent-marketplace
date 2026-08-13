package cursor

import "testing"

func TestLoadSkill_PluginPath(t *testing.T) {
	skill, err := LoadSkill(cursorPluginFS, "skills/my-skill")
	if err != nil {
		t.Fatalf("LoadSkill(plugin): %v", err)
	}
	if skill.Name() != "my-skill" {
		t.Fatalf("Name() = %q, want %q", skill.Name(), "my-skill")
	}
	if skill.Source.File != "skills/my-skill/SKILL.md" {
		t.Fatalf("Source.File = %q, want %q", skill.Source.File, "skills/my-skill/SKILL.md")
	}
}

func TestLoadSkill_WorkspacePathNormalizesSource(t *testing.T) {
	skill, err := LoadSkill(cursorWorkspaceFS, ".cursor/skills/my-skill")
	if err != nil {
		t.Fatalf("LoadSkill(workspace): %v", err)
	}
	if skill.Source.File != "skills/my-skill/SKILL.md" {
		t.Fatalf("Source.File = %q, want %q", skill.Source.File, "skills/my-skill/SKILL.md")
	}
}
