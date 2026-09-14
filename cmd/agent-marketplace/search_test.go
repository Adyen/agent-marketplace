package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func searchCommandServer(t *testing.T, fail bool) *httptest.Server {
	return searchCommandServerWithInstance(t, "mp-public", fail)
}

func searchCommandServerWithInstance(t *testing.T, instanceID string, fail bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-marketplace.json" {
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, "https://example.com"))
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
				{RegistryID: "src", RegistrySlug: "src", Kind: "skill", Name: "review", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Score: 1.0, Description: "Review skill"},
			},
			Total: 1, Offset: 0, Limit: 100,
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		_ = enc.Encode(resp)
	}))
}

type marketplaceSearchResponse struct {
	Items  []mpapi.ResourceListItem `json:"items"`
	Total  int                      `json:"total"`
	Offset int                      `json:"offset"`
	Limit  int                      `json:"limit"`
}

func TestSearchCommandJSONOutput(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
	if strings.Contains(output, `"score"`) {
		t.Fatalf("expected score to be omitted from JSON output, got:\n%s", output)
	}
}

func TestSearchCommandWithoutKindReturnsEveryResourceKind(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := t.TempDir()
	withWorkingDir(t, repoRoot)

	var receivedKind string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/agent-marketplace.json":
			_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("mp-public", "https://example.com"))
		case "/api/v1/search":
			receivedKind = r.URL.Query().Get("kind")
			_ = json.NewEncoder(w).Encode(marketplaceSearchResponse{
				Items: []mpapi.ResourceListItem{
					{RegistryID: "src", RegistrySlug: "src", Kind: mpapi.KindMcpServer, Name: "genai-architect", Revision: "sha256:" + strings.Repeat("a", 64), ContentDigest: "sha256:" + strings.Repeat("a", 64), Score: 1},
					{RegistryID: "src", RegistrySlug: "src", Kind: mpapi.KindSkill, Name: "architecture", Revision: "sha256:" + strings.Repeat("b", 64), ContentDigest: "sha256:" + strings.Repeat("b", 64), Score: 0.5},
				},
				Total: 2, Limit: 100,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), fmt.Sprintf(`
marketplaces:
  - url: %s
    instance_id: mp-public
`, server.URL))

	output, err := executeCommand("search", "--output", "json", "architect")
	if err != nil {
		t.Fatalf("expected search to succeed: %v\n%s", err, output)
	}
	if receivedKind != "" {
		t.Fatalf("search sent implicit kind filter %q", receivedKind)
	}
	if !strings.Contains(output, `"name": "genai-architect"`) || !strings.Contains(output, `"name": "architecture"`) {
		t.Fatalf("expected MCP server and skill in output, got:\n%s", output)
	}
}

func TestSearchCommandAdHocURL(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
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
	if strings.Contains(output, "SCORE") {
		t.Fatalf("expected score column to be omitted from table output, got:\n%s", output)
	}
}

func TestSearchTableQuotesAdversarialRemoteFields(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const hostile = "remote\x00\x1b[31m\nname\t\r\u202e"
	results := []marketplace.SearchResult{{
		Marketplace:      hostile,
		ResourceListItem: mpapi.ResourceListItem{RegistrySlug: hostile, Kind: hostile, Name: hostile, Description: hostile, Score: 1},
	}}
	var table bytes.Buffer
	if err := writeSearchTable(&table, results); err != nil {
		t.Fatal(err)
	}
	output := table.String()
	if strings.ContainsAny(output, "\x00\x1b\r\t") {
		t.Fatalf("table contains raw terminal controls: %q", output)
	}
	quoted := marketplace.QuoteTerminal(hostile)
	if strings.Count(output, quoted) != 5 {
		t.Fatalf("table = %q, want quoted remote fields %q five times", output, quoted)
	}
	if !strings.Contains(output, `\u202e`) {
		t.Fatalf("table = %q, want escaped bidi control", output)
	}

	var encoded bytes.Buffer
	if err := writeSearchJSON(&encoded, results, nil); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoded.Bytes()) {
		t.Fatalf("search JSON is invalid: %q", encoded.String())
	}
}

func TestSearchCommandPartialFailure(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
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
	if isolateCommandTest(t) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := executeCommandContext(ctx, "search", "review")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled search context, got: %v", err)
	}
}

func TestSearchCommandNoMarketplaceConfigured(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
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

func TestSearchRejectsRuleKindForHostedMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	err := validateHostedSearchKind(ir.KindRule)
	if err == nil || !strings.Contains(err.Error(), "unsupported hosted Marketplace kind") {
		t.Fatalf("validateHostedSearchKind(rule) error = %v, want clear unsupported hosted Marketplace kind error", err)
	}
	if err := validateHostedSearchKind(ir.KindSkill); err != nil {
		t.Fatalf("validateHostedSearchKind(skill) = %v, want accepted control", err)
	}
}
