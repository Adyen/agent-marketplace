package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/analyze"
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
		if err := meta.PutResourceRevision(ctx, rev); err != nil {
			t.Fatal(err)
		}
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
	if err := meta.PutResourceRevision(ctx, skillRevision("nodesc", "")); err != nil {
		t.Fatal(err)
	}

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
	rejectFinding := store.Finding{Code: analyze.PolicyLicenseNotAllowed, Severity: analyze.OutcomeReject}
	// An undeclared license is a distinct policy condition, but a
	// reject-severity finding blocks installation regardless of which one it is.
	undeclaredRejectFinding := store.Finding{Code: analyze.PolicyLicenseUndeclared, Severity: analyze.OutcomeReject}
	undeclaredWarnFinding := store.Finding{Code: analyze.PolicyLicenseUndeclared, Severity: analyze.OutcomeWarn}
	warnFinding := store.Finding{Code: "marketplace.analyze.remote-reference", Severity: "warn"}

	mirror := store.Source{ID: "src", Acquisition: store.AcquisitionMirror}
	indexOnly := store.Source{ID: "src", Acquisition: store.AcquisitionIndexOnly}

	revision := func(kind string, archive store.ArchiveDigest, findings ...store.Finding) store.ResourceRevision {
		return store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Source: "src", Kind: kind, Name: "res"},
			ContentDigest: store.RevisionDigest(digestOf("a")),
			ArchiveDigest: archive,
			Findings:      findings,
		}
	}

	for _, test := range []struct {
		name string
		rev  store.ResourceRevision
		src  store.Source
		want Availability
	}{
		{
			name: "mirrored artifact is installable",
			rev:  revision(KindSkill, store.ArchiveDigest(digestOf("b"))),
			src:  mirror,
			want: AvailabilityAvailable,
		},
		{
			name: "non-blocking findings stay installable",
			rev:  revision(KindSkill, store.ArchiveDigest(digestOf("b")), warnFinding),
			src:  mirror,
			want: AvailabilityAvailable,
		},
		{
			name: "a reject finding blocks even if an archive somehow remains",
			rev:  revision(KindSkill, store.ArchiveDigest(digestOf("b")), rejectFinding),
			src:  mirror,
			want: AvailabilityBlocked,
		},
		{
			name: "an undeclared-license reject blocks installation",
			rev:  revision(KindSkill, "", undeclaredRejectFinding),
			src:  mirror,
			want: AvailabilityBlocked,
		},
		{
			// The operator opted into warn for undeclared licenses, so the
			// artifact is still mirrored and the resource stays installable.
			name: "an undeclared-license warning stays installable",
			rev:  revision(KindSkill, store.ArchiveDigest(digestOf("b")), undeclaredWarnFinding),
			src:  mirror,
			want: AvailabilityAvailable,
		},
		{
			name: "a mirrored source with a stripped archive is blocked",
			rev:  revision(KindSkill, ""),
			src:  mirror,
			want: AvailabilityBlocked,
		},
		{
			// An index-only source never mirrors an archive, so an absent
			// archive digest says nothing about installability there.
			name: "index-only source without an archive stays available",
			rev:  revision(KindSkill, ""),
			src:  indexOnly,
			want: AvailabilityAvailable,
		},
		{
			// A collection is addressed by its manifest digest and never has
			// an archive digest, even when perfectly installable.
			name: "collection without an archive stays available",
			rev:  revision(KindCollection, ""),
			src:  mirror,
			want: AvailabilityAvailable,
		},
		{
			name: "revocation outranks everything else",
			rev: func() store.ResourceRevision {
				r := revision(KindSkill, "", rejectFinding)
				r.Revocation = &store.Revocation{Kind: store.RevocationRevision, Reason: "operator"}
				return r
			}(),
			src:  mirror,
			want: AvailabilityRevoked,
		},
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
	rejected.ArchiveDigest = ""
	rejected.Findings = []store.Finding{{
		Code:     "marketplace.analyze.hidden-content",
		Severity: analyze.OutcomeReject,
		Message:  "hidden content detected",
	}}

	items := searchListItems(t, mirrorSource(), rejected)
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
	if err := meta.PutResourceRevision(ctx, skillRevision("ok", listItemDescription)); err != nil {
		t.Fatal(err)
	}

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
	if err := meta.PutResourceRevision(ctx, skillRevision("retention", listItemDescription)); err != nil {
		t.Fatal(err)
	}

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
