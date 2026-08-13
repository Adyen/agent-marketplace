package materialize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/harness"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/resolver"
)

// devinEnvAt is devinEnv with a caller-chosen repo root, so a test can MOVE the
// repo between runs.
func devinEnvAt(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

func ownershipDiags(diags []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diags {
		if strings.HasPrefix(d.Code, "ownership.") && d.Severity <= diag.SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

// TestDevinRootIdentityChangeIsSilentButStillFailsClosed pins the WORST
// residual of keying first-use suppression on evidence stored under the root
// key.
//
// The root key is a hash of the canonicalized target root, so anything that
// changes a managed root's canonical spelling — moving or renaming the
// checkout, `.devin` becoming a symlink, a case-different spelling on a
// case-insensitive filesystem, a relocated XDG_STATE_HOME — looks exactly like
// a first-ever use, even though the aggregate on disk is full of entries this
// tool wrote and the old record is still sitting on the same disk. Before this
// change the user got ownership.state-missing; now the run is silent, and a
// resource REMOVED from configuration stays in the user's Devin config forever
// with nothing explaining why.
//
// The load-bearing half is asserted hard: fail-closed must hold regardless.
// The silence is asserted so the residual is visible and tested rather than a
// surprise; if a later change starts reporting it, DELETE the silence assertion
// (and the residual note on ownership.ErrStateUninitialized) — that is a fix,
// not a regression.
func TestDevinRootIdentityChangeIsSilentButStillFailsClosed(t *testing.T) {
	base := t.TempDir()
	repoA := filepath.Join(base, "repo-a")
	repoB := filepath.Join(base, "repo-b")
	devinEnvAt(t, repoA)

	alpha := []resolver.ResolvedArtifact{
		devinResolved("alpha", ir.NewMcpServer("alpha", "alpha-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	if _, err := Run(context.Background(), repoA, alpha); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	storeA, err := ownership.Open(devinProjectRoot(repoA))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storeA.StateFile()); err != nil {
		t.Fatalf("the first sync did not record ownership: %v", err)
	}

	// The user moves the checkout. The aggregate moves with it, still holding
	// the entry we wrote and proved we own.
	if err := os.Rename(repoA, repoB); err != nil {
		t.Fatalf("move the checkout: %v", err)
	}

	// "alpha" has been removed from configuration; only "beta" is desired.
	beta := []resolver.ResolvedArtifact{
		devinResolved("beta", ir.NewMcpServer("beta", "beta-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoB, beta)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	mcpPath := filepath.Join(devinProjectRoot(repoB), harness.DevinMcpConfigFile)
	got := readFileString(t, mcpPath)
	// Fail-closed still holds, and must: the stale entry is preserved, not pruned.
	if !strings.Contains(got, "alpha-binary") {
		t.Fatalf("fail-closed broken: the unproven entry was pruned:\n%s", got)
	}

	if !strings.Contains(got, "beta-binary") {
		t.Fatalf("the desired entry was not written, so the run did not actually reconcile:\n%s", got)
	}
	// The residual: nothing at all is reported. Documented on
	// ownership.ErrStateUninitialized.
	if found := ownershipDiags(report.Diags); len(found) != 0 {
		t.Fatalf("the root-identity residual is fixed — delete this assertion and the residual note: %+v", found)
	}
}

// TestDevinFirstRunSuppressionStillFailsClosedAgainstAHandwrittenAggregate
// attacks the fail-closed invariant on the NEW code path: a first-ever run
// (suppressed reporting) against an aggregate the user hand-wrote.
func TestDevinFirstRunSuppressionStillFailsClosedAgainstAHandwrittenAggregate(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	root := devinProjectRoot(repoRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(root, harness.DevinMcpConfigFile)
	handwritten := `{
  // a comment the user wrote
  "mcpServers": {
    "managed": {"command": "USER-OWNED"},
    "untouched": {"command": "keep-me"}
  }
}`
	if err := os.WriteFile(mcpPath, []byte(handwritten), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved := []resolver.ResolvedArtifact{
		devinResolved("managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := readFileString(t, mcpPath)
	if !strings.Contains(got, "USER-OWNED") {
		t.Fatalf("a handwritten entry was overwritten on a first run:\n%s", got)
	}
	if strings.Contains(got, "managed-binary") {
		t.Fatalf("a colliding generated entry was silently adopted on a first run:\n%s", got)
	}
	if !strings.Contains(got, "keep-me") {
		t.Fatalf("an unrelated handwritten entry was lost:\n%s", got)
	}
	if !strings.Contains(got, "a comment the user wrote") {
		t.Fatalf("comments were lost:\n%s", got)
	}
	if _, ok := findDiag(report.Diags, diag.CodeOwnershipUnmanagedKey); !ok {
		t.Fatalf("a refused collision reported no %s diagnostic: %+v", diag.CodeOwnershipUnmanagedKey, report.Diags)
	}
	// The suppression must not have suppressed the collision report too.
	if found := findDiags(report.Diags, diag.CodeOwnershipStateMissing); len(found) > 0 {
		t.Fatalf("first-run suppression leaked a state-missing warning: %+v", found)
	}
}

// TestDevinPerRootStateDiagnosticsAreDistinguishable attacks diagnostic
// IDENTITY rather than rendering.
//
// devinAggregateCodedDiag stopped overwriting Source.File with the coded error's
// own path (the per-root state file) and now uses the output-relative path.
// Both Devin scopes hold a file called mcp_config.json, so two warnings about
// two DIFFERENT files under two DIFFERENT roots are identical in
// (Code, Target, Resource, Source) and are told apart ONLY by their message.
//
// That is safe for every consumer in this repo (doctor keys on the rendered
// line; sync dedups warnings by message), and it makes the ownership codes
// consistent with every other Devin diagnostic, which has always been keyed by
// the bare rel. It is NOT safe for a consumer that dedups on code+resource+
// source alone: such a consumer will drop one of the two roots' warnings.
//
// This test pins both halves: the messages must differ (the property everything
// actually relies on), and the identity-only ambiguity is asserted so that a
// consumer author sees it here rather than in production.
func TestDevinPerRootStateDiagnosticsAreDistinguishable(t *testing.T) {
	repoRoot, home := devinEnv(t)
	repoDevin := devinProjectRoot(repoRoot)
	userDevin := devinUserRoot(home)
	for _, dir := range []string{repoDevin, userDevin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	resolved := []resolver.ResolvedArtifact{
		devinResolved("repo-mcp", ir.NewMcpServer("repo-mcp", "repo-binary", nil, nil, nil, ir.SourceRef{}), false),
		devinResolved("user-mcp", ir.NewMcpServer("user-mcp", "user-binary", nil, nil, nil, ir.SourceRef{}), true),
	}
	// Seed a committed generation for both roots so the next run's lost records
	// are real, reportable losses rather than a first use.
	if _, err := Run(context.Background(), repoRoot, resolved); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	for _, root := range []string{repoDevin, userDevin} {
		store, err := ownership.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(store.StateFile()); err != nil {
			t.Fatalf("drop record for %s: %v", root, err)
		}
	}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := findDiags(report.Diags, diag.CodeOwnershipStateMissing)
	if len(found) != 2 {
		t.Fatalf("want one %s per root, got %d: %+v", diag.CodeOwnershipStateMissing, len(found), found)
	}
	if found[0].Message == found[1].Message {
		t.Fatalf("two roots produced byte-identical messages, so nothing at all tells them apart: %+v", found)
	}
	for _, d := range found {
		if d.Source.File == "" {
			t.Fatalf("a per-file ownership warning lost its Source: %+v", d)
		}
	}
	// Residual, asserted so it stays visible: identity alone is ambiguous.
	a, b := found[0], found[1]
	if a.Code != b.Code || a.Target != b.Target || a.Resource != b.Resource || a.Source != b.Source {
		t.Fatalf("identity is no longer ambiguous across scopes — this residual is fixed, "+
			"update the comment and drop this assertion:\n  %+v\n  %+v", a, b)
	}
}

// TestDevinRootLevelDiagnosticKeepsASource pins that a diagnostic about the root
// itself (rel == "") still carries a usable Source. The helper no longer falls
// back to the coded error's own path, so this is the one place a Source can now
// be empty.
func TestDevinRootLevelDiagnosticKeepsASource(t *testing.T) {
	key := outputKey{target: "devin"}
	err := diag.NewCodedError(diag.CodeOwnershipStateCorrupt,
		diag.SourceRef{File: "/state/agent-marketplace/ownership/abc/state.json"},
		ownership.ErrStateCorrupt)
	d := devinAggregateCodedDiag(key, "/repo/.devin", "", err, diag.CodeMaterializeDevinAggregateWrite)
	if d.Source.File == "" {
		t.Fatalf("a root-level ownership diagnostic lost its Source entirely; "+
			"nothing in the report can now attribute it to a file or a root: %+v", d)
	}
}

// TestDevinAggregateMessageIsUnambiguous attacks the message shape: the helper
// prepends an absolute aggregate path to an error that already renders its own
// absolute path.
//
// A newline in the path is NOT asserted against: a path may legally contain
// one on POSIX, every Devin diagnostic has always rendered paths, and the
// resulting extra line is cosmetic — no consumer in this repo parses these
// lines back into structure.
func TestDevinAggregateMessageIsUnambiguous(t *testing.T) {
	cases := []struct {
		name, outDir, rel, message string
	}{
		{"format verbs in the path", "/repo/%s/%d/.devin", "mcp_config.json", "mcp_config.json: boom"},
		{"format verbs in the message", "/repo/.devin", "mcp_config.json", "mcp_config.json: bad verb %!d(string=x)"},
		{"colon in the path", "/repo/a:b/.devin", "mcp_config.json", "mcp_config.json: boom"},
		{"unicode in the path", "/repo/ünïcødé/.devin", "mcp_config.json", "mcp_config.json: boom"},
		{"nested rel", "/repo/.devin", "sub/mcp_config.json", "sub/mcp_config.json: boom"},
		{"very long path", "/repo/" + strings.Repeat("d", 4096) + "/.devin", "mcp_config.json", "mcp_config.json: boom"},
		{"empty outDir", "", "mcp_config.json", "mcp_config.json: boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := devinAggregateMessage(tc.outDir, tc.rel, tc.message)
			label := devinAggregateLabel(tc.outDir, tc.rel)
			if !strings.HasPrefix(got, label+": ") {
				t.Fatalf("message does not lead with the aggregate label\n label: %q\n   got: %q", label, got)
			}
			// The rel-only prefix must be gone, so the same path is not stated
			// twice in two different spellings.
			if strings.Contains(strings.TrimPrefix(got, label+": "), tc.rel+": ") {
				t.Fatalf("both the absolute label and the bare rel lead the message: %q", got)
			}
		})
	}
}

// TestDevinAggregateMessageKeepsTheAggregateWhenTheErrorAlreadyNamesIt covers
// the Contains() shortcut. The label is not prepended when the error already
// renders it, which is correct — but the aggregate must still be IN the
// message, because that is the whole contract of the helper.
func TestDevinAggregateMessageKeepsTheAggregateWhenTheErrorAlreadyNamesIt(t *testing.T) {
	outDir, rel := "/repo/.devin", "mcp_config.json"
	label := devinAggregateLabel(outDir, rel)
	got := devinAggregateMessage(outDir, rel, "could not rename staging file over "+label)
	if !strings.Contains(got, label) {
		t.Fatalf("the aggregate identity was lost: %q", got)
	}
}

// TestDevinConcurrentFirstRunsAgainstOneRoot attacks EverRecorded racing
// MkdirAll: two syncs of the same root at once on a first run.
func TestDevinConcurrentFirstRunsAgainstOneRoot(t *testing.T) {
	repoRoot, _ := devinEnv(t)
	resolved := []resolver.ResolvedArtifact{
		devinResolved("managed", ir.NewMcpServer("managed", "managed-binary", nil, nil, nil, ir.SourceRef{}), false),
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = Run(context.Background(), repoRoot, resolved)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Run %d: %v", i, err)
		}
	}
	store, err := ownership.Open(devinProjectRoot(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	rec, loadErr := store.Load()
	if loadErr != nil {
		t.Fatalf("two concurrent first runs left the ownership record unusable: %v", loadErr)
	}
	if len(rec.Files[harness.DevinMcpConfigFile].Entries) != 1 {
		t.Fatalf("record is inconsistent after concurrent first runs: %+v", rec.Files)
	}
}
