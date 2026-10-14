package config

import (
	"reflect"
	"testing"
)

func TestMergeAppliesPerEntryOverridesAndInheritance(t *testing.T) {
	global := &Config{
		Tags: []string{"security"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:global-inherit.git", Ref: "main", Skills: KindSelector{Include: []string{"no-curl-bash"}, Exclude: []string{"legacy-security"}}, Tags: []string{"security"}},
			{
				URL:      "git@example.com:global-override.git",
				Ref:      "main",
				Skills:   KindSelector{Include: []string{"global-explicit"}, Exclude: []string{"skip-global-explicit"}},
				Tags:     []string{"compliance"},
				Required: true,
			},
		},
	}

	repo := &Config{
		Tags: []string{"k8s"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:repo-inherit.git", Ref: "stable", Skills: KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"firewall-rule-lifecycle"}}, Tags: []string{"k8s"}},
			{
				URL:    "git@example.com:repo-override.git",
				Ref:    "stable",
				Skills: KindSelector{Include: []string{"repo-explicit"}, Exclude: []string{"skip-repo-explicit"}},
				Tags:   []string{"network"},
			},
		},
	}

	got := Merge(global, repo)
	want := &Config{
		Registries: []RegistryEntry{
			{
				URL:      "git@example.com:global-inherit.git",
				Ref:      "main",
				Skills:   KindSelector{Include: []string{"no-curl-bash"}, Exclude: []string{"legacy-security"}},
				Tags:     []string{"security"},
				Required: false,
				Source:   UserPersonal,
			},
			{
				URL:      "git@example.com:global-override.git",
				Ref:      "main",
				Skills:   KindSelector{Include: []string{"global-explicit"}, Exclude: []string{"skip-global-explicit"}},
				Tags:     []string{"compliance"},
				Required: true,
				Source:   UserPersonal,
			},
			{
				URL:      "git@example.com:repo-inherit.git",
				Ref:      "stable",
				Skills:   KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"firewall-rule-lifecycle"}},
				Tags:     []string{"k8s"},
				Required: false,
				Source:   RepoCommitted,
			},
			{
				URL:      "git@example.com:repo-override.git",
				Ref:      "stable",
				Skills:   KindSelector{Include: []string{"repo-explicit"}, Exclude: []string{"skip-repo-explicit"}},
				Tags:     []string{"network"},
				Required: false,
				Source:   RepoCommitted,
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected merged config %#v, got %#v", want, got)
	}
}

func TestMergeRequiredRegistriesIgnoreRepoExcludes(t *testing.T) {
	global := &Config{
		Registries: []RegistryEntry{{
			URL:      "git@example.com:security.git",
			Ref:      "main",
			Required: true,
			Skills:   KindSelector{Include: []string{"no-curl-bash"}},
		}},
	}

	repo := &Config{
		Registries: []RegistryEntry{{
			URL: "git@example.com:infra.git",
			Ref: "main",
		}},
	}

	got := Merge(global, repo)
	required := got.Registries[0]

	if !required.Required {
		t.Fatalf("expected first merged entry to remain required")
	}

	if !reflect.DeepEqual(required.Skills.Include, []string{"no-curl-bash"}) {
		t.Fatalf("expected required registry to keep global skills, got %#v", required.Skills.Include)
	}

	if len(required.Skills.Exclude) != 0 {
		t.Fatalf("expected repo excludes not to affect required registry, got %#v", required.Skills.Exclude)
	}
}

func TestMergePreservesExplicitEmptyPerEntryHarnessesOverride(t *testing.T) {
	repo := &Config{
		Harnesses: []string{"factory"},
		Registries: []RegistryEntry{
			{
				URL:       "git@example.com:repo.git",
				Ref:       "main",
				Harnesses: []string{},
			},
		},
	}

	got := Merge(nil, repo)
	if len(got.Registries) != 1 {
		t.Fatalf("expected 1 merged registry entry, got %d", len(got.Registries))
	}
	if got.Registries[0].Harnesses == nil {
		t.Fatalf("expected explicit empty per-entry harnesses override to remain non-nil")
	}
	if len(got.Registries[0].Harnesses) != 0 {
		t.Fatalf("expected explicit empty per-entry harnesses override to stay empty, got %v", got.Registries[0].Harnesses)
	}
}

func TestMergeRepoNilHarnessesInheritsFromGlobal(t *testing.T) {
	global := &Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory", "windsurf"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:org.git", Ref: "main"},
		},
	}
	repo := &Config{
		APIVersion: APIVersionV4,
		// Harnesses intentionally nil — project config inherits from global.
		Registries: []RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main"},
		},
	}

	got := Merge(global, repo)
	if !equalStrings(got.Harnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("expected nil repo Harnesses to inherit from global [factory, windsurf], got %v", got.Harnesses)
	}
}

func TestMergeRepoWithHarnessesOverridesGlobal(t *testing.T) {
	global := &Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory", "windsurf"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:org.git", Ref: "main"},
		},
	}
	repo := &Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:repo.git", Ref: "main"},
		},
	}

	got := Merge(global, repo)
	if !equalStrings(got.Harnesses, []string{"factory"}) {
		t.Fatalf("expected non-nil repo Harnesses to override global, got %v", got.Harnesses)
	}
}

func TestHasMatchingTag(t *testing.T) {
	tests := []struct {
		name       string
		skillTags  []string
		wantedTags []string
		want       bool
	}{
		{"match", []string{"k8s", "helm"}, []string{"k8s"}, true},
		{"no match", []string{"security"}, []string{"k8s"}, false},
		{"empty skill tags", nil, []string{"k8s"}, false},
		{"empty wanted tags", []string{"k8s"}, nil, false},
		{"both empty", nil, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HasMatchingTag(tc.skillTags, tc.wantedTags)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestRegistrySelectionKey(t *testing.T) {
	tests := []struct {
		name  string
		entry RegistryEntry
		index int
		want  string
	}{
		{
			name:  "git registry includes url, ref and index",
			entry: RegistryEntry{URL: "git@example.com:repo.git", Ref: "main"},
			index: 0,
			want:  "git@example.com:repo.git@main#0",
		},
		{
			name:  "same git registry with different index produces distinct keys",
			entry: RegistryEntry{URL: "git@example.com:repo.git", Ref: "main"},
			index: 3,
			want:  "git@example.com:repo.git@main#3",
		},
		{
			name:  "local registry omits ref",
			entry: RegistryEntry{URL: "/local/skills"},
			index: 0,
			want:  "/local/skills#0",
		},
		{
			name:  "relative local registry omits ref",
			entry: RegistryEntry{URL: "./skills"},
			index: 2,
			want:  "./skills#2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RegistrySelectionKey(tc.entry, tc.index); got != tc.want {
				t.Fatalf("RegistrySelectionKey() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@host:repo.git", "git@host:repo"},
		{"git@host:repo", "git@host:repo"},
		{"git@host:repo.git/", "git@host:repo"},
		{"https://host/repo.git", "https://host/repo"},
		{"https://host/repo", "https://host/repo"},
		{"https://host/repo/", "https://host/repo"},
		{"file:///tmp/repo.git", "/tmp/repo"},
		{"file:///tmp/repo", "/tmp/repo"},
		{"FILE:///tmp/repo.git", "/tmp/repo"},
		{"file://host/tmp/repo.git", "file://host/tmp/repo"},
	}
	for _, tc := range tests {
		got := NormalizeGitURL(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ptrStringSlice returns a pointer to the given slice, for constructing
// *[]string Config fields in tests.
func ptrStringSlice(s ...string) *[]string { return &s }

func TestMergeLayers_OrgAllowlistNotOverridableByUserOrRepo(t *testing.T) {
	orgPolicy := ptrStringSlice(`(^|\.)example\.com/`)

	layers := []Layer{
		{Source: Organization, Config: &Config{
			AllowedRegistries: orgPolicy,
			Registries:        []RegistryEntry{{URL: "gitlab.example.com/team/repo.git", Ref: "main"}},
		}},
		{Source: UserPersonal, Config: &Config{
			AllowedRegistries: ptrStringSlice(), // user tries to allow all
			Registries:        []RegistryEntry{{URL: "github.com/evil/repo.git", Ref: "main"}},
		}},
		{Source: RepoCommitted, Config: &Config{
			AllowedRegistries: ptrStringSlice(`(^|\.)github\.com/`), // repo tries to add github
			Registries:        []RegistryEntry{{URL: "github.com/team/repo.git", Ref: "main"}},
		}},
	}

	merged := MergeLayers(layers)
	if !reflect.DeepEqual(merged.AllowedRegistries, orgPolicy) {
		t.Fatalf("org AllowedRegistries should not be overridden by user/repo, got %v", merged.AllowedRegistries)
	}
}

func TestMergeLayers_NoOrgPolicy_UserCanSetAllowlist(t *testing.T) {
	userPolicy := ptrStringSlice(`(^|\.)github\.com/`, `(^|\.)example\.com/`)

	layers := []Layer{
		{Source: UserPersonal, Config: &Config{
			AllowedRegistries: userPolicy,
			Registries:        []RegistryEntry{{URL: "github.com/team/repo.git", Ref: "main"}},
		}},
		{Source: RepoCommitted, Config: &Config{
			Registries: []RegistryEntry{{URL: "gitlab.example.com/team/repo.git", Ref: "main"}},
		}},
	}

	merged := MergeLayers(layers)
	if !reflect.DeepEqual(merged.AllowedRegistries, userPolicy) {
		t.Fatalf("without org policy, user AllowedRegistries should apply, got %v", merged.AllowedRegistries)
	}
}

func TestMergeLayers_FirstNonNilWins_UserAllowlistNotOverridableByRepo(t *testing.T) {
	userPolicy := ptrStringSlice(`(^|\.)example\.com/`)
	repoPolicy := ptrStringSlice(`(^|\.)github\.com/`, `(^|\.)example\.com/`)

	layers := []Layer{
		{Source: UserPersonal, Config: &Config{
			AllowedRegistries: userPolicy,
			Registries:        []RegistryEntry{{URL: "gitlab.example.com/team/repo.git", Ref: "main"}},
		}},
		{Source: RepoCommitted, Config: &Config{
			AllowedRegistries: repoPolicy,
			Registries:        []RegistryEntry{{URL: "github.com/team/repo.git", Ref: "main"}},
		}},
	}

	merged := MergeLayers(layers)
	// First-non-nil wins: user layer set it first, repo cannot override.
	if !reflect.DeepEqual(merged.AllowedRegistries, userPolicy) {
		t.Fatalf("first-non-nil should win; repo cannot override user allowlist, got %v", merged.AllowedRegistries)
	}
}

func TestMergeLayers_OrgPolicySet_UserAndRepoIgnored(t *testing.T) {
	orgPolicy := ptrStringSlice(`(^|\.)example\.com/`)

	// Even if user and repo both try to set their own allowlists,
	// only the org one should survive (first-non-nil wins).
	layers := []Layer{
		{Source: Organization, Config: &Config{
			AllowedRegistries: orgPolicy,
			Registries:        []RegistryEntry{{URL: "gitlab.example.com/team/repo.git", Ref: "main"}},
		}},
		{Source: UserPersonal, Config: &Config{
			AllowedRegistries: ptrStringSlice(`.*`), // allow everything
			Registries:        []RegistryEntry{{URL: "github.com/evil/repo.git", Ref: "main"}},
		}},
		{Source: RepoCommitted, Config: &Config{
			AllowedRegistries: ptrStringSlice(`(^|\.)github\.com/`, `(^|\.)example\.com/`),
			Registries:        []RegistryEntry{{URL: "github.com/team/repo.git", Ref: "main"}},
		}},
	}

	merged := MergeLayers(layers)
	if !reflect.DeepEqual(merged.AllowedRegistries, orgPolicy) {
		t.Fatalf("org AllowedRegistries must win over user/repo, got %v", merged.AllowedRegistries)
	}
}

func TestMergeLayers_ExplicitEmptyAllowlistClaimsField(t *testing.T) {
	// When no earlier layer sets AllowedRegistries, a layer that sets
	// an explicit empty slice (allowed_registries: []) is non-nil and
	// claims the field. ValidateRegistryURLs treats a non-nil empty
	// allowlist as deny-all for non-local URLs.
	layers := []Layer{
		{Source: UserPersonal, Config: &Config{
			AllowedRegistries: ptrStringSlice(), // explicitly empty
			Registries:        []RegistryEntry{{URL: "github.com/any/repo.git", Ref: "main"}},
		}},
	}

	merged := MergeLayers(layers)
	if merged.AllowedRegistries == nil {
		t.Fatal("explicit empty AllowedRegistries should be non-nil")
	}
	if len(*merged.AllowedRegistries) != 0 {
		t.Fatalf("explicit empty AllowedRegistries should have length 0, got %v", *merged.AllowedRegistries)
	}
}

func TestMergeLayers_BackupIgnoresRepoCommittedConfig(t *testing.T) {
	userEnabled := true
	repoEnabled := false
	userMaxDays := 7
	repoMaxDays := 1
	userMaxPerDay := 5
	repoMaxPerDay := 2

	merged := MergeLayers([]Layer{
		{Source: UserPersonal, Config: &Config{
			Backup: &BackupConfig{
				Enabled:   &userEnabled,
				MaxDays:   &userMaxDays,
				MaxPerDay: &userMaxPerDay,
			},
		}},
		{Source: RepoCommitted, Config: &Config{
			Backup: &BackupConfig{
				Enabled:   &repoEnabled,
				MaxDays:   &repoMaxDays,
				MaxPerDay: &repoMaxPerDay,
			},
		}},
	})

	if merged.Backup == nil {
		t.Fatal("expected user backup config to be preserved")
	}
	if merged.Backup.Enabled == nil || *merged.Backup.Enabled != userEnabled {
		t.Fatalf("expected user backup enabled to win, got %#v", merged.Backup.Enabled)
	}
	if merged.Backup.MaxDays == nil || *merged.Backup.MaxDays != userMaxDays {
		t.Fatalf("expected user backup max_days to win, got %#v", merged.Backup.MaxDays)
	}
	if merged.Backup.MaxPerDay == nil || *merged.Backup.MaxPerDay != userMaxPerDay {
		t.Fatalf("expected user backup max_per_day to win, got %#v", merged.Backup.MaxPerDay)
	}
}

func TestMergeLayers_LoggingIgnoresRepoScopedConfig(t *testing.T) {
	userEnabled := true
	repoEnabled := false
	localEnabled := false
	userLevel := "debug"
	repoLevel := "error"
	localLevel := "warn"
	userFile := "/tmp/user.log"
	repoFile := "/tmp/repo.log"
	localFile := "/tmp/local.log"

	merged := MergeLayers([]Layer{
		{Source: UserPersonal, Config: &Config{
			Logging: &LoggingConfig{
				Enabled: &userEnabled,
				Level:   &userLevel,
				File:    &userFile,
			},
		}},
		{Source: RepoCommitted, Config: &Config{
			Logging: &LoggingConfig{
				Enabled: &repoEnabled,
				Level:   &repoLevel,
				File:    &repoFile,
			},
		}},
		{Source: RepoPersonal, Config: &Config{
			Logging: &LoggingConfig{
				Enabled: &localEnabled,
				Level:   &localLevel,
				File:    &localFile,
			},
		}},
	})

	if merged.Logging == nil {
		t.Fatal("expected user logging config to be preserved")
	}
	if merged.Logging.Enabled == nil || *merged.Logging.Enabled != userEnabled {
		t.Fatalf("expected user logging enabled to win, got %#v", merged.Logging.Enabled)
	}
	if merged.Logging.Level == nil || *merged.Logging.Level != userLevel {
		t.Fatalf("expected user logging level to win, got %#v", merged.Logging.Level)
	}
	if merged.Logging.File == nil || *merged.Logging.File != userFile {
		t.Fatalf("expected user logging file to win, got %#v", merged.Logging.File)
	}
}

func TestValidateLoggingOnlyAppliesToUserConfig(t *testing.T) {
	invalidMaxSize := -1
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Logging: &LoggingConfig{
			MaxSizeMB: &invalidMaxSize,
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected user-level validation to reject invalid logging config")
	}
	if err := cfg.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("expected repo-level validation to ignore logging config, got %v", err)
	}
}

func TestLocalConfigAsLayer_NilInputReturnsNil(t *testing.T) {
	got := LocalConfigAsLayer(nil)
	if got != nil {
		t.Fatalf("expected nil for nil input, got %#v", got)
	}
}

func TestLocalConfigAsLayer_EmptyRegistriesReturnsNil(t *testing.T) {
	localCfg := &LocalConfig{Registries: []RegistryEntry{}}
	got := LocalConfigAsLayer(localCfg)
	if got != nil {
		t.Fatalf("expected nil for empty registries, got %#v", got)
	}
}

func TestLocalConfigAsLayer_WithRegistriesReturnsLayer(t *testing.T) {
	localCfg := &LocalConfig{
		Registries: []RegistryEntry{
			{URL: "git@example.com:local-registry.git", Ref: "main", Tags: []string{"k8s"}},
		},
		Skills:      KindSelector{Include: []string{"local-skill"}},
		Commands:    KindSelector{Exclude: []string{"local-cmd"}},
		Collections: KindSelector{Include: []string{"local-coll"}},
	}

	layer := LocalConfigAsLayer(localCfg)
	if layer == nil {
		t.Fatal("expected non-nil layer for populated LocalConfig")
	}
	if layer.Source != RepoPersonal {
		t.Fatalf("expected source %d, got %d", RepoPersonal, layer.Source)
	}
	if layer.Config == nil {
		t.Fatal("expected non-nil Config in layer")
	}

	// Verify APIVersion carries the v4 runtime sentinel.
	if layer.Config.APIVersion != "agent-marketplace/v4" {
		t.Fatalf("expected APIVersion %q, got %q", "agent-marketplace/v4", layer.Config.APIVersion)
	}

	// Verify registry entries are carried through.
	if len(layer.Config.Registries) != 1 {
		t.Fatalf("expected 1 registry entry, got %d", len(layer.Config.Registries))
	}
	if layer.Config.Registries[0].URL != "git@example.com:local-registry.git" {
		t.Fatalf("expected registry URL %q, got %q", "git@example.com:local-registry.git", layer.Config.Registries[0].URL)
	}

	// Verify top-level selectors are NOT carried into the layer Config.
	// They are applied globally by the resolver's localCfg path instead;
	// carrying them here would cause double-application via per-entry
	// inheritance (surprising) + global post-hoc filter (expected).
	if len(layer.Config.Skills.Include) != 0 {
		t.Fatalf("expected Skills.Include to be empty in layer Config, got %v", layer.Config.Skills.Include)
	}
	if len(layer.Config.Commands.Exclude) != 0 {
		t.Fatalf("expected Commands.Exclude to be empty in layer Config, got %v", layer.Config.Commands.Exclude)
	}
	if len(layer.Config.Collections.Include) != 0 {
		t.Fatalf("expected Collections.Include to be empty in layer Config, got %v", layer.Config.Collections.Include)
	}

	// Verify tags from registries are carried.
	if !equalStrings(layer.Config.Registries[0].Tags, []string{"k8s"}) {
		t.Fatalf("expected registry tags [k8s], got %v", layer.Config.Registries[0].Tags)
	}
}

func TestLocalConfigAsLayer_ConfigHasAPIVersionV4Sentinel(t *testing.T) {
	localCfg := &LocalConfig{
		Registries: []RegistryEntry{
			{URL: "/local/path"},
		},
	}

	layer := LocalConfigAsLayer(localCfg)
	if layer == nil || layer.Config == nil {
		t.Fatal("expected non-nil layer with Config")
	}
	const wantVersion = "agent-marketplace/v4"
	if layer.Config.APIVersion != wantVersion {
		t.Fatalf("expected Config.APIVersion %q, got %q", wantVersion, layer.Config.APIVersion)
	}
	// The synthetic runtime layer must satisfy the same exact-v4 gate as every
	// other bound layer; a historical sentinel would be refused here.
	if err := layer.Config.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("personal-local runtime layer must pass exact-v4 validation: %v", err)
	}
	// The lower layer declares no version, so the merged runtime version can
	// only come from the personal-local sentinel.
	merged := MergeLayers([]Layer{
		{Source: RepoCommitted, Config: &Config{}},
		*layer,
	})
	if merged.APIVersion != wantVersion {
		t.Fatalf("expected merged APIVersion %q, got %q", wantVersion, merged.APIVersion)
	}
}

func TestMergeLayers_BackupIgnoresRepoPersonalConfig(t *testing.T) {
	userEnabled := true
	localEnabled := false
	userMaxDays := 7
	localMaxDays := 2
	userMaxPerDay := 5
	localMaxPerDay := 1

	merged := MergeLayers([]Layer{
		{Source: UserPersonal, Config: &Config{
			Backup: &BackupConfig{
				Enabled:   &userEnabled,
				MaxDays:   &userMaxDays,
				MaxPerDay: &userMaxPerDay,
			},
		}},
		{Source: RepoPersonal, Config: &Config{
			Backup: &BackupConfig{
				Enabled:   &localEnabled,
				MaxDays:   &localMaxDays,
				MaxPerDay: &localMaxPerDay,
			},
		}},
	})

	if merged.Backup == nil {
		t.Fatal("expected user backup config to be preserved")
	}
	if merged.Backup.Enabled == nil || *merged.Backup.Enabled != userEnabled {
		t.Fatalf("expected user backup enabled to win, got %#v", merged.Backup.Enabled)
	}
	if merged.Backup.MaxDays == nil || *merged.Backup.MaxDays != userMaxDays {
		t.Fatalf("expected user backup max_days to win, got %#v", merged.Backup.MaxDays)
	}
	if merged.Backup.MaxPerDay == nil || *merged.Backup.MaxPerDay != userMaxPerDay {
		t.Fatalf("expected user backup max_per_day to win, got %#v", merged.Backup.MaxPerDay)
	}
}
