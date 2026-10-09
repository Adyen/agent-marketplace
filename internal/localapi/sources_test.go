package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func sourceGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func newBareGitSource(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "repo.git")
	sourceGit(t, "", "init", "--initial-branch=main", work)
	writeFile(t, filepath.Join(work, "solo", "SKILL.md"), fixtureSkillBody)
	sourceGit(t, work, "add", ".")
	sourceGit(t, work, "commit", "-m", "seed")
	sourceGit(t, "", "clone", "--bare", work, bare)
	return bare
}

type marketplaceEndpointTestServer struct {
	*httptest.Server
	endpoint     string
	pathPrefix   string
	instanceID   string
	canonicalURL string
	status       int
	requests     atomic.Int32
}

func newMarketplaceEndpointTestServer(t *testing.T, pathPrefix, instanceID, canonicalURL string, status int) *marketplaceEndpointTestServer {
	t.Helper()
	fixture := &marketplaceEndpointTestServer{
		pathPrefix:   "/" + strings.Trim(pathPrefix, "/"),
		instanceID:   instanceID,
		canonicalURL: canonicalURL,
		status:       status,
	}
	fixture.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.requests.Add(1)
		if r.URL.Path != fixture.pathPrefix+"/.well-known/agent-marketplace.json" {
			http.NotFound(w, r)
			return
		}
		if fixture.status != 0 && fixture.status != http.StatusOK {
			http.Error(w, "unavailable", fixture.status)
			return
		}
		canonical := fixture.canonicalURL
		if canonical == "" {
			canonical = fixture.endpoint
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mpapi.DefaultWellKnown(fixture.instanceID, canonical))
	}))
	fixture.endpoint = fixture.URL + fixture.pathPrefix
	t.Cleanup(fixture.Close)
	return fixture
}

type marketplaceSourceFixture struct {
	home, userConfigPath, fragmentPath, organizationPath string
	repoConfigPath, localConfigPath, lockPath, statePath string
	userBefore, fragmentBefore, organizationBefore       []byte
	repoBefore, localBefore, lockBefore                  []byte
	lockExisted, stateExisted                            bool
	stateEntriesBefore                                   []string
	tmpDir                                               string
	api                                                  *API
	events                                               <-chan sessionEvent
}

func marketplaceSourceTestContents(t *testing.T, registeredEndpoint, organizationPolicy string) (userConfig, fragment, organization string) {
	t.Helper()
	keepDir := t.TempDir()
	fragmentDir := t.TempDir()
	writeFile(t, filepath.Join(keepDir, "solo", "SKILL.md"), fixtureSkillBody)
	writeFile(t, filepath.Join(fragmentDir, "fragment", "SKILL.md"), fixtureSkillBody)

	sources := "  keep: {directory: " + keepDir + "}\n"
	if registeredEndpoint != "" {
		sources += "  existing:\n    marketplace: " + registeredEndpoint + "\n"
	}
	userConfig = "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n" +
		"logging: {level: debug}\nsources:\n" + sources +
		"resources:\n  keep: {all: true, harnesses: [devin]}\n"
	fragment = "sources:\n  fragment: {directory: " + fragmentDir + "}\n" +
		"resources:\n  fragment: {all: true, harnesses: [claude]}\n"
	organization = "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n" + organizationPolicy
	return userConfig, fragment, organization
}

func newMarketplaceSourceFixture(t *testing.T, userConfig, fragment, organization string, lockData []byte) *marketplaceSourceFixture {
	t.Helper()
	home, repo := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	fixture := &marketplaceSourceFixture{
		home:             home,
		userConfigPath:   filepath.Join(home, ".agents", "config.yaml"),
		fragmentPath:     filepath.Join(home, ".agents", "fragment.yaml"),
		organizationPath: filepath.Join(t.TempDir(), "organization.yaml"),
		repoConfigPath:   filepath.Join(repo, ".agents", "config.yaml"),
		localConfigPath:  filepath.Join(repo, ".agents", "config.local.yaml"),
		lockPath:         filepath.Join(home, ".agents", "lock.yaml"),
		statePath:        os.Getenv("XDG_STATE_HOME"),
		tmpDir:           os.Getenv("TMPDIR"),
	}
	writeFile(t, fixture.userConfigPath, userConfig)
	writeFile(t, fixture.fragmentPath, fragment)
	writeFile(t, fixture.organizationPath, organization)
	config.SetTestOrganizationConfigPath(t, fixture.organizationPath)
	fixture.repoBefore = []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	fixture.localBefore = []byte("sources:\n  local: {directory: /tmp/personal-local-sentinel}\n")
	writeFile(t, fixture.repoConfigPath, string(fixture.repoBefore))
	writeFile(t, fixture.localConfigPath, string(fixture.localBefore))
	if lockData != nil {
		writeFile(t, fixture.lockPath, string(lockData))
	}
	var err error
	fixture.userBefore, err = os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.fragmentBefore, err = os.ReadFile(fixture.fragmentPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.organizationBefore, err = os.ReadFile(fixture.organizationPath)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.lockBefore, err = os.ReadFile(fixture.lockPath); err == nil {
		fixture.lockExisted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	fixture.api, err = NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fixture.api.Close)
	fixture.events = streamEvents(t, fixture.api)
	for nextEvent(t, fixture.events).snapshotStage() != "final" {
	}
	entries, err := os.ReadDir(fixture.statePath)
	if err == nil {
		fixture.stateExisted = true
		for _, entry := range entries {
			fixture.stateEntriesBefore = append(fixture.stateEntriesBefore, entry.Name())
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return fixture
}

func assertMarketplaceNoWrite(t *testing.T, fixture *marketplaceSourceFixture) {
	t.Helper()
	for _, check := range []struct {
		path   string
		before []byte
	}{
		{path: fixture.userConfigPath, before: fixture.userBefore},
		{path: fixture.fragmentPath, before: fixture.fragmentBefore},
		{path: fixture.organizationPath, before: fixture.organizationBefore},
		{path: fixture.repoConfigPath, before: fixture.repoBefore},
		{path: fixture.localConfigPath, before: fixture.localBefore},
	} {
		after, err := os.ReadFile(check.path)
		if err != nil || !bytes.Equal(after, check.before) {
			t.Fatalf("registration attempt changed %s: %q (err=%v)", check.path, after, err)
		}
	}
	if fixture.lockExisted {
		after, err := os.ReadFile(fixture.lockPath)
		if err != nil || !bytes.Equal(after, fixture.lockBefore) {
			t.Fatalf("registration attempt changed lock bytes: %q (err=%v)", after, err)
		}
	} else if _, err := os.Lstat(fixture.lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registration attempt created lock, stat error = %v", err)
	}
	entries, err := os.ReadDir(fixture.statePath)
	if fixture.stateExisted {
		if err != nil {
			t.Fatalf("read state directory: %v", err)
		}
		var got []string
		for _, entry := range entries {
			got = append(got, entry.Name())
		}
		if !reflect.DeepEqual(got, fixture.stateEntriesBefore) {
			t.Fatalf("registration attempt changed state entries: %v -> %v", fixture.stateEntriesBefore, got)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registration attempt created state directory, read error = %v", err)
	}
	assertNoSourceNativeOutput(t, fixture.home)
	residue, err := filepath.Glob(filepath.Join(fixture.tmpDir, ".agent-marketplace-config-*"))
	if err != nil || len(residue) != 0 {
		t.Fatalf("registration attempt left staging residue %v (err=%v)", residue, err)
	}
}

func assertNoSourceNativeOutput(t *testing.T, home string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(home, ".devin"),
		filepath.Join(home, ".claude"),
		filepath.Join(home, ".factory"),
		filepath.Join(home, ".codex"),
		os.Getenv("CODEX_HOME"),
		filepath.Join(home, ".agents", "skills"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("registration created native output %s (err=%v)", path, err)
		}
	}
}

func sourceAction(t *testing.T, api *API, body string) (int, map[string]any) {
	t.Helper()
	return postRaw(t, api, "/api/configuration/actions", body)
}

func jsonString(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func userSourceIDs(t *testing.T, api *API) map[string]string {
	t.Helper()
	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("metadata status = %d", status)
	}
	raw, ok := decoded["userSources"].([]any)
	if !ok {
		t.Fatalf("userSources missing: %v", decoded)
	}
	ids := map[string]string{}
	for _, item := range raw {
		view, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("user source entry is not an object: %v", item)
		}
		id, _ := view["id"].(string)
		if id == "" {
			t.Fatalf("user source %v carries no id", view)
		}
		key := fmt.Sprintf("%v|%v|%v|%v|%v", view["kind"], view["location"], view["ref"], view["path"], view["sourceHarness"])
		ids[key] = id
	}
	return ids
}

func projectedIDs(t *testing.T, decoded map[string]any, key string) []string {
	t.Helper()
	raw, ok := decoded[key].([]any)
	if !ok {
		t.Fatalf("%s missing: %v", key, decoded)
	}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		view, _ := item.(map[string]any)
		id, _ := view["id"].(string)
		if id == "" {
			t.Fatalf("%s entry carries no id: %v", key, view)
		}
		ids = append(ids, id)
	}
	return ids
}

func readUserSourceDescs(t *testing.T, home string) []config.SourceDescription {
	t.Helper()
	intent := config.ReadIntent(context.Background(), config.LoadEffectiveOptions{
		RepoRoot:       home,
		LocalConfigDir: home,
		GlobalPath:     filepath.Join(home, ".agents", "config.yaml"),
		RepoPath:       config.ProjectConfigPath(home),
		LocalPath:      config.LocalConfigPath(home),
	})
	return intent.Layers[config.UserPersonal].DescribeSources()
}

func descSet(descs []config.SourceDescription) map[string]bool {
	set := map[string]bool{}
	for _, d := range descs {
		set[fmt.Sprintf("%s|%s|%s|%s|%s", d.Kind, d.Location, d.Ref, d.Path, d.SourceHarness)] = true
	}
	return set
}

func mutationDiagnostics(t *testing.T, api *API) []diagnosticDTO {
	t.Helper()
	snapshot := postSnapshot(t, api)
	var out []diagnosticDTO
	for _, d := range snapshot.Diagnostics {
		if d.Category == string(session.DiagnosticMutation) {
			out = append(out, d)
		}
	}
	return out
}

// spec: gui-configuration/adding-a-git-or-directory-source-alone-changes-only-that-entry
func TestUserSourceAddAndRemoveThroughSharedEditor(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	keepDir := t.TempDir()
	fragDir := t.TempDir()
	addParent := t.TempDir()
	addDir := filepath.Join(addParent, "extra-source")
	writeFile(t, filepath.Join(addDir, "added", "SKILL.md"), "---\nname: added\ndescription: Added\n---\n# Added\n")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	fragment := filepath.Join(home, ".agents", "fragment.yaml")
	rootBytes := []byte("apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\nresources:\n  keep: {all: true, harnesses: [devin]}\n")
	fragBytes := []byte("sources:\n  frag: {directory: " + fragDir + "}\nresources:\n  frag: {all: true}\n")
	writeFile(t, userConfig, string(rootBytes))
	writeFile(t, fragment, string(fragBytes))
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	orgBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: " + t.TempDir() + "}\n")
	writeFile(t, orgPath, string(orgBytes))
	config.SetTestOrganizationConfigPath(t, orgPath)

	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("metadata status = %d", status)
	}
	userIDs := projectedIDs(t, decoded, "userSources")
	orgIDs := projectedIDs(t, decoded, "organizationSources")
	status, again := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("repeat metadata status = %d", status)
	}
	for i, id := range userIDs {
		if projectedIDs(t, again, "userSources")[i] != id {
			t.Fatal("user source ids are not stable across metadata reads")
		}
	}
	for _, id := range userIDs {
		for _, other := range orgIDs {
			if id == other {
				t.Fatal("user and organization source ids collide")
			}
		}
	}

	before := descSet(readUserSourceDescs(t, home))

	status, decoded = sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"directory","location":`+jsonString(addDir)+`}}`)
	if status != http.StatusAccepted {
		t.Fatalf("add directory status = %d, body = %v", status, decoded)
	}
	operation, _ := decoded["operation"].(map[string]any)
	if operation["kind"] != "mutation" {
		t.Fatalf("operation kind = %v, want mutation", operation["kind"])
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("directory add severity = %q", got)
	}

	dirName := filepath.Base(addDir)
	current, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	wantDirRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n  " + dirName + ":\n    directory: " + addDir + "\nresources:\n  keep: {all: true, harnesses: [devin]}\n"
	if string(current) != wantDirRoot {
		t.Fatalf("root after directory add =\n%s\nwant\n%s", current, wantDirRoot)
	}

	afterAdd := descSet(readUserSourceDescs(t, home))
	wantAdd := map[string]bool{}
	for k := range before {
		wantAdd[k] = true
	}
	wantAdd["directory|"+addDir+"|||"] = true
	if !reflect.DeepEqual(afterAdd, wantAdd) {
		t.Fatalf("effective sources after add = %v, want exactly %v", afterAdd, wantAdd)
	}

	info1, err := os.Stat(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	status, _ = sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"directory","location":`+jsonString(addDir)+`}}`)
	if status != http.StatusAccepted {
		t.Fatalf("replay add status = %d", status)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("replay add severity = %q", got)
	}
	info2, err := os.Stat(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _ := os.ReadFile(userConfig)
	if !bytes.Equal(replayed, current) || !info1.ModTime().Equal(info2.ModTime()) {
		t.Fatal("replay add rewrote the root")
	}

	bare := newBareGitSource(t)
	gitLocation := "file://" + filepath.ToSlash(bare)
	gitName := strings.TrimSuffix(filepath.Base(bare), ".git")
	status, decoded = sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"git","location":`+jsonString(gitLocation)+`,"ref":"main"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("add git status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("git add severity = %q", got)
	}
	current, err = os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	wantGitRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n  " + dirName + ":\n    directory: " + addDir + "\n  " + gitName + ":\n    git: " + gitLocation + "\n    ref: main\nresources:\n  keep: {all: true, harnesses: [devin]}\n"
	if string(current) != wantGitRoot {
		t.Fatalf("root after git add =\n%s\nwant\n%s", current, wantGitRoot)
	}
	wantWithGit := map[string]bool{}
	for k := range wantAdd {
		wantWithGit[k] = true
	}
	wantWithGit["git|"+gitLocation+"|main||"] = true
	if got := descSet(readUserSourceDescs(t, home)); !reflect.DeepEqual(got, wantWithGit) {
		t.Fatalf("effective sources after git add = %v, want exactly %v", got, wantWithGit)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "lock.yaml")); err != nil {
		t.Fatalf("user lock missing after git source add: %v", err)
	}

	ids := userSourceIDs(t, api)
	dirID := ids["directory|"+addDir+"|||"]
	if dirID == "" {
		t.Fatalf("no id for added directory source; metadata ids = %v", ids)
	}
	status, _ = sourceAction(t, api, `{"action":"remove-source","id":"`+dirID+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("remove directory status = %d", status)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("remove severity = %q", got)
	}
	current, err = os.ReadFile(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	wantRemoved := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin, future-harness]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n  " + gitName + ":\n    git: " + gitLocation + "\n    ref: main\nresources:\n  keep: {all: true, harnesses: [devin]}\n"
	if string(current) != wantRemoved {
		t.Fatalf("root after remove =\n%s\nwant\n%s", current, wantRemoved)
	}
	wantAfterRemove := map[string]bool{}
	for k := range before {
		wantAfterRemove[k] = true
	}
	wantAfterRemove["git|"+gitLocation+"|main||"] = true
	if after := descSet(readUserSourceDescs(t, home)); !reflect.DeepEqual(after, wantAfterRemove) {
		t.Fatalf("effective sources after remove = %v, want exactly %v", after, wantAfterRemove)
	}

	status, decoded = sourceAction(t, api, `{"action":"remove-source","id":"`+dirID+`"}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("stale-id remove status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}

	fragID := ids["directory|"+fragDir+"|||"]
	if fragID == "" {
		t.Fatal("no id for include-owned source")
	}
	status, _ = sourceAction(t, api, `{"action":"remove-source","id":"`+fragID+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("include-owned remove status = %d", status)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "error" {
		t.Fatalf("include-owned remove severity = %q, want error", got)
	}
	diags := mutationDiagnostics(t, api)
	if len(diags) != 1 || diags[0].Severity != "error" ||
		!strings.Contains(diags[0].Message, "config.edit.include-owned") {
		t.Fatalf("include-owned remove diagnostics = %#v, want one typed config.edit.include-owned error", diags)
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, current) {
		t.Fatal("include-owned removal changed the root")
	}
	if after, _ := os.ReadFile(fragment); !bytes.Equal(after, fragBytes) {
		t.Fatal("include-owned removal changed the fragment")
	}

	if after, _ := os.ReadFile(repoConfig); !bytes.Equal(after, repoBefore) {
		t.Fatal("repo-committed config changed")
	}
	if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
		t.Fatal("personal-local config changed")
	}
	if after, _ := os.ReadFile(orgPath); !bytes.Equal(after, orgBytes) {
		t.Fatal("organization config changed")
	}
	for _, dir := range []string{".devin", ".factory", ".claude", ".windsurf"} {
		if _, err := os.Stat(filepath.Join(home, dir)); !os.IsNotExist(err) {
			t.Fatalf("native output %s materialized by a source-only edit", dir)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "devin")); !os.IsNotExist(err) {
		t.Fatal("XDG devin output materialized by a source-only edit")
	}
	if _, err := os.Stat(os.Getenv("CODEX_HOME")); !os.IsNotExist(err) {
		t.Fatal("CODEX_HOME output materialized by a source-only edit")
	}
	residue, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
	if len(residue) != 0 {
		t.Fatalf("staging residue = %v", residue)
	}
}

func TestUserMarketplaceEndpointRegistration(t *testing.T) {
	userConfig, fragment, organization := marketplaceSourceTestContents(t, "", "")
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, nil)
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "marketplace-instance", "", 0)
	body := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`

	status, decoded := sourceAction(t, fixture.api, body)
	if status != http.StatusAccepted {
		t.Fatalf("Marketplace add status = %d, body = %v", status, decoded)
	}
	operation, ok := decoded["operation"].(map[string]any)
	if !ok || operation["kind"] != string(session.OperationMutation) {
		t.Fatalf("accepted operation = %v, want mutation", decoded["operation"])
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("Marketplace add severity = %q, want info", got)
	}
	if _, _, active := fixture.api.gate.Active(); active {
		t.Fatal("Marketplace add left the operation gate held")
	}

	wantRoot := strings.Replace(userConfig, "resources:\n", "  marketplace:\n    marketplace: "+endpoint.endpoint+"\nresources:\n", 1)
	if wantRoot == userConfig {
		t.Fatal("Marketplace fixture has no resource-group insertion point")
	}
	current, err := os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != wantRoot {
		t.Fatalf("user root after registration =\n%s\nwant\n%s", current, wantRoot)
	}
	for _, check := range []struct {
		path string
		want []byte
	}{
		{path: fixture.fragmentPath, want: fixture.fragmentBefore},
		{path: fixture.organizationPath, want: fixture.organizationBefore},
		{path: fixture.repoConfigPath, want: fixture.repoBefore},
		{path: fixture.localConfigPath, want: fixture.localBefore},
	} {
		got, err := os.ReadFile(check.path)
		if err != nil || !bytes.Equal(got, check.want) {
			t.Fatalf("registration changed %s: %q (err=%v)", check.path, got, err)
		}
	}
	if endpoint.requests.Load() == 0 {
		t.Fatal("registration did not fetch the path-prefixed well-known document")
	}
	ids := userSourceIDs(t, fixture.api)
	if ids["marketplace|"+endpoint.endpoint+"|||"] == "" {
		t.Fatalf("registered Marketplace endpoint missing from metadata: %v", ids)
	}
	lockData, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatalf("read user lock: %v", err)
	}
	lock, err := lockfile.Decode(lockData)
	if err != nil {
		t.Fatalf("decode user lock: %v", err)
	}
	if lock.Scope != lockfile.ScopeUser || len(lock.Marketplaces) != 1 ||
		lock.Marketplaces[0].URL != endpoint.endpoint ||
		lock.Marketplaces[0].InstanceID != "marketplace-instance" ||
		len(lock.Marketplaces[0].Resources) != 0 {
		t.Fatalf("endpoint lock = %#v", lock.Marketplaces)
	}
	assertNoSourceNativeOutput(t, fixture.home)

	configBeforeRepeat := append([]byte(nil), current...)
	lockBeforeRepeat := append([]byte(nil), lockData...)
	status, decoded = sourceAction(t, fixture.api, body)
	if status != http.StatusAccepted {
		t.Fatalf("repeat Marketplace add status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("repeat Marketplace add severity = %q, want info", got)
	}
	current, err = os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	lockData, err = os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, configBeforeRepeat) || !bytes.Equal(lockData, lockBeforeRepeat) {
		t.Fatal("repeat registration changed config or lock bytes")
	}
}

// spec: gui-configuration/blocked-endpoint-cannot-be-added
func TestUserMarketplaceEndpointAllowlistRejectedBeforeNetwork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy string
	}{
		{name: "organization pattern", policy: `allowed_marketplaces: ['^https://allowed\.example$']` + "\n"},
		{name: "empty organization allowlist", policy: "allowed_marketplaces: []\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userConfig, fragment, organization := marketplaceSourceTestContents(t, "", "")
			fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization+tc.policy, nil)
			endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "marketplace-instance", "", 0)
			endpoint.requests.Store(0)
			body := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`
			status, decoded := sourceAction(t, fixture.api, body)
			if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
				t.Fatalf("blocked add status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
			}
			if got := endpoint.requests.Load(); got != 0 {
				t.Fatalf("blocked endpoint made %d request(s) before policy rejection", got)
			}
			assertMarketplaceNoWrite(t, fixture)

			unrestrictedOrganization := organization
			writeFile(t, fixture.organizationPath, unrestrictedOrganization)
			status, decoded = sourceAction(t, fixture.api, body)
			if status != http.StatusAccepted {
				t.Fatalf("nil-allowlist control status = %d, body = %v", status, decoded)
			}
			if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
				t.Fatalf("nil-allowlist control severity = %q, want info", got)
			}
			if endpoint.requests.Load() == 0 {
				t.Fatal("nil-allowlist control did not reach the Marketplace server")
			}
			if after, err := os.ReadFile(fixture.organizationPath); err != nil || !bytes.Equal(after, []byte(unrestrictedOrganization)) {
				t.Fatalf("registration changed organization config: %q (err=%v)", after, err)
			}
		})
	}
}

func TestUserSourceAddHonorsComposedV4EmptyRegistryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		organizationRoot     string
		organizationFragment string
		userPolicy           string
		acceptRequest        bool
		includedOnly         bool
	}{
		{
			name:             "root deny-all beats permissive user policy",
			organizationRoot: "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nallowed_registries: []\n",
			userPolicy:       "allowed_registries: ['.*']\n",
		},
		{
			name:                 "included-only deny-all beats permissive user policy",
			organizationRoot:     "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\nharnesses: [claude]\n",
			organizationFragment: "allowed_registries: []\n",
			userPolicy:           "allowed_registries: ['.*']\n",
			includedOnly:         true,
		},
		{
			name:                 "included deny-all beats organization root and user patterns",
			organizationRoot:     "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\nharnesses: [claude]\nallowed_registries: ['.*']\n",
			organizationFragment: "allowed_registries: []\n",
			userPolicy:           "allowed_registries: ['.*']\n",
		},
		{
			name:             "nil policy accepts the request",
			organizationRoot: "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n",
			acceptRequest:    true,
		},
		{
			name:             "matching nonempty user policy accepts the request",
			organizationRoot: "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n",
			userPolicy:       "allowed_registries: ['.*']\n",
			acceptRequest:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, repo := newUserAPIFixture(t, false)
			t.Setenv("TMPDIR", t.TempDir())
			var requests atomic.Int32
			remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.NotFound(w, r)
			}))
			t.Cleanup(remote.Close)
			userConfigPath := filepath.Join(home, ".agents", "config.yaml")
			userBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\n" + tc.userPolicy)
			writeFile(t, userConfigPath, string(userBefore))
			organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
			writeFile(t, organizationPath, tc.organizationRoot)
			var organizationFragmentPath string
			var organizationFragmentBefore []byte
			if tc.organizationFragment != "" {
				organizationFragmentPath = filepath.Join(filepath.Dir(organizationPath), "policy.yaml")
				organizationFragmentBefore = []byte(tc.organizationFragment)
				writeFile(t, organizationFragmentPath, tc.organizationFragment)
			}
			if tc.includedOnly && (strings.Contains(tc.organizationRoot, "allowed_registries:") || strings.Contains(tc.organizationRoot, "allowed_marketplaces:")) {
				t.Fatal("included-only organization policy fixture redundantly declares a root allowlist")
			}
			config.SetTestOrganizationConfigPath(t, organizationPath)
			repoConfigPath := filepath.Join(repo, ".agents", "config.yaml")
			localConfigPath := filepath.Join(repo, ".agents", "config.local.yaml")
			repoBefore, _ := os.ReadFile(repoConfigPath)
			localBefore, _ := os.ReadFile(localConfigPath)
			lockPath := filepath.Join(home, ".agents", "lock.yaml")

			api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			closed := false
			t.Cleanup(func() {
				if !closed {
					api.Close()
				}
			})
			payload, err := json.Marshal(map[string]any{
				"action": "add-source",
				"source": map[string]string{"kind": "git", "location": remote.URL + "/repo.git", "ref": "main"},
			})
			if err != nil {
				t.Fatal(err)
			}
			requests.Store(0)
			status, decoded := postRaw(t, api, "/api/configuration/actions", string(payload))
			if tc.acceptRequest {
				if status != http.StatusAccepted {
					t.Fatalf("allowed Git source request status = %d, body = %v; want 202", status, decoded)
				}
				operation, ok := decoded["operation"].(map[string]any)
				if !ok || operation["kind"] != string(session.OperationMutation) {
					t.Fatalf("accepted Git source operation = %v, want mutation", decoded["operation"])
				}
				api.Close()
				closed = true
				return
			}
			if status == http.StatusAccepted {
				api.Close()
				closed = true
				t.Fatalf("empty organization registry policy accepted Git source request: %v", decoded)
			}
			if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
				t.Fatalf("blocked Git source status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
			}
			if got := requests.Load(); got != 0 {
				t.Fatalf("blocked Git source made %d HTTPS request(s) before policy rejection", got)
			}
			if current, err := os.ReadFile(userConfigPath); err != nil || !bytes.Equal(current, userBefore) {
				t.Fatalf("blocked Git source changed user root: %q (err=%v)", current, err)
			}
			if current, err := os.ReadFile(organizationPath); err != nil || current == nil || string(current) != tc.organizationRoot {
				t.Fatalf("blocked Git source changed organization root: %q (err=%v)", current, err)
			}
			if organizationFragmentPath != "" {
				if current, err := os.ReadFile(organizationFragmentPath); err != nil || !bytes.Equal(current, organizationFragmentBefore) {
					t.Fatalf("blocked Git source changed included policy: %q (err=%v)", current, err)
				}
			}
			if current, err := os.ReadFile(repoConfigPath); err != nil || !bytes.Equal(current, repoBefore) {
				t.Fatalf("blocked Git source changed repository config: %q (err=%v)", current, err)
			}
			if current, err := os.ReadFile(localConfigPath); err != nil || !bytes.Equal(current, localBefore) {
				t.Fatalf("blocked Git source changed local config: %q (err=%v)", current, err)
			}
			if _, err := os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("blocked Git source created a user lock: %v", err)
			}
			if _, _, active := api.gate.Active(); active {
				t.Fatal("blocked Git source left the operation gate held")
			}
			assertNoSourceNativeOutput(t, home)
			residue, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
			if err != nil || len(residue) != 0 {
				t.Fatalf("blocked Git source left staging residue %v (err=%v)", residue, err)
			}
		})
	}
}

// spec: config-layering/marketplace-url-allowlist
func TestUserMarketplaceEndpointMatchingAllowlistAccepted(t *testing.T) {
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "matching-instance", "", 0)
	userConfig, fragment, organization := marketplaceSourceTestContents(t, "", "")
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization+"allowed_marketplaces: ['.*']\n", nil)
	endpoint.requests.Store(0)
	body := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`

	status, decoded := sourceAction(t, fixture.api, body)
	if status != http.StatusAccepted {
		t.Fatalf("matching Marketplace allowlist status = %d, body = %v; want 202", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("matching Marketplace allowlist severity = %q, want info", got)
	}
	if got := endpoint.requests.Load(); got == 0 {
		t.Fatal("matching Marketplace policy control did not reach its local endpoint")
	}
	descriptions := readUserSourceDescs(t, fixture.home)
	var registeredDescription bool
	for _, description := range descriptions {
		if description.Kind == config.V4SourceMarketplace && description.Location == endpoint.endpoint {
			registeredDescription = true
		}
	}
	if !registeredDescription {
		t.Fatalf("matching Marketplace endpoint missing from source projection: %#v", descriptions)
	}
	lockData, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Decode(lockData)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Scope != lockfile.ScopeUser || len(lock.Marketplaces) != 1 {
		t.Fatalf("matching Marketplace lock = %#v, want one user endpoint", lock)
	}
	var registered bool
	for _, marketplace := range lock.Marketplaces {
		if marketplace.URL == endpoint.endpoint && marketplace.InstanceID == "matching-instance" && len(marketplace.Resources) == 0 {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("matching Marketplace endpoint was not registered without Resources: %#v", lock.Marketplaces)
	}
	assertNoSourceNativeOutput(t, fixture.home)
}

func marketplaceEndpointLock(endpoint, instanceID string) []byte {
	return []byte("apiVersion: agent-marketplace-lock/v2\nscope: user\nmarketplaces:\n  - url: " + endpoint + "\n    instance_id: " + instanceID + "\n")
}

func TestUserMarketplaceEndpointRegistrationOperationFailures(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		instanceID                  string
		canonicalURL                string
		status                      int
		differentConfiguredEndpoint bool
		configuredInstanceID        string
	}{
		{name: "well-known failure", instanceID: "marketplace-failure", status: http.StatusInternalServerError},
		{name: "canonical URL mismatch", instanceID: "marketplace-canonical", canonicalURL: "https://canonical.example"},
		{name: "invalid instance", instanceID: " "},
		{name: "same URL different instance", instanceID: "marketplace-new", configuredInstanceID: "marketplace-old"},
		{name: "same instance different URL", instanceID: "marketplace-shared", differentConfiguredEndpoint: true, configuredInstanceID: "marketplace-shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", tc.instanceID, tc.canonicalURL, tc.status)
			var configuredEndpoint string
			var lockData []byte
			if tc.differentConfiguredEndpoint {
				previous := newMarketplaceEndpointTestServer(t, "/previous", tc.configuredInstanceID, "", 0)
				configuredEndpoint = previous.endpoint
				lockData = marketplaceEndpointLock(configuredEndpoint, tc.configuredInstanceID)
			} else if tc.configuredInstanceID != "" {
				configuredEndpoint = endpoint.endpoint
				lockData = marketplaceEndpointLock(configuredEndpoint, tc.configuredInstanceID)
			}
			userConfig, fragment, organization := marketplaceSourceTestContents(t, configuredEndpoint, "")
			fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, lockData)
			endpoint.requests.Store(0)
			body := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`

			status, decoded := sourceAction(t, fixture.api, body)
			if status != http.StatusAccepted {
				t.Fatalf("failed Marketplace registration status = %d, body = %v", status, decoded)
			}
			if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "error" {
				t.Fatalf("failed registration notification severity = %q, want error", got)
			}
			diagnostics := mutationDiagnostics(t, fixture.api)
			if len(diagnostics) != 1 || diagnostics[0].Severity != "error" || diagnostics[0].Code == "" {
				t.Fatalf("failed registration diagnostics = %#v, want one coded mutation error", diagnostics)
			}
			if endpoint.requests.Load() == 0 {
				t.Fatal("valid registration failure did not reach the Marketplace server")
			}
			assertMarketplaceNoWrite(t, fixture)
		})
	}
}

func TestUserSourceActionsRejectInvalidRequests(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	dupDir := t.TempDir()
	fragDir := t.TempDir()
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	fragment := filepath.Join(home, ".agents", "fragment.yaml")
	writeFile(t, userConfig,
		"apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nallowed_registries: ['^file:///']\nsources:\n  dup1: {directory: "+dupDir+"}\n  dup2: {directory: "+dupDir+"}\n")
	writeFile(t, fragment, "sources:\n  frag: {directory: "+fragDir+"}\n")
	before, _ := os.ReadFile(userConfig)
	fragBefore, _ := os.ReadFile(fragment)
	orgPath := filepath.Join(t.TempDir(), "org.yaml")
	orgDir := t.TempDir()
	orgBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  orgdir: {directory: " + orgDir + "}\n")
	writeFile(t, orgPath, string(orgBytes))
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}
	statePath := os.Getenv("XDG_STATE_HOME")
	stateEntries, stateErr := os.ReadDir(statePath)
	stateExisted := stateErr == nil
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		t.Fatal(stateErr)
	}
	var stateBefore []string
	for _, entry := range stateEntries {
		stateBefore = append(stateBefore, entry.Name())
	}

	status, decoded := postRaw(t, api, "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("metadata status = %d", status)
	}
	var orgID, dupID string
	for _, item := range decoded["organizationSources"].([]any) {
		orgID, _ = item.(map[string]any)["id"].(string)
	}
	for _, item := range decoded["userSources"].([]any) {
		view := item.(map[string]any)
		if view["location"] == dupDir {
			dupID, _ = view["id"].(string)
		}
	}
	if orgID == "" || dupID == "" {
		t.Fatalf("missing projected ids: org=%q dup=%q", orgID, dupID)
	}

	marketplace := newMarketplaceEndpointTestServer(t, "/invalid", "marketplace-instance", "", 0)
	marketplaceCredential := strings.Replace(marketplace.endpoint, "://", "://test-user:test-password@", 1)
	bodies := []string{
		`{"action":"add-source"}`,
		`{"action":"add-source","source":null}`,
		`{"action":"add-source","source":{"kind":"marketplace"}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":""}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":null}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplaceCredential) + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint+"?token=fixture") + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint+"#fixture") + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"ref":""}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"ref":null}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"path":""}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"path":null}}`,
		`{"action":"add-source","source":{"kind":"marketplace","kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","Kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"Location":` + jsonString(marketplace.endpoint) + `}}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(marketplace.endpoint) + `,"unknown":true}}`,
		`{"action":"add-source","source":{"kind":"Git","location":"/tmp/x"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":"relative/dir"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":"./rel"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":""}}`,
		`{"action":"add-source","source":{"kind":"directory","location":null}}`,
		`{"action":"add-source","source":{"kind":"directory","location":42}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"ref":""}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"ref":null}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"ref":42}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"path":"sub"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"path":""}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"path":null}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `,"path":["x"]}}`,
		`{"action":"add-source","source":{"kind":"directory"}}`,
		`{"action":"add-source","source":{"kind":null,"location":"` + dupDir + `"}}`,
		`{"action":"add-source","source":{"kind":42,"location":"` + dupDir + `"}}`,
		`{"action":"add-source","source":{"kind":"git","location":null,"ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":""}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":null}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":42}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"bad ref!"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main","path":"../escape"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main","path":"/abs"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"https://user:pw@example.com/r.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"https://user@example.com/r.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"ssh://git:pw@example.com/r.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"user:pw@example.com:r.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"https://example.com/r.git?token=x","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"https://example.com/r.git#frag","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main","sourceHarness":"claude"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main","kind":"git"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main"},"scope":"user"}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main"},"targets":["devin"]}`,
		`{"action":"add-source","source":{"kind":"git","location":"https://blocked.example/repo.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main"},"source":{"kind":"git","location":"file:///tmp/r.git","ref":"main"}}`,
		`{"action":"add-source","source":{"kind":"directory","location":` + jsonString(dupDir) + `},"consent":true}`,
		`{"action":"remove-source"}`,
		`{"action":"remove-source","id":""}`,
		`{"action":"remove-source","id":null}`,
		`{"action":"remove-source","id":42}`,
		`{"action":"remove-source","id":"` + orgID + `"}`,
		`{"action":"remove-source","id":"` + dupID + `"}`,
		`{"action":"remove-source","id":"` + strings.Repeat("0", 64) + `"}`,
		`{"action":"remove-source","id":"` + dupID + `","scope":"organization"}`,
		`{"action":"remove-source","id":"` + dupID + `","id":"` + dupID + `"}`,
		`{"action":"save-targets","targets":["devin"],"id":"` + dupID + `"}`,
		`{"action":"remove-source","id":"` + dupID + `","targets":["devin"]}`,
	}
	for _, body := range bodies {
		status, decoded := sourceAction(t, api, body)
		if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
			t.Fatalf("body %q status/code = %d/%v, want 400/%s", body, status, decoded["code"], codeInvalidRequest)
		}
	}
	if got := marketplace.requests.Load(); got != 0 {
		t.Fatalf("invalid Marketplace requests reached the server %d time(s)", got)
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
		t.Fatalf("rejected bodies changed the root:\n%s", after)
	}
	if after, _ := os.ReadFile(fragment); !bytes.Equal(after, fragBefore) {
		t.Fatal("rejected bodies changed the fragment")
	}
	if after, _ := os.ReadFile(orgPath); !bytes.Equal(after, orgBytes) {
		t.Fatal("rejected bodies changed the organization config")
	}
	if after, _ := os.ReadFile(repoConfig); !bytes.Equal(after, repoBefore) {
		t.Fatal("rejected bodies changed the repo config")
	}
	if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
		t.Fatal("rejected bodies changed the local config")
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected bodies created a lock file: %v", err)
	}
	assertNoSourceNativeOutput(t, home)
	stateAfter, stateErr := os.ReadDir(statePath)
	if stateExisted {
		if stateErr != nil {
			t.Fatalf("read state after rejected bodies: %v", stateErr)
		}
		var stateAfterNames []string
		for _, entry := range stateAfter {
			stateAfterNames = append(stateAfterNames, entry.Name())
		}
		if !reflect.DeepEqual(stateBefore, stateAfterNames) {
			t.Fatalf("rejected bodies changed state entries: %v -> %v", stateBefore, stateAfterNames)
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		t.Fatalf("rejected bodies created state directory, read error = %v", stateErr)
	}
	residue, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
	if len(residue) != 0 {
		t.Fatalf("staging residue = %v", residue)
	}

	held, ok := api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold the gate")
	}
	status, decoded = sourceAction(t, api, `{"action":"add-source","source":{"kind":"directory","location":`+jsonString(dupDir)+`}}`)
	api.gate.Finish(held)
	if status != http.StatusConflict || decoded["code"] != codeBusy {
		t.Fatalf("busy add status/code = %d/%v, want 409/%s", status, decoded["code"], codeBusy)
	}

	writeFile(t, userConfig, "apiVersion: [broken\n")
	status, decoded = sourceAction(t, api, `{"action":"add-source","source":{"kind":"directory","location":`+jsonString(dupDir)+`}}`)
	if status != http.StatusBadRequest || decoded["code"] != codeNotReady {
		t.Fatalf("invalid root add status/code = %d/%v, want 400/%s", status, decoded["code"], codeNotReady)
	}
	if data, _ := os.ReadFile(userConfig); string(data) != "apiVersion: [broken\n" {
		t.Fatal("invalid root was overwritten")
	}

	writeFile(t, userConfig, string(before))
	decoy := t.TempDir()
	writeFile(t, filepath.Join(decoy, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	t.Setenv("HOME", decoy)
	status, decoded = sourceAction(t, api, `{"action":"add-source","source":{"kind":"directory","location":`+jsonString(dupDir)+`}}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("relocated-home add status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
		t.Fatal("relocated-home request changed the captured root")
	}
	if data, _ := os.ReadFile(filepath.Join(decoy, ".agents", "config.yaml")); bytes.Contains(data, []byte("sources")) {
		t.Fatalf("relocated-home request wrote the decoy root: %s", data)
	}
	t.Setenv("HOME", home)

	freshDir := t.TempDir()
	status, decoded = sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"directory","location":`+jsonString(freshDir)+`}}`)
	if status != http.StatusAccepted {
		t.Fatalf("positive add status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("positive add severity = %q", got)
	}
	freshID := userSourceIDs(t, api)["directory|"+freshDir+"|||"]
	if freshID == "" {
		t.Fatal("added source missing from projected ids")
	}
	status, _ = sourceAction(t, api, `{"action":"remove-source","id":"`+freshID+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("positive remove status = %d", status)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "info" {
		t.Fatalf("positive remove severity = %q", got)
	}
	status, decoded = sourceAction(t, api, `{"action":"remove-source","id":"`+freshID+`"}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("stale-id status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}
}

func TestUserSourceMutationRevalidatesStagedDescriptor(t *testing.T) {
	base := func(gitLocation string) string {
		return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {git: " + gitLocation + ", ref: main, source_harness: claude}\n"
	}
	for _, tc := range []struct {
		name  string
		actor func(gitLocation string) string
	}{
		{name: "unchanged-descriptor-removes", actor: base},
		{name: "harness", actor: func(loc string) string {
			return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {git: " + loc + ", ref: main, source_harness: windsurf}\n"
		}},
		{name: "location", actor: func(string) string {
			return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {git: file:///tmp/other/repo.git, ref: main, source_harness: claude}\n"
		}},
		{name: "ref", actor: func(loc string) string {
			return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {git: " + loc + ", ref: dev, source_harness: claude}\n"
		}},
		{name: "path", actor: func(loc string) string {
			return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {git: " + loc + ", ref: main, path: sub, source_harness: claude}\n"
		}},
		{name: "kind", actor: func(string) string {
			return "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  keep: {directory: /tmp/dir-source, source_harness: claude}\n"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := newUserAPIFixture(t, false)
			t.Setenv("TMPDIR", t.TempDir())
			gitLocation := "file:///tmp/seed/repo.git"
			userConfig := filepath.Join(home, ".agents", "config.yaml")
			writeFile(t, userConfig, base(gitLocation))
			actorBytes := []byte(tc.actor(gitLocation))

			runtime := session.RuntimeLifecycle()
			lifecycle := session.DependencyLifecycle{
				Mutate: func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
					writeFile(t, userConfig, string(actorBytes))
					return runtime.Mutate(ctx, ws, mutation)
				},
			}
			api, err := NewUser(context.Background(), home, lifecycle, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(api.Close)

			events := streamEvents(t, api)
			for nextEvent(t, events).snapshotStage() != "final" {
			}

			id := userSourceIDs(t, api)["git|"+gitLocation+"|main||claude"]
			if id == "" {
				t.Fatalf("no id for seeded source: %v", userSourceIDs(t, api))
			}
			status, decoded := sourceAction(t, api, `{"action":"remove-source","id":"`+id+`"}`)
			if status != http.StatusAccepted {
				t.Fatalf("remove status = %d, body = %v", status, decoded)
			}
			got := notificationSeverity(t, awaitNotification(t, events))

			after, err := os.ReadFile(userConfig)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "unchanged-descriptor-removes" {
				if got != "info" {
					t.Fatalf("unchanged remove severity = %q, want info", got)
				}
				want := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {}\n"
				if string(after) != want {
					t.Fatalf("unchanged root after remove =\n%s\nwant\n%s", after, want)
				}
				return
			}
			if got != "error" {
				t.Fatalf("raced remove severity = %q, want error", got)
			}
			diags := mutationDiagnostics(t, api)
			if len(diags) != 1 || diags[0].Severity != "error" ||
				!strings.Contains(diags[0].Message, "generation conflict") {
				t.Fatalf("raced remove diagnostics = %#v, want one generation-conflict error", diags)
			}
			if !bytes.Equal(after, actorBytes) {
				t.Fatalf("concurrent root was mutated:\n%s\nwant actor bytes:\n%s", after, actorBytes)
			}
		})
	}
}

func TestUserSourceAddRevalidatesStagedIntent(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
	actorBytes := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nallowed_registries: ['^https://only.example/']\n")

	runtime := session.RuntimeLifecycle()
	lifecycle := session.DependencyLifecycle{
		Mutate: func(ctx context.Context, ws pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
			writeFile(t, userConfig, string(actorBytes))
			return runtime.Mutate(ctx, ws, mutation)
		},
	}
	api, err := NewUser(context.Background(), home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	status, decoded := sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"git","location":"file:///tmp/r.git","ref":"main"}}`)
	if status != http.StatusAccepted {
		t.Fatalf("add status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, events)); got != "error" {
		t.Fatalf("raced add severity = %q, want error", got)
	}
	diags := mutationDiagnostics(t, api)
	if len(diags) != 1 || diags[0].Severity != "error" ||
		!strings.Contains(diags[0].Message, "generation conflict") {
		t.Fatalf("raced add diagnostics = %#v, want one generation-conflict error", diags)
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, actorBytes) {
		t.Fatalf("concurrent root was mutated:\n%s", after)
	}
}

func TestValidateAddSourceOriginForms(t *testing.T) {
	source := func(kind, location, ref string) *configurationActionSource {
		s := &configurationActionSource{Kind: kind, Location: location}
		if ref != "" {
			s.Ref = ref
			s.refSeen = true
		}
		return s
	}
	for _, location := range []string{
		"https://example.com/repo.git",
		"file:///srv/repo.git",
		"example.com:org/repo.git",
		"git@example.com:org/repo.git",
		"ssh://git@example.com/repo.git",
		"ssh://example.com/repo.git",
	} {
		entry, ok := validateAddSource(source("git", location, "main"), nil)
		if !ok {
			t.Fatalf("origin %q rejected", location)
		}
		if entry.V4Kind != config.V4SourceGit || entry.URL != location || entry.Ref != "main" {
			t.Fatalf("origin %q entry = %#v", location, entry)
		}
	}
	for _, location := range []string{
		"https://user:pw@example.com/repo.git",
		"https://user@example.com/repo.git",
		"ssh://git:pw@example.com/repo.git",
		"user:pw@example.com:org/repo.git",
		"https://example.com/repo.git?token=x",
		"https://example.com/repo.git#frag",
		"git://example.com/repo.git",
		"ext::sh -c x",
		"-oProxyCommand=x",
	} {
		if _, ok := validateAddSource(source("git", location, "main"), nil); ok {
			t.Fatalf("origin %q accepted", location)
		}
	}
	entry, ok := validateAddSource(source("directory", "  /srv/dir  ", ""), nil)
	if !ok || entry.URL != "/srv/dir" {
		t.Fatalf("padded directory location entry = %#v, ok=%v", entry, ok)
	}
}

func TestUserSourceAddAcceptsSSHAndSCPOrigins(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, _ := os.ReadFile(userConfig)
	noWrite := errors.New("no-write mutate")
	lifecycle := session.DependencyLifecycle{
		Mutate: func(context.Context, pipeline.Workspace, func(pipeline.ConfigPathMapper) error) error {
			return noWrite
		},
	}
	api, err := NewUser(context.Background(), home, lifecycle, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	events := streamEvents(t, api)
	for nextEvent(t, events).snapshotStage() != "final" {
	}

	for _, location := range []string{
		"git@example.com:org/repo.git",
		"example.com:org/repo.git",
		"ssh://git@example.com/repo.git",
		"https://example.com/repo.git",
		"file:///srv/repo.git",
	} {
		status, decoded := sourceAction(t, api,
			`{"action":"add-source","source":{"kind":"git","location":`+jsonString(location)+`,"ref":"main"}}`)
		if status != http.StatusAccepted {
			t.Fatalf("origin %q status = %d, body = %v", location, status, decoded)
		}
		if got := notificationSeverity(t, awaitNotification(t, events)); got != "error" {
			t.Fatalf("origin %q severity = %q, want error from the no-write stub", location, got)
		}
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
		t.Fatal("no-write origin proof changed the root")
	}
}

func TestUserSourceSettledOperationReplaysToLateSubscriber(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	t.Setenv("TMPDIR", t.TempDir())
	addDir := t.TempDir()
	writeFile(t, filepath.Join(addDir, "added", "SKILL.md"), fixtureSkillBody)

	api, err := NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)

	status, decoded := sourceAction(t, api,
		`{"action":"add-source","source":{"kind":"directory","location":`+jsonString(addDir)+`}}`)
	if status != http.StatusAccepted {
		t.Fatalf("add status = %d, body = %v", status, decoded)
	}
	operation, ok := decoded["operation"].(map[string]any)
	if !ok {
		t.Fatalf("accepted body carries no operation: %v", decoded)
	}
	acceptedID := uint64(operation["id"].(float64))
	if operation["kind"] != "mutation" {
		t.Fatalf("operation kind = %v, want mutation", operation["kind"])
	}

	settledRef := func(payload map[string]any) (map[string]any, bool) {
		marker, _ := payload["settledOperation"].(map[string]any)
		id, _ := marker["id"].(float64)
		if marker == nil || uint64(id) != acceptedID {
			return nil, false
		}
		return marker, true
	}
	rawSnapshot := func() map[string]any {
		status, decoded := postRaw(t, api, "/api/snapshot", "")
		if status != http.StatusOK {
			t.Fatalf("snapshot status = %d", status)
		}
		return decoded
	}

	deadline := time.Now().Add(30 * time.Second)
	var settled map[string]any
	for {
		settled = rawSnapshot()
		if settled["operation"] == nil {
			if _, ok := settledRef(settled); ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation never settled; last snapshot operation=%v settledOperation=%v", settled["operation"], settled["settledOperation"])
		}
		time.Sleep(10 * time.Millisecond)
	}
	if marker, _ := settledRef(settled); marker["kind"] != "mutation" {
		t.Fatalf("settled kind = %v, want mutation", marker["kind"])
	}

	events := streamEvents(t, api)
	first := nextEvent(t, events)
	if first.Type != "snapshot" {
		t.Fatalf("first replayed event type = %q, want snapshot", first.Type)
	}
	data, err := json.Marshal(first.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var replayed map[string]any
	if err := json.Unmarshal(data, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed["operation"] != nil {
		t.Fatalf("replayed final still carries active operation %v", replayed["operation"])
	}
	if replayed["stage"] != "final" {
		t.Fatalf("replayed stage = %v, want final", replayed["stage"])
	}
	marker, ok := settledRef(replayed)
	if !ok {
		t.Fatalf("replayed final settledOperation = %v, want id %d", replayed["settledOperation"], acceptedID)
	}
	if marker["kind"] != "mutation" {
		t.Fatalf("replayed settled kind = %v, want mutation", marker["kind"])
	}

	if id := userSourceIDs(t, api)["directory|"+addDir+"|||"]; id == "" {
		t.Fatalf("added source missing from metadata after settle; ids = %v", userSourceIDs(t, api))
	}
}

func TestUserSourceActionRequestControls(t *testing.T) {
	home, _ := newUserAPIFixture(t, true)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	before, _ := os.ReadFile(userConfig)

	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)

	send := func(method, path, body, token, origin string) int {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("X-Agent-Marketplace-Token", token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	token := api.Credential()
	if got := send(http.MethodGet, "/api/configuration/actions", "", token, ""); got != http.StatusMethodNotAllowed {
		t.Fatalf("GET actions status = %d, want 405", got)
	}
	if got := send(http.MethodPost, "/api/configuration/actions", `{"action":"remove-source","id":"x"}`, "", ""); got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", got)
	}
	if got := send(http.MethodPost, "/api/configuration/actions", `{"action":"remove-source","id":"x"}`, "wrong", ""); got != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d, want 401", got)
	}
	if got := send(http.MethodPost, "/api/configuration/actions", `{"action":"remove-source","id":"x"}`, token, "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("foreign-origin status = %d, want 403", got)
	}
	if got := send(http.MethodPost, "/api/configuration/actions?scope=user", `{"action":"remove-source","id":"x"}`, token, ""); got != http.StatusBadRequest {
		t.Fatalf("query actions status = %d, want 400", got)
	}
	if after, _ := os.ReadFile(userConfig); !bytes.Equal(after, before) {
		t.Fatal("control rejections changed the root")
	}
}

func TestMarketplaceSourceIDsIncludeLegacyInstanceID(t *testing.T) {
	home, _ := newUserAPIFixture(t, false)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	endpoint := "http://127.0.0.1:12345/services/catalog"
	writeFile(t, userConfig, "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nmarketplaces:\n  - url: "+endpoint+"\n    instance_id: instance-one\n  - url: "+endpoint+"\n    instance_id: instance-two\n")

	api := &API{homeDir: home, token: "work21-literal-key"}
	metadata := api.configurationMetadata(context.Background())
	if len(metadata.UserSources) != 2 {
		t.Fatalf("projected user Marketplace sources = %#v, want two", metadata.UserSources)
	}
	if metadata.UserSources[0].ID != "5b1eb108603c7c694934729dd117c9581908ce2e91674df345fc2ae66ae70fe1" {
		t.Fatalf("first Marketplace source ID = %q, want independent literal HMAC", metadata.UserSources[0].ID)
	}
	if metadata.UserSources[1].ID != "ea1e03f818c553d87f796cfd98f68d363cdccbcb448f5033620177ba975f86fc" {
		t.Fatalf("second Marketplace source ID = %q, want independent literal HMAC", metadata.UserSources[1].ID)
	}
	if metadata.UserSources[0].ID == metadata.UserSources[1].ID {
		t.Fatal("same URL with different legacy instance IDs projected the same opaque ID")
	}
	body, err := json.Marshal(metadata.UserSources)
	if err != nil {
		t.Fatal(err)
	}
	for _, instanceID := range []string{"instance-one", "instance-two"} {
		if bytes.Contains(body, []byte(instanceID)) {
			t.Fatalf("Marketplace DTO leaked instance ID %q: %s", instanceID, body)
		}
	}
	otherLaunch := (&API{homeDir: home, token: "another-work21-launch-key"}).configurationMetadata(context.Background())
	if otherLaunch.UserSources[0].ID == metadata.UserSources[0].ID {
		t.Fatal("Marketplace source ID was not bound to the launch token")
	}
}

func TestUserMarketplaceEndpointRegistrationRemovalRoundTrip(t *testing.T) {
	userConfig, fragment, organization := marketplaceSourceTestContents(t, "", "")
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, nil)
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "marketplace-instance", "", 0)
	beforeSources := descSet(readUserSourceDescs(t, fixture.home))
	addBody := `{"action":"add-source","source":{"kind":"marketplace","location":` + jsonString(endpoint.endpoint) + `}}`

	status, decoded := sourceAction(t, fixture.api, addBody)
	if status != http.StatusAccepted {
		t.Fatalf("Marketplace add status = %d, body = %v", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("Marketplace add severity = %q, want info", got)
	}
	registeredRoot := strings.Replace(userConfig, "resources:\n", "  marketplace:\n    marketplace: "+endpoint.endpoint+"\nresources:\n", 1)
	if registeredRoot == userConfig {
		t.Fatal("Marketplace fixture has no resource-group insertion point")
	}
	if current, err := os.ReadFile(fixture.userConfigPath); err != nil || string(current) != registeredRoot {
		t.Fatalf("user root after registration = %q, err=%v, want %q", current, err, registeredRoot)
	}
	lockData, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatalf("read endpoint-only user lock: %v", err)
	}
	lock, err := lockfile.Decode(lockData)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Scope != lockfile.ScopeUser || len(lock.Marketplaces) != 1 ||
		lock.Marketplaces[0].URL != endpoint.endpoint ||
		lock.Marketplaces[0].InstanceID != "marketplace-instance" ||
		len(lock.Marketplaces[0].Resources) != 0 {
		t.Fatalf("registered endpoint lock = %#v", lock.Marketplaces)
	}
	assertNoSourceNativeOutput(t, fixture.home)
	id := userSourceIDs(t, fixture.api)["marketplace|"+endpoint.endpoint+"|||"]
	if id == "" {
		t.Fatal("registered Marketplace endpoint has no projected ID")
	}
	endpoint.requests.Store(0)

	status, decoded = sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("Marketplace removal status = %d, body = %v; want 202", status, decoded)
	}
	operation, ok := decoded["operation"].(map[string]any)
	if !ok || operation["kind"] != string(session.OperationMutation) {
		t.Fatalf("Marketplace removal operation = %v, want mutation", decoded["operation"])
	}
	operationID := uint64(operation["id"].(float64))
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("Marketplace removal severity = %q, want info", got)
	}
	current, err := os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != userConfig {
		t.Fatalf("user root after Marketplace removal =\n%s\nwant\n%s", current, userConfig)
	}
	if after := descSet(readUserSourceDescs(t, fixture.home)); !reflect.DeepEqual(after, beforeSources) {
		t.Fatalf("effective sources after removal = %v, want exactly %v", after, beforeSources)
	}
	for _, check := range []struct {
		path string
		want []byte
	}{
		{path: fixture.fragmentPath, want: fixture.fragmentBefore},
		{path: fixture.organizationPath, want: fixture.organizationBefore},
		{path: fixture.repoConfigPath, want: fixture.repoBefore},
		{path: fixture.localConfigPath, want: fixture.localBefore},
	} {
		if got, err := os.ReadFile(check.path); err != nil || !bytes.Equal(got, check.want) {
			t.Fatalf("Marketplace removal changed %s: %q (err=%v)", check.path, got, err)
		}
	}
	assertMarketplaceEndpointOnlyLock(t, fixture.lockPath, endpoint.endpoint, "marketplace-instance", lockData)
	if got := endpoint.requests.Load(); got != 0 {
		t.Fatalf("Marketplace removal made %d network request(s), want no identity lookup", got)
	}
	assertNoSourceNativeOutput(t, fixture.home)
	residue, err := filepath.Glob(filepath.Join(fixture.tmpDir, ".agent-marketplace-config-*"))
	if err != nil || len(residue) != 0 {
		t.Fatalf("Marketplace removal left staging residue %v (err=%v)", residue, err)
	}
	if _, _, active := fixture.api.gate.Active(); active {
		t.Fatal("Marketplace removal left the operation gate held")
	}
	settled := postSnapshot(t, fixture.api)
	if settled.Operation != nil || settled.SettledOperation == nil ||
		settled.SettledOperation.ID != operationID || settled.SettledOperation.Kind != string(session.OperationMutation) {
		t.Fatalf("settled Marketplace removal snapshot = %#v", settled)
	}
	status, decoded = sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("repeated stale Marketplace removal status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}
	settledAgain := postSnapshot(t, fixture.api)
	if settledAgain.Operation != nil || settledAgain.SettledOperation == nil ||
		settledAgain.SettledOperation.ID != operationID || settledAgain.SettledOperation.Kind != string(session.OperationMutation) {
		t.Fatalf("stale Marketplace refusal changed the settled marker: %#v", settledAgain)
	}
}

func TestUserMarketplaceLegacyEndpointOnlyRemoval(t *testing.T) {
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "legacy-instance", "", 0)
	userConfig := "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nharnesses: [devin]\nlogging: {level: debug}\nmarketplaces:\n  - url: " + endpoint.endpoint + "\n    instance_id: legacy-instance\n    alias: legacy\n"
	fragment := "registries: []\n"
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n"
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, marketplaceEndpointLock(endpoint.endpoint, "legacy-instance"))
	id := userSourceIDs(t, fixture.api)["marketplace|"+endpoint.endpoint+"|||"]
	if id == "" {
		t.Fatal("legacy Marketplace endpoint has no projected ID")
	}
	endpoint.requests.Store(0)
	status, decoded := sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("legacy Marketplace removal status = %d, body = %v; want 202", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "info" {
		t.Fatalf("legacy Marketplace removal severity = %q, want info", got)
	}
	wantRoot := "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nharnesses: [devin]\nlogging: {level: debug}\n"
	if current, err := os.ReadFile(fixture.userConfigPath); err != nil || string(current) != wantRoot {
		t.Fatalf("legacy user root after removal =\n%s\nerr=%v\nwant\n%s", current, err, wantRoot)
	}
	if after, err := os.ReadFile(fixture.fragmentPath); err != nil || !bytes.Equal(after, []byte(fragment)) {
		t.Fatalf("legacy removal changed fragment: %q (err=%v)", after, err)
	}
	assertMarketplaceEndpointOnlyLock(t, fixture.lockPath, endpoint.endpoint, "legacy-instance", fixture.lockBefore)
	if got := endpoint.requests.Load(); got != 0 {
		t.Fatalf("legacy endpoint-only removal made %d network request(s), want no fetch", got)
	}
	status, decoded = sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("repeated stale legacy Marketplace removal status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}
	if current, err := os.ReadFile(fixture.userConfigPath); err != nil || string(current) != wantRoot {
		t.Fatalf("stale legacy removal changed root: %q (err=%v)", current, err)
	}
	assertMarketplaceEndpointOnlyLock(t, fixture.lockPath, endpoint.endpoint, "legacy-instance", fixture.lockBefore)
	if got := endpoint.requests.Load(); got != 0 {
		t.Fatalf("stale legacy removal made %d network request(s), want no fetch", got)
	}
	assertNoSourceNativeOutput(t, fixture.home)
	residue, err := filepath.Glob(filepath.Join(fixture.tmpDir, ".agent-marketplace-config-*"))
	if err != nil || len(residue) != 0 {
		t.Fatalf("legacy Marketplace removal left staging residue %v (err=%v)", residue, err)
	}
}

func assertMarketplaceEndpointOnlyLock(t *testing.T, path, endpoint, instanceID string, wantBytes []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, wantBytes) {
		t.Fatalf("endpoint-only user lock changed: before=%q after=%q", wantBytes, data)
	}
	lock, err := lockfile.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Scope != lockfile.ScopeUser || len(lock.Marketplaces) != 1 ||
		lock.Marketplaces[0].URL != endpoint || lock.Marketplaces[0].InstanceID != instanceID ||
		len(lock.Marketplaces[0].Resources) != 0 {
		t.Fatalf("reconciled endpoint-only user lock = %#v", lock)
	}
}

func assertMarketplaceRemovalFailure(t *testing.T, fixture *marketplaceSourceFixture, code string) {
	t.Helper()
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "error" {
		t.Fatalf("Marketplace removal severity = %q, want error", got)
	}
	diagnostics := mutationDiagnostics(t, fixture.api)
	if len(diagnostics) != 1 || diagnostics[0].Severity != "error" ||
		!strings.Contains(diagnostics[0].Message, code) {
		t.Fatalf("Marketplace removal diagnostics = %#v, want error containing %s", diagnostics, code)
	}
	assertMarketplaceNoWrite(t, fixture)
}

func marketplaceMutationRequestCounter(api *API, endpoint *marketplaceEndpointTestServer) *atomic.Int32 {
	runtime := session.RuntimeLifecycle()
	var requests atomic.Int32
	api.lifecycle = session.DependencyLifecycle{
		Sync:   runtime.Sync,
		Update: runtime.Update,
		Mutate: func(ctx context.Context, workspace pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
			err := runtime.Mutate(ctx, workspace, mutation)
			requests.Store(endpoint.requests.Load())
			return err
		},
	}
	return &requests
}

func TestUserMarketplaceEndpointRemovalRefusesIncludedOnlySource(t *testing.T) {
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "included-instance", "", 0)
	userConfig := "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nharnesses: [devin]\nlogging: {level: debug}\n"
	fragment := "marketplaces:\n  - url: " + endpoint.endpoint + "\n    instance_id: included-instance\n    alias: included\n"
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n"
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, marketplaceEndpointLock(endpoint.endpoint, "included-instance"))
	if bytes.Contains(fixture.userBefore, []byte("marketplaces:")) {
		t.Fatal("included-only fixture redundantly declares a root Marketplace")
	}
	id := userSourceIDs(t, fixture.api)["marketplace|"+endpoint.endpoint+"|||"]
	if id == "" {
		t.Fatal("included-only Marketplace has no projected user ID")
	}
	requestsAtMutationReturn := marketplaceMutationRequestCounter(fixture.api, endpoint)
	endpoint.requests.Store(0)

	status, decoded := sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("included-only Marketplace removal status = %d, body = %v; want 202", status, decoded)
	}
	assertMarketplaceRemovalFailure(t, fixture, "config.edit.include-owned")
	rootAfter, err := os.ReadFile(fixture.userConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rootAfter, []byte("marketplaces:")) {
		t.Fatal("included-only removal added a redundant root Marketplace")
	}
	if got := requestsAtMutationReturn.Load(); got != 0 {
		t.Fatalf("included-only removal mutation made %d network request(s), want no identity lookup", got)
	}
}

func TestUserMarketplaceEndpointRemovalRefusesLegacyPins(t *testing.T) {
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "pinned-instance", "", 0)
	digest := "sha256:" + strings.Repeat("a", 64)
	archiveDigest := "sha256:" + strings.Repeat("b", 64)
	userConfig := "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nmarketplaces:\n  - url: " + endpoint.endpoint + "\n    instance_id: pinned-instance\n    alias: protected\n    resources:\n      - registry_id: tools-id\n        registry_slug: tools\n        kind: skill\n        name: protected\n        revision: rev-one\n        digest: " + digest + "\n        acquisition: mirror\n        direct: true\n"
	fragment := "registries: []\n"
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n"
	lockData, err := lockfile.Encode(lockfile.Document{
		APIVersion: lockfile.APIVersionV2,
		Scope:      lockfile.ScopeUser,
		Marketplaces: []lockfile.MarketplaceLock{{
			URL:        endpoint.endpoint,
			InstanceID: "pinned-instance",
			RegistryBindings: []lockfile.MarketplaceRegistryBinding{
				{Name: "tools", RegistryID: "tools-id"},
			},
			Resources: []lockfile.MarketplaceResourceLock{{
				RegistryName:  "tools",
				RegistryID:    "tools-id",
				Kind:          "skill",
				Name:          "protected",
				Revision:      "rev-one",
				ContentDigest: lockfile.Digest(digest),
				ArchiveDigest: lockfile.Digest(archiveDigest),
				Acquisition:   lockfile.MarketplaceAcquisitionMirror,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, lockData)
	id := userSourceIDs(t, fixture.api)["marketplace|"+endpoint.endpoint+"|||"]
	if id == "" {
		t.Fatal("pin-bearing Marketplace has no projected user ID")
	}
	requestsAtMutationReturn := marketplaceMutationRequestCounter(fixture.api, endpoint)
	endpoint.requests.Store(0)

	status, decoded := sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("pin-bearing Marketplace removal status = %d, body = %v; want 202", status, decoded)
	}
	assertMarketplaceRemovalFailure(t, fixture, "config.edit.source-in-use")
	if got := requestsAtMutationReturn.Load(); got != 0 {
		t.Fatalf("pin-bearing removal mutation made %d network request(s), want no identity lookup", got)
	}
}

func TestUserMarketplaceRemovalRejectsAmbiguousDescriptors(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "duplicate-instance", "", 0)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  first: {marketplace: " + endpoint.endpoint + "}\n  second: {marketplace: " + endpoint.endpoint + "}\n")
	writeFile(t, userConfig, string(userBefore))
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)
	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	metadataStatus, metadata := postRaw(t, api, "/api/configuration", "")
	if metadataStatus != http.StatusOK {
		t.Fatalf("duplicate Marketplace metadata status = %d", metadataStatus)
	}
	ids := projectedIDs(t, metadata, "userSources")
	if len(ids) != 2 || ids[0] != ids[1] {
		t.Fatalf("duplicate Marketplace descriptors = %v, want the same ambiguous identity", ids)
	}

	status, decoded := sourceAction(t, api, `{"action":"remove-source","id":"`+ids[0]+`"}`)
	if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
		t.Fatalf("ambiguous Marketplace removal status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
	}
	if current, err := os.ReadFile(userConfig); err != nil || !bytes.Equal(current, userBefore) {
		t.Fatalf("ambiguous removal changed root: %q (err=%v)", current, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agents", "lock.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ambiguous removal created a lock: %v", err)
	}
	if current, err := os.ReadFile(repoConfig); err != nil || !bytes.Equal(current, repoBefore) {
		t.Fatalf("ambiguous removal changed repository config: %q (err=%v)", current, err)
	}
	if current, err := os.ReadFile(repoLocal); err != nil || !bytes.Equal(current, localBefore) {
		t.Fatalf("ambiguous removal changed local config: %q (err=%v)", current, err)
	}
	if got := endpoint.requests.Load(); got != 0 {
		t.Fatalf("ambiguous removal made %d network request(s), want no identity lookup", got)
	}
	assertNoSourceNativeOutput(t, home)
}

func TestUserMarketplaceLegacyRemovalRevalidatesInstanceIdentity(t *testing.T) {
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "before-instance", "", 0)
	userConfig := "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nmarketplaces:\n  - url: " + endpoint.endpoint + "\n    instance_id: before-instance\n"
	fragment := "registries: []\n"
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n"
	fixture := newMarketplaceSourceFixture(t, userConfig, fragment, organization, marketplaceEndpointLock(endpoint.endpoint, "before-instance"))
	actorBytes := []byte(strings.Replace(userConfig, "instance_id: before-instance", "instance_id: after-instance", 1))
	if bytes.Equal(actorBytes, []byte(userConfig)) {
		t.Fatal("staged identity actor fixture did not change the instance ID")
	}
	id := userSourceIDs(t, fixture.api)["marketplace|"+endpoint.endpoint+"|||"]
	if id == "" {
		t.Fatal("legacy Marketplace endpoint has no projected ID")
	}
	runtime := session.RuntimeLifecycle()
	var requestsAtMutationReturn atomic.Int32
	fixture.api.lifecycle = session.DependencyLifecycle{
		Sync:   runtime.Sync,
		Update: runtime.Update,
		Mutate: func(ctx context.Context, workspace pipeline.Workspace, mutation func(pipeline.ConfigPathMapper) error) error {
			writeFile(t, fixture.userConfigPath, string(actorBytes))
			err := runtime.Mutate(ctx, workspace, mutation)
			requestsAtMutationReturn.Store(endpoint.requests.Load())
			return err
		},
	}
	endpoint.requests.Store(0)

	status, decoded := sourceAction(t, fixture.api, `{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("legacy Marketplace removal status = %d, body = %v; want 202", status, decoded)
	}
	if got := notificationSeverity(t, awaitNotification(t, fixture.events)); got != "error" {
		t.Fatalf("stale legacy Marketplace removal severity = %q, want error", got)
	}
	diagnostics := mutationDiagnostics(t, fixture.api)
	if len(diagnostics) != 1 || diagnostics[0].Severity != "error" ||
		!strings.Contains(diagnostics[0].Message, "generation conflict") {
		t.Fatalf("stale legacy removal diagnostics = %#v, want one generation-conflict error", diagnostics)
	}
	if current, err := os.ReadFile(fixture.userConfigPath); err != nil || !bytes.Equal(current, actorBytes) {
		t.Fatalf("stale removal changed actor root: %q (err=%v), want %q", current, err, actorBytes)
	}
	if current, err := os.ReadFile(fixture.lockPath); err != nil || !bytes.Equal(current, fixture.lockBefore) {
		t.Fatalf("stale removal changed lock: %q (err=%v)", current, err)
	}
	for _, check := range []struct {
		path string
		want []byte
	}{
		{path: fixture.fragmentPath, want: fixture.fragmentBefore},
		{path: fixture.organizationPath, want: fixture.organizationBefore},
		{path: fixture.repoConfigPath, want: fixture.repoBefore},
		{path: fixture.localConfigPath, want: fixture.localBefore},
	} {
		if current, err := os.ReadFile(check.path); err != nil || !bytes.Equal(current, check.want) {
			t.Fatalf("stale removal changed %s: %q (err=%v)", check.path, current, err)
		}
	}
	if got := requestsAtMutationReturn.Load(); got != 0 {
		t.Fatalf("stale removal mutation made %d network request(s), want no identity lookup", got)
	}
	residue, err := filepath.Glob(filepath.Join(fixture.tmpDir, ".agent-marketplace-config-*"))
	if err != nil || len(residue) != 0 {
		t.Fatalf("stale removal left staging residue %v (err=%v)", residue, err)
	}
}

func TestUserMarketplaceRemovalPreflightControlsNoWrite(t *testing.T) {
	home, repo := newUserAPIFixture(t, false)
	t.Setenv("TMPDIR", t.TempDir())
	endpoint := newMarketplaceEndpointTestServer(t, "/services/catalog", "user-instance", "", 0)
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources:\n  marketplace: {marketplace: " + endpoint.endpoint + "}\n")
	writeFile(t, userConfig, string(userBefore))
	lockPath := filepath.Join(home, ".agents", "lock.yaml")
	lockBefore := marketplaceEndpointLock(endpoint.endpoint, "user-instance")
	writeFile(t, lockPath, string(lockBefore))
	orgDir := t.TempDir()
	orgPath := filepath.Join(t.TempDir(), "organization.yaml")
	orgBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources:\n  organization: {directory: " + orgDir + "}\n")
	writeFile(t, orgPath, string(orgBefore))
	config.SetTestOrganizationConfigPath(t, orgPath)
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore, _ := os.ReadFile(repoConfig)
	localBefore, _ := os.ReadFile(repoLocal)

	api, err := NewUser(context.Background(), home, session.DependencyLifecycle{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	metadataStatus, metadata := postRaw(t, api, "/api/configuration", "")
	if metadataStatus != http.StatusOK {
		t.Fatalf("metadata status = %d", metadataStatus)
	}
	userID := userSourceIDs(t, api)["marketplace|"+endpoint.endpoint+"|||"]
	var organizationID string
	for _, raw := range metadata["organizationSources"].([]any) {
		view := raw.(map[string]any)
		if view["location"] == orgDir {
			organizationID, _ = view["id"].(string)
		}
	}
	if userID == "" || organizationID == "" {
		t.Fatalf("missing IDs: user=%q organization=%q", userID, organizationID)
	}
	endpoint.requests.Store(0)

	for _, id := range []string{strings.Repeat("0", 64), organizationID} {
		status, decoded := sourceAction(t, api, `{"action":"remove-source","id":"`+id+`"}`)
		if status != http.StatusBadRequest || decoded["code"] != codeInvalidRequest {
			t.Fatalf("unowned Marketplace removal status/code = %d/%v, want 400/%s", status, decoded["code"], codeInvalidRequest)
		}
	}
	held, ok := api.gate.Begin(session.OperationSync)
	if !ok {
		t.Fatal("could not hold operation gate")
	}
	status, decoded := sourceAction(t, api, `{"action":"remove-source","id":"`+userID+`"}`)
	api.gate.Finish(held)
	if status != http.StatusConflict || decoded["code"] != codeBusy {
		t.Fatalf("busy Marketplace removal status/code = %d/%v, want 409/%s", status, decoded["code"], codeBusy)
	}

	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	send := func(token, origin string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+"/api/configuration/actions", strings.NewReader(`{"action":"remove-source","id":"`+userID+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("X-Agent-Marketplace-Token", token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := send("", ""); got != http.StatusUnauthorized {
		t.Fatalf("missing-token Marketplace removal status = %d, want 401", got)
	}
	if got := send("wrong", ""); got != http.StatusUnauthorized {
		t.Fatalf("wrong-token Marketplace removal status = %d, want 401", got)
	}
	if got := send(api.Credential(), "https://evil.example"); got != http.StatusForbidden {
		t.Fatalf("foreign-origin Marketplace removal status = %d, want 403", got)
	}

	for _, check := range []struct {
		path string
		want []byte
	}{
		{path: userConfig, want: userBefore},
		{path: lockPath, want: lockBefore},
		{path: orgPath, want: orgBefore},
		{path: repoConfig, want: repoBefore},
		{path: repoLocal, want: localBefore},
	} {
		if current, err := os.ReadFile(check.path); err != nil || !bytes.Equal(current, check.want) {
			t.Fatalf("refused Marketplace removal changed %s: %q (err=%v)", check.path, current, err)
		}
	}
	if got := endpoint.requests.Load(); got != 0 {
		t.Fatalf("refused Marketplace removal made %d network request(s), want none", got)
	}
	assertNoSourceNativeOutput(t, home)
	residue, err := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), ".agent-marketplace-config-*"))
	if err != nil || len(residue) != 0 {
		t.Fatalf("refused Marketplace removal left staging residue %v (err=%v)", residue, err)
	}
	if _, _, active := api.gate.Active(); active {
		t.Fatal("refused Marketplace removals left the operation gate held")
	}
	if _, err := os.Stat(os.Getenv("XDG_STATE_HOME")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused Marketplace removal created state output: %v", err)
	}
}
