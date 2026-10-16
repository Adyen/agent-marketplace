package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/localapi"
	"github.com/Adyen/agent-marketplace/internal/session"
)

func guiRemoveSource(t *testing.T, home, wantLocation string) {
	t.Helper()
	t.Setenv("HOME", home)

	api, err := localapi.NewUser(context.Background(), home, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	status, decoded := guiPost(t, server, api.Credential(), "/api/configuration", "")
	if status != http.StatusOK {
		t.Fatalf("GUI metadata status = %d", status)
	}
	var id string
	for _, item := range decoded["userSources"].([]any) {
		view := item.(map[string]any)
		if view["location"] == wantLocation {
			id, _ = view["id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("no GUI source id for location %q in %v", wantLocation, decoded["userSources"])
	}

	status, decoded = guiPost(t, server, api.Credential(), "/api/configuration/actions",
		`{"action":"remove-source","id":"`+id+`"}`)
	if status != http.StatusAccepted {
		t.Fatalf("GUI remove-source status = %d, body = %v", status, decoded)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		status, snapshot := guiPost(t, server, api.Credential(), "/api/snapshot", "")
		if status == http.StatusOK {
			if op, present := snapshot["operation"]; present && op == nil && snapshot["stage"] == "final" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("GUI remove-source operation did not settle")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// spec: gui-configuration/source-edit-matches-an-equivalent-cli-mutation
func TestGUIMarketplaceEndpointRegistrationMatchesCLI(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	shared := t.TempDir()
	keepDir := filepath.Join(shared, "keep-source")
	fragmentDir := filepath.Join(shared, "fragment-source")
	writeFile(t, filepath.Join(keepDir, "solo", "SKILL.md"), "---\nname: solo\ndescription: Solo skill\n---\n# Solo\n")
	writeFile(t, filepath.Join(fragmentDir, "fragment", "SKILL.md"), "---\nname: fragment\ndescription: Fragment skill\n---\n# Fragment\n")

	server := newMarketplaceEndpointServer(t, "marketplace-instance", "", nil)
	defer server.Close()
	endpoint := server.URL + "/services/marketplace"
	userRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\n" +
		"logging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n" +
		"resources:\n  keep: {all: true, harnesses: [devin]}\n"
	fragment := "sources:\n  fragment: {directory: " + fragmentDir + "}\n" +
		"resources:\n  fragment: {all: true, harnesses: [claude]}\n"
	organizationPath := filepath.Join(t.TempDir(), "organization.yaml")
	organization := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\ntelemetry: {level: off}\n"
	writeFile(t, organizationPath, organization)
	config.SetTestOrganizationConfigPath(t, organizationPath)

	repo := t.TempDir()
	repoConfigPath := filepath.Join(repo, ".agents", "config.yaml")
	repoLocalPath := filepath.Join(repo, ".agents", "config.local.yaml")
	repoBefore := []byte("apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	localBefore := []byte("apiVersion: agent-marketplace/v4\nsources:\n  personal: {directory: " + keepDir + "}\n")
	writeFile(t, repoConfigPath, string(repoBefore))
	writeFile(t, repoLocalPath, string(localBefore))
	withWorkingDir(t, repo)

	setHome := func(home string) {
		t.Setenv("HOME", home)
		t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".xdg", "config"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".xdg", "state"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".xdg", "cache"))
	}

	guiHome := t.TempDir()
	setHome(guiHome)
	guiConfigPath := filepath.Join(guiHome, ".agents", "config.yaml")
	guiFragmentPath := filepath.Join(guiHome, ".agents", "fragment.yaml")
	writeFile(t, guiConfigPath, userRoot)
	writeFile(t, guiFragmentPath, fragment)
	api, err := localapi.NewUser(context.Background(), guiHome, session.RuntimeLifecycle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	guiServer := httptest.NewServer(api.Handler())
	defer guiServer.Close()

	deadline := time.Now().Add(30 * time.Second)
	for {
		status, snapshot := guiPost(t, guiServer, api.Credential(), "/api/snapshot", "")
		if status != http.StatusOK {
			t.Fatalf("initial GUI snapshot status = %d", status)
		}
		if snapshot["stage"] == "final" && snapshot["operation"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial GUI snapshot did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}

	requestBody, err := json.Marshal(map[string]any{
		"action": "add-source",
		"source": map[string]string{"kind": "marketplace", "location": endpoint},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, accepted := guiPost(t, guiServer, api.Credential(), "/api/configuration/actions", string(requestBody))
	if status != http.StatusAccepted {
		t.Fatalf("GUI add-source status = %d, body = %v", status, accepted)
	}
	operation, ok := accepted["operation"].(map[string]any)
	if !ok || operation["kind"] != "mutation" {
		t.Fatalf("GUI accepted operation = %v, want mutation", accepted["operation"])
	}
	deadline = time.Now().Add(60 * time.Second)
	for {
		status, snapshot := guiPost(t, guiServer, api.Credential(), "/api/snapshot", "")
		if status != http.StatusOK {
			t.Fatalf("GUI completion snapshot status = %d", status)
		}
		settled, _ := snapshot["settledOperation"].(map[string]any)
		if snapshot["stage"] == "final" && snapshot["operation"] == nil && settled != nil && settled["id"] == operation["id"] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("GUI operation did not settle: %v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}
	guiBytes, err := os.ReadFile(guiConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := strings.Replace(userRoot, "resources:\n", "  marketplace:\n    marketplace: "+endpoint+"\nresources:\n", 1)
	if !bytes.Equal(guiBytes, []byte(wantRoot)) {
		t.Fatalf("GUI user root =\n%s\nwant\n%s", guiBytes, wantRoot)
	}
	if after, _ := os.ReadFile(guiFragmentPath); string(after) != fragment {
		t.Fatal("GUI changed the included fragment")
	}
	assertEndpointLock := func(path string) []byte {
		t.Helper()
		lock := readLockDocument(t, path)
		if lock.Scope != "user" || len(lock.Marketplaces) != 1 ||
			lock.Marketplaces[0].URL != endpoint ||
			lock.Marketplaces[0].InstanceID != "marketplace-instance" ||
			len(lock.Marketplaces[0].Resources) != 0 {
			t.Fatalf("endpoint lock at %s = %#v", path, lock.Marketplaces)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	guiLockPath := filepath.Join(guiHome, ".agents", "lock.yaml")
	guiLockBytes := assertEndpointLock(guiLockPath)
	cliHome := t.TempDir()
	setHome(cliHome)
	cliConfigPath := filepath.Join(cliHome, ".agents", "config.yaml")
	cliFragmentPath := filepath.Join(cliHome, ".agents", "fragment.yaml")
	writeFile(t, cliConfigPath, userRoot)
	writeFile(t, cliFragmentPath, fragment)
	if output, err := executeCommand("install", endpoint, "--scope", "user"); err != nil {
		t.Fatalf("CLI Marketplace registration failed: %v\n%s", err, output)
	}
	cliBytes, err := os.ReadFile(cliConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(guiBytes, cliBytes) {
		t.Fatalf("GUI and CLI user roots differ:\nGUI:\n%s\nCLI:\n%s", guiBytes, cliBytes)
	}
	if after, _ := os.ReadFile(cliFragmentPath); string(after) != fragment {
		t.Fatal("CLI changed the included fragment")
	}
	if after, _ := os.ReadFile(organizationPath); string(after) != organization {
		t.Fatal("registration changed organization configuration")
	}
	if after, _ := os.ReadFile(repoConfigPath); !bytes.Equal(after, repoBefore) {
		t.Fatal("registration changed repository configuration")
	}
	if after, _ := os.ReadFile(repoLocalPath); !bytes.Equal(after, localBefore) {
		t.Fatal("registration changed personal-local configuration")
	}
	cliLockPath := filepath.Join(cliHome, ".agents", "lock.yaml")
	cliLockBytes := assertEndpointLock(cliLockPath)
	if !bytes.Equal(guiLockBytes, cliLockBytes) {
		t.Fatalf("GUI and CLI user locks differ:\nGUI:\n%s\nCLI:\n%s", guiLockBytes, cliLockBytes)
	}
	for _, home := range []string{guiHome, cliHome} {
		for _, path := range []string{
			filepath.Join(home, ".devin"),
			filepath.Join(home, ".claude"),
			filepath.Join(home, ".factory"),
			filepath.Join(home, ".codex"),
			filepath.Join(home, ".agents", "skills"),
		} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("endpoint registration created resource output %s (err=%v)", path, err)
			}
		}
	}
}

// spec: gui-configuration/source-edit-matches-an-equivalent-cli-mutation
func TestGUIRegistryRemovalMatchesCLI(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}

	for _, tc := range []struct {
		name      string
		sourceKey string
	}{
		{name: "directory"},
		{name: "git"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shared := t.TempDir()
			keepDir := filepath.Join(shared, "keep-source")
			fragDir := filepath.Join(shared, "frag-source")
			writeFile(t, filepath.Join(keepDir, "solo", "SKILL.md"), "---\nname: solo\ndescription: Solo skill\n---\n# Solo\n")
			writeFile(t, filepath.Join(fragDir, "frag", "SKILL.md"), "---\nname: frag\ndescription: Frag skill\n---\n# Frag\n")

			var location, sourceYAML string
			var cliArgs []string
			switch tc.name {
			case "directory":
				location = filepath.Join(shared, "gone-dir")
				writeFile(t, filepath.Join(location, "gone", "SKILL.md"), "---\nname: gone\ndescription: Gone skill\n---\n# Gone\n")
				sourceYAML = "directory: " + location
				cliArgs = []string{"remove", "--from", location, "--scope", "user"}
			case "git":
				origin, working := createBareRepoWithWorkingClone(t, "gone.git")
				writeSkill(t, working, "gone", "Gone skill", nil)
				commitAndPushAll(t, working, "seed")
				location = "file://" + filepath.ToSlash(origin)
				sourceYAML = "git: " + location + ", ref: main"
				cliArgs = []string{"remove", "--from", location, "--ref", "main", "--scope", "user"}
			}
			tc.sourceKey = "gone-src"

			root := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\n  " + tc.sourceKey + ": {" + sourceYAML + "}\nresources:\n  keep: {include: [skill/solo], exclude: [skill/retired], harnesses: [devin]}\n  " + tc.sourceKey + ": {all: true}\n"
			fragment := "sources:\n  frag: {directory: " + fragDir + "}\nresources:\n  frag: {all: true, harnesses: [claude]}\n"
			wantRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nlogging: {level: debug}\nsources:\n  keep: {directory: " + keepDir + "}\nresources:\n  keep: {include: [skill/solo], exclude: [skill/retired], harnesses: [devin]}\n"

			repo := t.TempDir()
			writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n")
			repoLocal := filepath.Join(repo, ".agents", "config.local.yaml")
			writeFile(t, repoLocal, "{}\n")
			repoBefore, _ := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml"))
			localBefore, _ := os.ReadFile(repoLocal)

			guiHome := t.TempDir()
			writeFile(t, filepath.Join(guiHome, ".agents", "config.yaml"), root)
			guiFragment := filepath.Join(guiHome, ".agents", "fragment.yaml")
			writeFile(t, guiFragment, fragment)

			guiRemoveSource(t, guiHome, location)
			guiBytes, err := os.ReadFile(filepath.Join(guiHome, ".agents", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			cliHome := t.TempDir()
			writeFile(t, filepath.Join(cliHome, ".agents", "config.yaml"), root)
			cliFragment := filepath.Join(cliHome, ".agents", "fragment.yaml")
			writeFile(t, cliFragment, fragment)
			t.Setenv("HOME", cliHome)
			withWorkingDir(t, repo)

			if output, err := executeCommand(cliArgs...); err != nil {
				t.Fatalf("CLI remove failed: %v\n%s", err, output)
			}
			cliBytes, err := os.ReadFile(filepath.Join(cliHome, ".agents", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}

			if string(guiBytes) != wantRoot {
				t.Fatalf("GUI root =\n%s\nwant\n%s", guiBytes, wantRoot)
			}
			if !bytes.Equal(guiBytes, cliBytes) {
				t.Fatalf("GUI and CLI roots differ:\nGUI:\n%s\nCLI:\n%s", guiBytes, cliBytes)
			}
			if after, _ := os.ReadFile(guiFragment); string(after) != fragment {
				t.Fatal("GUI changed the included fragment")
			}
			if after, _ := os.ReadFile(cliFragment); string(after) != fragment {
				t.Fatal("CLI changed the included fragment")
			}
			if after, _ := os.ReadFile(filepath.Join(repo, ".agents", "config.yaml")); !bytes.Equal(after, repoBefore) {
				t.Fatal("repo-committed config changed")
			}
			if after, _ := os.ReadFile(repoLocal); !bytes.Equal(after, localBefore) {
				t.Fatal("personal-local config changed")
			}
		})
	}
}
