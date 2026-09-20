package harnesscompile

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestWindsurfSubagentScopeIsNotBroadened(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"scope"}`)},
		"hooks/hooks.json":           {Data: []byte(`{"Stop":[{"hooks":[{"type":"command","command":"global-stop"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":"subagent-only"}]}]}`)},
	}
	result, err := Convert(src, HarnessClaude, TargetWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() || !diag.HasCode(result.Diagnostics, diag.CodeHookWindsurfEventUnmappable) {
		t.Errorf("missing unmappable-event warning: %v", result.Diagnostics)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Path != ".windsurf/hooks.json" {
		t.Fatalf("valid Stop hook was lost: %v", result.Artifacts)
	}
	var body struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &body); err != nil {
		t.Fatal(err)
	}
	entries := body.Hooks["post_cascade_response"]
	if len(entries) != 1 || entries[0].Command != "global-stop" {
		t.Fatalf("subagent-only hook gained global response scope: %s", result.Artifacts[0].Bytes)
	}
}

func TestWindsurfWriteGuardRetainsEditCoverage(t *testing.T) {
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_write_code":[{"command":"guard-write-and-edit"}]}}`)}}
	result, err := Convert(src, HarnessWindsurf, TargetClaude)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() || len(result.Artifacts) != 1 || result.Artifacts[0].Path != "hooks/hooks.json" {
		t.Fatalf("conversion failed: %+v", result)
	}
	var body struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &body); err != nil {
		t.Fatal(err)
	}
	entries := body.Hooks["PreToolUse"]
	if len(entries) != 1 || entries[0].Matcher != "Write|Edit" || entries[0].Command != "guard-write-and-edit" {
		t.Fatalf("write/edit guard narrowed: %s", result.Artifacts[0].Bytes)
	}
	for _, roundTrip := range []bool{false, true} {
		from, input := HarnessWindsurf, src
		if roundTrip {
			from, input = HarnessClaude, fstest.MapFS{"hooks/hooks.json": {Data: result.Artifacts[0].Bytes}}
		}
		back, err := Convert(input, from, TargetWindsurf)
		if err != nil || back.HasErrors() || len(back.Artifacts) != 1 {
			t.Fatalf("native write guard did not survive round trip: %v (%+v)", err, back)
		}
		var native struct {
			Hooks map[string][]struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if err := json.Unmarshal(back.Artifacts[0].Bytes, &native); err != nil {
			t.Fatal(err)
		}
		if len(native.Hooks["pre_write_code"]) != 1 || native.Hooks["pre_write_code"][0].Command != "guard-write-and-edit" {
			t.Fatalf("wrong native round trip: %s", back.Artifacts[0].Bytes)
		}
	}
}
