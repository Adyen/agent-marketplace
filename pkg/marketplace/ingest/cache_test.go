package ingest

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/artifact"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

type countingAnalyzer struct {
	id      string
	version string
	kinds   []ir.ResourceKind
	called  int
	audit   analyze.Audit
}

func (a *countingAnalyzer) ID() string               { return a.id }
func (a *countingAnalyzer) Version() string          { return a.version }
func (a *countingAnalyzer) Kinds() []ir.ResourceKind { return a.kinds }
func (a *countingAnalyzer) Analyze(context.Context, analyze.Subject, analyze.AnalyzerConfig) (analyze.Audit, error) {
	a.called++
	return a.audit, nil
}

func TestCacheAwareAnalyze(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(db)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	primaryPath := "SKILL.md"
	body := []byte("# review\n")
	files := analyze.FileSet{}
	subjectDigest := auditSubjectDigest(contentDigest, files, analyze.DefaultLimits())

	makeSet := func(configDigest store.PolicyDigest) resolvedAnalyzerSet {
		return resolvedAnalyzerSet{
			Entries: []resolvedAnalyzer{{
				Registration: analyze.Registration{
					Analyzer: &countingAnalyzer{
						id:      "marketplace.test",
						version: "1.0.0",
						kinds:   []ir.ResourceKind{ir.KindSkill},
						audit: analyze.Audit{
							Verdict: analyze.VerdictPass,
							Summary: "ok",
						},
					},
					Role: analyze.RoleGating,
				},
				ConfigDigest: configDigest,
			}},
		}
	}

	t.Run("cache hit skips analyzer", func(t *testing.T) {
		configDigest := store.PolicyDigest("sha256:" + strings.Repeat("b", 64))
		key := store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(subjectDigest),
			AnalyzerID:      "marketplace.test",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    configDigest,
		}
		audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
			ResourceAuditKey: key,
			Role:             analyze.RoleGating,
			Verdict:          analyze.VerdictPass,
			Summary:          "cached",
			Duration:         time.Millisecond,
			AssessedAt:       time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("FinalizeResourceAudit: %v", err)
		}
		if err := meta.PutResourceAudit(ctx, audit); err != nil {
			t.Fatalf("PutResourceAudit: %v", err)
		}

		set := makeSet(configDigest)
		analyzer := set.Entries[0].Registration.Analyzer.(*countingAnalyzer)
		var run auditRun
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
		subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}
		report, err := fn(ctx, subject, contentDigest)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if analyzer.called != 0 {
			t.Fatalf("analyzer called %d times, want 0", analyzer.called)
		}
		if len(report.Audits) != 1 {
			t.Fatalf("got %d audits, want 1", len(report.Audits))
		}
		if report.Audits[0].AnalyzerID != "marketplace.test" {
			t.Fatalf("audit analyzer = %s, want marketplace.test", report.Audits[0].AnalyzerID)
		}
		if len(run.audits) != 1 || run.audits[0].ResourceAuditKey != key {
			t.Fatalf("run audit key mismatch: got %v, want %v", run.audits, key)
		}
	})

	t.Run("cache miss runs analyzer", func(t *testing.T) {
		configDigest := store.PolicyDigest("sha256:" + strings.Repeat("c", 64))
		set := makeSet(configDigest)
		analyzer := set.Entries[0].Registration.Analyzer.(*countingAnalyzer)
		var run auditRun
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
		subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}
		report, err := fn(ctx, subject, contentDigest)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if analyzer.called != 1 {
			t.Fatalf("analyzer called %d times, want 1", analyzer.called)
		}
		if len(report.Audits) != 1 {
			t.Fatalf("got %d audits, want 1", len(report.Audits))
		}
		if report.Audits[0].AnalyzerID != "marketplace.test" {
			t.Fatalf("audit analyzer = %s, want marketplace.test", report.Audits[0].AnalyzerID)
		}
		if len(run.audits) != 1 {
			t.Fatalf("run.audits = %d, want 1", len(run.audits))
		}
		key := store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(subjectDigest),
			AnalyzerID:      "marketplace.test",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    configDigest,
		}
		if run.audits[0].ResourceAuditKey != key {
			t.Fatalf("run audit key = %v, want %v", run.audits[0].ResourceAuditKey, key)
		}
	})

	t.Run("changed config digest is a miss", func(t *testing.T) {
		// Pre-seed a hit for config digest b.
		configDigestB := store.PolicyDigest("sha256:" + strings.Repeat("b", 64))
		keyB := store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(subjectDigest),
			AnalyzerID:      "marketplace.test",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    configDigestB,
		}
		audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
			ResourceAuditKey: keyB,
			Role:             analyze.RoleGating,
			Verdict:          analyze.VerdictPass,
			Summary:          "cached",
			Duration:         time.Millisecond,
			AssessedAt:       time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("FinalizeResourceAudit: %v", err)
		}
		if err := meta.PutResourceAudit(ctx, audit); err != nil {
			t.Fatalf("PutResourceAudit: %v", err)
		}

		// Now ask for a different config digest.
		configDigestC := store.PolicyDigest("sha256:" + strings.Repeat("d", 64))
		set := makeSet(configDigestC)
		analyzer := set.Entries[0].Registration.Analyzer.(*countingAnalyzer)
		var run auditRun
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
		subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}
		if _, err := fn(ctx, subject, contentDigest); err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if analyzer.called != 1 {
			t.Fatalf("analyzer called %d times, want 1 for changed config digest", analyzer.called)
		}
	})

	t.Run("changed support files change subject digest and miss", func(t *testing.T) {
		// Seed a hit for the empty support file set.
		configDigest := store.PolicyDigest("sha256:" + strings.Repeat("e", 64))
		key := store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(subjectDigest),
			AnalyzerID:      "marketplace.test",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    configDigest,
		}
		audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
			ResourceAuditKey: key,
			Role:             analyze.RoleGating,
			Verdict:          analyze.VerdictPass,
			Summary:          "cached",
			Duration:         time.Millisecond,
			AssessedAt:       time.Unix(1, 0).UTC(),
		})
		if err != nil {
			t.Fatalf("FinalizeResourceAudit: %v", err)
		}
		if err := meta.PutResourceAudit(ctx, audit); err != nil {
			t.Fatalf("PutResourceAudit: %v", err)
		}

		changedFiles := analyze.FileSet{"notes.md": []byte("changed")}
		set := resolvedAnalyzerSet{
			Entries: []resolvedAnalyzer{{
				Registration: analyze.Registration{
					Analyzer: &countingAnalyzer{
						id:      "marketplace.test",
						version: "1.0.0",
						kinds:   []ir.ResourceKind{ir.KindSkill},
						audit: analyze.Audit{
							Verdict: analyze.VerdictPass,
							Summary: "ok",
						},
					},
					Role: analyze.RoleGating,
				},
				ConfigDigest: configDigest,
			}},
		}
		analyzer := set.Entries[0].Registration.Analyzer.(*countingAnalyzer)
		var run auditRun
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, changedFiles, &run)
		subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: changedFiles, Limits: analyze.DefaultLimits()}
		if _, err := fn(ctx, subject, contentDigest); err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if analyzer.called != 1 {
			t.Fatalf("analyzer called %d times, want 1 for changed support files", analyzer.called)
		}
	})

	t.Run("cache miss does not write returned audit", func(t *testing.T) {
		configDigest := store.PolicyDigest("sha256:" + strings.Repeat("f", 64))
		key := store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(subjectDigest),
			AnalyzerID:      "marketplace.test",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    configDigest,
		}
		set := makeSet(configDigest)
		var run auditRun
		subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
		if _, err := fn(ctx, subject, contentDigest); err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if len(run.audits) != 1 {
			t.Fatalf("run.audits = %d, want 1", len(run.audits))
		}
		if _, err := meta.GetCachedResourceAudit(ctx, key); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expected cached audit to be absent before publication, got err=%v", err)
		}
		if _, err := meta.GetResourceAudit(ctx, key, run.audits[0].ResultDigest); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expected returned audit to be absent from metadata store, got err=%v", err)
		}
	})
}

func TestCacheAwareAnalyzeLimitsChangeMissesCache(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(db)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	primaryPath := "SKILL.md"
	body := []byte("# review\n")
	files := analyze.FileSet{}
	configDigest := store.PolicyDigest("sha256:" + strings.Repeat("b", 64))

	// Seed a hit produced under the default limits.
	defaultLimits := analyze.DefaultLimits()
	key := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   store.RevisionDigest(auditSubjectDigest(contentDigest, files, defaultLimits)),
		AnalyzerID:      "marketplace.test",
		AnalyzerVersion: "1.0.0",
		ConfigDigest:    configDigest,
	}
	audit, err := store.FinalizeResourceAudit(store.ResourceAudit{
		ResourceAuditKey: key,
		Role:             analyze.RoleGating,
		Verdict:          analyze.VerdictPass,
		Summary:          "cached",
		Duration:         time.Millisecond,
		AssessedAt:       time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := meta.PutResourceAudit(ctx, audit); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}

	analyzer := &countingAnalyzer{
		id:      "marketplace.test",
		version: "1.0.0",
		kinds:   []ir.ResourceKind{ir.KindSkill},
		audit:   analyze.Audit{Verdict: analyze.VerdictPass, Summary: "ok"},
	}
	set := resolvedAnalyzerSet{
		Entries: []resolvedAnalyzer{{
			Registration: analyze.Registration{Analyzer: analyzer, Role: analyze.RoleGating},
			ConfigDigest: configDigest,
		}},
	}

	// Identical content but a changed MaxFindings must miss the cache.
	changedLimits := defaultLimits
	changedLimits.MaxFindings = 5
	var run auditRun
	fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
	subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: changedLimits}
	if _, err := fn(ctx, subject, contentDigest); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if analyzer.called != 1 {
		t.Fatalf("analyzer called %d times, want 1 for changed MaxFindings", analyzer.called)
	}
	if len(run.audits) != 1 || run.audits[0].ResourceAuditKey == key {
		t.Fatalf("expected a distinct audit key under changed limits, got %v", run.audits)
	}

	// The same limits again hit the freshly stored shape.
	if err := meta.PutResourceAudit(ctx, run.audits[0]); err != nil {
		t.Fatalf("PutResourceAudit changed limits: %v", err)
	}
	var run2 auditRun
	fn2 := cacheAwareAnalyze(meta, coord, set, time.Unix(3, 0).UTC(), primaryPath, body, files, &run2)
	if _, err := fn2(ctx, subject, contentDigest); err != nil {
		t.Fatalf("Analyze second run: %v", err)
	}
	if analyzer.called != 1 {
		t.Fatalf("analyzer called %d times, want still 1 for unchanged limits", analyzer.called)
	}
}

func TestCacheAwareAnalyzeMixedHitMiss(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(db)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	primaryPath := "SKILL.md"
	body := []byte("# review\n")
	files := analyze.FileSet{}
	subjectDigest := auditSubjectDigest(contentDigest, files, analyze.DefaultLimits())

	newAnalyzer := func(id, version string, audit analyze.Audit) *countingAnalyzer {
		return &countingAnalyzer{
			id:      id,
			version: version,
			kinds:   []ir.ResourceKind{ir.KindSkill},
			audit:   audit,
		}
	}

	missA := newAnalyzer("marketplace.miss-a", "1.0.0", analyze.Audit{Verdict: analyze.VerdictPass, Summary: "ok"})
	hitA := newAnalyzer("marketplace.hit", "1.0.0", analyze.Audit{Verdict: analyze.VerdictPass, Summary: "cached"})
	missB := newAnalyzer("marketplace.miss-b", "1.0.0", analyze.Audit{Verdict: analyze.VerdictPass, Summary: "ok"})

	hitConfig := store.PolicyDigest("sha256:" + strings.Repeat("c", 64))
	hitKey := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   store.RevisionDigest(subjectDigest),
		AnalyzerID:      hitA.id,
		AnalyzerVersion: hitA.version,
		ConfigDigest:    hitConfig,
	}
	hitAudit, err := store.FinalizeResourceAudit(store.ResourceAudit{
		ResourceAuditKey: hitKey,
		Role:             analyze.RoleGating,
		Verdict:          analyze.VerdictPass,
		Summary:          "cached",
		Duration:         time.Millisecond,
		AssessedAt:       time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := meta.PutResourceAudit(ctx, hitAudit); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}

	set := resolvedAnalyzerSet{
		Entries: []resolvedAnalyzer{
			{Registration: analyze.Registration{Analyzer: missA, Role: analyze.RoleGating}, ConfigDigest: store.PolicyDigest("sha256:" + strings.Repeat("b", 64))},
			{Registration: analyze.Registration{Analyzer: hitA, Role: analyze.RoleGating}, ConfigDigest: hitConfig},
			{Registration: analyze.Registration{Analyzer: missB, Role: analyze.RoleGating}, ConfigDigest: store.PolicyDigest("sha256:" + strings.Repeat("d", 64))},
		},
	}

	var run auditRun
	subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}
	fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
	report, err := fn(ctx, subject, contentDigest)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if missA.called != 1 || hitA.called != 0 || missB.called != 1 {
		t.Fatalf("call counts missA=%d hitA=%d missB=%d, want 1,0,1", missA.called, hitA.called, missB.called)
	}
	wantIDs := []string{"marketplace.miss-a", "marketplace.hit", "marketplace.miss-b"}
	if len(report.Audits) != len(wantIDs) {
		t.Fatalf("report audits = %d, want %d", len(report.Audits), len(wantIDs))
	}
	for i, want := range wantIDs {
		if report.Audits[i].AnalyzerID != want {
			t.Fatalf("report audit[%d].AnalyzerID = %s, want %s", i, report.Audits[i].AnalyzerID, want)
		}
	}
	if len(run.audits) != len(wantIDs) {
		t.Fatalf("run.audits = %d, want %d", len(run.audits), len(wantIDs))
	}
	if run.audits[1].ResourceAuditKey != hitKey {
		t.Fatalf("run audit[1] key = %v, want %v", run.audits[1].ResourceAuditKey, hitKey)
	}
}

func TestCacheAwareAnalyzeErrorAudit(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "marketplace.db")
	meta := sqlite.New(db)
	if err := meta.Open(ctx); err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "src", Kind: "skill", Name: "review"}
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	primaryPath := "SKILL.md"
	body := []byte("# review\n")
	files := analyze.FileSet{}
	subjectDigest := auditSubjectDigest(contentDigest, files, analyze.DefaultLimits())
	configDigest := store.PolicyDigest("sha256:" + strings.Repeat("f", 64))

	key := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   store.RevisionDigest(subjectDigest),
		AnalyzerID:      "marketplace.test",
		AnalyzerVersion: "1.0.0",
		ConfigDigest:    configDigest,
	}

	preseeded, err := store.FinalizeResourceAudit(store.ResourceAudit{
		ResourceAuditKey: key,
		Role:             analyze.RoleGating,
		Verdict:          analyze.VerdictError,
		Summary:          "audit unavailable",
		Error:            "preseeded error",
		Findings: []store.Finding{
			{Code: analyze.PolicyAuditUnavailable, Message: "preseeded", Severity: analyze.OutcomeWarn, Consentable: new(bool)},
		},
		Duration:   time.Millisecond,
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := meta.PutResourceAudit(ctx, preseeded); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}

	errorMsg := "analyzer failed intentionally"
	a := &countingAnalyzer{
		id:      "marketplace.test",
		version: "1.0.0",
		kinds:   []ir.ResourceKind{ir.KindSkill},
		audit:   analyze.Audit{Verdict: analyze.VerdictError, Error: errorMsg},
	}
	set := resolvedAnalyzerSet{
		Entries: []resolvedAnalyzer{{
			Registration: analyze.Registration{Analyzer: a, Role: analyze.RoleGating},
			ConfigDigest: configDigest,
		}},
	}
	subject := analyze.Subject{Resource: ir.NewSkill("review", "Review", "ok", nil, nil, ir.SourceRef{}), Files: files, Limits: analyze.DefaultLimits()}

	run := func(expected int) (analyze.Report, auditRun) {
		var run auditRun
		fn := cacheAwareAnalyze(meta, coord, set, time.Unix(2, 0).UTC(), primaryPath, body, files, &run)
		report, err := fn(ctx, subject, contentDigest)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if a.called != expected {
			t.Fatalf("analyzer called %d times, want %d", a.called, expected)
		}
		return report, run
	}

	// A stored error audit is never a hit, so the first execution runs the analyzer.
	report1, run1 := run(1)
	if len(report1.Audits) != 1 || report1.Audits[0].Verdict != analyze.VerdictError {
		t.Fatalf("expected one error verdict, got %+v", report1.Audits)
	}
	if len(run1.audits) != 1 || run1.audits[0].Verdict != analyze.VerdictError {
		t.Fatalf("expected returned error audit retained for publication, got %+v", run1.audits)
	}
	if len(run1.audits[0].Findings) != 1 || run1.audits[0].Findings[0].Consentable == nil || *run1.audits[0].Findings[0].Consentable {
		t.Fatalf("expected non-consentable gating finding, got %+v", run1.audits[0].Findings)
	}
	if run1.audits[0].Findings[0].Code != analyze.PolicyAuditUnavailable {
		t.Fatalf("expected code %s, got %s", analyze.PolicyAuditUnavailable, run1.audits[0].Findings[0].Code)
	}
	if strings.Contains(run1.audits[0].Findings[0].Message, errorMsg) {
		t.Fatalf("finding message must not contain raw error %q, got %q", errorMsg, run1.audits[0].Findings[0].Message)
	}
	if _, err := meta.GetCachedResourceAudit(ctx, key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no cached hit for error audit, got err=%v", err)
	}

	// The preseeded error audit still exists but is not a hit.
	gotPreseeded, err := meta.GetResourceAudit(ctx, key, preseeded.ResultDigest)
	if err != nil {
		t.Fatalf("GetResourceAudit preseeded: %v", err)
	}
	if gotPreseeded.Error != "preseeded error" {
		t.Fatalf("preseeded error audit changed: %v", gotPreseeded)
	}

	// A second error execution is also a miss because the returned error is not stored.
	run(2)

	// Switch the analyzer to pass and store the success audit.
	a.audit = analyze.Audit{Verdict: analyze.VerdictPass, Summary: "ok"}
	_, run3 := run(3)
	if len(run3.audits) != 1 || run3.audits[0].Verdict != analyze.VerdictPass {
		t.Fatalf("expected pass audit, got %+v", run3.audits)
	}
	if err := meta.PutResourceAudit(ctx, run3.audits[0]); err != nil {
		t.Fatalf("PutResourceAudit success: %v", err)
	}

	// The next pass execution is a hit and the earlier error audit still coexists.
	report4, run4 := run(3)
	if len(run4.audits) != 1 || run4.audits[0].Verdict != analyze.VerdictPass {
		t.Fatalf("expected cached pass audit, got %+v", run4.audits)
	}
	if run4.audits[0].ResourceAuditKey != run3.audits[0].ResourceAuditKey {
		t.Fatalf("cached audit key changed")
	}
	if run4.audits[0].ResultDigest != run3.audits[0].ResultDigest {
		t.Fatalf("cached audit result digest changed")
	}
	if report4.Audits[0].Verdict != analyze.VerdictPass {
		t.Fatalf("report audit verdict = %s, want pass", report4.Audits[0].Verdict)
	}
	cached, err := meta.GetCachedResourceAudit(ctx, key)
	if err != nil {
		t.Fatalf("GetCachedResourceAudit: %v", err)
	}
	if cached.ResultDigest != run3.audits[0].ResultDigest {
		t.Fatalf("cached result digest = %s, want %s", cached.ResultDigest, run3.audits[0].ResultDigest)
	}
	if _, err := meta.GetResourceAudit(ctx, key, preseeded.ResultDigest); err != nil {
		t.Fatalf("coexisting error audit should still be retrievable: %v", err)
	}
}

func TestCanonicalSubjectDigest(t *testing.T) {
	contentDigest := "sha256:" + strings.Repeat("a", 64)

	baseFiles := analyze.FileSet{
		"notes.md": []byte("notes"),
		"data.csv": []byte("data"),
	}
	base := artifact.SubjectDigest(contentDigest, baseFiles)

	if !isCanonicalDigest(base) {
		t.Fatalf("base digest is not canonical: %q", base)
	}

	t.Run("map insertion order does not matter", func(t *testing.T) {
		// Populate the same logical set in opposite insertion orders.
		first := make(analyze.FileSet)
		first["z.md"] = []byte("zulu")
		first["a.md"] = []byte("alpha")

		second := make(analyze.FileSet)
		second["a.md"] = []byte("alpha")
		second["z.md"] = []byte("zulu")

		if d1, d2 := artifact.SubjectDigest(contentDigest, first), artifact.SubjectDigest(contentDigest, second); d1 != d2 {
			t.Fatalf("insertion order changed digest: %q vs %q", d1, d2)
		}
	})

	t.Run("content digest changes digest", func(t *testing.T) {
		changed := "sha256:" + strings.Repeat("b", 64)
		if d := artifact.SubjectDigest(changed, baseFiles); d == base {
			t.Fatalf("content digest change did not change subject digest")
		}
	})

	t.Run("support path changes digest", func(t *testing.T) {
		moved := analyze.FileSet{"renamed.md": []byte("notes")}
		if d := artifact.SubjectDigest(contentDigest, moved); d == base {
			t.Fatalf("support path change did not change subject digest")
		}
	})

	t.Run("support bytes change digest", func(t *testing.T) {
		changed := analyze.FileSet{"notes.md": []byte("altered"), "data.csv": []byte("data")}
		if d := artifact.SubjectDigest(contentDigest, changed); d == base {
			t.Fatalf("support bytes change did not change subject digest")
		}
	})
}

func isCanonicalDigest(s string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	hex := s[len(prefix):]
	if len(hex) != 64 {
		return false
	}
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		if !isLowerHex(c) {
			return false
		}
	}
	return true
}

func isLowerHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
