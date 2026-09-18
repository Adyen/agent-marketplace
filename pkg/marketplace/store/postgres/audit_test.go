package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/postgres"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func sha256Text(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func TestPutAndGetResourceAudit(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	audit := putResourceAudit(t, ctx, s, "pass", "")

	got, err := s.GetResourceAudit(ctx, audit.ResourceAuditKey, audit.ResultDigest)
	if err != nil {
		t.Fatalf("GetResourceAudit: %v", err)
	}
	if !got.Equal(audit) {
		t.Fatalf("GetResourceAudit returned a different audit: %+v", got)
	}
}

func TestPutResourceAuditIdempotentRetry(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	audit := putResourceAudit(t, ctx, s, "pass", "")

	audit.AssessedAt = audit.AssessedAt.Add(time.Hour)
	if err := s.PutResourceAudit(ctx, audit); err != nil {
		t.Fatalf("PutResourceAudit retry: %v", err)
	}
	got, err := s.GetResourceAudit(ctx, audit.ResourceAuditKey, audit.ResultDigest)
	if err != nil {
		t.Fatalf("GetResourceAudit: %v", err)
	}
	if got.ResultDigest != audit.ResultDigest {
		t.Fatalf("retry changed result digest: %q != %q", got.ResultDigest, audit.ResultDigest)
	}
}

func TestPutResourceAuditTwoDifferentNonErrorConflict(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	audit := putResourceAudit(t, ctx, s, "pass", "")

	second := audit
	second.Summary = "different"
	second.AssessedAt = audit.AssessedAt.Add(time.Hour)
	second.ResultDigest = ""
	if err := s.PutResourceAudit(ctx, second); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("PutResourceAudit of a second non-error result returned %v, want ErrConflict", err)
	}
}

func TestPutResourceAuditErrorThenSuccessCoexist(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	errAudit := putResourceAudit(t, ctx, s, "error", "audit unavailable")

	if _, err := s.GetCachedResourceAudit(ctx, errAudit.ResourceAuditKey); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cache returned error audit: %v", err)
	}

	okAudit := errAudit
	okAudit.Verdict = "pass"
	okAudit.Error = ""
	okAudit.Summary = "recovered"
	okAudit.AssessedAt = errAudit.AssessedAt.Add(time.Hour)
	okAudit.ResultDigest = ""
	okAudit.Duration = 1
	if err := s.PutResourceAudit(ctx, okAudit); err != nil {
		t.Fatalf("PutResourceAudit success: %v", err)
	}

	got, err := s.GetCachedResourceAudit(ctx, okAudit.ResourceAuditKey)
	if err != nil {
		t.Fatalf("GetCachedResourceAudit after success: %v", err)
	}
	if got.Verdict != "pass" {
		t.Fatalf("cache returned verdict %q, want pass", got.Verdict)
	}
}

func TestListAndDetailResourceAuditsForAssessment(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)

	audits := []store.ResourceAudit{
		putResourceAuditForAssessment(t, ctx, s, assessment.AssessmentDigest, 0, "marketplace.hidden-content"),
		putResourceAuditForAssessment(t, ctx, s, assessment.AssessmentDigest, 1, "marketplace.assets"),
	}

	list, err := s.ListResourceAuditsForAssessment(ctx, assessment.AssessmentDigest)
	if err != nil {
		t.Fatalf("ListResourceAuditsForAssessment: %v", err)
	}
	if len(list) != len(audits) {
		t.Fatalf("List returned %d audits, want %d", len(list), len(audits))
	}
	for i, want := range audits {
		if !list[i].Equal(want) {
			t.Fatalf("List[%d] = %+v, want %+v", i, list[i], want)
		}
	}

	detail, err := s.GetResourceAuditForAssessment(ctx, assessment.AssessmentDigest, audits[1].AnalyzerID)
	if err != nil {
		t.Fatalf("GetResourceAuditForAssessment: %v", err)
	}
	if !detail.Equal(audits[1]) {
		t.Fatalf("GetResourceAuditForAssessment returned a different audit: %+v", detail)
	}
}

func TestListResourceAuditsForAssessmentEmpty(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)

	list, err := s.ListResourceAuditsForAssessment(ctx, assessment.AssessmentDigest)
	if err != nil {
		t.Fatalf("ListResourceAuditsForAssessment: %v", err)
	}
	if list == nil {
		t.Fatal("List returned nil for an empty list")
	}
	if len(list) != 0 {
		t.Fatalf("List returned %d audits for an unlinked assessment, want 0", len(list))
	}
}

func TestLinkResourceAssessmentAuditConflicts(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)

	first := putResourceAuditForAssessment(t, ctx, s, assessment.AssessmentDigest, 0, "marketplace.hidden-content")
	firstLink := store.AssessmentAuditLink{
		AssessmentDigest: assessment.AssessmentDigest,
		Ordinal:          0,
		AuditKey:         first.ResourceAuditKey,
		ResultDigest:     first.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, firstLink); err != nil {
		t.Fatalf("LinkResourceAssessmentAudit first: %v", err)
	}
	if err := s.LinkResourceAssessmentAudit(ctx, firstLink); err != nil {
		t.Fatalf("LinkResourceAssessmentAudit retry: %v", err)
	}

	second := putResourceAuditForAssessment(t, ctx, s, assessment.AssessmentDigest, 1, "marketplace.assets")
	conflict := store.AssessmentAuditLink{
		AssessmentDigest: assessment.AssessmentDigest,
		Ordinal:          0,
		AuditKey:         second.ResourceAuditKey,
		ResultDigest:     second.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, conflict); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate ordinal returned %v, want ErrConflict", err)
	}

	dupAnalyzer := store.AssessmentAuditLink{
		AssessmentDigest: assessment.AssessmentDigest,
		Ordinal:          2,
		AuditKey:         first.ResourceAuditKey,
		ResultDigest:     first.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, dupAnalyzer); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate analyzer returned %v, want ErrConflict", err)
	}
}

func putResourceAudit(t *testing.T, ctx context.Context, s *postgres.Store, verdict, errorText string) store.ResourceAudit {
	t.Helper()
	audit := store.ResourceAudit{
		ResourceAuditKey: store.ResourceAuditKey{
			Coordinate: store.ResourceCoordinate{
				Marketplace: "marketplace",
				Registry:    "registry",
				Kind:        "skill",
				Name:        "review",
			},
			SubjectDigest:   store.RevisionDigest(sha256Text("a")),
			AnalyzerID:      "marketplace.hidden-content",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest(sha256Text("b")),
		},
		Role:       "gating",
		Verdict:    verdict,
		Summary:    "summary",
		Findings:   []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "ok"}},
		Duration:   123 * time.Millisecond,
		Error:      errorText,
		AssessedAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
	if verdict == "error" {
		audit.Summary = "audit unavailable"
		audit.Findings = nil
	}
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := s.PutResourceAudit(ctx, finalized); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}
	got, err := s.GetResourceAudit(ctx, finalized.ResourceAuditKey, finalized.ResultDigest)
	if err != nil {
		t.Fatalf("GetResourceAudit: %v", err)
	}
	return got
}

func putResourceAssessment(t *testing.T, ctx context.Context, s *postgres.Store) store.ResourceAssessment {
	t.Helper()
	coord := store.ResourceCoordinate{
		Marketplace: "marketplace",
		Registry:    "registry",
		Kind:        "skill",
		Name:        "review",
	}
	contentDigest := store.RevisionDigest(sha256Text("c"))
	if err := s.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: coord, ContentDigest: contentDigest}); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	assessment := store.ResourceAssessment{
		Coordinate:               coord,
		ContentDigest:            contentDigest,
		AnalyzerVersion:          "1.0.0",
		PolicyDigest:             store.PolicyDigest(sha256Text("d")),
		Decision:                 store.AssessmentDecisionAllow,
		Findings:                 []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "ok"}},
		InspectionManifestDigest: store.RevisionDigest(sha256Text("e")),
		AssessedAt:               time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
	finalized, err := store.FinalizeResourceAssessment(assessment)
	if err != nil {
		t.Fatalf("FinalizeResourceAssessment: %v", err)
	}
	if err := s.PutResourceAssessment(ctx, finalized); err != nil {
		t.Fatalf("PutResourceAssessment: %v", err)
	}
	got, err := s.GetResourceAssessment(ctx, finalized.AssessmentDigest)
	if err != nil {
		t.Fatalf("GetResourceAssessment: %v", err)
	}
	return got
}

func putResourceAuditForAssessment(t *testing.T, ctx context.Context, s *postgres.Store, assessmentDigest store.AssessmentDigest, ordinal int, analyzerID string) store.ResourceAudit {
	t.Helper()
	audit := store.ResourceAudit{
		ResourceAuditKey: store.ResourceAuditKey{
			Coordinate: store.ResourceCoordinate{
				Marketplace: "marketplace",
				Registry:    "registry",
				Kind:        "skill",
				Name:        "review",
			},
			SubjectDigest:   store.RevisionDigest(sha256Text("a")),
			AnalyzerID:      analyzerID,
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest(sha256Text("b")),
		},
		Role:       "gating",
		Verdict:    "pass",
		Summary:    "ok",
		Findings:   []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "ok"}},
		Duration:   time.Duration(ordinal+1) * time.Millisecond,
		AssessedAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := s.PutResourceAudit(ctx, finalized); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}
	link := store.AssessmentAuditLink{
		AssessmentDigest: assessmentDigest,
		Ordinal:          ordinal,
		AuditKey:         finalized.ResourceAuditKey,
		ResultDigest:     finalized.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, link); err != nil {
		t.Fatalf("LinkResourceAssessmentAudit: %v", err)
	}
	return finalized
}

func TestGetResourceAuditRejectsInvalidKey(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	_, err := s.GetResourceAudit(ctx, store.ResourceAuditKey{}, store.AuditDigest(sha256Text("x")))
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetResourceAudit invalid key returned %v, want validation error", err)
	}
}

func TestGetCachedResourceAuditRejectsInvalidKey(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	_, err := s.GetCachedResourceAudit(ctx, store.ResourceAuditKey{})
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetCachedResourceAudit invalid key returned %v, want validation error", err)
	}
}

func TestListResourceAuditsForAssessmentRejectsInvalidDigest(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	_, err := s.ListResourceAuditsForAssessment(ctx, store.AssessmentDigest("sha256:bad"))
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListResourceAuditsForAssessment invalid digest returned %v, want validation error", err)
	}
}

func TestGetResourceAuditForAssessmentRejectsInvalidInputs(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)
	_, err := s.GetResourceAuditForAssessment(ctx, store.AssessmentDigest("sha256:bad"), "marketplace.hidden-content")
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetResourceAuditForAssessment invalid digest returned %v, want validation error", err)
	}
	_, err = s.GetResourceAuditForAssessment(ctx, assessment.AssessmentDigest, "Analyzer!")
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetResourceAuditForAssessment invalid analyzer id returned %v, want validation error", err)
	}
}

func TestLinkResourceAssessmentAuditMissingAssessment(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	audit := putResourceAudit(t, ctx, s, "pass", "")
	link := store.AssessmentAuditLink{
		AssessmentDigest: store.AssessmentDigest(sha256Text("d")),
		Ordinal:          0,
		AuditKey:         audit.ResourceAuditKey,
		ResultDigest:     audit.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, link); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing assessment returned %v, want ErrNotFound", err)
	}
}

func TestLinkResourceAssessmentAuditMissingAudit(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)
	link := store.AssessmentAuditLink{
		AssessmentDigest: assessment.AssessmentDigest,
		Ordinal:          0,
		AuditKey: store.ResourceAuditKey{
			Coordinate:      assessment.Coordinate,
			SubjectDigest:   store.RevisionDigest(sha256Text("d")),
			AnalyzerID:      "marketplace.hidden-content",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest(sha256Text("b")),
		},
		ResultDigest: store.AuditDigest(sha256Text("2")),
	}
	if err := s.LinkResourceAssessmentAudit(ctx, link); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing audit returned %v, want ErrNotFound", err)
	}
}

func TestLinkResourceAssessmentAuditCrossCoordinateConflict(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	assessment := putResourceAssessment(t, ctx, s)

	otherCoord := store.ResourceCoordinate{
		Marketplace: "marketplace",
		Registry:    "registry",
		Kind:        "skill",
		Name:        "other",
	}
	if err := s.PutResourceRevision(ctx, store.ResourceRevision{Coordinate: otherCoord, ContentDigest: store.RevisionDigest(sha256Text("c"))}); err != nil {
		t.Fatalf("PutResourceRevision: %v", err)
	}
	otherAudit := putResourceAuditWithCoordinate(t, ctx, s, otherCoord)

	link := store.AssessmentAuditLink{
		AssessmentDigest: assessment.AssessmentDigest,
		Ordinal:          0,
		AuditKey:         otherAudit.ResourceAuditKey,
		ResultDigest:     otherAudit.ResultDigest,
	}
	if err := s.LinkResourceAssessmentAudit(ctx, link); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cross-coordinate link returned %v, want ErrConflict", err)
	}

	list, err := s.ListResourceAuditsForAssessment(ctx, assessment.AssessmentDigest)
	if err != nil {
		t.Fatalf("ListResourceAuditsForAssessment: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("failed write changed list: got %d, want 0", len(list))
	}
}

func putResourceAuditWithCoordinate(t *testing.T, ctx context.Context, s *postgres.Store, coord store.ResourceCoordinate) store.ResourceAudit {
	t.Helper()
	audit := store.ResourceAudit{
		ResourceAuditKey: store.ResourceAuditKey{
			Coordinate:      coord,
			SubjectDigest:   store.RevisionDigest(sha256Text("a")),
			AnalyzerID:      "marketplace.hidden-content",
			AnalyzerVersion: "1.0.0",
			ConfigDigest:    store.PolicyDigest(sha256Text("b")),
		},
		Role:       "gating",
		Verdict:    "pass",
		Summary:    "ok",
		Findings:   []store.Finding{{Code: "safe", Severity: "info", Policy: "allow", Message: "ok"}},
		Duration:   1 * time.Millisecond,
		AssessedAt: time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC),
	}
	finalized, err := store.FinalizeResourceAudit(audit)
	if err != nil {
		t.Fatalf("FinalizeResourceAudit: %v", err)
	}
	if err := s.PutResourceAudit(ctx, finalized); err != nil {
		t.Fatalf("PutResourceAudit: %v", err)
	}
	return finalized
}

func TestScanRejectsCorruptResourceAuditRow(t *testing.T) {
	dsn := testDSN(t)
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	ctx := context.Background()
	s, schema := newTestStore(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open direct db: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	coord := store.ResourceCoordinate{
		Marketplace: "marketplace",
		Registry:    "registry",
		Kind:        "skill",
		Name:        "review",
	}
	subject := store.RevisionDigest(sha256Text("a"))
	config := store.PolicyDigest(sha256Text("b"))
	result := store.AuditDigest(sha256Text("c"))

	_, err = db.ExecContext(ctx, `INSERT INTO resource_audits
		(marketplace_id, registry_id, kind, name, subject_digest, analyzer_id, analyzer_version, config_digest,
		 result_digest, role, verdict, summary, findings, duration_ns, error, assessed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, subject,
		"marketplace.hidden-content", "1.0.0", config, result,
		"corrupt", "pass", "ok", "[]", 1, "", time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC).UnixMicro())
	if err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	key := store.ResourceAuditKey{
		Coordinate:      coord,
		SubjectDigest:   subject,
		AnalyzerID:      "marketplace.hidden-content",
		AnalyzerVersion: "1.0.0",
		ConfigDigest:    config,
	}
	_, err = s.GetResourceAudit(ctx, key, result)
	if err == nil || errors.Is(err, store.ErrNotFound) {
		t.Fatalf("corrupt row returned %v, want validation error", err)
	}
}
