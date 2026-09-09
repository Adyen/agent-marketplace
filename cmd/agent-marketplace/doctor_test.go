package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/devincli"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/harness"
)

func scenarioDoctorCommandReportsDetectedHarnessAndInstallRoots(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)

	repoRoot := t.TempDir()
	canonicalRepo, _ := ownership.CanonicalRoot(repoRoot)
	canonicalHome, _ := ownership.CanonicalRoot(homeDir)
	canonicalCodexHome, _ := ownership.CanonicalRoot(codexHome)
	writeClaudePluginSkill(t, repoRoot, "doctor-skill")
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("expected doctor command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"agent-marketplace doctor",
		"detected harness: claude",
		"diagnostic codes: harness-compilation/diag/codes.md",
		filepath.Join(repoRoot, ".factory"),
		filepath.Join(homeDir, ".factory"),
		// Devin's canonical roots and its bidirectional capability set are
		// reported like every other target's: "Help text, status, list badges,
		// conversion allowlists, and target displays include Devin".
		filepath.Join(repoRoot, ".devin"),
		filepath.Join(homeDir, ".config", "devin"),
		"devin repo=",
		"Codex repo=repo-agents=" + filepath.Join(canonicalRepo, ".agents") + ", repo-codex=" + filepath.Join(canonicalRepo, ".codex"),
		"machine=machine-agents=" + filepath.Join(canonicalHome, ".agents") + ", machine-codex=" + canonicalCodexHome,
		"capabilities=compile,ingest,convert,tui",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected doctor output to contain %q, got:\n%s", needle, output)
		}
	}
}

// devinDoctorEnv isolates HOME, the caches, and the ownership state root, then
// points the process at an empty repo.
func devinDoctorEnv(t *testing.T) (repoRoot, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	repoRoot = t.TempDir()
	withWorkingDir(t, repoRoot)
	return repoRoot, home
}

func staticProbe(probe devincli.Probe) func(context.Context) devincli.Probe {
	return func(context.Context) devincli.Probe { return probe }
}

// staticResolve pins the selection a repair measures "exact current desired
// matches" against, so the contract is tested against a known desired set
// rather than against whatever a cloned registry happens to provide.
func staticResolve(selection []resolver.ResolvedArtifact) func(context.Context, string) []resolver.ResolvedArtifact {
	return func(context.Context, string) []resolver.ResolvedArtifact { return selection }
}

// TestDoctorDevinVersionAndRecommendationLines pins the plan's detection
// contract exactly: "`doctor` detects `devin`, prints the installed version,
// reports compatibility with 3000.3+, and recommends selecting the harness when
// installed but unconfigured", and "warns when its version is below 3000.3"
// WITHOUT blocking anything (every case exits 0).
func scenarioDoctorDevinVersionAndRecommendationLines(t *testing.T) {
	supported := devincli.Probe{
		Status:  devincli.StatusSupported,
		Path:    "/opt/devin/bin/devin",
		Version: devincli.Version{Major: 3000, Minor: 3, Patch: 27},
	}
	unsupported := devincli.Probe{
		Status:  devincli.StatusUnsupported,
		Path:    "/opt/devin/bin/devin",
		Version: devincli.Version{Major: 3000, Minor: 2, Patch: 17},
	}
	unknown := devincli.Probe{
		Status: devincli.StatusUnknown,
		Path:   "/opt/devin/bin/devin",
		Err:    errors.New("exit status 1"),
	}

	tests := []struct {
		name           string
		probe          devincli.Probe
		harnesses      string
		wantLines      []string
		wantNotContain []string
	}{
		{
			name:      "not installed",
			probe:     devincli.Probe{Status: devincli.StatusNotInstalled},
			wantLines: []string{"devin cli: not installed (sync does not require it)", "devin harness: not selected"},
			// Nothing is recommended when nothing is installed.
			wantNotContain: []string{"doctor.devin.recommended"},
		},
		{
			name:  "installed at the floor and unconfigured is recommended",
			probe: supported,
			wantLines: []string{
				"devin cli: 3000.3.27 at /opt/devin/bin/devin (compatible with 3000.3+)",
				"devin harness: not selected — a Devin CLI is installed; add \"devin\" to a harnesses list (TUI: press H) [doctor.devin.recommended]",
			},
		},
		{
			name:  "installed below the floor warns and does not block",
			probe: unsupported,
			wantLines: []string{
				"devin cli: 3000.2.17 at /opt/devin/bin/devin (warning: below the 3000.3 compatibility floor; sync is not blocked) [doctor.devin.version-unsupported]",
			},
		},
		{
			name:  "installed with an unknown version warns and does not block",
			probe: unknown,
			wantLines: []string{
				"devin cli: version unknown at /opt/devin/bin/devin (warning: 3000.3 compatibility could not be confirmed; sync is not blocked) [doctor.devin.version-unknown]",
			},
		},
		{
			name:      "already selected is not recommended again",
			probe:     supported,
			harnesses: "apiVersion: agent-marketplace/v2\nharnesses:\n  - factory\n  - devin\nregistries: []\n",
			wantLines: []string{"devin harness: selected (global)"},
			wantNotContain: []string{
				"doctor.devin.recommended",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, home := devinDoctorEnv(t)
			if tc.harnesses != "" {
				path := filepath.Join(home, ".agents", "config.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.harnesses), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			var out bytes.Buffer
			if err := runDoctor(doctorOptions{out: &out, probe: staticProbe(tc.probe)}); err != nil {
				t.Fatalf("doctor must never fail because of a version probe: %v\n%s", err, out.String())
			}
			for _, want := range tc.wantLines {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("doctor output missing line %q, got:\n%s", want, out.String())
				}
			}
			for _, unwanted := range tc.wantNotContain {
				if strings.Contains(out.String(), unwanted) {
					t.Fatalf("doctor output unexpectedly contains %q, got:\n%s", unwanted, out.String())
				}
			}
		})
	}
}

// prepareDevinRepairFixture syncs one Devin MCP server and then drops the
// durable ownership record, which is the state `doctor --repair` exists for:
// "Missing or corrupt ownership state makes all existing aggregate entries
// unmanaged".
func prepareDevinRepairFixture(t *testing.T, repoRoot string) (mcpPath string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	selection = []resolver.ResolvedArtifact{{
		Entry:     catalog.Entry{Name: "managed", Resource: ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{})},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetDevin},
	}}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	outDir := filepath.Join(repoRoot, ".devin")
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("drop ownership state: %v", err)
	}
	return filepath.Join(outDir, harness.DevinMcpConfigFile), selection
}

// containsPath reports whether text names path, tolerating the /var →
// /private/var symlink macOS temp directories sit behind: the command resolves
// its repo root, while a test fixture writes through the unresolved spelling.
func containsPath(text, path string) bool {
	if strings.Contains(text, path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && strings.Contains(text, resolved)
}

func devinRepairPreview(t *testing.T, repoRoot string, selection []resolver.ResolvedArtifact) *materialize.RepairPreview {
	t.Helper()
	preview, err := materialize.DevinRepairPreview(context.Background(), repoRoot, selection)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	return preview
}

// TestDoctorRepairOffTTYIsPreviewOnly pins "off-TTY, `doctor --repair` is
// preview-only" and "exit 0 means clean, repaired, or declined without
// operational failure".
func scenarioDoctorRepairOffTTYIsPreviewOnly(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	// Sanity: the fixture really does present something repairable, so the
	// preview-only assertion below is not vacuous.
	if !devinRepairPreview(t, repoRoot, selection).Actionable() {
		t.Fatal("fixture produced no repair candidates")
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
		t.Fatalf("off-TTY repair must exit 0: %v\n%s", err, out.String())
	}

	for _, want := range []string{
		"repair: devin shared-aggregate ownership",
		"adopt mcp-server \"managed\" (exact match for resource \"managed\")",
		"preview only: not a terminal; re-run on a terminal to confirm, or use --repair --yes to adopt exact matches",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("off-TTY repair output missing %q, got:\n%s", want, out.String())
		}
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("off-TTY repair mutated the aggregate:\n%s", after)
	}
	// The decisive proof: nothing was adopted, so the candidate is still there.
	if !devinRepairPreview(t, repoRoot, selection).Actionable() {
		t.Fatal("off-TTY repair adopted something; it must be preview-only")
	}
}

// TestDoctorRepairYesAdoptsWithoutPrompting pins "`doctor --repair --yes`
// adopts exact current desired matches without prompting".
func scenarioDoctorRepairYesAdoptsWithoutPrompting(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, selection := prepareDevinRepairFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("--repair --yes must exit 0 on success: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "adopted 1 entr(y/ies) in mcp_config.json") {
		t.Fatalf("--yes did not report the adoption:\n%s", out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("--yes must not prompt:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("adoption changed the aggregate bytes:\n%s", after)
	}
	if devinRepairPreview(t, repoRoot, selection).Actionable() {
		t.Fatal("the entry is still a candidate; --yes did not record ownership")
	}
}

// TestDoctorRepairYesNeverRemovesOrphans pins the asymmetry the plan requires:
// --yes covers adoption, but "orphan-removal candidates require ... explicit
// per-entry confirmation", which --yes cannot supply.
func scenarioDoctorRepairYesNeverRemovesOrphans(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	mcpPath, _ := prepareDevinRepairFixture(t, repoRoot)
	before, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is selected any more, so the entry is undesired and unproven but
	// still evidence-backed: an orphan candidate.
	if preview := devinRepairPreview(t, repoRoot, nil); len(preview.Files) == 0 || len(preview.Files[0].Orphans) != 1 {
		t.Fatalf("fixture did not produce exactly one orphan candidate: %#v", preview.Files)
	}

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader("y\ny\ny\n"),
		isTTY:   false,
		repair:  true,
		yes:     true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(nil),
	}); err != nil {
		t.Fatalf("--repair --yes must exit 0: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "left unchanged: removal requires an interactive per-entry confirmation") {
		t.Fatalf("--yes did not report the orphan as left alone:\n%s", out.String())
	}
	after, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("--yes removed an orphan entry:\n%s", after)
	}
}

// TestDoctorRepairOnTTYPromptsPerAction pins "on a TTY it then prompts for each
// authorized action" and that a decline is a clean exit 0 that changes nothing.
func scenarioDoctorRepairOnTTYPromptsPerAction(t *testing.T) {
	tests := []struct {
		name        string
		answers     string
		wantRemoved bool
		wantOut     []string
	}{
		{
			name:        "declining leaves the entry alone",
			answers:     "n\n",
			wantRemoved: false,
			wantOut: []string{
				"remove mcp-server \"managed\" from mcp_config.json? this deletes the entry [y/N]",
				"declined: mcp-server \"managed\" in mcp_config.json left unchanged",
			},
		},
		{
			name:        "an empty answer is a decline",
			answers:     "\n",
			wantRemoved: false,
			wantOut:     []string{"declined: mcp-server \"managed\" in mcp_config.json left unchanged"},
		},
		{
			name:        "confirming removes exactly that entry",
			answers:     "y\n",
			wantRemoved: true,
			wantOut:     []string{"removed mcp-server \"managed\" from mcp_config.json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, _ := devinDoctorEnv(t)
			mcpPath, _ := prepareDevinRepairFixture(t, repoRoot)

			var out bytes.Buffer
			if err := runDoctor(doctorOptions{
				out:     &out,
				in:      strings.NewReader(tc.answers),
				isTTY:   true,
				repair:  true,
				probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
				resolve: staticResolve(nil),
			}); err != nil {
				t.Fatalf("repair must exit 0 when repaired or declined: %v\n%s", err, out.String())
			}
			for _, want := range tc.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("repair output missing %q, got:\n%s", want, out.String())
				}
			}
			body, err := os.ReadFile(mcpPath)
			if err != nil {
				t.Fatal(err)
			}
			if removed := !strings.Contains(string(body), "managed-binary"); removed != tc.wantRemoved {
				t.Fatalf("entry removed = %t, want %t; file:\n%s", removed, tc.wantRemoved, body)
			}
		})
	}
}

// TestDoctorRepairExitsOneOnCorruptAggregate pins "exit 1 means corrupt
// state/file or partial repair failure", and that the file is left as found.
func scenarioDoctorRepairExitsOneOnCorruptAggregate(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	outDir := filepath.Join(repoRoot, ".devin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := "{ \"mcpServers\": { \"a\": }\n"
	mcpPath := filepath.Join(outDir, harness.DevinMcpConfigFile)
	if err := os.WriteFile(mcpPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runDoctor(doctorOptions{
		out:    &out,
		isTTY:  false,
		repair: true,
		probe:  staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
	})
	if err == nil {
		t.Fatalf("a corrupt aggregate must exit 1, got success:\n%s", out.String())
	}
	// The diagnostic names the AGGREGATE — root and file — not just the
	// basename, so a user with a .devin and a ~/.config/devin knows which one to
	// go and fix. Severity, aggregate, parse location and stable code must all
	// be on ONE line: satisfying them from three different lines would let the
	// path come from the preview listing while the diagnostic itself stayed
	// anonymous.
	var reported string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "$.mcpServers.a: expected a JSON value [json.malformed]") {
			reported = line
		}
	}
	if reported == "" {
		t.Fatalf("repair output does not report the malformed aggregate with its stable code:\n%s", out.String())
	}
	if !strings.HasPrefix(reported, "  error: ") {
		t.Fatalf("a malformed aggregate must be reported at error severity, got %q", reported)
	}
	if !containsPath(reported, mcpPath) {
		t.Fatalf("repair output does not name the malformed aggregate %s:\n%s", mcpPath, out.String())
	}
	if got, readErr := os.ReadFile(mcpPath); readErr != nil || string(got) != broken {
		t.Fatalf("repair modified a malformed aggregate: %v %q", readErr, got)
	}
}

// TestDoctorRepairReportsNothingToRepair pins the clean path: no candidates,
// exit 0, and an explicit statement rather than silence.
func scenarioDoctorRepairReportsNothingToRepair(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	// A synced-and-still-owned root has no adoption matches (everything is
	// already proven ours) and no orphans (nothing is undesired).
	selection := prepareDevinRepairFixtureOwned(t, repoRoot)

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
}

func prepareDevinRepairFixtureOwned(t *testing.T, repoRoot string) []resolver.ResolvedArtifact {
	t.Helper()
	selection := []resolver.ResolvedArtifact{devinMcpArtifact("managed", "managed-binary")}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	return selection
}

// TestWriteRepairDiagnosticsPrintsEachLineOnce pins the dedup. A beta tester's
// `doctor --repair` printed one ownership warning twice for one root, which
// reads as two problems. A line identical to one already printed adds nothing,
// so it is printed once; a line that differs is a different fact and still
// prints.
func scenarioWriteRepairDiagnosticsPrintsEachLineOnce(t *testing.T) {
	duplicate := diag.Diagnostic{
		Code:     diag.CodeOwnershipStateMissing,
		Severity: diag.SeverityWarning,
		Source:   diag.SourceRef{File: harness.DevinMcpConfigFile},
		Message:  "/root/.config/devin/mcp_config.json: ownership: no durable ownership record",
	}
	other := duplicate
	other.Source = diag.SourceRef{File: harness.DevinUserConfigFile}
	other.Message = "/root/.config/devin/config.json: ownership: no durable ownership record"
	suppressed := diag.Diagnostic{
		Code:     diag.CodeOwnershipStateMissing,
		Severity: diag.SeverityInfo,
		Message:  "info noise",
	}

	var out bytes.Buffer
	preview := &materialize.RepairPreview{Diags: []diag.Diagnostic{duplicate, duplicate, other, suppressed}}
	if err := writeRepairDiagnostics(&out, preview); err != nil {
		t.Fatalf("writeRepairDiagnostics: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want exactly 2 printed lines (one per distinct diagnostic), got %d:\n%s", len(lines), out.String())
	}
	seen := map[string]int{}
	for _, line := range lines {
		seen[line]++
	}
	for line, count := range seen {
		if count != 1 {
			t.Fatalf("line %q printed %d times", line, count)
		}
	}
	for _, want := range []string{"mcp_config.json", "config.json"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dedup dropped the diagnostic for %s:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "info noise") {
		t.Fatalf("info-level preview noise was printed:\n%s", out.String())
	}
}

// TestWriteRepairDiagnosticsKeepsTwoResourcesWithOneMessage pins the limit of the
// dedup. The rendered line drops Resource, and some catalogued messages are
// constant — rule.devin.unsupported says the same sentence for every rule and
// distinguishes them only by Resource. Keying the dedup on the line alone would
// silently turn "two rules were dropped" into "a rule was dropped".
func scenarioWriteRepairDiagnosticsKeepsTwoResourcesWithOneMessage(t *testing.T) {
	shared := diag.Diagnostic{
		Code:     diag.CodeRuleDevinUnsupported,
		Severity: diag.SeverityWarning,
		Message:  "Rules are out of scope for the Devin target; no rule artifact was emitted",
	}
	first, second := shared, shared
	first.Resource = "first-rule"
	second.Resource = "second-rule"

	var out bytes.Buffer
	preview := &materialize.RepairPreview{Diags: []diag.Diagnostic{first, second, second}}
	if err := writeRepairDiagnostics(&out, preview); err != nil {
		t.Fatalf("writeRepairDiagnostics: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one line per affected resource (and the exact repeat dropped), got %d:\n%s",
			len(lines), out.String())
	}
}

// prepareDevinTwoAggregateRepairFixture syncs a hook AND an MCP server into one
// Devin root, then drops the durable record. One vanished record legitimately
// concerns both aggregates, which is the situation that used to print the same
// line twice.
func prepareDevinTwoAggregateRepairFixture(t *testing.T, repoRoot string) (outDir string, selection []resolver.ResolvedArtifact) {
	t.Helper()
	selection = []resolver.ResolvedArtifact{
		{
			Entry:     catalog.Entry{Name: "my-hook", Resource: ir.NewHook("my-hook", "PostToolUse", "echo hi", "command", nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetDevin},
		},
		{
			Entry:     catalog.Entry{Name: "managed", Resource: ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{})},
			Source:    config.RepoCommitted,
			Harnesses: []hcc.Target{hcc.TargetDevin},
		},
	}
	if _, err := materialize.Run(context.Background(), repoRoot, selection); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	outDir = filepath.Join(repoRoot, ".devin")
	for _, rel := range []string{harness.DevinProjectHooksFile, harness.DevinMcpConfigFile} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Fatalf("fixture needs two aggregates in one root: %v", err)
		}
	}
	store, err := ownership.Open(outDir)
	if err != nil {
		t.Fatalf("open ownership store: %v", err)
	}
	if err := os.Remove(store.StateFile()); err != nil {
		t.Fatalf("drop ownership state: %v", err)
	}
	return outDir, selection
}

// TestDoctorRepairDistinguishesTwoAggregatesInOneRoot pins both halves of the
// second defect: each aggregate's diagnostic names that aggregate, and no line
// is printed twice.
func scenarioDoctorRepairDistinguishesTwoAggregatesInOneRoot(t *testing.T) {
	repoRoot, _ := devinDoctorEnv(t)
	outDir, selection := prepareDevinTwoAggregateRepairFixture(t, repoRoot)

	var out bytes.Buffer
	if err := runDoctor(doctorOptions{
		out:     &out,
		in:      strings.NewReader(""),
		isTTY:   false,
		repair:  true,
		probe:   staticProbe(devincli.Probe{Status: devincli.StatusNotInstalled}),
		resolve: staticResolve(selection),
	}); err != nil {
		t.Fatalf("a missing record is a warning, so repair must exit 0: %v\n%s", err, out.String())
	}

	var stateLines []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, diag.CodeOwnershipStateMissing) {
			stateLines = append(stateLines, line)
		}
	}
	if len(stateLines) != 2 {
		t.Fatalf("want one %s line per aggregate, got %d:\n%s", diag.CodeOwnershipStateMissing, len(stateLines), out.String())
	}
	if stateLines[0] == stateLines[1] {
		t.Fatalf("the two aggregates printed identical lines: %q", stateLines[0])
	}
	for _, rel := range []string{harness.DevinProjectHooksFile, harness.DevinMcpConfigFile} {
		want := filepath.Join(outDir, rel)
		found := false
		for _, line := range stateLines {
			if containsPath(line, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("no line names %s:\n%s", want, out.String())
		}
	}
}

// TestDoctorYesRequiresRepair pins that --yes cannot silently do nothing: it is
// only meaningful for the repair contract.
func scenarioDoctorYesRequiresRepair(t *testing.T) {
	devinDoctorEnv(t)
	output, err := executeCommand("doctor", "--yes")
	if err == nil {
		t.Fatalf("doctor --yes without --repair must fail, got:\n%s", output)
	}
	if !strings.Contains(err.Error(), "--yes only applies to --repair") {
		t.Fatalf("unexpected error: %v", err)
	}
}
func scenarioDoctorReportsManagedInstallationDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission diagnostics are Unix-specific")
	}

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries: []
`)
	if err := os.Chmod(orgPath, 0o664); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	config.SetTestV3OrganizationConfigPath(t, orgPath)

	repoRoot := t.TempDir()
	writeClaudePluginSkill(t, repoRoot, "doctor-managed-skill")
	writeFile(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("doctor")
	if err != nil {
		t.Fatalf("expected doctor command to succeed: %v\n%s", err, output)
	}

	for _, needle := range []string{
		"org managed: false",
		"MANAGED INSTALLATION",
		"state: unmanaged",
		"validation: untrusted",
		"organization.trust.config.writable",
		"is writable by group or other (mode",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected doctor output to contain %q, got:\n%s", needle, output)
		}
	}

}
