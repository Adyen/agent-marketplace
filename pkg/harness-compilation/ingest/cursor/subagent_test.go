package cursor

import "testing"

func TestLoadSubagentPreservesNativeModelIdentity(t *testing.T) {
	sub, err := LoadSubagent(cursorPluginFS, "agents/my-agent.md")
	if err != nil {
		t.Fatalf("LoadSubagent: %v", err)
	}
	if sub.Model != "claude-sonnet-4-5-20250929" || sub.ModelSource != "cursor" {
		t.Fatalf("native model identity lost: %+v", sub)
	}
	if len(sub.Tools) != 2 || sub.Tools[0] != "Read" || sub.Tools[1] != "Write" {
		t.Fatalf("Tools = %#v, want [Read Write]", sub.Tools)
	}
}
