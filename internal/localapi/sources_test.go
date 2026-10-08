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
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/session"
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

	bodies := []string{
		`{"action":"add-source"}`,
		`{"action":"add-source","source":null}`,
		`{"action":"add-source","source":{"kind":"marketplace","location":"https://m.example"}}`,
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
