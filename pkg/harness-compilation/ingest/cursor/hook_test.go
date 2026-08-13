package cursor

import "testing"

func TestLoadHooks_NormalizesCursorPluginRootToken(t *testing.T) {
	hooks, diags, err := LoadHooks(cursorWorkspaceFS, ".cursor/hooks/hooks.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("LoadHooks diagnostics = %d, want 0", len(diags))
	}
	if len(hooks) != 1 {
		t.Fatalf("LoadHooks hooks = %d, want 1", len(hooks))
	}
	if hooks[0].Command != "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh" {
		t.Fatalf("Command = %q, want %q", hooks[0].Command, "${CLAUDE_PLUGIN_ROOT}/scripts/check.sh")
	}
	if hooks[0].Source.File != "hooks/hooks.json" {
		t.Fatalf("Source.File = %q, want %q", hooks[0].Source.File, "hooks/hooks.json")
	}
}
