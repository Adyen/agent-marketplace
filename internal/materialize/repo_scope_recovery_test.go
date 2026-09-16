// Repo-scope aggregate recovery (bead agent-marketplace-5exj.30).
//
// Safety backups in internal/backup are machine scope only, and bead .30
// decided to keep them that way rather than add a second backup mechanism for
// repo-scope aggregates. The full argument lives in the internal/backup package
// doc. The decision rests on two properties, and this file is what pins them:
//
//   - For the five marker-merge targets, a merge does not damage handwritten
//     content in the first place, so there is nothing for a repo-scope archive
//     to recover (TestRepoScopeAggregateMergePreservesHandwrittenContent).
//   - For a target reconciled through internal/ownership, repo-scope aggregates
//     already have mandatory raw-byte backups, keyed by canonical root and
//     stored outside the repository
//     (TestDevinRepoScopeAggregateRecoverableFromOwnershipBackup). That is the
//     coverage every other target inherits as
//     docs/plans/2026-08-04-marker-to-ownership-migration.md's M4-M8 land.
package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The handwritten fixture is deliberately shape-agnostic: a comment (which
// encoding/json deletes) and one top-level member that no compiler writes
// (which a whole-document re-serialization would reorder or restyle). Every
// aggregate container in the tree — {"mcpServers": {…}}, {"hooks": {Event: […]}},
// {Event: [ruleGroup]}, {"skills": […], "agents": […]} — accepts an unrelated
// top-level member, so one fixture covers all four without the test having to
// know which target uses which.
const (
	handwrittenComment = `// handwritten: agent-marketplace must not touch this`
	handwrittenMember  = `"x-user-note": "keep me"`
	handwrittenDoc     = "{\n  " + handwrittenComment + "\n  " + handwrittenMember + "\n}\n"
)

// compileRepoScopeArtifacts compiles one resource of every probe kind for a
// target at repo scope and returns the artifacts, using the same options the
// production materializer uses for a marker-merge target.
func compileRepoScopeArtifacts(t *testing.T, target hcc.Target) []artifact.Artifact {
	t.Helper()
	var artifacts []artifact.Artifact
	probes := probeResources()
	for _, kind := range sortedKinds(probes) {
		result, err := hcc.Compile(probes[kind], target,
			hcc.WithManagedBy(managedByField, managedByValue))
		if err != nil {
			t.Fatalf("Compile(%s, %s): %v", kind, target, err)
		}
		if result == nil {
			continue
		}
		artifacts = append(artifacts, result.Artifacts...)
	}
	return artifacts
}

// runRepoScopeMerge drives the PRODUCTION merge write path — materializePlans,
// splitMergeArtifacts, materializeMergeArtifacts — against a repo-scope install
// root, exactly as materializeTargetRoot does for a real sync. Nothing here
// reimplements the merge; a regression in the write path shows up as a failure.
func runRepoScopeMerge(t *testing.T, target hcc.Target, outDir string, artifacts []artifact.Artifact) {
	t.Helper()
	merged := 0
	for _, plan := range materializePlans(target, outDir, artifacts, nil, false) {
		_, mergeArtifacts := splitMergeArtifacts(plan.artifacts)
		if len(mergeArtifacts) == 0 {
			continue
		}
		if _, diags, err := materializeMergeArtifacts(plan.outDir, mergeArtifacts, false, true); err != nil {
			t.Fatalf("materializeMergeArtifacts(%s): %v", plan.outDir, err)
		} else if len(diags) > 0 {
			t.Fatalf("materializeMergeArtifacts(%s) reported %v", plan.outDir, diags)
		}
		merged += len(mergeArtifacts)
	}
	if merged == 0 {
		t.Fatalf("no merge artifact was produced for %s at repo scope; the assertions would be vacuous", target)
	}
}

// TestRepoScopeAggregateMergePreservesHandwrittenContent is the property that
// makes "no repo-scope backup machinery" safe for the five marker-merge
// targets, pinned over every file in the declared repo-scope inventory.
//
// A repo-scope aggregate holds BOTH generated and handwritten content — that is
// the whole reason the merge path exists — and it is not archived anywhere. The
// bead's premise was that a merge could therefore destroy handwritten content
// unrecoverably. After beads .26 and .28 put both the write and the prune path
// on pkg/harness-compilation/jsonc, a merge does not rewrite content it does not
// own at all, so the damage the archive would have recovered from does not
// occur. This test is what keeps that true.
//
// The expectation is not restated anywhere: the file set comes from
// harness.TargetInfo.AggregateFiles.Project, which
// TestAggregateFilesMatchMaterializedMergeArtifacts independently pins against
// the paths the compilers really emit. A target that grows a new repo-scope
// aggregate is covered here the moment it is declared.
//
// Non-vacuity is asserted three ways per file: the merge must actually have
// written a managed entry (so the file was really touched), the output must
// differ from the handwritten input (so "preserved" is not "never written"), and
// at least one target must be exercised (so an all-ownership registry cannot
// silently empty the test).
func TestRepoScopeAggregateMergePreservesHandwrittenContent(t *testing.T) {
	exercised := 0
	for _, target := range sortedTargets(t) {
		info, ok := harness.TargetInfoFor(target)
		if !ok {
			t.Fatalf("missing target metadata for %s", target)
		}
		if info.OwnershipReconciledAggregates {
			// Its aggregates never reach this path; they are covered by
			// TestDevinRepoScopeAggregateRecoverableFromOwnershipBackup.
			continue
		}
		exercised++
		t.Run(string(target), func(t *testing.T) {
			// outDir stands in for <repo>/<Roots.Repo>, the repo-scope install
			// root the declared paths are relative to.
			outDir := t.TempDir()
			declared := info.AggregateFilesForScope(false)
			if len(declared) == 0 {
				t.Fatalf("%s declares no repo-scope aggregate files; every marker-merge target merges into at least one", target)
			}
			for _, rel := range declared {
				full := filepath.Join(outDir, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatalf("mkdir for %s: %v", rel, err)
				}
				if err := os.WriteFile(full, []byte(handwrittenDoc), 0o644); err != nil {
					t.Fatalf("write %s: %v", full, err)
				}
			}

			artifacts := compileRepoScopeArtifacts(t, target)
			runRepoScopeMerge(t, target, outDir, artifacts)

			first := make(map[string]string, len(declared))
			for _, rel := range declared {
				got := readFileString(t, filepath.Join(outDir, filepath.FromSlash(rel)))
				first[rel] = got

				if !strings.Contains(got, handwrittenComment) {
					t.Errorf("repo-scope aggregate %q of %s LOST the handwritten comment, which no backup would have recovered:\n%s", rel, target, got)
				}
				if !strings.Contains(got, handwrittenMember) {
					t.Errorf("repo-scope aggregate %q of %s LOST or restyled the handwritten member %s:\n%s", rel, target, handwrittenMember, got)
				}
				if !strings.Contains(got, `"`+managedByField+`"`) {
					t.Errorf("repo-scope aggregate %q of %s has no managed entry; the merge did not run and the preservation assertions above are vacuous:\n%s", rel, target, got)
				}
				if got == handwrittenDoc {
					t.Errorf("repo-scope aggregate %q of %s is byte-identical to the handwritten input; nothing was merged and the assertions above are vacuous", rel, target)
				}
			}

			// Convergence: a second sync of an unchanged desired set must not
			// move the user's bytes either. A merge that churned the file on
			// every run would keep re-risking handwritten content forever.
			runRepoScopeMerge(t, target, outDir, artifacts)
			for _, rel := range declared {
				got := readFileString(t, filepath.Join(outDir, filepath.FromSlash(rel)))
				if got != first[rel] {
					t.Errorf("repo-scope aggregate %q of %s is not convergent; a second identical sync rewrote it.\nfirst:\n%s\nsecond:\n%s", rel, target, first[rel], got)
				}
			}
		})
	}
	if exercised == 0 {
		t.Fatal("every target is ownership-reconciled, so this test exercised nothing; move its coverage to the ownership test before deleting it")
	}
}

// TestDevinRepoScopeAggregateRecoverableFromOwnershipBackup answers the bead's
// acceptance criterion for the mechanism that is actually going to cover every
// target: "a handwritten repo-scope aggregate damaged by a merge is
// recoverable".
//
// It is recoverable today for Devin, at REPO scope, without internal/backup
// being involved at all, because internal/ownership is keyed by canonical
// absolute root rather than by target and is opened scope-agnostically by
// reconcileDevinAggregates. The three properties that make it the right home for
// this problem are all asserted here:
//
//  1. the exact pre-mutation bytes come back, comments included;
//  2. the retained bytes live OUTSIDE the repository (under $XDG_STATE_HOME), so
//     the archive itself never needs gitignoring and never enters a diff — the
//     objection that sinks any in-repo backup scheme;
//  3. two different repository roots get two different backup directories, which
//     is exactly the root dimension internal/backup's <target>/<timestamp>
//     namespace does not have.
func TestDevinRepoScopeAggregateRecoverableFromOwnershipBackup(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	rel := harness.DevinMcpConfigFile
	path := filepath.Join(root, rel)
	handwritten := `{
  // handwritten: a backup must return these exact bytes
  "mcpServers": {
    "user-owned": { "command": "user-bin" }
  }
}
`
	if err := os.WriteFile(path, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("mcp", ir.NewMcpServer("mcp", "bin", nil, nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoErrorDiags(t, report.Diags)

	afterMerge := readFileString(t, path)
	if afterMerge == handwritten {
		t.Fatal("the merge changed nothing, so there was no mutation to back up and this test is vacuous")
	}

	// Damage the file the way a bad merge would.
	if err := os.WriteFile(path, []byte("DAMAGED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := ownership.Open(root)
	if err != nil {
		t.Fatalf("ownership.Open(%s): %v", root, err)
	}
	backups, err := store.SafetyBackups(rel)
	if err != nil {
		t.Fatalf("SafetyBackups(%s): %v", rel, err)
	}
	if len(backups) == 0 {
		t.Fatalf("no safety backup was retained for the REPO-scope aggregate %s; repo scope is meant to be covered by ownership, not by internal/backup", rel)
	}
	// Oldest first: the first retained backup holds the bytes from before the
	// first managed merge, which is the handwritten file.
	raw, err := backups[0].RawBytes()
	if err != nil {
		t.Fatalf("RawBytes: %v", err)
	}
	if string(raw) != handwritten {
		t.Fatalf("the retained bytes are not the handwritten file.\nwant:\n%s\ngot:\n%s", handwritten, raw)
	}
	if backups[0].Manifest.PriorAbsent {
		t.Error("the manifest claims the aggregate was absent; it was handwritten and present")
	}

	// Recovery is a copy of those bytes back over the damaged file.
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readFileString(t, path); got != handwritten {
		t.Fatalf("recovery did not restore the handwritten bytes.\nwant:\n%s\ngot:\n%s", handwritten, got)
	}

	// Property 2: the retained bytes are not inside the repository.
	backupDir := store.SafetyBackupDir(rel)
	if within(t, repoRoot, backupDir) {
		t.Errorf("safety backups for a repo-scope aggregate live inside the repository at %s; they would need gitignoring and would carry user content into the working tree", backupDir)
	}
	stateRoot := os.Getenv("XDG_STATE_HOME")
	if stateRoot == "" {
		t.Fatal("devinEnv must set XDG_STATE_HOME or the location assertion is meaningless")
	}
	if !within(t, stateRoot, backupDir) {
		t.Errorf("safety backup directory %s is not under the XDG state root %s", backupDir, stateRoot)
	}

	// Property 3: the store has a root dimension internal/backup lacks.
	otherRepo := t.TempDir()
	otherRoot := devinProjectRoot(otherRepo)
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	otherStore, err := ownership.Open(otherRoot)
	if err != nil {
		t.Fatalf("ownership.Open(%s): %v", otherRoot, err)
	}
	if other := otherStore.SafetyBackupDir(rel); other == backupDir {
		t.Errorf("two repository roots share one safety backup directory %s; ownership state must be keyed by canonical root", other)
	}
}

// within reports whether path is root or lives beneath it, after resolving
// symlinks so a /tmp -> /private/tmp style indirection cannot produce a false
// negative.
func within(t *testing.T, root, path string) bool {
	t.Helper()
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	rel, err := filepath.Rel(resolve(root), resolve(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
