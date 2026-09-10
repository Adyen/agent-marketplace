package gemini

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

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
	if err := json.Unmarshal([]byte(`{"hooks":{"BeforeTool":[{"matcher":"Read|Edit","hooks":[{"type":"command","command":"echo audit","timeout":2500,"description":"audit commands","env":{"AUDIT_MODE":"local"}}]}]}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native hook definition = %s", artifacts[0].Bytes)
	}
}
