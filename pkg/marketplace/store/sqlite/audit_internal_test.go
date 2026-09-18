package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func sha256TextInternal(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}

func TestScanRejectsCorruptResourceAuditRow(t *testing.T) {
	ctx := context.Background()
	s := New(filepath.Join(t.TempDir(), "corrupt.db"))
	if err := s.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	coord := store.ResourceCoordinate{
		Marketplace: "marketplace",
		Registry:    "registry",
		Kind:        "skill",
		Name:        "review",
	}
	subject := store.RevisionDigest(sha256TextInternal("a"))
	config := store.PolicyDigest(sha256TextInternal("b"))
	result := store.AuditDigest(sha256TextInternal("c"))

	_, err := s.q.ExecContext(ctx, `INSERT INTO resource_audits
		(marketplace_id, registry_id, kind, name, subject_digest, analyzer_id, analyzer_version, config_digest,
		 result_digest, role, verdict, summary, findings, duration_ns, error, assessed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		coord.Marketplace, coord.Registry, coord.Kind, coord.Name, subject,
		"marketplace.hidden-content", "1.0.0", config, result,
		"corrupt", "pass", "ok", "[]", 1, "", timeToMicro(time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)))
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
