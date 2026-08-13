package cursor

import "testing"

func TestLoadSubagent_NormalizesCursorModelToCanonicalIR(t *testing.T) {
	sub, err := LoadSubagent(cursorPluginFS, "agents/my-agent.md")
	if err != nil {
		t.Fatalf("LoadSubagent: %v", err)
	}
	if sub.Model != "sonnet" {
		t.Fatalf("Model = %q, want %q", sub.Model, "sonnet")
	}
	if len(sub.Tools) != 2 || sub.Tools[0] != "Read" || sub.Tools[1] != "Write" {
		t.Fatalf("Tools = %#v, want [Read Write]", sub.Tools)
	}
}
