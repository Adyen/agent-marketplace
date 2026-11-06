package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/session"
)

type startupHostRow struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Kind                string   `json:"kind"`
	Registry            string   `json:"registry"`
	Scope               string   `json:"scope"`
	Checked             bool     `json:"checked"`
	PendingVerification bool     `json:"pendingVerification"`
	ResourceAction      string   `json:"resourceAction"`
	HarnessOverride     bool     `json:"harnessOverride"`
	Harnesses           []string `json:"harnesses"`
}
type startupHostDiagnostic struct {
	Severity string `json:"severity"`
}
type startupHostPayload struct {
	Stage       string                  `json:"stage"`
	Drift       string                  `json:"drift"`
	Rows        []startupHostRow        `json:"rows"`
	Diagnostics []startupHostDiagnostic `json:"diagnostics"`
}
type startupHostEvent struct {
	Type       string             `json:"type"`
	Sequence   uint64             `json:"sequence"`
	Generation uint64             `json:"generation"`
	Payload    startupHostPayload `json:"payload"`
}
type protectedHostPath struct {
	exists    bool
	mode      os.FileMode
	directory bool
	contents  []byte
	entries   []string
}

func startupHostFixture(t *testing.T, userConfig func(string) string) []string {
	t.Helper()
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	home, repo := newHostFixture(t)
	source := filepath.Join(t.TempDir(), "local")
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" {
		t.Fatal("host fixture has no XDG config root")
	}
	for _, name := range []string{"alpha", "beta"} {
		title := strings.ToUpper(name[:1]) + name[1:]
		body := strings.NewReplacer("solo", name, "Solo", title).Replace(fixtureSkillBody)
		writeFile(t, filepath.Join(source, name, "SKILL.md"), body)
	}
	orgConfig := config.OrganizationConfigPath()
	writeFile(t, filepath.Join(home, ".agents", "config.yaml"), userConfig(source))
	homeSkills := filepath.Join(home, ".config", "devin", "skills")
	sentinel := filepath.Join(homeSkills, "unrelated", "SKILL.md")
	writeFile(t, sentinel, "unrelated native sentinel\n")
	return startupProtectedPaths(home, repo, source, orgConfig, xdgConfig, sentinel)
}
func startupProtectedPaths(home, repo, source, orgConfig, xdgConfig, sentinel string) []string {
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	homeNative := filepath.Join(home, ".config", "devin")
	homeSkills := filepath.Join(homeNative, "skills")
	xdgNative := filepath.Join(xdgConfig, "devin")
	xdgSkills := filepath.Join(xdgNative, "skills")
	repoNative := filepath.Join(repo, ".devin")
	repoSkills := filepath.Join(repoNative, "skills")
	return []string{
		userConfig, config.UserPersonal.LockPath(userConfig), filepath.Join(home, ".agents", "config.local.yaml"),
		repoConfig, config.RepoCommitted.LockPath(repoConfig), repoLocal, config.RepoPersonal.LockPath(repoLocal),
		orgConfig, config.Organization.LockPath(orgConfig),
		source, filepath.Join(source, "alpha"), filepath.Join(source, "alpha", "SKILL.md"),
		filepath.Join(source, "beta"), filepath.Join(source, "beta", "SKILL.md"),
		homeNative, homeSkills, filepath.Join(homeSkills, "alpha", "SKILL.md"), filepath.Join(homeSkills, "beta", "SKILL.md"),
		filepath.Join(homeSkills, "unrelated"), sentinel,
		xdgNative, xdgSkills, filepath.Join(xdgSkills, "alpha", "SKILL.md"), filepath.Join(xdgSkills, "beta", "SKILL.md"),
		repoNative, repoSkills, filepath.Join(repoSkills, "alpha", "SKILL.md"), filepath.Join(repoSkills, "beta", "SKILL.md"),
	}
}
func readHostStartupStages(t *testing.T, app *App) []startupHostEvent {
	t.Helper()
	server := httptest.NewServer(app.handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatal("create authenticated startup event request")
	}
	req.Header.Set("X-Agent-Marketplace-Token", app.Bootstrap().Credential)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal("read authenticated startup event stream")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatal("startup event stream returned an unexpected status")
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	stages := make([]startupHostEvent, 0, 4)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event startupHostEvent
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			t.Fatal("decode startup snapshot event")
		}
		if event.Type != "snapshot" {
			continue
		}
		stages = append(stages, event)
		if event.Payload.Stage == "final" {
			return stages
		}
	}
	if scanner.Err() != nil {
		t.Fatal("scan startup event stream")
	}
	t.Fatal("startup stream ended before its first final snapshot")
	return nil
}
func captureProtectedHostPaths(t *testing.T, paths []string) []protectedHostPath {
	t.Helper()
	states := make([]protectedHostPath, len(paths))
	for i, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal("inspect protected host path")
		}
		state := protectedHostPath{exists: true, mode: info.Mode(), directory: info.IsDir()}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				t.Fatal("read protected directory entries")
			}
			for _, entry := range entries {
				state.entries = append(state.entries, entry.Name())
			}
		} else {
			state.contents, err = os.ReadFile(path)
			if err != nil {
				t.Fatal("read protected file contents")
			}
		}
		states[i] = state
	}
	return states
}
func assertProtectedHostPathsUnchanged(t *testing.T, paths []string, before []protectedHostPath) {
	t.Helper()
	after := captureProtectedHostPaths(t, paths)
	if len(before) != len(after) {
		t.Fatal("protected host path count changed")
	}
	for i, prior := range before {
		current := after[i]
		if prior.exists != current.exists || prior.mode != current.mode || prior.directory != current.directory || !bytes.Equal(prior.contents, current.contents) || !reflect.DeepEqual(prior.entries, current.entries) {
			t.Fatal("startup changed a protected host file or directory")
		}
	}
}
func assertHostStartupStageOrder(t *testing.T, events []startupHostEvent) {
	t.Helper()
	want := []string{"provisional", "config", "inventory", "final"}
	if len(events) != len(want) {
		t.Fatal("startup did not deliver exactly four stages through final")
	}
	generation := events[0].Generation
	if generation == 0 {
		t.Fatal("startup stage generation was zero")
	}
	for i, event := range events {
		if event.Type != "snapshot" || event.Payload.Stage != want[i] || event.Generation != generation {
			t.Fatal("startup stage order or generation changed")
		}
		if i > 0 && event.Sequence <= events[i-1].Sequence {
			t.Fatal("startup event sequence was not strictly increasing")
		}
	}
}
func assertHostSkillRows(t *testing.T, rows []startupHostRow, pending bool) {
	t.Helper()
	if len(rows) != 2 {
		t.Fatal("startup snapshot did not contain both directory skills")
	}
	for i, name := range []string{"alpha", "beta"} {
		candidate := rows[i]
		if candidate.ID == "" {
			t.Fatal("startup row had empty opaque identity")
		}
		if candidate.Name != name {
			t.Fatal("startup row name/order mismatch")
		}
		if candidate.Kind != "skill" {
			t.Fatal("startup row kind mismatch")
		}
		if candidate.Registry != "local" {
			t.Fatal("startup row registry label mismatch")
		}
		if candidate.Scope != "user" {
			t.Fatal("startup row scope mismatch")
		}
		if !candidate.Checked {
			t.Fatal("startup row selection state mismatch")
		}
		if candidate.PendingVerification != pending {
			t.Fatal("startup row pending verification flag mismatch")
		}
		if candidate.HarnessOverride || len(candidate.Harnesses) != 1 || candidate.Harnesses[0] != "devin" {
			t.Fatal("startup row harness badge mismatch")
		}
		if pending && candidate.ResourceAction != "none" {
			t.Fatal("unverified startup row exposed a mutation action")
		}
	}
}

// spec: session-startup/startup-snapshot-delivers-config-inventory-then-assessed-stages
// spec: session-startup/user-scope-local-all-true-selections-expand-into-pending-rows
func TestGuiStartupHostStagesUserAllTrue(t *testing.T) {
	paths := startupHostFixture(t, func(source string) string {
		return fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [devin]\ntelemetry:\n  level: off\nsources:\n  local:\n    directory: %q\nresources:\n  local: {all: true}\n", source)
	})
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create user-scope host GUI")
	}
	t.Cleanup(app.close)
	stages := readHostStartupStages(t, app)
	app.close()
	assertHostStartupStageOrder(t, stages)
	assertHostSkillRows(t, stages[0].Payload.Rows, true)
	assertHostSkillRows(t, stages[1].Payload.Rows, true)
	for i := range stages[0].Payload.Rows {
		pending := stages[0].Payload.Rows[i]
		configured := stages[1].Payload.Rows[i]
		if configured.ID != pending.ID || configured.Name != pending.Name || configured.Checked != pending.Checked ||
			configured.Scope != pending.Scope || configured.HarnessOverride != pending.HarnessOverride ||
			!reflect.DeepEqual(configured.Harnesses, pending.Harnesses) {
			t.Fatal("configuration stage changed pending row identity or labels")
		}
	}
	if stages[0].Payload.Drift != "pending" || stages[1].Payload.Drift != "pending" ||
		stages[2].Payload.Stage != "inventory" || stages[2].Payload.Drift != "pending" {
		t.Fatal("startup provisional/config/inventory drift assessments changed")
	}
	assertHostSkillRows(t, stages[2].Payload.Rows, false)
	assertHostSkillRows(t, stages[3].Payload.Rows, false)
	if stages[3].Payload.Drift != "ready" {
		t.Fatal("final startup assessment was not ready")
	}
	for _, diagnostic := range stages[3].Payload.Diagnostics {
		if diagnostic.Severity == "error" {
			t.Fatal("final startup assessment contained an error diagnostic")
		}
	}
	assertProtectedHostPathsUnchanged(t, paths, before)
}

// spec: session-startup/invalid-configuration-delivers-one-fail-closed-final-stage
func TestGuiStartupHostInvalidRootFailsClosed(t *testing.T) {
	paths := startupHostFixture(t, func(string) string { return "apiVersion: [broken\n" })
	before := captureProtectedHostPaths(t, paths)
	app, err := NewApp(context.Background(), session.DependencyLifecycle{})
	if err != nil {
		t.Fatal("create host GUI for invalid-root pipeline")
	}
	t.Cleanup(app.close)
	stages := readHostStartupStages(t, app)
	app.close()
	if len(stages) != 1 || stages[0].Type != "snapshot" || stages[0].Generation == 0 ||
		stages[0].Payload.Stage != "final" || stages[0].Payload.Drift != "failed" || len(stages[0].Payload.Rows) != 0 {
		t.Fatal("invalid user root did not deliver one fail-closed final snapshot")
	}
	hasError := false
	for _, diagnostic := range stages[0].Payload.Diagnostics {
		if diagnostic.Severity == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("invalid user root final snapshot had no error diagnostic")
	}
	assertProtectedHostPathsUnchanged(t, paths, before)
}
