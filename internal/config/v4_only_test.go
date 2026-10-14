package config

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var v4OnlyVersions = []struct {
	name, header, code string
}{
	{"v1", "apiVersion: agent-marketplace/v1\n", "v4.api-version"},
	{"v2", "apiVersion: agent-marketplace/v2\n", "v4.api-version"},
	{"v3", "apiVersion: agent-marketplace/v3\n", "v4.api-version"},
	{"unknown", "apiVersion: agent-marketplace/v99\n", "v4.api-version"},
	{"empty", "apiVersion: ''\n", "v4.api-version"},
	{"null", "apiVersion: null\n", "v4.api-version"},
	{"number", "apiVersion: 4\n", "v4.api-version"},
	{"boolean", "apiVersion: true\n", "v4.api-version"},
	{"sequence", "apiVersion: [agent-marketplace/v4]\n", "v4.api-version"},
	{"mapping", "apiVersion: {version: agent-marketplace/v4}\n", "v4.api-version"},
	{"whitespace", "apiVersion: ' agent-marketplace/v4 '\n", "v4.api-version"},
	{"case", "apiVersion: agent-marketplace/V4\n", "v4.api-version"},
	{"versionless", "", "v4.api-version-required"},
	{"v4", "apiVersion: agent-marketplace/v4\n", ""},
}

type v4OnlyFixture struct {
	repo  string
	paths map[Source]string
}

func newV4OnlyFixture(t *testing.T) v4OnlyFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for key, path := range map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME": filepath.Join(home, ".cache"), "XDG_STATE_HOME": filepath.Join(home, ".local", "state"),
		"CODEX_HOME": filepath.Join(home, ".codex"),
	} {
		t.Setenv(key, path)
	}
	fixture := v4OnlyFixture{
		repo: filepath.Join(root, "repo"),
		paths: map[Source]string{
			Organization:  filepath.Join(root, "organization", "config.yaml"),
			UserPersonal:  filepath.Join(home, ".agents", "config.yaml"),
			RepoCommitted: filepath.Join(root, "repo", ".agents", "config.yaml"),
			RepoPersonal:  filepath.Join(root, "repo", ".agents", "config.local.yaml"),
		},
	}
	mustMkdirAll(t, home)
	mustMkdirAll(t, fixture.repo)
	withWorkingDir(t, fixture.repo)
	SetTestOrganizationConfigPath(t, fixture.paths[Organization])
	return fixture
}

func v4OnlyRootBody(source Source) string {
	switch source {
	case UserPersonal:
		return "harnesses: [claude]\nlogging: {level: debug}\ntelemetry: {level: off}\n"
	case Organization:
		return "harnesses: [claude]\ntelemetry: {level: basic}\n"
	case RepoPersonal:
		return "materialization: {mode: file}\n"
	default:
		return "harnesses: [claude]\n"
	}
}

type v4OnlyFileState struct {
	data []byte
	info os.FileInfo
}

func v4OnlyWriteFixture(t *testing.T, path, content string) v4OnlyFileState {
	t.Helper()
	writeFile(t, path, content)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(946684800, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	return v4OnlyFileSnapshot(t, path)
}

func v4OnlyFileSnapshot(t *testing.T, path string) v4OnlyFileState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return v4OnlyFileState{data: data, info: info}
}

func v4OnlyAssertUnchanged(t *testing.T, path string, before v4OnlyFileState) {
	t.Helper()
	after := v4OnlyFileSnapshot(t, path)
	if !os.SameFile(after.info, before.info) {
		t.Errorf("config identity changed for %q", path)
	}
	if !bytes.Equal(after.data, before.data) {
		t.Errorf("config bytes changed for %q:\nbefore:\n%safter:\n%s", path, before.data, after.data)
	}
	if after.info.Mode() != before.info.Mode() {
		t.Errorf("config mode changed for %q: %v -> %v", path, before.info.Mode(), after.info.Mode())
	}
	if !after.info.ModTime().Equal(before.info.ModTime()) {
		t.Errorf("config mtime changed for %q: %v -> %v", path, before.info.ModTime(), after.info.ModTime())
	}
}

func v4OnlyAssertDiagnostic(t *testing.T, err error, code, path string) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) {
		t.Errorf("error = %T %v, want *V4ValidationError with code %q", err, err, code)
		return
	}
	if validation.Code != code {
		t.Errorf("diagnostic code = %q, want %q (error: %v)", validation.Code, code, err)
	}
	if validation.Path == "" || !strings.Contains(err.Error(), path) && !strings.Contains(err.Error(), canonicalPath(t, path)) {
		t.Errorf("error = %v, want context for config %q", err, path)
	}
	if !strings.Contains(err.Error(), "apiVersion") {
		t.Errorf("error = %v, want apiVersion field context", err)
	}
}

// spec: consumer-config-format/v4-roots-are-accepted-in-every-consumer-layer
// spec: consumer-config-format/versionless-roots-are-refused-in-every-consumer-layer
// spec: consumer-config-format/explicit-non-v4-root-versions-are-refused
func TestV4OnlyPublicRootLoaders(t *testing.T) {
	type rootReader struct {
		name   string
		source Source
		load   func(v4OnlyFixture, string) (any, error)
	}
	readers := []rootReader{
		{"LoadConfigFromPath/repo", RepoCommitted, func(_ v4OnlyFixture, p string) (any, error) { return LoadConfigFromPath(p) }},
		{"LoadConfigFromPath/user", UserPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadConfigFromPath(p) }},
		{"LoadConfigFromPath/local", RepoPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadConfigFromPath(p) }},
		{"LoadRootConfigFromPath/repo", RepoCommitted, func(_ v4OnlyFixture, p string) (any, error) { return LoadRootConfigFromPath(p) }},
		{"LoadRootConfigFromPath/user", UserPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadRootConfigFromPath(p) }},
		{"LoadRootConfigFromPath/local", RepoPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadRootConfigFromPath(p) }},
		{"LoadLocalConfigFromPath", RepoPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadLocalConfigFromPath(p) }},
		{"LoadGlobalConfig", UserPersonal, func(_ v4OnlyFixture, _ string) (any, error) { return LoadGlobalConfig() }},
		{"LoadRepoConfig", RepoCommitted, func(_ v4OnlyFixture, _ string) (any, error) { return LoadRepoConfig() }},
		{"LoadLocalConfig", RepoPersonal, func(_ v4OnlyFixture, _ string) (any, error) { return LoadLocalConfig() }},
		{"LoadOrganizationConfig", Organization, func(_ v4OnlyFixture, _ string) (any, error) { return LoadOrganizationConfig() }},
		{"LoadUserSettings", UserPersonal, func(_ v4OnlyFixture, _ string) (any, error) { return LoadUserSettings() }},
		{"LoadOrganizationSettings", Organization, func(_ v4OnlyFixture, _ string) (any, error) { return LoadOrganizationSettings() }},
		{"LoadUserLoggingConfig", UserPersonal, func(_ v4OnlyFixture, _ string) (any, error) { return LoadUserLoggingConfig() }},
		{"LoadV4ConfigFromPath/repo", RepoCommitted, func(_ v4OnlyFixture, p string) (any, error) { return LoadV4ConfigFromPath(p) }},
		{"LoadV4ConfigFromPath/local", RepoPersonal, func(_ v4OnlyFixture, p string) (any, error) { return LoadV4ConfigFromPath(p) }},
	}
	for _, layer := range []struct {
		name   string
		source Source
	}{
		{"organization", Organization}, {"user", UserPersonal}, {"repo", RepoCommitted}, {"local", RepoPersonal},
	} {
		readers = append(readers, rootReader{"LoadV4ConfigIfPresent/" + layer.name, layer.source, func(_ v4OnlyFixture, p string) (any, error) {
			return LoadV4ConfigIfPresent(p, layer.source)
		}})
		readers = append(readers, rootReader{"ReadIntent/" + layer.name, layer.source, func(f v4OnlyFixture, _ string) (any, error) {
			intent := ReadIntent(context.Background(), LoadEffectiveOptions{RepoRoot: f.repo})
			return intent, errors.Join(intent.Errors...)
		}})
	}
	for _, reader := range readers {
		t.Run(reader.name, func(t *testing.T) {
			for _, version := range v4OnlyVersions {
				t.Run(version.name, func(t *testing.T) {
					fixture := newV4OnlyFixture(t)
					path := fixture.paths[reader.source]
					before := v4OnlyWriteFixture(t, path, version.header+v4OnlyRootBody(reader.source))
					t.Cleanup(func() { v4OnlyAssertUnchanged(t, path, before) })
					got, err := reader.load(fixture, path)
					if version.code != "" {
						v4OnlyAssertDiagnostic(t, err, version.code, path)
						return
					}
					if err != nil {
						t.Fatalf("exact v4 root rejected: %v", err)
					}
					v4OnlyAssertLoaded(t, got, reader.source)
				})
			}
		})
	}
}

func v4OnlyAssertLoaded(t *testing.T, got any, source Source) {
	t.Helper()
	var settings Settings
	switch cfg := got.(type) {
	case *Config:
		if cfg == nil || cfg.APIVersion != "agent-marketplace/v4" {
			t.Fatalf("config = %#v, want explicit v4", cfg)
		}
		settings = Settings{Harnesses: cfg.Harnesses, Materialization: cfg.Materialization, Logging: cfg.Logging, Telemetry: cfg.Telemetry}
	case *LocalConfig:
		if cfg == nil || cfg.APIVersion != "agent-marketplace/v4" {
			t.Fatalf("local config = %#v, want explicit v4", cfg)
		}
		settings.Materialization = cfg.Materialization
	case *V4Config:
		if cfg == nil || cfg.APIVersion != "agent-marketplace/v4" {
			t.Fatalf("v4 config = %#v, want explicit v4", cfg)
		}
		settings = Settings{Harnesses: cfg.Harnesses, Materialization: cfg.Materialization, Logging: cfg.Logging, Telemetry: cfg.Telemetry}
	case Settings:
		settings = cfg
	case *LoggingConfig:
		if cfg == nil || cfg.Level == nil || *cfg.Level != "debug" {
			t.Fatalf("logging = %#v, want debug", cfg)
		}
		return
	case Intent:
		if !cfg.Layers[source].Present() || cfg.Layers[source].Err() != nil {
			t.Fatalf("v4 intent layer missing or invalid: %#v", cfg.Layers[source])
		}
		settings = cfg.Layers[source].Settings()
	default:
		t.Fatalf("unexpected consumer result %T", got)
	}
	if source == RepoPersonal {
		if settings.Materialization == nil || settings.Materialization.Mode != "file" {
			t.Fatalf("materialization = %#v, want file", settings.Materialization)
		}
		return
	}
	if !reflect.DeepEqual(settings.Harnesses, []string{"claude"}) {
		t.Errorf("harnesses = %v, want [claude]", settings.Harnesses)
	}
	if source == UserPersonal && (settings.Logging == nil || settings.Logging.Level == nil || *settings.Logging.Level != "debug" || settings.Telemetry == nil || settings.Telemetry.Level != "off") {
		t.Errorf("user settings lost logging or telemetry: %#v", settings)
	}
	if source == Organization && (settings.Telemetry == nil || settings.Telemetry.Level != "basic") {
		t.Errorf("organization settings lost telemetry: %#v", settings)
	}
}

// spec: consumer-config-format/v4-fragments-preserve-include-authority
// spec: consumer-config-format/explicit-non-v4-fragments-refuse-the-include-tree
func TestV4OnlyIncludedFragments(t *testing.T) {
	for _, reader := range []struct {
		name   string
		source Source
		load   func(string) (*Config, error)
	}{
		{"LoadConfigFromPath/repo", RepoCommitted, LoadConfigFromPath},
		{"LoadConfigFromPath/user", UserPersonal, LoadConfigFromPath},
		{"LoadConfigFromPath/local", RepoPersonal, LoadConfigFromPath},
		{"LoadLocalConfigFromPath", RepoPersonal, func(p string) (*Config, error) {
			cfg, err := LoadLocalConfigFromPath(p)
			if cfg == nil {
				return nil, err
			}
			return &Config{APIVersion: cfg.APIVersion, Registries: cfg.Registries}, err
		}},
		{"LoadOrganizationConfig", Organization, func(_ string) (*Config, error) { return LoadOrganizationConfig() }},
	} {
		t.Run(reader.name, func(t *testing.T) {
			for _, version := range v4OnlyVersions {
				t.Run(version.name, func(t *testing.T) {
					fixture := newV4OnlyFixture(t)
					path := fixture.paths[reader.source]
					fragment := filepath.Join(filepath.Dir(path), "fragment.yaml")
					registry := filepath.Join(filepath.Dir(fragment), "resources")
					mustMkdirAll(t, registry)
					registry = canonicalPath(t, registry)
					rootBefore := v4OnlyWriteFixture(t, path, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
					fragmentBefore := v4OnlyWriteFixture(t, fragment, version.header+"sources:\n  included:\n    directory: ./resources\nresources:\n  included:\n    include: [skill/from-fragment]\n")
					t.Cleanup(func() {
						v4OnlyAssertUnchanged(t, path, rootBefore)
						v4OnlyAssertUnchanged(t, fragment, fragmentBefore)
					})
					cfg, err := reader.load(path)
					if version.code != "" && version.name != "versionless" {
						v4OnlyAssertDiagnostic(t, err, "v4.api-version", fragment)
						return
					}
					if err != nil {
						t.Fatalf("valid v4 fragment rejected: %v", err)
					}
					if cfg == nil || cfg.APIVersion != "agent-marketplace/v4" || len(cfg.Registries) != 1 {
						t.Fatalf("included config = %#v, want one v4 registry", cfg)
					}
					entry := cfg.Registries[0]
					if entry.URL != registry || !reflect.DeepEqual(entry.Skills.Include, []string{"from-fragment"}) {
						t.Errorf("included registry = %#v, want %q selecting only from-fragment", entry, registry)
					}
				})
			}
		})
	}
}

func TestV4OnlyAlternateWriters(t *testing.T) {
	for _, writer := range []struct {
		name                   string
		source                 Source
		write                  func(string) (int, error)
		wantHarnesses          []any
		wantTelemetry, wantRef string
	}{
		{"SetHarnessesInConfig/user", UserPersonal, func(p string) (int, error) { return 0, SetHarnessesInConfig(p, []string{"claude"}) }, []any{"claude"}, "off", "main"},
		{"SetHarnessesInConfig/repo", RepoCommitted, func(p string) (int, error) { return 0, SetHarnessesInConfig(p, []string{"claude"}) }, []any{"claude"}, "", "main"},
		{"SetUserTelemetryLevel", UserPersonal, func(p string) (int, error) { return 0, SetUserTelemetryLevel(p, "detailed") }, []any{"factory"}, "detailed", "main"},
		{"UpdateRegistryRefInConfig/user", UserPersonal, func(p string) (int, error) {
			return UpdateRegistryRefInConfig(p, "https://example.test/agents.git", "main", "release")
		}, []any{"factory"}, "off", "release"},
		{"UpdateRegistryRefInConfig/repo", RepoCommitted, func(p string) (int, error) {
			return UpdateRegistryRefInConfig(p, "https://example.test/agents.git", "main", "release")
		}, []any{"factory"}, "", "release"},
		{"UpdateRegistryRefInConfig/local", RepoPersonal, func(p string) (int, error) {
			return UpdateRegistryRefInConfig(p, "https://example.test/agents.git", "main", "release")
		}, nil, "", "release"},
	} {
		t.Run(writer.name, func(t *testing.T) {
			for _, version := range v4OnlyVersions {
				t.Run(version.name, func(t *testing.T) {
					fixture := newV4OnlyFixture(t)
					path := fixture.paths[writer.source]
					body := "# consumer config\n"
					if writer.source != RepoPersonal {
						body += "harnesses: [factory] # harness selection\n"
					}
					if writer.source == UserPersonal {
						body += "logging: {level: debug} # unrelated logging\ntelemetry: {level: off}\n"
					}
					if version.name == "v4" {
						body += "sources:\n  agents:\n    git: https://example.test/agents.git\n    ref: main # registry ref\n  other: {git: https://example.test/other.git, ref: stable} # keep other source\nresources:\n  agents: [skill/review] # selected resource\n"
					} else {
						body += "registries:\n  - url: https://example.test/agents.git\n    ref: main # registry ref\n    skills: {include: [review]} # selected resource\n  - url: https://example.test/other.git\n    ref: stable # keep other source\n"
					}
					before := v4OnlyWriteFixture(t, path, version.header+body)
					entriesBefore, err := os.ReadDir(filepath.Dir(path))
					if err != nil {
						t.Fatal(err)
					}
					if version.code != "" {
						t.Cleanup(func() {
							v4OnlyAssertUnchanged(t, path, before)
							entriesAfter, err := os.ReadDir(filepath.Dir(path))
							if err != nil {
								t.Fatal(err)
							}
							if len(entriesAfter) != len(entriesBefore) {
								t.Errorf("refused writer left files in config directory: %v", entriesAfter)
							}
						})
					}
					updated, err := writer.write(path)
					if version.code != "" {
						v4OnlyAssertDiagnostic(t, err, version.code, path)
						if writer.wantRef == "release" && updated != 0 {
							t.Errorf("refused writer updated %d registry refs, want 0", updated)
						}
						return
					}
					if err != nil {
						t.Fatalf("valid v4 edit rejected: %v", err)
					}
					if writer.wantRef == "release" && updated != 1 {
						t.Errorf("updated refs = %d, want 1", updated)
					}
					after := v4OnlyFileSnapshot(t, path)
					if bytes.Equal(before.data, after.data) {
						t.Error("v4 positive control did not change config bytes")
					}
					if after.info.Mode() != before.info.Mode() {
						t.Errorf("v4 writer changed permissions: %v -> %v", before.info.Mode(), after.info.Mode())
					}
					want := map[string]any{
						"apiVersion": "agent-marketplace/v4",
						"sources": map[string]any{
							"agents": map[string]any{"git": "https://example.test/agents.git", "ref": writer.wantRef},
							"other":  map[string]any{"git": "https://example.test/other.git", "ref": "stable"},
						},
						"resources": map[string]any{"agents": []any{"skill/review"}},
					}
					if writer.wantHarnesses != nil {
						want["harnesses"] = writer.wantHarnesses
					}
					if writer.wantTelemetry != "" {
						want["logging"] = map[string]any{"level": "debug"}
						want["telemetry"] = map[string]any{"level": writer.wantTelemetry}
					}
					var got map[string]any
					if err := yaml.Unmarshal(after.data, &got); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("edited document = %#v, want %#v", got, want)
					}
					for _, comment := range []string{"# consumer config", "# registry ref", "# keep other source", "# selected resource"} {
						if !bytes.Contains(after.data, []byte(comment)) {
							t.Errorf("v4 edit lost comment %q:\n%s", comment, after.data)
						}
					}
					if writer.wantHarnesses != nil && !bytes.Contains(after.data, []byte("# harness selection")) {
						t.Error("v4 edit lost harness comment")
					}
					if writer.wantTelemetry != "" && !bytes.Contains(after.data, []byte("# unrelated logging")) {
						t.Error("v4 edit lost unrelated logging comment")
					}
				})
			}
		})
	}
}
