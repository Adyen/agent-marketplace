package harnesscompile

// Native hook-location ingestion tests (OpenSpec change
// native-hook-integration). Each ingester preserves the authored native
// event, matcher, timeout, and command and records the actual native
// vocabulary as HookSource; canonical translation happens only in the
// catalog resolver. Expected values are hand-authored fixture data, never
// derived from the shipped catalogs.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func ingestedHooks(t *testing.T, src fs.FS, from Harness) []*ir.Hook {
	t.Helper()
	b, err := IngestBundle(src, from)
	if err != nil {
		t.Fatalf("IngestBundle(%s): %v", from, err)
	}
	var hooks []*ir.Hook
	for _, r := range b.Resources {
		if h, ok := r.(*ir.Hook); ok {
			hooks = append(hooks, h)
		}
	}
	return hooks
}

func ingestedOneHook(t *testing.T, src fs.FS, from Harness) *ir.Hook {
	t.Helper()
	hooks := ingestedHooks(t, src, from)
	if len(hooks) != 1 {
		t.Fatalf("want exactly one ingested hook, got %d", len(hooks))
	}
	return hooks[0]
}

// spec: hook-resources/claude-settings-hooks-are-ingested
func TestNativeHookIngestClaudeSettings(t *testing.T) {
	src := fstest.MapFS{
		".claude/settings.json": &fstest.MapFile{Data: []byte(`{
			"permissions": {"allow": ["Bash(ls)"]},
			"model": "opus",
			"hooks": {
				"PreToolUse": [
					{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo ac61-settings"}]}
				]
			}
		}`)},
	}
	b, err := IngestBundle(src, HarnessClaude)
	if err != nil {
		t.Fatalf("IngestBundle: %v", err)
	}
	var hooks []*ir.Hook
	for _, r := range b.Resources {
		if h, ok := r.(*ir.Hook); ok {
			hooks = append(hooks, h)
			continue
		}
		t.Fatalf("settings members other than hooks must not become resources, got %T", r)
	}
	if len(hooks) != 1 {
		t.Fatalf("want exactly one hook per handler, got %d", len(hooks))
	}
	h := hooks[0]
	if h.Event != "PreToolUse" || h.Matcher != "Bash" || h.Command != "echo ac61-settings" {
		t.Fatalf("hook = {event:%q matcher:%q command:%q}, want the authored native values", h.Event, h.Matcher, h.Command)
	}
	if h.HookSource != "claude" {
		t.Fatalf("HookSource = %q, want claude", h.HookSource)
	}
}

// spec: hook-resources/cursor-native-hooks-retain-native-events-on-ingestion
func TestNativeHookIngestCursorVersionedEnvelope(t *testing.T) {
	src := fstest.MapFS{
		".cursor/hooks.json": &fstest.MapFile{Data: []byte(`{
			"version": 1,
			"hooks": {
				"beforeSubmitPrompt": [{"type": "command", "command": "echo ac62-cursor"}]
			}
		}`)},
	}
	h := ingestedOneHook(t, src, HarnessCursor)
	if h.Event != "beforeSubmitPrompt" || h.Command != "echo ac62-cursor" {
		t.Fatalf("hook = {event:%q command:%q}, want the authored native values", h.Event, h.Command)
	}
	if h.HookSource != "cursor" {
		t.Fatalf("HookSource = %q, want cursor", h.HookSource)
	}
	if got := compileHook(t, TargetCursor, h); got.event != "beforeSubmitPrompt" || got.command != "echo ac62-cursor" {
		t.Fatalf("cursor roundtrip = {event:%q command:%q}", got.event, got.command)
	}
	if got := compileHook(t, TargetClaude, h); got.event != "UserPromptSubmit" || got.command != "echo ac62-cursor" {
		t.Fatalf("claude translation = {event:%q command:%q}, want UserPromptSubmit", got.event, got.command)
	}
}

// spec: hook-resources/devin-desktop-workspace-hooks-are-ingested-for-windsurf
func TestNativeHookIngestWindsurfDevinDesktop(t *testing.T) {
	src := fstest.MapFS{
		".devin/hooks.json": &fstest.MapFile{Data: []byte(`{
			"hooks": {
				"pre_run_command": [{"type": "command", "command": "echo ac63-windsurf"}]
			}
		}`)},
	}
	h := ingestedOneHook(t, src, HarnessWindsurf)
	if h.Event != "pre_run_command" || h.Matcher != "" || h.Command != "echo ac63-windsurf" {
		t.Fatalf("hook = {event:%q matcher:%q command:%q}, want the authored native values with no fabricated matcher", h.Event, h.Matcher, h.Command)
	}
	if h.HookSource != "windsurf" {
		t.Fatalf("HookSource = %q, want windsurf", h.HookSource)
	}
	got := compileHook(t, TargetWindsurf, h)
	if got.event != "pre_run_command" || got.hasMatcher || got.command != "echo ac63-windsurf" {
		t.Fatalf("windsurf roundtrip = {event:%q matcher:%q command:%q}", got.event, got.matcher, got.command)
	}
	got = compileHook(t, TargetClaude, h)
	if got.event != "PreToolUse" || got.matcher != "Bash" || got.command != "echo ac63-windsurf" {
		t.Fatalf("claude translation = {event:%q matcher:%q command:%q}, want PreToolUse matcher Bash", got.event, got.matcher, got.command)
	}
}

func TestNativeHookIngestSameHarnessRoundTrips(t *testing.T) {
	cases := []struct {
		name         string
		src          fstest.MapFS
		from         Harness
		target       Target
		wantEvent    string
		wantMatcher  string
		wantCommand  string
		wantTimeout  *int
		wantSource   string
		wantExtraKey string
		wantExtraVal any
	}{
		{
			name: "claude settings",
			src: fstest.MapFS{".claude/settings.json": &fstest.MapFile{Data: []byte(
				`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo rt-claude"}]}]}}`)}},
			from: HarnessClaude, target: TargetClaude, wantEvent: "PreToolUse", wantMatcher: "Bash", wantCommand: "echo rt-claude", wantSource: "claude",
		},
		{
			name: "factory standalone",
			src: fstest.MapFS{".factory/hooks.json": &fstest.MapFile{Data: []byte(
				`{"PreToolUse": [{"matcher": "Execute", "commandRegex": "^echo", "hooks": [{"type": "command", "command": "echo rt-factory"}]}]}`)}},
			from: HarnessFactory, target: TargetFactory, wantEvent: "PreToolUse", wantMatcher: "Execute", wantCommand: "echo rt-factory", wantSource: "factory",
			wantExtraKey: "commandRegex", wantExtraVal: "^echo",
		},
		{
			name: "cursor versioned envelope",
			src: fstest.MapFS{".cursor/hooks.json": &fstest.MapFile{Data: []byte(
				`{"version": 1, "hooks": {"beforeSubmitPrompt": [{"type": "command", "command": "echo rt-cursor"}]}}`)}},
			from: HarnessCursor, target: TargetCursor, wantEvent: "beforeSubmitPrompt", wantMatcher: "", wantCommand: "echo rt-cursor", wantSource: "cursor",
		},
		{
			name: "windsurf devin workspace file",
			src: fstest.MapFS{".devin/hooks.json": &fstest.MapFile{Data: []byte(
				`{"hooks": {"pre_run_command": [{"type": "command", "command": "echo rt-windsurf"}]}}`)}},
			from: HarnessWindsurf, target: TargetWindsurf, wantEvent: "pre_run_command", wantMatcher: "", wantCommand: "echo rt-windsurf", wantSource: "windsurf",
		},
		{
			name: "gemini settings",
			src: fstest.MapFS{".gemini/settings.json": &fstest.MapFile{Data: []byte(
				`{"hooks": {"BeforeTool": [{"matcher": "run_shell_command", "hooks": [{"type": "command", "command": "echo rt-gemini", "timeout": 2500}]}]}}`)}},
			from: HarnessGemini, target: TargetGemini, wantEvent: "BeforeTool", wantMatcher: "run_shell_command", wantCommand: "echo rt-gemini", wantTimeout: intPtr(2500), wantSource: "gemini",
		},
		{
			name: "devin standalone",
			src: fstest.MapFS{".devin/hooks.v1.json": &fstest.MapFile{Data: []byte(
				`{"PostCompaction": [{"type": "command", "command": "echo rt-devin"}]}`)}},
			from: HarnessDevin, target: TargetDevin, wantEvent: "PostCompaction", wantMatcher: "", wantCommand: "echo rt-devin", wantSource: "devin",
		},
		{
			name: "codex native file",
			src: fstest.MapFS{".codex/hooks.json": &fstest.MapFile{Data: []byte(
				`{"hooks": {"SessionStart": [{"matcher": "startup", "hooks": [{"type": "command", "command": "echo rt-codex"}]}]}}`)}},
			from: HarnessCodex, target: TargetCodex, wantEvent: "SessionStart", wantMatcher: "startup", wantCommand: "echo rt-codex", wantSource: "codex",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := ingestedOneHook(t, tc.src, tc.from)
			if h.Event != tc.wantEvent || h.Matcher != tc.wantMatcher || h.Command != tc.wantCommand {
				t.Fatalf("ingested hook = {event:%q matcher:%q command:%q}, want authored {%q %q %q}", h.Event, h.Matcher, h.Command, tc.wantEvent, tc.wantMatcher, tc.wantCommand)
			}
			if h.HookSource != tc.wantSource {
				t.Fatalf("HookSource = %q, want %q", h.HookSource, tc.wantSource)
			}
			if tc.wantTimeout == nil {
				if h.Timeout != nil {
					t.Fatalf("ingested timeout = %d, want absent", *h.Timeout)
				}
			} else if h.Timeout == nil || *h.Timeout != *tc.wantTimeout {
				t.Fatalf("ingested timeout = %v, want %d", h.Timeout, *tc.wantTimeout)
			}
			if tc.wantExtraKey != "" {
				if got := h.Extra[tc.wantExtraKey]; got != tc.wantExtraVal {
					t.Fatalf("ingested extra %q = %v, want %v", tc.wantExtraKey, got, tc.wantExtraVal)
				}
			}
			raw, ok := tc.src[h.Source.File]
			if !ok || raw == nil {
				t.Fatalf("fixture has no source file %q", h.Source.File)
			}
			sum := sha256.Sum256(raw.Data)
			wantDigest := "sha256:" + hex.EncodeToString(sum[:])
			if got := h.SourceDefinitionDigest(); got != wantDigest {
				t.Fatalf("SourceDefinitionDigest = %q, want %q for %s", got, wantDigest, h.Source.File)
			}
			res, err := Compile(h, tc.target)
			if err != nil {
				t.Fatalf("Compile(hook, %s): %v", tc.target, err)
			}
			if diag.HasErrors(res.Diagnostics) {
				t.Fatalf("unexpected error diagnostics on %s: %+v", tc.target, res.Diagnostics)
			}
			if len(res.Artifacts) != 1 {
				t.Fatalf("want exactly one artifact on %s, got %d (diags: %+v)", tc.target, len(res.Artifacts), res.Diagnostics)
			}
			got := decodeHookArtifact(t, tc.target, res.Artifacts[0].Bytes)
			if got.event != tc.wantEvent {
				t.Fatalf("same-harness event = %q, want %q", got.event, tc.wantEvent)
			}
			if got.matcher != tc.wantMatcher {
				t.Fatalf("same-harness matcher = %q (present %v), want %q", got.matcher, got.hasMatcher, tc.wantMatcher)
			}
			if got.command != tc.wantCommand {
				t.Fatalf("same-harness command = %q, want %q", got.command, tc.wantCommand)
			}
			if tc.wantTimeout == nil {
				if got.timeout != nil {
					t.Fatalf("same-harness timeout on %s = %d, want absent", tc.target, *got.timeout)
				}
			} else if got.timeout == nil || *got.timeout != *tc.wantTimeout {
				t.Fatalf("same-harness timeout on %s = %v, want %d", tc.target, got.timeout, *tc.wantTimeout)
			}
			// A same-harness Gemini timeout is already in milliseconds and
			// must be re-emitted verbatim with no rounding diagnostic.
			if tc.target == TargetGemini {
				for _, d := range res.Diagnostics {
					if d.Code == diag.CodeHookTimeoutRounded {
						t.Fatalf("same-harness gemini compile emitted %s; diagnostics = %+v", d.Code, res.Diagnostics)
					}
				}
			}
			if tc.wantExtraKey != "" {
				want := `"` + tc.wantExtraKey + `":"` + tc.wantExtraVal.(string) + `"`
				if !bytes.Contains(res.Artifacts[0].Bytes, []byte(want)) {
					t.Fatalf("%s artifact lost %s=%v:\n%s", tc.target, tc.wantExtraKey, tc.wantExtraVal, res.Artifacts[0].Bytes)
				}
			}
		})
	}
}

func intPtr(v int) *int { return &v }

// spec: hook-resources/devin-desktop-workspace-hooks-are-ingested-for-windsurf
func TestNativeHookIngestWindsurfOnlyEvent(t *testing.T) {
	// post_setup_worktree is documented only by Windsurf: it has no canonical
	// event, so it must survive a same-harness round trip and be suppressed
	// with the target's event-unmappable code everywhere else.
	src := fstest.MapFS{".devin/hooks.json": &fstest.MapFile{Data: []byte(
		`{"hooks": {"post_setup_worktree": [{"type": "command", "command": "echo ws-only"}]}}`)}}
	h := ingestedOneHook(t, src, HarnessWindsurf)
	if h.Event != "post_setup_worktree" || h.Command != "echo ws-only" || h.HookSource != "windsurf" {
		t.Fatalf("ingested hook = {event:%q command:%q source:%q}, want windsurf post_setup_worktree", h.Event, h.Command, h.HookSource)
	}
	got := compileHook(t, TargetWindsurf, h)
	if got.event != "post_setup_worktree" || got.hasMatcher || got.command != "echo ws-only" {
		t.Fatalf("windsurf roundtrip = {event:%q matcher:%q command:%q}, want post_setup_worktree preserved", got.event, got.matcher, got.command)
	}
	refuseHook(t, TargetClaude, h, "hook.event-unmappable")
}

// spec: hook-resources/devin-desktop-workspace-hooks-are-ingested-for-windsurf
func TestNativeHookIngestDevinAutodetection(t *testing.T) {
	windsurfOnly := fstest.MapFS{
		".devin/hooks.json": &fstest.MapFile{Data: []byte(`{"hooks": {"pre_run_command": [{"type": "command", "command": "echo ws"}]}}`)},
	}
	if got, ok := Autodetect(windsurfOnly); !ok || got != HarnessWindsurf {
		t.Fatalf("Autodetect(.devin/hooks.json only) = %q, %v; want windsurf", got, ok)
	}
	for _, marker := range []struct {
		name  string
		extra map[string]*fstest.MapFile
	}{
		{"devin hooks.v1.json", map[string]*fstest.MapFile{".devin/hooks.v1.json": {Data: []byte(`{"Stop": [{"type": "command", "command": "echo dv"}]}`)}}},
		{"devin config.json", map[string]*fstest.MapFile{".devin/config.json": {Data: []byte(`{}`)}}},
		{"devin skills dir", map[string]*fstest.MapFile{".devin/skills/x/SKILL.md": {Data: []byte("---\nname: x\n---\n")}}},
		{"devin agents dir", map[string]*fstest.MapFile{".devin/agents/a.md": {Data: []byte("---\nname: a\n---\n")}}},
	} {
		src := fstest.MapFS{}
		for k, v := range windsurfOnly {
			src[k] = v
		}
		for k, v := range marker.extra {
			src[k] = v
		}
		if got, ok := Autodetect(src); !ok || got != HarnessDevin {
			t.Fatalf("Autodetect with %s present = %q, %v; want devin", marker.name, got, ok)
		}
		// With Devin markers present, explicitly selecting Windsurf must not
		// claim .devin/hooks.json: it either errors or ingests zero hooks.
		if b, err := IngestBundle(src, HarnessWindsurf); err == nil {
			hooks := 0
			for _, r := range b.Resources {
				if _, ok := r.(*ir.Hook); ok {
					hooks++
				}
			}
			if hooks != 0 {
				t.Fatalf("with %s present windsurf ingested %d hooks from a claimed .devin dir", marker.name, hooks)
			}
		}
	}
}

// spec: hook-resources/claude-settings-hooks-are-ingested
func TestNativeHookIngestSettingsMemberIsolation(t *testing.T) {
	// Settings envelopes model a single "hooks" member: hook-shaped content
	// elsewhere in the file is unrelated configuration, never a resource.
	absent := []struct {
		name string
		from Harness
		path string
		body string
	}{
		{"claude settings", HarnessClaude, ".claude/settings.json",
			`{"permissions": {"allow": ["Bash(ls)"]}, "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "x"}]}]}`},
		{"cursor envelope", HarnessCursor, ".cursor/hooks.json",
			`{"version": 1, "preToolUse": [{"type": "command", "command": "x"}]}`},
	}
	for _, tc := range absent {
		t.Run("absent/"+tc.name, func(t *testing.T) {
			src := fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte(tc.body)}}
			b, err := IngestBundle(src, tc.from)
			if err != nil {
				t.Fatalf("IngestBundle(%s): %v", tc.from, err)
			}
			for _, r := range b.Resources {
				if _, ok := r.(*ir.Hook); ok {
					t.Fatalf("%s: an unrelated top-level member must not become a hook resource", tc.name)
				}
			}
		})
	}

	// A single case-variant spelling is a supported encoding/json alias and is
	// consumed as the modeled member; both spellings together are a duplicate.
	aliasSrc := fstest.MapFS{".claude/settings.json": &fstest.MapFile{Data: []byte(
		`{"Hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo alias"}]}]}}`)}}
	if h := ingestedOneHook(t, aliasSrc, HarnessClaude); h.Command != "echo alias" {
		t.Fatalf("alias-consumed hook command = %q, want echo alias", h.Command)
	}
	for _, body := range []string{
		`{"hooks": {"PreToolUse": []}, "Hooks": {"PostToolUse": []}}`,
		`{"Hooks": {"PostToolUse": []}, "hooks": {"PreToolUse": []}}`,
		`{"hooks": null, "Hooks": {"PreToolUse": []}}`,
	} {
		for _, tc := range []struct {
			name string
			from Harness
			path string
		}{
			{"claude settings", HarnessClaude, ".claude/settings.json"},
			{"cursor envelope", HarnessCursor, ".cursor/hooks.json"},
		} {
			src := fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte(body)}}
			_, err := IngestBundle(src, tc.from)
			coded, ok := diag.AsCodedError(err)
			if !ok || coded.Code != diag.CodeJSONDuplicateKey {
				t.Fatalf("%s %q: want coded %s, got %v", tc.name, body, diag.CodeJSONDuplicateKey, err)
			}
		}
	}

	// A present but null or non-object hooks member is malformed, not absent.
	for _, body := range []string{`{"hooks": null}`, `{"hooks": []}`, `{"hooks": "PreToolUse"}`} {
		for _, tc := range []struct {
			name string
			from Harness
			path string
		}{
			{"claude settings", HarnessClaude, ".claude/settings.json"},
			{"cursor envelope", HarnessCursor, ".cursor/hooks.json"},
		} {
			src := fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte(body)}}
			_, err := IngestBundle(src, tc.from)
			coded, ok := diag.AsCodedError(err)
			if !ok || coded.Code != diag.CodeJSONMalformed {
				t.Fatalf("%s %q: want coded %s, got %v", tc.name, body, diag.CodeJSONMalformed, err)
			}
		}
	}

	// The standalone Factory file is a pure event map: null and non-array
	// event members are malformed, never silently skipped.
	for _, body := range []string{`{"PreToolUse": null}`, `{"PreToolUse": {"matcher": "Execute"}}`, `{"PreToolUse": "x"}`} {
		src := fstest.MapFS{".factory/hooks.json": &fstest.MapFile{Data: []byte(body)}}
		if _, err := IngestBundle(src, HarnessFactory); err == nil {
			t.Fatalf("factory %q must be rejected, not silently skipped", body)
		}
	}
}

func TestNativeHookFilesRejectAliasDuplicates(t *testing.T) {
	// Nested execution fields in the new native hook files decode through the
	// same typed, alias-safe loaders as the legacy locations: a case-variant
	// duplicate of a modeled field, a literal duplicate, and a mistyped member
	// are rejected with the stable coded JSON error instead of choosing one
	// silently. Every target also carries a valid control so a missing layout
	// can never satisfy the refusal assertions.
	cases := []struct {
		name    string
		from    Harness
		path    string
		valid   string
		invalid []struct {
			body string
			code string
		}
	}{
		{"claude settings", HarnessClaude, ".claude/settings.json",
			`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo ok"}]}]}}`,
			[]struct {
				body string
				code string
			}{
				{`{"hooks": {"PreToolUse": [{"matcher": "Bash", "Matcher": "Edit", "hooks": [{"type": "command", "command": "x"}]}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"PreToolUse": [{"Matcher": "Edit", "matcher": "Bash", "hooks": [{"type": "command", "command": "x"}]}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "x", "command": "y"}]}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "x", "Command": "y"}]}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": {}}]}]}}`, diag.CodeJSONMalformed},
			}},
		{"cursor envelope", HarnessCursor, ".cursor/hooks.json",
			`{"version": 1, "hooks": {"preToolUse": [{"type": "command", "command": "echo ok"}]}}`,
			[]struct {
				body string
				code string
			}{
				{`{"version": 1, "hooks": {"preToolUse": [{"type": "command", "command": "x", "Command": "y"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"version": 1, "hooks": {"preToolUse": [{"type": "command", "Command": "y", "command": "x"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"version": 1, "hooks": {"preToolUse": [{"type": "command", "command": "x", "command": "y"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"version": 1, "hooks": {"preToolUse": [{"type": "command", "command": {}}]}}`, diag.CodeJSONMalformed},
			}},
		{"factory standalone", HarnessFactory, ".factory/hooks.json",
			`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo ok"}]}]}`,
			[]struct {
				body string
				code string
			}{
				{`{"PreToolUse": [{"matcher": "Execute", "Matcher": "Read", "hooks": [{"type": "command", "command": "x"}]}]}`, diag.CodeJSONDuplicateKey},
				{`{"PreToolUse": [{"Matcher": "Read", "matcher": "Execute", "hooks": [{"type": "command", "command": "x"}]}]}`, diag.CodeJSONDuplicateKey},
				{`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "x", "command": "y"}]}]}`, diag.CodeJSONDuplicateKey},
				{`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "x", "Command": "y"}]}]}`, diag.CodeJSONDuplicateKey},
				{`{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": {}}]}]}`, diag.CodeJSONMalformed},
			}},
		{"windsurf devin file", HarnessWindsurf, ".devin/hooks.json",
			`{"hooks": {"pre_run_command": [{"type": "command", "command": "echo ok"}]}}`,
			[]struct {
				body string
				code string
			}{
				{`{"hooks": {"pre_run_command": [{"command": "x", "Command": "y"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"pre_run_command": [{"Command": "y", "command": "x"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"pre_run_command": [{"command": "x", "command": "y"}]}}`, diag.CodeJSONDuplicateKey},
				{`{"hooks": {"pre_run_command": [{"command": {}}]}}`, diag.CodeJSONMalformed},
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte(tc.valid)}}
			if hooks := ingestedHooks(t, src, tc.from); len(hooks) != 1 {
				t.Fatalf("%s valid control must ingest one hook, got %d", tc.name, len(hooks))
			}
			for i, bad := range tc.invalid {
				src := fstest.MapFS{tc.path: &fstest.MapFile{Data: []byte(bad.body)}}
				b, err := IngestBundle(src, tc.from)
				if err == nil {
					t.Fatalf("%s invalid %d must be rejected, not ingested (resources: %d)", tc.name, i, len(b.Resources))
				}
				coded, ok := diag.AsCodedError(err)
				if !ok || coded.Code != bad.code {
					t.Fatalf("%s invalid %d: want coded %s, got %v", tc.name, i, bad.code, err)
				}
			}
		})
	}
}

func TestNativeHookIngestNameMergeNativeAndLegacy(t *testing.T) {
	// A hook in the legacy location and a hook in the native location share
	// the generated base name: each input must yield its own distinct,
	// stable resource and repeat ingestion must produce the identical set.
	cases := []struct {
		name  string
		from  Harness
		files map[string]string
	}{
		{"claude legacy+settings", HarnessClaude, map[string]string{
			".claude/hooks/hooks.json": `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo legacy-claude"}]}]}`,
			".claude/settings.json":    `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo native-claude"}]}]}}`,
		}},
		{"cursor legacy+envelope", HarnessCursor, map[string]string{
			".cursor/hooks/hooks.json": `{"PreToolUse": [{"type": "command", "command": "echo legacy-cursor", "matcher": "Bash"}]}`,
			".cursor/hooks.json":       `{"version": 1, "hooks": {"PreToolUse": [{"type": "command", "command": "echo native-cursor"}]}}`,
		}},
		{"factory legacy+standalone", HarnessFactory, map[string]string{
			".factory/hooks/hooks.json": `{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo legacy-factory"}]}]}`,
			".factory/hooks.json":       `{"PreToolUse": [{"matcher": "Execute", "hooks": [{"type": "command", "command": "echo native-factory"}]}]}`,
		}},
	}
	signature := func(hooks []*ir.Hook) []string {
		var out []string
		for _, h := range hooks {
			out = append(out, h.Name()+"\x00"+h.Command)
		}
		slices.Sort(out)
		return out
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fstest.MapFS{}
			for path, body := range tc.files {
				src[path] = &fstest.MapFile{Data: []byte(body)}
			}
			hooks := ingestedHooks(t, src, tc.from)
			if len(hooks) != 2 {
				t.Fatalf("%s must ingest one hook per file, got %d", tc.name, len(hooks))
			}
			names := map[string]bool{}
			commands := map[string]bool{}
			for _, h := range hooks {
				names[h.Name()] = true
				commands[h.Command] = true
			}
			if len(names) != 2 || len(commands) != 2 {
				t.Fatalf("%s must produce two distinct hooks, got names %v commands %v", tc.name, names, commands)
			}
			first := signature(hooks)
			again := signature(ingestedHooks(t, src, tc.from))
			if !slices.Equal(first, again) {
				t.Fatalf("%s repeat ingestion changed the hook set: %v vs %v", tc.name, first, again)
			}
		})
	}
}
