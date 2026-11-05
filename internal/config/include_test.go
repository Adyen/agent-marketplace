package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeIncludeConfig(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func registryURLs(cfg *Config) []string {
	out := make([]string, 0, len(cfg.Registries))
	for _, entry := range cfg.Registries {
		out = append(out, entry.URL)
	}
	return out
}

// A shared fragment is evaluated once per branch, not globally deduplicated.
func TestConfigIncludeResolutionOrderAndSharedDAG(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	leaf := filepath.Join(dir, "nested", "leaf.yaml")
	first := filepath.Join(dir, "first.yaml")
	absolute := filepath.Join(dir, "absolute.yaml")
	writeIncludeConfig(t, leaf, "sources: {shared: {directory: /leaf}}\nresources: {shared: [skill/leaf]}\n")
	writeIncludeConfig(t, first, "include: nested/leaf.yaml\nresources: {shared: {exclude: [skill/leaf], include: [skill/first]}}\n")
	writeIncludeConfig(t, absolute, "resources: {shared: [skill/absolute]}\n")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude:\n  - first.yaml\n  - "+absolute+"\n  - nested/../first.yaml\nresources: {shared: [skill/root, skill/root-second]}\n")
	v4, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{canonicalPath(t, leaf), canonicalPath(t, first), canonicalPath(t, absolute), canonicalPath(t, leaf), canonicalPath(t, first), canonicalPath(t, root)}
	if !reflect.DeepEqual(v4.Files, wantFiles) {
		t.Fatalf("files = %v, want per-branch order %v", v4.Files, wantFiles)
	}
	wantLocators := []string{"skill/leaf", "skill/first", "skill/leaf", "skill/absolute", "skill/leaf", "skill/first", "skill/leaf", "skill/root", "skill/root-second"}
	wantExclusions := []string{"", "", "skill/leaf", "", "", "", "skill/leaf", "", ""}
	if len(v4.Decisions) != len(wantLocators) {
		t.Fatalf("decisions = %#v, want nine ordered decisions", v4.Decisions)
	}
	for i, decision := range v4.Decisions {
		if decision.Locator != wantLocators[i] || decision.Exclusion != wantExclusions[i] || decision.Provenance.Precedence != i {
			t.Errorf("decision %d = %#v", i, decision)
		}
	}
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := registryURLs(cfg); !reflect.DeepEqual(got, []string{"/leaf"}) || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"absolute", "first", "root", "root-second"}) {
		t.Fatalf("runtime selection = %#v", cfg.Registries)
	}
}

func TestLoadConfigTreeFromRootDataUsesSuppliedBytes(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, filepath.Join(dir, "fragment.yaml"), "sources: {a: {directory: /fragment}}\nresources: {a: [skill/fragment]}\n")
	data := []byte("apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nsources: {b: {directory: /root}}\nresources: {b: [skill/root]}\n")
	writeIncludeConfig(t, root, string(data))
	canonical, err := canonicalConfigPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(canonical, canonical, data, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := li.bind(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registryURLs(cfg), []string{"/fragment", "/root"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registries = %v, want %v", got, want)
	}
}

func TestIncludeSelectorConflictsFavorLaterFilesAndExcludes(t *testing.T) {
	for _, tc := range []struct{ filename, kind string }{
		{"config.yaml", "collection"}, {"config.local.yaml", "skill"},
	} {
		t.Run(tc.filename, func(t *testing.T) {
			dir := t.TempDir()
			writeIncludeConfig(t, filepath.Join(dir, "fragment.yaml"), "sources: {shared: {directory: /shared}}\nresources: {shared: {include: ["+tc.kind+"/root-excludes], exclude: ["+tc.kind+"/root-wins, "+tc.kind+"/fragment-wins]}}\n")
			root := filepath.Join(dir, tc.filename)
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nresources: {shared: {include: ["+tc.kind+"/root-wins, "+tc.kind+"/same-file], exclude: ["+tc.kind+"/root-excludes, "+tc.kind+"/same-file]}}\n")
			cfg, err := LoadConfigFromPath(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Registries) != 1 {
				t.Fatalf("source policy lost: %#v", cfg.Registries)
			}
			got := cfg.Registries[0].Collections
			if tc.kind == "skill" {
				got = cfg.Registries[0].Skills
			}
			want := KindSelector{Include: []string{"root-wins"}, Exclude: []string{"fragment-wins", "root-excludes", "same-file"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s selector = %#v, want %#v", tc.kind, got, want)
			}
		})
	}
}

func TestNestedIncludesComposeSourcePolicyOnce(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "leaf.yaml"), "sources: {a: {directory: /leaf}}\nresources: {a: {all: true, harnesses: [factory]}}\n")
	writeIncludeConfig(t, filepath.Join(dir, "middle.yaml"), "include: leaf.yaml\nsources: {b: {directory: /middle}}\nresources: {a: {harnesses: [claude]}, b: {all: true, harnesses: [claude]}}\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: middle.yaml\nharnesses: [devin]\nsources: {c: {directory: /root}}\nresources: {a: {harnesses: [devin]}, c: {all: true, harnesses: [devin]}}\n")
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	got := [][]string{cfg.Registries[0].Harnesses, cfg.Registries[1].Harnesses, cfg.Registries[2].Harnesses}
	want := [][]string{{"devin"}, {"claude"}, {"devin"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registry harnesses = %v, want %v", got, want)
	}
}

func TestContainingSourcePolicyPreservesExplicitEmptyOverrides(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "leaf.yaml"), "sources: {a: {directory: /leaf}, b: {directory: /explicit}}\nresources: {a: {include: [skill/leaf], tags: [leaf]}, b: {include: [], tags: [], harnesses: []}}\n")
	writeIncludeConfig(t, filepath.Join(dir, "middle.yaml"), "include: leaf.yaml\nresources: {a: {tags: [middle]}}\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: middle.yaml\nresources: {a: {include: [skill/root], exclude: [skill/blocked], tags: [root-tag]}}\n")
	v4, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"leaf", "root"}) || !reflect.DeepEqual(cfg.Registries[0].Skills.Exclude, []string{"blocked"}) || !reflect.DeepEqual(cfg.Registries[0].Tags, []string{"root-tag"}) {
		t.Fatalf("source policy = %#v", cfg.Registries[0])
	}
	if len(v4.Groups["b"].Include) != 0 || cfg.Registries[1].Tags == nil || len(cfg.Registries[1].Tags) != 0 || cfg.Registries[1].Harnesses == nil || len(cfg.Registries[1].Harnesses) != 0 {
		t.Fatalf("explicit empty overrides lost: group=%#v entry=%#v", v4.Groups["b"], cfg.Registries[1])
	}
}

func TestConfigIncludeContainingFileHasHigherPrecedence(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "base.yaml"), "harnesses: [factory]\nmaterialization:\n  mode: symlink\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: base.yaml\nharnesses: [claude]\nmaterialization:\n  mode: file\n")
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"claude"}) || cfg.Materialization == nil || cfg.Materialization.Mode != "file" {
		t.Fatalf("config = %#v, want containing file values", cfg)
	}
}

func TestConfigIncludeFailuresIdentifyPath(t *testing.T) {
	for _, tc := range []struct{ name, child, content, code string }{
		{"missing", "missing.yaml", "", ""},
		{"malformed", "bad.yaml", "sources: [\n", "v4.parse"},
		{"wrong layer", "wrong.yaml", "logging: {level: debug}\n", "v4.layer-field"},
		{"explicit legacy", "legacy.yaml", "apiVersion: agent-marketplace/v3\n", "v4.api-version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			child := filepath.Join(dir, tc.child)
			if tc.content != "" {
				writeIncludeConfig(t, child, tc.content)
			}
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: "+tc.child+"\n")
			_, err := LoadConfigFromPath(root)
			if err == nil || !strings.Contains(err.Error(), child) {
				t.Fatalf("error = %v, want offending path", err)
			}
			if tc.code != "" {
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != tc.code {
					t.Fatalf("error = %v, want %s", err, tc.code)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing include error = %v", err)
			}
		})
	}
}

func TestUnreadableConfigIncludeFailsOwningLayer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file mode test requires a non-root user")
	}
	dir := t.TempDir()
	child := filepath.Join(dir, "child.yaml")
	writeIncludeConfig(t, child, "sources: {shared: {directory: /child}}\nresources: {shared: [skill/review]}\n")
	if err := os.Chmod(child, 0); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: child.yaml\n")
	_, err := LoadConfigFromPath(root)
	if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), child) {
		t.Fatalf("error = %v, want unreadable included path", err)
	}
}

func TestConfigIncludeCyclesFail(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "indirect"}[indirect], func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			if indirect {
				writeIncludeConfig(t, filepath.Join(dir, "child.yaml"), "include: config.yaml\n")
				writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: child.yaml\n")
			} else {
				writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: config.yaml\n")
			}
			_, err := LoadConfigFromPath(root)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != "v4.include-cycle" {
				t.Fatalf("error = %v, want v4.include-cycle", err)
			}
		})
	}
}

// spec: consumer-config-format/versionless-roots-are-refused-in-every-consumer-layer
// spec: consumer-config-format/v4-fragments-preserve-include-authority
func TestIncludedFragmentMayOmitAPIVersionButRootMayNot(t *testing.T) {
	dir := t.TempDir()
	fragment := filepath.Join(dir, "fragment.yaml")
	root := filepath.Join(dir, "config.yaml")
	for _, version := range []string{"", "apiVersion: agent-marketplace/v4\n"} {
		writeIncludeConfig(t, fragment, version+"sources: {shared: {directory: ./registry}}\nresources: {shared: [skill/review]}\n")
		writeIncludeConfig(t, root, "include: fragment.yaml\n")
		_, err := LoadConfigFromPath(root)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.api-version-required" || validation.Path != root {
			t.Fatalf("error = %v, want root version refusal", err)
		}
		writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n")
		cfg, err := LoadConfigFromPath(root)
		if err != nil || cfg == nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != filepath.Join(canonicalPath(t, dir), "registry") || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"review"}) {
			t.Fatalf("accepted fragment lost selection/base: cfg=%#v err=%v", cfg, err)
		}
	}
}

func TestConfigIncludesAcrossAllLayers(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"org", "user", "repo", "local"} {
		writeIncludeConfig(t, filepath.Join(dir, name+"-fragment.yaml"), "sources: {shared: {directory: /"+name+"-fragment}}\nresources: {shared: [skill/review]}\n")
	}
	org, user, repo, local := filepath.Join(dir, "org.yaml"), filepath.Join(dir, "user.yaml"), filepath.Join(dir, "repo.yaml"), filepath.Join(dir, "config.local.yaml")
	for name, path := range map[string]string{"org": org, "user": user, "repo": repo, "local": local} {
		writeIncludeConfig(t, path, "apiVersion: agent-marketplace/v4\ninclude: "+name+"-fragment.yaml\n")
	}
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{OrganizationPath: org, GlobalPath: user, RepoPath: repo, LocalPath: local, RepoRoot: dir, LocalConfigDir: dir})
	if len(ec.Errors) != 0 {
		t.Fatalf("errors = %v", ec.Errors)
	}
	if got, want := registryURLs(ec.Merged), []string{"/org-fragment", "/user-fragment", "/repo-fragment", "/local-fragment"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registries = %v, want %v", got, want)
	}
}

func TestPublicMachineLoadersPreserveIncludedTelemetry(t *testing.T) {
	for _, name := range []string{"organization", "user"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, ".agents", "config.yaml")
			writeIncludeConfig(t, filepath.Join(dir, ".agents", "telemetry.yaml"), "telemetry:\n  level: basic\n")
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: telemetry.yaml\n")
			var cfg *Config
			var err error
			if name == "organization" {
				SetTestOrganizationConfigPath(t, root)
				cfg, err = LoadOrganizationConfig()
			} else {
				t.Setenv("HOME", dir)
				cfg, err = LoadGlobalConfig()
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Telemetry == nil || cfg.Telemetry.Level != "basic" {
				t.Fatalf("telemetry = %#v, want included basic policy", cfg.Telemetry)
			}
		})
	}
}

func TestContainingFileTelemetryOverridesIncludedFragment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIncludeConfig(t, filepath.Join(home, ".agents", "telemetry.yaml"), "telemetry:\n  level: basic\n")
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\ninclude: telemetry.yaml\ntelemetry:\n  level: detailed\n")
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telemetry == nil || cfg.Telemetry.Level != "detailed" {
		t.Fatalf("telemetry = %#v, want containing detailed policy", cfg.Telemetry)
	}
}

// spec: logging/user-logging-settings-include-fragments
func TestUserLoggingLoaderResolvesIncludes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIncludeConfig(t, filepath.Join(home, ".agents", "logging.yaml"), "logging:\n  level: debug\n")
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v4\ninclude: logging.yaml\n")
	logging, err := LoadUserLoggingConfig()
	if err != nil {
		t.Fatal(err)
	}
	if logging == nil || logging.Level == nil || *logging.Level != "debug" {
		t.Fatalf("logging = %#v", logging)
	}
}

// spec: configuration-settings/settings-with-an-unreadable-acquisition-lock
// spec: logging/logging-needs-no-acquisition-lock
func TestLoadUserLoggingConfigIgnoresUnreadableLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), `apiVersion: agent-marketplace/v4
logging: {level: debug}
sources: {hosted: {marketplace: https://marketplace.invalid}}
`)
	if err := os.MkdirAll(filepath.Join(home, ".agents", "lock.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	logging, err := LoadUserLoggingConfig()
	if err != nil {
		t.Fatalf("LoadUserLoggingConfig: %v", err)
	}
	if logging == nil || logging.Level == nil || *logging.Level != "debug" {
		t.Fatalf("logging = %#v", logging)
	}
}

func TestLoadRootConfigFromPathDoesNotExposeIncludedEntries(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "child.yaml"), "sources: {child: {directory: /child}}\nresources: {child: [skill/child]}\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: child.yaml\nsources: {root: {directory: /root}}\nresources: {root: [skill/root]}\n")
	cfg, err := LoadRootConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registryURLs(cfg), []string{"/root"}; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"root"}) {
		t.Fatalf("root registries = %#v, want root-only selection", cfg.Registries)
	}
}
