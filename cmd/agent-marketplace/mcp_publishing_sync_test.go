package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: mcp-publishing-metadata-native-fence/publishing-only-changes-do-not-rewrite-healthy-native-settings
func TestSyncMcpPublishingOnlyUpdateKeepsNativeBytes(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))

	origin, working := createBareRepoWithWorkingClone(t, "mcp-publishing.git")
	writeFile(t, filepath.Join(working, "mcp.json"), `{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-mcp"],"_agent-marketplace":{"description":"Docs v1"}}}}`)
	commitAndPushAll(t, working, "seed mcp registry")

	repoRoot := filepath.Join(t.TempDir(), "repo")
	writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), `
apiVersion: agent-marketplace/v2
harnesses:
  - factory
registries:
  - url: `+origin+`
    ref: main
    mcp:
      include:
        - docs
`)
	writeFile(t, filepath.Join(repoRoot, ".factory", "mcp.json"), `{"mcpServers":{"sibling":{"command":"handwritten"}}}`)

	withWorkingDir(t, repoRoot)

	if output, err := executeCommand("sync"); err != nil {
		t.Fatalf("initial sync: %v\n%s", err, output)
	}
	native := filepath.Join(repoRoot, ".factory", "mcp.json")
	initial, err := os.ReadFile(native)
	if err != nil {
		t.Fatalf("read installed native MCP config: %v", err)
	}
	if !bytes.Contains(initial, []byte(`"docs"`)) || !bytes.Contains(initial, []byte(`"sibling"`)) {
		t.Fatalf("initial sync did not install the owned server beside the handwritten sibling: %s", initial)
	}
	if bytes.Contains(bytes.ToLower(initial), []byte("_agent-marketplace")) {
		t.Fatalf("publishing metadata leaked into native MCP settings: %s", initial)
	}
	assertNativeSiblingUnowned := func(phase string, got []byte) {
		t.Helper()
		var doc map[string]any
		if err := json.Unmarshal(got, &doc); err != nil {
			t.Fatalf("%s: native MCP config is not JSON: %v\n%s", phase, err, got)
		}
		servers, _ := doc["mcpServers"].(map[string]any)
		sibling, ok := servers["sibling"].(map[string]any)
		if !ok || sibling["command"] != "handwritten" {
			t.Fatalf("%s: unmanaged sibling missing or rewritten: %s", phase, got)
		}
		if _, owned := sibling["_managed_by"]; owned {
			t.Fatalf("%s: handwritten sibling was claimed: %s", phase, got)
		}
	}
	assertNativeSiblingUnowned("initial sync", initial)
	initialInfo, err := os.Stat(native)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(repoRoot, ".agents", "lock.yaml")
	initialLock := readLockDocument(t, lockPath)
	if len(initialLock.Registries) != 1 || initialLock.Registries[0].Commit.IsZero() {
		t.Fatalf("initial lock missing pinned registry: %+v", initialLock.Registries)
	}

	writeFile(t, filepath.Join(working, "mcp.json"), `{"mcpServers":{"docs":{"command":"npx","args":["-y","docs-mcp"],"_agent-marketplace":{"description":"Docs v2","readme":"README.md","readme_content":"# Publishing docs"}}}}`)
	commitAndPushAll(t, working, "publishing metadata only")

	if output, err := executeCommand("update"); err != nil {
		t.Fatalf("update: %v\n%s", err, output)
	}
	updatedLock := readLockDocument(t, lockPath)
	if len(updatedLock.Registries) != 1 || updatedLock.Registries[0].Commit.String() == initialLock.Registries[0].Commit.String() {
		t.Fatalf("authorized update did not advance the pinned revision: before=%+v after=%+v", initialLock.Registries, updatedLock.Registries)
	}
	updatedLockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	assertNativeUnchanged := func(phase string) {
		t.Helper()
		got, err := os.ReadFile(native)
		if err != nil {
			t.Fatalf("%s: read native MCP config: %v", phase, err)
		}
		if !bytes.Equal(got, initial) {
			t.Fatalf("%s rewrote healthy native bytes:\nbefore: %s\nafter:  %s", phase, initial, got)
		}
		info, err := os.Stat(native)
		if err != nil || !info.ModTime().Equal(initialInfo.ModTime()) {
			t.Fatalf("%s changed native modification time: %v", phase, err)
		}
		assertNativeSiblingUnowned(phase, got)
	}
	assertNativeUnchanged("authorized publishing-only update")

	for _, args := range [][]string{{"sync"}, {"sync", "--locked"}, {"sync", "--check"}} {
		if output, err := executeCommand(args...); err != nil {
			t.Fatalf("%v after publishing-only update: %v\n%s", args, err, output)
		}
		assertNativeUnchanged(strings.Join(args, " "))
		lockAfter, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lockAfter, updatedLockBytes) {
			t.Fatalf("%v rewrote the healthy acquisition lock", args)
		}
	}
}

// spec: mcp-publishing-metadata-native-fence/every-target-consumes-the-reserved-namespace
func TestSyncMcpPublishingOwnershipTargetsMaterialize(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	const docMarker = "MCP-PUBLISHING-OWNERSHIP-DO-NOT-MATERIALIZE"
	registryDoc := `{"mcpServers":{` +
		`"docsstdio":{"command":"echo","args":["control"],"_agent-marketplace":{"description":"Docs stdio","readme_content":"` + docMarker + ` stdio"}},` +
		`"docsremote":{"url":"https://example.test/mcp","_agent-marketplace":{"description":"Docs remote","readme_content":"` + docMarker + ` remote"}}}}`

	for _, tc := range []struct {
		name      string
		harness   string
		userScope bool
		nativeRel string
	}{
		{"devin/repo", "devin", false, filepath.Join(".devin", "mcp_config.json")},
		{"devin/user", "devin", true, filepath.Join(".config", "devin", "mcp_config.json")},
		{"codex/repo", "codex", false, filepath.Join(".codex", "config.toml")},
		{"codex/user", "codex", true, filepath.Join(".codex", "config.toml")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
			t.Setenv("CODEX_HOME", filepath.Join(homeDir, ".codex"))

			origin, working := createBareRepoWithWorkingClone(t, "mcp-publishing-"+strings.ReplaceAll(tc.name, "/", "-")+".git")
			writeFile(t, filepath.Join(working, "mcp.json"), registryDoc)
			commitAndPushAll(t, working, "seed mcp registry")

			config := `
apiVersion: agent-marketplace/v2
harnesses:
  - ` + tc.harness + `
registries:
  - url: ` + origin + `
    ref: main
    mcp:
      include:
        - docsstdio
        - docsremote
`
			repoRoot := filepath.Join(t.TempDir(), "repo")
			native := filepath.Join(repoRoot, tc.nativeRel)
			if tc.userScope {
				writeConfig(t, filepath.Join(homeDir, ".agents", "config.yaml"), config)
				writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), "\napiVersion: agent-marketplace/v2\nregistries: []\n")
				native = filepath.Join(homeDir, tc.nativeRel)
			} else {
				writeConfig(t, filepath.Join(repoRoot, ".agents", "config.yaml"), config)
			}
			withWorkingDir(t, repoRoot)

			if output, err := executeCommand("sync"); err != nil {
				t.Fatalf("sync: %v\n%s", err, output)
			}
			content, err := os.ReadFile(native)
			if err != nil {
				t.Fatalf("native MCP settings missing at %s: %v", native, err)
			}
			if !bytes.Contains(content, []byte("docsstdio")) || !bytes.Contains(content, []byte("docsremote")) {
				t.Fatalf("native MCP settings missing both server identities: %s", content)
			}
			if !bytes.Contains(content, []byte("echo")) || !bytes.Contains(content, []byte("control")) {
				t.Fatalf("native MCP settings missing the stdio command/args: %s", content)
			}
			if !bytes.Contains(content, []byte("https://example.test/mcp")) {
				t.Fatalf("native MCP settings missing the remote URL: %s", content)
			}
			if bytes.Contains(bytes.ToLower(content), []byte("_agent-marketplace")) || bytes.Contains(content, []byte(docMarker)) {
				t.Fatalf("publishing metadata materialized into native MCP settings: %s", content)
			}
			entries, err := os.ReadDir(filepath.Dir(native))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(strings.ToUpper(entry.Name()), "README") {
					t.Fatalf("publishing README materialized at %s", filepath.Join(filepath.Dir(native), entry.Name()))
				}
			}
		})
	}
}
