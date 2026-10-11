package bundle

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadHooksRejectsDuplicateNestedKeys(t *testing.T) {
	fs := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"echo hi","metadata":{"name":"first","name":"second"}}]}}`)}}
	if _, _, err := LoadHooks(fs, "hooks.json"); err == nil {
		t.Fatal("LoadHooks accepted duplicate nested JSON key")
	}
}

func TestLoadHooksRejectsMalformedTopLevelHooks(t *testing.T) {
	fs := fstest.MapFS{"hooks.json": {Data: []byte(`{"hooks":[]}`)}}
	if _, _, err := LoadHooks(fs, "hooks.json"); err == nil {
		t.Fatal("LoadHooks accepted non-object top-level hooks")
	}
}

func TestLoadHooks(t *testing.T) {
	tests := []struct {
		name          string
		fs            fstest.MapFS
		path          string
		wantCount     int
		wantEvents    []string
		wantCommands  []string
		wantDiagCount int
		wantErr       bool
		errContain    string
	}{
		{
			name: "single command hook",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"echo hi"}]}}`),
				},
			},
			path:         "hooks/hooks.json",
			wantCount:    1,
			wantEvents:   []string{"PreCommit"},
			wantCommands: []string{"echo hi"},
		},
		{
			name: "multiple events with multiple rules",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"lint"}],"PostCommit":[{"type":"command","command":"notify"},{"type":"command","command":"log"}]}}`),
				},
			},
			path:      "hooks/hooks.json",
			wantCount: 3,
		},
		{
			name: "unsupported hook type produces warning",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"type":"http","command":"curl http://example.com"}]}}`),
				},
			},
			path:          "hooks/hooks.json",
			wantCount:     1,
			wantDiagCount: 1,
		},
		{
			name: "mixed supported and unsupported types",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"lint"},{"type":"prompt","command":"prompt-user"}]}}`),
				},
			},
			path:          "hooks/hooks.json",
			wantCount:     2,
			wantDiagCount: 1,
		},
		{
			name: "empty hooks object",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{}}`),
				},
			},
			path:      "hooks/hooks.json",
			wantCount: 0,
		},
		{
			name:       "path traversal rejected",
			fs:         fstest.MapFS{},
			path:       "../hooks/hooks.json",
			wantErr:    true,
			errContain: "path traversal",
		},
		{
			name: "invalid JSON",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{invalid json}`),
				},
			},
			path:       "hooks/hooks.json",
			wantErr:    true,
			errContain: "parse",
		},
		{
			name:       "missing file",
			fs:         fstest.MapFS{},
			path:       "hooks/missing.json",
			wantErr:    true,
			errContain: "read",
		},
		{
			name: "hook with extra fields preserved",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"lint","timeout":30}]}}`),
				},
			},
			path:      "hooks/hooks.json",
			wantCount: 1,
		},
		{
			name: "no type defaults to command",
			fs: fstest.MapFS{
				"hooks/hooks.json": {
					Data: []byte(`{"hooks":{"PreCommit":[{"command":"echo default"}]}}`),
				},
			},
			path:         "hooks/hooks.json",
			wantCount:    1,
			wantCommands: []string{"echo default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hooks, diags, err := LoadHooks(tt.fs, tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContain)
				}
				if !containsString(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(hooks) != tt.wantCount {
				t.Errorf("hook count: got %d, want %d", len(hooks), tt.wantCount)
			}
			if len(diags) != tt.wantDiagCount {
				t.Errorf("diag count: got %d, want %d", len(diags), tt.wantDiagCount)
			}
			if tt.wantEvents != nil && len(hooks) >= len(tt.wantEvents) {
				for i, want := range tt.wantEvents {
					if hooks[i].Event != want {
						t.Errorf("hook[%d].Event: got %q, want %q", i, hooks[i].Event, want)
					}
				}
			}
			if tt.wantCommands != nil && len(hooks) >= len(tt.wantCommands) {
				for i, want := range tt.wantCommands {
					if hooks[i].Command != want {
						t.Errorf("hook[%d].Command: got %q, want %q", i, hooks[i].Command, want)
					}
				}
			}
			// Check that unsupported-type diagnostics have correct code.
			for _, d := range diags {
				if d.Code != diag.CodeHookClaudeUnsupportedType {
					t.Errorf("diag code: got %q, want %q", d.Code, diag.CodeHookClaudeUnsupportedType)
				}
				if d.Severity != diag.SeverityWarning {
					t.Errorf("diag severity: got %v, want Warning", d.Severity)
				}
			}
		})
	}
}

func TestLoadHooks_HookExtraFields(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"lint","timeout":30,"mode":"strict"}]}}`),
		},
	}
	hooks, _, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	h := hooks[0]
	if h.Timeout == nil || *h.Timeout != 30 {
		t.Errorf("timeout: got %v, want 30", h.Timeout)
	}
	if v, ok := h.Extra["mode"]; !ok || v != "strict" {
		t.Errorf("extra[mode]: got %v, want %q", v, "strict")
	}
}

// spec: hook-resources/non-command-handler-type-loads-with-a-warning
func TestLoadHooks_HookTypeField(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{"hooks":{"PreCommit":[{"type":"http","command":"curl"}]}}`),
		},
	}
	hooks, diags, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Type != "http" {
		t.Errorf("type: got %q, want %q", hooks[0].Type, "http")
	}
	if hooks[0].Command != "curl" {
		t.Errorf("command: got %q, want %q", hooks[0].Command, "curl")
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Code != diag.CodeHookClaudeUnsupportedType {
		t.Errorf("diag code: got %q, want %q", diags[0].Code, diag.CodeHookClaudeUnsupportedType)
	}
}

func TestLoadHooks_SourceRef(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{"hooks":{"PreCommit":[{"type":"command","command":"lint"}]}}`),
		},
	}
	hooks, _, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	wantRef := ir.SourceRef{File: "hooks/hooks.json", Line: 0}
	if hooks[0].Source != wantRef {
		t.Errorf("source: got %+v, want %+v", hooks[0].Source, wantRef)
	}
}

func TestHookRuleUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantType    string
		wantCmd     string
		wantTimeout *int
		wantExtra   map[string]any
	}{
		{
			name:     "standard fields",
			input:    `{"type":"command","command":"echo"}`,
			wantType: "command",
			wantCmd:  "echo",
		},
		{
			name:        "typed timeout",
			input:       `{"type":"command","command":"echo","timeout":5}`,
			wantType:    "command",
			wantCmd:     "echo",
			wantTimeout: func() *int { v := 5; return &v }(),
		},
		{
			name:     "no type field",
			input:    `{"command":"echo"}`,
			wantType: "",
			wantCmd:  "echo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r hookRule
			if err := json.Unmarshal([]byte(tt.input), &r); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Type != tt.wantType {
				t.Errorf("type: got %q, want %q", r.Type, tt.wantType)
			}
			if r.Command != tt.wantCmd {
				t.Errorf("command: got %q, want %q", r.Command, tt.wantCmd)
			}
			if tt.wantTimeout != nil {
				if r.Timeout == nil || *r.Timeout != *tt.wantTimeout {
					t.Errorf("timeout: got %v, want %d", r.Timeout, *tt.wantTimeout)
				}
			}
			if tt.wantExtra != nil {
				if r.ExtraFields == nil {
					t.Error("expected non-nil ExtraFields")
				} else {
					for k, v := range tt.wantExtra {
						got, ok := r.ExtraFields[k]
						if !ok {
							t.Errorf("extra key %q missing", k)
						} else if got != v {
							t.Errorf("extra[%q]: got %v (%T), want %v (%T)", k, got, got, v, v)
						}
					}
				}
			}
		})
	}
}

func TestLoadHooks_ClaudeWorkspaceFormat(t *testing.T) {
	tests := []struct {
		name         string
		data         string
		wantCount    int
		wantEvents   []string
		wantCommands []string
		wantMatchers []string
	}{
		{
			name: "claude workspace format with matcher",
			data: `{
				"SessionStart": [
					{ "hooks": [ { "type": "command", "command": "echo start" } ] }
				],
				"PreToolUse": [
					{ "matcher": "Execute", "hooks": [ { "type": "command", "command": "block-npm" } ] }
				]
			}`,
			wantCount:    2,
			wantEvents:   []string{"PreToolUse", "SessionStart"},
			wantCommands: []string{"block-npm", "echo start"},
			wantMatchers: []string{"Execute", ""},
		},
		{
			name: "claude workspace format with multiple hooks per event",
			data: `{
				"PreToolUse": [
					{ "matcher": "Read", "hooks": [ { "type": "command", "command": "audit-read" } ] },
					{ "matcher": "Write", "hooks": [ { "type": "command", "command": "audit-write" } ] }
				]
			}`,
			wantCount:    2,
			wantEvents:   []string{"PreToolUse", "PreToolUse"},
			wantCommands: []string{"audit-read", "audit-write"},
			wantMatchers: []string{"Read", "Write"},
		},
		{
			name: "claude workspace format ignores metadata keys",
			data: `{
				"claudeHooksImported": true,
				"importedClaudeHooks": [],
				"showHookOutput": false,
				"SessionStart": [
					{ "hooks": [ { "type": "command", "command": "echo hello" } ] }
				]
			}`,
			wantCount:    1,
			wantEvents:   []string{"SessionStart"},
			wantCommands: []string{"echo hello"},
		},
		{
			name: "claude workspace format with no type defaults to command",
			data: `{
				"SessionEnd": [
					{ "hooks": [ { "command": "cleanup.sh" } ] }
				]
			}`,
			wantCount:    1,
			wantEvents:   []string{"SessionEnd"},
			wantCommands: []string{"cleanup.sh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := fstest.MapFS{
				"hooks/hooks.json": {Data: []byte(tt.data)},
			}
			hooks, diags, err := LoadHooks(src, "hooks/hooks.json")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(diags) > 0 {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if len(hooks) != tt.wantCount {
				t.Fatalf("hook count: got %d, want %d", len(hooks), tt.wantCount)
			}
			if tt.wantEvents != nil && len(hooks) >= len(tt.wantEvents) {
				for i, want := range tt.wantEvents {
					if hooks[i].Event != want {
						t.Errorf("hook[%d].Event: got %q, want %q", i, hooks[i].Event, want)
					}
				}
			}
			if tt.wantCommands != nil && len(hooks) >= len(tt.wantCommands) {
				for i, want := range tt.wantCommands {
					if hooks[i].Command != want {
						t.Errorf("hook[%d].Command: got %q, want %q", i, hooks[i].Command, want)
					}
				}
			}
			if tt.wantMatchers != nil && len(hooks) >= len(tt.wantMatchers) {
				for i, want := range tt.wantMatchers {
					if hooks[i].Matcher != want {
						t.Errorf("hook[%d].Matcher: got %q, want %q", i, hooks[i].Matcher, want)
					}
				}
			}
		})
	}
}

// spec: hook-resources/repeated-event-and-type-receive-distinct-names
func TestLoadHooks_AssignsDistinctNamesToRepeatedEventTypes(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{
				"SessionStart": [
					{ "hooks": [ { "type": "command", "command": "load-project-context" } ] },
					{ "hooks": [ { "type": "command", "command": "load-open-issues" } ] }
				]
			}`),
		},
	}

	hooks, _, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("hook count: got %d, want 2", len(hooks))
	}

	gotNames := []string{hooks[0].Name(), hooks[1].Name()}
	wantNames := []string{"SessionStart-command", "SessionStart-command-2"}
	if gotNames[0] != wantNames[0] || gotNames[1] != wantNames[1] {
		t.Errorf("hook names: got %v, want %v", gotNames, wantNames)
	}
}

// spec: hook-resources/repeated-event-and-type-receive-distinct-names
func TestLoadHooks_SuffixedNameDoesNotCollideWithTypedBaseName(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{
				"SessionStart": [
					{ "hooks": [ { "type": "command", "command": "first" } ] },
					{ "hooks": [ { "type": "command", "command": "second" } ] },
					{ "hooks": [ { "type": "command-2", "command": "third" } ] }
				]
			}`),
		},
	}

	hooks, _, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(hooks) != 3 {
		t.Fatalf("hook count: got %d, want 3", len(hooks))
	}

	seen := make(map[string]string, len(hooks))
	for _, h := range hooks {
		if prev, dup := seen[h.Name()]; dup {
			t.Fatalf("duplicate hook name %q for commands %q and %q", h.Name(), prev, h.Command)
		}
		seen[h.Name()] = h.Command
	}
}

func TestLoadHooks_CommentStripping(t *testing.T) {
	src := fstest.MapFS{
		"hooks/hooks.json": {
			Data: []byte(`{
				"hooks": {
					"PreCommit": [
						// This is a comment
						{"type": "command", "command": "lint"}
					]
				}
			}`),
		},
	}
	hooks, _, err := LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Command != "lint" {
		t.Errorf("command: got %q, want %q", hooks[0].Command, "lint")
	}
}

func TestLoadHooksDefaultDoesNotPromoteFactoryGroupCommandRegex(t *testing.T) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh"}]}]}`)}}
	hooks, _, err := LoadHooks(src, "hooks.json")
	if err != nil {
		t.Fatalf("LoadHooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].Matcher != "Execute" || hooks[0].Command != "${DROID_PLUGIN_ROOT}/run.sh" || hooks[0].HookSource != "" {
		t.Fatalf("default hook = %#v", hooks)
	}
	if _, ok := hooks[0].Extra["commandRegex"]; ok {
		t.Fatalf("default bundle loader promoted group commandRegex into handler extra: %#v", hooks[0].Extra)
	}
}

func TestLoadFactoryHooksForNativeProjectionPreservesGroupCommandRegex(t *testing.T) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"PreToolUse":[{"matcher":"Execute","commandRegex":"^make.*$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/one.sh"},{"type":"command","command":"${DROID_PLUGIN_ROOT}/two.sh"}]}]}`)}}
	hooks, diags, err := LoadFactoryHooksForNativeProjection(src, "hooks.json")
	if err != nil {
		t.Fatalf("LoadFactoryHooksForNativeProjection: %v", err)
	}
	if len(diags) != 0 || len(hooks) != 2 {
		t.Fatalf("hooks/diagnostics = %#v / %#v", hooks, diags)
	}
	for _, hook := range hooks {
		if hook.Event != "PreToolUse" || hook.Matcher != "Execute" || !strings.HasPrefix(hook.Command, "${DROID_PLUGIN_ROOT}/") || hook.HookSource != "" {
			t.Fatalf("bundle native hook = %#v", hook)
		}
		if hook.Extra["commandRegex"] != "^make.*$" {
			t.Fatalf("hook extra = %#v, want group commandRegex", hook.Extra)
		}
	}
	hooks[0].Extra["commandRegex"] = "mutated"
	if hooks[1].Extra["commandRegex"] != "^make.*$" {
		t.Fatalf("per-handler extras alias each other: %#v", hooks)
	}
}

func TestLoadFactoryHooksForNativeProjectionFlatFallback(t *testing.T) {
	src := fstest.MapFS{"hooks.json": {Data: []byte(`{"PreToolUse":[{"matcher":"Execute","type":"command","command":"${DROID_PLUGIN_ROOT}/flat.sh","commandRegex":"^flat$"}]}`)}}
	hooks, _, err := LoadFactoryHooksForNativeProjection(src, "hooks.json")
	if err != nil {
		t.Fatalf("LoadFactoryHooksForNativeProjection: %v", err)
	}
	if len(hooks) != 1 || hooks[0].Type != "command" || hooks[0].Matcher != "Execute" || hooks[0].Command != "${DROID_PLUGIN_ROOT}/flat.sh" || hooks[0].Extra["commandRegex"] != "^flat$" {
		t.Fatalf("flat Factory native hook = %#v", hooks)
	}
}

func TestLoadFactoryHooksForNativeProjectionRejectsFlatNonStringCommandRegex(t *testing.T) {
	data := []byte(`{"PreToolUse":[{"matcher":"Execute","type":"command","command":"${DROID_PLUGIN_ROOT}/flat.sh","commandRegex":7}]}`)
	src := fstest.MapFS{"hooks.json": {Data: data}}
	hooks, _, err := LoadFactoryHooksForNativeProjection(src, "hooks.json")
	if err == nil || len(hooks) != 0 {
		t.Fatalf("flat native load = %#v, %v; want refusal", hooks, err)
	}
	if string(src["hooks.json"].Data) != string(data) {
		t.Fatal("source snapshot changed during refusal")
	}
}

func TestLoadFactoryHooksForNativeProjectionRejectsUnrepresentableGroups(t *testing.T) {
	handler := `"hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh"}]`
	cases := []struct {
		name  string
		group string
	}{
		{name: "group type present empty", group: `"type":"","matcher":"Execute",` + handler},
		{name: "group command present empty", group: `"command":"","matcher":"Execute",` + handler},
		{name: "group prompt present empty", group: `"prompt":"","matcher":"Execute",` + handler},
		{name: "group timeout present zero", group: `"timeout":0,"matcher":"Execute",` + handler},
		{name: "empty group string extra", group: `"matcher":"Execute","emptyOption":"",` + handler},
		{name: "empty group null extra", group: `"matcher":"Execute","emptyOption":null,` + handler},
		{name: "empty group object extra", group: `"matcher":"Execute","emptyOption":{},` + handler},
		{name: "empty group array extra", group: `"matcher":"Execute","emptyOption":[],` + handler},
		{name: "unsupported group extra", group: `"matcher":"Execute","providerOption":{"mode":"strict"},` + handler},
		{name: "non-string commandRegex", group: `"matcher":"Execute","commandRegex":7,` + handler},
		{name: "group handler commandRegex conflict", group: `"matcher":"Execute","commandRegex":"^group$","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh","commandRegex":"^handler$"}]`},
		{name: "handler commandRegex without group", group: `"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh","commandRegex":"^handler$"}]`},
		{name: "handler commandRegex alias without group", group: `"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh","CommandRegex":"^handler$"}]`},
		{name: "nested handler matcher present", group: `"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh","matcher":"Read"}]`},
		{name: "nested handler matcher empty present", group: `"matcher":"Execute","hooks":[{"type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh","matcher":""}]`},
		{name: "commandRegex alias lower first", group: `"matcher":"Execute","commandRegex":"^group$","CommandRegex":"^alias$",` + handler},
		{name: "commandRegex alias upper first", group: `"matcher":"Execute","CommandRegex":"^alias$","commandRegex":"^group$",` + handler},
		{name: "typed type alias lower first", group: `"matcher":"Execute","hooks":[{"type":"command","Type":"prompt","command":"${DROID_PLUGIN_ROOT}/run.sh"}]`},
		{name: "typed type alias upper first", group: `"matcher":"Execute","hooks":[{"Type":"prompt","type":"command","command":"${DROID_PLUGIN_ROOT}/run.sh"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(`{"PreToolUse":[{` + tc.group + `}]}`)
			src := fstest.MapFS{"hooks.json": {Data: data}}
			before := string(src["hooks.json"].Data)
			hooks, _, err := LoadFactoryHooksForNativeProjection(src, "hooks.json")
			if err == nil {
				t.Fatalf("LoadFactoryHooksForNativeProjection returned hooks %#v without refusing", hooks)
			}
			if len(hooks) != 0 {
				t.Fatalf("refusal returned hooks %#v", hooks)
			}
			if string(src["hooks.json"].Data) != before {
				t.Fatal("source snapshot changed during refusal")
			}
		})
	}
}
