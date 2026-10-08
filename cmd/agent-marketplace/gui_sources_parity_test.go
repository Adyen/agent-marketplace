package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

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

			root := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nsources:\n  keep: {directory: " + keepDir + "}\n  " + tc.sourceKey + ": {" + sourceYAML + "}\nresources:\n  keep: {all: true}\n  " + tc.sourceKey + ": {all: true}\n"
			fragment := "sources:\n  frag: {directory: " + fragDir + "}\nresources:\n  frag: {all: true}\n"
			wantRoot := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [devin]\nsources:\n  keep: {directory: " + keepDir + "}\nresources:\n  keep: {all: true}\n"

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
