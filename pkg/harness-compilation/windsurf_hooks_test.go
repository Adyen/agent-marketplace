package harnesscompile

import (
	"encoding/json"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestWindsurfHookConversionMergesSameEventHandlers(t *testing.T) {
	src := fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"hooks"}`)},
		"hooks/hooks.json":           {Data: []byte(`{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"},{"type":"command","command":"echo second"}]}]}`)},
	}
	for _, name := range []string{"Convert", "CompileBundle"} {
		t.Run(name, func(t *testing.T) {
			var result *Result
			var err error
			if name == "Convert" {
				result, err = Convert(src, HarnessClaude, TargetWindsurf)
			} else {
				result, err = CompileBundle(src, TargetWindsurf)
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.HasErrors() || len(result.Artifacts) != 1 || result.Artifacts[0].Path != ".windsurf/hooks.json" {
				t.Fatalf("result artifacts=%#v diagnostics=%#v", result.Artifacts, result.Diagnostics)
			}
			var document struct {
				Hooks map[string][]struct {
					Command string `json:"command"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
				t.Fatal(err)
			}
			groups := document.Hooks["pre_run_command"]
			if len(groups) != 2 || groups[0].Command != "echo first" || groups[1].Command != "echo second" {
				t.Fatalf("native handlers = %#v, want first and second", groups)
			}
		})
	}
}

func TestWindsurfHookConversionPreservesHandlerOrder(t *testing.T) {
	src := fstest.MapFS{".windsurf/hooks.json": {Data: []byte(`{"hooks":{"pre_run_command":[{"command":"echo first"},{"command":"echo second"},{"command":"echo third"}]}}`)}}
	result, err := Convert(src, HarnessWindsurf, TargetWindsurf)
	if err != nil {
		t.Fatal(err)
	}
	if result.HasErrors() || len(result.Artifacts) != 1 {
		t.Fatalf("result artifacts=%#v diagnostics=%#v", result.Artifacts, result.Diagnostics)
	}
	var document struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(result.Artifacts[0].Bytes, &document); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, hook := range document.Hooks["pre_run_command"] {
		got = append(got, hook.Command)
	}
	if !reflect.DeepEqual(got, []string{"echo first", "echo second", "echo third"}) {
		t.Fatalf("native handlers = %v", got)
	}
}
