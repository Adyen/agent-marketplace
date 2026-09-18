package gemini_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest/gemini"
)

func TestNativeHookGroupsPreserveMatcherAndExtraLocations(t *testing.T) {
	src := fstest.MapFS{"settings.json": {Data: []byte(`{"hooks":{"BeforeTool":[{"matcher":"Read","sequential":true,"x-group":{"policy":"keep"},"hooks":[{"type":"command","command":"echo one","matcher":"ignored-inner","x-rule":"one","hooks":[{"unknown":true}]},{"type":"command","command":"echo two","name":"named"}]}]}}`)}}
	hooks, diagnostics, err := gemini.LoadHooks(src, "settings.json")
	if err != nil || len(diagnostics) != 0 || len(hooks) != 2 {
		t.Fatalf("load grouped hooks: %v %+v %+v", err, diagnostics, hooks)
	}
	if hooks[0].Matcher != "Read" || hooks[1].Matcher != "Read" || hooks[0].Command != "echo one" || hooks[1].Command != "echo two" || hooks[0].Name() == hooks[1].Name() {
		t.Fatalf("grouped hook semantics: %+v %+v", hooks[0], hooks[1])
	}
	result, err := compile.Compile(hooks[0], compile.TargetGemini)
	if err != nil || len(result.Artifacts) != 1 {
		t.Fatalf("compile: %v %+v", err, result)
	}
	var got, want map[string]any
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"hooks":{"BeforeTool":[{"matcher":"Read","sequential":true,"x-group":{"policy":"keep"},"hooks":[{"type":"command","command":"echo one","x-rule":"one"}]}]}}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group/rule fields moved or unselected siblings emitted: %s", result.Artifacts[0].Bytes)
	}
	if !diag.HasCode(result.Diagnostics, diag.CodeCanonicalFieldOverridesExtra) {
		t.Fatalf("diagnostics = %#v, want canonical-field diagnostic", result.Diagnostics)
	}
}

func TestLegacyFlatHookInputRemainsSupported(t *testing.T) {
	src := fstest.MapFS{"settings.json": {Data: []byte(`{"hooks":{"BeforeTool":[{"type":"command","command":"echo legacy","matcher":"Edit","x-rule":true}]}}`)}}
	hooks, diagnostics, err := gemini.LoadHooks(src, "settings.json")
	if err != nil || len(diagnostics) != 0 || len(hooks) != 1 {
		t.Fatalf("load legacy: %v %+v %+v", err, diagnostics, hooks)
	}
	if hooks[0].Command != "echo legacy" || hooks[0].Matcher != "Edit" || hooks[0].Extra["x-rule"] != true {
		t.Fatalf("legacy fields changed: %+v", hooks[0])
	}
}
