package mapping

import "testing"

func TestCodexReasoningEffortClassification(t *testing.T) {
	tests := []struct {
		value     string
		canonical string
		native    string
		valid     bool
	}{
		{"", "", "", true},
		{"low", "low", "", true},
		{"medium", "medium", "", true},
		{"high", "high", "", true},
		{"minimal", "", "minimal", true},
		{"xhigh", "", "xhigh", true},
		{"max", "", "max", true},
		{"ultra", "", "ultra", true},
		{"none", "", "none", true},
		{"future-effort", "", "future-effort", true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			canonical, native, valid := CodexReasoningEffort(tt.value)
			if canonical != tt.canonical || native != tt.native || valid != tt.valid {
				t.Fatalf("CodexReasoningEffort(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.value, canonical, native, valid, tt.canonical, tt.native, tt.valid)
			}
		})
	}
}

func TestCodexSandboxModesAreClosed(t *testing.T) {
	for _, mode := range []string{"", "read-only", "workspace-write", "danger-full-access"} {
		if !CodexSandboxModeSupported(mode) {
			t.Errorf("CodexSandboxModeSupported(%q) = false", mode)
		}
	}
	for _, mode := range []string{"host-root", "workspace", "dangerous"} {
		if CodexSandboxModeSupported(mode) {
			t.Errorf("CodexSandboxModeSupported(%q) = true", mode)
		}
	}
}

func TestCodexModelMappingsDoNotInventCrossVendorEquivalents(t *testing.T) {
	for _, entry := range modelTable {
		got, ok := Model(entry.Claude, TargetCodex)
		if got != "" || ok {
			t.Errorf("Model(%q, Codex) = (%q, %v), want (\"\", false)", entry.Claude, got, ok)
		}
	}
}

func TestCodexToolMappingsDoNotInventSubagentRestrictions(t *testing.T) {
	for _, entry := range toolTable {
		got, ok := entry.Targets[TargetCodex]
		if !ok {
			t.Errorf("tool entry %q has no explicit Codex cell", entry.Claude)
			continue
		}
		if got != nil {
			t.Errorf("tool entry %q Codex mapping = %v, want nil", entry.Claude, got)
		}
	}
}

func TestCodexHookEvent(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"PreToolUse", "PreToolUse", "PreToolUse", true},
		{"PostToolUse", "PostToolUse", "PostToolUse", true},
		{"PermissionRequest has no direct equivalent", "PermissionRequest", "", false},
		{"UserPromptSubmit", "UserPromptSubmit", "UserPromptSubmit", true},
		{"Stop", "Stop", "Stop", true},
		{"SubagentStop has no direct equivalent", "SubagentStop", "", false},
		{"SubagentStart has no direct equivalent", "SubagentStart", "", false},
		{"PreCompact has no direct equivalent", "PreCompact", "", false},
		{"PostCompact has no direct equivalent", "PostCompact", "", false},
		{"SessionStart", "SessionStart", "SessionStart", true},
		{"SessionEnd has no direct equivalent", "SessionEnd", "", false},
		{"Notification is unmappable", "Notification", "", false},
		{"PostCompaction is not Codex PostCompact", "PostCompaction", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := HookEvent(tt.source, "", TargetCodex)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("HookEvent(%q, Codex) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestNativeCodexHookEventDoesNotEquateSameNamedLifecycles(t *testing.T) {
	for _, event := range []string{"PermissionRequest", "SubagentStart", "SubagentStop", "PreCompact", "PostCompact", "SessionEnd"} {
		if got, ok := NativeCodexHookEvent(event, "", TargetClaude); ok || got != "" {
			t.Errorf("NativeCodexHookEvent(%q, Claude) = %q, %v; want unmappable", event, got, ok)
		}
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"} {
		if got, ok := NativeCodexHookEvent(event, "", TargetClaude); !ok || got != event {
			t.Errorf("NativeCodexHookEvent(%q, Claude) = %q, %v; want identity", event, got, ok)
		}
	}
}

func TestCodexEnvTokenRequiresCallerSuppliedPluginRoot(t *testing.T) {
	value, literal := EnvToken("${CLAUDE_PLUGIN_ROOT}", TargetCodex)
	if value != "" || !literal {
		t.Errorf("EnvToken(${CLAUDE_PLUGIN_ROOT}, Codex) = (%q, %v), want (\"\", true)", value, literal)
	}
}

func TestCodexMcpInterpolation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
		wantOK bool
	}{
		{"inline values remain inline", "inline", "inline", true},
		{"environment references are supported", "${env:VAR}", "${env:VAR}", true},
		{"file interpolation is unmappable", "${file:PATH}", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := McpInterpolationSupported(tt.source, TargetCodex)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("McpInterpolationSupported(%q, Codex) = (%q, %v), want (%q, %v)", tt.source, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCodexReverseHookEventsAreClosed(t *testing.T) {
	for _, entry := range codexReverseTable {
		got, matcher, ok := ReverseHookEvent(entry.TargetEvent, TargetCodex)
		if !ok || got != entry.ClaudeEvent || matcher != entry.Matcher {
			t.Errorf("ReverseHookEvent(%q, Codex) = (%q, %q, %v)", entry.TargetEvent, got, matcher, ok)
		}
	}
	for _, event := range []string{"Notification", "PostCompaction", "MadeUpEvent"} {
		if _, _, ok := ReverseHookEvent(event, TargetCodex); ok {
			t.Errorf("ReverseHookEvent(%q, Codex) accepted unsupported event", event)
		}
	}
}
