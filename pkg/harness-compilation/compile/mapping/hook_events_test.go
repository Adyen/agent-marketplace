package mapping

import (
	"strings"
	"testing"

	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

func TestHookEvent_ClaudeToWindsurf(t *testing.T) {
	tests := []struct {
		claudeEvent string
		matcher     string // optional tool matcher for PreToolUse
		want        string // expected Windsurf event
		ok          bool   // true if mapping exists
	}{
		// PreToolUse + tool-specific matchers → split-per-tool Windsurf events
		{"PreToolUse", "Read", "pre_read_code", true},
		{"PreToolUse", "Write", "pre_write_code", true},
		{"PreToolUse", "Edit", "pre_write_code", true},
		{"PreToolUse", "MultiEdit", "pre_write_code", true},
		{"PreToolUse", "Bash", "pre_run_command", true},
		{"PreToolUse", "Glob", "pre_read_code", true},
		{"PreToolUse", "Grep", "pre_read_code", true},
		{"PreToolUse", "WebFetch", "pre_read_code", true},
		{"PreToolUse", "WebSearch", "pre_read_code", true},

		// PreToolUse + MCP matcher → pre_mcp_tool_use
		{"PreToolUse", "mcp__server__tool", "pre_mcp_tool_use", true},
		{"PreToolUse", "mcp__anything", "pre_mcp_tool_use", true},

		// PreToolUse with no/empty matcher → unmappable
		{"PreToolUse", "", "", false},

		// Direct event mappings (no matcher needed)
		{"UserPromptSubmit", "", "pre_user_prompt", true},
		{"Stop", "", "post_cascade_response", true},
		{"SubagentStop", "", "", false},
		{"PreToolUse", "Write|Edit", "pre_write_code", true},
		{"Notification", "", "", false}, // no Windsurf equivalent
		{"PreCompact", "", "", false},   // no Windsurf equivalent
		{"PostToolUse", "", "", false},  // no Windsurf equivalent
	}
	for _, tt := range tests {
		got, ok := HookEvent(tt.claudeEvent, tt.matcher, TargetWindsurf)
		if ok != tt.ok {
			t.Errorf("HookEvent(%q, %q, Windsurf) = (%q, %v), want ok=%v", tt.claudeEvent, tt.matcher, got, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("HookEvent(%q, %q, Windsurf) = %q, want %q", tt.claudeEvent, tt.matcher, got, tt.want)
		}
	}
}

func TestHookEvent_ClaudeToClaude(t *testing.T) {
	// Claude → Claude should be identity
	tests := []struct {
		claudeEvent string
		matcher     string
		want        string
		ok          bool
	}{
		{"PreToolUse", "Read", "PreToolUse", true},
		{"UserPromptSubmit", "", "UserPromptSubmit", true},
		{"Stop", "", "Stop", true},
	}
	for _, tt := range tests {
		got, ok := HookEvent(tt.claudeEvent, tt.matcher, TargetClaude)
		if ok != tt.ok {
			t.Errorf("HookEvent(%q, %q, Claude) ok=%v, want %v", tt.claudeEvent, tt.matcher, ok, tt.ok)
		}
		if got != tt.want {
			t.Errorf("HookEvent(%q, %q, Claude) = %q, want %q", tt.claudeEvent, tt.matcher, got, tt.want)
		}
	}
}

func TestHookEvent_ClaudeToGemini(t *testing.T) {
	tests := []struct {
		claudeEvent string
		matcher     string
		want        string
		ok          bool
	}{
		{"PreToolUse", "Read", "BeforeTool", true},
		{"PostToolUse", "", "AfterTool", true},
		{"PreCompact", "", "PreCompress", true},
		{"UserPromptSubmit", "", "BeforeAgent", true},
		{"Stop", "", "", false},
	}
	for _, tt := range tests {
		got, ok := HookEvent(tt.claudeEvent, tt.matcher, TargetGemini)
		if ok != tt.ok {
			t.Errorf("HookEvent(%q, %q, Gemini) ok=%v, want %v", tt.claudeEvent, tt.matcher, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("HookEvent(%q, %q, Gemini) = %q, want %q", tt.claudeEvent, tt.matcher, got, tt.want)
		}
	}
}

func TestHookEvent_Unmappable(t *testing.T) {
	// Events that have no Windsurf equivalent
	_, ok := HookEvent("Notification", "", TargetWindsurf)
	if ok {
		t.Error("HookEvent(Notification, '', Windsurf) should be unmappable (ok=false)")
	}
}

func TestHookEvent_MCPMatcher(t *testing.T) {
	// Any matcher starting with "mcp__" should map to pre_mcp_tool_use for Windsurf
	tests := []struct {
		matcher string
		want    string
		ok      bool
	}{
		{"mcp__myserver__mytool", "pre_mcp_tool_use", true},
		{"mcp__x", "pre_mcp_tool_use", true},
		{"MCP_Tool", "", false}, // doesn't start with "mcp__"
		{"read", "", false},     // lowercase, not an MCP pattern
	}
	for _, tt := range tests {
		got, ok := HookEvent("PreToolUse", tt.matcher, TargetWindsurf)
		if ok != tt.ok {
			t.Errorf("HookEvent(PreToolUse, %q, Windsurf) ok=%v, want %v", tt.matcher, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("HookEvent(PreToolUse, %q, Windsurf) = %q, want %q", tt.matcher, got, tt.want)
		}
	}
}

func TestHookEvent_GeminiPromptRoundTrip(t *testing.T) {
	if got, ok := HookEvent("UserPromptSubmit", "", TargetGemini); !ok || got != "BeforeAgent" {
		t.Fatalf("HookEvent(UserPromptSubmit, Gemini) = (%q, %v), want (BeforeAgent, true)", got, ok)
	}
	if gotEvent, gotMatcher, ok := ReverseHookEvent("BeforeAgent", TargetGemini); !ok || gotEvent != "UserPromptSubmit" || gotMatcher != "" {
		t.Fatalf("ReverseHookEvent(BeforeAgent, Gemini) = (%q, %q, %v), want (UserPromptSubmit, \"\", true)", gotEvent, gotMatcher, ok)
	}
}

func TestHookEvent_WindsurfToClaude(t *testing.T) {
	// Reverse mapping: Windsurf event → Claude event + matcher
	tests := []struct {
		windsurfEvent string
		wantEvent     string
		wantMatcher   string
		ok            bool
	}{
		{"pre_read_code", "PreToolUse", "Read", true},
		{"pre_write_code", "PreToolUse", "Write|Edit", true},
		{"pre_run_command", "PreToolUse", "Bash", true},
		{"pre_mcp_tool_use", "PreToolUse", "mcp", true},
		{"pre_user_prompt", "UserPromptSubmit", "", true},
		{"post_cascade_response", "Stop", "", true},
		{"unknown_event", "", "", false},
	}
	for _, tt := range tests {
		gotEvent, gotMatcher, ok := ReverseHookEvent(tt.windsurfEvent, TargetWindsurf)
		if ok != tt.ok {
			t.Errorf("ReverseHookEvent(%q, Windsurf) ok=%v, want %v", tt.windsurfEvent, ok, tt.ok)
		}
		if ok {
			if gotEvent != tt.wantEvent {
				t.Errorf("ReverseHookEvent(%q, Windsurf) event = %q, want %q", tt.windsurfEvent, gotEvent, tt.wantEvent)
			}
			if gotMatcher != tt.wantMatcher {
				t.Errorf("ReverseHookEvent(%q, Windsurf) matcher = %q, want %q", tt.windsurfEvent, gotMatcher, tt.wantMatcher)
			}
		}
	}
}

func TestHookEventTable_Coverage(t *testing.T) {
	// Ensure all Claude events in the table have a cell for every declared
	// target (even if the value is "" meaning unmappable)
	allTargets := targetmeta.Targets()
	for _, entry := range hookEventTable {
		for _, tgt := range allTargets {
			if _, ok := entry.Targets[tgt]; !ok {
				t.Errorf("hook event entry %q missing target %s", entry.Claude, tgt)
			}
		}
	}
}

func TestHookEvent_AllClaudeEventsCovered(t *testing.T) {
	// Anchor: these Claude events must be in the table
	anchors := []string{
		"PreToolUse",
		"PostToolUse",
		"UserPromptSubmit",
		"Stop",
		"SubagentStop",
		"Notification",
		"PreCompact",
	}
	seen := make(map[string]bool)
	for _, entry := range hookEventTable {
		seen[entry.Claude] = true
	}
	for _, a := range anchors {
		if !seen[a] {
			t.Errorf("hook event table missing anchor entry %q", a)
		}
	}
}

func TestIsMCPMatcher(t *testing.T) {
	tests := []struct {
		matcher string
		want    bool
	}{
		{"mcp__server__tool", true},
		{"mcp__x", true},
		{"MCP__x", false},
		{"Read", false},
		{"", false},
		{"Bash", false},
	}
	for _, tt := range tests {
		got := strings.HasPrefix(tt.matcher, "mcp__")
		if got != tt.want {
			t.Errorf("mcp prefix check for %q = %v, want %v", tt.matcher, got, tt.want)
		}
	}
}
