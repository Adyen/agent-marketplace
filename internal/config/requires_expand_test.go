package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

// spec: configuration-settings/expansion-is-disabled-when-no-layer-sets-it
func TestRequiresExpansionDefaultsFalseWhenUnset(t *testing.T) {
	var nilCfg *Config
	if nilCfg.RequiresExpansionEnabled() {
		t.Fatal("nil config must report expansion disabled")
	}
	if (&Config{}).RequiresExpansionEnabled() {
		t.Fatal("config without requires must report expansion disabled")
	}
	explicitFalse := false
	cfg := &Config{Requires: &RequiresConfig{Expand: &explicitFalse}}
	if cfg.RequiresExpansionEnabled() {
		t.Fatal("explicit requires.expand: false must report expansion disabled")
	}
	explicitTrue := true
	on := &Config{Requires: &RequiresConfig{Expand: &explicitTrue}}
	if !on.RequiresExpansionEnabled() {
		t.Fatal("requires.expand: true must report expansion enabled")
	}
}

// spec: configuration-settings/expansion-setting-follows-layered-and-include-precedence
func TestV4RequiresExpandParsesAndOverridesIncludes(t *testing.T) {
	dir := t.TempDir()
	fragment := `requires: {expand: true}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}
	root := `apiVersion: agent-marketplace/v4
include: fragment.yaml
requires: {expand: false}
`
	rootPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(rootPath, []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if v4.Requires == nil || v4.Requires.Expand == nil || *v4.Requires.Expand {
		t.Fatalf("containing file must override the include: Requires = %#v", v4.Requires)
		return
	}
}

// spec: configuration-settings/expansion-setting-follows-layered-and-include-precedence
func TestV4RequiresExpandInheritedFromInclude(t *testing.T) {
	dir := t.TempDir()
	fragment := `requires: {expand: true}
`
	if err := os.WriteFile(filepath.Join(dir, "fragment.yaml"), []byte(fragment), 0o644); err != nil {
		t.Fatal(err)
	}
	root := `apiVersion: agent-marketplace/v4
include: fragment.yaml
`
	rootPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(rootPath, []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	if v4.Requires == nil || v4.Requires.Expand == nil || !*v4.Requires.Expand {
		t.Fatalf("include value must be retained when the containing file is silent: Requires = %#v", v4.Requires)
		return
	}
}

// spec: configuration-settings/expansion-setting-follows-layered-and-include-precedence
func TestRequiresExpandLayerPrecedencePreservesExplicitFalse(t *testing.T) {
	tru := true
	fls := false
	org := &Config{Requires: &RequiresConfig{Expand: &tru}}
	user := &Config{Requires: &RequiresConfig{Expand: &fls}}
	repo := &Config{}
	local := &LocalConfig{Requires: &RequiresConfig{Expand: &tru}}

	merged := MergeLayers([]Layer{
		{Source: Organization, Config: org},
		{Source: UserPersonal, Config: user},
		{Source: RepoCommitted, Config: repo},
	})
	if merged.RequiresExpansionEnabled() {
		t.Fatal("explicit false at a higher-precedence layer must override true")
	}

	layers := []Layer{
		{Source: Organization, Config: org},
		{Source: UserPersonal, Config: user},
		{Source: RepoCommitted, Config: repo},
	}
	if l := LocalConfigAsLayer(local); l != nil {
		layers = append(layers, *l)
	} else {
		t.Fatal("a local layer carrying only requires.expand must still merge")
	}
	merged = MergeLayers(layers)
	if !merged.RequiresExpansionEnabled() {
		t.Fatal("personal-local requires.expand: true must win as the highest-precedence layer")
	}
}

// spec: configuration-settings/invalid-expansion-values-are-rejected
func TestV4RequiresExpandRejectsInvalidValues(t *testing.T) {
	cases := map[string]struct {
		body string
		code string
	}{
		"non-mapping": {"requires: yes\n", "v4.type"},
		"non-boolean": {"requires: {expand: yes please}\n", "v4.type"},
		"unknown key": {"requires: {unknown: true}\n", "v4.unknown-field"},
	}
	for name, tc := range cases {
		_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\n"+tc.body), "config.yaml", true)
		var v4Err *V4ValidationError
		if err == nil || !errors.As(err, &v4Err) {
			t.Fatalf("%s: expected a V4ValidationError, got %v", name, err)
		}
		if v4Err.Code != tc.code {
			t.Fatalf("%s: stable validation code = %q, want %q (err %v)", name, v4Err.Code, tc.code, err)
		}
	}
}

// spec: configuration-settings/legacy-configuration-uses-the-release-default
func TestLegacyConfigUsesReleaseDefault(t *testing.T) {
	for _, version := range []string{"v2", "v3"} {
		cfg, errs := loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/"+version+"\nharnesses: [claude]\n"), userLayer, false, true)
		if len(errs) > 0 {
			t.Fatalf("%s config failed to load: %v", version, errs)
		}
		if cfg.RequiresExpansionEnabled() {
			t.Fatalf("%s config must keep the release default of expansion disabled", version)
		}
		_, errs = loadV3ConfigWithAPIVersion([]byte("apiVersion: agent-marketplace/"+version+"\nharnesses: [claude]\nrequires: {expand: true}\n"), userLayer, false, true)
		if len(errs) == 0 {
			t.Fatalf("requires in a %s config must be rejected by strict validation", version)
		}
	}
}

// spec: configuration-settings/expansion-setting-follows-layered-and-include-precedence
func TestRequiresExpandRealLoadersRespectNestedIncludesAndLayers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("fragB.yaml", "requires: {expand: false}\n")
	write("fragA.yaml", "include: fragB.yaml\n")
	userPath := write("config.yaml", "apiVersion: agent-marketplace/v4\ninclude: fragA.yaml\n")
	orgPath := write("org.yaml", "apiVersion: agent-marketplace/v4\nrequires: {expand: true}\n")
	repoPath := write("repo.yaml", "apiVersion: agent-marketplace/v4\nrequires: {expand: true}\n")

	user, err := LoadConfigFromPath(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if user.Requires == nil || user.Requires.Expand == nil || *user.Requires.Expand {
		t.Fatalf("nested-include explicit false lost through the file loader: %#v", user.Requires)
		return
	}

	localPath := write("config.local.yaml", "apiVersion: agent-marketplace/v4\nrequires: {expand: false}\n")
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: orgPath,
		GlobalPath:       userPath,
		RepoPath:         repoPath,
		LocalPath:        localPath,
	})
	if len(ec.Errors) != 0 {
		t.Fatalf("LoadEffective errors: %v", ec.Errors)
	}
	layers := ec.LayerResults()
	if len(layers) != 4 {
		t.Fatalf("LayerResults length = %d, want 4", len(layers))
	}
	wantExpand := map[Source]bool{Organization: true, UserPersonal: false, RepoCommitted: true, RepoPersonal: false}
	for _, layer := range layers {
		cfg := layer.Config
		if cfg == nil && layer.Local != nil {
			cfg = &Config{Requires: layer.Local.Requires}
		}
		if cfg == nil || cfg.Requires == nil || cfg.Requires.Expand == nil || *cfg.Requires.Expand != wantExpand[layer.Source] {
			t.Fatalf("layer %s requires = %#v, want expand %v", layer.Source, cfg, wantExpand[layer.Source])
			return
		}
	}
	if ec.Merged.RequiresExpansionEnabled() {
		t.Fatal("personal-local explicit false must override repo true as the highest-precedence layer")
	}
}

// spec: configuration-settings/settings-with-an-unreadable-acquisition-lock
func TestRequiresExpandSettingsReadDoesNotNeedAcquisitionLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: agent-marketplace/v4
requires: {expand: true}
sources:
  hosted: {marketplace: https://market.example}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath, err := lockfile.PathForConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("not a lock document: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := loadSettings(path, userLayer)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Requires == nil || settings.Requires.Expand == nil || !*settings.Requires.Expand {
		t.Fatalf("settings must expose requires.expand without touching acquisition locks: %#v", settings.Requires)
		return
	}
	if _, err := LoadConfigFromPath(path); err == nil {
		t.Fatal("full layer load must refuse a Marketplace source whose lock is unreadable")
	}
}

// spec: configuration-settings/migration-does-not-add-the-expansion-setting
func TestMigrationDoesNotSeedRequiresExpand(t *testing.T) {
	for _, version := range []string{"v2", "v3"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			user := filepath.Join(dir, "user.yaml")
			if err := os.WriteFile(user, []byte(`apiVersion: agent-marketplace/`+version+`
harnesses: [claude]
registries:
  - url: https://example.com/repo.git
    ref: main
    skills: {include: [alpha]}
`), 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := MigrateConfigFilesToV4(MigratePaths{User: user}, V4MigrationUser, false)
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.UserMigrated {
				t.Fatal("expected the user config to migrate")
				return
			}
			data, err := os.ReadFile(user)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "requires") {
				t.Fatalf("migrated v4 document must not seed requires.expand:\n%s", data)
			}
			migrated, err := LoadConfigFromPath(user)
			if err != nil {
				t.Fatalf("migrated %s config must still load: %v", version, err)
			}
			if migrated.RequiresExpansionEnabled() {
				t.Fatal("migrated config must report expansion disabled")
			}
		})
	}
}
