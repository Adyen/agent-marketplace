package artifact

import (
	"sort"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestExtractCapabilitiesSkill(t *testing.T) {
	s := ir.NewSkill("review", "Review", "Body", []string{"user"}, nil, ir.SourceRef{})
	s.AllowedTools = []string{"read_file"}
	s.Subagent = "planner"
	s.Agent = "coder"

	caps := ExtractCapabilities(s)
	if !sort.StringsAreSorted(caps) {
		t.Fatal("capabilities should be sorted")
	}

	want := map[string]bool{
		"skill":            true,
		"tool:allowed":     true,
		"tool:read_file":   true,
		"subagent:planner": true,
		"agent:coder":      true,
		"trigger:user":     true,
	}
	for _, c := range caps {
		delete(want, c)
	}
	if len(want) != 0 {
		t.Fatalf("missing capabilities: %v", want)
	}
}

func TestExtractCapabilitiesMcpServer(t *testing.T) {
	m := ir.NewMcpServer("server", "npx", []string{"-y"}, map[string]string{"KEY": "val"}, nil, ir.SourceRef{})
	m.URL = "https://example.com"
	caps := ExtractCapabilities(m)
	want := []string{"mcp", "mcp:env", "mcp:remote"}
	for _, w := range want {
		found := false
		for _, c := range caps {
			if c == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing capability %q in %v", w, caps)
		}
	}
}

func TestExtractCapabilitiesCollection(t *testing.T) {
	c := ir.NewCollection("defaults", "1.0.0", "Desc", []ir.CollectionMember{
		{Kind: ir.KindSkill, Name: "review"},
		{Kind: ir.KindCommand, Name: "lint"},
	}, nil, nil, ir.SourceRef{})
	caps := ExtractCapabilities(c)
	want := []string{"collection", "collection:member:command", "collection:member:skill"}
	for _, w := range want {
		found := false
		for _, c := range caps {
			if c == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing capability %q in %v", w, caps)
		}
	}
}

func TestUniqueSorted(t *testing.T) {
	in := []string{"b", "a", "b", "c"}
	got := uniqueSorted(in)
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected %v", got)
	}
}
