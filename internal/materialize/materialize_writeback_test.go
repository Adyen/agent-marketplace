package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	targetmeta "github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"

	"github.com/Adyen/agent-marketplace/internal/harness"
)

// writeBackFakeSecret stands in for a credential a user really keeps in an MCP
// env or headers block. Every assertion below checks it never reaches an error
// message, so duplicate-key rejection cannot leak a value.
const writeBackFakeSecret = "sk-test-NOT-A-REAL-SECRET"

// stagingResidue reports any temporary file atomicWriteFile could have left in
// dir. A refused write-back must leave none.
func stagingResidue(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var residue []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".agent-marketplace-") {
			residue = append(residue, entry.Name())
		}
	}
	return residue
}

// assertDuplicateKeyRefusal checks the stable diagnostic contract shared with
// the read-only loaders (bead agent-marketplace-5exj.16): a *diag.CodedError
// carrying json.duplicate-key at error severity, naming the file and the
// 1-based line, and carrying no user value.
func assertDuplicateKeyRefusal(t *testing.T, err error, path string, wantLine int) {
	t.Helper()
	if err == nil {
		t.Fatal("duplicate-key JSON was accepted")
	}
	coded, ok := diag.AsCodedError(err)
	if !ok {
		t.Fatalf("error %T does not carry a stable diagnostic code: %v", err, err)
	}
	if coded.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", coded.Code, diag.CodeJSONDuplicateKey)
	}
	if coded.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want %v", coded.Severity, diag.SeverityError)
	}
	doc, documented := diag.DocForCode(coded.Code)
	if !documented {
		t.Fatalf("code %q is not in the codes.md catalog", coded.Code)
	}
	if doc.Severity != diag.SeverityError {
		t.Fatalf("catalog severity for %q = %v, want %v", coded.Code, doc.Severity, diag.SeverityError)
	}
	if coded.Source.File != path {
		t.Fatalf("source file = %q, want %q", coded.Source.File, path)
	}
	if coded.Source.Line != wantLine {
		t.Fatalf("source line = %d, want %d", coded.Source.Line, wantLine)
	}
	if strings.Contains(err.Error(), writeBackFakeSecret) {
		t.Fatalf("error message leaked a user value: %q", err)
	}
}

// assertKeyedDuplicateRefusalDiag checks the per-file diagnostic contract of
// bead agent-marketplace-5exj.25: the refusal reaches Report.Diags as the KEYED
// json.duplicate-key diagnostic (so the TUI and `status` can filter on it)
// rather than as free text, at the catalogued error severity (so `sync` exits
// non-zero), scoped to the one file by its output-root-relative path, and
// carrying no user value.
func assertKeyedDuplicateRefusalDiag(t *testing.T, d diag.Diagnostic, wantResource string, wantLine int) {
	t.Helper()
	if d.Code != diag.CodeJSONDuplicateKey {
		t.Fatalf("code = %q, want %q", d.Code, diag.CodeJSONDuplicateKey)
	}
	if d.Severity != diag.SeverityError {
		t.Fatalf("severity = %v, want %v (a refused prune must fail the run)", d.Severity, diag.SeverityError)
	}
	if d.Resource != wantResource {
		t.Fatalf("resource = %q, want %q", d.Resource, wantResource)
	}
	if d.Source.File != wantResource {
		t.Fatalf("source file = %q, want %q", d.Source.File, wantResource)
	}
	if d.Source.Line != wantLine {
		t.Fatalf("source line = %d, want %d", d.Source.Line, wantLine)
	}
	if doc, ok := diag.DocForCode(d.Code); !ok {
		t.Fatalf("code %q is not in the codes.md catalog", d.Code)
	} else if d.Hint != doc.Hint {
		t.Fatalf("hint = %q, want the catalog hint %q", d.Hint, doc.Hint)
	}
	if !strings.Contains(d.Message, filepath.Base(wantResource)) {
		t.Fatalf("message %q does not name the destination file", d.Message)
	}
	if strings.Contains(d.Message, writeBackFakeSecret) {
		t.Fatalf("diagnostic message leaked a user value: %q", d.Message)
	}
}

// TestStripManagedEntriesFromFileRefusesDuplicateKeys is the core guarantee of
// bead agent-marketplace-5exj.18: a user's harness JSON with a duplicate object
// key is never decoded, collapsed, and written back. The destination must be
// byte-identical afterwards and no staging file may survive.
//
// The cases cover the real aggregate shapes of all five marker-merge targets.
func TestStripManagedEntriesFromFileRefusesDuplicateKeys(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		content  string
		wantLine int
	}{
		{
			// Claude: .mcp.json, duplicate inside a user server's headers.
			name: "claude mcp headers duplicate",
			file: ".mcp.json",
			content: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"user-server\": {\n" +
				"      \"command\": \"user\",\n" +
				"      \"headers\": {\n" +
				"        \"Authorization\": \"" + writeBackFakeSecret + "\",\n" +
				"        \"Authorization\": \"" + writeBackFakeSecret + "\"\n" +
				"      }\n" +
				"    },\n" +
				"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  }\n" +
				"}\n",
			wantLine: 7,
		},
		{
			// Factory: settings.json, duplicate at the top level.
			name: "factory settings top-level duplicate",
			file: "settings.json",
			content: "{\n" +
				"  \"hooks\": {\"PostToolUse\": [{\"_managed_by\": \"agent-marketplace\"}]},\n" +
				"  \"hooks\": {}\n" +
				"}\n",
			wantLine: 3,
		},
		{
			// Cursor: mcp.json, duplicate inside a user server's env.
			name: "cursor mcp env duplicate",
			file: "mcp.json",
			content: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"},\n" +
				"    \"user\": {\"env\": {\"TOKEN\": \"" + writeBackFakeSecret + "\", \"TOKEN\": \"other\"}}\n" +
				"  }\n" +
				"}\n",
			wantLine: 4,
		},
		{
			// Windsurf: mcp_config.json, the same server key twice.
			name: "windsurf duplicate server key",
			file: "mcp_config.json",
			content: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"docs\": {\"command\": \"safe\"},\n" +
				"    \"managed\": {\"_managed_by\": \"agent-marketplace\"},\n" +
				"    \"docs\": {\"command\": \"rm -rf /\"}\n" +
				"  }\n" +
				"}\n",
			wantLine: 5,
		},
		{
			// Gemini: gemini-extension.json array container, duplicate inside a
			// preserved array element.
			name: "gemini array element duplicate",
			file: "gemini-extension.json",
			content: "{\n" +
				"  \"skills\": [\n" +
				"    {\"name\": \"user\", \"name\": \"user-renamed\"},\n" +
				"    {\"name\": \"managed\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  ]\n" +
				"}\n",
			wantLine: 3,
		},
		{
			// A duplicate of the ownership marker itself must not decide
			// ownership by last-value-wins either.
			name: "duplicate managed-by marker",
			file: "mcp.json",
			content: "{\n" +
				"  \"mcpServers\": {\n" +
				"    \"entry\": {\n" +
				"      \"_managed_by\": \"someone-else\",\n" +
				"      \"_managed_by\": \"agent-marketplace\"\n" +
				"    }\n" +
				"  }\n" +
				"}\n",
			wantLine: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, dryRun := range []bool{false, true} {
				dir := t.TempDir()
				path := filepath.Join(dir, tt.file)
				original := []byte(tt.content)
				if err := os.WriteFile(path, original, 0o644); err != nil {
					t.Fatal(err)
				}

				cleaned, removed, err := stripManagedEntriesFromFile(path, dryRun, false, nil)
				assertDuplicateKeyRefusal(t, err, path, tt.wantLine)
				if cleaned != nil {
					t.Fatalf("dryRun=%v: cleaned = %q, want nil", dryRun, cleaned)
				}
				if removed != nil {
					t.Fatalf("dryRun=%v: removed = %v, want nil", dryRun, removed)
				}

				after, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatalf("read back %s: %v", path, readErr)
				}
				if !bytes.Equal(after, original) {
					t.Fatalf("dryRun=%v: destination mutated:\nbefore: %q\nafter:  %q", dryRun, original, after)
				}
				if residue := stagingResidue(t, dir); len(residue) != 0 {
					t.Fatalf("dryRun=%v: staging residue left behind: %v", dryRun, residue)
				}
			}
		})
	}
}

// TestPruneManagedMergeFilesRefusesDuplicateKeys proves the refusal survives
// both walk entry points and that neither one writes anything on the way out.
func TestPruneManagedMergeFilesRefusesDuplicateKeys(t *testing.T) {
	original := []byte("{\n" +
		"  \"mcpServers\": {\n" +
		"    \"user\": {\"env\": {\"TOKEN\": \"" + writeBackFakeSecret + "\", \"TOKEN\": \"second\"}},\n" +
		"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"}\n" +
		"  }\n" +
		"}\n")

	tests := []struct {
		name string
		run  func(outDir string) []diag.Diagnostic
	}{
		{
			name: "pruneManagedMergeFiles",
			run: func(outDir string) []diag.Diagnostic {
				_, diags := pruneManagedMergeFiles(outDir, false)
				return diags
			},
		},
		{
			name: "pruneUncoveredManagedMergeFiles",
			run: func(outDir string) []diag.Diagnostic {
				_, diags := pruneUncoveredManagedMergeFiles(outDir, nil, false, nil, nil)
				return diags
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			path := filepath.Join(outDir, "mcp.json")
			if err := os.WriteFile(path, original, 0o644); err != nil {
				t.Fatal(err)
			}

			diags := tt.run(outDir)
			if len(diags) != 1 {
				t.Fatalf("diagnostics = %+v, want exactly one", diags)
			}
			assertKeyedDuplicateRefusalDiag(t, diags[0], "mcp.json", 3)

			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read back: %v", readErr)
			}
			if !bytes.Equal(after, original) {
				t.Fatalf("destination mutated:\nbefore: %q\nafter:  %q", original, after)
			}
			if residue := stagingResidue(t, outDir); len(residue) != 0 {
				t.Fatalf("staging residue left behind: %v", residue)
			}
		})
	}
}

// TestStripManagedEntriesFromFilePreservesCommentsAndOrder pins the exact bytes
// of a successful write-back. Everything the marketplace did not generate keeps
// its original spelling, position, and comments; only marked entries disappear.
func TestStripManagedEntriesFromFilePreservesCommentsAndOrder(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		in          string
		want        string
		wantRemoved []string
	}{
		{
			name: "map container keeps comments and key order",
			file: "mcp.json",
			in: "{\n" +
				"  // top comment\n" +
				"  \"zzz-first\": \"kept as written\",\n" +
				"  \"mcpServers\": {\n" +
				"    // my own server, do not touch\n" +
				"    \"user\": {\"command\":\"user\",   \"env\": {\"TOKEN\": \"" + writeBackFakeSecret + "\"}},\n" +
				"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  },\n" +
				"  \"aaa-last\": 1\n" +
				"}\n",
			want: "{\n" +
				"  // top comment\n" +
				"  \"zzz-first\": \"kept as written\",\n" +
				"  \"mcpServers\": {\n" +
				"    // my own server, do not touch\n" +
				"    \"user\": {\"command\":\"user\",   \"env\": {\"TOKEN\": \"" + writeBackFakeSecret + "\"}}\n" +
				"  },\n" +
				"  \"aaa-last\": 1\n" +
				"}\n",
			wantRemoved: []string{"managed"},
		},
		{
			name: "array container keeps handwritten element bytes",
			file: "plugin.json",
			in: "{\n" +
				"  \"skills\": [\n" +
				"    // handwritten\n" +
				"    {\"name\": \"user-skill\",  \"description\": \"keep me\"},\n" +
				"    {\"name\": \"managed-skill\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  ]\n" +
				"}\n",
			want: "{\n" +
				"  \"skills\": [\n" +
				"    // handwritten\n" +
				"    {\"name\": \"user-skill\",  \"description\": \"keep me\"}\n" +
				"  ]\n" +
				"}\n",
			wantRemoved: []string{"managed-skill"},
		},
		{
			name: "fully managed array container drops the key",
			file: "plugin.json",
			in: "{\n" +
				"  \"keep\": {\"a\": 1},\n" +
				"  \"droids\": [\n" +
				"    {\"name\": \"managed-droid\", \"_managed_by\": \"agent-marketplace\"}\n" +
				"  ]\n" +
				"}\n",
			want: "{\n" +
				"  \"keep\": {\"a\": 1}\n" +
				"}\n",
			wantRemoved: []string{"managed-droid"},
		},
		{
			name: "unnamed managed array entry reports the container key",
			file: "hooks.json",
			in: "{\n" +
				"  \"PostToolUse\": [\n" +
				"    {\"matcher\": \"mine\"},\n" +
				"    {\"_managed_by\": \"agent-marketplace\"}\n" +
				"  ]\n" +
				"}\n",
			want: "{\n" +
				"  \"PostToolUse\": [\n" +
				"    {\"matcher\": \"mine\"}\n" +
				"  ]\n" +
				"}\n",
			wantRemoved: []string{"PostToolUse"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.file)
			if err := os.WriteFile(path, []byte(tt.in), 0o644); err != nil {
				t.Fatal(err)
			}

			cleaned, removed, err := stripManagedEntriesFromFile(path, false, false, nil)
			if err != nil {
				t.Fatalf("stripManagedEntriesFromFile: %v", err)
			}
			if string(cleaned) != tt.want {
				t.Fatalf("cleaned =\n%q\nwant\n%q", cleaned, tt.want)
			}
			if strings.Join(removed, ",") != strings.Join(tt.wantRemoved, ",") {
				t.Fatalf("removed = %v, want %v", removed, tt.wantRemoved)
			}

			onDisk, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read back: %v", readErr)
			}
			if string(onDisk) != tt.want {
				t.Fatalf("on-disk bytes =\n%q\nwant\n%q", onDisk, tt.want)
			}
			if residue := stagingResidue(t, dir); len(residue) != 0 {
				t.Fatalf("staging residue left behind: %v", residue)
			}

			// A dry run must produce the same bytes and touch nothing.
			dryDir := t.TempDir()
			dryPath := filepath.Join(dryDir, tt.file)
			if err := os.WriteFile(dryPath, []byte(tt.in), 0o644); err != nil {
				t.Fatal(err)
			}
			dryCleaned, _, dryErr := stripManagedEntriesFromFile(dryPath, true, false, nil)
			if dryErr != nil {
				t.Fatalf("dry run: %v", dryErr)
			}
			if string(dryCleaned) != tt.want {
				t.Fatalf("dry-run cleaned =\n%q\nwant\n%q", dryCleaned, tt.want)
			}
			untouched, readErr := os.ReadFile(dryPath)
			if readErr != nil {
				t.Fatalf("read back dry-run file: %v", readErr)
			}
			if string(untouched) != tt.in {
				t.Fatalf("dry run mutated the file:\n%q", untouched)
			}
		})
	}
}

// TestStripManagedEntriesFromFileWriteBackIsIdempotent proves the patched bytes
// are a fixed point: a second prune of the same file changes nothing, so the
// new concrete-syntax write-back cannot oscillate across syncs.
func TestStripManagedEntriesFromFileWriteBackIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	in := "{\n  \"mcpServers\": {\n    \"user\": {\"command\": \"u\"},\n    \"managed\": {\"_managed_by\": \"agent-marketplace\"}\n  }\n}\n"
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	first, removed, err := stripManagedEntriesFromFile(path, false, false, nil)
	if err != nil {
		t.Fatalf("first prune: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed = %v, want one entry", removed)
	}
	// The marker is gone, so the second pass short-circuits before parsing and
	// reports nothing. What matters is that the bytes on disk did not move.
	_, removedAgain, err := stripManagedEntriesFromFile(path, false, false, nil)
	if err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if len(removedAgain) != 0 {
		t.Fatalf("second prune removed %v, want nothing", removedAgain)
	}
	onDisk, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read back: %v", readErr)
	}
	if !bytes.Equal(first, onDisk) {
		t.Fatalf("write-back is not a fixed point:\nwritten: %q\non disk: %q", first, onDisk)
	}
}

// TestMarkerMergeTargetsAreNotAggregateProtected records the blast radius: the
// five marker-merge targets all still flow through stripManagedEntriesFromFile,
// so the guarantees above apply to every one of them. Only Devin is fenced off
// (bead agent-marketplace-5exj.10 / devin.go), and that fence is unchanged.
func TestMarkerMergeTargetsAreNotAggregateProtected(t *testing.T) {
	markerMerge := []hcc.Target{
		hcc.TargetClaude,
		hcc.TargetFactory,
		hcc.TargetCursor,
		hcc.TargetWindsurf,
		hcc.TargetGemini,
	}
	for _, target := range markerMerge {
		for _, machineScope := range []bool{false, true} {
			if got := aggregateProtectedPaths(target, machineScope); len(got) != 0 {
				t.Fatalf("aggregateProtectedPaths(%s, machine=%v) = %v, want empty", target, machineScope, got)
			}
		}
	}
	// Devin and the privately staged Codex target reconcile aggregates through
	// durable ownership rather than in-file markers.
	codex := hcc.Target(targetmeta.TargetCodex)
	for target, info := range harness.TargetInfoMap {
		if info.OwnershipReconciledAggregates != (target == hcc.TargetDevin || target == codex) {
			t.Fatalf("OwnershipReconciledAggregates(%s) = %v; only Devin and Codex should bypass marker merging",
				target, info.OwnershipReconciledAggregates)
		}
	}
}
