package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareRequestedRefRegistry(t *testing.T, env *Env) string {
	t.Helper()
	regURL := env.CreateBareRepo("requested-ref", map[string]string{".mcp.json": `{"mcpServers":{"main-server":{"command":"echo MAIN"}}}`})
	regPath := strings.TrimPrefix(regURL, "file://")
	work := filepath.Join(filepath.Dir(regPath), "requested-ref-work")
	if result := env.Exec("git", "clone", regURL, work); result.ExitCode != 0 {
		t.Fatalf("clone: %s", result.Combined)
	}
	if err := os.WriteFile(filepath.Join(work, ".mcp.json"), []byte(`{"mcpServers":{"alternate-server":{"command":"echo ALTERNATE"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := env.Exec("git", "-C", work, "checkout", "-b", "alternate"); result.ExitCode != 0 {
		t.Fatalf("checkout: %s", result.Combined)
	}
	if result := env.Exec("git", "-C", work, "add", ".mcp.json"); result.ExitCode != 0 {
		t.Fatalf("git add: %s", result.Combined)
	}
	if result := env.Exec("git", "-C", work, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "alternate"); result.ExitCode != 0 {
		t.Fatalf("commit: %s", result.Combined)
	}
	if result := env.Exec("git", "-C", work, "push", "origin", "alternate"); result.ExitCode != 0 {
		t.Fatalf("push fixture ref: %s", result.Combined)
	}
	return regURL
}

func requestedRefConfig(regURL string) string {
	return "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: " + regURL + "\n    ref: alternate\n  - url: " + regURL + "\n    ref: main\n"
}

func TestInstallRequestedRef(t *testing.T) {
	t.Run("bundle selects requested main ref", func(t *testing.T) {
		env := newEnv(t)
		env.InitGitRepo()
		regURL := prepareRequestedRefRegistry(t, env)
		env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
		env.WriteRepoConfig(requestedRefConfig(regURL))
		result := env.RunAM("install", "--from", regURL, "--ref", "main", "--kind", "mcp", "--scope", "repo")
		if result.ExitCode != 0 {
			t.Fatalf("install exit=%d output=%s", result.ExitCode, result.Combined)
		}
		config := env.ReadFile("/workspace/.agents/config.yaml")
		generated := env.ReadFile("/workspace/.factory/mcp.json")
		if !strings.Contains(generated, "MAIN") || strings.Contains(generated, "ALTERNATE") {
			t.Fatalf("generated MCP = %s", generated)
		}
		if strings.Contains(config, "ref: alternate\n    default: true") {
			t.Fatalf("requested install promoted alternate ref: %s", config)
		}
		if strings.Count(config, "ref: main") != 1 || strings.Count(config, "ref: alternate") != 1 {
			t.Fatalf("config entries = %s", config)
		}
	})

	t.Run("named install rejects resource absent at requested ref", func(t *testing.T) {
		env := newEnv(t)
		env.InitGitRepo()
		regURL := prepareRequestedRefRegistry(t, env)
		env.WriteGlobalConfig("harnesses: [factory]\nregistries: []")
		env.WriteRepoConfig(requestedRefConfig(regURL))
		result := env.RunAM("install", "main-server", "--from", regURL, "--ref", "main", "--kind", "mcp", "--scope", "repo")
		if result.ExitCode != 0 {
			t.Fatalf("main install exit=%d output=%s", result.ExitCode, result.Combined)
		}
		before := env.ReadFile("/workspace/.agents/config.yaml")
		missing := env.RunAM("install", "alternate-server", "--from", regURL, "--ref", "main", "--kind", "mcp", "--scope", "repo")
		if missing.ExitCode == 0 {
			t.Fatal("resource absent at requested ref unexpectedly installed")
		}
		after := env.ReadFile("/workspace/.agents/config.yaml")
		if before != after {
			t.Fatalf("failed install changed config\nbefore=%s\nafter=%s", before, after)
		}
		generated := env.ReadFile("/workspace/.factory/mcp.json")
		if !strings.Contains(generated, "MAIN") || strings.Contains(generated, "ALTERNATE") {
			t.Fatalf("generated MCP = %s", generated)
		}
	})
}
