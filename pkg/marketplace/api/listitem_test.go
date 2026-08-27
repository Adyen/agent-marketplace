package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/fsblob"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store/sqlite"
)

const listItemDescription = "Analyze user retention and engagement patterns by cohort."

func digestOf(c string) string { return "sha256:" + strings.Repeat(c, 64) }

// searchListItems drives the real search endpoint and returns the decoded list
// items, so assertions cover the wire format rather than the DTO helper alone.
func searchListItems(t *testing.T, src store.Source, revs ...store.ResourceRevision) []ResourceListItem {
	t.Helper()
	return searchListItemsWithDecision(t, src, store.AssessmentDecisionAllow, revs...)
}

func searchListItemsWithDecision(t *testing.T, src store.Source, decision store.AssessmentDecision, revs ...store.ResourceRevision) []ResourceListItem {
	t.Helper()
	ctx := context.Background()

	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })

	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	for _, rev := range revs {
		publishAssessedTestRevision(ctx, t, meta, rev, decision, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))
	}

	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("search status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Items []ResourceListItem `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode search response: %v", err)
	}
	return result.Items
}

func mirrorSource() store.Source {
	return store.Source{ID: "src", Slug: "src", Listed: true, Acquisition: store.AcquisitionMirror}
}

func skillRevision(name, description string) store.ResourceRevision {
	return store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: KindSkill, Name: name},
		ContentDigest: store.RevisionDigest(digestOf("a")),
		ArchiveDigest: store.ArchiveDigest(digestOf("b")),
		Description:   description,
	}
}

// This is the regression test for the reported symptom: every resource showed
// "No description provided for this resource." because the list endpoint never
// served one.
func TestSearchServesResourceDescription(t *testing.T) {
	items := searchListItems(t, mirrorSource(), skillRevision("retention", listItemDescription))

	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Description != listItemDescription {
		t.Fatalf("description = %q, want %q", items[0].Description, listItemDescription)
	}
}

// A resource whose author wrote no description must omit the field rather than
// serve an empty string, so clients can distinguish "absent" from "empty".
func TestSearchOmitsEmptyDescription(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSource(ctx, mirrorSource()); err != nil {
		t.Fatal(err)
	}
	publishAssessedTestRevision(ctx, t, meta, skillRevision("nodesc", ""), store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))

	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	if strings.Contains(recorder.Body.String(), `"description"`) {
		t.Fatalf("empty description was serialized: %s", recorder.Body.String())
	}
}

func TestSearchBoundsHostileDescriptionOnEgress(t *testing.T) {
	hostile := "escape\x1b[31m and newline\n" + strings.Repeat("x", store.MaxDescriptionBytes*2)
	items := searchListItems(t, mirrorSource(), skillRevision("hostile", hostile))

	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0].Description
	if len(got) > store.MaxDescriptionBytes {
		t.Fatalf("served description length = %d, want <= %d", len(got), store.MaxDescriptionBytes)
	}
	if strings.ContainsAny(got, "\n\x1b") {
		t.Fatal("served description retained control characters")
	}
}

func TestResourceAvailability(t *testing.T) {
	mirror := store.Source{ID: "src", Acquisition: store.AcquisitionMirror}
	indexOnly := store.Source{ID: "src", Acquisition: store.AcquisitionIndexOnly}
	revision := func(kind string, archive store.ArchiveDigest, decision store.AssessmentDecision) store.AssessedResourceRevision {
		intrinsic := store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: kind, Name: "res"},
			ContentDigest: store.RevisionDigest(digestOf("a")), ArchiveDigest: archive,
		}
		return testAssessedRevision(t, intrinsic, decision)
	}
	revoked := revision(KindSkill, "", store.AssessmentDecisionReject)
	revoked.Revocation = &store.Revocation{Kind: store.RevocationRevision, Reason: "operator"}

	for _, test := range []struct {
		name string
		rev  store.AssessedResourceRevision
		src  store.Source
		want Availability
	}{
		{"mirrored allow is installable", revision(KindSkill, store.ArchiveDigest(digestOf("b")), store.AssessmentDecisionAllow), mirror, AvailabilityAvailable},
		{"warning stays installable", revision(KindSkill, store.ArchiveDigest(digestOf("b")), store.AssessmentDecisionWarn), mirror, AvailabilityAvailable},
		{"explicit reject blocks with archive", revision(KindSkill, store.ArchiveDigest(digestOf("b")), store.AssessmentDecisionReject), mirror, AvailabilityBlocked},
		{"missing mirrored artifact blocks", revision(KindSkill, "", store.AssessmentDecisionAllow), mirror, AvailabilityBlocked},
		{"index-only needs no archive", revision(KindSkill, "", store.AssessmentDecisionAllow), indexOnly, AvailabilityAvailable},
		{"collection needs no archive", revision(KindCollection, "", store.AssessmentDecisionAllow), mirror, AvailabilityAvailable},
		{"revocation outranks reject", revoked, mirror, AvailabilityRevoked},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := resourceAvailability(test.rev, test.src); got != test.want {
				t.Fatalf("resourceAvailability = %q, want %q", got, test.want)
			}
		})
	}
}

// The reported instance is ~100% policy-rejected. The list endpoint must say
// so on every card rather than presenting them as installable.
func TestSearchReportsAvailabilityForPolicyRejectedResources(t *testing.T) {
	rejected := skillRevision("rejected", listItemDescription)
	items := searchListItemsWithDecision(t, mirrorSource(), store.AssessmentDecisionReject, rejected)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Availability != AvailabilityBlocked {
		t.Fatalf("availability = %q, want %q", items[0].Availability, AvailabilityBlocked)
	}
	// A blocked resource stays listed, and keeps a usable description.
	if items[0].Description != listItemDescription {
		t.Fatalf("blocked resource lost its description: %q", items[0].Description)
	}
}

func TestSearchReportsAvailabilityForInstallableResources(t *testing.T) {
	items := searchListItems(t, mirrorSource(), skillRevision("ok", listItemDescription))
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Availability != AvailabilityAvailable {
		t.Fatalf("availability = %q, want %q", items[0].Availability, AvailabilityAvailable)
	}
}

// Availability must reach the wire under its documented name; a client cannot
// use a field that only exists on the Go struct.
func TestSearchSerializesAvailabilityField(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSource(ctx, mirrorSource()); err != nil {
		t.Fatal(err)
	}
	publishAssessedTestRevision(ctx, t, meta, skillRevision("ok", listItemDescription), store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))

	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/search", nil))

	body := recorder.Body.String()
	if !strings.Contains(body, `"availability":"available"`) {
		t.Fatalf("availability missing from search payload: %s", body)
	}
}

// The detail endpoint serializes the store revision directly, so the same
// description must be reachable there without a second lookup path.
func TestResourceDetailServesDescription(t *testing.T) {
	ctx := context.Background()
	meta := sqlite.New(":memory:")
	if err := meta.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = meta.Close() })
	if err := meta.PutMarketplace(ctx, store.Marketplace{ID: "mp", Name: "Marketplace"}); err != nil {
		t.Fatal(err)
	}
	if err := meta.PutSource(ctx, mirrorSource()); err != nil {
		t.Fatal(err)
	}
	publishAssessedTestRevision(ctx, t, meta, skillRevision("retention", listItemDescription), store.AssessmentDecisionAllow, store.RevisionDigest(digestOf("d")), strings.Repeat("e", 40))

	handler := NewServer(config.Config{Instance: config.Instance{ID: "mp"}}, meta, fsblob.New(t.TempDir())).PublicHandler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/resources/src/skill/retention", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	var got store.ResourceRevision
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if got.Description != listItemDescription {
		t.Fatalf("detail description = %q, want %q", got.Description, listItemDescription)
	}
}
