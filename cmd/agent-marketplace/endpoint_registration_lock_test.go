package main

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// endpointRegistrationFixture is one writable v4 layer that registers a
// Marketplace endpoint without selecting any resources. The served endpoint
// answers only its well-known identity document and counts every request.
type endpointRegistrationFixture struct {
	root       string
	home       string
	repo       string
	scope      lockfile.Scope
	configPath string
	lockPath   string
	url        string
	requests   *atomic.Int32
}

func newEndpointRegistrationFixture(t *testing.T, scope lockfile.Scope) *endpointRegistrationFixture {
	t.Helper()
	root := canonicalTempDir(t)
	f := &endpointRegistrationFixture{
		root:     root,
		home:     filepath.Join(root, "home"),
		repo:     filepath.Join(root, "repo"),
		scope:    scope,
		requests: &atomic.Int32{},
	}
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("endpoint-mp", "https://example.com"))
	}))
	t.Cleanup(server.Close)
	f.url = server.URL

	registration := "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: " + f.url + "}\n"
	userConfig := filepath.Join(f.home, ".agents", "config.yaml")
	repoConfig := filepath.Join(f.repo, ".agents", "config.yaml")
	// Every layer that sync would otherwise seed already exists, so a refusal
	// that writes nothing leaves both trees byte- and mtime-identical.
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	writeFile(t, repoConfig, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	switch scope {
	case lockfile.ScopeUser:
		f.configPath = userConfig
		f.lockPath = filepath.Join(f.home, ".agents", "lock.yaml")
		writeFile(t, f.configPath, registration+"harnesses: [factory]\n")
	case lockfile.ScopeRepo:
		f.configPath = repoConfig
		f.lockPath = filepath.Join(f.repo, ".agents", "lock.yaml")
		writeFile(t, f.configPath, registration+"harnesses: [factory]\n")
	case lockfile.ScopeLocal:
		f.configPath = filepath.Join(f.repo, ".agents", "config.local.yaml")
		f.lockPath = filepath.Join(f.repo, ".agents", "lock.local.yaml")
		writeFile(t, f.configPath, registration)
	default:
		t.Fatalf("unsupported fixture scope %q", scope)
	}
	withWorkingDir(t, f.repo)
	config.ResetRepoRootCache()
	t.Cleanup(config.ResetRepoRootCache)
	return f
}

// recordedEndpoint serves a second Marketplace endpoint, identified as
// endpoint-locked, for tests whose lock already records it. Every request is
// counted so a test can prove a recorded endpoint is never contacted again.
func (f *endpointRegistrationFixture) recordedEndpoint(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown("endpoint-locked", "https://example.com"))
	}))
	t.Cleanup(server.Close)
	return server.URL, requests
}

// healthyLock is the independently authored v2 lock that records the served
// endpoint identity and nothing else.
func (f *endpointRegistrationFixture) healthyLock() string {
	return "apiVersion: agent-marketplace-lock/v2\nscope: " + string(f.scope) + "\nmarketplaces:\n  - url: " + f.url + "\n    instance_id: endpoint-mp\n"
}

func (f *endpointRegistrationFixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	snapshot := dependencyTreeSnapshot(t, f.home)
	maps.Copy(snapshot, dependencyTreeSnapshot(t, f.repo))
	return snapshot
}

func requireUnchangedEndpointRegistrationTree(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	if maps.Equal(before, after) {
		return
	}
	var diff []string
	for path, value := range after {
		if previous, ok := before[path]; !ok {
			diff = append(diff, "created "+path)
		} else if previous != value {
			diff = append(diff, "changed "+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			diff = append(diff, "removed "+path)
		}
	}
	slices.Sort(diff)
	t.Fatalf("%s wrote consumer files:\n%s", label, strings.Join(diff, "\n"))
}

// spec: deployed-acquisition-locks/writing-commands-record-a-registration-identity
func TestEndpointRegistrationLockWritingCommandsCreateIdentityLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "lock"} {
		for _, scope := range []lockfile.Scope{lockfile.ScopeUser, lockfile.ScopeRepo, lockfile.ScopeLocal} {
			t.Run(command+"/"+string(scope), func(t *testing.T) {
				f := newEndpointRegistrationFixture(t, scope)
				authored, err := os.ReadFile(f.configPath)
				if err != nil {
					t.Fatal(err)
				}

				output, err := executeCommand(command)
				if err != nil {
					t.Fatalf("%s: %v\n%s", command, err, output)
				}
				if f.requests.Load() == 0 {
					t.Fatalf("%s created no identity evidence through the endpoint", command)
				}
				raw, err := os.ReadFile(f.lockPath)
				if err != nil {
					t.Fatalf("%s did not create the owning lock: %v\n%s", command, err, output)
				}
				if string(raw) != f.healthyLock() {
					t.Fatalf("%s lock bytes:\n%s\nwant:\n%s", command, raw, f.healthyLock())
				}
				doc := readLockDocument(t, f.lockPath)
				if doc.APIVersion != "agent-marketplace-lock/v2" || doc.Scope != scope || len(doc.Registries) != 0 || len(doc.Marketplaces) != 1 {
					t.Fatalf("%s lock header = %#v", command, doc)
				}
				locked := doc.Marketplaces[0]
				if locked.URL != f.url || locked.InstanceID != "endpoint-mp" || len(locked.Resources) != 0 || len(locked.RegistryBindings) != 0 {
					t.Fatalf("%s endpoint record = %#v", command, locked)
				}
				after, err := os.ReadFile(f.configPath)
				if err != nil || string(after) != string(authored) {
					t.Fatalf("%s changed authored config: %v\nbefore:\n%s\nafter:\n%s", command, err, authored, after)
				}
			})
		}
	}
}

func TestEndpointRegistrationLockLeavesHealthyLockUntouched(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, test := range []struct {
		command string
		// sync's resolver still verifies the locked identity against the
		// endpoint; lock has no resolve phase and must stay offline.
		networkFree bool
	}{
		{command: "sync"},
		{command: "lock", networkFree: true},
	} {
		t.Run(test.command, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			writeFile(t, f.lockPath, f.healthyLock())
			before := f.snapshot(t)

			output, err := executeCommand(test.command)
			if err != nil {
				t.Fatalf("%s: %v\n%s", test.command, err, output)
			}
			if got := f.requests.Load(); test.networkFree && got != 0 {
				t.Fatalf("%s re-acquired a healthy endpoint identity with %d requests", test.command, got)
			}
			raw, err := os.ReadFile(f.lockPath)
			if err != nil || string(raw) != f.healthyLock() {
				t.Fatalf("%s rewrote the healthy lock: %v\n%s", test.command, err, raw)
			}
			requireUnchangedEndpointRegistrationTree(t, test.command, before, f.snapshot(t))
		})
	}
}

// spec: deployed-acquisition-locks/existing-lock-gains-only-the-missing-registration-identity
// spec: deployed-acquisition-locks/read-only-modes-refuse-an-unrecorded-registration
func TestEndpointRegistrationLockAppendsNewRegistrationToExistingLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	// The fixture endpoint is the newly registered one. The repository lock
	// already records a second endpoint, which must never be contacted again.
	f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
	lockedURL, lockedRequests := f.recordedEndpoint(t)
	writeFile(t, f.configPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+f.url+"}\n  locked: {marketplace: "+lockedURL+"}\nharnesses: [factory]\n")
	header := "apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces:\n"
	lockedRecord := "  - url: " + lockedURL + "\n    instance_id: endpoint-locked\n"
	hostedRecord := "  - url: " + f.url + "\n    instance_id: endpoint-mp\n"
	writeFile(t, f.lockPath, header+lockedRecord)
	if err := os.Chmod(f.lockPath, 0o600); err != nil {
		t.Fatal(err)
	}
	// Lock records are ordered by endpoint URL. Both endpoints listen on
	// ephemeral ports, so the order is known only once they are bound.
	want := header + lockedRecord + hostedRecord
	if f.url < lockedURL {
		want = header + hostedRecord + lockedRecord
	}
	before := f.snapshot(t)

	output, err := executeCommand("lock", "--check")
	var lockErr *pipeline.LockViewError
	if !errors.As(err, &lockErr) || lockErr.Code != "lock.change-required" || lockErr.Scope != lockfile.ScopeRepo {
		t.Fatalf("lock --check error = %v, want repo lock.change-required\n%s", err, output)
	}
	if hosted, existing := f.requests.Load(), lockedRequests.Load(); hosted != 0 || existing != 0 {
		t.Fatalf("lock --check made %d new-endpoint and %d locked-endpoint requests", hosted, existing)
	}
	requireUnchangedEndpointRegistrationTree(t, "lock --check", before, f.snapshot(t))

	output, err = executeCommand("lock")
	if err != nil {
		t.Fatalf("lock: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Locks: 1 changed") {
		t.Fatalf("lock output = %q, want one changed lock", output)
	}
	if f.requests.Load() == 0 {
		t.Fatal("lock acquired no identity for the new registration")
	}
	if got := lockedRequests.Load(); got != 0 {
		t.Fatalf("lock re-acquired the already locked endpoint with %d requests", got)
	}
	raw, err := os.ReadFile(f.lockPath)
	if err != nil || string(raw) != want {
		t.Fatalf("lock bytes = %q, %v; want:\n%s", raw, err, want)
	}
	info, err := os.Stat(f.lockPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %v, %v; want 0600 preserved", info, err)
	}
	// Writing commands keep dependency-transaction and exact-cache
	// coordination state below HOME by design; it is neither configuration
	// nor harness output. Everything else must stay byte- and mtime-identical
	// apart from the paths the caller expects to change.
	coordination := []string{filepath.Join(f.home, ".local"), filepath.Join(f.home, ".cache")}
	consumerFiles := func(snapshot map[string]string, changing ...string) map[string]string {
		kept := maps.Clone(snapshot)
		maps.DeleteFunc(kept, func(path, _ string) bool {
			if path == f.home || slices.Contains(changing, path) {
				return true
			}
			for _, root := range coordination {
				if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
					return true
				}
			}
			return false
		})
		return kept
	}
	userConfig := filepath.Join(f.home, ".agents", "config.yaml")
	for _, path := range []string{userConfig, f.configPath, f.lockPath} {
		if _, ok := consumerFiles(before)[path]; !ok {
			t.Fatalf("consumer snapshot does not cover %s", path)
		}
	}
	// Only the lock and the directory its replacement is renamed into change.
	appended := f.snapshot(t)
	lockEntries := []string{f.lockPath, filepath.Dir(f.lockPath)}
	requireUnchangedEndpointRegistrationTree(t, "lock", consumerFiles(before, lockEntries...), consumerFiles(appended, lockEntries...))

	acquired := f.requests.Load()
	if output, err := executeCommand("lock"); err != nil || !strings.Contains(output, "Locks: 0 changed") {
		t.Fatalf("repeated lock: %v\n%s", err, output)
	}
	if hosted, existing := f.requests.Load(), lockedRequests.Load(); hosted != acquired || existing != 0 {
		t.Fatalf("repeated lock contacted complete endpoints: %d new-endpoint, %d locked-endpoint requests", hosted-acquired, existing)
	}
	requireUnchangedEndpointRegistrationTree(t, "repeated lock", consumerFiles(appended), consumerFiles(f.snapshot(t)))
	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("sync after append: %v\n%s", err, output)
	}
	requireUnchangedEndpointRegistrationTree(t, "sync after append", consumerFiles(appended), consumerFiles(f.snapshot(t)))
	if output, err := executeCommand("lock", "--check"); err != nil || !strings.Contains(output, "Locks: 0 changed") {
		t.Fatalf("lock --check after append: %v\n%s", err, output)
	}
	if raw, err := os.ReadFile(f.lockPath); err != nil || string(raw) != want {
		t.Fatalf("final lock bytes = %q, %v; want:\n%s", raw, err, want)
	}
}

func TestEndpointRegistrationLockReadOnlyModesNeverAcquire(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	// Locked modes refuse the absent lock outright. lock --check reports the
	// identity record that lock would write as a required change, still
	// without contacting the endpoint or writing anything.
	for _, test := range []struct {
		args []string
		code string
	}{
		{args: []string{"sync", "--locked"}, code: "lock.missing"},
		{args: []string{"sync", "--offline"}, code: "lock.missing"},
		{args: []string{"sync", "--check"}, code: "lock.missing"},
		{args: []string{"lock", "--check"}, code: "lock.change-required"},
	} {
		label := strings.Join(test.args, " ")
		t.Run(label, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			before := f.snapshot(t)

			output, err := executeCommand(test.args...)
			var lockErr *pipeline.LockViewError
			if !errors.As(err, &lockErr) || lockErr.Code != test.code || lockErr.Scope != lockfile.ScopeRepo {
				t.Fatalf("%s error = %v, want repo %s\n%s", label, err, test.code, output)
			}
			if got := f.requests.Load(); got != 0 {
				t.Fatalf("%s made %d Marketplace requests", label, got)
			}
			if _, err := os.Lstat(f.lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s created a lock: %v", label, err)
			}
			requireUnchangedEndpointRegistrationTree(t, label, before, f.snapshot(t))
		})
	}
}

// spec: deployed-acquisition-locks/read-only-modes-refuse-an-unrecorded-registration
func TestEndpointRegistrationLockReadOnlyModesRefuseUnrecordedRegistration(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	// The repository lock exists and records endpoint A. The configuration
	// also registers the fixture endpoint B without resources, and the lock
	// has no identity record for B. Read-only modes must refuse from config
	// and lock bytes alone: trusting B's identity on first use would bypass
	// the lock they promise to honor.
	for _, test := range []struct {
		args []string
		code string
	}{
		{args: []string{"sync", "--check"}, code: "lock.change-required"},
		{args: []string{"sync", "--locked"}, code: "lock.stale"},
		{args: []string{"sync", "--offline"}, code: "lock.stale"},
	} {
		label := strings.Join(test.args, " ")
		t.Run(label, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			recordedURL, recordedRequests := f.recordedEndpoint(t)
			writeFile(t, f.configPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+f.url+"}\n  locked: {marketplace: "+recordedURL+"}\nharnesses: [factory]\n")
			header := "apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces:\n"
			recordedRecord := "  - url: " + recordedURL + "\n    instance_id: endpoint-locked\n"
			hostedRecord := "  - url: " + f.url + "\n    instance_id: endpoint-mp\n"
			writeFile(t, f.lockPath, header+recordedRecord)
			before := f.snapshot(t)

			output, err := executeCommand(test.args...)
			var lockErr *pipeline.LockViewError
			if !errors.As(err, &lockErr) || lockErr.Code != test.code || lockErr.Scope != lockfile.ScopeRepo || lockErr.Path != f.lockPath {
				t.Fatalf("%s error = %v, want repo %s naming %s\n%s", label, err, test.code, f.lockPath, output)
			}
			if !strings.Contains(err.Error(), f.url) {
				t.Fatalf("%s error does not name the unrecorded registration %s: %v", label, f.url, err)
			}
			if test.code == "lock.stale" && !strings.Contains(err.Error(), "agent-marketplace lock") {
				t.Fatalf("%s error does not point at agent-marketplace lock: %v", label, err)
			}
			if unrecorded, recorded := f.requests.Load(), recordedRequests.Load(); unrecorded != 0 || recorded != 0 {
				t.Fatalf("%s made %d unrecorded-endpoint and %d recorded-endpoint requests", label, unrecorded, recorded)
			}
			requireUnchangedEndpointRegistrationTree(t, label, before, f.snapshot(t))

			// Control: once lock records B's identity, the same read-only
			// mode accepts the lock and leaves config and lock untouched.
			if output, err := executeCommand("lock"); err != nil {
				t.Fatalf("lock: %v\n%s", err, output)
			}
			if f.requests.Load() == 0 {
				t.Fatal("lock acquired no identity for the unrecorded registration")
			}
			want := header + recordedRecord + hostedRecord
			if f.url < recordedURL {
				want = header + hostedRecord + recordedRecord
			}
			if raw, err := os.ReadFile(f.lockPath); err != nil || string(raw) != want {
				t.Fatalf("lock bytes = %q, %v; want:\n%s", raw, err, want)
			}
			authored := map[string]string{}
			for _, path := range []string{f.configPath, f.lockPath} {
				authored[path] = dependencyTreeSnapshot(t, path)[path]
			}
			if output, err := executeCommand(test.args...); err != nil {
				t.Fatalf("%s after lock: %v\n%s", label, err, output)
			}
			for path, state := range authored {
				if got := dependencyTreeSnapshot(t, path)[path]; got != state {
					t.Fatalf("%s after lock changed %s", label, path)
				}
			}
		})
	}
}

func TestEndpointRegistrationLockRefusesMalformedLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "lock"} {
		t.Run(command, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			writeFile(t, f.lockPath, "apiVersion: agent-marketplace-lock/v2\nscope: repo\nmarketplaces: [\n")
			before := f.snapshot(t)

			output, err := executeCommand(command)
			var lockErr *pipeline.LockViewError
			if !errors.As(err, &lockErr) || lockErr.Code != "lock.invalid" || lockErr.Scope != lockfile.ScopeRepo {
				t.Fatalf("%s error = %v, want repo lock.invalid\n%s", command, err, output)
			}
			if got := f.requests.Load(); got != 0 {
				t.Fatalf("%s made %d Marketplace requests over a malformed lock", command, got)
			}
			requireUnchangedEndpointRegistrationTree(t, command, before, f.snapshot(t))
		})
	}
}

func TestEndpointRegistrationLockHonorsAllowlistBeforeNetwork(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "lock"} {
		t.Run(command, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			orgPath := filepath.Join(f.root, "organization", "config.yaml")
			writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nallowed_marketplaces: ['^https://allowed\\.example$']\n")
			config.SetTestOrganizationConfigPath(t, orgPath)
			before := f.snapshot(t)

			output, err := executeCommand(command)
			var blocked *config.MarketplaceURLBlockedError
			if !errors.As(err, &blocked) || len(blocked.BlockedURLs) != 1 || blocked.BlockedURLs[0] != f.url {
				t.Fatalf("%s error = %v, want allowlist denial of %s\n%s", command, err, f.url, output)
			}
			if got := f.requests.Load(); got != 0 {
				t.Fatalf("disallowed Marketplace received %d requests", got)
			}
			if _, err := os.Lstat(f.lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s created a lock for a disallowed endpoint: %v", command, err)
			}
			requireUnchangedEndpointRegistrationTree(t, command, before, f.snapshot(t))
		})
	}
}

func TestEndpointRegistrationLockHonorsLockScopeSelection(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
	before := f.snapshot(t)

	if output, err := executeCommand("lock", "--scope", "user"); err != nil {
		t.Fatalf("lock --scope user: %v\n%s", err, output)
	}
	if got := f.requests.Load(); got != 0 {
		t.Fatalf("unselected repo registration made %d Marketplace requests", got)
	}
	requireUnchangedEndpointRegistrationTree(t, "lock --scope user", before, f.snapshot(t))

	if output, err := executeCommand("lock", "--scope", "repo"); err != nil {
		t.Fatalf("lock --scope repo: %v\n%s", err, output)
	}
	if f.requests.Load() == 0 {
		t.Fatal("selected repo registration acquired no endpoint identity")
	}
	if raw, err := os.ReadFile(f.lockPath); err != nil || string(raw) != f.healthyLock() {
		t.Fatalf("selected repo lock = %q, %v; want:\n%s", raw, err, f.healthyLock())
	}
}

func TestEndpointRegistrationLockNeverWritesOrganizationLock(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, command := range []string{"sync", "lock"} {
		t.Run(command, func(t *testing.T) {
			f := newEndpointRegistrationFixture(t, lockfile.ScopeRepo)
			// Move the registration from the writable repository layer to the
			// externally managed organization layer.
			writeFile(t, f.configPath, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			authority := filepath.Join(f.root, "organization")
			orgPath := filepath.Join(authority, "config.yaml")
			writeFile(t, orgPath, "apiVersion: agent-marketplace/v4\nsources:\n  hosted: {marketplace: "+f.url+"}\n")
			if err := os.Chmod(authority, 0o555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(authority, 0o700) })
			config.SetTestOrganizationConfigPath(t, orgPath)
			before := f.snapshot(t)

			if output, err := executeCommand(command); err != nil {
				t.Fatalf("%s: %v\n%s", command, err, output)
			}
			if f.requests.Load() == 0 {
				t.Fatalf("%s never acquired the organization registration transiently", command)
			}
			if _, err := os.Lstat(filepath.Join(authority, "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s wrote an organization lock: %v", command, err)
			}
			requireUnchangedEndpointRegistrationTree(t, command, before, f.snapshot(t))
		})
	}
}
