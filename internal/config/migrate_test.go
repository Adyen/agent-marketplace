package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeV2Config(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte(content))
}

func readConfigString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config %q: %v", path, err)
	}
	return string(data)
}

// spec: consumer-config-format/versionless-roots-are-refused-in-every-consumer-layer
func TestRootCannotInheritV4FromFragment(t *testing.T) {
	for _, source := range []Source{Organization, UserPersonal, RepoCommitted, RepoPersonal} {
		t.Run(source.String(), func(t *testing.T) {
			fixture := newV4OnlyFixture(t)
			path := fixture.paths[source]
			before := v4OnlyWriteFixture(t, path, "include: fragment.yaml\n")
			fragment := filepath.Join(filepath.Dir(path), "fragment.yaml")
			fragmentBefore := v4OnlyWriteFixture(t, fragment, "apiVersion: agent-marketplace/v4\n")
			intent := ReadIntent(t.Context(), LoadEffectiveOptions{RepoRoot: fixture.repo})
			v4OnlyAssertDiagnostic(t, intent.Layers[source].Err(), "v4.api-version-required", path)
			v4OnlyAssertUnchanged(t, path, before)
			v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
		})
	}
}

func TestHarnessEditV4KeepsSeparateLayerTargets(t *testing.T) {
	fixture := newV4OnlyFixture(t)
	user := fixture.paths[UserPersonal]
	repo := fixture.paths[RepoCommitted]
	v4OnlyWriteFixture(t, user, "apiVersion: agent-marketplace/v4\nharnesses: [factory, codex]\n")
	v4OnlyWriteFixture(t, repo, "apiVersion: agent-marketplace/v4\nharnesses: [claude]\n")
	if err := SetHarnessesInConfig(user, []string{"factory", "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := SetHarnessesInConfig(repo, []string{"claude"}); err != nil {
		t.Fatal(err)
	}
	userConfig, err := LoadGlobalConfig()
	if err != nil || !equalStrings(userConfig.Harnesses, []string{"factory", "codex"}) {
		t.Fatalf("user target set = %#v, %v", userConfig, err)
	}
	repoConfig, err := LoadConfigFromPath(repo)
	if err != nil || !equalStrings(repoConfig.Harnesses, []string{"claude"}) {
		t.Fatalf("repo target set = %#v, %v", repoConfig, err)
	}
}
