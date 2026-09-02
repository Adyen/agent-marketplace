package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
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

func TestConfigIncludeResolutionOrderAndDeduplication(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, filepath.Join(dir, "nested", "leaf.yaml"), "registries:\n  - path: /leaf\n")
	writeIncludeConfig(t, filepath.Join(dir, "first.yaml"), "include: nested/leaf.yaml\nregistries:\n  - path: /first\n")
	absolute := filepath.Join(dir, "absolute.yaml")
	writeIncludeConfig(t, absolute, "registries:\n  - path: /absolute\n")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude:\n  - first.yaml\n  - "+absolute+"\n  - nested/../first.yaml\nregistries:\n  - path: /root\n  - path: /root-second\n")

	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registryURLs(cfg), []string{"/leaf", "/first", "/absolute", "/root", "/root-second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registries = %v, want %v", got, want)
	}
	precedence := make([]int, 0, len(cfg.Registries))
	for _, entry := range cfg.Registries {
		precedence = append(precedence, entry.FilePrecedence)
	}
	if want := []int{1, 2, 3, 4, 4}; !reflect.DeepEqual(precedence, want) {
		t.Fatalf("file precedence = %v, want %v", precedence, want)
	}
}

func TestIncludeSelectorConflictsFavorLaterFilesAndExcludes(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "fragment.yaml"), "collections:\n  exclude: [root-wins, fragment-wins]\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\ncollections:\n  include: [root-wins, same-file]\n  exclude: [same-file]\n")
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	wantCollections := KindSelector{Include: []string{"root-wins"}, Exclude: []string{"fragment-wins", "same-file"}}
	if !reflect.DeepEqual(cfg.Collections, wantCollections) {
		t.Fatalf("collections = %#v, want %#v", cfg.Collections, wantCollections)
	}

	localRoot := filepath.Join(dir, "config.local.yaml")
	writeIncludeConfig(t, filepath.Join(dir, "local-fragment.yaml"), "skills:\n  exclude: [root-skill, fragment-skill]\n")
	writeIncludeConfig(t, localRoot, "include: local-fragment.yaml\nskills:\n  include: [root-skill, same-skill]\n  exclude: [same-skill]\n")
	local, err := LoadLocalConfigFromPath(localRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantSkills := KindSelector{Include: []string{"root-skill"}, Exclude: []string{"fragment-skill", "same-skill"}}
	if !reflect.DeepEqual(local.Skills, wantSkills) {
		t.Fatalf("local skills = %#v, want %#v", local.Skills, wantSkills)
	}
}

func TestNestedIncludesComposeDefaultsOnce(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "leaf.yaml"), "registries:\n  - path: /leaf\n")
	writeIncludeConfig(t, filepath.Join(dir, "middle.yaml"), "include: leaf.yaml\nharnesses: [claude]\nregistries:\n  - path: /middle\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: middle.yaml\nharnesses: [devin]\nregistries:\n  - path: /root\n")
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

func TestConfigIncludeContainingFileHasHigherPrecedence(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "base.yaml"), "harnesses: [factory]\nmaterialization:\n  mode: symlink\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: base.yaml\nharnesses: [claude]\nmaterialization:\n  mode: file\n")
	cfg, err := LoadConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Harnesses, []string{"claude"}) || cfg.Materialization == nil || cfg.Materialization.Mode != "file" {
		t.Fatalf("config = %#v, want containing file values", cfg)
	}
}

func TestConfigIncludeFailuresIdentifyPath(t *testing.T) {
	for _, tc := range []struct {
		name, child string
	}{
		{name: "missing", child: "missing.yaml"},
		{name: "malformed", child: "bad.yaml"},
		{name: "wrong layer", child: "wrong.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			if tc.name == "malformed" {
				writeIncludeConfig(t, filepath.Join(dir, tc.child), "registries: [\n")
			}
			if tc.name == "wrong layer" {
				writeIncludeConfig(t, filepath.Join(dir, tc.child), "logging:\n  level: debug\n")
			}
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: "+tc.child+"\n")
			_, err := LoadConfigFromPath(root)
			if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, tc.child)) {
				t.Fatalf("error = %v, want offending path", err)
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
	writeIncludeConfig(t, child, "registries:\n  - path: /child\n")
	if err := os.Chmod(child, 0); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: child.yaml\n")
	_, err := LoadConfigFromPath(root)
	if err == nil || !strings.Contains(err.Error(), child) {
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
				writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: child.yaml\n")
			} else {
				writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: config.yaml\n")
			}
			if _, err := LoadConfigFromPath(root); err == nil || !strings.Contains(err.Error(), "cycle") {
				t.Fatalf("error = %v, want cycle", err)
			}
		})
	}
}

func TestIncludedFragmentMayOmitAPIVersionButRootMayNot(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "fragment.yaml"), "registries:\n  - path: /fragment\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "include: fragment.yaml\n")
	if _, err := LoadConfigFromPath(root); err == nil || !strings.Contains(err.Error(), "apiVersion") {
		t.Fatalf("error = %v, want root apiVersion failure", err)
	}
}

func TestConfigIncludesAcrossAllLayers(t *testing.T) {
	dir := t.TempDir()
	paths := []string{"org", "user", "repo", "local"}
	for _, name := range paths {
		writeIncludeConfig(t, filepath.Join(dir, name+"-fragment.yaml"), "registries:\n  - path: /"+name+"-fragment\n")
	}
	org := filepath.Join(dir, "org.yaml")
	user := filepath.Join(dir, "user.yaml")
	repo := filepath.Join(dir, "repo.yaml")
	local := filepath.Join(dir, "config.local.yaml")
	writeIncludeConfig(t, org, "apiVersion: agent-marketplace/v2\ninclude: org-fragment.yaml\n")
	writeIncludeConfig(t, user, "apiVersion: agent-marketplace/v2\ninclude: user-fragment.yaml\n")
	writeIncludeConfig(t, repo, "apiVersion: agent-marketplace/v2\ninclude: repo-fragment.yaml\n")
	writeIncludeConfig(t, local, "include: local-fragment.yaml\n")
	ec := LoadEffective(context.Background(), LoadEffectiveOptions{OrganizationPath: org, GlobalPath: user, RepoPath: repo, LocalPath: local, RepoRoot: dir, LocalConfigDir: dir})
	if len(ec.Errors) != 0 {
		t.Fatalf("errors = %v", ec.Errors)
	}
	if got, want := registryURLs(ec.Merged), []string{"/org-fragment", "/user-fragment", "/repo-fragment", "/local-fragment"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registries = %v, want %v", got, want)
	}
}

func TestPublicMachineLoadersPreserveIncludedTelemetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		load func(t *testing.T, root string) (*Config, error)
	}{
		{
			name: "organization",
			load: func(t *testing.T, root string) (*Config, error) {
				SetTestV3OrganizationConfigPath(t, root)
				return LoadOrganizationConfig()
			},
		},
		{
			name: "user",
			load: func(t *testing.T, root string) (*Config, error) {
				t.Setenv("HOME", filepath.Dir(filepath.Dir(root)))
				return LoadGlobalConfig()
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, ".agents", "config.yaml")
			writeIncludeConfig(t, filepath.Join(dir, ".agents", "telemetry.yaml"), "telemetry:\n  level: basic\n")
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: telemetry.yaml\n")
			cfg, err := tc.load(t, root)
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
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\ninclude: telemetry.yaml\ntelemetry:\n  level: detailed\n")
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Telemetry == nil || cfg.Telemetry.Level != "detailed" {
		t.Fatalf("telemetry = %#v, want containing detailed policy", cfg.Telemetry)
	}
}

func TestUserLoggingLoaderResolvesIncludes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeIncludeConfig(t, filepath.Join(home, ".agents", "logging.yaml"), "logging:\n  level: debug\n")
	writeIncludeConfig(t, filepath.Join(home, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\ninclude: logging.yaml\n")
	logging, err := LoadUserLoggingConfig()
	if err != nil {
		t.Fatal(err)
	}
	if logging == nil || logging.Level == nil || *logging.Level != "debug" {
		t.Fatalf("logging = %#v", logging)
	}
}

func TestLoadRootConfigFromPathDoesNotExposeIncludedEntries(t *testing.T) {
	dir := t.TempDir()
	writeIncludeConfig(t, filepath.Join(dir, "child.yaml"), "registries:\n  - path: /child\n")
	root := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: child.yaml\nregistries:\n  - path: /root\n")
	cfg, err := LoadRootConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := registryURLs(cfg), []string{"/root"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root registries = %v, want %v", got, want)
	}
}

func TestDisableResourceWithShadowFallbackDoesNotEditIncludedRegistry(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.yaml")
	root := filepath.Join(dir, "config.yaml")
	childData := "registries:\n  - url: git@example.com:team.git\n    ref: main\n    skills: {include: [owned-by-child]}\n"
	writeIncludeConfig(t, child, childData)
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: child.yaml\n")

	if err := DisableResourceWithShadowFallback(root, "git@example.com:team.git", "", "main", ir.KindSkill, "owned-by-child"); err != nil {
		t.Fatal(err)
	}
	gotChild, err := os.ReadFile(child)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotChild) != childData {
		t.Fatalf("included file changed:\n%s", gotChild)
	}
	rootCfg, err := LoadRootConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootCfg.Registries) != 1 || !stringSliceContains(rootCfg.Registries[0].Skills.Exclude, "owned-by-child") {
		t.Fatalf("root shadow entry = %#v", rootCfg.Registries)
	}
	gotRoot, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotRoot), "include: child.yaml") {
		t.Fatalf("root include was not preserved:\n%s", gotRoot)
	}
}

func TestEditorPreservesIncludeAndMutatesOnlyRoot(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child.yaml")
	root := filepath.Join(dir, "config.yaml")
	childData := "registries:\n  - path: /child\n    skills: {include: [old]}\n"
	writeIncludeConfig(t, child, childData)
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: child.yaml\nregistries:\n  - path: /root\n")
	if err := AddSkillsToRegistryInConfig(root, "/root", "", []string{"new"}); err != nil {
		t.Fatal(err)
	}
	gotChild, err := os.ReadFile(child)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotChild) != childData {
		t.Fatalf("included file changed:\n%s", gotChild)
	}
	gotRoot, err := os.ReadFile(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotRoot), "include: child.yaml") || !strings.Contains(string(gotRoot), "new") {
		t.Fatalf("root edit did not preserve include/target root:\n%s", gotRoot)
	}
}
