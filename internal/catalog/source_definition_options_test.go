package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func legacyHookDefinitionDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestLegacyLoadOptionsBindObservedHookDefinition(t *testing.T) {
	claudeDefinition := []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo legacy-bare-mcp"}]}]}}`)
	claude, err := LoadFSWithOptions(fstest.MapFS{
		".claude/settings.json": {Data: claudeDefinition},
	}, "claude", IngestOptions{LegacyBareMCP: true})
	if err != nil {
		t.Fatalf("legacy bare-MCP Claude load: %v", err)
	}
	if len(claude.Entries) != 1 || claude.Entries[0].Kind != ir.KindHook {
		t.Fatalf("legacy bare-MCP Claude entries = %#v, want one hook", claude.Entries)
	}
	claudeHook, ok := claude.Entries[0].Resource.(*ir.Hook)
	if !ok || claudeHook.Source.File != ".claude/settings.json" || claudeHook.HookSource != "claude" {
		t.Fatalf("legacy bare-MCP Claude hook = %#v", claude.Entries[0].Resource)
	}
	if got, want := claudeHook.SourceDefinitionDigest(), legacyHookDefinitionDigest(claudeDefinition); got != want {
		t.Fatalf("legacy bare-MCP Claude SourceDefinitionDigest = %q, want %q", got, want)
	}

	windsurfDefinition := []byte(`{"hooks":{"pre_run_command":[{"command":"echo legacy-windsurf"}]}}`)
	windsurf, err := LoadFSWithOptions(fstest.MapFS{
		".windsurf/hooks.json": {Data: windsurfDefinition},
	}, "windsurf", IngestOptions{LegacyWindsurfHooks: true})
	if err != nil {
		t.Fatalf("legacy Windsurf hook load: %v", err)
	}
	if len(windsurf.Entries) != 1 || windsurf.Entries[0].Kind != ir.KindHook || windsurf.Entries[0].Name != "hooks" {
		t.Fatalf("legacy Windsurf entries = %#v, want the hooks inventory resource", windsurf.Entries)
	}
	windsurfHook, ok := windsurf.Entries[0].Resource.(*ir.Hook)
	if !ok || windsurfHook.Source.File != ".windsurf/hooks.json" || windsurfHook.HookSource != "windsurf" {
		t.Fatalf("legacy Windsurf hook = %#v", windsurf.Entries[0].Resource)
	}
	if got, want := windsurfHook.SourceDefinitionDigest(), legacyHookDefinitionDigest(windsurfDefinition); got != want {
		t.Fatalf("legacy Windsurf SourceDefinitionDigest = %q, want %q", got, want)
	}
}
