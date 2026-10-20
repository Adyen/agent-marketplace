package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

const versionRefusalGuide = "https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/migrate-configuration.md"

// spec: consumer-config-format/writable-layer-version-refusals-name-the-older-migrator
// spec: consumer-config-format/organization-version-refusals-direct-users-to-their-administrator
// spec: consumer-config-format/other-diagnostics-keep-their-existing-guidance
func TestVersionRefusalCommandRemediation(t *testing.T) {
	if isolateCommandTest(t) {
		return
	}
	for _, tc := range []struct {
		scope, file string
	}{
		{scope: "user", file: "config.yaml"},
		{scope: "repo", file: "config.yaml"},
		{scope: "local", file: "config.local.yaml"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			home := versionRefusalHome(t)
			repo := t.TempDir()
			base := repo
			if tc.scope == "user" {
				base = home
			}
			writeFile(t, filepath.Join(base, ".agents", tc.file), "sources: {}\n")
			withWorkingDir(t, repo)
			assertVersionRefusalHint(t, runVersionRefusal(t, "list"), "agent-marketplace migrate --to v4 --scope "+tc.scope+" --dry-run")
		})
	}
	t.Run("organization", func(t *testing.T) {
		versionRefusalHome(t)
		orgPath := filepath.Join(t.TempDir(), "organization.yaml")
		writeFile(t, orgPath, "sources: {}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		repo := t.TempDir()
		withWorkingDir(t, repo)
		output := runVersionRefusal(t, "list")
		if !strings.Contains(output, "Ask your administrator") || !strings.Contains(output, "convert and deploy") ||
			!strings.Contains(output, versionRefusalGuide) || strings.Contains(output, "agent-marketplace migrate") ||
			strings.Count(output, versionRefusalGuide) != 1 {
			t.Fatalf("organization output has a migrate command or repeated hint:\n%s", output)
		}
	})
	t.Run("multiple layers", func(t *testing.T) {
		home, repo := versionRefusalHome(t), t.TempDir()
		orgPath := filepath.Join(t.TempDir(), "organization.yaml")
		writeFile(t, orgPath, "sources: {}\n")
		config.SetTestOrganizationConfigPath(t, orgPath)
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "sources: {}\n")
		writeFile(t, filepath.Join(repo, ".agents", "config.yaml"), "sources: {}\n")
		withWorkingDir(t, repo)
		output := runVersionRefusal(t, "list")
		if !strings.Contains(output, "Ask your administrator") || !strings.Contains(output, "--scope user --dry-run") ||
			!strings.Contains(output, "--scope repo --dry-run") || strings.Count(output, versionRefusalGuide) != 3 {
			t.Fatalf("list output lacks one guidance hint or repeats one:\n%s", output)
		}
	})
	t.Run("status", func(t *testing.T) {
		home := versionRefusalHome(t)
		repo := t.TempDir()
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "sources: {}\n")
		withWorkingDir(t, repo)
		assertVersionRefusalHint(t, runVersionRefusal(t, "status"), "agent-marketplace migrate --to v4 --scope user --dry-run")
	})
	t.Run("other diagnostic", func(t *testing.T) {
		home := versionRefusalHome(t)
		repo := t.TempDir()
		writeFile(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\nregistries: []\n")
		withWorkingDir(t, repo)

		output, err := executeCommand("list")
		if err == nil || !strings.Contains(output, "v4.legacy-key") ||
			strings.Contains(output, versionRefusalGuide) || strings.Contains(output, "v4.1.4") {
			t.Fatalf("non-version diagnostic gained version guidance:\n%s", output)
		}
	})
}

func runVersionRefusal(t *testing.T, command string) string {
	t.Helper()
	output, err := executeCommand(command)
	if err == nil || !strings.Contains(output, "v4.api-version-required") {
		t.Fatalf("%s refusal = %v\n%s", command, err, output)
	}
	return output
}

func versionRefusalHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	config.SetTestOrganizationConfigPath(t, filepath.Join(t.TempDir(), "missing-org.yaml"))
	return home
}

func assertVersionRefusalHint(t *testing.T, output, command string) {
	t.Helper()
	for _, required := range []string{
		"agent-marketplace v4.1.4",
		command,
		versionRefusalGuide,
		"include trees or unrepresentable input",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("output lacks remediation %q:\n%s", required, output)
		}
	}
	if strings.Count(output, versionRefusalGuide) != 1 {
		t.Fatalf("refusal guidance appeared %d times:\n%s", strings.Count(output, versionRefusalGuide), output)
	}
}
