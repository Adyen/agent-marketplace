package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicFetchBoundsAndFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"valid", 200, "# Models", false},
		{"not found", 404, "missing", true},
		{"rate limited", 429, "retry", true},
		{"unavailable", 503, "outage", true},
		{"oversized", 200, strings.Repeat("x", maxSourceBytes+1), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := publicClient()
			client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("audit must only make unauthenticated GET requests")
				}
				return &http.Response{StatusCode: tt.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tt.body)), Request: r}, nil
			})
			got, err := fetchPublic(context.Background(), client, "https://docs.factory.ai/docs/models.md")
			if (err != nil) != tt.wantError || err == nil && string(got.Body) != tt.body {
				t.Fatalf("fetch = %#v, error = %v", got, err)
			}
		})
	}
}

func TestMarkdownEndpointsUseWildcardAccept(t *testing.T) {
	client := publicClient()
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusNotFound
		if r.Header.Get("Accept") == "*/*" {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("# Subagents")), Request: r}, nil
	})
	if _, err := fetchPublic(context.Background(), client, "https://cursor.com/docs/subagents.md"); err != nil {
		t.Fatalf("explicit .md endpoints must not trigger Cursor content negotiation: %v", err)
	}
}

func TestPublicFetchRejectsUnsafeRedirectsAndURLs(t *testing.T) {
	client := publicClient()
	for _, raw := range []string{"http://docs.factory.ai/docs/models.md", "https://user:secret@docs.factory.ai/docs/models.md", "https://127.0.0.1/private", "https://example.com/models", "https://docs.factory.ai:444/models", "https://docs.factory.ai/models?token=secret"} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if client.CheckRedirect(req, nil) == nil {
			t.Fatalf("accepted redirect to %s", raw)
		}
		if _, err := fetchPublic(context.Background(), client, raw); err == nil {
			t.Fatalf("accepted initial URL %s", raw)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://learn.chatgpt.com/docs/models.md", nil)
	if err := client.CheckRedirect(req, nil); err != nil {
		t.Fatalf("official docs redirect rejected: %v", err)
	}
	if client.Timeout <= 0 || client.CheckRedirect(req, make([]*http.Request, 5)) == nil {
		t.Fatal("requests and redirect chains must be bounded")
	}
}

func TestAuditCLIPrintsReportBeforeFailure(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"check", "--root", "../.."}, strings.NewReader(""), &out, func(context.Context, string) (fetched, error) {
		return fetched{}, errors.New("offline test")
	}, time.Now())
	if !errors.Is(err, errAuditFailed) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	var r report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil || !r.Failed || len(r.Sources) != len(sources) {
		t.Fatalf("failure report was lost: %s / %v", out.String(), err)
	}
	for _, s := range r.Sources {
		if s.Status != "unknown" {
			t.Fatalf("unavailable source reported as %s", s.Status)
		}
	}
}

func TestSummaryPreservesFailureAndEscapesEvidence(t *testing.T) {
	r := report{Schema: 1, Failed: true, Sources: []sourceReport{{Name: "factory-models", Status: "drift", Findings: []finding{{Code: "catalog.model-missing", Model: "<script>|bad", Message: "Review `model`"}}}}}
	body, _ := json.Marshal(r)
	var out bytes.Buffer
	if err := run([]string{"summary"}, bytes.NewReader(body), &out, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "<script>") || !strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "catalog.model-missing") {
		t.Fatalf("unsafe or incomplete summary: %s", out.String())
	}
	// The summary must not overstate its scope: it covers monitored sources
	// only, and unmonitored evidence is checked offline for exact listing.
	for _, clause := range []string{
		"This report covers the monitored sources only: their documentation changes, inventory differences against the catalogs, and sources whose evidence is unknown.",
		"except the commit-pinned revisions and repository documents they check only for exact listing",
	} {
		if !strings.Contains(out.String(), clause) {
			t.Fatalf("summary lacks the scope clause %q:\n%s", clause, out.String())
		}
	}
	for _, body := range []string{"", "{}", "{\"schema\":1}", "not-json"} {
		if run([]string{"summary"}, strings.NewReader(body), io.Discard, nil, time.Now()) == nil {
			t.Fatalf("accepted incomplete summary input %q", body)
		}
	}
}

func TestCLIRejectsUnknownOptions(t *testing.T) {
	for _, args := range [][]string{nil, {"update"}, {"check", "--update"}, {"check", "extra"}, {"summary", "extra"}} {
		if err := run(args, strings.NewReader(""), io.Discard, nil, time.Now()); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
