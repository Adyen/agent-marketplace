package localapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func serveHarness(t *testing.T, api *API, bootstrapFields map[string]string) {
	t.Helper()
	bootstrap := os.NewFile(3, "bootstrap")
	if bootstrap == nil {
		t.Fatal("fd 3 bootstrap pipe is required")
	}
	defer func() { _ = bootstrap.Close() }()

	loopback, err := api.ListenLoopback(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loopback.Close() }()

	bootstrapFields["url"] = loopback.URL
	bootstrapFields["credential"] = api.Credential()
	line, err := json.Marshal(bootstrapFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Minute)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		buf := make([]byte, 1)
		for {
			if _, err := bootstrap.Read(buf); err != nil {
				return
			}
		}
	}()
	select {
	case <-closed:
	case <-deadline:
	}
}

func TestLocalAPIHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_E2E") != "1" {
		t.Skip("real-core harness; enabled only by the Playwright E2E driver")
	}
	ws, home := newFixture(t)
	api, err := New(context.Background(), ws, home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	serveHarness(t, api, map[string]string{"repo": ws.RepoRoot()})
}

func TestLocalAPIUserHarness(t *testing.T) {
	if os.Getenv("AM_LOCALAPI_USER_E2E") != "1" {
		t.Skip("user-scope harness; enabled only by the Playwright E2E driver")
	}
	fixture := os.Getenv("AM_LOCALAPI_USER_FIXTURE")
	home, repo := newUserAPIFixture(t, fixture != "first-run")
	var orgConfigPath string
	if fixture == "first-run" {
		if err := os.MkdirAll(filepath.Join(home, ".factory"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if fixture == "harnesses" {
		userConfig := filepath.Join(home, ".agents", "config.yaml")
		source := filepath.Join(home, "fixture-source")
		writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
		writeFile(t, userConfig,
			"apiVersion: agent-marketplace/v4\nharnesses: [devin, e2e-future-harness]\nsources:\n  local: {directory: "+source+"}\nresources:\n  local: {all: true}\n")
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		orgSource := filepath.Join(t.TempDir(), "org-regs")
		writeFile(t, filepath.Join(orgSource, "orgskill", "SKILL.md"), fixtureSkillBody)
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: "+orgSource+"}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		orgConfigPath = orgPath
	}
	bootstrap := map[string]string{
		"home":      home,
		"repo":      repo,
		"codexHome": os.Getenv("CODEX_HOME"),
		"xdgConfig": os.Getenv("XDG_CONFIG_HOME"),
		"xdgState":  os.Getenv("XDG_STATE_HOME"),
		"xdgCache":  os.Getenv("XDG_CACHE_HOME"),
	}
	if orgConfigPath != "" {
		bootstrap["orgConfig"] = orgConfigPath
	}
	if fixture == "sources" || fixture == "marketplace" || fixture == "sources-git" {
		gitSource := fixture == "sources-git"
		userDir := filepath.Join(home, ".agents")
		keepDir := filepath.Join(home, "keep-source")
		fragDir := filepath.Join(home, "frag-source")
		addDir := filepath.Join(home, "added-source")
		writeFile(t, filepath.Join(keepDir, "solo", "SKILL.md"), fixtureSkillBody)
		writeFile(t, filepath.Join(fragDir, "frag", "SKILL.md"), fixtureSkillBody)
		fragResources := "{all: true}"
		rootLogging := ""
		keepResources := "{all: true}"
		if gitSource {
			fragResources = "{all: true, harnesses: [claude]}"
			rootLogging = "logging: {level: debug}\n"
			keepResources = "{all: true, harnesses: [devin]}"
		} else {
			writeFile(t, filepath.Join(addDir, "added", "SKILL.md"), fixtureSkillBody)
		}
		writeFile(t, filepath.Join(userDir, "fragment.yaml"),
			"sources:\n  frag: {directory: "+fragDir+"}\nresources:\n  frag: "+fragResources+"\n")
		writeFile(t, filepath.Join(userDir, "config.yaml"),
			"apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n"+rootLogging+"sources:\n  keep: {directory: "+keepDir+"}\nresources:\n  keep: "+keepResources+"\n")
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		orgSource := filepath.Join(t.TempDir(), "org-regs")
		writeFile(t, filepath.Join(orgSource, "orgskill", "SKILL.md"), fixtureSkillBody)
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: "+orgSource+"}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
		if gitSource {
			bootstrap["gitSourceURL"] = "file://" + filepath.ToSlash(newBareGitSource(t))
		} else {
			bootstrap["addSourceDir"] = addDir
		}
	}
	if fixture == "marketplace" {
		marketplace := newMarketplaceEndpointTestServer(t, "/playwright/catalog", "playwright-marketplace", "", 0)
		bootstrap["marketplaceEndpoint"] = marketplace.endpoint
	}
	if fixture == "first-run" {
		addDir := filepath.Join(home, "optional-directory")
		writeFile(t, filepath.Join(addDir, "queued", "SKILL.md"), fixtureSkillBody)
		bootstrap["addSourceDir"] = addDir
		marketplace := newMarketplaceEndpointTestServer(t, "/playwright/first-run", "first-run-marketplace", "", 0)
		marketplace.failNextStatus(http.StatusInternalServerError)
		bootstrap["marketplaceEndpoint"] = marketplace.endpoint
	}
	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	serveHarness(t, api, bootstrap)
}
