package materialize

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
)

// The prune-walk fixtures. Every one of them carries the ownership marker, so
// none of them is short-circuited by the "no marker, nothing to do" fast path
// in stripManagedEntriesFromFile: each file really is parsed.
const (
	// pruneWalkGood is a file the walk can prune: one marked entry to remove,
	// one handwritten entry to preserve.
	pruneWalkGood = "{\n" +
		"  \"mcpServers\": {\n" +
		"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"},\n" +
		"    \"user\": {\"command\": \"u\"}\n" +
		"  }\n" +
		"}\n"

	// pruneWalkGoodPruned is the exact expected result of pruning pruneWalkGood.
	pruneWalkGoodPruned = "{\n" +
		"  \"mcpServers\": {\n" +
		"    \"user\": {\"command\": \"u\"}\n" +
		"  }\n" +
		"}\n"

	// pruneWalkBad is refused: the user's env block repeats a key, so the
	// document's meaning is parser-dependent and bead .18 refuses to rewrite it.
	// The duplicate is on line 3.
	pruneWalkBad = "{\n" +
		"  \"mcpServers\": {\n" +
		"    \"user\": {\"env\": {\"TOKEN\": \"" + writeBackFakeSecret + "\", \"TOKEN\": \"second\"}},\n" +
		"    \"managed\": {\"command\": \"m\", \"_managed_by\": \"agent-marketplace\"}\n" +
		"  }\n" +
		"}\n"

	// pruneWalkMalformed is not JSON at all. Bead .18 deliberately chose to
	// leave such a file completely alone WITHOUT an error, and this bead must
	// not turn that silence into a diagnostic.
	pruneWalkMalformed = "{ not valid json \"_managed_by\": \"agent-marketplace\"\n"
)

// pruneWalkFile is one fixture placed in the walked root.
type pruneWalkFile struct {
	rel     string
	content string
	// want is the exact expected content after the walk. For the refused and
	// the malformed file it equals content: they must be byte-identical.
	want string
	// pruned marks the files the walk is expected to rewrite and report.
	pruned bool
}

// pruneWalkFixtures is the acceptance criterion's layout: ONE bad file next to
// SEVERAL good ones.
//
// The names are load-bearing and the test asserts it. filepath.WalkDir visits
// lexically, so "20-bad.json" is visited BEFORE "30-good.json", "40-*.json" and
// everything under "nested/". Under the old code — which returned the refusal
// out of the WalkDir callback — those three were never reached and silently
// stayed un-pruned. That is what makes this test non-vacuous: reverting the fix
// fails it on the un-pruned siblings, not merely on the diagnostic shape.
var pruneWalkFixtures = []pruneWalkFile{
	{rel: "10-good.json", content: pruneWalkGood, want: pruneWalkGoodPruned, pruned: true},
	{rel: "20-bad.json", content: pruneWalkBad, want: pruneWalkBad},
	{rel: "30-good.json", content: pruneWalkGood, want: pruneWalkGoodPruned, pruned: true},
	{rel: "40-malformed.json", content: pruneWalkMalformed, want: pruneWalkMalformed},
	{rel: "nested/50-good.json", content: pruneWalkGood, want: pruneWalkGoodPruned, pruned: true},
}

const pruneWalkBadRel = "20-bad.json"

func writePruneWalkFixtures(t *testing.T, outDir string) {
	t.Helper()
	for _, f := range pruneWalkFixtures {
		path := filepath.Join(outDir, filepath.FromSlash(f.rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPruneWalkRefusesOneFileAndKeepsPruningTheRest is the headline test for
// bead agent-marketplace-5exj.25's acceptance criterion: "A refused file yields
// a per-file diagnostic and the walk continues pruning every other file in the
// root; the refusal reaches Report.Diags as a keyed diagnostic rather than a
// bare error; a test covers one bad file next to several good ones."
//
// It runs against BOTH walk entry points, because both had the bug.
func TestPruneWalkRefusesOneFileAndKeepsPruningTheRest(t *testing.T) {
	// Non-vacuity guard: at least one good file must be visited AFTER the bad
	// one, or an aborting walk would pass this test by accident.
	sawGoodAfterBad := false
	for _, f := range pruneWalkFixtures {
		if f.pruned && f.rel > pruneWalkBadRel {
			sawGoodAfterBad = true
		}
	}
	if !sawGoodAfterBad {
		t.Fatalf("fixture layout is vacuous: no prunable file sorts after %q", pruneWalkBadRel)
	}

	tests := []struct {
		name string
		run  func(outDir string) (*hcc.WriteReport, []diag.Diagnostic)
	}{
		{
			name: "pruneManagedMergeFiles",
			run: func(outDir string) (*hcc.WriteReport, []diag.Diagnostic) {
				return pruneManagedMergeFiles(outDir, false)
			},
		},
		{
			name: "pruneUncoveredManagedMergeFiles",
			run: func(outDir string) (*hcc.WriteReport, []diag.Diagnostic) {
				return pruneUncoveredManagedMergeFiles(outDir, nil, false, nil, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outDir := t.TempDir()
			writePruneWalkFixtures(t, outDir)

			report, diags := tt.run(outDir)

			// Exactly one diagnostic, keyed, naming the one refused file.
			if len(diags) != 1 {
				t.Fatalf("diagnostics = %+v, want exactly one (only %q is refused)", diags, pruneWalkBadRel)
			}
			assertKeyedDuplicateRefusalDiag(t, diags[0], pruneWalkBadRel, 3)

			// Every other file reached its expected end state.
			var wantWritten []string
			for _, f := range pruneWalkFixtures {
				path := filepath.Join(outDir, filepath.FromSlash(f.rel))
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read back %s: %v", f.rel, err)
				}
				if string(got) != f.want {
					t.Fatalf("%s:\n got %q\nwant %q", f.rel, got, f.want)
				}
				if f.pruned {
					wantWritten = append(wantWritten, f.rel)
				}
			}

			gotWritten := append([]string(nil), report.Written...)
			sort.Strings(gotWritten)
			sort.Strings(wantWritten)
			if strings.Join(gotWritten, ",") != strings.Join(wantWritten, ",") {
				t.Fatalf("report.Written = %v, want %v", gotWritten, wantWritten)
			}
			if len(report.Removed) != 0 {
				t.Fatalf("report.Removed = %v, want none", report.Removed)
			}
			if residue := stagingResidue(t, outDir); len(residue) != 0 {
				t.Fatalf("staging residue left behind: %v", residue)
			}
		})
	}
}

// TestPruneWalkLeavesMalformedFilesSilent pins the half of bead .18's decision
// this bead must not disturb: a malformed destination is left completely alone
// and produces NO diagnostic, even though the per-file error handling around it
// is new. A user who already has to fix that file by hand gains nothing from a
// failed sync.
func TestPruneWalkLeavesMalformedFilesSilent(t *testing.T) {
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "broken.json"), []byte(pruneWalkMalformed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "fine.json"), []byte(pruneWalkGood), 0o644); err != nil {
		t.Fatal(err)
	}

	report, diags := pruneManagedMergeFiles(outDir, false)
	if len(diags) != 0 {
		t.Fatalf("malformed JSON produced diagnostics %+v, want none", diags)
	}
	if len(report.Written) != 1 || report.Written[0] != "fine.json" {
		t.Fatalf("report.Written = %v, want [fine.json]", report.Written)
	}
	broken, err := os.ReadFile(filepath.Join(outDir, "broken.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(broken) != pruneWalkMalformed {
		t.Fatalf("malformed file was rewritten: %q", broken)
	}
}

// TestRunSurfacesPruneRefusalAsKeyedReportDiag proves the second half of the
// bead end to end: the refusal reaches Report.Diags as the keyed
// json.duplicate-key diagnostic (not a free-text materialize.write.error), at
// error severity so `sync` exits non-zero, scoped to the one file by its
// output-root-relative path, and stamped with the target — while its siblings
// in the same root are still pruned.
//
// Windsurf's repo scope is used deliberately: it is the one target whose prune
// root is a subdirectory, so this also pins that the diagnostic's Resource is
// lifted into the same ".windsurf/"-prefixed namespace as the report rows.
func TestRunSurfacesPruneRefusalAsKeyedReportDiag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repoRoot := t.TempDir()
	outDir := filepath.Join(repoRoot, windsurfDir)
	writePruneWalkFixtures(t, outDir)

	report, err := Run(context.Background(), repoRoot, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var refusals []diag.Diagnostic
	var otherErrors []diag.Diagnostic
	for _, d := range report.Diags {
		switch {
		case d.Code == diag.CodeJSONDuplicateKey:
			refusals = append(refusals, d)
		case d.Severity == diag.SeverityError:
			otherErrors = append(otherErrors, d)
		}
	}
	if len(otherErrors) != 0 {
		t.Fatalf("unexpected additional error diagnostics: %+v", otherErrors)
	}
	if len(refusals) != 1 {
		t.Fatalf("json.duplicate-key diagnostics = %+v, want exactly one", refusals)
	}
	wantResource := windsurfDir + "/" + pruneWalkBadRel
	assertKeyedDuplicateRefusalDiag(t, refusals[0], wantResource, 3)
	if refusals[0].Target != string(hcc.TargetWindsurf) {
		t.Fatalf("target = %q, want %q", refusals[0].Target, hcc.TargetWindsurf)
	}
	// Error severity is what makes `sync` exit non-zero (internal/pipeline
	// promotes error diagnostics into SyncResult.Errors): a prune that did not
	// happen is a real failure, the same call bead .20 made for skipped Devin
	// aggregate entries and bead .18 made for the refusal itself.
	if !diag.HasErrors(report.Diags) {
		t.Fatal("a refused prune must make the run fail")
	}

	for _, f := range pruneWalkFixtures {
		got, readErr := os.ReadFile(filepath.Join(outDir, filepath.FromSlash(f.rel)))
		if readErr != nil {
			t.Fatalf("read back %s: %v", f.rel, readErr)
		}
		if string(got) != f.want {
			t.Fatalf("%s:\n got %q\nwant %q", f.rel, got, f.want)
		}
	}
}
