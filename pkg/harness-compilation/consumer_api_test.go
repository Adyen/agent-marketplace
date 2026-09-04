// This file is deliberately in the external test package harnesscompile_test
// and imports ONLY the root package. It is the regression guard for the
// documented consumer contract: importing
//
//	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
//
// and nothing else must be enough to load, compile, materialize, ingest, and
// autodetect supported resources. Do NOT add blank imports of compile/<target>
// or ingest/<target> here — that would hide the very bug this file exists to
// catch.
package harnesscompile_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// claudeSourceFS is a minimal Claude-layout bundle covering every documented
// per-resource loader.
func claudeSourceFS() fstest.MapFS {
	return fstest.MapFS{
		".claude-plugin/plugin.json": {Data: []byte(`{"name":"demo","version":"0.1.0"}`)},
		"skills/demo-skill/SKILL.md": {Data: []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n")},
		"commands/demo-cmd.md":       {Data: []byte("---\nname: demo-cmd\ndescription: Demo command\n---\nRun the thing\n")},
		"agents/demo-agent.md":       {Data: []byte("---\nname: demo-agent\ndescription: Demo agent\n---\nReview the thing\n")},
		"hooks/hooks.json":           {Data: []byte(`{"PreToolUse":[{"matcher":"Read","hooks":[{"type":"command","command":"echo hi"}]}]}`)},
		".mcp.json":                  {Data: []byte(`{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-server"]}}}`)},
	}
}

// assertRootResourceTypes is a compile-time assertion that the root package
// exposes a name for every type the documented loaders return.
func assertRootResourceTypes(*hcc.Skill, *hcc.Command, *hcc.Subagent, *hcc.Hook, *hcc.McpServer) {}

func TestRootPackageExposesCodexAliases(t *testing.T) {
	if hcc.TargetCodex != "codex" || hcc.HarnessCodex != hcc.TargetCodex {
		t.Fatalf("Codex aliases = %q, %q", hcc.TargetCodex, hcc.HarnessCodex)
	}
}

// TestRootLoadersMatchDocumentedContract exercises the documented root-level
// Load* API and feeds each loaded resource through Compile for every declared
// compiler target.
func TestRootLoadersMatchDocumentedContract(t *testing.T) {
	src := claudeSourceFS()

	skill, err := hcc.LoadSkill(src, "skills/demo-skill")
	if err != nil {
		t.Fatalf("hcc.LoadSkill: %v", err)
	}
	command, err := hcc.LoadCommand(src, "commands/demo-cmd.md")
	if err != nil {
		t.Fatalf("hcc.LoadCommand: %v", err)
	}
	subagent, err := hcc.LoadSubagent(src, "agents/demo-agent.md")
	if err != nil {
		t.Fatalf("hcc.LoadSubagent: %v", err)
	}
	hooks, hookDiags, err := hcc.LoadHooks(src, "hooks/hooks.json")
	if err != nil {
		t.Fatalf("hcc.LoadHooks: %v", err)
	}
	if len(hooks) == 0 {
		t.Fatal("hcc.LoadHooks returned no hooks")
	}
	for _, d := range hookDiags {
		if d.Severity == hcc.SeverityError {
			t.Fatalf("hcc.LoadHooks diagnostic: %s: %s", d.Code, d.Message)
		}
	}
	servers, err := hcc.LoadMcpServers(src, ".mcp.json")
	if err != nil {
		t.Fatalf("hcc.LoadMcpServers: %v", err)
	}
	if len(servers) == 0 {
		t.Fatal("hcc.LoadMcpServers returned no servers")
	}

	// The loaders must return values nameable through the root package alone:
	// this call only compiles if the root type aliases cover every loader.
	assertRootResourceTypes(skill, command, subagent, hooks[0], servers[0])

	resources := []hcc.Resource{skill, command, subagent, hooks[0], servers[0]}
	for _, definition := range hcc.TargetDefinitions() {
		if !definition.Compiler {
			continue
		}
		target := hcc.Target(definition.Target)
		t.Run(string(target), func(t *testing.T) {
			for _, resource := range resources {
				result, err := hcc.Compile(resource, target)
				if err != nil {
					t.Fatalf("hcc.Compile(%s, %s): %v", resource.Name(), target, err)
				}
				for _, d := range result.Diagnostics {
					if d.Severity == hcc.SeverityError {
						t.Fatalf("hcc.Compile(%s, %s): error diagnostic %s: %s", resource.Name(), target, d.Code, d.Message)
					}
				}
			}
		})
	}
}

// TestRootPackageCompilesAndMaterializesWithoutBlankImports proves that
// CompileBundle (which needs both the Claude ingester and the target compiler)
// works for every declared compiler target with the root import alone.
func TestRootPackageCompilesAndMaterializesWithoutBlankImports(t *testing.T) {
	src := claudeSourceFS()
	outRoot := t.TempDir()

	for _, definition := range hcc.TargetDefinitions() {
		if !definition.Compiler {
			continue
		}
		target := hcc.Target(definition.Target)
		t.Run(string(target), func(t *testing.T) {
			result, err := hcc.CompileBundle(src, target)
			if err != nil {
				t.Fatalf("hcc.CompileBundle(%s): %v", target, err)
			}
			for _, d := range result.Diagnostics {
				if d.Severity == hcc.SeverityError {
					t.Fatalf("hcc.CompileBundle(%s): error diagnostic %s: %s", target, d.Code, d.Message)
				}
			}
			if len(result.Artifacts) == 0 {
				// A target whose per-kind emission is still staged (Devin)
				// must say so through diagnostics instead of silently
				// producing nothing. Nothing to materialize in that case.
				if len(result.Diagnostics) == 0 {
					t.Fatalf("hcc.CompileBundle(%s) produced neither artifacts nor diagnostics", target)
				}
				return
			}
			outDir := filepath.Join(outRoot, string(target))
			report, err := hcc.Materialize(result, outDir,
				hcc.WithWriteMode(hcc.WriteModeSync))
			requiresExternalReconciliation := false
			for _, candidate := range result.Artifacts {
				if candidate.ExternalReconciliationRequired {
					requiresExternalReconciliation = true
					break
				}
			}
			if requiresExternalReconciliation {
				if !errors.Is(err, hcc.ErrExternalReconciliationRequired) {
					t.Fatalf("hcc.Materialize(%s) error = %v, want %v", target, err, hcc.ErrExternalReconciliationRequired)
				}
				entries, readErr := os.ReadDir(outDir)
				if readErr != nil && !os.IsNotExist(readErr) {
					t.Fatalf("read refused materialization directory: %v", readErr)
				}
				if len(entries) != 0 {
					t.Fatalf("hcc.Materialize(%s) wrote %d entries before refusing external reconciliation", target, len(entries))
				}
				return
			}
			if err != nil {
				t.Fatalf("hcc.Materialize(%s): %v", target, err)
			}
			if len(report.Written) == 0 {
				t.Fatalf("hcc.Materialize(%s) wrote nothing", target)
			}
		})
	}
}

// TestRootPackageIngestsEveryDeclaredSource proves the ingest half of the
// contract: every target that declares an ingester is reachable through
// IngestBundle and Autodetect with the root import alone.
func TestRootPackageIngestsEveryDeclaredSource(t *testing.T) {
	sources := map[hcc.Harness]fstest.MapFS{
		hcc.HarnessClaude:  claudeSourceFS(),
		hcc.HarnessFactory: {".factory/skills/demo-skill/SKILL.md": {Data: []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n")}},
		hcc.HarnessCursor:  {".cursor/rules/demo.mdc": {Data: []byte("---\ndescription: Demo rule\nalwaysApply: true\n---\nAlways do the thing\n")}},
		hcc.HarnessGemini:  {".gemini/skills/demo-skill/SKILL.md": {Data: []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n")}},
		hcc.HarnessWindsurf: {
			".windsurf/rules/demo.md":              {Data: []byte("---\ntrigger: always_on\n---\nAlways do the thing\n")},
			".windsurf/workflows/demo-flow.md":     {Data: []byte("---\ndescription: Demo workflow\n---\nRun the thing\n")},
			".windsurf/skills/demo-skill/SKILL.md": {Data: []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n")},
		},
		// The Devin fixture is the WORKSPACE shape on purpose: it is the only
		// shape autodetection recognizes, and this test asserts Autodetect as
		// well as IngestBundle. The direct `~/.config/devin` shape deliberately
		// fails that assertion, which is covered in ingest/devin.
		hcc.HarnessDevin: {".devin/skills/demo-skill/SKILL.md": {Data: []byte("---\nname: demo-skill\ndescription: Demo skill\n---\nDo the thing\n")}},
		hcc.HarnessCodex: {
			".codex/agents/demo-agent.toml": {Data: []byte("name = 'demo-agent'\ndescription = 'Demo agent'\ndeveloper_instructions = 'Review the thing'\n")},
		},
	}

	for _, definition := range hcc.TargetDefinitions() {
		if !definition.Ingester {
			continue
		}
		source := hcc.Harness(definition.Target)
		src, ok := sources[source]
		if !ok {
			t.Fatalf("no fixture for declared ingester source %q; add one so the contract stays covered", source)
		}
		t.Run(string(source), func(t *testing.T) {
			bundle, err := hcc.IngestBundle(src, source)
			if err != nil {
				t.Fatalf("hcc.IngestBundle(%s): %v", source, err)
			}
			if len(bundle.Resources) == 0 {
				t.Fatalf("hcc.IngestBundle(%s) returned no resources", source)
			}
			detected, ok := hcc.Autodetect(src)
			if !ok {
				t.Fatalf("hcc.Autodetect(%s) detected nothing", source)
			}
			if detected != source {
				t.Fatalf("hcc.Autodetect(%s) = %q, want %q", source, detected, source)
			}
		})
	}
}

// TestRootPackageValidatesItsOwnRegistrations is the startup-diagnostic view of
// the same contract: an external consumer can assert wiring completeness
// without importing the registries.
func TestRootPackageValidatesItsOwnRegistrations(t *testing.T) {
	if err := hcc.ValidateTargetRegistrations(); err != nil {
		t.Fatalf("hcc.ValidateTargetRegistrations: %v", err)
	}
}
