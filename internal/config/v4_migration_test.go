package config

import (
	"strings"
	"testing"
)

func TestLegacyLoaderRejectsV4InstallationKeysWithMigrationHint(t *testing.T) {
	data := []byte(`apiVersion: agent-marketplace/v2
harnesses: [factory]
sources: {shared: {git: https://example.test/a.git, ref: main}}
resources: {shared: [skill/review]}
`)
	_, errs := loadV3ConfigWithAPIVersion(data, repoLayer, false, true)
	if len(errs) == 0 {
		t.Fatal("expected v2 loader to reject v4 installation keys")
	}
	joined := ""
	for _, err := range errs {
		joined += err.Error() + "\n"
	}
	if !strings.Contains(joined, "v4 installation field") || !strings.Contains(joined, "agent-marketplace/v4") {
		t.Fatalf("missing migration-oriented diagnostic: %s", joined)
	}
}

func TestV4RootRejectsLegacyInstallationKeys(t *testing.T) {
	_, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nskills: {include: [review]}\n"))
	if err == nil || !strings.Contains(err.Error(), "legacy installation key") {
		t.Fatalf("error = %v, want legacy-key migration diagnostic", err)
	}
}

func TestV4VersionBoundaryRejectsExplicitNonV4Include(t *testing.T) {
	_, err := parseV4Document([]byte("apiVersion: agent-marketplace/v3\n"), "fragment.yaml", false)
	if err == nil || !strings.Contains(err.Error(), "agent-marketplace/v4") {
		t.Fatalf("error = %v, want v4 version boundary", err)
	}
}
