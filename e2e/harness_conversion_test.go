package e2e_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// conversionOutcome is how one in-process conversion must end.
type conversionOutcome int

const (
	// conversionClean: Convert and Materialize both succeed.
	conversionClean conversionOutcome = iota
	// conversionRefused: Convert fails, so nothing is materialized.
	conversionRefused
	// conversionPartial: Convert succeeds with error diagnostics, Materialize
	// writes the clean resources and returns hcc.ErrPartialResult.
	conversionPartial
	// conversionSharedRefused: Convert succeeds, but the artifact is a native
	// shared aggregate that Materialize refuses without external reconciliation,
	// so nothing is written.
	conversionSharedRefused
)

// conversionStep is one harness conversion run in-process through
// pkg/harness-compilation. Paths are relative to the scenario workspace.
type conversionStep struct {
	files         map[string]string // workspace-relative files written before the step
	from, to      hcc.Harness
	source, out   string // workspace-relative directories
	outcome       conversionOutcome
	diagnostic    string            // code expected in Result.Diagnostics, or in the refusal text
	diagnostics   int               // expected len(Result.Diagnostics); 0 leaves it unchecked
	contains      map[string]string // out-relative file -> required substring
	artifacts     map[string]string // artifact path -> required substring in Result.Artifacts
	notContains   map[string]string // out-relative file -> forbidden substring
	unchanged     []string          // out-relative files whose bytes must not change
	treeUnchanged bool              // the whole output tree must not change
}

func TestHarnessConversionSecurity(t *testing.T) {
	scenarios := []struct {
		name  string
		desc  string
		steps []conversionStep
	}{
		{
			name: "security/mcp_parse_error_not_absence",
			desc: "Given an MCP duplicate key whose text resembles a missing-file error, When converting, Then malformed input is refused without changing output and a repaired file converts normally",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/.windsurf/skills/control/SKILL.md": "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n",
						"source/.windsurf/mcp_config.json":         `{"mcpServers":{"fixture":{"command":"fixture","file does not exist":1,"file does not exist":2}}}`,
						"output/keep.txt":                          "HANDWRITTEN OUTPUT\n",
					},
					from: hcc.HarnessWindsurf, to: hcc.HarnessFactory,
					source: "source", out: "output",
					outcome:       conversionRefused,
					diagnostic:    "json.duplicate-key",
					treeUnchanged: true,
				},
				{
					files: map[string]string{
						"source/.windsurf/mcp_config.json": `{"mcpServers":{"fixture":{"command":"fixture"}}}`,
					},
					from: hcc.HarnessWindsurf, to: hcc.HarnessFactory,
					source: "source", out: "output",
					outcome:   conversionClean,
					contains:  map[string]string{"mcp.json": "fixture", "skills/control/SKILL.md": "CONTROL BODY"},
					unchanged: []string{"keep.txt"},
				},
			},
		},
		{
			name: "security/windsurf_direct_root_layout",
			desc: "Given direct-root Windsurf resources with root agent notes and MCP configuration, When converting, Then resource output is not hidden by a nonexistent workspace layout",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/AGENTS.md":           "# Notes\n",
						"source/workflows/deploy.md": "---\ndescription: Deploy\n---\nWORKFLOW BODY\n",
						"source/hooks.json":          `{"hooks":{"pre_run_command":[{"command":"guard-command"}]}}`,
						"source/mcp_config.json":     `{"mcpServers":{"native-server":{"command":"fixture-server"}}}`,
					},
					from: hcc.HarnessWindsurf, to: hcc.HarnessWindsurf,
					source: "source", out: "converted",
					outcome: conversionClean,
					contains: map[string]string{
						".windsurf/workflows/deploy.md": "WORKFLOW BODY",
						".windsurf/hooks.json":          "guard-command",
						".windsurf/mcp_config.json":     "fixture-server",
					},
				},
			},
		},
		{
			name: "security/factory_declared_identity",
			desc: "Given mixed Factory layouts with colliding declared names and a distinct filename collision, When converting, Then the first resource survives and the distinct resource is retained",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/commands/source.md":          "---\nname: review\ndescription: First\n---\nFIRST RESOURCE\n",
						"source/.factory/commands/alias.md":  "---\nname: review\ndescription: Shadow\n---\nSHADOW RESOURCE\n",
						"source/.factory/commands/review.md": "---\nname: other\ndescription: Unique\n---\nUNIQUE RESOURCE\n",
					},
					from: hcc.HarnessFactory, to: hcc.HarnessFactory,
					source: "source", out: "converted",
					outcome: conversionClean,
					contains: map[string]string{
						"commands/review.md": "FIRST RESOURCE",
						"commands/other.md":  "UNIQUE RESOURCE",
					},
					notContains: map[string]string{"commands/review.md": "SHADOW RESOURCE"},
				},
			},
		},
		{
			name: "security/codex_literal_header",
			desc: "Given a Codex literal that resembles a bare environment reference and a safe sibling, When converting to Windsurf, Then the unsafe reinterpretation is refused while literal-preserving conversion remains available",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/.codex/config.toml": "[mcp_servers.literal]\nurl = 'https://example.invalid/mcp'\n[mcp_servers.literal.http_headers]\nX-Literal = '$SYNTHETIC_KEY'\n[mcp_servers.safe]\nurl = 'https://example.invalid/mcp'\n[mcp_servers.safe.http_headers]\nX-Literal = 'ordinary'\n",
					},
					from: hcc.HarnessCodex, to: hcc.HarnessWindsurf,
					source: "source", out: "converted",
					outcome:     conversionPartial,
					diagnostic:  "mcp.security-restriction-unsupported",
					contains:    map[string]string{".windsurf/mcp_config.json": "ordinary"},
					notContains: map[string]string{".windsurf/mcp_config.json": "SYNTHETIC_KEY"},
				},
				{
					from: hcc.HarnessCodex, to: hcc.HarnessCodex,
					source: "source", out: "native",
					outcome:   conversionSharedRefused,
					artifacts: map[string]string{".codex/config.toml": "$SYNTHETIC_KEY"},
				},
			},
		},
		{
			name: "security/native_metadata_authority",
			desc: "Given foreign execution metadata and native workflow controls, When converting harnesses, Then only typed native controls remain active and ordinary metadata is preserved",
			steps: []conversionStep{
				{
					files: map[string]string{
						"agent-source/droids/guard.md": "---\nname: guard\ndescription: Guard\nx-note: preserved\nmcpServers:\n  hidden:\n    command: must-not-activate\n---\nBody\n",
					},
					from: hcc.HarnessFactory, to: hcc.HarnessGemini,
					source: "agent-source", out: "gemini-output",
					outcome:     conversionClean,
					contains:    map[string]string{".gemini/agents/guard.md": "x-note: preserved"},
					notContains: map[string]string{".gemini/agents/guard.md": "mcpServers:"},
				},
				{
					files: map[string]string{
						"command-source/commands/guard.md": "---\nname: guard\ndescription: Guard\nx-note: preserved\nauto_execution_mode: always\n---\nBody\n",
					},
					from: hcc.HarnessFactory, to: hcc.HarnessWindsurf,
					source: "command-source", out: "windsurf-output",
					outcome:     conversionClean,
					contains:    map[string]string{".windsurf/workflows/guard.md": "x-note: preserved"},
					notContains: map[string]string{".windsurf/workflows/guard.md": "auto_execution_mode:"},
				},
				{
					files: map[string]string{
						"native/.windsurf/workflows/guard.md": "---\ndescription: Guard\nauto_execution_mode: always\n---\nBody\n",
					},
					from: hcc.HarnessWindsurf, to: hcc.HarnessWindsurf,
					source: "native", out: "native-output",
					outcome:  conversionClean,
					contains: map[string]string{".windsurf/workflows/guard.md": "auto_execution_mode: always"},
				},
			},
		},
		{
			name: "security/factory_hook_event",
			desc: "Given a Devin permission hook and a valid tool hook, When converting to Factory, Then unsupported authorization behavior is reported and omitted without suppressing the valid sibling",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/.devin/hooks.v1.json": `{"PermissionRequest":[{"hooks":[{"type":"command","command":"permission-check"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"valid-check"}]}]}`,
					},
					from: hcc.HarnessDevin, to: hcc.HarnessFactory,
					source: "source", out: "converted",
					outcome:     conversionPartial,
					diagnostic:  "hook.event-unmappable",
					contains:    map[string]string{"hooks/hooks.json": "valid-check"},
					notContains: map[string]string{"hooks/hooks.json": "PermissionRequest"},
				},
			},
		},
		{
			name: "security/windsurf_hook_scope_conversion",
			desc: "Given scoped hooks, When converting between Windsurf and Claude, Then Write and Edit coverage survives and subagent hooks do not become global response hooks",
			steps: []conversionStep{
				{
					files: map[string]string{
						"source/.windsurf/hooks.json": `{"hooks":{"pre_write_code":[{"command":"guard-write-and-edit"}]}}`,
					},
					from: hcc.HarnessWindsurf, to: hcc.HarnessClaude,
					source: "source", out: "converted",
					outcome:  conversionClean,
					contains: map[string]string{"hooks/hooks.json": "Write|Edit"},
				},
				{
					from: hcc.HarnessClaude, to: hcc.HarnessWindsurf,
					source: "converted", out: "roundtrip",
					outcome:  conversionClean,
					contains: map[string]string{".windsurf/hooks.json": "pre_write_code"},
				},
				{
					files: map[string]string{
						"scoped/hooks/hooks.json": `{"Stop":[{"hooks":[{"type":"command","command":"global-stop"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":"subagent-only"}]}]}`,
					},
					from: hcc.HarnessClaude, to: hcc.HarnessWindsurf,
					source: "scoped", out: "scope-output",
					outcome:     conversionClean,
					diagnostics: 1,
					contains:    map[string]string{".windsurf/hooks.json": "global-stop"},
					notContains: map[string]string{".windsurf/hooks.json": "subagent-only"},
				},
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Log(scenario.desc)
			workspace := t.TempDir()
			for _, step := range scenario.steps {
				runConversionStep(t, workspace, step)
			}
		})
	}
}

func runConversionStep(t *testing.T, workspace string, step conversionStep) {
	t.Helper()
	for rel, body := range step.files {
		writeConversionFile(t, filepath.Join(workspace, filepath.FromSlash(rel)), body)
	}
	source := filepath.Join(workspace, filepath.FromSlash(step.source))
	out := filepath.Join(workspace, filepath.FromSlash(step.out))
	before := snapshotConversionTree(t, out)

	result, convertErr := hcc.Convert(os.DirFS(source), step.from, step.to, hcc.WithPluginRoot(source))
	var materializeErr error
	if convertErr == nil {
		_, materializeErr = hcc.Materialize(result, out, hcc.WithWriteMode(hcc.WriteModeOverwrite))
	}

	switch step.outcome {
	case conversionClean:
		if convertErr != nil || materializeErr != nil {
			t.Fatalf("conversion %s -> %s failed: convert=%v materialize=%v", step.from, step.to, convertErr, materializeErr)
		}
	case conversionRefused:
		if convertErr == nil {
			t.Fatalf("conversion %s -> %s succeeded, want refusal", step.from, step.to)
		}
	case conversionPartial:
		if convertErr != nil {
			t.Fatalf("conversion %s -> %s refused: %v, want partial result", step.from, step.to, convertErr)
		}
		if !errors.Is(materializeErr, hcc.ErrPartialResult) {
			t.Fatalf("materialize error = %v, want hcc.ErrPartialResult", materializeErr)
		}
	case conversionSharedRefused:
		if convertErr != nil {
			t.Fatalf("conversion %s -> %s refused: %v, want converted artifacts", step.from, step.to, convertErr)
		}
		if !errors.Is(materializeErr, hcc.ErrExternalReconciliationRequired) {
			t.Fatalf("materialize error = %v, want hcc.ErrExternalReconciliationRequired", materializeErr)
		}
	}

	if step.diagnostic != "" {
		if !hasDiagnosticCode(result, step.diagnostic) && !strings.Contains(stringifyErr(convertErr), step.diagnostic) {
			t.Fatalf("diagnostic %q not reported (convert error: %v)", step.diagnostic, convertErr)
		}
	}
	if got := diagnosticTotal(result); step.diagnostics != 0 && got != step.diagnostics {
		t.Fatalf("diagnostics = %d, want %d", got, step.diagnostics)
	}

	if step.treeUnchanged {
		assertConversionTreeEqual(t, before, snapshotConversionTree(t, out))
	}
	for rel, substr := range step.contains {
		if got := readConversionFile(t, out, rel); !strings.Contains(got, substr) {
			t.Fatalf("%s does not contain %q:\n%s", rel, substr, got)
		}
	}
	for rel, substr := range step.artifacts {
		if !hasArtifactContaining(result, rel, substr) {
			t.Fatalf("no artifact %s contains %q", rel, substr)
		}
	}
	for rel, substr := range step.notContains {
		if got := readConversionFile(t, out, rel); strings.Contains(got, substr) {
			t.Fatalf("%s unexpectedly contains %q:\n%s", rel, substr, got)
		}
	}
	for _, rel := range step.unchanged {
		if got, want := readConversionFile(t, out, rel), before[filepath.ToSlash(rel)]; got != want {
			t.Fatalf("%s changed:\ngot  %q\nwant %q", rel, got, want)
		}
	}
}

func writeConversionFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readConversionFile(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// snapshotConversionTree maps every file under root to its bytes. A missing
// root is an empty tree.
func snapshotConversionTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertConversionTreeEqual(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("output tree changed: before %d files, after %d files", len(before), len(after))
	}
	for rel, body := range before {
		if after[rel] != body {
			t.Fatalf("output file %s changed", rel)
		}
	}
}

func stringifyErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func diagnosticTotal(result *hcc.Result) int {
	if result == nil {
		return 0
	}
	return len(result.Diagnostics)
}

func hasArtifactContaining(result *hcc.Result, path, substr string) bool {
	if result == nil {
		return false
	}
	for _, a := range result.Artifacts {
		if a.Path == path && strings.Contains(string(a.Bytes), substr) {
			return true
		}
	}
	return false
}

func hasDiagnosticCode(result *hcc.Result, code string) bool {
	if result == nil {
		return false
	}
	for _, d := range result.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}
