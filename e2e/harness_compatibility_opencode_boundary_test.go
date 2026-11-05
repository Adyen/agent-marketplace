package e2e_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestCompatibilityOpenCodeSkillPayloadIsNotADiagnostic(t *testing.T) {
	e := newEnv(t)
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nprintf '%s\\n' '[{\"name\":\"customize-opencode\",\"description\":\"invalid config\",\"content\":\"authentication required\"}]'\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	probe := compatibilityResourceProbes("opencode", compatibilityFixture{prefix: "amcompat-opencode"})[0]
	command := probe.run(e, binary)
	if err := probe.observe(&command, e, compatibilityFixture{prefix: "amcompat-opencode"}, "repo", nil, true); err != nil {
		t.Fatalf("built-in skill text was treated as a runtime failure: %v", err)
	}
	e.WriteFile(binary, "#!/bin/sh\nprintf '%s\\n' '[]'\nprintf '%s\\n' 'invalid config' >&2\n")
	command = probe.run(e, binary)
	if err := probe.observe(&command, e, compatibilityFixture{prefix: "amcompat-opencode"}, "repo", nil, true); err == nil {
		t.Fatal("real native stderr failure was hidden by valid inventory JSON")
	}
}

func TestCompatibilityOpenCodeSkillObservationRequiresInventoryIdentity(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-opencode"}
	probe := compatibilityResourceProbes("opencode", f)[0]
	for _, output := range []string{
		`[{"name":"unrelated","description":"amcompat-opencode-skill"}]`,
		`{"error":"amcompat-opencode-skill"}`,
	} {
		command := compatibilityCommand{ExitCode: 0, output: output}
		if err := probe.observe(&command, e, f, "repo", nil, false); err == nil {
			t.Fatal("fixture text outside the inventory identity was claimed as discovery")
		}
	}
	command := compatibilityCommand{ExitCode: 0, output: `[{"name":"amcompat-opencode-skill","description":"Compatibility fixture amcompat-opencode-skill"}]`}
	if err := probe.observe(&command, e, f, "repo", nil, false); err != nil {
		t.Fatalf("actual fixture identity was not observed: %v", err)
	}
}

func TestCompatibilityOpenCodeRuntimePreparationIsOfflineAndRepeatable(t *testing.T) {
	e := newEnv(t)
	if err := configureCompatibilityRuntime(e, "opencode"); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{filepath.Join(e.workspace, ".opencode"), filepath.Join(e.home, ".config", "opencode")} {
		if got := e.ReadFile(filepath.Join(root, "package.json")); got != `{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}` {
			t.Fatalf("unreviewed dependency fixture: %q", got)
		}
		if got := e.ReadFile(filepath.Join(root, "package-lock.json")); got != `{"lockfileVersion":3,"packages":{"":{"dependencies":{"@opencode-ai/plugin":"1.18.34"}}}}` {
			t.Fatalf("unreviewed dependency lock: %q", got)
		}
		entries, err := os.ReadDir(filepath.Join(root, "node_modules"))
		if err != nil || len(entries) != 0 {
			t.Fatalf("fixture installed executable dependencies: %v", err)
		}
		before, err := os.Stat(filepath.Join(root, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := configureCompatibilityRuntime(e, "opencode"); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(filepath.Join(root, "package.json"))
		if err != nil || !after.ModTime().Equal(before.ModTime()) {
			t.Fatal("repeated preparation rewrote dependency authority")
		}
	}
	for _, setting := range []string{"OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_EXPERIMENTAL_DISABLE_FILEWATCHER=1"} {
		if !slices.Contains(compatibilityEnv(e), setting) {
			t.Fatalf("offline native environment missing %s", setting)
		}
	}
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nprintf '{}'\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	command := runCompatibilityCommand(e, binary, []string{"debug", "config"}, time.Second)
	if command.Error != "" {
		t.Fatal(command.Error)
	}
}

func TestCompatibilityOpenCodePathsRetainNativeCreatedJSONC(t *testing.T) {
	e := newEnv(t)
	f := compatibilityFixture{prefix: "amcompat-opencode"}
	nativeConfig := filepath.Join(e.home, ".config", "opencode", "opencode.jsonc")
	e.WriteFile(nativeConfig, "{ // native bootstrap\n}\n")
	paths := compatibilityPaths(e, "opencode", "user", f)
	if len(paths["mcp"]) != 1 || paths["mcp"][0] != nativeConfig {
		t.Fatal("native-created JSONC was not the expected machine MCP destination")
	}
	repoPaths := compatibilityPaths(e, "opencode", "repo", f)
	if len(repoPaths["mcp"]) != 1 || repoPaths["mcp"][0] != filepath.Join(e.workspace, "opencode.json") {
		t.Fatal("native machine bootstrap changed the repository destination")
	}
}
