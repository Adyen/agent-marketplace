package marketplace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/inspection"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func assessedRevision(revision store.ResourceRevision, inspectionDigest ...string) store.AssessedResourceRevision {
	inspection := "sha256:" + strings.Repeat("d", 64)
	if len(inspectionDigest) > 0 {
		inspection = inspectionDigest[0]
	}
	assessment, err := store.FinalizeResourceAssessment(store.ResourceAssessment{
		Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
		AnalyzerVersion: "test-analyzer", PolicyDigest: store.PolicyDigest("sha256:" + strings.Repeat("c", 64)),
		Decision: store.AssessmentDecisionAllow, InspectionManifestDigest: store.RevisionDigest(inspection),
		AssessedAt: time.Unix(1, 0).UTC(),
	})
	if err != nil {
		panic(err)
	}
	return store.AssessedResourceRevision{
		ResourceRevision: revision,
		Assessment:       assessment,
		Occurrence: store.ResourceOccurrence{
			Coordinate: revision.Coordinate, ContentDigest: revision.ContentDigest,
			Commit: strings.Repeat("a", 40), ManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("b", 64)),
		},
	}
}

func assessedRevisionOccurrence(revision store.ResourceRevision, commit, manifestDigest string) store.AssessedResourceRevision {
	assessed := assessedRevision(revision)
	assessed.Occurrence.Commit = commit
	assessed.Occurrence.ManifestDigest = store.RevisionDigest(manifestDigest)
	return assessed
}

func rejectedRevision(revision store.ResourceRevision) store.AssessedResourceRevision {
	return rejectAssessment(assessedRevision(revision))
}

func rejectAssessment(assessed store.AssessedResourceRevision) store.AssessedResourceRevision {
	assessed.Assessment.Decision = store.AssessmentDecisionReject
	assessed.Assessment.AssessmentDigest = ""
	assessment, err := store.FinalizeResourceAssessment(assessed.Assessment)
	if err != nil {
		panic(err)
	}
	assessed.Assessment = assessment
	return assessed
}

func revokedRevision(revision store.ResourceRevision, target string) store.AssessedResourceRevision {
	assessed := assessedRevision(revision)
	assessed.Revocation = &store.Revocation{Kind: store.RevocationRevision, TargetID: target, Reason: "test", AppliedAt: time.Now().UTC()}
	return assessed
}

func writeTestWellKnown(w http.ResponseWriter, instanceID string) {
	_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com", "https://example.com"))
}

func writeTestRegistry(w http.ResponseWriter, src store.Registry) {
	_ = json.NewEncoder(w).Encode(mpapi.RegistryDTO{
		ID: src.ID, Slug: src.Slug, Aliases: src.Aliases, Origins: src.Origins,
		Ref: src.Ref, Roots: src.Roots, Acquisition: src.Acquisition,
		License: src.License, Listed: src.Listed,
	})
}

func TestNewClientValidatesBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{"empty", "", "marketplace base URL is required"},
		{"ftp", "ftp://example.com", "must use http or https"},
		{"not-url", "://bad", "invalid marketplace base URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.baseURL)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewClient(%q) error = %v, want containing %q", tc.baseURL, err, tc.wantErr)
			}
		})
	}
}

func TestClientWellKnown(t *testing.T) {
	want := mpapi.WellKnown{
		InstanceID:         "mp-test",
		CanonicalURL:       "https://example.com",
		APIBase:            "/api/v1",
		BrowserBase:        "https://example.com",
		BrowserLatestRoute: "https://example.com/r/{registry}/{kind}/{name}",
		BrowserExactRoute:  "https://example.com/r/{registry}/{kind}/{name}/revisions/{revision}",
		Capabilities:       mpapi.DefaultCapabilities(),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(want)
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := c.WellKnown(context.Background())
	if err != nil {
		t.Fatalf("WellKnown: %v", err)
	}
	if got.InstanceID != want.InstanceID {
		t.Fatalf("InstanceID = %q, want %q", got.InstanceID, want.InstanceID)
	}
	if got.APIBase != want.APIBase {
		t.Fatalf("APIBase = %q, want %q", got.APIBase, want.APIBase)
	}
}

func TestClientWellKnownAllowsAPIOnlyAndRejectsPartialBrowserMetadata(t *testing.T) {
	base := mpapi.DefaultWellKnown("mp", "https://api.example")
	if err := validateAPIWellKnown(base); err != nil {
		t.Fatalf("API-only well-known rejected: %v", err)
	}
	base.BrowserBase = "https://web.example"
	if err := validateAPIWellKnown(base); err == nil || !strings.Contains(err.Error(), "browser fields are incomplete") {
		t.Fatalf("partial browser metadata error = %v", err)
	}
}

func TestClientRejectsNullAndIncompleteEnvelopes(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "well-known null", path: "/.well-known/agent-marketplace.json", body: "null"},
		{name: "well-known incomplete", path: "/.well-known/agent-marketplace.json", body: `{"instance_id":"mp"}`},
		{name: "search null", path: "/api/v1/search", body: "null"},
		{name: "search incomplete", path: "/api/v1/search", body: `{"items":[],"total":0,"offset":0}`},
		{name: "search null items", path: "/api/v1/search", body: `{"items":null,"total":0,"offset":0,"limit":100}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c, err := NewClient(server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if tc.path == "/.well-known/agent-marketplace.json" {
				if _, err := c.WellKnown(context.Background()); err == nil {
					t.Fatal("WellKnown accepted an invalid envelope")
				}
			} else if _, err := c.Search(context.Background(), SearchRequest{}); err == nil {
				t.Fatal("Search accepted an invalid envelope")
			}
		})
	}
}

func TestClientRejectsPrivateAndLegacyRegistryFields(t *testing.T) {
	for _, field := range []string{"revision_commit", "credentials_ref", "refresh_error", "policy_overrides"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				payload := map[string]any{
					"id": "source", "slug": "source", "origins": []string{"https://example.com/repo"},
					"ref": "main", "roots": []map[string]string{{"path": ".agents"}},
					"acquisition": "index-only", "listed": true, field: "must not cross boundary",
				}
				_ = json.NewEncoder(w).Encode(payload)
			}))
			defer server.Close()
			c, err := NewClient(server.URL)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := c.GetRegistry(context.Background(), "source"); err == nil {
				t.Fatalf("GetRegistry accepted private field %q", field)
			}
		})
	}
}

func TestClientInspectionMethodsVerifyProvenanceAndContent(t *testing.T) {
	ctx := context.Background()
	revision := "sha256:" + strings.Repeat("a", 64)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"}
	body := []byte("# review\\n")
	projection, err := inspection.Build(coord, revision, body, map[string][]byte{"notes.txt": []byte("notes")}, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	rev := assessedRevision(store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(revision)}, projection.Manifest.ManifestDigest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/revisions/"+revision):
			_ = json.NewEncoder(w).Encode(rev)
		case strings.HasSuffix(r.URL.Path, "/inspection"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(projection.Payload)
		case strings.HasSuffix(r.URL.Path, "/inspection/content"):
			w.Header().Set("Content-Type", inspection.MediaMarkdown)
			_, _ = w.Write(body)
		case strings.HasSuffix(r.URL.Path, "/inspection/content/"+inspection.Digest(body)):
			w.Header().Set("Content-Type", inspection.MediaMarkdown)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := client.GetResourceInspection(ctx, "source-id", "skill", "review", revision)
	if err != nil || manifest.ManifestDigest != projection.Manifest.ManifestDigest {
		t.Fatalf("GetResourceInspection = %q, %v", manifest.ManifestDigest, err)
	}
	primary, err := client.GetResourceInspectionPrimary(ctx, "source-id", "skill", "review", revision)
	if err != nil || string(primary.Data) != string(body) || primary.ContentType != inspection.MediaMarkdown {
		t.Fatalf("primary = %#v, %v", primary, err)
	}
	file, err := client.GetResourceInspectionFile(ctx, "source-id", "skill", "review", revision, inspection.Digest(body))
	if err != nil || string(file.Data) != string(body) {
		t.Fatalf("file = %#v, %v", file, err)
	}
}

func TestClientInspectionRejectsProvenanceMismatch(t *testing.T) {
	revision := "sha256:" + strings.Repeat("a", 64)
	coord := store.ResourceCoordinate{Marketplace: "mp", Registry: "source-id", Kind: "skill", Name: "review"}
	projection, err := inspection.Build(coord, revision, []byte("# review\\n"), nil, 10, 1024, 4096)
	if err != nil {
		t.Fatal(err)
	}
	rev := assessedRevision(store.ResourceRevision{Coordinate: coord, ContentDigest: store.RevisionDigest(revision)}, "sha256:"+strings.Repeat("b", 64))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/revisions/"+revision) {
			_ = json.NewEncoder(w).Encode(rev)
			return
		}
		_, _ = w.Write(projection.Payload)
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetResourceInspection(context.Background(), "source-id", "skill", "review", revision); err == nil || !strings.Contains(err.Error(), "manifest digest mismatch") {
		t.Fatalf("provenance mismatch error = %v", err)
	}
}

func TestClientSearch(t *testing.T) {
	resp := SearchResponse{
		Items: []mpapi.ResourceListItem{
			{RegistryID: "src-test", RegistrySlug: "test-source", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Score: 1.0},
		},
		Total:  1,
		Offset: 0,
		Limit:  100,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("q") != "review" || q.Get("kind") != "skill" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
	defer server.Close()

	c, err := NewClient(server.URL, WithUserAgent("test/1.0"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := c.Search(context.Background(), SearchRequest{Query: "review", Kind: "skill"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "review" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestClientSearchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"search failed"}`))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Search(context.Background(), SearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "search failed") {
		t.Fatalf("Search error = %v, want containing 'search failed'", err)
	}
}

func TestAPIErrorBoundsAndSanitizesTerminalContent(t *testing.T) {
	body := append([]byte("prefix\x1b[31m\n\u202e"), []byte(strings.Repeat("x", maxAPIErrorBytes*3))...)
	err := apiError(http.StatusBadGateway, body)
	if err == nil {
		t.Fatal("apiError returned nil")
	}
	message := err.Error()
	if strings.ContainsAny(message, "\x00\x1b\n\r\t") {
		t.Fatalf("API error contains raw control character: %q", message)
	}
	if strings.Contains(message, "\u202e") {
		t.Fatalf("API error contains raw bidi marker: %q", message)
	}
	if !strings.Contains(message, `\x1b`) || !strings.Contains(message, `\n`) || !strings.Contains(message, `\u202e`) {
		t.Fatalf("API error did not preserve escaped diagnostics: %q", message)
	}
	if len(message) > maxAPIErrorOutputBytes+64 {
		t.Fatalf("API error was not bounded: %d bytes", len(message))
	}
}

func TestClientSearchTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c, err := NewClient(server.URL, WithTimeout(time.Nanosecond))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = c.Search(ctx, SearchRequest{})
	if err == nil {
		t.Fatal("Search expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "Client.Timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestClientSearchBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Search(context.Background(), SearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

func TestNewClientRejectsUnsafeBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"http://",
		"http://?query=value",
		"http://example.com/path?query=value",
		"http://example.com/path#fragment",
		"http://user:pass@example.com",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewClient(baseURL); err == nil {
				t.Fatalf("NewClient(%q) succeeded", baseURL)
			}
		})
	}
}

func TestClientEscapesDynamicPathSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.EscapedPath(), "/src%2Fwith%20space/skill/name%2Fpart/revisions/") {
			t.Fatalf("request path = %q, want escaped dynamic segments", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate: store.ResourceCoordinate{
				Marketplace: "mp", Registry: "src/with space", Kind: "skill", Name: "name/part",
			},
			ContentDigest: store.RevisionDigest("sha256:" + strings.Repeat("a", 64)),
		}))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetResourceRevision(context.Background(), "src/with space", "skill", "name/part", "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatalf("GetResourceRevision: %v", err)
	}
}

func TestClientBindsResourceResponsesToRequestedCoordinate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
			Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source", Kind: "skill", Name: "other"},
			ContentDigest: store.RevisionDigest(digest),
		}))
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetLatestResourceRevision(context.Background(), "source", "skill", "review"); err == nil || !strings.Contains(err.Error(), "coordinate.name") {
		t.Fatalf("GetLatestResourceRevision error = %v, want coordinate mismatch", err)
	}
	if _, err := c.GetResourceRevision(context.Background(), "source", "skill", "review", digest); err == nil || !strings.Contains(err.Error(), "coordinate.name") {
		t.Fatalf("GetResourceRevision error = %v, want coordinate mismatch", err)
	}
}

func TestClientBindsResourceRegistryAfterRegistryResolution(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/registries/source-slug":
			writeTestRegistry(w, store.Registry{ID: "source-id", Slug: "source-slug", Origins: []string{"https://example.com/repo"}, Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror, Listed: true})
		default:
			_ = json.NewEncoder(w).Encode(assessedRevision(store.ResourceRevision{
				Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "other-source", Kind: "skill", Name: "review"},
				ContentDigest: store.RevisionDigest(digest),
			}))
		}
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetRegistryBySlug(context.Background(), "source-slug"); err != nil {
		t.Fatalf("GetRegistryBySlug: %v", err)
	}
	if _, err := c.GetLatestResourceRevision(context.Background(), "source-slug", "skill", "review"); err == nil || !strings.Contains(err.Error(), "coordinate.registry") {
		t.Fatalf("GetLatestResourceRevision error = %v, want registry mismatch", err)
	}
}

func TestClientBindsRegistryResponseToRequestedRegistryID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.PublicRegistry{
			ID: "other-source", Slug: "requested-source", Origins: []string{"https://example.com/repo"},
			Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Acquisition: store.AcquisitionMirror,
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetRegistry(context.Background(), "requested-source"); err == nil || !strings.Contains(err.Error(), "registry ID") {
		t.Fatalf("GetRegistry error = %v, want requested registry ID mismatch", err)
	}
}

func TestClientBindsRegistryOccurrenceToRequestedRegistryAndCommit(t *testing.T) {
	commit := strings.Repeat("a", 40)
	manifest := "sha256:" + strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.ExactRegistryOccurrence{
			RegistryID: "other-source", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Commit: commit,
			RegistryRevisionManifestDigest: store.RevisionDigest(manifest),
			Acquisition:                    store.AcquisitionIndexOnly,
			AcquisitionOrigin:              "https://example.com/repo.git",
			OccurredAt:                     time.Now().UTC(),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetRegistryOccurrence(context.Background(), "requested-source", commit); err == nil || !strings.Contains(err.Error(), "registry ID") {
		t.Fatalf("GetRegistryOccurrence error = %v, want requested source mismatch", err)
	}
}

func TestClientBindsRegistryOccurrenceCommitToRequestedCommit(t *testing.T) {
	requestedCommit := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(mpapi.ExactRegistryOccurrence{
			RegistryID: "source", Ref: "main", Roots: []store.RegistryRoot{{Path: ".agents"}}, Commit: strings.Repeat("b", 40),
			RegistryRevisionManifestDigest: store.RevisionDigest("sha256:" + strings.Repeat("c", 64)),
			Acquisition:                    store.AcquisitionIndexOnly,
			AcquisitionOrigin:              "https://example.com/repo.git",
			OccurredAt:                     time.Now().UTC(),
		})
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetRegistryOccurrence(context.Background(), "source", requestedCommit); err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("GetRegistryOccurrence error = %v, want requested commit mismatch", err)
	}
}

func TestValidateAcquisitionOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://example.com/org/repo.git",
		"http://example.com/org/repo.git",
		"ssh://example.com/org/repo.git",
		"git@example.com:org/repo.git",
		"file:///tmp/repo.git",
	} {
		if err := validateAcquisitionOrigin(origin); err != nil {
			t.Errorf("validateAcquisitionOrigin(%q) unexpected error: %v", origin, err)
		}
	}
	for _, test := range []struct {
		origin string
		want   string
	}{
		{origin: "/tmp/repo", want: "bare relative or absolute paths"},
		{origin: "./repo", want: "bare relative or absolute paths"},
		{origin: "https://user:pass@example.com/repo.git", want: "userinfo"},
		{origin: "https://example.com/repo.git?token=secret", want: "query"},
		{origin: "git://example.com/repo.git", want: "git:// protocol"},
		{origin: "ext::sh -c evil", want: "transport helper"},
		{origin: "example.com/repo.git", want: "HTTP(S)"},
	} {
		if err := validateAcquisitionOrigin(test.origin); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("validateAcquisitionOrigin(%q) = %v, want %q", test.origin, err, test.want)
		}
	}
}

func TestClientRejectsNonCanonicalResourceMetadata(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source", Kind: "skill", Name: "review"},
		ContentDigest: store.RevisionDigest(digest),
	})
	revision.Occurrence.Commit = "abc123"
	if err := validateAPIResourceRevision(revision); err == nil {
		t.Fatal("resource revision accepted a development occurrence commit")
	}
	collectionRevision := assessedRevision(store.ResourceRevision{
		Coordinate:    store.ResourceCoordinate{Marketplace: "mp", Registry: "source", Kind: "collection", Name: "bundle"},
		ContentDigest: store.RevisionDigest(digest),
		ArchiveDigest: store.ArchiveDigest(digest),
	})
	if err := validateAPIResourceRevision(collectionRevision); err == nil || !strings.Contains(err.Error(), "virtual collection") {
		t.Fatalf("collection archive revision validation = %v", err)
	}
	item := mpapi.ResourceListItem{RegistryID: "source", Kind: "skill", Name: "review", Revision: "refs/heads/main", ContentDigest: digest}
	if err := validateAPIResourceListItem(item); err == nil {
		t.Fatal("resource list item accepted a symbolic revision")
	}
}

func TestClientRejectsInvalidArtifactDigest(t *testing.T) {
	c, err := NewClient("https://example.com")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, digest := range []string{"", "sha256:short", "sha256:" + strings.Repeat("A", 64), "sha256:" + strings.Repeat("g", 64), "sha512:" + strings.Repeat("a", 64)} {
		if _, err := c.GetArtifact(context.Background(), digest); err == nil {
			t.Fatalf("GetArtifact(%q) succeeded", digest)
		}
	}
}

func TestClientDetectsOversizedResponse(t *testing.T) {
	resp := &http.Response{
		ContentLength: maxArtifactBytes + 1,
		Body:          io.NopCloser(strings.NewReader("small")),
	}
	if _, err := readResponseBody(resp, maxArtifactBytes); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("readResponseBody error = %v, want oversized response error", err)
	}
}

func TestClientRejectsCrossOriginRedirect(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/search", http.StatusFound)
	}))
	defer source.Close()

	c, err := NewClient(source.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Search(context.Background(), SearchRequest{}); err == nil || !strings.Contains(err.Error(), "redirect origin") {
		t.Fatalf("Search error = %v, want redirect origin error", err)
	}
	if targetRequests != 0 {
		t.Fatalf("cross-origin redirect reached target %d times", targetRequests)
	}
}

func TestClientRejectsRedirectMutationByCheckRedirect(t *testing.T) {
	var targetRequests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/search", http.StatusFound)
	}))
	defer source.Close()

	hc := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		u := *req.URL
		mutated, err := http.NewRequest(http.MethodGet, target.URL+u.Path, nil)
		if err != nil {
			return err
		}
		req.URL = mutated.URL
		return nil
	}}
	c, err := NewClient(source.URL, WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Search(context.Background(), SearchRequest{}); err == nil || !strings.Contains(err.Error(), "redirect origin") {
		t.Fatalf("Search error = %v, want redirect origin error", err)
	}
	if targetRequests != 0 {
		t.Fatalf("mutated redirect reached target %d times", targetRequests)
	}
}

func BenchmarkClientSearch(b *testing.B) {
	resp := SearchResponse{
		Items: make([]mpapi.ResourceListItem, 50),
		Total: 50,
	}
	for i := range resp.Items {
		resp.Items[i] = canonicalSearchItem(fmt.Sprintf("src-%d", i), "skill", fmt.Sprintf("skill-%d", i), 0.5)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
	defer server.Close()

	c, err := NewClient(server.URL)
	if err != nil {
		b.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.Search(ctx, SearchRequest{Query: "review", Kind: "skill"})
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}
