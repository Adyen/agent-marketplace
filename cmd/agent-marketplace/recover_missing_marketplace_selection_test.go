package main

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: deployed-acquisition-locks/optional-removed-selection-is-skipped-while-the-rest-is-locked
// spec: deployed-acquisition-locks/required-removed-selection-fails-without-writing-the-lock
// spec: deployed-acquisition-locks/other-lookup-failures-still-fail-without-writing-the-lock
// spec: resource-removal/remove-a-marketplace-selection-when-the-layer-has-no-lock
// spec: resource-removal/remove-a-marketplace-selection-missing-from-an-existing-lock
func TestRecoverMissingMarketplaceSelectionWithoutLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	t.Run("optional 404 across writing commands and lock states", func(t *testing.T) {
		for _, command := range []string{"sync", "lock"} {
			for _, existingLock := range []bool{false, true} {
				name := command + "/absent-lock"
				if existingLock {
					name = command + "/missing-record"
				}
				t.Run(name, func(t *testing.T) {
					fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone", "keep"}, false)
					if existingLock {
						writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"keep"}, false)
						if output, err := executeCommand("sync"); err != nil {
							t.Fatalf("seed lock: %v\n%s", err, output)
						}
						writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"gone", "keep"}, false)
					}

					beforeConfig, err := os.ReadFile(fixture.configPath)
					if err != nil {
						t.Fatal(err)
					}
					var beforeKeep lockfile.MarketplaceResourceLock
					if existingLock {
						beforeKeep = marketplaceLockResourceByName(t, readLockDocument(t, fixture.lockPath), "keep")
					}

					args := []string{command}
					if command == "lock" {
						args = append(args, "--scope", "repo")
					}
					output, err := executeCommand(args...)
					if err != nil {
						t.Fatalf("%s: %v\n%s", command, err, output)
					}
					for _, want := range []string{fixture.endpoint, "tools", "skill", "gone", "agent-marketplace remove gone"} {
						if !strings.Contains(output, want) {
							t.Errorf("%s output lacks %q:\n%s", command, want, output)
						}
					}
					if got := fixture.goneLookups.Load(); got != 1 {
						t.Fatalf("%s latest lookup count for removed selection = %d, want 1", command, got)
					}
					if after, err := os.ReadFile(fixture.configPath); err != nil || !reflect.DeepEqual(beforeConfig, after) {
						t.Fatalf("%s changed authored config: %v", command, err)
					}

					doc := readLockDocument(t, fixture.lockPath)
					if doc.Scope != lockfile.ScopeRepo || len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 {
						t.Fatalf("%s lock contents = %#v", command, doc)
					}
					if got := doc.Marketplaces[0].URL; got != fixture.endpoint {
						t.Fatalf("%s Marketplace URL = %q", command, got)
					}
					if got := doc.Marketplaces[0].Resources[0].Name; got != "keep" {
						t.Fatalf("%s locked resources = %#v, want only keep", command, doc.Marketplaces[0].Resources)
					}
					if existingLock && !reflect.DeepEqual(beforeKeep, doc.Marketplaces[0].Resources[0]) {
						t.Fatalf("%s changed the unrelated keep record:\nbefore: %#v\nafter: %#v", command, beforeKeep, doc.Marketplaces[0].Resources[0])
					}
					if command == "sync" {
						assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "keep", "SKILL.md"))
						if _, err := os.Stat(filepath.Join(fixture.repoRoot, ".factory", "skills", "gone")); !os.IsNotExist(err) {
							t.Fatalf("removed skill was materialized: %v", err)
						}
					}
				})
			}
		}
	})

	t.Run("required 404 refuses without a lock", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone", "keep"}, true)
		output, err := executeCommand("sync")
		if err == nil {
			t.Fatalf("sync succeeded for a required removed selection:\n%s", output)
		}
		diagnostic := err.Error() + "\n" + output
		for _, want := range []string{"gone", "required", "resources.hosted.required"} {
			if !strings.Contains(diagnostic, want) {
				t.Errorf("required-selection error lacks %q:\n%s", want, diagnostic)
			}
		}
		if _, err := os.Stat(fixture.lockPath); !os.IsNotExist(err) {
			t.Fatalf("required lookup wrote a lock: %v", err)
		}
		if got := fixture.goneLookups.Load(); got != 1 {
			t.Fatalf("required latest lookup count = %d, want 1", got)
		}
	})

	t.Run("server error refuses without a lock", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusServiceUnavailable, []string{"gone", "keep"}, false)
		output, err := executeCommand("sync")
		if err == nil {
			t.Fatalf("sync succeeded after a server error:\n%s", output)
		}
		if !strings.Contains(err.Error()+output, "503") {
			t.Fatalf("server error was not reported:\n%v\n%s", err, output)
		}
		if _, err := os.Stat(fixture.lockPath); !os.IsNotExist(err) {
			t.Fatalf("server error wrote a lock: %v", err)
		}
		if got := fixture.goneLookups.Load(); got != 1 {
			t.Fatalf("failed latest lookup count = %d, want 1", got)
		}
	})

	t.Run("recorded pin stays exact while an unrecorded sibling is acquired", func(t *testing.T) {
		fixture, withdrawn, latestLookups := newRecoverRecordedMarketplaceFixture(t)
		output, err := executeCommand("sync")
		if err != nil {
			t.Fatalf("seed lock: %v\n%s", err, output)
		}
		beforeRecorded := marketplaceLockResourceByName(t, readLockDocument(t, fixture.lockPath), "recorded")
		latestLookups.Store(0)
		withdrawn.Store(true)
		writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"recorded", "new"}, false)

		output, err = executeCommand("sync")
		if err != nil {
			t.Fatalf("sync with an unrecorded sibling: %v\n%s", err, output)
		}
		if strings.Contains(output, "agent-marketplace remove recorded") {
			t.Fatalf("sync treated the recorded pin as removed:\n%s", output)
		}
		if got := latestLookups.Load(); got != 0 {
			t.Fatalf("sync looked up the recorded pin's latest revision %d times", got)
		}
		doc := readLockDocument(t, fixture.lockPath)
		if got := marketplaceLockResourceByName(t, doc, "recorded"); !reflect.DeepEqual(beforeRecorded, got) {
			t.Fatalf("recorded lock entry changed:\nbefore: %#v\nafter: %#v", beforeRecorded, got)
		}
		_ = marketplaceLockResourceByName(t, doc, "new")
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "recorded", "SKILL.md"))
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "new", "SKILL.md"))
	})

	t.Run("explicit revision 404 refuses without a lock", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone", "keep"}, false)
		writeConfig(t, fixture.configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+fixture.endpoint+`}
resources:
  hosted:
    include:
      - tools/skill/gone:
          revision: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - tools/skill/keep
`)
		output, err := executeCommand("sync")
		if err == nil {
			t.Fatalf("sync succeeded for an explicit revision 404:\n%s", output)
		}
		if !strings.Contains(err.Error()+output, "404") {
			t.Fatalf("explicit revision failure was not reported:\n%v\n%s", err, output)
		}
		if _, err := os.Stat(fixture.lockPath); !os.IsNotExist(err) {
			t.Fatalf("explicit revision failure wrote a lock: %v", err)
		}
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("explicit revision performed a latest lookup %d times", got)
		}
	})

	t.Run("remove without a lock", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone", "keep"}, false)
		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err != nil {
			t.Fatalf("remove: %v\n%s", err, output)
		}
		assertRemovedMarketplaceSelection(t, fixture)
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove queried the removed selection %d times", got)
		}

		configAfterRemove, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err = executeCommand("sync")
		if err != nil {
			t.Fatalf("sync after remove: %v\n%s", err, output)
		}
		if after, err := os.ReadFile(fixture.configPath); err != nil || !reflect.DeepEqual(configAfterRemove, after) {
			t.Fatalf("sync changed config after removal: %v", err)
		}
		assertRemainingMarketplaceLock(t, fixture)
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "keep", "SKILL.md"))
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove or following sync queried the removed selection %d times", got)
		}
	})

	t.Run("endpoint-only lock supports removal without another lookup", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, nil, false)
		output, err := executeCommand("lock", "--scope", "repo")
		if err != nil {
			t.Fatalf("create endpoint-only lock: %v\n%s", err, output)
		}
		doc := readLockDocument(t, fixture.lockPath)
		if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].RegistryBindings) != 0 || len(doc.Marketplaces[0].Resources) != 0 {
			t.Fatalf("endpoint-only lock = %#v", doc.Marketplaces)
		}

		writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"gone"}, false)
		output, err = executeCommand("sync")
		if err != nil {
			t.Fatalf("sync only the absent optional selection: %v\n%s", err, output)
		}
		lookupsBeforeRemove := fixture.goneLookups.Load()
		output, err = executeCommand("remove", "gone", "--scope", "repo")
		if err != nil {
			t.Fatalf("remove from endpoint-only lock: %v\n%s", err, output)
		}
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(config), "tools/skill/gone") {
			t.Fatalf("removed request remains in config:\n%s", config)
		}
		if got := fixture.goneLookups.Load(); got != lookupsBeforeRemove {
			t.Fatalf("remove queried the removed selection: lookups before %d, after %d", lookupsBeforeRemove, got)
		}
	})

	t.Run("target evidence is checked when another selection fails first", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, nil, false)
		if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
			t.Fatalf("create endpoint-only lock: %v\n%s", err, output)
		}
		writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"aardvark", "gone"}, false)
		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err != nil {
			t.Fatalf("remove target when another selection is missing: %v\n%s", err, output)
		}
		config, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(config), "tools/skill/gone") || !strings.Contains(string(config), "tools/skill/aardvark") {
			t.Fatalf("remove changed the wrong request:\n%s", config)
		}
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove queried the named selection %d times", got)
		}
	})

	t.Run("remove uses installed alias from warning", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone", "keep"}, false)
		writeConfig(t, fixture.configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+fixture.endpoint+`}
resources:
  hosted:
    include:
      - tools/skill/gone: {as: gone-copy}
      - tools/skill/keep
`)
		output, err := executeCommand("sync")
		if err != nil {
			t.Fatalf("sync: %v\n%s", err, output)
		}
		hint := "agent-marketplace remove gone-copy"
		if !strings.Contains(output, hint) {
			t.Fatalf("warning lacks exact installed-name hint %q:\n%s", hint, output)
		}
		output, err = executeCommand(strings.Fields(hint)[1:]...)
		if err != nil {
			t.Fatalf("execute warning hint: %v\n%s", err, output)
		}
		if got := fixture.goneLookups.Load(); got != 1 {
			t.Fatalf("remove hint queried the removed selection; latest lookups = %d", got)
		}
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "keep", "SKILL.md"))
	})

	t.Run("unqualified name stays ambiguous across Marketplace and directory sources", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone"}, false)
		registry := filepath.Join(fixture.repoRoot, "registry")
		writeSkill(t, registry, "gone", "Directory selection", nil)
		writeConfig(t, fixture.configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+fixture.endpoint+`}
  local: {directory: `+registry+`, source_harness: factory}
resources:
  hosted: {include: [tools/skill/gone]}
  local: {include: [skill/gone]}
`)
		before, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err == nil || !strings.Contains(err.Error()+"\n"+output, "multiple config layers") {
			t.Fatalf("unqualified remove did not report ambiguity: %v\n%s", err, output)
		}
		if after, readErr := os.ReadFile(fixture.configPath); readErr != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("ambiguous remove changed config: %v", readErr)
		}

		output, err = executeCommand("remove", "gone", "--scope", "repo", "--from", fixture.endpoint)
		if err != nil {
			t.Fatalf("remove qualified by Marketplace endpoint: %v\n%s", err, output)
		}
		after, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(after), "tools/skill/gone") || !strings.Contains(string(after), "skill/gone") {
			t.Fatalf("qualified removal changed the wrong selection:\n%s", after)
		}
	})

	t.Run("collection name remains part of removal ambiguity", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone"}, false)
		writeConfig(t, fixture.configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+fixture.endpoint+`}
resources:
  hosted: {include: [tools/skill/gone, tools/collection/gone]}
`)
		before, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err == nil || !strings.Contains(err.Error()+"\n"+output, "--kind") {
			t.Fatalf("collection collision did not request kind disambiguation: %v\n%s", err, output)
		}
		if after, readErr := os.ReadFile(fixture.configPath); readErr != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("ambiguous collection removal changed config: %v", readErr)
		}
	})

	t.Run("scope disambiguates another layer before endpoint removal", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone"}, false)
		userRegistry := filepath.Join(t.TempDir(), "user-registry")
		writeSkill(t, userRegistry, "gone", "User selection", nil)
		userConfigPath, err := config.GlobalConfigPath()
		if err != nil {
			t.Fatal(err)
		}
		writeConfig(t, userConfigPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  local: {directory: `+userRegistry+`, source_harness: factory}
resources:
  local: {include: [skill/gone]}
`)
		userBefore, err := os.ReadFile(userConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("remove", "gone")
		if err == nil || !strings.Contains(err.Error()+"\n"+output, "multiple config layers") {
			t.Fatalf("unscoped removal did not report the other layer: %v\n%s", err, output)
		}
		repoBefore, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}

		output, err = executeCommand("remove", "gone", "--scope", "repo", "--from", fixture.endpoint)
		if err != nil {
			t.Fatalf("remove repo selection by endpoint: %v\n%s", err, output)
		}
		if userAfter, readErr := os.ReadFile(userConfigPath); readErr != nil || !reflect.DeepEqual(userBefore, userAfter) {
			t.Fatalf("repo removal changed user config: %v", readErr)
		}
		if repoAfter, readErr := os.ReadFile(fixture.configPath); readErr != nil ||
			reflect.DeepEqual(repoBefore, repoAfter) || strings.Contains(string(repoAfter), "tools/skill/gone") {
			t.Fatalf("qualified removal did not edit only repo selection: %v\n%s", readErr, repoAfter)
		}
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("endpoint-qualified removal queried the removed selection %d times", got)
		}
	})

	t.Run("remove missing from an existing lock", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"keep"}, false)
		if output, err := executeCommand("sync"); err != nil {
			t.Fatalf("seed lock: %v\n%s", err, output)
		}
		writeRecoverMissingMarketplaceConfig(t, fixture.configPath, fixture.endpoint, []string{"gone", "keep"}, false)
		beforeLock, err := os.ReadFile(fixture.lockPath)
		if err != nil {
			t.Fatal(err)
		}

		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err != nil {
			t.Fatalf("remove: %v\n%s", err, output)
		}
		assertRemovedMarketplaceSelection(t, fixture)
		if after, err := os.ReadFile(fixture.lockPath); err != nil || !reflect.DeepEqual(beforeLock, after) {
			t.Fatalf("remove changed unrelated lock records: %v", err)
		}
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove queried the removed selection %d times", got)
		}

		output, err = executeCommand("sync")
		if err != nil {
			t.Fatalf("sync after remove: %v\n%s", err, output)
		}
		if after, err := os.ReadFile(fixture.lockPath); err != nil || !reflect.DeepEqual(beforeLock, after) {
			t.Fatalf("sync after removal changed the lock: %v", err)
		}
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "keep", "SKILL.md"))
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove or following sync queried the removed selection %d times", got)
		}
	})
	t.Run("remove refuses unsupported Marketplace qualifiers", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, []string{"gone"}, false)
		before, err := os.ReadFile(fixture.configPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			flag, value, code string
		}{
			{flag: "--ref", value: "main", code: "marketplace.remove-unsupported-qualifier"},
			{flag: "--path", value: "resources", code: "marketplace.remove-unsupported-qualifier"},
		} {
			output, err := executeCommand("remove", "gone", "--scope", "repo", "--from", fixture.endpoint, tc.flag, tc.value)
			if err == nil || !strings.Contains(err.Error()+"\n"+output, tc.code) {
				t.Fatalf("qualified Marketplace removal with %s did not refuse: %v\n%s", tc.flag, err, output)
			}
			if after, readErr := os.ReadFile(fixture.configPath); readErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("qualified Marketplace removal changed config: %v", readErr)
			}
		}
	})
	t.Run("Git source sharing the Marketplace URL", func(t *testing.T) {
		backend := newV4MarketplaceServerWithResources(t, "mp", []string{"keep"}, false, nil, nil)
		t.Cleanup(backend.Close)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = strings.TrimPrefix(r.URL.Path, "/registry")
			backend.Config.Handler.ServeHTTP(w, r)
		}))
		t.Cleanup(server.Close)
		endpoint := server.URL + "/registry"
		repo := t.TempDir()
		configPath := filepath.Join(repo, ".agents", "config.yaml")
		writeConfig(t, configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+endpoint+`}
  git: {git: `+endpoint+`, ref: main}
resources:
  hosted: {include: [tools/skill/gone]}
  git: {include: []}
`)
		withWorkingDir(t, repo)
		before, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		output, err := executeCommand("remove", "gone", "--scope", "repo", "--from", endpoint)
		if err == nil || !strings.Contains(err.Error()+"\n"+output, "marketplace.remove-ambiguous-source") {
			t.Fatalf("same-URL Git source did not retain the ambiguity refusal: %v\n%s", err, output)
		}
		if after, readErr := os.ReadFile(configPath); readErr != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("ambiguous Marketplace removal changed config: %v", readErr)
		}
	})
	t.Run("remove missing optional selection with partial lock and available required sibling", func(t *testing.T) {
		fixture := newRecoverMissingMarketplaceFixture(t, http.StatusNotFound, nil, false)
		required := newV4MarketplaceServerWithResources(t, "required", []string{"keep"}, false, nil, nil)
		t.Cleanup(required.Close)
		writeFile(t, fixture.lockPath, "apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces:\n  - url: "+required.URL+"\n    instance_id: required\n")
		writeConfig(t, fixture.configPath, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  hosted: {marketplace: `+fixture.endpoint+`}
  required: {marketplace: `+required.URL+`}
resources:
  hosted: {include: [tools/skill/gone]}
  required: {include: [tools/skill/keep], required: true}
`)
		output, err := executeCommand("remove", "gone", "--scope", "repo")
		if err != nil {
			t.Fatalf("remove optional selection: %v\n%s", err, output)
		}
		marketplaceLockResourceByURL(t, readLockDocument(t, fixture.lockPath), required.URL, "keep")
		assertFileExists(t, filepath.Join(fixture.repoRoot, ".factory", "skills", "keep", "SKILL.md"))
		if got := fixture.goneLookups.Load(); got != 0 {
			t.Fatalf("remove queried the removed selection %d times", got)
		}
	})
}

func newRecoverRecordedMarketplaceFixture(t *testing.T) (recoverMissingMarketplaceFixture, *atomic.Bool, *atomic.Int32) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	backend := newV4MarketplaceServerWithResources(t, "mp", []string{"recorded", "new"}, false, nil, nil)
	t.Cleanup(backend.Close)
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	withdrawn := &atomic.Bool{}
	latestLookups := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/resources/tools/skill/recorded" && withdrawn.Load() {
			latestLookups.Add(1)
			http.NotFound(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeRecoverMissingMarketplaceConfig(t, configPath, server.URL, []string{"recorded"}, false)
	withWorkingDir(t, repo)
	return recoverMissingMarketplaceFixture{
		repoRoot: repo, configPath: configPath,
		lockPath: filepath.Join(repo, ".agents", "lock.yaml"),
		endpoint: server.URL,
	}, withdrawn, latestLookups
}

type recoverMissingMarketplaceFixture struct {
	repoRoot    string
	configPath  string
	lockPath    string
	endpoint    string
	goneLookups *atomic.Int32
}

func newRecoverMissingMarketplaceFixture(t *testing.T, goneStatus int, selected []string, required bool) recoverMissingMarketplaceFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))

	backend := newV4MarketplaceServerWithResources(t, "mp", []string{"keep", "aardvark"}, false, nil, nil)
	t.Cleanup(backend.Close)
	goneLookups := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/resources/tools/skill/gone" {
			goneLookups.Add(1)
			http.Error(w, http.StatusText(goneStatus), goneStatus)
			return
		}
		backend.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	repo := t.TempDir()
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	writeRecoverMissingMarketplaceConfig(t, configPath, server.URL, selected, required)
	withWorkingDir(t, repo)
	return recoverMissingMarketplaceFixture{
		repoRoot:    repo,
		configPath:  configPath,
		lockPath:    filepath.Join(repo, ".agents", "lock.yaml"),
		endpoint:    server.URL,
		goneLookups: goneLookups,
	}
}

func writeRecoverMissingMarketplaceConfig(t *testing.T, configPath, endpoint string, selected []string, required bool) {
	t.Helper()
	requiredField := ""
	if required {
		requiredField = ", required: true"
	}
	resources := make([]string, len(selected))
	for i, name := range selected {
		resources[i] = "tools/skill/" + name
	}
	writeConfig(t, configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  hosted: {marketplace: "+endpoint+"}\nresources:\n  hosted: {include: ["+strings.Join(resources, ", ")+"]"+requiredField+"}\n")
}

func assertRemovedMarketplaceSelection(t *testing.T, fixture recoverMissingMarketplaceFixture) {
	t.Helper()
	config, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "gone") || !strings.Contains(string(config), "keep") {
		t.Fatalf("config after removal =\n%s", config)
	}
}

func assertRemainingMarketplaceLock(t *testing.T, fixture recoverMissingMarketplaceFixture) {
	t.Helper()
	doc := readLockDocument(t, fixture.lockPath)
	if len(doc.Marketplaces) != 1 || len(doc.Marketplaces[0].Resources) != 1 || doc.Marketplaces[0].Resources[0].Name != "keep" {
		t.Fatalf("lock after removal = %#v", doc.Marketplaces)
	}
}
