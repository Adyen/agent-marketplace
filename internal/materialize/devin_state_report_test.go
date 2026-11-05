package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// findDiags returns every diagnostic carrying a code.
func findDiags(diags []diag.Diagnostic, code string) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// TestDevinFirstEverSyncReportsNoStateMissingWarning pins the silent happy path.
//
// A brand-new machine has no ownership state and no pre-existing aggregate, and
// nothing bootstraps a record before the first read (Store.Open creates nothing;
// only Store.Apply commits one). The absence is therefore UNAVOIDABLE on run #1,
// which makes reporting it as a warning noise that every Devin user sees exactly
// once and then learns to ignore — including later, when the same code means a
// record really did vanish.
//
// The run must succeed completely, create state, write the entry, and say
// nothing about ownership.state-missing.
func TestDevinFirstEverSyncReportsNoStateMissingWarning(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)

	// The premise of the regression: nothing exists yet.
	if _, err := os.Lstat(mcpPath); !os.IsNotExist(err) {
		t.Fatalf("fixture is not a first-ever run: %v", err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)

	// 1. No user-visible ownership.state-missing diagnostic, at any severity.
	if found := findDiags(report.Diags, diag.CodeOwnershipStateMissing); len(found) > 0 {
		t.Fatalf("a first-ever sync reported %s: %+v", diag.CodeOwnershipStateMissing, found)
	}
	// 2. And nothing else smuggles the durable state path into the report either.
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	for _, d := range report.Diags {
		if strings.Contains(d.Message, store.StateFile()) {
			t.Fatalf("a first-ever sync reported the ownership state file: %s: %s", d.Code, d.Message)
		}
	}

	// 3. The run really did the work: the entry is written and state exists, so
	// the silence above is not "nothing happened".
	if got := readFileString(t, mcpPath); !strings.Contains(got, "managed-binary") {
		t.Fatalf("the desired entry was not written:\n%s", got)
	}
	if _, err := os.Stat(store.StateFile()); err != nil {
		t.Fatalf("the first sync did not create ownership state: %v", err)
	}
	// 4. And the next sync is proof the state is usable: still silent.
	report2, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if found := findDiags(report2.Diags, diag.CodeOwnershipStateMissing); len(found) > 0 {
		t.Fatalf("the second sync reported %s: %+v", diag.CodeOwnershipStateMissing, found)
	}
}

// TestDevinVanishedOwnershipStateStillWarns is the other half of the
// distinction: a record that was written and then went away IS worth a warning,
// and the warning must name the aggregate it concerns rather than the state file.
func TestDevinVanishedOwnershipStateStillWarns(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("drop ownership state: %v", err)
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	found := findDiags(report.Diags, diag.CodeOwnershipStateMissing)
	if len(found) != 1 {
		t.Fatalf("want exactly one %s warning after the record vanished, got %+v",
			diag.CodeOwnershipStateMissing, report.Diags)
	}
	if found[0].Severity != diag.SeverityWarning {
		t.Fatalf("severity = %v, want warning", found[0].Severity)
	}
	if !strings.Contains(found[0].Message, mcpPath) {
		t.Fatalf("the warning does not name the aggregate it concerns (%s): %s", mcpPath, found[0].Message)
	}

	// Fail-closed is unchanged by the reporting change: the entry we really did
	// write is now unmanaged, so it is preserved rather than pruned.
	if got := readFileString(t, mcpPath); !strings.Contains(got, "managed-binary") {
		t.Fatalf("an entry became unmanaged and was lost anyway:\n%s", got)
	}
}

// TestDevinAggregateDiagnosticsDistinguishTwoFilesInOneRoot pins that two
// aggregates under ONE root produce two DIFFERENT lines.
//
// Devin's user scope holds both config.json (hooks) and mcp_config.json, and the
// ownership record covers the whole root, so one vanished record produces one
// diagnostic per file. Built from the state file path they were byte-identical,
// which is what made `doctor --repair` look like it printed the same warning
// twice for no reason.
func TestDevinAggregateDiagnosticsDistinguishTwoFilesInOneRoot(t *testing.T) {
	repoRoot, home := devinEnv(t)
	root := devinUserRoot(home)

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "my-hook", ir.NewHook("my-hook", "PostToolUse", "echo hi", "command", nil, ir.SourceRef{}), true),
		devinResolved(t, "managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), true),
	}
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	configPath := filepath.Join(root, harness.DevinUserConfigFile)
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	for _, path := range []string{configPath, mcpPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("fixture needs two aggregates in one root: %v", err)
		}
	}

	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("drop ownership state: %v", err)
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	found := findDiags(report.Diags, diag.CodeOwnershipStateMissing)
	if len(found) != 2 {
		t.Fatalf("want one %s diagnostic per aggregate file, got %d: %+v",
			diag.CodeOwnershipStateMissing, len(found), found)
	}
	if found[0].Message == found[1].Message {
		t.Fatalf("two aggregates in one root produced identical diagnostics: %q", found[0].Message)
	}
	byFile := map[string]string{}
	for _, d := range found {
		byFile[d.Source.File] = d.Message
	}
	for path, rel := range map[string]string{configPath: harness.DevinUserConfigFile, mcpPath: harness.DevinMcpConfigFile} {
		message, ok := byFile[rel]
		if !ok {
			t.Fatalf("no diagnostic for %s: %+v", rel, found)
		}
		if !strings.Contains(message, path) {
			t.Fatalf("the diagnostic for %s does not name it: %s", rel, message)
		}
	}
}

// TestDevinCollisionWarningNamesTheAggregateRoot pins that a collision warning
// identifies WHICH aggregate collided. Devin has two roots and the same
// basename in both, so "mcp_config.json" alone cannot tell a user whether to go
// and look in ~/.config/devin or in <repo>/.devin.
func TestDevinCollisionWarningNamesTheAggregateRoot(t *testing.T) {
	repoRoot, home := devinEnv(t)
	for _, root := range []string{devinProjectRoot(repoRoot), devinUserRoot(home)} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
		if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"collide":{"command":"hand-written-binary"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved(t, "collide", ir.NewMcpServer("collide", "generated-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved(t, "collide", ir.NewMcpServer("collide", "generated-binary", nil, nil, nil, ir.SourceRef{}), true),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := findDiags(report.Diags, diag.CodeOwnershipUnmanagedKey)
	if len(found) != 2 {
		t.Fatalf("want one collision warning per scope, got %d: %+v", len(found), found)
	}
	for _, root := range []string{devinProjectRoot(repoRoot), devinUserRoot(home)} {
		want := filepath.Join(root, harness.DevinMcpConfigFile)
		named := false
		for _, d := range found {
			if strings.Contains(d.Message, want) {
				named = true
			}
		}
		if !named {
			t.Fatalf("no collision warning names %s: %+v", want, found)
		}
	}
}
