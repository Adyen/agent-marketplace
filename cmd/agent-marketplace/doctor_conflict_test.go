package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// conflictSecret is a recognizable non-credential stand-in for MCP entry
// content. No repair output may ever contain it.
const conflictSecret = "NOT-A-REAL-SECRET-c0ffee"

// prepareDevinConflictFixture reproduces the state a beta tester could not get
// out of: a hand-written mcp_config.json entry whose KEY equals a selected MCP
// server's key but whose VALUE differs, with no ownership state for the root.
// Sync reports a collision and installs nothing; adoption and orphan removal
// both decline to describe it.
func prepareDevinConflictFixture(t *testing.T, repoRoot string) (mcpPath string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	outDir := filepath.Join(repoRoot, ".devin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath = filepath.Join(outDir, harness.DevinMcpConfigFile)
	body := "{\n  \"mcpServers\": {\n    \"genai-atlassian\": { \"command\": \"/usr/local/bin/atlassian-mcp\", \"env\": { \"TOKEN\": \"" +
		conflictSecret + "\" } }\n  }\n}\n"
	if err := os.WriteFile(mcpPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	selection = []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{Name: "genai-atlassian",
			Resource: ir.NewMcpServer("genai-atlassian", "generated-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	return mcpPath, selection
}

// TestDoctorRepairPreviewsValueConflictWithoutLeakingValues pins the preview
// surface for the third repair class: it says THAT the values differ, names
// enough identity to act (kind, key, aggregate path, resource), carries digests,
// and carries no part of either value.
func scenarioDoctorRepairPreviewsValueConflictWithoutLeakingValues(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinConflictFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\ny\ny\n"),
		isTTY:   false,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("a value conflict is repairable, not broken: %v\n%s", err, out.String())
	}

	for _, want := range []string{
		"repair: devin shared-aggregate ownership",
		"replace mcp-server \"genai-atlassian\" (resource \"genai-atlassian\"): the existing entry has the same key but a different value",
		"existing value digest sha256:",
		"generated value digest sha256:",
		diag.CodeDoctorRepairConflictReplaceable,
		"replacing DELETES the existing value; it needs an interactive per-entry confirmation and is never done by --yes",
		"preview only: not a terminal",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("repair preview missing %q, got:\n%s", want, out.String())
		}
	}
	// Nothing about the VALUES: not the user's env value, not either command.
	for _, forbidden := range []string{
		conflictSecret, "/usr/local/bin/atlassian-mcp", "generated-binary", "TOKEN",
	} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("repair preview leaked %q:\n%s", forbidden, out.String())
		}
	}
	if after, err := os.ReadFile(mcpPath); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("an off-TTY preview mutated the aggregate: %v\n%s", err, after)
	}
}

// TestDoctorRepairNeverReplacesUnderYes pins the asymmetry with adoption:
// adoption is byte-neutral and batchable, replacing a user's entry is not. --yes
// reports the conflict and leaves it alone.
func scenarioDoctorRepairNeverReplacesUnderYes(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinConflictFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\ny\ny\n"),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("--repair --yes must exit 0: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "left unchanged: replacing it deletes user-written content and requires an interactive per-entry confirmation") {
		t.Fatalf("--yes did not report the conflict as left alone:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("--yes replaced a conflicting entry:\n%s", after)
	}
	if !strings.Contains(string(after), conflictSecret) {
		t.Fatalf("--yes destroyed the user's value:\n%s", after)
	}
}

// TestDoctorRepairYesOnATTYStillPromptsBeforeReplacing pins the combination the
// off-TTY test above cannot reach: --yes AND a terminal.
//
// "--yes never replaces" has to hold there too, and it holds for a different
// reason: the conflict loop does not consult opts.yes at all, so --yes on a
// terminal still asks. A regression that made --yes batch conflicts the way it
// batches adoptions would pass every other test in this file.
func scenarioDoctorRepairYesOnATTYStillPromptsBeforeReplacing(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinConflictFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("n\n"),
		isTTY:   true,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("--repair --yes on a terminal must exit 0: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "with the generated entry? this deletes the existing value [y/N]") {
		t.Fatalf("--yes on a terminal skipped the per-entry confirmation:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "declined: mcp-server \"genai-atlassian\" in mcp_config.json left unchanged (the existing value is kept)") {
		t.Fatalf("--yes on a terminal did not honour the decline:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("--repair --yes replaced a conflicting entry on a terminal:\n%s", after)
	}
}

// TestDoctorRepairConflictPromptsPerEntry pins the interactive contract and the
// full loop: on a TTY the user is asked per entry; declining leaves the file
// byte-identical; confirming replaces the entry, and the next sync is clean.
func scenarioDoctorRepairConflictPromptsPerEntry(t *testing.T) {
	tests := []struct {
		name         string
		answers      string
		wantReplaced bool
		wantOut      []string
	}{
		{
			name:         "declining leaves the entry byte-identical",
			answers:      "n\n",
			wantReplaced: false,
			wantOut: []string{
				"replace mcp-server \"genai-atlassian\" in mcp_config.json with the generated entry? this deletes the existing value [y/N]",
				"declined: mcp-server \"genai-atlassian\" in mcp_config.json left unchanged (the existing value is kept)",
			},
		},
		{
			name:         "an empty answer is a decline",
			answers:      "\n",
			wantReplaced: false,
			wantOut:      []string{"declined: mcp-server \"genai-atlassian\" in mcp_config.json left unchanged (the existing value is kept)"},
		},
		{
			name:         "confirming replaces exactly that entry",
			answers:      "y\n",
			wantReplaced: true,
			wantOut:      []string{"replaced mcp-server \"genai-atlassian\" in mcp_config.json with the generated entry"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			mcpPath, selection := prepareDevinConflictFixture(t, repoRoot)
			before, err := os.ReadFile(mcpPath)
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			if err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader(tc.answers),
				isTTY:   true,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(selection),
			}); err != nil {
				t.Fatalf("repair must exit 0 when repaired or declined: %v\n%s", err, out.String())
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("repair output missing %q, got:\n%s", want, out.String())
				}
			}
			after, err := os.ReadFile(mcpPath)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantReplaced {
				if !bytes.Equal(before, after) {
					t.Fatalf("a declined replacement changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
				}
				return
			}
			if strings.Contains(string(after), conflictSecret) {
				t.Fatalf("the confirmed replacement kept the old value:\n%s", after)
			}
			if !strings.Contains(string(after), "generated-binary") {
				t.Fatalf("the confirmed replacement did not install the generated entry:\n%s", after)
			}
			// The loop is open: sync no longer collides and repair is clean.
			report, err := materialize.Run(context.Background(), repoRoot, selection)
			if err != nil {
				t.Fatalf("re-sync: %v", err)
			}
			for _, d := range report.Diags {
				if d.Code == diag.CodeOwnershipUnmanagedKey {
					t.Fatalf("the collision survived the replacement: %s", d.Message)
				}
			}
			if devinRepairPreview(t, repoRoot, selection).Actionable() {
				t.Fatal("the replaced entry is still nominated after a clean re-sync")
			}
		})
	}
}

// TestDoctorRepairReportsInspectedFilesWithNoCandidates pins the second half of
// the dead end: "nothing to repair" has to be a statement about known files.
// A beta tester who saw it could not tell whether the tool had even looked at
// the aggregate they were staring at.
func scenarioDoctorRepairReportsInspectedFilesWithNoCandidates(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	selection := []resolver.ResolvedArtifact{devinMcpArtifact("managed", "managed-binary")}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	mcpPath := filepath.Join(repoRoot, ".devin", harness.DevinMcpConfigFile)

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		isTTY:   true,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("a clean root must exit 0: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "nothing to repair") {
		t.Fatalf("repair output does not report a clean root:\n%s", out.String())
	}
	if !containsPath(out.String(), mcpPath) {
		t.Fatalf("repair output does not name the inspected aggregate %s:\n%s", mcpPath, out.String())
	}
	if !strings.Contains(out.String(), "no repair candidates") {
		t.Fatalf("repair output does not say the inspected file produced no candidates:\n%s", out.String())
	}
}
