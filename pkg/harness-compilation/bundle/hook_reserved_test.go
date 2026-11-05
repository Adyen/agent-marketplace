package bundle

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	_ "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/gemini"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

func TestLoadHooksWrappedGroupsFlattenAndCompile(t *testing.T) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo one"},{"type":"command","command":"echo two"}]}]}}`)}}
	hooks, _, err := LoadHooks(src, "hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 2 || hooks[0].Command != "echo one" || hooks[1].Command != "echo two" || hooks[0].Matcher != "Read" || hooks[1].Matcher != "Read" {
		t.Fatalf("hooks = %#v", hooks)
	}
	if hooks[0].Name() != "PostToolUse-command" || hooks[1].Name() != "PostToolUse-command-2" {
		t.Fatalf("names = %q, %q", hooks[0].Name(), hooks[1].Name())
	}
	for _, hook := range hooks {
		if _, ok := hook.Extra["hooks"]; ok {
			t.Fatalf("hook extra retained executable group: %#v", hook.Extra)
		}
		result, err := compile.Compile(hook, compile.TargetGemini)
		if err != nil {
			t.Fatal(err)
		}
		text := string(result.Artifacts[0].Bytes)
		if !strings.Contains(text, hook.Command) {
			t.Fatalf("compiled command missing: %s", text)
		}
		for _, other := range hooks {
			if other != hook && strings.Contains(text, other.Command) {
				t.Fatalf("compiled sibling command leaked: %s", text)
			}
		}
	}
}

// spec: hook-resources/workspace-and-wrapped-forms-flatten-identically
func TestLoadHooksWrappedAndWorkspaceFlatRemainSupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "wrapped flat", data: `{"hooks":{"PostToolUse":[{"type":"command","command":"echo wrapped"}]}}`},
		{name: "workspace flat", data: `{"PostToolUse":[{"type":"command","command":"echo workspace"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hooks, _, err := LoadHooks(fstest.MapFS{"hooks.json": {Data: []byte(tc.data)}}, "hooks.json")
			if err != nil || len(hooks) != 1 || hooks[0].Command == "" {
				t.Fatalf("hooks = %#v err=%v", hooks, err)
			}
		})
	}
}

func BenchmarkLoadWrappedHookGroups(b *testing.B) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo one"},{"type":"command","command":"echo two"}]}]}}`)}}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := LoadHooks(src, "hooks.json"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLoadHooksWrappedGroupsRejectMalformedAndAliases(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		code string
	}{
		{name: "null group", data: `{"hooks":{"PostToolUse":null}}`},
		{name: "command aliases", data: `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"one","Command":"two"}]}]}}`, code: diag.CodeJSONDuplicateKey},
		{name: "hooks aliases", data: `{"hooks":{"PostToolUse":[{"hooks":[{"type":"command","command":"one"}],"Hooks":[{"type":"command","command":"two"}]}]}}`, code: diag.CodeJSONDuplicateKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := LoadHooks(fstest.MapFS{"hooks.json": {Data: []byte(tc.data)}}, "hooks.json")
			if err == nil {
				t.Fatal("LoadHooks accepted malformed or ambiguous group")
			}
			if tc.code != "" {
				var coded *diag.CodedError
				if !errors.As(err, &coded) || coded.Code != tc.code {
					t.Fatalf("error = %v, want coded %s", err, tc.code)
				}
			}
		})
	}
}
