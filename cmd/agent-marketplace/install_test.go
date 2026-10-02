package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/cache"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func TestInstallMarketplaceEndpointAddsItToUserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
	defer server.Close()
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("install Marketplace endpoint: %v\n%s", err, output)
	}
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if output != "Added Marketplace "+server.URL+" to "+configPath+"\n" {
		t.Fatalf("output = %q", output)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read user config: %v", err)
	}
	if !strings.Contains(string(data), "marketplace: "+server.URL) || strings.Contains(string(data), "marketplaces:") {
		t.Fatalf("user config:\n%s", data)
	}
	doc := readLockDocument(t, filepath.Join(homeDir, ".agents", "lock.yaml"))
	if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].URL != server.URL || doc.Marketplaces[0].InstanceID != "marketplace-instance" || len(doc.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint lock = %#v", doc.Marketplaces)
	}
}

func TestInstallMarketplaceEndpointSupportsPathPrefix(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
	defer server.Close()
	endpoint := server.URL + "/services/marketplace"
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", endpoint)
	if err != nil {
		t.Fatalf("install path-prefixed Marketplace endpoint: %v\n%s", err, output)
	}
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if output != "Added Marketplace "+endpoint+" to "+configPath+"\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestInstallBareGitProjectURLRetainsFromGuidance(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	defer server.Close()
	url := server.URL + "/group/project"
	withWorkingDir(t, t.TempDir())

	_, err := executeCommand("install", url)
	if err == nil || !strings.Contains(err.Error(), "well-known document") || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("bare Git URL error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("bare Git URL made %d requests, want 1", requests)
	}
}

func TestInstallMarketplaceEndpointUsesExplicitRepoAndLocalScopes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, scope := range []string{"repo", "local"} {
		t.Run(scope, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
			defer server.Close()
			workspace, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, workspace)

			output, err := executeCommand("install", server.URL, "--scope", scope)
			if err != nil {
				t.Fatalf("install Marketplace endpoint at %s scope: %v\n%s", scope, err, output)
			}
			name := "config.yaml"
			if scope == "local" {
				name = "config.local.yaml"
			}
			configPath := filepath.Join(workspace, ".agents", name)
			if output != "Added Marketplace "+server.URL+" to "+configPath+"\n" {
				t.Fatalf("output = %q", output)
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read %s config: %v", scope, err)
			}
			if !strings.Contains(string(data), "marketplace: "+server.URL) || strings.Contains(string(data), "marketplaces:") {
				t.Fatalf("config = %s", data)
			}
			lockName := "lock.yaml"
			if scope == "local" {
				lockName = "lock.local.yaml"
			}
			doc := readLockDocument(t, filepath.Join(workspace, ".agents", lockName))
			if len(doc.Marketplaces) != 1 || doc.Marketplaces[0].InstanceID != "marketplace-instance" {
				t.Fatalf("%s endpoint lock = %#v", scope, doc.Marketplaces)
			}
			if _, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml")); !os.IsNotExist(err) {
				t.Fatalf("user config should not be written, stat error = %v", err)
			}
		})
	}
}

func TestInstallMarketplaceEndpointRepeatPreservesUserConfig(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	requests := 0
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", &requests)
	defer server.Close()
	withWorkingDir(t, t.TempDir())
	if output, err := executeCommand("install", server.URL); err != nil {
		t.Fatalf("first registration: %v\n%s", err, output)
	}
	configPath := filepath.Join(homeDir, ".agents", "config.yaml")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("repeat registration: %v\n%s", err, output)
	}
	if output != "Marketplace "+server.URL+" is already configured in "+configPath+"\n" {
		t.Fatalf("output = %q", output)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("repeat registration changed config:\nbefore:\n%safter:\n%s", before, after)
	}
	if requests != 2 {
		t.Fatalf("repeat registration made %d well-known requests, want 2", requests)
	}
}

func TestInstallMarketplaceEndpointIsIdempotentInBroaderScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
	defer server.Close()
	organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
	writeFile(t, organizationPath, "apiVersion: agent-marketplace/v2\nmarketplaces:\n  - url: "+server.URL+"\n    instance_id: marketplace-instance\n")
	config.SetTestOrganizationConfigPath(t, organizationPath)
	withWorkingDir(t, t.TempDir())

	output, err := executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("repeat organization Marketplace registration: %v\n%s", err, output)
	}
	if output != "Marketplace "+server.URL+" is already configured in "+organizationPath+"\n" {
		t.Fatalf("output = %q", output)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("idempotent registration wrote user config, stat error = %v", err)
	}
}

func TestInstallMarketplaceEndpointPromotesFromNarrowerScope(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
	defer server.Close()
	workspace := t.TempDir()
	repoPath := filepath.Join(workspace, ".agents", "config.yaml")
	writeConfig(t, repoPath, "marketplaces:\n  - url: "+server.URL+"\n    instance_id: marketplace-instance")
	withWorkingDir(t, workspace)

	output, err := executeCommand("install", server.URL)
	if err != nil {
		t.Fatalf("promote repo Marketplace registration: %v\n%s", err, output)
	}
	userPath := filepath.Join(homeDir, ".agents", "config.yaml")
	if output != "Added Marketplace "+server.URL+" to "+userPath+"\n" {
		t.Fatalf("output = %q", output)
	}
	cfg, err := config.LoadConfigFromPath(userPath)
	if err != nil || len(cfg.Marketplaces) != 1 {
		t.Fatalf("user Marketplace config = %#v, %v", cfg, err)
	}
}

func TestInstallMarketplaceEndpointRejectsPolicyBeforeNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	requests := 0
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", &requests)
	defer server.Close()
	organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
	writeFile(t, organizationPath, "apiVersion: agent-marketplace/v2\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
	config.SetTestOrganizationConfigPath(t, organizationPath)
	withWorkingDir(t, t.TempDir())

	_, err := executeCommand("install", server.URL)
	if err == nil || !strings.Contains(err.Error(), "rejected before network access") {
		t.Fatalf("policy error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("blocked registration made %d request(s)", requests)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("blocked registration wrote user config, stat error = %v", err)
	}
}

func TestInstallMarketplaceEndpointRejectsCanonicalURLMismatch(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	server := newMarketplaceEndpointServer(t, "marketplace-instance", "https://canonical.example", nil)
	defer server.Close()
	withWorkingDir(t, t.TempDir())

	_, err := executeCommand("install", server.URL)
	if err == nil || !strings.Contains(err.Error(), `reports canonical URL "https://canonical.example"`) {
		t.Fatalf("canonical mismatch error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("canonical mismatch wrote user config, stat error = %v", err)
	}
}

func TestInstallMarketplaceEndpointRejectsInvalidWellKnownWithoutMutation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name   string
		path   string
		status int
		body   string
	}{
		{name: "malformed JSON", status: http.StatusOK, body: "{"},
		{name: "missing identity", status: http.StatusOK, body: `{}`},
		{name: "server error", status: http.StatusInternalServerError, body: `unavailable`},
		{name: "path-prefixed endpoint", path: "/services/marketplace", status: http.StatusOK, body: "{"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			withWorkingDir(t, t.TempDir())

			_, err := executeCommand("install", server.URL+tc.path)
			if err == nil || !strings.Contains(err.Error(), "well-known document") {
				t.Fatalf("invalid well-known error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(homeDir, ".agents", "config.yaml")); !os.IsNotExist(err) {
				t.Fatalf("invalid well-known document wrote user config, stat error = %v", err)
			}
		})
	}
}

func TestInstallMarketplaceEndpointRejectsIdentityConflictsWithoutMutation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name          string
		configuredURL string
		configuredID  string
		serverID      string
		want          string
	}{
		{name: "same URL different instance", configuredID: "old-instance", serverID: "new-instance", want: "now reports"},
		{name: "same instance different URL", configuredURL: "https://other.example", configuredID: "marketplace-instance", serverID: "marketplace-instance", want: "migration must be explicit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			server := newMarketplaceEndpointServer(t, tc.serverID, "", nil)
			defer server.Close()
			configuredURL := tc.configuredURL
			if configuredURL == "" {
				configuredURL = server.URL
			}
			configPath := filepath.Join(homeDir, ".agents", "config.yaml")
			writeConfig(t, configPath, "marketplaces:\n  - url: "+configuredURL+"\n    instance_id: "+tc.configuredID)
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			withWorkingDir(t, t.TempDir())

			_, err = executeCommand("install", server.URL)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("identity conflict error = %v", err)
			}
			after, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("identity conflict changed config:\nbefore:\n%safter:\n%s", before, after)
			}
		})
	}
}

func newMarketplaceEndpointServer(t *testing.T, instanceID, canonicalURL string, requests *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			*requests++
		}
		if !strings.HasSuffix(r.URL.Path, "/.well-known/agent-marketplace.json") {
			http.NotFound(w, r)
			return
		}
		canonical := canonicalURL
		if canonical == "" {
			basePath := strings.TrimSuffix(r.URL.Path, "/.well-known/agent-marketplace.json")
			canonical = "http://" + r.Host + basePath
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(instanceID, canonical))
	}))
}

func TestMarketplaceInstallOutputShowsCompactVerifiedDetails(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	result := &pipeline.InstallResult{
		Installed:          []string{"review", "fix", "explain"},
		IsCollection:       true,
		CollectionLabel:    "coderabbit-skills",
		MarketplacePreview: "Marketplace install preview:\ncontent_digest: sha256:secret\ncollection_leaves: [{\"name\":\"review\"}]",
		MarketplaceSummary: testMarketplaceInstallSummary(),
	}

	var compact bytes.Buffer
	if err := writeInstallResult(&compact, result, pipeline.ScopeRepo, false, false); err != nil {
		t.Fatal(err)
	}
	want := "Installing coderabbit-skills · collection\n\nIncludes 3 skills:\n  explain\n  fix\n  review\n\nSource: awesome-ai\n\n✓ Installed coderabbit-skills · 3 skills\n"
	if got := compact.String(); got != want {
		t.Fatalf("compact output = %q, want %q", got, want)
	}
	for _, hidden := range []string{"sha256:secret", "collection_leaves", "content_digest"} {
		if strings.Contains(compact.String(), hidden) {
			t.Fatalf("compact output leaked %q: %s", hidden, compact.String())
		}
	}

	var verbose bytes.Buffer
	if err := writeInstallResult(&verbose, result, pipeline.ScopeRepo, true, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Includes 3 skills:", "Marketplace install preview:", "content_digest:", "collection_leaves:", "✓ Installed coderabbit-skills · 3 skills"} {
		if !strings.Contains(verbose.String(), want) {
			t.Fatalf("verbose output missing %q:\n%s", want, verbose.String())
		}
	}
}

func TestMarketplaceInstallDetailsTruncateLargeCollections(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	resources := make([]pipeline.MarketplaceInstallResource, 12)
	for i := range resources {
		resources[i] = pipeline.MarketplaceInstallResource{Name: fmt.Sprintf("skill-%02d", i+1), Kind: "skill"}
	}
	summary := pipeline.MarketplaceInstallSummary{Name: "large", Kind: "collection", Sources: []string{"awesome-ai"}, Resources: resources}
	var compact bytes.Buffer
	if err := writeMarketplaceInstallDetails(&compact, summary, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compact.String(), "skill-11") || !strings.Contains(compact.String(), "… and 2 more (use --verbose)") {
		t.Fatalf("large compact inventory was not truncated:\n%s", compact.String())
	}
	var verbose bytes.Buffer
	if err := writeMarketplaceInstallDetails(&verbose, summary, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verbose.String(), "skill-12") || strings.Contains(verbose.String(), "… and") {
		t.Fatalf("verbose inventory was truncated:\n%s", verbose.String())
	}

	mixed := pipeline.MarketplaceInstallSummary{Name: "mixed", Kind: "collection", Resources: []pipeline.MarketplaceInstallResource{{Name: "deploy", Kind: "command"}, {Name: "review", Kind: "skill"}}}
	var grouped bytes.Buffer
	if err := writeMarketplaceInstallDetails(&grouped, mixed, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Includes 2 resources:", "  Commands:\n    deploy", "  Skills:\n    review"} {
		if !strings.Contains(grouped.String(), want) {
			t.Fatalf("mixed inventory missing %q:\n%s", want, grouped.String())
		}
	}
}

func TestMarketplaceInstallResultColorsOnTerminal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	oldStyle := installOutputStyle
	defer func() { installOutputStyle = oldStyle }()
	installOutputStyle = func(io.Writer) syncOutputStyle { return syncOutputStyle{color: true} }

	var success bytes.Buffer
	result := &pipeline.InstallResult{Installed: []string{"review"}, MarketplaceSummary: pipeline.MarketplaceInstallSummary{Name: "review", Kind: "skill"}}
	if err := writeInstallResult(&success, result, pipeline.ScopeRepo, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(success.String(), "\x1b[32m✓ Installed review\x1b[0m") {
		t.Fatalf("success was not green: %q", success.String())
	}

	var warning bytes.Buffer
	consent := &pipeline.MarketplaceConsentError{Summary: testMarketplaceInstallSummary(), Acceptable: true}
	if err := writeMarketplaceConsentRequired(&warning, consent, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warning.String(), "\x1b[38;5;208mWarning: 2 security findings\x1b[0m") || !strings.Contains(warning.String(), "\x1b[31mNothing has been installed.\x1b[0m") {
		t.Fatalf("warning/failure colors missing: %q", warning.String())
	}
}

func TestMarketplaceRiskConfirmationShowsDetailsAndAcceptsYes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	prepared := &pipeline.PreparedMarketplaceInstall{}
	consent := &pipeline.MarketplaceConsentError{Preview: "content_digest: sha256:secret", Prepared: prepared, Summary: testMarketplaceInstallSummary(), Acceptable: true, Err: errors.New("risk acceptance required")}
	oldCommit := commitMarketplaceInstall
	defer func() { commitMarketplaceInstall = oldCommit }()
	commitMarketplaceInstall = func(_ context.Context, got *pipeline.PreparedMarketplaceInstall) (*pipeline.InstallResult, error) {
		if got != prepared {
			t.Fatal("confirmation did not commit the prepared plan")
		}
		return &pipeline.InstallResult{Installed: []string{"review", "fix", "explain"}, IsCollection: true, MarketplaceSummary: consent.Summary}, nil
	}

	var out bytes.Buffer
	result, err := confirmMarketplaceInstall(context.Background(), &out, strings.NewReader("y\n"), consent, false)
	if err != nil || result == nil {
		t.Fatalf("accepted install = %#v, %v", result, err)
	}
	for _, want := range []string{"Includes 3 skills:", "Source: awesome-ai", "Warning: 2 security findings", "MEDIUM  [coderabbit-skills] Reviews pull requests", "LOW  [fix] Writes source files", "Active capabilities: read, write", "Install despite these risks? [y/N]: "} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("confirmation output missing %q:\n%s", want, out.String())
		}
	}
	for _, hidden := range []string{"content_digest", "View details", "Choose [1-3]"} {
		if strings.Contains(out.String(), hidden) {
			t.Fatalf("confirmation output retained %q:\n%s", hidden, out.String())
		}
	}
	if err := writeInstallResult(&out, result, pipeline.ScopeRepo, false, true); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "Installing coderabbit-skills") != 1 || !strings.HasSuffix(out.String(), "✓ Installed coderabbit-skills · 3 skills\n") {
		t.Fatalf("post-confirmation output repeated install details:\n%s", out.String())
	}
}

func TestMarketplaceRiskConfirmationDefaultsToNo(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	consent := &pipeline.MarketplaceConsentError{Prepared: &pipeline.PreparedMarketplaceInstall{}, Summary: testMarketplaceInstallSummary(), Acceptable: true, Err: errors.New("risk acceptance required")}
	oldCommit := commitMarketplaceInstall
	defer func() { commitMarketplaceInstall = oldCommit }()
	commitMarketplaceInstall = func(context.Context, *pipeline.PreparedMarketplaceInstall) (*pipeline.InstallResult, error) {
		t.Fatal("default no committed the install")
		return nil, nil
	}
	var out bytes.Buffer
	result, err := confirmMarketplaceInstall(context.Background(), &out, strings.NewReader("\n"), consent, false)
	if err != nil || result != nil || !strings.HasSuffix(out.String(), "Nothing has been installed.\n") {
		t.Fatalf("default no result/output = %#v/%q/%v", result, out.String(), err)
	}
}

func TestMarketplaceBlockedInstallIsAlwaysSurfaced(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	consent := &pipeline.MarketplaceConsentError{Summary: testMarketplaceInstallSummary(), Err: errors.New("blocked-resource: policy rejection")}
	var out bytes.Buffer
	if err := writeMarketplaceConsentRequired(&out, consent, false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Blocked: 2 security findings", "Includes 3 skills:", "Nothing has been installed."} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("blocked output missing %q: %s", want, out.String())
		}
	}
}

func testMarketplaceInstallSummary() pipeline.MarketplaceInstallSummary {
	return pipeline.MarketplaceInstallSummary{
		Name: "coderabbit-skills", Kind: "collection", Sources: []string{"awesome-ai"},
		Resources:    []pipeline.MarketplaceInstallResource{{Name: "explain", Kind: "skill"}, {Name: "fix", Kind: "skill"}, {Name: "review", Kind: "skill"}},
		Findings:     []pipeline.MarketplaceInstallFinding{{Resource: "coderabbit-skills", Severity: "medium", Message: "Reviews pull requests"}, {Resource: "fix", Severity: "low", Message: "Writes source files"}},
		Capabilities: []string{"read", "write"},
	}
}

func TestInstallCommandNormalizesMCPKindForMarketplace(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	kind, ok, err := parseOptionalKind("mcp")
	if err != nil {
		t.Fatalf("parseOptionalKind: %v", err)
	}
	if !ok || kind != ir.KindMcpServer {
		t.Fatalf("kind = %v/%v, want mcpserver/true", kind, ok)
	}
}

func TestInstallValidationAllowsSelectorAndBareCoordinateWithMarketplaceFlags(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, flag := range []string{"--as=review-copy", "--yes", "--accept-risk"} {
		t.Run(flag, func(t *testing.T) {
			cmd := newInstallCmd()
			cmd.SetOut(io.Discard)
			if err := cmd.Flags().Set("marketplace", "public"); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(flag, "=") {
				name := strings.SplitN(flag, "=", 2)
				if err := cmd.Flags().Set(name[0][2:], name[1]); err != nil {
					t.Fatal(err)
				}
			} else if err := cmd.Flags().Set(flag[2:], "true"); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Args(cmd, []string{"source/skill/review"}); err != nil {
				t.Fatalf("validation rejected %s: %v", flag, err)
			}
		})
	}
}

func TestInstallValidationRejectsMarketplaceSelectorCombinations(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	tests := []struct {
		name  string
		flags map[string]string
		args  []string
	}{
		{name: "from", flags: map[string]string{"marketplace": "public", "from": "https://registry.example"}, args: []string{"source/skill/review"}},
		{name: "all", flags: map[string]string{"marketplace": "public", "all": "true"}},
		{name: "tag", flags: map[string]string{"marketplace": "public", "tag": "security"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newInstallCmd()
			for flag, value := range tc.flags {
				if err := cmd.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Args(cmd, tc.args); err == nil {
				t.Fatal("validation unexpectedly accepted invalid Marketplace combination")
			}
		})
	}
}

func TestInstallCommandAddsSkillToRepoConfigAndSyncsIt(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "vm-lifecycle")
	if err != nil {
		t.Fatalf("expected install command to succeed: %v\n%s", err, output)
	}

	if output != "Installed vm-lifecycle in this repo\n" {
		t.Fatalf("expected install output, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install: %v", err)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected repo config registry to include installed skill, got %#v", cfg.Registries[0].Skills)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "main"), "vm-lifecycle"))
}

func TestInstallCommandAddsSkillToExistingRegistrySpecificList(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack", "puppet"})
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", []string{"k8s"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
    skills:
      - vm-lifecycle
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "k8s-onboarding")
	if err != nil {
		t.Fatalf("expected install command to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install: %v", err)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle", "k8s-onboarding"}) {
		t.Fatalf("expected registry-specific skills to include installed skill, got %#v", cfg.Registries[0].Skills)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "k8s-onboarding"), filepath.Join(cache.RegistryCachePath(origin, "main"), "k8s-onboarding"))
}

func TestInstallCommandAddsTagToRepoConfigAndSyncsMatchingSkills(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "k8s-app-onboarding", "Kubernetes app onboarding", []string{"k8s", "helm"})
	writeSkill(t, working, "k8s-conventions", "Kubernetes conventions", []string{"k8s"})
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--tag", "k8s")
	if err != nil {
		t.Fatalf("expected tag install command to succeed: %v\n%s", err, output)
	}

	if output != "Installed k8s-app-onboarding, k8s-conventions in this repo\n" {
		t.Fatalf("expected tag install output, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after tag install: %v", err)
	}

	if len(cfg.Tags) != 0 {
		t.Fatalf("expected no top-level tag, got %#v", cfg.Tags)
	}
	if len(cfg.Registries) != 1 || !reflect.DeepEqual(cfg.Registries[0].Tags, []string{"k8s"}) {
		t.Fatalf("expected registry-scoped tag, got %#v", cfg.Registries)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "k8s-app-onboarding"), filepath.Join(cache.RegistryCachePath(origin, "main"), "k8s-app-onboarding"))
	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "k8s-conventions"), filepath.Join(cache.RegistryCachePath(origin, "main"), "k8s-conventions"))
}

func TestInstallCommandAddsAllDiscoveredSkillsToRepoConfigAndSyncsThem(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "k8s-app-onboarding", "Kubernetes app onboarding", []string{"k8s"})
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err != nil {
		t.Fatalf("expected install --all command to succeed: %v\n%s", err, output)
	}

	if output != "Installed k8s-app-onboarding, vm-lifecycle in this repo\n" {
		t.Fatalf("expected install --all output, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --all: %v", err)
	}

	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"k8s-app-onboarding", "vm-lifecycle"}) {
		t.Fatalf("expected repo config registry to include all discovered skills, got %#v", cfg.Registries[0].Skills)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "k8s-app-onboarding"), filepath.Join(cache.RegistryCachePath(origin, "main"), "k8s-app-onboarding"))
	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "main"), "vm-lifecycle"))
}

func TestInstallCommandAddsAllDiscoveredSkillsForSingleRegistryWhenRegistryFlagIsSet(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	infraOrigin, infraWorking := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, infraWorking, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, infraWorking, "network-change-workflow", "Network change workflow", []string{"network"})
	commitAndPushAll(t, infraWorking, "Add infra skills")

	securityOrigin, securityWorking := createBareRepoWithWorkingClone(t, "example-security-skills.git")
	writeSkill(t, securityWorking, "secret-scanning", "Secret scanning", []string{"security"})
	commitAndPushAll(t, securityWorking, "Add security skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+infraOrigin+`
    ref: main
  - url: `+securityOrigin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all", "--registry", securityOrigin)
	if err != nil {
		t.Fatalf("expected install --all --registry command to succeed: %v\n%s", err, output)
	}

	if output != "Installed secret-scanning in this repo\n" {
		t.Fatalf("expected install --all --registry output, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after registry-scoped install --all: %v", err)
	}

	if got := cfg.Registries[0].Skills; !got.IsZero() {
		t.Fatalf("expected non-targeted registry skills to remain unset, got %#v", got)
	}

	if got := cfg.Registries[1].Skills.Include; !reflect.DeepEqual(got, []string{"secret-scanning"}) {
		t.Fatalf("expected targeted registry skills to include all discovered skills, got %#v", got)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "secret-scanning"), filepath.Join(cache.RegistryCachePath(securityOrigin, "main"), "secret-scanning"))
	if _, err := os.Lstat(filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle")); !os.IsNotExist(err) {
		t.Fatalf("expected non-targeted registry skill to remain unsynced, got err=%v", err)
	}
}

func TestInstallCommandFailsWhenInstallAllFindsCrossRegistryNameCollision(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	infraOrigin, infraWorking := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, infraWorking, "shared-skill", "Shared skill", []string{"infra"})
	commitAndPushAll(t, infraWorking, "Add infra skills")

	securityOrigin, securityWorking := createBareRepoWithWorkingClone(t, "example-security-skills.git")
	writeSkill(t, securityWorking, "shared-skill", "Shared skill", []string{"security"})
	commitAndPushAll(t, securityWorking, "Add security skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+infraOrigin+`
    ref: main
  - url: `+securityOrigin+`
    ref: main
`)

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected initial config to be readable: %v", err)
	}

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err == nil {
		t.Fatalf("expected install --all command to fail, got success with output %q", output)
	}

	if !strings.Contains(err.Error(), `skill "shared-skill" is provided by multiple registries`) {
		t.Fatalf("expected collision error, got %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to remain readable: %v", err)
	}

	if string(after) != string(before) {
		t.Fatalf("expected config to remain unchanged on collision\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInstallCommandFailsWhenSkillDoesNotExist(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
`)

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected initial config to be readable: %v", err)
	}

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "missing-skill")
	if err == nil {
		t.Fatalf("expected install command to fail, got success with output %q", output)
	}

	if !strings.Contains(err.Error(), "\"missing-skill\" not found in configured registries") {
		t.Fatalf("expected missing skill error, got %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to remain readable: %v", err)
	}

	if string(after) != string(before) {
		t.Fatalf("expected config to remain unchanged on failure\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInstallCommandFailsWhenSkillIsMalformed(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "awesome-ai.git")
	writeFile(t, filepath.Join(working, "skills", "broken-skill", "SKILL.md"), `---
name: broken-skill
description: [unterminated
metadata:
  tags:
    - broken
---

# Broken
`)
	commitAndPushAll(t, working, "Add malformed skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
`)

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected initial config to be readable: %v", err)
	}

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "broken-skill")
	if err == nil {
		t.Fatalf("expected install command to fail for malformed skill, got success with output %q", output)
	}

	for _, needle := range []string{
		"skill \"broken-skill\" cannot be installed",
		"failed to parse",
		"skills/broken-skill/SKILL.md",
		"did not find expected ',' or ']'",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected install output to contain %q, got:\n%s", needle, output)
		}
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to remain readable: %v", err)
	}

	if string(after) != string(before) {
		t.Fatalf("expected config to remain unchanged on failure\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInstallCommandFailsWhenTagDoesNotExist(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	configPath := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, configPath, `
registries:
  - url: `+origin+`
    ref: main
`)

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected initial config to be readable: %v", err)
	}

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--tag", "missing-tag")
	if err == nil {
		t.Fatalf("expected tag install command to fail, got success with output %q", output)
	}

	if !strings.Contains(err.Error(), "tag \"missing-tag\" not found in configured registries") {
		t.Fatalf("expected missing tag error, got %v", err)
	}

	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("expected config to remain readable: %v", err)
	}

	if string(after) != string(before) {
		t.Fatalf("expected config to remain unchanged on failure\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestInstallSubagentWithWindsurfTargetReturnsSuccess(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-agents.git")
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "Add subagent")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - windsurf
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--kind", "subagent", "worker-generic")
	if err != nil {
		t.Fatalf("expected subagent install to succeed for windsurf target: %v\n%s", err, output)
	}

	if output != "Installed worker-generic in this repo\n" {
		t.Fatalf("expected install output, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after subagent install: %v", err)
	}
	if !reflect.DeepEqual(cfg.Registries[0].Subagents.Include, []string{"worker-generic"}) {
		t.Fatalf("expected repo config registry to include installed subagent, got %#v", cfg.Registries[0].Subagents)
	}
}

func TestInstallFromFlagUsesRegistryDefaultBranchWhenRefIsOmitted(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingCloneOnBranch(t, "example-infra-skills.git", "trunk")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	commitAndPushAllToBranch(t, working, "Add infra skill", "trunk")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "vm-lifecycle", "--from", origin)
	if err != nil {
		t.Fatalf("expected install --from without --ref to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --from: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry entry, got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].Ref != "trunk" {
		t.Fatalf("expected registry default branch to be persisted as ref=trunk, got %q", cfg.Registries[0].Ref)
	}
	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected registry to include vm-lifecycle, got %#v", cfg.Registries[0].Skills)
	}
}

func TestInstallFromFlagInstallsNamedResourceFromAdHocRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", []string{"k8s"})
	commitAndPushAll(t, working, "Add infra skills")

	// Start with a minimal config so FindRepoRoot can locate the repo.
	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "vm-lifecycle", "--from", origin, "--ref", "main")
	if err != nil {
		t.Fatalf("expected install --from to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed vm-lifecycle") {
		t.Fatalf("expected install output for vm-lifecycle, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --from: %v", err)
	}

	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry entry, got %d", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != origin {
		t.Fatalf("expected registry URL %q, got %q", origin, cfg.Registries[0].URL)
	}
	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected registry to include vm-lifecycle, got %#v", cfg.Registries[0].Skills)
	}

	assertSymlinkTarget(t, filepath.Join(repoRoot, ".agents", "skills", "vm-lifecycle"), filepath.Join(cache.RegistryCachePath(origin, "main"), "vm-lifecycle"))
}

func TestInstallFromFlagWithoutNameInstallsAllResources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	writeSkill(t, working, "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, working, "k8s-onboarding", "Kubernetes onboarding", []string{"k8s"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", origin, "--ref", "main")
	if err != nil {
		t.Fatalf("expected install --from (bundle mode) to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed k8s-onboarding, vm-lifecycle") {
		t.Fatalf("expected install output for both skills, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --from: %v", err)
	}

	if !cfg.Registries[0].Default {
		t.Fatalf("expected registry entry to have default=true (bundle mode), got false")
	}
}

func TestInstallFromFlagWithKindFilter(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-agents-resources.git")
	writeSkill(t, working, "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "Add mixed resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", origin, "--ref", "main", "--kind", "subagent")
	if err != nil {
		t.Fatalf("expected install --from --kind subagent to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed worker-generic") {
		t.Fatalf("expected only subagent installed, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	// Bundle mode with --kind uses default:true; --kind only filters reported names.
	if !cfg.Registries[0].Default {
		t.Fatalf("expected registry entry to have default=true (bundle mode with --kind), got false")
	}
}

func TestInstallFromFlagWithWebURLInfersResourceName(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-infra-skills.git")
	// Each skill lives in its own subdirectory that acts as a bundle root
	// (contains skills/ dir), so --path can target a specific skill.
	writeSkill(t, filepath.Join(working, "vm-lifecycle"), "vm-lifecycle", "VM lifecycle", []string{"openstack"})
	writeSkill(t, filepath.Join(working, "k8s-onboarding"), "k8s-onboarding", "Kubernetes onboarding", []string{"k8s"})
	commitAndPushAll(t, working, "Add infra skills")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	// Install without a name arg — the skill name should be inferred from the URL path.
	// We simulate a web URL by using --from with --path pointing to a specific bundle dir.
	output, err := executeCommand("install", "--from", origin, "--ref", "main", "--path", "vm-lifecycle")
	if err != nil {
		t.Fatalf("expected install --from --path (name inference) to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed vm-lifecycle") {
		t.Fatalf("expected vm-lifecycle installed via name inference, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry entry, got %d", len(cfg.Registries))
	}
	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("expected only vm-lifecycle included (not bundle mode), got %#v", cfg.Registries[0].Skills)
	}
}

func TestInstallFromFlagIncompatibleWithTagAndAll(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "--from", "some-url", "--tag", "k8s")
	if err == nil {
		t.Fatal("expected --from + --tag to fail")
	}
	if !strings.Contains(err.Error(), "cannot specify both --from and --tag") {
		t.Fatalf("expected incompatible flags error, got %v", err)
	}

	_, err = executeCommand("install", "--from", "some-url", "--all")
	if err == nil {
		t.Fatal("expected --from + --all to fail")
	}
	if !strings.Contains(err.Error(), "cannot specify both --from and --all") {
		t.Fatalf("expected incompatible flags error, got %v", err)
	}
}

func TestInstallFromFlagRefAndPathRequireFrom(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "some-skill", "--ref", "main")
	if err == nil {
		t.Fatal("expected --ref without --from to fail")
	}
	if !strings.Contains(err.Error(), "--ref requires --from") {
		t.Fatalf("expected --ref requires --from error, got %v", err)
	}

	_, err = executeCommand("install", "some-skill", "--path", "sub/dir")
	if err == nil {
		t.Fatal("expected --path without --from to fail")
	}
	if !strings.Contains(err.Error(), "--path requires --from") {
		t.Fatalf("expected --path requires --from error, got %v", err)
	}
}

func TestInstallFromFlagBlockedURLReturnsError(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
allowed_registries:
  - ^gitlab\.example\.com/
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "some-skill", "--from", "https://github.com/evil/repo.git")
	if err == nil {
		t.Fatal("expected blocked URL to fail")
	}
	if !strings.Contains(err.Error(), "blocked by allowed_registries") {
		t.Fatalf("expected blocked URL error, got %v", err)
	}
}

func TestInstallFromFlagAcceptsScopeLocal(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	// --from --scope local is now allowed (LREG-005): the local config
	// layer is visible during discovery. The command should proceed
	// past scope validation and fail only for an invalid/bogus URL.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
allowed_registries:
  - ^gitlab\.example\.com/
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "--from", "some-url", "--scope", "local")
	if err == nil {
		t.Fatal("expected --from with bogus URL to fail")
	}
	// The rejection guard for --from --scope local has been removed.
	// The failure should come from allowlist validation, not from
	// a "--from cannot be combined with --scope local" error.
	if strings.Contains(err.Error(), "--from cannot be combined with --scope local") {
		t.Fatalf("should no longer reject --from --scope local, got %v", err)
	}
}

func TestInstallAllDiscoversAllResourceKinds(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "mixed-resources.git")
	// Use bare-plugin layout: skills/ dir alongside commands/ and agents/.
	writeSkill(t, working, "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	writeCommand(t, working, "build-project", "Build the project", "make build")
	commitAndPushAll(t, working, "Add mixed resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err != nil {
		t.Fatalf("expected install --all with mixed kinds to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --all: %v", err)
	}

	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	entry := cfg.Registries[0]

	// Verify skills were installed.
	if !reflect.DeepEqual(entry.Skills.Include, []string{"deploy-helper"}) {
		t.Fatalf("expected skills [deploy-helper], got %#v", entry.Skills.Include)
	}
	// Verify subagents were installed.
	if !reflect.DeepEqual(entry.Subagents.Include, []string{"worker-generic"}) {
		t.Fatalf("expected subagents [worker-generic], got %#v", entry.Subagents.Include)
	}
	// Verify commands were installed.
	if !reflect.DeepEqual(entry.Commands.Include, []string{"build-project"}) {
		t.Fatalf("expected commands [build-project], got %#v", entry.Commands.Include)
	}
}

func TestInstallAllPreservesExistingRepoConfigFields(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "preserve-config.git")
	writeSkill(t, working, "new-skill", "New skill", []string{"test"})
	writeCommand(t, working, "new-cmd", "New command", "echo new")
	commitAndPushAll(t, working, "Add resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
tags:
  - existing-tag
registries:
  - url: `+origin+`
    ref: main
    skills:
      - already-installed-skill
    commands:
      include:
        - already-installed-cmd
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all")
	if err != nil {
		t.Fatalf("expected install --all to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --all: %v", err)
	}

	// Tags must be preserved.
	if !reflect.DeepEqual(cfg.Tags, []string{"existing-tag"}) {
		t.Fatalf("expected tags [existing-tag], got %v", cfg.Tags)
	}

	// Existing skills must be preserved alongside new ones.
	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	entry := cfg.Registries[0]

	expectedSkills := []string{"already-installed-skill", "new-skill"}
	if !reflect.DeepEqual(entry.Skills.Include, expectedSkills) {
		t.Fatalf("expected skills %v, got %v", expectedSkills, entry.Skills.Include)
	}

	expectedCommands := []string{"already-installed-cmd", "new-cmd"}
	if !reflect.DeepEqual(entry.Commands.Include, expectedCommands) {
		t.Fatalf("expected commands %v, got %v", expectedCommands, entry.Commands.Include)
	}

	// Verify the raw YAML file still contains the apiVersion and tags.
	raw, err := os.ReadFile(filepath.Join(repoRoot, ".agents", "config.yaml"))
	if err != nil {
		t.Fatalf("read repo config: %v", err)
	}
	if !strings.Contains(string(raw), "apiVersion:") {
		t.Fatal("repo config YAML missing apiVersion after install --all")
	}
	if !strings.Contains(string(raw), "existing-tag") {
		t.Fatal("repo config YAML missing existing-tag after install --all")
	}
}

func TestInstallAllWithKindFlagFiltersToSingleKind(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "mixed-resources.git")
	writeSkill(t, filepath.Join(working, "skills"), "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "Add mixed resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--all", "--kind", "subagent")
	if err != nil {
		t.Fatalf("expected install --all --kind subagent to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	entry := cfg.Registries[0]
	// Only subagents should be installed, not skills.
	if !reflect.DeepEqual(entry.Subagents.Include, []string{"worker-generic"}) {
		t.Fatalf("expected subagents [worker-generic], got %#v", entry.Subagents.Include)
	}
	if len(entry.Skills.Include) > 0 {
		t.Fatalf("expected no skills installed with --kind subagent, got %#v", entry.Skills.Include)
	}
}

func TestInstallFromBundleModeDiscoversAllResourceKinds(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "mixed-resources.git")
	writeSkill(t, filepath.Join(working, "skills"), "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	writeCommand(t, working, "build-project", "Build the project", "make build")
	commitAndPushAll(t, working, "Add mixed resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", origin, "--ref", "main")
	if err != nil {
		t.Fatalf("expected install --from (bundle mode, multi-kind) to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install --from: %v", err)
	}

	if len(cfg.Registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(cfg.Registries))
	}
	entry := cfg.Registries[0]

	// Bundle mode without --kind sets default:true instead of listing
	// individual includes for each kind.
	if !entry.Default {
		t.Fatalf("expected registry entry to have default=true (bundle mode), got false")
	}
}

func TestInstallFromBundleModeWithKindFilter(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "mixed-resources.git")
	writeSkill(t, filepath.Join(working, "skills"), "deploy-helper", "Deploy helper", []string{"deploy"})
	writeSubagent(t, working, "worker-generic", "Worker", []string{"Read"})
	commitAndPushAll(t, working, "Add mixed resources")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", origin, "--ref", "main", "--kind", "subagent")
	if err != nil {
		t.Fatalf("expected install --from --kind subagent to succeed: %v\n%s", err, output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load: %v", err)
	}

	entry := cfg.Registries[0]
	// Bundle mode with --kind still uses default:true (all resources
	// auto-selected); the --kind flag only filters the reported names.
	if !entry.Default {
		t.Fatalf("expected registry entry to have default=true (bundle mode with --kind), got false")
	}
}

func TestInstallAllWithNonexistentRegistryStillErrors(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "some-registry.git")
	writeSkill(t, working, "some-skill", "Some skill", []string{"test"})
	commitAndPushAll(t, working, "Add skill")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: `+origin+`
    ref: main
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "--all", "--registry", "nonexistent-registry")
	if err == nil {
		t.Fatal("expected install --all --registry <nonexistent> to fail")
	}
	if !strings.Contains(err.Error(), `registry "nonexistent-registry" is not declared in any config layer`) {
		t.Fatalf("expected registry-not-found error, got %v", err)
	}
}

func TestInstallFromMetaDirectoryInstallsAllSubBundleResources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-multi-plugin.git")

	// Create a meta-directory with two sub-bundle directories.
	// Each sub-directory is a bundle root (has skills/ dir), but the
	// parent "plugins" directory is NOT a bundle root.
	writeFile(t, filepath.Join(working, "plugins", "chargeback_analyst", "skills", "chargeback_analyst", "SKILL.md"), `---
name: chargeback-analyst
description: Chargeback analyst
metadata:
  tags:
    - payments
---

# Chargeback Analyst
`)
	writeFile(t, filepath.Join(working, "plugins", "dispute_handler", "skills", "dispute_handler", "SKILL.md"), `---
name: dispute-handler
description: Dispute handler
metadata:
  tags:
    - payments
---

# Dispute Handler
`)
	commitAndPushAll(t, working, "Add multi-plugin structure")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	// Install from the meta-directory (no specific resource name).
	output, err := executeCommand("install", "--from", origin, "--ref", "main", "--path", "plugins")
	if err != nil {
		t.Fatalf("expected install --from meta-directory to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed") {
		t.Fatalf("expected install output to contain 'Installed', got %q", output)
	}
	if !strings.Contains(output, "chargeback-analyst") {
		t.Fatalf("expected output to include chargeback-analyst, got %q", output)
	}
	if !strings.Contains(output, "dispute-handler") {
		t.Fatalf("expected output to include dispute-handler, got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install: %v", err)
	}
	if len(cfg.Registries) == 0 {
		t.Fatal("expected at least one registry entry in config")
	}
}

func TestInstallNamedResourceFromMetaDirectory(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origin, working := createBareRepoWithWorkingClone(t, "example-multi-plugin.git")

	writeFile(t, filepath.Join(working, "plugins", "chargeback_analyst", "skills", "chargeback_analyst", "SKILL.md"), `---
name: chargeback-analyst
description: Chargeback analyst
metadata:
  tags:
    - payments
---

# Chargeback Analyst
`)
	writeFile(t, filepath.Join(working, "plugins", "dispute_handler", "skills", "dispute_handler", "SKILL.md"), `---
name: dispute-handler
description: Dispute handler
metadata:
  tags:
    - payments
---

# Dispute Handler
`)
	commitAndPushAll(t, working, "Add multi-plugin structure")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	// Install a specific resource from the meta-directory.
	output, err := executeCommand("install", "chargeback-analyst", "--from", origin, "--ref", "main", "--path", "plugins")
	if err != nil {
		t.Fatalf("expected install <name> --from meta-directory to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "Installed chargeback-analyst") {
		t.Fatalf("expected output to include chargeback-analyst, got %q", output)
	}
	if strings.Contains(output, "dispute-handler") {
		t.Fatalf("expected output NOT to include dispute-handler (only one resource), got %q", output)
	}

	cfg, err := config.LoadRepoConfig()
	if err != nil {
		t.Fatalf("expected repo config to load after install: %v", err)
	}
	if len(cfg.Registries) == 0 {
		t.Fatal("expected at least one registry entry in config")
	}
}

func TestInstallPositionalURLSuggestsFromFlag(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)
	withWorkingDir(t, repoRoot)

	_, err := executeCommand("install", "https://gitlab.example.com/example-org/infra-skills/-/tree/main/skills/helm-chart-mirroring")
	if err == nil {
		t.Fatal("expected positional URL install to fail with hint about --from")
	}
	if !strings.Contains(err.Error(), "--from") {
		t.Fatalf("expected error to suggest --from, got %v", err)
	}
}

// TestInstallCommandRejectsUserScopeForRepoRelativeRegistry verifies that
// installing a skill from a repo-relative local registry at user scope is
// refused before anything is written to ~/.agents/config.yaml.
func TestInstallCommandRejectsUserScopeForRepoRelativeRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	skillDir := filepath.Join(repoRoot, "plugins", "gitlab-mr", "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ./plugins/gitlab-mr
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "my-skill", "--scope", "user")
	if err == nil {
		t.Fatalf("expected install --scope user to fail for repo-relative registry, got output:\n%s", output)
	}
	combined := err.Error() + "\n" + output
	if !strings.Contains(combined, "repo-relative") || !strings.Contains(combined, "global config") {
		t.Fatalf("expected repo-relative global config error, got err=%v output=%s", err, output)
	}

	// The user config must not contain the repo-relative URL.
	globalConfig := filepath.Join(homeDir, ".agents", "config.yaml")
	if data, readErr := os.ReadFile(globalConfig); readErr == nil {
		if strings.Contains(string(data), "plugins/gitlab-mr") {
			t.Fatalf("repo-relative URL leaked into user config:\n%s", string(data))
		}
	}
}

func TestInstallCommandRejectsUserScopeCollectionFromRepoRelativeRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	registryRoot := filepath.Join(repoRoot, "local-registry", "skills")
	skillDir := filepath.Join(registryRoot, "audit")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: audit\ndescription: Audit\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	collectionDir := filepath.Join(registryRoot, "collections", "security")
	if err := os.MkdirAll(collectionDir, 0o755); err != nil {
		t.Fatalf("mkdir collection: %v", err)
	}
	if err := os.WriteFile(filepath.Join(collectionDir, "COLLECTION.yaml"), []byte("name: security\nskills:\n  - name: audit\n"), 0o644); err != nil {
		t.Fatalf("write collection: %v", err)
	}
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries:
  - url: ./local-registry
    path: skills
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "security", "--scope", "user")
	if err == nil {
		t.Fatalf("expected user-scope collection install to fail, got output:\n%s", output)
	}
	combined := err.Error() + "\n" + output
	if !strings.Contains(combined, "repo-relative") || !strings.Contains(combined, "global config") {
		t.Fatalf("expected repo-relative global config error, got err=%v output=%s", err, output)
	}
	globalConfig := filepath.Join(homeDir, ".agents", "config.yaml")
	if data, readErr := os.ReadFile(globalConfig); readErr == nil && strings.Contains(string(data), "security") {
		t.Fatalf("repo-relative collection leaked into user config:\n%s", string(data))
	}
}

// TestInstallCommandFailsFastOnMissingLocalRegistry verifies that installing
// from a missing local ad-hoc registry fails before writing a broken entry to
// the repo config.
func TestInstallCommandFailsFastOnMissingLocalRegistry(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
registries: []
`)

	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", "./missing-registry", "some-skill")
	if err == nil {
		t.Fatalf("expected install --from missing registry to fail, got output:\n%s", output)
	}
	combined := err.Error() + "\n" + output
	if !strings.Contains(combined, "local registry path does not exist") {
		t.Fatalf("expected missing local registry error, got err=%v output=%s", err, output)
	}

	// No broken registry entry should be persisted.
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	if data, readErr := os.ReadFile(repoConfig); readErr == nil {
		if strings.Contains(string(data), "missing-registry") {
			t.Fatalf("missing local registry URL leaked into repo config:\n%s", string(data))
		}
	}
}

func TestInstallCommandRejectsTraversingLocalRegistryPathBeforeWrite(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "local-registry"), 0o755); err != nil {
		t.Fatalf("mkdir registry: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "outside"), 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, repoConfig, `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", "./local-registry", "--path", "../outside", "some-skill")
	if err == nil {
		t.Fatalf("expected traversing local registry path to fail, got output:\n%s", output)
	}
	if data, readErr := os.ReadFile(repoConfig); readErr == nil && strings.Contains(string(data), "local-registry") {
		t.Fatalf("traversing local registry path leaked into repo config:\n%s", string(data))
	}
}

func TestInstallCommandFailsFastOnMissingLocalRegistryPath(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, "local-registry"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	repoConfig := filepath.Join(repoRoot, ".agents", "config.yaml")
	writeConfig(t, repoConfig, `
registries: []
`)
	withWorkingDir(t, repoRoot)

	output, err := executeCommand("install", "--from", "./local-registry", "--path", "missing", "some-skill")
	if err == nil {
		t.Fatalf("expected install --from with missing path to fail, got output:\n%s", output)
	}
	combined := err.Error() + "\n" + output
	if !strings.Contains(combined, "local registry path does not exist") {
		t.Fatalf("expected missing local registry path error, got err=%v output=%s", err, output)
	}
	if data, readErr := os.ReadFile(repoConfig); readErr == nil && strings.Contains(string(data), "local-registry") {
		t.Fatalf("local registry with missing path leaked into repo config:\n%s", string(data))
	}
}

func TestWriteInstallResultReportsUnselectedResources(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		name   string
		result pipeline.InstallResult
		want   string
	}{
		{"mixed", pipeline.InstallResult{Installed: []string{"cmd-one", "deploy"}, NotSelected: []string{"review"}},
			"Installed cmd-one, deploy in this repo\nNot installed: review; the effective configuration does not select it (for example, an exclusion wins). Install by name to select explicitly\n"},
		{"none installed", pipeline.InstallResult{NotSelected: []string{"review", "lint\x1b[31m"}},
			"Not installed: review, lint\\x1b[31m; the effective configuration does not select them (for example, an exclusion wins). Install by name to select explicitly\n"},
		{"all installed", pipeline.InstallResult{Installed: []string{"review"}}, "Installed review in this repo\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeInstallResult(&out, &tc.result, pipeline.ScopeRepo, false, false); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("output = %q, want %q", got, tc.want)
			}
		})
	}
}
