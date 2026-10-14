package codex

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile"
	compilecodex "github.com/Adyen/agent-marketplace/pkg/harness-compilation/compile/codex"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ingest"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestIngestLogsSkippedInputsWithoutSecretValues(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	src := fstest.MapFS{
		".agents/skills/benign/SKILL.md": {Data: []byte(`---
name: benign
description: Benign command
x-agent-marketplace-kind: command
argument-hint: SUPER_SECRET_ARGUMENT_HINT
---
Benign.
`)},
		".agents/skills/restricted/SKILL.md": {Data: []byte(`---
name: restricted
description: Restricted command
x-agent-marketplace-kind: command
allowed-tools: [SUPER_SECRET_TOOL]
---
SUPER_SECRET_SKILL_BODY
`)},
		".codex/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"prompt","prompt":"SUPER_SECRET_HOOK_PAYLOAD"}]}]}}`)},
		".codex/config.toml": {Data: []byte(`
[mcp_servers.private]
command = 'server'
bearer_token_env_var = 'SUPER_SECRET_BEARER_ENV'
`)},
	}
	if _, err := LoadBundle(src); err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	for _, code := range []string{
		diag.CodeSkillCodexSecurityRestrictionUnsupported,
		diag.CodeIngestCodexFieldDropped,
		diag.CodeIngestCodexHookUnsupported,
		diag.CodeIngestCodexMcpSecurityUnsupported,
	} {
		if !strings.Contains(output, code) {
			t.Errorf("logs missing skip code %q:\n%s", code, output)
		}
	}
	for _, secret := range []string{"SUPER_SECRET_ARGUMENT_HINT", "SUPER_SECRET_TOOL", "SUPER_SECRET_SKILL_BODY", "SUPER_SECRET_HOOK_PAYLOAD", "SUPER_SECRET_BEARER_ENV"} {
		if strings.Contains(output, secret) {
			t.Errorf("logs leaked %q:\n%s", secret, output)
		}
	}
}

func TestDetectRequiresCodexMarkerAndLoadsSiblingSkills(t *testing.T) {
	sharedOnly := fstest.MapFS{".agents/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")}}
	if Detect(sharedOnly) {
		t.Fatal("shared .agents/skills selected Codex without .codex marker")
	}
	explicit, err := LoadBundle(sharedOnly)
	if err != nil || len(explicit.Resources) != 1 || explicit.Resources[0].Name() != "demo" {
		t.Fatalf("explicit LoadBundle = %#v, %v; want sibling skill without autodetection marker", explicit, err)
	}
	src := fstest.MapFS{
		".codex/agents/reviewer.toml":    {Data: []byte("name = \"reviewer\"\ndescription = \"Review\"\nmodel = \"gpt-5.6-terra\"\nmodel_reasoning_effort = \"high\"\ndeveloper_instructions = \"Be exact.\"\n")},
		".agents/skills/demo/SKILL.md":   {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")},
		".agents/skills/deploy/SKILL.md": {Data: []byte("---\nname: deploy\ndescription: Deploy\nx-agent-marketplace-kind: command\n---\nDeploy.\n")},
	}
	if harness, ok := ingest.Autodetect(src); !ok || harness != ingest.HarnessCodex {
		t.Fatalf("Autodetect = %q, %v", harness, ok)
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 3 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	want := []ir.ResourceKind{ir.KindSkill, ir.KindCommand, ir.KindSubagent}
	for i, resource := range bundle.Resources {
		if resource.Kind() != want[i] {
			t.Errorf("resource %d kind = %s, want %s", i, resource.Kind(), want[i])
		}
	}
}

func TestMarkedCommandsWithAuthorityControlsFailClosed(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: nil},
		".agents/skills/safe/SKILL.md": {Data: []byte(`---
name: safe
description: Safe command
x-agent-marketplace-kind: command
---
Safe.
`)},
		".agents/skills/tools/SKILL.md": {Data: []byte(`---
name: tools
description: Restricted command
x-agent-marketplace-kind: command
allowed-tools:
  - Read
---
Restricted.
`)},
		".agents/skills/permissions/SKILL.md": {Data: []byte(`---
name: permissions
description: Restricted command
x-agent-marketplace-kind: command
permissions:
  deny:
    - Bash
---
Restricted.
`)},
		".agents/skills/agent/SKILL.md": {Data: []byte(`---
name: agent
description: Delegated command
x-agent-marketplace-kind: command
agent: reviewer
---
Delegated.
`)},
		".agents/skills/subagent/SKILL.md": {Data: []byte(`---
name: subagent
description: Delegated command
x-agent-marketplace-kind: command
subagent: reviewer
---
Delegated.
`)},
		".agents/skills/triggers/SKILL.md": {Data: []byte(`---
name: triggers
description: Activated command
x-agent-marketplace-kind: command
triggers: [user]
---
Activated.
`)},
		".agents/skills/empty-triggers/SKILL.md": {Data: []byte(`---
name: empty-triggers
description: Explicitly deactivated command
x-agent-marketplace-kind: command
triggers: []
---
Deactivated.
`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "safe" || bundle.Resources[0].Kind() != ir.KindCommand {
		t.Fatalf("resources = %#v, want only safe marked command", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeSkillCodexSecurityRestrictionUnsupported) {
		t.Fatalf("diagnostics = %#v, want marked-command security error", bundle.Diagnostics)
	}
}

func TestMarkedCommandsReportBenignFieldLoss(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: nil},
		".agents/skills/argument/SKILL.md": {Data: []byte(`---
name: argument
description: Argument command
x-agent-marketplace-kind: command
argument-hint: <path>
---
Argument.
`)},
		".agents/skills/model/SKILL.md": {Data: []byte(`---
name: model
description: Model command
x-agent-marketplace-kind: command
model: gpt-5.6-terra
---
Model.
`)},
		".agents/skills/support/SKILL.md": {Data: []byte(`---
name: support
description: Support command
x-agent-marketplace-kind: command
---
Support.
`)},
		".agents/skills/support/scripts/run.sh": {Data: []byte("#!/bin/sh\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 3 {
		t.Fatalf("resources = %#v, want three marked commands", bundle.Resources)
	}
	for _, resource := range bundle.Resources {
		if resource.Kind() != ir.KindCommand {
			t.Fatalf("resource %q kind = %s, want command", resource.Name(), resource.Kind())
		}
	}
	var fieldLosses int
	for _, diagnostic := range bundle.Diagnostics {
		if diagnostic.Code == diag.CodeIngestCodexFieldDropped {
			fieldLosses++
			if diagnostic.Severity != diag.SeverityWarning {
				t.Fatalf("field-loss diagnostic = %#v, want warning", diagnostic)
			}
		}
	}
	if fieldLosses != 3 {
		t.Fatalf("diagnostics = %#v, want one benign field-loss warning per command", bundle.Diagnostics)
	}
}

func TestNativeSecurityFieldsRoundTrip(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/reviewer.toml": {Data: []byte("name = 'reviewer'\ndescription = 'Review'\ndeveloper_instructions = 'Review.'\nsandbox_mode = 'read-only'\n")},
		".codex/config.toml":          {Data: []byte("[mcp_servers.private]\nurl = 'https://example.test/mcp'\nbearer_token_env_var = 'TOKEN'\n[mcp_servers.private.env_http_headers]\nX_API_KEY = 'API_KEY'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 2 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	for _, resource := range bundle.Resources {
		artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(resource)
		if err != nil {
			t.Fatal(err)
		}
		if diag.HasErrors(diagnostics) || len(artifacts) != 1 {
			t.Fatalf("round trip %s = %#v, %#v", resource.Name(), artifacts, diagnostics)
		}
		text := string(artifacts[0].Bytes)
		if resource.Kind() == ir.KindSubagent && !strings.Contains(text, "sandbox_mode = 'read-only'") {
			t.Errorf("sandbox mode lost:\n%s", text)
		}
		if resource.Kind() == ir.KindMcpServer && (!strings.Contains(text, "bearer_token_env_var = 'TOKEN'") || !strings.Contains(text, "env_http_headers")) {
			t.Errorf("MCP authentication lost:\n%s", text)
		}
	}
}

func TestSubagentSandboxModeClosedEnum(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			src := fstest.MapFS{
				".codex/agents/reviewer.toml": {Data: []byte("name = 'reviewer'\ndescription = 'Review'\ndeveloper_instructions = 'Review.'\nsandbox_mode = '" + mode + "'\n")},
			}
			bundle, err := LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 || diag.HasErrors(bundle.Diagnostics) {
				t.Fatalf("bundle = %#v, want one valid subagent", bundle)
			}
			artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(bundle.Resources[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(artifacts) != 1 || diag.HasErrors(diagnostics) || !strings.Contains(string(artifacts[0].Bytes), "sandbox_mode = '"+mode+"'") {
				t.Fatalf("round trip = %#v, %#v, want sandbox mode %q", artifacts, diagnostics, mode)
			}
		})
	}

	t.Run("invalid", func(t *testing.T) {
		src := fstest.MapFS{
			".codex/agents/reviewer.toml": {Data: []byte("name = 'reviewer'\ndescription = 'Review'\ndeveloper_instructions = 'Review.'\nsandbox_mode = 'host-root'\n")},
		}
		bundle, err := LoadBundle(src)
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle.Resources) != 0 || !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexSubagentSecurityUnsupported) {
			t.Fatalf("bundle = %#v, want invalid sandbox mode suppressed with security diagnostic", bundle)
		}
	})
}

func TestLoadHooksMCPAndDeprecatedPrompts(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json":        {Data: []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./check.sh"}]}]}}`)},
		".codex/config.toml":       {Data: []byte("[mcp_servers.docs]\ncommand = \"npx\"\nargs = [\"-y\", \"docs-mcp\"]\n[mcp_servers.docs.env]\nTOKEN = \"${TOKEN}\"\n")},
		".codex/prompts/legacy.md": {Data: []byte("Legacy command.\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []ir.ResourceKind{ir.KindHook, ir.KindMcpServer, ir.KindCommand}
	if len(bundle.Resources) != len(want) {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	for i, resource := range bundle.Resources {
		if resource.Kind() != want[i] {
			t.Errorf("resource %d kind = %s, want %s", i, resource.Kind(), want[i])
		}
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexPromptDeprecated) {
		t.Fatalf("diagnostics = %#v, want stable deprecated-prompt diagnostic", bundle.Diagnostics)
	}
	var legacy ir.Resource
	for _, resource := range bundle.Resources {
		if resource.Name() == "legacy" {
			legacy = resource
			break
		}
	}
	if legacy == nil || legacy.Kind() != ir.KindCommand {
		t.Fatalf("legacy resource = %#v, want command", legacy)
	}
	artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(diagnostics) || len(artifacts) != 1 || artifacts[0].Path != ".agents/skills/legacy/SKILL.md" {
		t.Fatalf("legacy round trip = %#v, %#v", artifacts, diagnostics)
	}
	for _, artifact := range artifacts {
		if strings.Contains(artifact.Path, ".codex/prompts") {
			t.Fatalf("deprecated prompt re-emitted at %q", artifact.Path)
		}
	}
}

func TestMalformedResourceDoesNotDiscardIndependentResources(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/bad.toml":       {Data: []byte("name = [")},
		".codex/agents/good.toml":      {Data: []byte("name = 'good'\ndescription = 'Good'\ndeveloper_instructions = 'Good.'\n")},
		".agents/skills/demo/SKILL.md": {Data: []byte("---\nname: demo\ndescription: Demo\n---\nBody\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 2 {
		t.Fatalf("resources = %#v, want valid skill and subagent", bundle.Resources)
	}
	if !diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("diagnostics = %#v, want malformed-resource error", bundle.Diagnostics)
	}
}

func TestDuplicateIdentityIsRejectedWithoutChoosingWinner(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/a.toml": {Data: []byte("name = 'same'\ndescription = 'A'\ndeveloper_instructions = 'A'\n")},
		".codex/agents/b.toml": {Data: []byte("name = 'same'\ndescription = 'B'\ndeveloper_instructions = 'B'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 0 {
		t.Fatalf("resources = %#v, want both duplicates suppressed", bundle.Resources)
	}
	if !diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

// spec: hook-resources/unsupported-codex-handlers-are-skipped
func TestUnsupportedHooksAreSuppressedDuringIngestion(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./valid"},{"type":"prompt","command":"ignored"},{"type":"mcp_tool","server":"future","tool":"unreleased"}]}],"Notification":[{"hooks":[{"type":"command","command":"./inert"}]}]}}`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	// Unsupported handler types are still skipped at ingestion; the
	// undeclared Notification event is preserved verbatim with a warning
	// instead of being dropped, and the resolver refuses it cross-harness.
	if len(bundle.Resources) != 2 {
		t.Fatalf("resources = %#v, want the command hook plus the preserved Notification hook", bundle.Resources)
	}
	for _, r := range bundle.Resources {
		if r.Kind() != ir.KindHook {
			t.Fatalf("resource = %#v, want only hooks", r)
		}
	}
	for _, code := range []string{diag.CodeIngestCodexHookUnsupported, diag.CodeIngestCodexHookEventUnmappable} {
		if !hasDiagnosticCode(bundle.Diagnostics, code) {
			t.Fatalf("diagnostics = %#v, want %q", bundle.Diagnostics, code)
		}
	}
}

func TestHookDescriptionMetadataDoesNotSuppressHooks(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{
  "description": "Project hook policy",
  "hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "./check"}]}]}
}`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].(*ir.Hook).Command != "./check" {
		t.Fatalf("resources = %#v, want hook retained after dropping description", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexFieldDropped) {
		t.Fatalf("diagnostics = %#v, want description-drop warning", bundle.Diagnostics)
	}
}

func TestHookFieldInventoryPreservesTimeoutAndIsolatesBadHandlers(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "./timed",
            "timeout": 17,
            "statusMessage": "Checking",
            "additionalContextLimit": 1024,
            "command_windows": "check.cmd"
          },
          {"type": "command", "command": "./async", "async": "yes"},
          {"type": "command", "command": "./unknown", "approvalPolicy": "never"},
          {"type": "command", "command": 42}
        ]
      },
      {"matcher": 42, "hooks": [{"type": "command", "command": "./bad-entry"}]}
    ]
  }
}`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %#v, want only valid timed handler", bundle.Resources)
	}
	hook, ok := bundle.Resources[0].(*ir.Hook)
	if !ok || hook.Command != "./timed" || hook.Timeout == nil || *hook.Timeout != 17 {
		t.Fatalf("hook = %#v, want timeout-preserving handler", bundle.Resources[0])
	}
	artifacts, compileDiagnostics, err := (compilecodex.Compiler{}).Translate(hook)
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(compileDiagnostics) || len(artifacts) != 1 {
		t.Fatalf("hook round trip = %#v, %#v", artifacts, compileDiagnostics)
	}
	output := string(artifacts[0].Bytes)
	for _, want := range []string{`"timeout":17`, `"commandWindows":"check.cmd"`, `"additionalContextLimit":1024`, `"statusMessage":"Checking"`} {
		if !strings.Contains(output, want) {
			t.Errorf("hook round trip missing %s: %s", want, output)
		}
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexHookUnsupported) {
		t.Fatalf("diagnostics = %#v, want handler errors", bundle.Diagnostics)
	}
}

func TestHookAsyncRoundTrip(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "async"}[async], func(t *testing.T) {
			src := fstest.MapFS{
				".codex/hooks.json": {Data: []byte(fmt.Sprintf(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./check","async":%t}]}]}}`, async))},
			}
			bundle, err := LoadBundle(src)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 || diag.HasErrors(bundle.Diagnostics) {
				t.Fatalf("bundle = %#v, want native hook retained", bundle)
			}
			hook := bundle.Resources[0].(*ir.Hook)
			if hook.Ext.Codex == nil || hook.Ext.Codex.HookAsync == nil || *hook.Ext.Codex.HookAsync != async {
				t.Fatalf("native async provenance = %#v, want %t", hook.Ext.Codex, async)
			}
			result, err := compile.Compile(hook, compile.TargetCodex)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Artifacts) != 1 || diag.HasErrors(result.Diagnostics) || !strings.Contains(string(result.Artifacts[0].Bytes), fmt.Sprintf(`"async":%t`, async)) {
				t.Fatalf("round trip = %#v, want async=%t", result, async)
			}
		})
	}
}

func TestSessionEndAsyncIsNormalizedToSynchronous(t *testing.T) {
	bundle, err := LoadBundle(fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"./cleanup","async":true}]}]}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ingestion keeps the authored async value; the Codex compiler still
	// normalizes SessionEnd to synchronous at emission.
	if len(bundle.Resources) != 1 || diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("bundle = %#v, want the preserved hook", bundle)
	}
	hook := bundle.Resources[0].(*ir.Hook)
	if hook.Ext.Codex.HookAsync == nil || !*hook.Ext.Codex.HookAsync {
		t.Fatalf("native async provenance = %#v, want preserved true", hook.Ext.Codex.HookAsync)
	}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || strings.Contains(string(result.Artifacts[0].Bytes), `"async"`) {
		t.Fatalf("round trip = %#v, want synchronous SessionEnd output", result)
	}
}

func TestSessionEndTimeoutIsCapped(t *testing.T) {
	bundle, err := LoadBundle(fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"./cleanup","timeout":30}]}]}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Ingestion keeps the authored timeout; the documented 3-second bound is
	// applied by the resolver at compile time.
	if len(bundle.Resources) != 1 || diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("bundle = %#v, want the preserved hook", bundle)
	}
	hook := bundle.Resources[0].(*ir.Hook)
	if hook.Timeout == nil || *hook.Timeout != 30 {
		t.Fatalf("timeout = %#v, want the authored 30", hook.Timeout)
	}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), `"timeout":3`) {
		t.Fatalf("round trip = %#v, want capped SessionEnd timeout", result)
	}
}

func TestHookOptionalNullFieldsAreOmitted(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./check","timeout":null,"command_windows":null,"additionalContextLimit":null,"statusMessage":null}]}]}}`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || diag.HasErrors(bundle.Diagnostics) {
		t.Fatalf("bundle = %#v, want valid hook with omitted optional fields", bundle)
	}
	hook := bundle.Resources[0].(*ir.Hook)
	if hook.Timeout != nil || hook.Ext.Codex.CommandWindows != nil || hook.Ext.Codex.AdditionalContextLimit != nil || hook.Ext.Codex.HookStatusMessage != nil {
		t.Fatalf("hook = %#v, want null fields normalized to absent", hook)
	}
	result, err := compile.Compile(hook, compile.TargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"timeout", "commandWindows", "additionalContextLimit", "statusMessage"} {
		if strings.Contains(string(result.Artifacts[0].Bytes), `"`+field+`"`) {
			t.Fatalf("round trip emitted null field %q: %s", field, result.Artifacts[0].Bytes)
		}
	}
}

func TestHookUnknownContainersAndUnsupportedMatchersFailClosed(t *testing.T) {
	t.Run("unknown top-level field suppresses file", func(t *testing.T) {
		src := fstest.MapFS{
			".codex/hooks.json": {Data: []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./check"}]}]},"approvalPolicy":"never"}`)},
		}
		bundle, err := LoadBundle(src)
		if err != nil {
			t.Fatal(err)
		}
		if len(bundle.Resources) != 0 || !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexHookUnsupported) {
			t.Fatalf("resources/diagnostics = %#v / %#v", bundle.Resources, bundle.Diagnostics)
		}
	})

	t.Run("bad entry and ignored matchers do not discard sibling", func(t *testing.T) {
		src := fstest.MapFS{
			".codex/hooks.json": {Data: []byte(`{"hooks":{
  "PreToolUse":[
    {"policy":"strict","hooks":[{"type":"command","command":"./bad-entry"}]},
    {"matcher":"Read","hooks":[{"type":"command","command":"./safe"}]}
  ],
  "UserPromptSubmit":[{"matcher":"ignored","hooks":[{"type":"command","command":"./prompt"}]}],
  "Stop":[{"matcher":"ignored","hooks":[{"type":"command","command":"./stop"}]}]
}}`)},
		}
		bundle, err := LoadBundle(src)
		if err != nil {
			t.Fatal(err)
		}
		// The malformed entry is skipped; matchers on events are preserved as
		// authored — the resolver, not the ingester, rejects a matcher on an
		// event that declares none.
		if len(bundle.Resources) != 3 {
			t.Fatalf("resources = %#v, want the three well-formed hooks", bundle.Resources)
		}
		if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexHookUnsupported) {
			t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
		}
	})
}

func TestHookDuplicateKeysFailClosedWithStableDiagnostic(t *testing.T) {
	tests := map[string]string{
		"document": `{
  "hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "./first"}]}]},
  "hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "./second"}]}]}
}`,
		"event": `{"hooks": {
  "PreToolUse": [{"hooks": [{"type": "command", "command": "./first"}]}],
  "PreToolUse": [{"hooks": [{"type": "command", "command": "./second"}]}]
}}`,
		"entry": `{"hooks": {"PreToolUse": [{
  "matcher": "Read",
  "matcher": "Bash",
  "hooks": [{"type": "command", "command": "./check"}]
}]}}`,
		"handler": `{"hooks": {"PreToolUse": [{"hooks": [{
  "type": "command",
  "command": "./first",
  "command": "./second"
}]}]}}`,
	}

	for name, hooks := range tests {
		t.Run(name, func(t *testing.T) {
			bundle, err := LoadBundle(fstest.MapFS{
				".codex/hooks.json": {Data: []byte(hooks)},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 0 {
				t.Fatalf("resources = %#v, want duplicate-key hook file rejected", bundle.Resources)
			}
			if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeJSONDuplicateKey) {
				t.Fatalf("diagnostics = %#v, want %q", bundle.Diagnostics, diag.CodeJSONDuplicateKey)
			}
		})
	}
}

func TestSecuritySensitiveMCPFieldsSuppressAffectedServer(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte("[mcp_servers.safe]\ncommand = 'safe'\n[mcp_servers.restricted]\ncommand = 'restricted'\nenabled_tools = ['read']\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "safe" {
		t.Fatalf("resources = %#v, want only safe server", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, "ingest.codex.mcp-security-unsupported") {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

func TestMCPTransportValidationIsPerServer(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte(`
[mcp_servers.safe]
command = 'safe'

[mcp_servers.both]
command = 'both'
url = 'https://example.test/both'

[mcp_servers.neither]
enabled = true

[mcp_servers.stdio-auth]
command = 'stdio'
bearer_token_env_var = 'TOKEN'

[mcp_servers.remote-args]
url = 'https://example.test/remote'
args = ['--invalid']

[mcp_servers.remote-cwd]
url = 'https://example.test/cwd'
cwd = '/srv'

[mcp_servers.remote-env-vars]
url = 'https://example.test/env'
[mcp_servers.remote-env-vars.env_vars]
source = 'remote'
`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "safe" {
		t.Fatalf("resources = %#v, want only valid stdio server", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexMcpSecurityUnsupported) {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

func TestMalformedMCPServerDoesNotDiscardValidSibling(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte(`
[mcp_servers.bad]
command = ['not', 'a', 'string']

[mcp_servers.bad-env-vars]
command = 'bad-env'
[mcp_servers.bad-env-vars.env_vars]
source = 'remote'

[mcp_servers.good]
command = 'good'
`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "good" {
		t.Fatalf("resources = %#v, want valid sibling", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexLoadResourceFailed) {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

func TestMCPEnvVarsRoundTrip(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte("[mcp_servers.demo]\ncommand = 'demo'\nenv_vars = ['TOKEN']\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(bundle.Resources[0])
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(diagnostics) || len(artifacts) != 1 || !strings.Contains(string(artifacts[0].Bytes), "env_vars = ['TOKEN']") {
		t.Fatalf("round trip = %#v, %#v", artifacts, diagnostics)
	}
}

func TestMCPWorkingDirectoryRoundTrip(t *testing.T) {
	const cwd = "${CLAUDE_PLUGIN_ROOT}/demo"
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte("[mcp_servers.demo]\ncommand = 'demo'\ncwd = '" + cwd + "'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	server := bundle.Resources[0].(*ir.McpServer)
	if server.Extra["cwd"] != cwd || server.Ext.Codex == nil || server.Ext.Codex.MCP == nil || server.Ext.Codex.MCP.CWD != cwd {
		t.Fatalf("working directory provenance lost: extra=%#v sidecar=%#v", server.Extra, server.Ext.Codex)
	}
	result, err := compile.Compile(server, compile.TargetCodex, compile.WithPluginRoot("/must-not-expand"))
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(result.Diagnostics) || len(result.Artifacts) != 1 || !strings.Contains(string(result.Artifacts[0].Bytes), "cwd = '"+cwd+"'") || strings.Contains(string(result.Artifacts[0].Bytes), "/must-not-expand") {
		t.Fatalf("round trip = %#v", result)
	}
}

func TestMCPBenignTimingFieldsWarnWithoutSuppressingServer(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte("[mcp_servers.demo]\ncommand = 'demo'\nstartup_timeout_ms = 1000\nstartup_timeout_sec = 2\ntool_timeout_sec = 30\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "demo" {
		t.Fatalf("resources = %#v, want timing-only server retained", bundle.Resources)
	}
	if diag.HasErrors(bundle.Diagnostics) || !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexFieldDropped) {
		t.Fatalf("diagnostics = %#v, want benign field-loss warning", bundle.Diagnostics)
	}
}

func TestCodexMCPFieldLossClassifiesDocumentedFields(t *testing.T) {
	tests := []struct {
		name           string
		field          string
		wantSecurity   bool
		wantBenignLoss bool
	}{
		{name: "supported", field: "command"},
		{name: "display name", field: "name", wantBenignLoss: true},
		{name: "required startup", field: "required", wantBenignLoss: true},
		{name: "parallel calls", field: "supports_parallel_tool_calls", wantBenignLoss: true},
		{name: "timeout", field: "startup_timeout_ms", wantBenignLoss: true},
		{name: "security policy", field: "enabled_tools", wantSecurity: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			security, benign := codexMCPFieldLoss(map[string]any{tt.field: true})
			if security != tt.wantSecurity || benign != tt.wantBenignLoss {
				t.Fatalf("codexMCPFieldLoss(%q) = (%v, %v), want (%v, %v)", tt.field, security, benign, tt.wantSecurity, tt.wantBenignLoss)
			}
		})
	}
}

func TestMCPMixedEnvVarSourcesRoundTripAndInvalidSourceIsolation(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte(`
[mcp_servers.bad]
command = 'bad'
env_vars = [{ name = 'BAD', source = 'elsewhere' }]

[mcp_servers.mixed]
command = 'mixed'
experimental_environment = 'remote'
env_vars = ['LOCAL', { name = 'LOCAL_OBJECT', source = 'local' }, { name = 'REMOTE', source = 'remote' }]

[mcp_servers.sibling]
command = 'sibling'
`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 2 || bundle.Resources[0].Name() != "mixed" || bundle.Resources[1].Name() != "sibling" {
		t.Fatalf("resources = %#v, want mixed and sibling only", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexMcpSecurityUnsupported) {
		t.Fatalf("diagnostics = %#v, want invalid-source security diagnostic", bundle.Diagnostics)
	}
	mixed := bundle.Resources[0].(*ir.McpServer)
	if mixed.Env["LOCAL"] != "${LOCAL}" || mixed.Env["LOCAL_OBJECT"] != "${LOCAL_OBJECT}" {
		t.Fatalf("canonical env = %#v, want local forwarding entries", mixed.Env)
	}
	if _, ok := mixed.Env["REMOTE"]; ok {
		t.Fatalf("canonical env = %#v, remote source must remain Codex-native", mixed.Env)
	}
	if mixed.Ext.Codex == nil || mixed.Ext.Codex.MCP == nil || len(mixed.Ext.Codex.MCP.RemoteEnvVars) != 1 || mixed.Ext.Codex.MCP.RemoteEnvVars[0] != "REMOTE" {
		t.Fatalf("Codex sidecar = %#v, want remote environment provenance", mixed.Ext.Codex)
	}
	if mixed.Ext.Codex.MCP.ExperimentalEnvironment != "remote" {
		t.Fatalf("Codex sidecar = %#v, want remote execution provenance", mixed.Ext.Codex)
	}

	artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(mixed)
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(diagnostics) || len(artifacts) != 1 {
		t.Fatalf("round trip = %#v, %#v", artifacts, diagnostics)
	}
	output := string(artifacts[0].Bytes)
	for _, want := range []string{"experimental_environment = 'remote'", "'LOCAL'", "'LOCAL_OBJECT'", "name = 'REMOTE'", "source = 'remote'"} {
		if !strings.Contains(output, want) {
			t.Errorf("round trip missing %q:\n%s", want, output)
		}
	}
	roundTrip, err := LoadBundle(fstest.MapFS{".codex/config.toml": {Data: artifacts[0].Bytes}})
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Resources) != 1 {
		t.Fatalf("re-ingested resources = %#v", roundTrip.Resources)
	}
	roundTripMCP := roundTrip.Resources[0].(*ir.McpServer).Ext.Codex.MCP
	if len(roundTripMCP.RemoteEnvVars) != 1 || roundTripMCP.RemoteEnvVars[0] != "REMOTE" {
		t.Fatalf("re-ingested sidecar = %#v, want remote source preserved", roundTripMCP)
	}
}

func TestMCPRemoteEnvVarsRequireRemoteExecution(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte(`
[mcp_servers.missing]
command = 'missing'
env_vars = [{ name = 'REMOTE', source = 'remote' }]

[mcp_servers.unsupported]
command = 'unsupported'
experimental_environment = 'elsewhere'

[mcp_servers.local]
command = 'local'
experimental_environment = 'local'

[mcp_servers.http-local]
url = 'https://example.test/mcp'
experimental_environment = 'local'

[mcp_servers.remote]
command = 'remote'
experimental_environment = 'remote'
env_vars = [{ name = 'REMOTE', source = 'remote' }]
`)},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 2 || bundle.Resources[0].Name() != "local" || bundle.Resources[1].Name() != "remote" {
		t.Fatalf("resources = %#v, want valid local- and remote-execution servers", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexMcpSecurityUnsupported) {
		t.Fatalf("diagnostics = %#v, want remote-execution security diagnostic", bundle.Diagnostics)
	}
	local := bundle.Resources[0].(*ir.McpServer)
	if local.Ext.Codex == nil || local.Ext.Codex.MCP == nil || local.Ext.Codex.MCP.ExperimentalEnvironment != "local" {
		t.Fatalf("Codex sidecar = %#v, want local execution provenance", local.Ext.Codex)
	}
	server := bundle.Resources[1].(*ir.McpServer)
	if server.Ext.Codex == nil || server.Ext.Codex.MCP == nil || server.Ext.Codex.MCP.ExperimentalEnvironment != "remote" {
		t.Fatalf("Codex sidecar = %#v, want remote execution provenance", server.Ext.Codex)
	}
}

func TestHooksNullShapesFailClosedWithoutDiscardingValidSiblings(t *testing.T) {
	tests := []struct {
		name      string
		document  string
		wantValid bool
	}{
		{name: "document hooks", document: `{"hooks":null}`},
		{name: "event", document: `{"hooks":{"PreToolUse":null,"Stop":[{"hooks":[{"type":"command","command":"valid"}]}]}}`, wantValid: true},
		{name: "matcher", document: `{"hooks":{"PreToolUse":[{"matcher":null,"hooks":[{"type":"command","command":"invalid"}]},{"matcher":"Read","hooks":[{"type":"command","command":"valid"}]}]}}`, wantValid: true},
		{name: "nested hooks", document: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":null},{"matcher":"Read","hooks":[{"type":"command","command":"valid"}]}]}}`, wantValid: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle, err := LoadBundle(fstest.MapFS{".codex/hooks.json": {Data: []byte(tt.document)}})
			if err != nil {
				t.Fatal(err)
			}
			if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexHookUnsupported) {
				t.Fatalf("diagnostics = %#v, want null-shape hook diagnostic", bundle.Diagnostics)
			}
			if tt.wantValid && len(bundle.Resources) != 1 {
				t.Fatalf("resources = %#v, want valid sibling preserved", bundle.Resources)
			}
			if !tt.wantValid && len(bundle.Resources) != 0 {
				t.Fatalf("resources = %#v, want malformed document skipped", bundle.Resources)
			}
		})
	}
}

func TestMCPRemoteOAuthResourceRoundTrip(t *testing.T) {
	src := fstest.MapFS{
		".codex/config.toml": {Data: []byte("[mcp_servers.private]\nurl = 'https://example.test/mcp'\noauth_resource = 'https://example.test/resource'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 {
		t.Fatalf("resources = %#v, want remote MCP server", bundle.Resources)
	}
	server := bundle.Resources[0].(*ir.McpServer)
	if server.OAuthResource != "https://example.test/resource" {
		t.Fatalf("OAuthResource = %q", server.OAuthResource)
	}
	artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(server)
	if err != nil {
		t.Fatal(err)
	}
	if diag.HasErrors(diagnostics) || len(artifacts) != 1 || !strings.Contains(string(artifacts[0].Bytes), "oauth_resource = 'https://example.test/resource'") {
		t.Fatalf("round trip = %#v, %#v", artifacts, diagnostics)
	}
}

func TestSubagentUnknownFieldsFailClosedByAuthority(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/benign.toml":     {Data: []byte("name = 'benign'\ndescription = 'Benign'\ndeveloper_instructions = 'Review.'\nmodel_verbosity = 'low'\nnickname_candidates = ['reviewer']\n")},
		".codex/agents/restricted.toml": {Data: []byte("name = 'restricted'\ndescription = 'Restricted'\ndeveloper_instructions = 'Review.'\napproval_policy = 'never'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 1 || bundle.Resources[0].Name() != "benign" {
		t.Fatalf("resources = %#v, want only benign subagent", bundle.Resources)
	}
	if !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexFieldDropped) || !hasDiagnosticCode(bundle.Diagnostics, "ingest.codex.subagent-security-unsupported") {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

func TestNativeCodexEffortRoundTripsWithProvenance(t *testing.T) {
	for _, effort := range []string{"minimal", "xhigh", "max", "ultra", "none", "future-effort"} {
		t.Run(effort, func(t *testing.T) {
			bundle, err := LoadBundle(fstest.MapFS{
				".codex/agents/reviewer.toml": {Data: []byte("name = 'reviewer'\ndescription = 'Review'\ndeveloper_instructions = 'Review.'\nmodel_reasoning_effort = '" + effort + "'\n")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Resources) != 1 {
				t.Fatalf("resources = %#v, want one subagent", bundle.Resources)
			}
			subagent := bundle.Resources[0].(*ir.Subagent)
			if subagent.Effort != effort || subagent.ModelSource != "codex" || subagent.Ext.Codex != nil {
				t.Fatalf("native effort/provenance lost or duplicated: %+v", subagent)
			}

			artifacts, diagnostics, err := (compilecodex.Compiler{}).Translate(subagent)
			if err != nil {
				t.Fatal(err)
			}
			wantTOML := "model_reasoning_effort = '" + effort + "'"
			if diag.HasErrors(diagnostics) || len(artifacts) != 1 || !strings.Contains(string(artifacts[0].Bytes), wantTOML) {
				t.Fatalf("round trip = %#v, %#v, want %q", artifacts, diagnostics, wantTOML)
			}
			if hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexFieldDropped) || len(diagnostics) != 0 {
				t.Fatalf("lossless native effort produced a loss diagnostic: %+v / %+v", bundle.Diagnostics, diagnostics)
			}
		})
	}
}

func TestSubagentRequiredFieldsAndEffortValidationArePerFile(t *testing.T) {
	src := fstest.MapFS{
		".codex/agents/high.toml":                 {Data: []byte("name = 'high'\ndescription = 'High'\ndeveloper_instructions = 'Review.'\nmodel_reasoning_effort = 'high'\n")},
		".codex/agents/native.toml":               {Data: []byte("name = 'native'\ndescription = 'Native'\ndeveloper_instructions = 'Review.'\nmodel_reasoning_effort = 'xhigh'\n")},
		".codex/agents/unknown.toml":              {Data: []byte("name = 'unknown'\ndescription = 'Unknown'\ndeveloper_instructions = 'Review.'\nmodel_reasoning_effort = 'future-effort'\n")},
		".codex/agents/missing-name.toml":         {Data: []byte("description = 'Missing'\ndeveloper_instructions = 'Review.'\n")},
		".codex/agents/missing-description.toml":  {Data: []byte("name = 'missing-description'\ndeveloper_instructions = 'Review.'\n")},
		".codex/agents/missing-instructions.toml": {Data: []byte("name = 'missing-instructions'\ndescription = 'Missing'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 3 {
		t.Fatalf("resources = %#v, want three complete subagents", bundle.Resources)
	}
	efforts := make(map[string]string, len(bundle.Resources))
	for _, resource := range bundle.Resources {
		efforts[resource.Name()] = resource.(*ir.Subagent).Effort
	}
	if efforts["high"] != "high" || efforts["native"] != "xhigh" || efforts["unknown"] != "future-effort" {
		t.Fatalf("native efforts changed: %#v", efforts)
	}
	for _, resource := range bundle.Resources {
		if resource.(*ir.Subagent).ModelSource != "codex" {
			t.Fatalf("resource %q lost native reasoning provenance", resource.Name())
		}
	}
	if hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexFieldDropped) || !hasDiagnosticCode(bundle.Diagnostics, diag.CodeIngestCodexLoadResourceFailed) {
		t.Fatalf("diagnostics = %#v", bundle.Diagnostics)
	}
}

func TestNativeLiteralTokensRoundTripWithoutInterpolation(t *testing.T) {
	src := fstest.MapFS{
		".codex/hooks.json":  {Data: []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/native"}]}]}}`)},
		".codex/config.toml": {Data: []byte("[mcp_servers.native-stdio]\ncommand = '${env:COMMAND}'\nargs = ['${file:/native/arg}']\nenv_vars = ['FORWARDED']\n[mcp_servers.native-stdio.env]\nTOKEN = '${TOKEN}'\n[mcp_servers.native-remote]\nurl = 'https://example.test/mcp'\n[mcp_servers.native-remote.http_headers]\nAuthorization = '${env:TOKEN}'\n[mcp_servers.native-remote.env_http_headers]\nX-Token = 'HEADER_TOKEN'\n")},
	}
	bundle, err := LoadBundle(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Resources) != 3 {
		t.Fatalf("resources = %#v", bundle.Resources)
	}
	for _, resource := range bundle.Resources {
		var opts compile.Options
		compile.WithPluginRoot("/plugins/demo")(&opts)
		artifacts, diagnostics, err := (compilecodex.Compiler{}).TranslateWithOptions(resource, opts)
		if err != nil {
			t.Fatal(err)
		}
		if diag.HasErrors(diagnostics) || len(artifacts) != 1 {
			t.Fatalf("round trip %s = %#v, %#v", resource.Name(), artifacts, diagnostics)
		}
		text := string(artifacts[0].Bytes)
		if resource.Kind() == ir.KindHook && !strings.Contains(text, `$(git rev-parse --show-toplevel)\"/plugins/demo/native`) {
			t.Fatalf("native hook token not resolved to the installed support base: %s", text)
		}
		switch resource.Name() {
		case "native-stdio":
			for _, want := range []string{"command = '${env:COMMAND}'", "args = ['${file:/native/arg}']", "TOKEN = '${TOKEN}'", "env_vars = ['FORWARDED']"} {
				if !strings.Contains(text, want) {
					t.Errorf("native stdio MCP output missing %q:\n%s", want, text)
				}
			}
		case "native-remote":
			for _, want := range []string{"url = 'https://example.test/mcp'", "Authorization = '${env:TOKEN}'", "X-Token = 'HEADER_TOKEN'"} {
				if !strings.Contains(text, want) {
					t.Errorf("native remote MCP output missing %q:\n%s", want, text)
				}
			}
		}
	}
}

func hasDiagnosticCode(diagnostics []diag.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
