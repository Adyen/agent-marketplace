package tui

import (
	"slices"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: plugin-packages/plugin-members-appear-under-their-plugin
func TestPluginMembersAppearBelowOneParentRow(t *testing.T) {
	members := []ir.PluginMember{
		{Resource: ir.NewSkill("review", "", "", nil, nil, ir.SourceRef{})},
		{Resource: ir.NewCommand("run-check", "", "", nil, ir.SourceRef{})},
		{Resource: ir.NewSubagent("reviewer", "", "", "", nil, "", nil, "", nil, ir.SourceRef{})},
		{Resource: ir.NewHook("before-review", "PreToolUse", "./scripts/run.sh", "command", nil, ir.SourceRef{})},
		{Resource: ir.NewMcpServer("review-index", "./scripts/mcp.sh", nil, nil, nil, ir.SourceRef{})},
	}
	plugin := &ir.Plugin{
		Format:   "claude-plugin",
		Manifest: ir.Manifest{Name: "review-kit"},
		Members:  members,
	}
	parent := row{Artifact: resolver.ResolvedArtifact{
		Entry: catalog.Entry{
			Kind: ir.KindPlugin, Name: plugin.Name(), Resource: plugin,
			Plugin: &catalog.PluginEntry{Format: plugin.Format, Members: members},
		},
		Selection: &resolver.PluginSelection{Members: members},
	}}

	items := buildStatusTree([]row{parent}, nil)
	var displayed []flatItem
	for _, item := range items {
		if item.Kind != flatSection {
			displayed = append(displayed, item)
		}
	}
	want := []string{"review-kit", "review", "run-check", "reviewer", "before-review", "review-index"}
	got := make([]string, 0, len(displayed))
	for _, item := range displayed {
		got = append(got, item.Row.Artifact.Name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("plugin rows = %v, want one parent followed by its members %v", got, want)
	}
	if displayed[0].Depth != 1 {
		t.Fatalf("plugin parent depth = %d, want top-level depth 1", displayed[0].Depth)
	}
	for _, item := range displayed[1:] {
		if item.Depth != 2 {
			t.Errorf("member %q depth = %d, want child depth 2", item.Row.Artifact.Name, item.Depth)
		}
	}
}
