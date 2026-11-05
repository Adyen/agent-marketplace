package ir

import (
	"strings"
	"testing"
)

func TestKindPluginAppendedAfterKindRule(t *testing.T) {
	if KindPlugin != KindRule+1 {
		t.Fatalf("KindPlugin = %d, want %d (appended after KindRule without renumbering)", KindPlugin, KindRule+1)
	}
	if KindPlugin.String() != "plugin" {
		t.Fatalf("KindPlugin.String() = %q, want %q", KindPlugin.String(), "plugin")
	}
}

func TestValidateResourceRefusesPlugin(t *testing.T) {
	p := &Plugin{Manifest: Manifest{Name: "demo-plugin"}}
	if _, _, err := ValidateResource(p); err == nil {
		t.Fatal("ValidateResource must refuse a plugin resource")
	}
}

func TestPluginMemberKey(t *testing.T) {
	skill := NewSkill("summarize", "d", "b", nil, nil, SourceRef{})
	member := PluginMember{Resource: skill}
	if member.Key() != "skill/summarize" {
		t.Fatalf("Key() = %q, want %q", member.Key(), "skill/summarize")
	}
}

func TestValidatePluginName(t *testing.T) {
	cases := []struct {
		format, name string
		wantErr      bool
	}{
		{"agent-plugin", "my-plugin", false},
		{"agent-plugin", "my.plugin", false},
		{"agent-plugin", "9lives", false},
		{"agent-plugin", "Bad", true},
		{"agent-plugin", "bad--name", true},
		{"agent-plugin", "bad..name", true},
		{"agent-plugin", "-bad", true},
		{"agent-plugin", "", true},
		{"agent-plugin", strings.Repeat("a", 65), true},
		{"claude-plugin", "my-plugin", false},
		{"claude-plugin", "9lives", true},
		{"claude-plugin", "my.plugin", true},
		{"claude-plugin", "", true},
	}
	for _, c := range cases {
		err := ValidatePluginName(c.format, c.name)
		if c.wantErr && err == nil {
			t.Errorf("ValidatePluginName(%q, %q) = nil, want error", c.format, c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("ValidatePluginName(%q, %q) = %v, want nil", c.format, c.name, err)
		}
	}
}
