package e2e_test

import (
	"os"
	"strings"
	"testing"
)

func TestSyncCreatesPrivateAggregates(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteFile("/workspace/registry/.mcp.json", `{"mcpServers":{"private-server":{"command":"echo","args":["initial"]}}}`)
	env.WriteGlobalConfig("harnesses: [devin, codex, gemini, claude]\nregistries: []")
	env.WriteRepoConfig("harnesses: [devin, codex, gemini, claude]\nregistries:\n  - url: ./registry\n    mcp: [private-server]\n")
	env.MustRunAM("sync")

	paths := []string{"/workspace/.mcp.json", "/workspace/.gemini/settings.json", "/workspace/.devin/mcp_config.json", "/workspace/.codex/config.toml"}
	for _, path := range paths {
		info, err := os.Stat(env.mustResolvePath(path))
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}

	claudePath := env.mustResolvePath("/workspace/.mcp.json")
	if err := os.Chmod(claudePath, 0o640); err != nil {
		t.Fatal(err)
	}
	env.WriteFile("/workspace/registry/.mcp.json", `{"mcpServers":{"private-server":{"command":"echo","args":["updated"]}}}`)
	env.MustRunAM("sync")
	info, err := os.Stat(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("updated Claude aggregate mode = %o, want 640", info.Mode().Perm())
	}
	for _, path := range paths {
		if !strings.Contains(env.ReadFile(path), "updated") {
			t.Fatalf("updated content missing from %s", path)
		}
	}
}
