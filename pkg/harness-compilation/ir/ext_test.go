package ir

import (
	"testing"

	extpkg "github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir/ext"
)

func TestResourcesStartWithZeroValueExtBag(t *testing.T) {
	if got := NewSkill("skill", "desc", "body", nil, nil, SourceRef{}).Ext; got != (extpkg.Bag{}) {
		t.Fatalf("skill Ext = %#v, want zero-value bag", got)
	}
	if got := NewCommand("cmd", "desc", "body", nil, SourceRef{}).Ext; got != (extpkg.Bag{}) {
		t.Fatalf("command Ext = %#v, want zero-value bag", got)
	}
	if got := NewSubagent("agent", "desc", "body", "sonnet", nil, "", nil, "", nil, SourceRef{}).Ext; got != (extpkg.Bag{}) {
		t.Fatalf("subagent Ext = %#v, want zero-value bag", got)
	}
	if got := NewHook("hook", "PreToolUse", "echo hi", "command", nil, SourceRef{}).Ext; got != (extpkg.Bag{}) {
		t.Fatalf("hook Ext = %#v, want zero-value bag", got)
	}
	if got := NewMcpServer("server", "npx", nil, nil, nil, SourceRef{}).Ext; got != (extpkg.Bag{}) {
		t.Fatalf("mcp Ext = %#v, want zero-value bag", got)
	}
}
