package gemini

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// Canonical hook input is translated through the shipped catalogs: the
// matcher covers the same concept set with Gemini tool names and the timeout
// converts to milliseconds (OpenSpec native-hook-integration).
func TestNativeHookDefinitionsUseMatcherGroups(t *testing.T) {
	hook := ir.NewHookWithMatcherOptions("audit", "PreToolUse", "Read|Edit", "echo audit", "command", map[string]any{"description": "audit commands", "env": map[string]any{"AUDIT_MODE": "local"}}, ir.SourceRef{}, ir.WithHookTimeout(2500))
	artifacts, diagnostics, err := translateHook(hook, compile.Options{})
	if err != nil || len(diagnostics) != 0 || len(artifacts) != 1 {
		t.Fatalf("translate: %v %+v %+v", err, diagnostics, artifacts)
	}
	var got map[string]any
	if err := json.Unmarshal(artifacts[0].Bytes, &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(`{"hooks":{"BeforeTool":[{"matcher":"^(read_file|read_many_files|replace)$","hooks":[{"type":"command","command":"echo audit","timeout":2500000,"description":"audit commands","env":{"AUDIT_MODE":"local"}}]}]}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native hook definition = %s", artifacts[0].Bytes)
	}
}
