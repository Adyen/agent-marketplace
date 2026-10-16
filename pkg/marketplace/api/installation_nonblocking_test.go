package api

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"
)

// spec: marketplace-api/searches-remain-responsive-during-refresh
func TestSearchInstallationsDoesNotWaitForBlockedTelemetry(t *testing.T) {
	public, fixture, _, _ := adoptionSearchSetup(t, adoptionSearchSamples())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	fixture.handler.Store(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusBadGateway)
	})

	type outcome struct {
		status int
		body   string
	}
	done := make(chan outcome, 2)
	search := func() {
		resp, err := http.Get(public.URL + "/api/v1/search?sort=installations")
		if err != nil {
			done <- outcome{status: -1}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		done <- outcome{status: resp.StatusCode, body: string(raw)}
	}
	go search()
	go search()

	want := []string{"alpha", "preview", "beta", "preview-extra", "zulu", "local-only"}
	deadline := time.After(1500 * time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got.status != http.StatusOK {
				t.Fatalf("search status = %d, body = %s", got.status, got.body)
			}
			var result adoptionSearchResult
			if err := json.Unmarshal([]byte(got.body), &result); err != nil || result.Total != 6 {
				t.Fatalf("search result malformed: %v", err)
			}
			if !slices.Equal(searchNames(result), want) {
				t.Fatalf("cold fallback order = %v, want %v", searchNames(result), want)
			}
			for _, item := range result.Items {
				if item.InstallationCount != nil {
					t.Fatalf("cold search emitted a count: %+v", item)
				}
			}
		case <-deadline:
			t.Fatal("search still waiting on telemetry after 1.5s")
		}
	}
}
