package materialize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// devinEnv isolates HOME, the cache, and the ownership state root so a Devin
// materialization test can never read or write the developer's real files.
// Ownership state lives outside the repo and outside the cache by design (plan,
// "Durable state"), so it needs its own explicit redirection.
func devinEnv(t *testing.T) (repoRoot, home string) {
	t.Helper()
	repoRoot = t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	return repoRoot, home
}

func devinResolved(name string, resource ir.Resource, machineScope bool) resolver.ResolvedArtifact {
	source := config.RepoCommitted
	if machineScope {
		source = config.UserPersonal
	}
	return resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: name, Resource: resource},
		Source:    source,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}
}

func devinProjectRoot(repoRoot string) string { return filepath.Join(repoRoot, ".devin") }

func devinUserRoot(home string) string { return filepath.Join(home, ".config", "devin") }

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func findDiag(diags []diag.Diagnostic, code string) (diag.Diagnostic, bool) {
	for _, d := range diags {
		if d.Code == code {
			return d, true
		}
	}
	return diag.Diagnostic{}, false
}

func requireNoErrorDiags(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected error diagnostic %s: %s", d.Code, d.Message)
		}
	}
}

// TestDevinAllFiveKindsMaterializeAtCanonicalPaths pins acceptance criterion
// "All five selectable kinds materialize at correct project/user paths".
func TestDevinAllFiveKindsMaterializeAtCanonicalPaths(t *testing.T) {
	for _, machineScope := range []bool{false, true} {
		name := "project"
		if machineScope {
			name = "user"
		}
		for _, mode := range []config.MaterializationMode{config.MaterializationModeSymlink, config.MaterializationModeFile} {
			t.Run(name+"/"+string(mode), func(t *testing.T) {
				repoRoot, home := devinEnv(t)

				resolved := []resolver.ResolvedArtifact{
					devinResolved("my-skill", ir.NewSkill("my-skill", "A skill", "Skill body", nil, nil, ir.SourceRef{}), machineScope),
					devinResolved("my-command", ir.NewCommand("my-command", "A command", "Command body", nil, ir.SourceRef{}), machineScope),
					devinResolved("my-agent", ir.NewSubagent("my-agent", "An agent", "Agent body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), machineScope),
					devinResolved("my-hook", ir.NewHook("my-hook", "PostToolUse", "echo hi", "command", nil, ir.SourceRef{}), machineScope),
					devinResolved("my-mcp", ir.NewMcpServer("my-mcp", "server-bin", []string{"--stdio"}, nil, nil, ir.SourceRef{}), machineScope),
				}

				report, err := Run(context.Background(), repoRoot, resolved, WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode}))
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				requireNoErrorDiags(t, report.Diags)

				root := devinProjectRoot(repoRoot)
				hooksFile := harness.DevinProjectHooksFile
				if machineScope {
					root = devinUserRoot(home)
					hooksFile = harness.DevinUserConfigFile
				}

				// Skills and converted commands are directories at skills/<name>;
				// flat subagents are files at agents/<name>.md, linked or copied.
				for _, rel := range []string{
					filepath.Join("skills", "my-skill"),
					filepath.Join("skills", "my-command"),
					filepath.Join("agents", "my-agent.md"),
				} {
					path := filepath.Join(root, rel)
					info, lerr := os.Lstat(path)
					if lerr != nil {
						t.Fatalf("expected %s to exist: %v", path, lerr)
					}
					if (info.Mode()&os.ModeSymlink != 0) != (mode == config.MaterializationModeSymlink) {
						t.Fatalf("%s has mode %v, want installation mode %s", path, info.Mode(), mode)
					}
				}
				if _, err := os.Stat(filepath.Join(root, "skills", "my-command", "SKILL.md")); err != nil {
					t.Fatalf("expected the converted command to resolve to skills/my-command/SKILL.md: %v", err)
				}
				// The command-provenance extension has to survive materialization.
				body := readFileString(t, filepath.Join(root, "skills", "my-command", "SKILL.md"))
				if !strings.Contains(body, "x-agent-marketplace-kind: command") {
					t.Fatalf("converted command SKILL.md lost its provenance extension:\n%s", body)
				}

				hooks := readFileString(t, filepath.Join(root, hooksFile))
				if !strings.Contains(hooks, "PostToolUse") {
					t.Fatalf("%s does not contain the compiled hook event:\n%s", hooksFile, hooks)
				}
				var document map[string]json.RawMessage
				if err := json.Unmarshal([]byte(hooks), &document); err != nil {
					t.Fatal(err)
				}
				_, wrapped := document["hooks"]
				if wrapped != machineScope {
					t.Fatalf("only user-scope hooks belong under the top-level hooks member:\n%s", hooks)
				}

				mcp := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile))
				if !strings.Contains(mcp, `"my-mcp"`) || !strings.Contains(mcp, `"mcpServers"`) {
					t.Fatalf("%s does not contain the compiled MCP entry:\n%s", harness.DevinMcpConfigFile, mcp)
				}

				// No `_managed_by` marker may be injected into any Devin aggregate:
				// ownership is proven from the durable store, never inferred.
				for _, rel := range []string{hooksFile, harness.DevinMcpConfigFile} {
					if strings.Contains(readFileString(t, filepath.Join(root, rel)), managedByField) {
						t.Fatalf("%s carries an in-file ownership marker; Devin ownership must be proven, not inferred", rel)
					}
				}
			})
		}
	}
}

// TestDevinUserHooksPatchConfigWithoutTouchingUnrelatedSettings is the sharpest
// test of the D9 JSONC layer, per the acceptance criteria: unrelated keys,
// comments, and key order must survive byte-for-byte.
func TestDevinUserHooksPatchConfigWithoutTouchingUnrelatedSettings(t *testing.T) {
	repoRoot, home := devinEnv(t)
	root := devinUserRoot(home)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{
  // Devin telemetry preference, hand-written.
  "telemetry": false,
  "zzz_last": {
    "nested":    "keeps    its    spacing"
  },
  /* block comment above an unrelated key */
  "aaa_first": [1, 2, 3]
}
`
	configPath := filepath.Join(root, harness.DevinUserConfigFile)
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("user-hook", ir.NewHook("user-hook", "SessionStart", "echo start", "command", nil, ir.SourceRef{}), true),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)

	patched := readFileString(t, configPath)

	// The patch must be purely additive: every byte of the original document up
	// to and including its last member is preserved verbatim, in order. Only the
	// separating comma and the new member may be added after it.
	prefix := original[:strings.Index(original, `"aaa_first": [1, 2, 3]`)+len(`"aaa_first": [1, 2, 3]`)]
	if !strings.HasPrefix(patched, prefix) {
		t.Fatalf("unrelated settings, comments, or key order changed.\nwant prefix:\n%q\ngot:\n%q", prefix, patched)
	}
	if rest := patched[len(prefix):]; !strings.HasPrefix(rest, ",") {
		t.Fatalf("the patch was not purely additive after the last original member: %q", rest)
	}
	for _, fragment := range []string{
		"// Devin telemetry preference, hand-written.",
		`"telemetry": false`,
		`"nested":    "keeps    its    spacing"`,
		"/* block comment above an unrelated key */",
		`"aaa_first": [1, 2, 3]`,
	} {
		if !strings.Contains(patched, fragment) {
			t.Fatalf("patched config.json lost %q:\n%s", fragment, patched)
		}
	}
	// Key order must be source order, not sorted order.
	if idx1, idx2 := strings.Index(patched, `"zzz_last"`), strings.Index(patched, `"aaa_first"`); idx1 > idx2 {
		t.Fatalf("member order was re-sorted:\n%s", patched)
	}
	if !strings.Contains(patched, `"SessionStart"`) {
		t.Fatalf("the generated hook was not patched in:\n%s", patched)
	}

	// A second run is a no-op: it must not rewrite the file.
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	requireNoErrorDiags(t, report2.Diags)
	if got := readFileString(t, configPath); got != patched {
		t.Fatalf("second run rewrote config.json.\nfirst:\n%s\nsecond:\n%s", patched, got)
	}
	if after, err := os.Stat(configPath); err == nil && !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("second run replaced config.json even though nothing changed")
	}
}

// TestDevinUnmanagedMcpCollisionIsPreserved pins "unmanaged collisions are
// preserved": the existing entry is neither overwritten nor deleted, and only
// the generated entry fails.
func TestDevinUnmanagedMcpCollisionIsPreserved(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{
  "mcpServers": {
    "collide": { "command": "hand-written-binary" }
  }
}
`
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("collide", ir.NewMcpServer("collide", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("fine", ir.NewMcpServer("fine", "other-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	d, ok := findDiag(report.Diags, diag.CodeOwnershipUnmanagedKey)
	if !ok {
		t.Fatalf("expected %s, got %v", diag.CodeOwnershipUnmanagedKey, report.Diags)
	}
	if d.Severity != diag.SeverityWarning {
		t.Fatalf("%s severity = %v, want warning", d.Code, d.Severity)
	}
	if !strings.Contains(d.Message, "collide") {
		t.Fatalf("collision diagnostic does not name the colliding key: %s", d.Message)
	}

	patched := readFileString(t, mcpPath)
	if !strings.Contains(patched, "hand-written-binary") {
		t.Fatalf("the unmanaged entry was overwritten:\n%s", patched)
	}
	if strings.Contains(patched, "generated-binary") {
		t.Fatalf("the generated entry won a collision it must lose:\n%s", patched)
	}
	// The sibling resource still commits: "valid resources/files commit despite
	// sibling errors".
	if !strings.Contains(patched, "other-binary") {
		t.Fatalf("the non-colliding sibling entry was not written:\n%s", patched)
	}
}

// TestDevinStaleProvenEntryPrunesAndUnmanagedNeverDoes pins "Only stale PROVEN
// entries prune" from both directions.
func TestDevinStaleProvenEntryPrunesAndUnmanagedNeverDoes(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte("{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-binary\" }\n  }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withEntry := []resolver.ResolvedArtifact{
		devinResolved("managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if report, err := Run(context.Background(), repoRoot, withEntry); err != nil {
		t.Fatalf("first Run: %v", err)
	} else {
		requireNoErrorDiags(t, report.Diags)
	}
	if got := readFileString(t, mcpPath); !strings.Contains(got, "managed-binary") {
		t.Fatalf("managed entry was not written:\n%s", got)
	}

	// Deselect the resource. The proven owned entry prunes; the handwritten one
	// is untouched because it was never ours.
	report, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)
	got := readFileString(t, mcpPath)
	if strings.Contains(got, "managed-binary") {
		t.Fatalf("stale proven owned entry was not pruned:\n%s", got)
	}
	if !strings.Contains(got, "user-binary") {
		t.Fatalf("handwritten entry was pruned; only PROVEN entries may prune:\n%s", got)
	}
}

// TestDevinMissingOwnershipStateNeverPrunes pins the fail-closed direction:
// "Missing or corrupt ownership state makes all existing aggregate entries
// unmanaged", so nothing is pruned even for an entry we really did write.
func TestDevinMissingOwnershipStateNeverPrunes(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)

	withEntry := []resolver.ResolvedArtifact{
		devinResolved("managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, withEntry); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Destroy the durable authority, keeping the aggregate exactly as written.
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if err := os.RemoveAll(filepath.Join(stateRoot, "agent-marketplace", "ownership")); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := readFileString(t, mcpPath); !strings.Contains(got, "managed-binary") {
		t.Fatalf("an entry was pruned without proof of ownership:\n%s", got)
	}
}

// TestDevinDriftedOwnedEntryIsReplaced pins "Manual edits to a proven owned
// entry are reported as drift and replaced from marketplace configuration".
func TestDevinDriftedOwnedEntryIsReplaced(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)

	resolved := []resolver.ResolvedArtifact{
		devinResolved("drifter", ir.NewMcpServer("drifter", "correct-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	edited := strings.ReplaceAll(readFileString(t, mcpPath), "correct-binary", "hand-edited-binary")
	if err := os.WriteFile(mcpPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	d, ok := findDiag(report.Diags, diag.CodeOwnershipEntryDrift)
	if !ok {
		t.Fatalf("expected %s, got %v", diag.CodeOwnershipEntryDrift, report.Diags)
	}
	if d.Severity != diag.SeverityWarning {
		t.Fatalf("%s severity = %v, want warning", d.Code, d.Severity)
	}
	got := readFileString(t, mcpPath)
	if strings.Contains(got, "hand-edited-binary") || !strings.Contains(got, "correct-binary") {
		t.Fatalf("drifted owned entry was not replaced from configuration:\n%s", got)
	}
}

// TestDevinHandwrittenHooksKeepOrderAndSatisfyIdenticalDesiredEntries covers two
// plan rules at once: "Handwritten hooks keep their relative order and execute
// before generated hooks" and "One semantically identical unmanaged hook may
// satisfy any number of selected marketplace resources without duplication".
func TestDevinHandwrittenHooksKeepOrderAndSatisfyIdenticalDesiredEntries(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(root, harness.DevinProjectHooksFile)
	original := `{
  "PostToolUse": [
    { "hooks": [{ "command": "first-handwritten", "type": "command" }] },
    { "hooks": [{ "command": "second-handwritten", "type": "command" }] }
  ]
}
`
	if err := os.WriteFile(hooksPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("gen", ir.NewHook("gen", "PostToolUse", "generated", "command", nil, ir.SourceRef{}), false),
		// Semantically identical to the first handwritten entry.
		devinResolved("dup", ir.NewHook("dup", "PostToolUse", "first-handwritten", "command", nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)

	if d, ok := findDiag(report.Diags, diag.CodeMaterializeDevinHookSatisfiedByUnmanaged); !ok {
		t.Fatalf("expected %s, got %v", diag.CodeMaterializeDevinHookSatisfiedByUnmanaged, report.Diags)
	} else if d.Severity != diag.SeverityWarning || d.Resource != "dup" {
		t.Fatalf("%s = severity %v resource %q, want warning/\"dup\"", d.Code, d.Severity, d.Resource)
	}

	patched := readFileString(t, hooksPath)
	if strings.Count(patched, "first-handwritten") != 1 {
		t.Fatalf("an identical handwritten hook was duplicated:\n%s", patched)
	}
	first := strings.Index(patched, "first-handwritten")
	second := strings.Index(patched, "second-handwritten")
	generated := strings.Index(patched, "generated")
	if first >= second || second >= generated {
		t.Fatalf("handwritten hooks must keep their relative order and precede generated hooks:\n%s", patched)
	}
}

// TestDevinMalformedAggregateBlocksOnlyThatFile pins "Malformed destination JSON
// blocks that file only" plus "Malformed JSON is safety-backed up before parsing,
// left unchanged, and reported with a precise parse location".
func TestDevinMalformedAggregateBlocksOnlyThatFile(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "{ \"mcpServers\": { \"a\": }\n"
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("mcp", ir.NewMcpServer("mcp", "bin", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("hook", ir.NewHook("hook", "PostToolUse", "echo", "command", nil, ir.SourceRef{}), false),
		devinResolved("skill", ir.NewSkill("skill", "d", "b", nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, ok := findDiag(report.Diags, diag.CodeJSONMalformed); !ok {
		if _, ok := findDiag(report.Diags, diag.CodeMaterializeDevinAggregateMalformed); !ok {
			t.Fatalf("expected a malformed-JSON diagnostic, got %v", report.Diags)
		}
	}
	if got := readFileString(t, mcpPath); got != broken {
		t.Fatalf("a malformed aggregate must be left byte-for-byte as found.\nwant %q\ngot  %q", broken, got)
	}
	// The other aggregate and the skill symlink still materialize.
	if _, err := os.Stat(filepath.Join(root, harness.DevinProjectHooksFile)); err != nil {
		t.Fatalf("malformed mcp_config.json blocked the unrelated hooks file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "skills", "skill")); err != nil {
		t.Fatalf("malformed mcp_config.json blocked an unrelated skill: %v", err)
	}
	// The raw pre-parse bytes are retained.
	backupRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-marketplace", "safety-backups")
	found := false
	_ = filepath.Walk(backupRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".bak") {
			if data, readErr := os.ReadFile(path); readErr == nil && string(data) == broken {
				found = true
			}
		}
		return nil
	})
	if !found {
		t.Fatalf("no safety backup holds the exact malformed bytes under %s", backupRoot)
	}
}

// TestDevinPartialFailureCommitsSiblingsAndPostponesPrune pins bead
// agent-marketplace-5exj.17's contract, narrowed to one file: an erroring hook
// must not stop mcp_config.json from pruning.
func TestDevinPartialFailureCommitsSiblingsAndPostponesPrune(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)

	good := devinResolved("keep", ir.NewHook("keep", "PostToolUse", "echo keep", "command", nil, ir.SourceRef{}), false)
	staleHook := devinResolved("drop", ir.NewHook("drop", "PostToolUse", "echo drop", "command", nil, ir.SourceRef{}), false)
	staleMcp := devinResolved("mcp-drop", ir.NewMcpServer("mcp-drop", "bin", nil, nil, nil, ir.SourceRef{}), false)

	if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{good, staleHook, staleMcp}); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	hooksPath := filepath.Join(root, harness.DevinProjectHooksFile)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	if got := readFileString(t, hooksPath); !strings.Contains(got, "echo drop") {
		t.Fatalf("setup: expected both hooks to be written:\n%s", got)
	}

	// A subagent that shadows a Devin built-in is a hard error
	// (subagent.devin.builtin-name-conflict), so this run is partial. It also
	// drops both stale resources.
	//
	// This used to be a rule: bead agent-marketplace-5exj.22 made
	// rule.devin.unsupported a WARNING, because dropping an out-of-scope rule is
	// not a failure, so a rule can no longer produce a resource-level error. The
	// contract under test — an erroring resource that contributes to NO
	// aggregate file must not suppress pruning of any aggregate file — is
	// unchanged; only the resource that carries it moved.
	failing := resolver.ResolvedArtifact{
		Entry: catalog.Entry{Name: "subagent_explore", Resource: ir.NewSubagent(
			"subagent_explore", "shadows a built-in", "body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}
	// A hook that Devin cannot represent: an unknown event skips only that entry,
	// so force a resource-level error with the subagent above, and additionally
	// keep one valid hook so the file still has desired content.
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{good, failing})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if _, ok := findDiag(report.Diags, diag.CodeSubagentDevinBuiltinNameConflict); !ok {
		t.Fatalf("expected %s, got %v", diag.CodeSubagentDevinBuiltinNameConflict, report.Diags)
	}
	hasError := false
	for _, d := range report.Diags {
		if d.Severity == diag.SeverityError {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("a partial run must surface an error diagnostic so sync exits non-zero")
	}
	// The valid hook still commits.
	if got := readFileString(t, hooksPath); !strings.Contains(got, "echo keep") {
		t.Fatalf("a valid sibling hook did not commit during a partial run:\n%s", got)
	}
	// The subagent contributes to no aggregate file, so per-file prune
	// suppression is NOT triggered and the genuinely deselected entries still
	// prune.
	if got := readFileString(t, hooksPath); strings.Contains(got, "echo drop") {
		t.Fatalf("a deselected hook did not prune even though no hook resource errored:\n%s", got)
	}
	if got := readFileString(t, mcpPath); strings.Contains(got, `"mcp-drop"`) {
		t.Fatalf("an erroring subagent suppressed pruning of an unrelated aggregate file:\n%s", got)
	}
}

// TestDevinRuleWarnsWithoutFailingTheRun is the consumer-side half of bead
// agent-marketplace-5exj.22. A rule selected for Devin still materializes
// nothing, but the diagnostic is a warning, so:
//
//   - internal/pipeline turns it into a printed `warning:` line rather than an
//     entry in Result.Errors, so `sync` exits zero;
//   - the run is not partial, so stale entries in unrelated aggregate files
//     still prune.
//
// The old ERROR was justified by "Devin has no rule primitive", which the D12
// probe falsified on 3000.3.27; the drop itself is a scope decision, and a
// scope decision must not fail a user's sync.
func TestDevinRuleWarnsWithoutFailingTheRun(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)

	keep := devinResolved("keep", ir.NewHook("keep", "PostToolUse", "echo keep", "command", nil, ir.SourceRef{}), false)
	staleMcp := devinResolved("mcp-drop", ir.NewMcpServer("mcp-drop", "bin", nil, nil, nil, ir.SourceRef{}), false)
	if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{keep, staleMcp}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	rule := resolver.ResolvedArtifact{
		Entry:     catalog.Entry{Name: "a-rule", Resource: ir.NewRule("a-rule", "desc", "body", nil, false, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}
	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{keep, rule})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	d, ok := findDiag(report.Diags, diag.CodeRuleDevinUnsupported)
	if !ok {
		t.Fatalf("expected %s, got %v", diag.CodeRuleDevinUnsupported, report.Diags)
	}
	if d.Severity != diag.SeverityWarning {
		t.Errorf("severity = %v, want %v: an out-of-scope drop must not fail sync", d.Severity, diag.SeverityWarning)
	}
	for _, other := range report.Diags {
		if other.Severity == diag.SeverityError {
			t.Errorf("a rule selected for Devin produced an error diagnostic %s: %v", other.Code, other)
		}
	}
	if _, postponed := findDiag(report.Diags, diag.CodeMaterializeDevinAggregatePrunePostponed); postponed {
		t.Error("a warning-only run must not postpone pruning")
	}
	// The loss is still real: nothing is written for the rule anywhere under
	// the Devin root.
	if _, statErr := os.Stat(filepath.Join(root, "rules")); !os.IsNotExist(statErr) {
		t.Errorf("stat %s = %v, want the rule to remain unmaterialized", filepath.Join(root, "rules"), statErr)
	}
	// And a genuinely deselected entry still prunes, because nothing was skipped.
	if got := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile)); strings.Contains(got, `"mcp-drop"`) {
		t.Errorf("a deselected MCP entry survived a clean run:\n%s", got)
	}
}

// TestDevinErroringHookPostponesPruneForThatFileOnly proves the narrowed prune
// suppression directly: a skipped HOOK resource stops hooks.v1.json from
// pruning, and nothing else.
func TestDevinErroringHookPostponesPruneForThatFileOnly(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)

	keepHook := devinResolved("keep", ir.NewHook("keep", "PostToolUse", "echo keep", "command", nil, ir.SourceRef{}), false)
	dropHook := devinResolved("drop", ir.NewHook("drop", "PostToolUse", "echo drop", "command", nil, ir.SourceRef{}), false)
	dropMcp := devinResolved("mcp-drop", ir.NewMcpServer("mcp-drop", "bin", nil, nil, nil, ir.SourceRef{}), false)
	if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{keepHook, dropHook, dropMcp}); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// A hook with an unmappable event is skipped by the compiler with a warning,
	// so build a resource-level hook failure instead: a nil-resource hook cannot
	// be constructed, so use a hook whose kind errors is not available. Instead
	// simulate the skip directly at the reconciler level.
	work := &ownedJSONAggregateWork{}
	work.markIncomplete(harness.DevinProjectHooksFile)
	key := outputKey{target: hcc.TargetDevin, machineScope: false}
	report := &Report{PerTarget: map[hcc.Target]TargetReport{}}
	report = reconcileDevinAggregates(context.Background(), report, key, root, work, false)

	d, ok := findDiag(report.Diags, diag.CodeMaterializeDevinAggregatePrunePostponed)
	if !ok {
		t.Fatalf("expected %s, got %v", diag.CodeMaterializeDevinAggregatePrunePostponed, report.Diags)
	}
	if d.Severity != diag.SeverityWarning {
		t.Fatalf("%s severity = %v, want warning", d.Code, d.Severity)
	}
	if !strings.Contains(d.Message, harness.DevinProjectHooksFile) {
		t.Fatalf("prune-postponed diagnostic does not name the file: %s", d.Message)
	}
	if got := readFileString(t, filepath.Join(root, harness.DevinProjectHooksFile)); !strings.Contains(got, "echo keep") {
		t.Fatalf("a postponed prune must leave the file untouched:\n%s", got)
	}
	// mcp_config.json was NOT marked incomplete, so its stale entry still pruned.
	if got := readFileString(t, filepath.Join(root, harness.DevinMcpConfigFile)); strings.Contains(got, `"mcp-drop"`) {
		t.Fatalf("prune suppression leaked to an unrelated aggregate file:\n%s", got)
	}
}

// TestDevinResourceModeMigration exercises the generic installation contract
// for both directory-shaped commands and flat subagent profiles.
func TestDevinResourceModeMigration(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(fmt.Sprintf("machine=%v", machine), func(t *testing.T) {
			repoRoot, home := devinEnv(t)
			root := devinProjectRoot(repoRoot)
			if machine {
				root = devinUserRoot(home)
			}
			resolved := []resolver.ResolvedArtifact{
				devinResolved("command", ir.NewCommand("command", "d", "command body", nil, ir.SourceRef{}), machine),
				devinResolved("agent", ir.NewSubagent("agent", "d", "agent body", "sonnet", nil, "", nil, "", nil, ir.SourceRef{}), machine),
			}
			paths := []string{"skills/command", "agents/agent.md"}
			for _, mode := range []config.MaterializationMode{config.MaterializationModeFile, config.MaterializationModeSymlink, config.MaterializationModeFile} {
				options := []RunOption{WithMaterializationModes(config.MaterializationModes{Project: mode, Machine: mode})}
				report, err := Run(context.Background(), repoRoot, resolved, options...)
				if err != nil {
					t.Fatal(err)
				}
				requireNoErrorDiags(t, report.Diags)
				for _, rel := range paths {
					info, err := os.Lstat(filepath.Join(root, rel))
					if err != nil || (info.Mode()&os.ModeSymlink != 0) != (mode == config.MaterializationModeSymlink) {
						t.Fatalf("%s in %s mode: info=%v err=%v", rel, mode, info, err)
					}
				}
				if got := readFileString(t, filepath.Join(root, "skills/command/SKILL.md")); !strings.Contains(got, "x-agent-marketplace-kind: command") || !strings.Contains(got, "command body") {
					t.Fatalf("converted command lost content: %s", got)
				}
				if got := readFileString(t, filepath.Join(root, "agents/agent.md")); !strings.Contains(got, "agent body") {
					t.Fatalf("subagent lost content: %s", got)
				}
				check, err := Run(context.Background(), repoRoot, resolved, append(options, WithDryRun())...)
				if err != nil || !check.InSync() {
					t.Fatalf("mode %s has drift: report=%+v err=%v", mode, check, err)
				}
			}
			// Unmodified regular outputs remain owned and can be pruned.
			report, err := Run(context.Background(), repoRoot, nil, WithMaterializationModes(config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}))
			if err != nil {
				t.Fatal(err)
			}
			requireNoErrorDiags(t, report.Diags)
			for _, rel := range paths {
				if _, err := os.Lstat(filepath.Join(root, rel)); !os.IsNotExist(err) {
					t.Fatalf("stale %s was not pruned: %v", rel, err)
				}
			}
		})
	}
}

// TestDevinSymlinkLifecycle walks the whole symlink matrix the work package
// requires: create, update to a new target, replace when the source moves,
// remove on prune, and every pre-existing destination shape.
func TestDevinSymlinkLifecycle(t *testing.T) {
	skill := func(name, body string) ir.Resource {
		return ir.NewSkill(name, "desc", body, nil, nil, ir.SourceRef{})
	}

	t.Run("create then update then prune", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		link := filepath.Join(devinProjectRoot(repoRoot), "skills", "life")

		resolved := []resolver.ResolvedArtifact{devinResolved("life", skill("life", "v1"), false)}
		if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
			t.Fatalf("create: %v", err)
		}
		firstTarget, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("create: expected a symlink at %s: %v", link, err)
		}
		if got := readFileString(t, filepath.Join(link, "SKILL.md")); !strings.Contains(got, "v1") {
			t.Fatalf("create: symlink does not resolve to the compiled skill:\n%s", got)
		}

		// A different registry coordinate changes the cache directory, so the
		// symlink must be repointed rather than left stale.
		moved := resolved[0]
		moved.RegistryURL = "https://example.invalid/other.git"
		if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{moved}); err != nil {
			t.Fatalf("update: %v", err)
		}
		secondTarget, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if secondTarget == firstTarget {
			t.Fatalf("update: symlink target did not change when the source moved (%s)", secondTarget)
		}

		if _, err := Run(context.Background(), repoRoot, nil); err != nil {
			t.Fatalf("prune: %v", err)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatalf("prune: expected %s to be removed, got %v", link, err)
		}
	})

	preexisting := []struct {
		name string
		// setup creates the destination before sync runs.
		setup func(t *testing.T, dst string)
		// wantSymlink is true when sync is expected to take the destination over.
		wantSymlink bool
		wantDiag    string
	}{
		{
			name: "regular file destination is preserved",
			setup: func(t *testing.T, dst string) {
				if err := os.WriteFile(dst, []byte("hand-written"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantSymlink: false,
			wantDiag:    "materialize.collision.user-owned",
		},
		{
			name: "user-owned directory destination is preserved",
			setup: func(t *testing.T, dst string) {
				if err := os.MkdirAll(dst, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("hand-written"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantSymlink: false,
			wantDiag:    "materialize.collision.user-owned",
		},
		{
			name: "dangling symlink into our cache is replaced",
			setup: func(t *testing.T, dst string) {
				if err := os.Symlink(filepath.Join(cacheCompiledDir(t), "gone"), dst); err != nil {
					t.Fatal(err)
				}
			},
			wantSymlink: true,
		},
		{
			name: "foreign symlink destination is preserved",
			setup: func(t *testing.T, dst string) {
				elsewhere := t.TempDir()
				if err := os.Symlink(elsewhere, dst); err != nil {
					t.Fatal(err)
				}
			},
			wantSymlink: true, // it stays a symlink, but to the user's target
			wantDiag:    "materialize.collision.user-owned",
		},
	}
	for _, tc := range preexisting {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinEnv(t)
			skillsDir := filepath.Join(devinProjectRoot(repoRoot), "skills")
			if err := os.MkdirAll(skillsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(skillsDir, "life")
			tc.setup(t, dst)
			before, _ := os.Readlink(dst)

			report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{devinResolved("life", skill("life", "v1"), false)})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.wantDiag != "" {
				if _, ok := findDiag(report.Diags, tc.wantDiag); !ok {
					t.Fatalf("expected %s, got %v", tc.wantDiag, report.Diags)
				}
			}
			info, lerr := os.Lstat(dst)
			if lerr != nil {
				t.Fatalf("destination disappeared: %v", lerr)
			}
			isLink := info.Mode()&os.ModeSymlink != 0
			if isLink != tc.wantSymlink {
				t.Fatalf("destination symlink = %v, want %v (mode %v)", isLink, tc.wantSymlink, info.Mode())
			}
			if tc.wantDiag == "materialize.collision.user-owned" && before != "" {
				if now, _ := os.Readlink(dst); now != before {
					t.Fatalf("a user-owned symlink was repointed from %q to %q", before, now)
				}
			}
		})
	}

	t.Run("case-insensitive filesystem destination", func(t *testing.T) {
		repoRoot, _ := devinEnv(t)
		skillsDir := filepath.Join(devinProjectRoot(repoRoot), "skills")
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A handwritten directory whose name differs only in case. On a
		// case-INSENSITIVE filesystem (macOS default) this IS the destination and
		// must be preserved as a collision; on a case-sensitive one it is an
		// unrelated directory that must also be preserved. Either way, nothing
		// the user wrote may be destroyed.
		other := filepath.Join(skillsDir, "LIFE")
		if err := os.MkdirAll(other, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "SKILL.md"), []byte("hand-written"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{devinResolved("life", skill("life", "v1"), false)}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if got := readFileString(t, filepath.Join(other, "SKILL.md")); got != "hand-written" {
			t.Fatalf("a handwritten skill differing only in case was modified: %q", got)
		}
		lower, lowerErr := os.Lstat(filepath.Join(skillsDir, "life"))
		if caseInsensitive(t, skillsDir) {
			// The paths are the same inode: the handwritten directory wins and no
			// symlink may replace it.
			if lowerErr == nil && lower.Mode()&os.ModeSymlink != 0 {
				t.Fatal("on a case-insensitive filesystem a handwritten directory was replaced by a symlink")
			}
			return
		}
		if lowerErr != nil || lower.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("on a case-sensitive filesystem the managed symlink must still be created, got %v/%v", lower, lowerErr)
		}
	})
}

// cacheCompiledDir returns the compiled-artifact cache root the current test
// environment uses, so a test can build a dangling link that still looks managed.
func cacheCompiledDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_CACHE_HOME"), "agent-marketplace", "compiled")
}

// caseInsensitive reports whether dir lives on a case-insensitive filesystem.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, ".CaseProbe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Stat(filepath.Join(dir, ".caseprobe"))
	return err == nil
}

// TestDevinAggregateNamesMatchCompiledArtifactPaths pins the harness-side copies
// of the Devin aggregate names against the paths the compiler really emits, so
// the two path contracts cannot drift.
func TestDevinAggregateNamesMatchCompiledArtifactPaths(t *testing.T) {
	tests := []struct {
		name         string
		resource     ir.Resource
		machineScope bool
		wantPath     string
	}{
		{"project hook", ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}), false, harness.DevinProjectHooksFile},
		{"user hook", ir.NewHook("h", "PostToolUse", "echo", "command", nil, ir.SourceRef{}), true, harness.DevinUserConfigFile},
		{"project mcp", ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}), false, harness.DevinMcpConfigFile},
		{"user mcp", ir.NewMcpServer("m", "bin", nil, nil, nil, ir.SourceRef{}), true, harness.DevinMcpConfigFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []hcc.Option{}
			if tt.machineScope {
				opts = append(opts, hcc.WithScope(hcc.ScopeUser))
			}
			result, err := hcc.Compile(tt.resource, hcc.TargetDevin, opts...)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if len(result.Artifacts) != 1 {
				t.Fatalf("Compile produced %d artifacts, want 1", len(result.Artifacts))
			}
			got := result.Artifacts[0].Path
			if got != tt.wantPath {
				t.Fatalf("artifact path = %q, want %q", got, tt.wantPath)
			}
			if !isDevinAggregatePath(got, tt.machineScope) {
				t.Fatalf("%q is not recognized as a Devin aggregate at machine_scope=%v", got, tt.machineScope)
			}
		})
	}
}

// TestDevinCommandPrunesFromSkillsDirectory pins the prune coverage the carried
// context called out: a skills/<name>/ entry owned by a COMMAND must prune.
func TestDevinCommandPrunesFromSkillsDirectory(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	link := filepath.Join(devinProjectRoot(repoRoot), "skills", "cmd")

	resolved := []resolver.ResolvedArtifact{
		devinResolved("cmd", ir.NewCommand("cmd", "desc", "body", nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("expected a command symlink at %s: %v", link, err)
	}
	if _, err := Run(context.Background(), repoRoot, nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("a stale command skill directory was not pruned: %v", err)
	}
}

// TestDevinAggregatesAreProtectedFromMarkerStripping pins that a handwritten
// Devin aggregate is never handed to stripManagedEntriesFromFile, which decodes
// and re-marshals with encoding/json and would silently collapse duplicate keys
// and drop comments (bead agent-marketplace-5exj.18).
func TestDevinAggregatesAreProtectedFromMarkerStripping(t *testing.T) {
	for _, machineScope := range []bool{false, true} {
		protected := aggregateProtectedPaths(hcc.TargetDevin, machineScope)
		info := harness.TargetInfoMap[hcc.TargetDevin]
		for _, rel := range info.AggregateFilesForScope(machineScope) {
			if !protected[rel] {
				t.Fatalf("machine_scope=%v: %q is not protected from marker stripping", machineScope, rel)
			}
		}
	}
	// Non-Devin targets keep the marker-merge path: excluding their aggregates
	// would silently stop pruning them.
	if got := aggregateProtectedPaths(hcc.TargetFactory, true); len(got) != 0 {
		t.Fatalf("aggregateProtectedPaths(factory) = %v, want empty", got)
	}
}

// TestDevinAggregateProtectionPreservesCommentsUnderMarkerWalk proves the
// protection end-to-end: a comment-laden config.json containing the literal
// `_managed_by` string survives a sync unchanged apart from the patch.
func TestDevinAggregateProtectionPreservesCommentsUnderMarkerWalk(t *testing.T) {
	repoRoot, home := devinEnv(t)
	root := devinUserRoot(home)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	original := `{
  // a comment that encoding/json would delete
  "notes": "_managed_by is mentioned here on purpose",
  "hooks": {
    "Stop": [
      { "command": "handwritten-stop", "type": "command" }
    ]
  }
}
`
	path := filepath.Join(root, harness.DevinUserConfigFile)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		devinResolved("skill", ir.NewSkill("skill", "d", "b", nil, nil, ir.SourceRef{}), true),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := readFileString(t, path); got != original {
		t.Fatalf("a handwritten Devin config.json was rewritten.\nwant:\n%s\ngot:\n%s", original, got)
	}
}

// TestDevinSymlinkFailureIsAnErrorNotAFileWrite is the direct test of the bead's
// hard constraint: "if a symlink cannot be created, you must fail, not silently
// write a regular file."
func TestDevinSymlinkFailureIsAnErrorNotAFileWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions, so the failure cannot be provoked")
	}
	repoRoot, _ := devinEnv(t)
	skillsDir := filepath.Join(devinProjectRoot(repoRoot), "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A read-only parent makes os.Symlink fail with EACCES.
	if err := os.Chmod(skillsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) })

	report, err := Run(context.Background(), repoRoot, []resolver.ResolvedArtifact{
		devinResolved("nolink", ir.NewSkill("nolink", "d", "b", nil, nil, ir.SourceRef{}), false),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sawError := false
	for _, d := range report.Diags {
		if d.Severity == diag.SeverityError {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("a failed symlink must produce an error diagnostic, got %v", report.Diags)
	}
	if err := os.Chmod(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(skillsDir, "nolink")
	if info, lerr := os.Lstat(dst); lerr == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s was written as a regular %v after a symlink failure; there is no real-file fallback", dst, info.Mode())
		}
		t.Fatalf("%s exists as a symlink even though creating it failed", dst)
	}
}
