package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
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
	if fixture == "legacy" {
		source := filepath.Join(home, "fixture-source")
		writeFile(t, filepath.Join(source, "solo", "SKILL.md"), fixtureSkillBody)
		writeLegacyUserRoot(t, home, legacyUserRootBody("agent-marketplace/v2", source))
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
	if fixture == "navigation" {
		source := filepath.Join(home, "navigation-source")
		for i := 0; i < 60; i++ {
			name := fmt.Sprintf("skill-%03d", i)
			body := fmt.Sprintf("---\nname: %s\ndescription: Navigation fixture %s\n---\n# %s\n", name, name, name)
			writeFile(t, filepath.Join(source, name, "SKILL.md"), body)
		}
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"),
			"apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  navigation: {directory: "+source+"}\nresources:\n  navigation: {all: true}\n")
		bootstrap["navigationSourceDir"] = source
	}
	lifecycle := session.RuntimeLifecycle()
	if fixture == "held-sync" {
		markerDir := t.TempDir()
		started := filepath.Join(markerDir, "sync-started")
		release := filepath.Join(markerDir, "sync-release")
		bootstrap["syncStarted"] = started
		bootstrap["syncRelease"] = release
		inner := lifecycle.Sync
		lifecycle.Sync = func(ctx context.Context, ws pipeline.Workspace) ([]error, error) {
			if err := os.WriteFile(started, []byte("1"), 0o644); err != nil {
				return nil, err
			}
			deadline := time.Now().Add(90 * time.Second)
			for {
				if _, err := os.Stat(release); err == nil {
					break
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if time.Now().After(deadline) {
					return nil, fmt.Errorf("held-sync release file never appeared")
				}
				time.Sleep(20 * time.Millisecond)
			}
			return inner(ctx, ws)
		}
	}
	if fixture == "release" {
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
	}
	apiCtx := context.Background()
	var telemetryHarness *localTelemetryHarness
	if mode := os.Getenv("AM_LOCALAPI_TELEMETRY_MODE"); mode != "" {
		orgPath := filepath.Join(t.TempDir(), "org.yaml")
		writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\ngui:\n  version_check: false\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		bootstrap["orgConfig"] = orgPath
		var telemetrySetupErr error
		apiCtx, telemetryHarness, telemetrySetupErr = newLocalTelemetryHarness(t, home, mode, os.Getenv("AM_LOCALAPI_TELEMETRY_CAPTURE"))
		if telemetrySetupErr != nil {
			t.Fatal("setup private telemetry harness")
		}
	}
	api, err := NewUser(apiCtx, home, lifecycle, nil, WithVersion(os.Getenv("AM_LOCALAPI_RELEASE_VERSION")))
	if err != nil {
		if telemetryHarness != nil {
			telemetryHarness.finish(t)
		}
		t.Fatal(err)
	}
	if telemetryHarness == nil {
		defer api.Close()
	} else {
		defer func() {
			api.Close()
			telemetryHarness.finish(t)
		}()
	}
	if marker := os.Getenv("AM_LOCALAPI_RELEASE_MARK"); marker != "" {
		api.releaseClient.Transport = releaseHarnessTransport(marker, os.Getenv("AM_LOCALAPI_RELEASE_STATUS"), os.Getenv("AM_LOCALAPI_RELEASE_BODY"))
	}
	serveHarness(t, api, bootstrap)
}

func releaseHarnessTransport(marker, statusEnv, bodyPath string) http.RoundTripper {
	status := http.StatusOK
	if statusEnv != "" {
		if parsed, err := strconv.Atoi(statusEnv); err == nil {
			status = parsed
		}
	}
	return releaseRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		file, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := file.WriteString(r.Method + " " + r.URL.String() + "\n"); err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := file.Close(); err != nil {
			return nil, err
		}
		var body []byte
		if bodyPath != "" {
			if data, err := os.ReadFile(bodyPath); err == nil {
				body = data
			}
		}
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d", status),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    r,
		}, nil
	})
}
