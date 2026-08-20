package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func searchCommandServer(t *testing.T, fail bool) *httptest.Server {
	return searchCommandServerWithInstance(t, "mp-public", fail)
}

func searchCommandServerWithInstance(t *testing.T, instanceID string, fail bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(map[string]string{"instance_id": instanceID})
			return
		}
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"down"}`))
			return
		}
		resp := marketplaceSearchResponse{
			Items: []mpapi.ResourceListItem{
				{SourceSlug: "src", Kind: "skill", Name: "review", Score: 1.0, Description: "Review skill"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
}

type marketplaceSearchResponse struct {
	Items []mpapi.ResourceListItem `json:"items"`
	Total int                      `json:"total"`
}

func TestSearchCommandJSONOutput(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	server := searchCommandServer(t, false)
	defer server.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), fmt.Sprintf(`
marketplaces:
  - url: %s
    instance_id: mp-public
    alias: public
`, server.URL))

	output, err := executeCommand("search", "--marketplace", "public", "--output", "json", "review")
	if err != nil {
		t.Fatalf("expected search to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"name": "review"`) {
		t.Fatalf("expected review in output, got:\n%s", output)
	}
	if !strings.Contains(output, `"marketplace": "public"`) {
		t.Fatalf("expected marketplace annotation, got:\n%s", output)
	}
}

func TestSearchCommandAdHocURL(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	server := searchCommandServer(t, false)
	defer server.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), ``)

	output, err := executeCommand("search", "--marketplace", server.URL, "--output", "json", "review")
	if err != nil {
		t.Fatalf("expected search to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"name": "review"`) {
		t.Fatalf("expected review in output, got:\n%s", output)
	}
}

func TestSearchCommandTableOutput(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	server := searchCommandServer(t, false)
	defer server.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), fmt.Sprintf(`
marketplaces:
  - url: %s
    instance_id: mp-public
    alias: public
`, server.URL))

	output, err := executeCommand("search", "--marketplace", "public", "review")
	if err != nil {
		t.Fatalf("expected search to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "review") {
		t.Fatalf("expected review in table output, got:\n%s", output)
	}
	if !strings.Contains(output, "MARKETPLACE") {
		t.Fatalf("expected table header, got:\n%s", output)
	}
}

func TestSearchCommandPartialFailure(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	good := searchCommandServerWithInstance(t, "mp-good", false)
	bad := searchCommandServerWithInstance(t, "mp-bad", true)
	defer good.Close()
	defer bad.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), fmt.Sprintf(`
marketplaces:
  - url: %s
    instance_id: mp-good
    alias: good
  - url: %s
    instance_id: mp-bad
    alias: bad
`, good.URL, bad.URL))

	output, err := executeCommand("search", "--output", "json", "review")
	if err != nil {
		t.Fatalf("expected partial search to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"name": "review"`) {
		t.Fatalf("expected result in output, got:\n%s", output)
	}
	if !strings.Contains(output, `"errors"`) {
		t.Fatalf("expected structured errors in JSON output, got:\n%s", output)
	}
}

func TestSearchCommandAllowlistBlock(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	server := searchCommandServer(t, false)
	defer server.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), fmt.Sprintf(`
allowed_marketplaces: ['^other\.com$']
marketplaces:
  - url: %s
    instance_id: mp-public
    alias: public
`, server.URL))

	output, err := executeCommand("search", "--marketplace", "public", "--output", "json", "review")
	if err == nil {
		t.Fatalf("expected allowlist to block search; output:\n%s", output)
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("expected blocked error, got: %v", err)
	}
}

func TestSearchCommandUsesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := executeCommandContext(ctx, "search", "review")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled search context, got: %v", err)
	}
}

func TestSearchCommandNoMarketplaceConfigured(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), ``)

	output, err := executeCommand("search", "review")
	if err == nil {
		t.Fatalf("expected search to fail with no marketplaces; output:\n%s", output)
	}
	if !strings.Contains(err.Error(), "no marketplaces") {
		t.Fatalf("expected no marketplaces error, got: %v\n%s", err, output)
	}
}
