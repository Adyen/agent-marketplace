package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// mcpTransportLeaks are the value classes a diagnostic, hint or log attribute
// may never carry (pkg/harness-compilation/diag/codes.go): MCP urls, transport
// values and environment values. The error fixture below hides a planted
// credential-shaped marker in both an `env` value and the server url so the
// sync output can be searched for them. The marker style matches
// pkg/harness-compilation/compile/devin/mcp_test.go's
// TestMcpDiagnosticsNeverQuoteValues, which pins the same policy one layer down.
var mcpTransportLeaks = []string{"NOT-A-REAL", "c0ffee", "invalid/mcp", "websocket"}

// writeUndocumentedTransportMCP writes a `.mcp.json` holding one remote MCP
// server per name, each requesting a transport outside Devin's documented
// http/sse vocabulary, plus a credential-shaped marker in its url and in an env
// value. The Devin compiler skips each entry with mcp.devin.transport-unsupported.
//
// WHY THIS CODE. The test needs a diagnostic that is still SeverityError after
// bead agent-marketplace-pu6b, which moved the scoped allowed-tools codes
// (skill.devin.tool-unknown and its subagent sibling) from error to warning.
// pu6b explicitly did NOT reverse the eight hook.devin.*/mcp.devin.* codes that
// REMOVE a desired aggregate entry, and mcp.devin.transport-unsupported is one
// of them: dropping the transport instead would silently default it. That
// severity is additionally pinned, independently of this file, by
// TestSyncExitCodeForSkippedDevinAggregateEntries in sync_devin_exit_test.go,
// so a future downgrade fails loudly there first rather than quietly turning
// this test vacuous.
func writeUndocumentedTransportMCP(t *testing.T, repoDir string, names ...string) {
	t.Helper()
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, `    "`+name+`": {
      "url": "https://NOT-A-REAL-HOST-`+name+`.invalid/mcp",
      "transport": "websocket",
      "env": {"API_TOKEN": "NOT-A-REAL-ENV-`+name+`-c0ffee"}
    }`)
	}
	writeFile(t, filepath.Join(repoDir, ".mcp.json"), `{
  "mcpServers": {
`+strings.Join(entries, ",\n")+`
  }
}
`)
}

// TestSyncNamesEachFailingResourceOnTheErrorPath reproduces the beta tester's
// report from bead agent-marketplace-oc2r end to end, on the ERROR path. Three
// failing resources used to print three byte-identical lines with nothing to
// tell them apart:
//
//	Error: mcp.devin.transport-unsupported: the MCP server requests a transport Devin does not document; the server entry was skipped
//	mcp.devin.transport-unsupported: the MCP server requests a transport Devin does not document; the server entry was skipped
//	mcp.devin.transport-unsupported: the MCP server requests a transport Devin does not document; the server entry was skipped
//
// Each line must now name the resource and its source file, the summary must
// count the distinct failures, and the compiler's hint must be rendered exactly
// once because on the error path it is DETACHED from the lines it belongs to
// (cmd/agent-marketplace/sync.go's printRemediations).
//
// The original fixture was three skills carrying a scoped allowed-tools
// specifier. Bead agent-marketplace-pu6b made skill.devin.tool-unknown a
// WARNING that installs the artifact, so that fixture no longer reaches the
// error path at all and the test passed vacuously; it was re-fixtured onto
// mcp.devin.transport-unsupported, which pu6b deliberately left an error (see
// writeUndocumentedTransportMCP). Do not point this test back at scoped
// allowed-tools.
func TestSyncNamesEachFailingResourceOnTheErrorPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "bad-transport.git")
	names := []string{"alpha", "beta", "gamma"}
	writeUndocumentedTransportMCP(t, working, names...)
	writeSkill(t, working, "keeper", "Compiles fine", nil)
	commitAndPushAll(t, working, "Add MCP servers with an undocumented transport")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - devin
registries:
  - url: `+origin+`
    ref: main
    skills:
      - keeper
    mcp:
      - alpha
      - beta
      - gamma
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err == nil {
		t.Fatalf("sync exited 0 despite three skipped MCP entries; output:\n%s", output)
	}

	combined := output + "\n" + err.Error()

	// The three lines are distinguishable: no two of them are byte-identical.
	seen := make(map[string]int)
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "mcp.devin.transport-unsupported") {
			continue
		}
		seen[line]++
	}
	if len(seen) != len(names) {
		t.Fatalf("expected %d distinct transport-unsupported lines, got %d: %#v", len(names), len(seen), seen)
	}
	for line, count := range seen {
		if count != 1 {
			t.Errorf("line %q printed %d times; identical failures from one resource must collapse", line, count)
		}
	}

	// Every error line names ITS resource, which is what makes the three
	// otherwise identical failures tellable apart.
	for _, name := range names {
		want := `resource "` + name + `"`
		named := 0
		for line := range seen {
			if strings.Contains(line, want) {
				named++
			}
		}
		if named != 1 {
			t.Errorf("%d error lines name %s, want exactly 1; output:\n%s\nerror: %v", named, want, output, err)
		}
	}

	// The source file IS named, on the error line itself rather than merely
	// somewhere in the output. Unlike the previous skill fixture, where each
	// resource had its own skills/<name>/SKILL.md, all three MCP servers are
	// defined in ONE `.mcp.json`, so a per-name DISTINCT source path is not a
	// meaningful assertion here — the shared path is the truth, and it is
	// asserted on all three lines instead. That differing source paths keep two
	// diagnostics with the same code and resource apart is pinned one layer
	// down by internal/pipeline's diagIssue dedup-key tests.
	for line := range seen {
		if !strings.Contains(line, ".mcp.json") {
			t.Errorf("error line %q does not name its source file %q", line, ".mcp.json")
		}
	}

	if !strings.Contains(output, "Errors 3") {
		t.Errorf("sync summary does not report three distinct errors; output:\n%s", output)
	}
	// The compiler already authored remediation; it must reach the user. The
	// three failures share one hint and on the error path the hint is printed
	// DETACHED from the error lines, so identical advice is rendered exactly
	// once — repeating it per resource would add no attribution, while the
	// three error lines above already name every failing resource. This is the
	// property the whole test exists for.
	if got := strings.Count(output, "Devin documents the http and sse remote transports"); got != 1 {
		t.Errorf("compiler hint rendered %d times, want exactly 1; output:\n%s", got, output)
	}

	for _, forbidden := range mcpTransportLeaks {
		if strings.Contains(combined, forbidden) {
			t.Errorf("sync output leaks %q, which the diagnostic policy forbids; output:\n%s\nerror: %v", forbidden, output, err)
		}
	}

	// Suppression stays per resource: the healthy sibling still materialized.
	assertFileExists(t, filepath.Join(repoRoot, ".devin", "skills", "keeper", "SKILL.md"))
}

// TestSyncCollapsesIdenticalWarningDiagnosticsFromOneResource is the other half of the
// dedup contract, exercised on the WARNING path. One skill with two scoped
// specifiers yields two byte-identical diagnostics; they describe one resource,
// so the user sees one line. The property is severity-agnostic — cmd's
// dedupIssues is the one helper both the warning loop and the error path call —
// and a single MCP server cannot emit two identical diagnostics, so the
// scoped-tools fixture is still the right shape for it.
//
// Bead agent-marketplace-pu6b moved skill.devin.tool-unknown from an error that
// suppressed the artifact to a WARNING that omits the `allowed-tools` key and
// installs the artifact anyway, so this test now asserts a zero exit and an
// installed skill. Do not "restore" the old error expectations: the error-path
// output shape lives in TestSyncNamesEachFailingResourceOnTheErrorPath.
func TestSyncCollapsesIdenticalWarningDiagnosticsFromOneResource(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "scoped-tools-twice.git")
	writeFile(t, filepath.Join(working, "skills", "alpha", "SKILL.md"),
		"---\nname: alpha\ndescription: alpha skill\nallowed-tools:\n  - Bash(git push --force:*)\n  - Bash(git commit:*)\n---\n\n# alpha\n")
	commitAndPushAll(t, working, "Add a skill with two scoped specifiers")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - devin
registries:
  - url: `+origin+`
    ref: main
    skills:
      - alpha
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("sync")
	if err != nil {
		t.Fatalf("sync exited non-zero for a warning-severity diagnostic: %v\noutput:\n%s", err, output)
	}

	lines := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "skill.devin.tool-unknown") {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("two identical diagnostics from one resource printed %d lines, want 1; output:\n%s", lines, output)
	}
	// `Errors 1` is gone with the severity: the summary now reports no errors
	// and the collapse is asserted on the printed warning lines above. That
	// the summary count equals the number of ERROR lines the user is about to
	// read is covered by TestSyncNamesEachFailingResourceOnTheErrorPath.
	if !strings.Contains(output, "Errors 0") {
		t.Errorf("sync summary reports errors for a warning-only run; output:\n%s", output)
	}
	// On the WARNING path the hint is ATTACHED: sync.go's warning loop calls
	// printRemediation once per printed line, so the hint count tracks the line
	// count instead of testing detachment (that is the error test's job). One
	// collapsed line therefore means exactly one hint; had the collapse failed,
	// this would read 2 as well.
	if got := strings.Count(output, "cannot be scoped to a command"); got != 1 {
		t.Errorf("hint rendered %d times, want exactly 1 attached to the single collapsed warning; output:\n%s", got, output)
	}
	// The artifact still installs: pu6b's whole point is that the key is
	// omitted rather than the skill withheld.
	assertFileExists(t, filepath.Join(repoRoot, ".devin", "skills", "alpha", "SKILL.md"))
}
