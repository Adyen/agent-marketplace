package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestLoadEffectiveCanceledContextDoesNotBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ec := loadEffectiveForTest(ctx, LoadEffectiveOptions{})
	if len(ec.Errors) != 1 || !errors.Is(ec.Errors[0], context.Canceled) {
		t.Fatalf("errors = %v, want one context.Canceled", ec.Errors)
	}
	if ec.Merged != nil {
		t.Fatalf("merged = %#v, want nil", ec.Merged)
	}
	for _, lr := range ec.LayerResults() {
		if lr.Config != nil || lr.Local != nil || len(lr.Errors) > 0 || len(lr.Reads) > 0 {
			t.Fatalf("layer result = %#v, want zero", lr)
		}
	}
}

func TestReadIntentRejectsMissingRepoRoot(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"org.yaml", "user.yaml", "repo.yaml", "local.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("apiVersion: agent-marketplace/v4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	intent := ReadIntent(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "org.yaml"),
		GlobalPath:       filepath.Join(dir, "user.yaml"),
		RepoPath:         filepath.Join(dir, "repo.yaml"),
		LocalPath:        filepath.Join(dir, "local.yaml"),
	})
	if len(intent.Errors) == 0 {
		t.Fatal("ReadIntent with an empty RepoRoot must return an intent error instead of discovering roots from the working directory")
	}
	for source, layer := range intent.Layers {
		if layer.Present() || len(layer.Reads) > 0 {
			t.Fatalf("layer %d read configuration without a repository root: %#v", source, layer)
		}
	}
}

func TestLayerIntentViews(t *testing.T) {
	dir := t.TempDir()
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
harnesses: [claude, devin]
allowed_registries: []
allowed_marketplaces: [https://mp-allowed.example]
sources:
  zdir: {directory: ./regs}
  agit: {git: https://example.com/repo.git, ref: main}
  mmp: {marketplace: https://market.example}
resources:
  agit: {all: true}
  mmp: {include: [main/collection/onboarding]}
  zdir: {all: true}
`), dir+"/config.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	li := LayerIntent{Source: UserPersonal, v4: v4}

	wantSources := []SourceDecl{
		{Kind: V4SourceGit, URL: "https://example.com/repo.git", Ref: "main"},
		{Kind: V4SourceMarketplace, URL: "https://market.example"},
		{Kind: V4SourceDirectory},
	}
	if got := li.Sources(); !reflect.DeepEqual(got, wantSources) {
		t.Fatalf("sources = %#v, want %#v", got, wantSources)
	}
	if !li.NeedsLock() {
		t.Fatal("v4 layer with git and acquiring marketplace sources must need a lock")
	}
	if !li.NeedsMarketplaceLock() {
		t.Fatal("v4 layer with an acquiring marketplace source must need a marketplace lock")
	}

	settings := li.Settings()
	if settings.AllowedRegistries == nil || len(*settings.AllowedRegistries) != 0 {
		t.Fatalf("allowed registries = %#v, want explicit empty", settings.AllowedRegistries)
	}
	if settings.AllowedMarketplaces == nil || !reflect.DeepEqual(*settings.AllowedMarketplaces, []string{"https://mp-allowed.example"}) {
		t.Fatalf("allowed marketplaces = %#v", settings.AllowedMarketplaces)
	}
	(*settings.AllowedMarketplaces)[0] = "mutated"
	if (*v4.AllowedMarketplaces)[0] != "https://mp-allowed.example" {
		t.Fatal("Settings must clone allowlists")
	}
	settings.Harnesses[0] = "mutated"
	if v4.Harnesses[0] != "claude" {
		t.Fatal("Settings must clone harnesses")
	}

	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		_, err := readLayerIntent(dir+"/config.yaml", "", []byte("apiVersion: "+version+"\nregistries: []\n"), userLayer, nil)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != dir+"/config.yaml" || validation.Line != 1 {
			t.Fatalf("legacy intent %s = %v, want path-specific v4.api-version", version, err)
		}
	}
	absent := LayerIntent{Source: UserPersonal}
	if absent.Present() || absent.NeedsLock() || absent.NeedsMarketplaceLock() || absent.Sources() != nil {
		t.Fatal("absent intent exposed acquisition authority")
	}
}

func TestIntentAllowedMarketplacesPreservesEmptyPolicyAndOrganizationPrecedence(t *testing.T) {
	for _, root := range []struct {
		name       string
		apiVersion string
	}{
		{name: "v4", apiVersion: APIVersionV4},
	} {
		t.Run(root.name, func(t *testing.T) {
			layerFor := func(source Source, settings string) LayerIntent {
				t.Helper()
				kind := userLayer
				if source == Organization {
					kind = organizationLayer
				}
				path := filepath.Join(t.TempDir(), "config.yaml")
				data := []byte("apiVersion: " + root.apiVersion + "\nharnesses: [devin]\n" + settings)
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				layer := loadLayerIntent(source, path, kind, nil)
				if err := layer.Err(); err != nil {
					t.Fatal(err)
				}
				return layer
			}

			absent := Intent{Layers: [5]LayerIntent{
				UserPersonal: layerFor(UserPersonal, ""),
			}}
			if got := absent.AllowedRegistries(); got != nil {
				t.Fatalf("absent registry allowlist = %#v, want nil", got)
			}
			if got := absent.AllowedMarketplaces(); got != nil {
				t.Fatalf("absent Marketplace allowlist = %#v, want nil", got)
			}

			empty := Intent{Layers: [5]LayerIntent{
				UserPersonal: layerFor(UserPersonal, "allowed_registries: []\nallowed_marketplaces: []\n"),
			}}
			if got := empty.AllowedRegistries(); got == nil || len(got) != 0 {
				t.Fatalf("explicit empty registry allowlist = %#v, want nonnil empty", got)
			}
			if got := empty.AllowedMarketplaces(); got == nil || len(got) != 0 {
				t.Fatalf("explicit empty Marketplace allowlist = %#v, want nonnil empty", got)
			}

			const userRegistryPattern = `^https://registry\.user\.example/`
			const userPattern = `^https://user\.example$`
			userLayer := layerFor(UserPersonal, "allowed_registries: ['"+userRegistryPattern+"']\nallowed_marketplaces: ['"+userPattern+"']\n")
			user := Intent{Layers: [5]LayerIntent{UserPersonal: userLayer}}
			userRegistries := user.AllowedRegistries()
			if !reflect.DeepEqual(userRegistries, []string{userRegistryPattern}) {
				t.Fatalf("user registry allowlist = %#v, want [%q]", userRegistries, userRegistryPattern)
			}
			userAllowed := user.AllowedMarketplaces()
			if !reflect.DeepEqual(userAllowed, []string{userPattern}) {
				t.Fatalf("user Marketplace allowlist = %#v, want [%q]", userAllowed, userPattern)
			}
			if err := ValidateMarketplaceURLs([]MarketplaceEntry{{URL: "https://user.example"}}, userAllowed); err != nil {
				t.Fatalf("user Marketplace allowlist should permit its matching endpoint: %v", err)
			}

			organization := layerFor(Organization, "allowed_registries: []\nallowed_marketplaces: []\n")
			combined := Intent{Layers: [5]LayerIntent{
				Organization: organization,
				UserPersonal: userLayer,
			}}
			if got := combined.AllowedRegistries(); got == nil || len(got) != 0 {
				t.Fatalf("organization empty registry allowlist = %#v, want nonnil empty despite user pattern", got)
			}
			if got := combined.AllowedMarketplaces(); got == nil || len(got) != 0 {
				t.Fatalf("organization empty Marketplace allowlist = %#v, want nonnil empty despite user pattern", got)
			}
		})
	}
}

func readV4PolicyIntentFixture(t *testing.T, organizationRoot, organizationFragment, userRoot string) (Intent, EffectiveConfig) {
	t.Helper()
	organizationDir := t.TempDir()
	organizationPath := filepath.Join(organizationDir, "config.yaml")
	organizationFragmentPath := filepath.Join(organizationDir, "policy.yaml")
	userPath := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	repoRoot := t.TempDir()
	layerEditorWrite(t, organizationPath, organizationRoot)
	layerEditorWrite(t, userPath, userRoot)
	files := map[string]string{organizationPath: organizationRoot, userPath: userRoot}
	if organizationFragment != "" {
		layerEditorWrite(t, organizationFragmentPath, organizationFragment)
		files[organizationFragmentPath] = organizationFragment
	}
	intent := ReadIntent(context.Background(), LoadEffectiveOptions{
		RepoRoot:         repoRoot,
		LocalConfigDir:   repoRoot,
		OrganizationPath: organizationPath,
		GlobalPath:       userPath,
		RepoPath:         ProjectConfigPath(repoRoot),
		LocalPath:        LocalConfigPath(repoRoot),
	})
	if len(intent.Errors) != 0 {
		t.Fatalf("read real v4 policy fixture: %v", intent.Errors)
	}
	effective := Bind(intent, Evidence{})
	if len(effective.Errors) != 0 || effective.Merged == nil {
		t.Fatalf("bind real v4 policy fixture: errors=%v merged=%#v", effective.Errors, effective.Merged)
	}
	for path, want := range files {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Fatalf("policy composition changed %s: %q (err=%v)", path, got, err)
		}
	}
	return intent, effective
}

func assertPolicySlice(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", name, got, want)
	}
}

func assertPolicyPointer(t *testing.T, name string, got *[]string, want []string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s = %#v, want nil", name, got)
		}
		return
	}
	if got == nil || *got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("%s = %#v, want pointer to %#v", name, got, want)
	}
}

// spec: config-layering/allowed-marketplace-patterns-follow-include-authority-in-a-v4-root
// spec: config-layering/marketplace-url-allowlist
func TestReadIntentAndBindPreserveRealV4SourcePolicyComposition(t *testing.T) {
	userPatterns := `allowed_registries: ['registry-first', 'registry-second']
allowed_marketplaces: ['^https://user-marketplace\\.example$', '^https://secondary-marketplace\\.example$']
`
	userRoot := "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nlogging: {level: info}\n"
	userWithPatterns := userRoot + userPatterns
	registryPatterns := []string{"registry-first", "registry-second"}
	marketplacePatterns := []string{`^https://user-marketplace\\.example$`, `^https://secondary-marketplace\\.example$`}
	organizationRoot := "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nlogging: {level: debug, max_age_days: 5}\n"
	organizationRootPatterns := "allowed_registries: ['.*']\nallowed_marketplaces: ['.*']\n"
	organizationEmpty := "allowed_registries: []\nallowed_marketplaces: []\n"
	cases := []struct {
		name                     string
		organizationRoot         string
		organizationFragment     string
		userRoot                 string
		organizationRegistries   []string
		organizationMarketplaces []string
		userRegistries           []string
		userMarketplaces         []string
		effectiveRegistries      []string
		effectiveMarketplaces    []string
		includedPolicyIsRootOnly bool
	}{
		{
			name:                     "absent remains nil",
			organizationRoot:         organizationRoot,
			userRoot:                 userRoot,
			organizationRegistries:   nil,
			organizationMarketplaces: nil,
			userRegistries:           nil,
			userMarketplaces:         nil,
			effectiveRegistries:      nil,
			effectiveMarketplaces:    nil,
		},
		{
			name:                     "explicit user empty remains nonnil",
			organizationRoot:         organizationRoot,
			userRoot:                 userRoot + organizationEmpty,
			organizationRegistries:   nil,
			organizationMarketplaces: nil,
			userRegistries:           []string{},
			userMarketplaces:         []string{},
			effectiveRegistries:      []string{},
			effectiveMarketplaces:    []string{},
		},
		{
			name:                     "nonempty user order remains unchanged",
			organizationRoot:         organizationRoot,
			userRoot:                 userWithPatterns,
			organizationRegistries:   nil,
			organizationMarketplaces: nil,
			userRegistries:           registryPatterns,
			userMarketplaces:         marketplacePatterns,
			effectiveRegistries:      registryPatterns,
			effectiveMarketplaces:    marketplacePatterns,
		},
		{
			name:                     "organization root empty beats permissive user",
			organizationRoot:         organizationRoot + organizationEmpty,
			userRoot:                 userWithPatterns,
			organizationRegistries:   []string{},
			organizationMarketplaces: []string{},
			userRegistries:           registryPatterns,
			userMarketplaces:         marketplacePatterns,
			effectiveRegistries:      []string{},
			effectiveMarketplaces:    []string{},
		},
		{
			name:                     "included-only empty beats permissive user",
			organizationRoot:         "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\nharnesses: [claude]\nlogging: {max_age_days: 5}\n",
			organizationFragment:     organizationEmpty + "logging: {level: debug}\n",
			userRoot:                 userWithPatterns,
			organizationRegistries:   []string{},
			organizationMarketplaces: []string{},
			userRegistries:           registryPatterns,
			userMarketplaces:         marketplacePatterns,
			effectiveRegistries:      []string{},
			effectiveMarketplaces:    []string{},
			includedPolicyIsRootOnly: true,
		},
		{
			name:                     "included empty beats containing root and user patterns",
			organizationRoot:         "apiVersion: agent-marketplace/v4\ninclude: policy.yaml\nharnesses: [claude]\nlogging: {max_age_days: 5}\n" + organizationRootPatterns,
			organizationFragment:     organizationEmpty + "logging: {level: debug}\n",
			userRoot:                 userWithPatterns,
			organizationRegistries:   []string{},
			organizationMarketplaces: []string{},
			userRegistries:           registryPatterns,
			userMarketplaces:         marketplacePatterns,
			effectiveRegistries:      []string{},
			effectiveMarketplaces:    []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.includedPolicyIsRootOnly && (strings.Contains(tc.organizationRoot, "allowed_registries:") || strings.Contains(tc.organizationRoot, "allowed_marketplaces:")) {
				t.Fatal("included-only policy fixture redundantly repeats an allowlist in the root")
			}
			intent, effective := readV4PolicyIntentFixture(t, tc.organizationRoot, tc.organizationFragment, tc.userRoot)
			organizationSettings := intent.Layers[Organization].Settings()
			userSettings := intent.Layers[UserPersonal].Settings()
			assertPolicyPointer(t, "organization settings allowed_registries", organizationSettings.AllowedRegistries, tc.organizationRegistries)
			assertPolicyPointer(t, "organization settings allowed_marketplaces", organizationSettings.AllowedMarketplaces, tc.organizationMarketplaces)
			assertPolicyPointer(t, "user settings allowed_registries", userSettings.AllowedRegistries, tc.userRegistries)
			assertPolicyPointer(t, "user settings allowed_marketplaces", userSettings.AllowedMarketplaces, tc.userMarketplaces)
			assertPolicySlice(t, "Intent.AllowedRegistries", intent.AllowedRegistries(), tc.effectiveRegistries)
			assertPolicySlice(t, "Intent.AllowedMarketplaces", intent.AllowedMarketplaces(), tc.effectiveMarketplaces)
			assertPolicyPointer(t, "bound config allowed_registries", effective.Merged.AllowedRegistries, tc.effectiveRegistries)
			assertPolicyPointer(t, "bound config allowed_marketplaces", effective.Merged.AllowedMarketplaces, tc.effectiveMarketplaces)
			assertPolicySlice(t, "bound registry allowlist", effective.Allowed, tc.effectiveRegistries)
			assertPolicySlice(t, "bound Marketplace allowlist", effective.AllowedMarketplaces, tc.effectiveMarketplaces)
			if !reflect.DeepEqual(organizationSettings.Harnesses, []string{"claude"}) || organizationSettings.Logging == nil || organizationSettings.Logging.Level == nil || *organizationSettings.Logging.Level != "debug" || organizationSettings.Logging.MaxAgeDays == nil || *organizationSettings.Logging.MaxAgeDays != 5 {
				t.Fatalf("organization settings outside policies changed: %#v", organizationSettings)
			}
			if !reflect.DeepEqual(userSettings.Harnesses, []string{"devin"}) || userSettings.Logging == nil || userSettings.Logging.Level == nil || *userSettings.Logging.Level != "info" {
				t.Fatalf("user settings outside policies changed: %#v", userSettings)
			}
		})
	}
}

func TestLayerIntentDescribeSources(t *testing.T) {
	dir := t.TempDir()
	includePath := filepath.Join(dir, "fragment.yaml")
	rootPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(includePath, []byte(`apiVersion: agent-marketplace/v4
sources:
  frag: {directory: ./frag-regs}
resources:
  frag: {all: true}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	rootBytes := []byte(`apiVersion: agent-marketplace/v4
include: [fragment.yaml]
harnesses: [devin]
sources:
  zgit: {git: https://example.com/repo.git, ref: main, path: sub/dir, source_harness: claude}
  adir: {directory: ./regs, source_harness: windsurf}
  mmp: {marketplace: https://market.example}
resources:
  zgit: {all: true}
  adir: {all: true}
  mmp: {include: [main/collection/onboarding]}
`)
	if err := os.WriteFile(rootPath, rootBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalConfigPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(rootPath, canonical, rootBytes, userLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !li.NeedsLock() {
		t.Fatal("fixture must include git and acquiring marketplace sources")
	}
	base := filepath.Dir(canonical)
	want := []SourceDescription{
		{Kind: V4SourceDirectory, Location: filepath.Join(base, "regs"), SourceHarness: "windsurf"},
		{Kind: V4SourceDirectory, Location: filepath.Join(base, "frag-regs")},
		{Kind: V4SourceMarketplace, Location: "https://market.example"},
		{Kind: V4SourceGit, Location: "https://example.com/repo.git", Ref: "main", Path: "sub/dir", SourceHarness: "claude"},
	}
	if got := li.DescribeSources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("v4 described sources = %#v, want %#v", got, want)
	}

	legacyPath := filepath.Join(dir, "legacy.yaml")
	legacyBytes := []byte(`apiVersion: agent-marketplace/v3
harnesses: [devin]
registries:
  - url: https://example.com/legacy.git
    ref: v1
    path: sub/dir
    source_harness: claude
  - path: ` + filepath.Join(dir, "local-regs") + `
    source_harness: windsurf
marketplaces:
  - url: https://mp.example
    instance_id: main
`)
	if err := os.WriteFile(legacyPath, legacyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ll := loadLayerIntent(UserPersonal, legacyPath, userLayer, nil)
	if err := ll.Err(); err != nil {
		t.Fatal(err)
	}
	wantLegacy := []SourceDescription{
		{Kind: V4SourceGit, Location: "https://example.com/legacy.git", Ref: "v1", Path: "sub/dir", SourceHarness: "claude"},
		{Kind: V4SourceDirectory, Location: filepath.Join(dir, "local-regs"), SourceHarness: "windsurf"},
		{Kind: V4SourceMarketplace, Location: "https://mp.example", InstanceID: "main"},
	}
	if got := ll.DescribeSources(); !reflect.DeepEqual(got, wantLegacy) {
		t.Fatalf("legacy described sources = %#v, want %#v", got, wantLegacy)
	}

	if got := (LayerIntent{Source: UserPersonal}).DescribeSources(); got != nil {
		t.Fatalf("absent layer described sources = %#v, want nil", got)
	}
}

func TestLayerIntentExplicitSelections(t *testing.T) {
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  local: {directory: ./regs}
  hosted: {marketplace: https://market.example}
resources:
  local: {include: [skill/alpha, subagent/beta]}
  hosted: {include: [tools/skill/review]}
`), filepath.Join(t.TempDir(), "config.yaml"), true)
	if err != nil {
		t.Fatal(err)
	}
	li := LayerIntent{Source: RepoCommitted, v4: v4}
	want := []ExplicitSelection{
		{Kind: ir.KindSkill, Name: "alpha"},
		{Kind: ir.KindSubagent, Name: "beta"},
	}
	if got := li.ExplicitSelections(); !reflect.DeepEqual(got, want) {
		t.Fatalf("v4 explicit selections = %#v, want %#v", got, want)
	}

	gitConfig, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.com/a.git, ref: main}}
resources: {shared: [skill/one, mcp/srv]}
`), filepath.Join(t.TempDir(), "config.yaml"), true)
	if err != nil {
		t.Fatal(err)
	}
	gitIntent := LayerIntent{Source: RepoCommitted, v4: gitConfig}
	wantGit := []ExplicitSelection{
		{Kind: ir.KindMcpServer, Name: "srv"},
		{Kind: ir.KindSkill, Name: "one"},
	}
	if got := gitIntent.ExplicitSelections(); !reflect.DeepEqual(got, wantGit) {
		t.Fatalf("git explicit selections = %#v, want %#v", got, wantGit)
	}

	if got := (LayerIntent{Source: RepoCommitted}).ExplicitSelections(); got != nil {
		t.Fatalf("absent layer selections = %#v, want nil", got)
	}
}

func TestBindPreservesLayerErrorOrder(t *testing.T) {
	orgEvidenceErr := errors.New("organization evidence error")
	userReadErr := errors.New("user config read error")

	intent := Intent{
		Layers: [5]LayerIntent{
			Organization:  {Source: Organization, v4: &V4Config{}},
			UserPersonal:  {Source: UserPersonal, errors: []error{userReadErr}},
			RepoCommitted: {Source: RepoCommitted},
			RepoPersonal:  {Source: RepoPersonal},
		},
	}
	for _, source := range intentLayerOrder {
		intent.Errors = append(intent.Errors, intent.Layers[source].errors...)
	}

	ev := Evidence{}
	ev.errors[Organization] = []error{orgEvidenceErr}

	ec := Bind(intent, ev)

	want := append([]error{}, ec.Organization.Errors...)
	want = append(want, ec.User.Errors...)
	want = append(want, ec.Repo.Errors...)
	want = append(want, ec.LocalLayer.Errors...)
	if len(ec.Errors) != len(want) {
		t.Fatalf("errors = %v, want %v", ec.Errors, want)
	}
	for i := range want {
		if ec.Errors[i] != want[i] {
			t.Fatalf("errors[%d] = %v, want %v (full errors %v)", i, ec.Errors[i], want[i], ec.Errors)
		}
	}
	if len(ec.Errors) != 2 || ec.Errors[0] != orgEvidenceErr || ec.Errors[1] != userReadErr {
		t.Fatalf("errors = %v, want [organization evidence error, user read error]", ec.Errors)
	}
	counts := map[error]int{}
	for _, err := range ec.Errors {
		counts[err]++
	}
	if counts[orgEvidenceErr] != 1 || counts[userReadErr] != 1 {
		t.Fatalf("errors = %v, want each error exactly once", ec.Errors)
	}
}

func TestIntentViews(t *testing.T) {
	org := LayerIntent{Source: Organization, v4: &V4Config{Harnesses: []string{"cursor"}}}
	user := LayerIntent{Source: UserPersonal, v4: &V4Config{Harnesses: []string{"devin"}}}
	repo := LayerIntent{Source: RepoCommitted, v4: &V4Config{Harnesses: []string{"claude"}}}

	intent := Intent{Layers: [5]LayerIntent{Organization: org, UserPersonal: user, RepoCommitted: repo}}
	harnesses, source := intent.TargetHarnesses()
	if source != RepoCommitted || !reflect.DeepEqual(harnesses, []string{"claude"}) {
		t.Fatalf("target harnesses = %v from %v, want [claude] from repo", harnesses, source)
	}
	harnesses[0] = "mutated"
	if repo.v4.Harnesses[0] != "claude" {
		t.Fatal("TargetHarnesses must return a clone")
	}

	intent.Layers[RepoCommitted] = LayerIntent{Source: RepoCommitted}
	harnesses, source = intent.TargetHarnesses()
	if source != UserPersonal || !reflect.DeepEqual(harnesses, []string{"devin"}) {
		t.Fatalf("target harnesses = %v from %v, want [devin] from user", harnesses, source)
	}

	intent.Layers[UserPersonal] = LayerIntent{Source: UserPersonal}
	harnesses, source = intent.TargetHarnesses()
	if source != Organization || !reflect.DeepEqual(harnesses, []string{"cursor"}) {
		t.Fatalf("target harnesses = %v from %v, want [cursor] from organization", harnesses, source)
	}

	intent.Layers[Organization] = LayerIntent{Source: Organization}
	if harnesses, source := intent.TargetHarnesses(); harnesses != nil || source != SourceUnset {
		t.Fatalf("target harnesses = %v from %v, want unset", harnesses, source)
	}
}

func writeIntentV4Files(t *testing.T, dir string) (rootPath, includePath string, rootBytes, includeBytes []byte) {
	t.Helper()
	includePath = filepath.Join(dir, "fragment.yaml")
	rootPath = filepath.Join(dir, "config.yaml")
	includeBytes = []byte(`apiVersion: agent-marketplace/v4
sources:
  frag: {directory: ./frag-regs}
resources:
  frag: {all: true}
`)
	rootBytes = []byte(`apiVersion: agent-marketplace/v4
include: [fragment.yaml]
sources:
  main: {directory: ./regs}
resources:
  main: {all: true}
`)
	if err := os.WriteFile(includePath, includeBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rootPath, rootBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return rootPath, includePath, rootBytes, includeBytes
}

func TestReadLayerIntentV4CapturesReadsAtParse(t *testing.T) {
	dir := t.TempDir()
	rootPath, includePath, rootBytes, includeBytes := writeIntentV4Files(t, dir)
	rootCanonical, err := canonicalConfigPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	includeCanonical, err := canonicalConfigPath(includePath)
	if err != nil {
		t.Fatal(err)
	}

	includeReads := 0
	original := v4ReadConfigFile
	v4ReadConfigFile = func(path string) ([]byte, error) {
		if path == includeCanonical {
			includeReads++
			if includeReads > 1 {
				return nil, errors.New("v4 include reread after parse")
			}
		}
		return original(path)
	}
	t.Cleanup(func() { v4ReadConfigFile = original })

	li, err := readLayerIntent(rootPath, rootCanonical, rootBytes, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if includeReads != 1 {
		t.Fatalf("include read count = %d, want exactly 1", includeReads)
	}
	if len(li.Reads) != len(li.v4.Files) {
		t.Fatalf("reads = %#v not parallel to files = %#v", li.Reads, li.v4.Files)
	}
	for i := range li.v4.Files {
		if li.Reads[i].Path != li.v4.Files[i] {
			t.Fatalf("read %d path %q does not match file %q", i, li.Reads[i].Path, li.v4.Files[i])
		}
	}
	byPath := make(map[string]ConfigRead, len(li.Reads))
	for _, read := range li.Reads {
		byPath[read.Path] = read
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	// Includes are requested relative to the canonical root directory.
	includeAbs := filepath.Join(filepath.Dir(rootCanonical), "fragment.yaml")
	rootRead, ok := byPath[rootCanonical]
	if !ok {
		t.Fatalf("missing root read in %#v", li.Reads)
	}
	if rootRead.RequestedPath != rootAbs || rootRead.Digest != fmt.Sprintf("%x", sha256.Sum256(rootBytes)) || rootRead.Overlay {
		t.Fatalf("root read = %#v, want requested %q digest of parsed bytes without overlay", rootRead, rootAbs)
	}
	includeRead, ok := byPath[includeCanonical]
	if !ok {
		t.Fatalf("missing include read in %#v", li.Reads)
	}
	if includeRead.RequestedPath != includeAbs || includeRead.Digest != fmt.Sprintf("%x", sha256.Sum256(includeBytes)) || includeRead.Overlay {
		t.Fatalf("include read = %#v, want requested %q digest of parsed bytes without overlay", includeRead, includeAbs)
	}
}

func TestReadLayerIntentV4OverlayIncludeRead(t *testing.T) {
	dir := t.TempDir()
	rootPath, includePath, rootBytes, includeBytes := writeIntentV4Files(t, dir)
	rootCanonical, err := canonicalConfigPath(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	includeCanonical, err := canonicalConfigPath(includePath)
	if err != nil {
		t.Fatal(err)
	}

	diskReads := 0
	original := v4ReadConfigFile
	v4ReadConfigFile = func(path string) ([]byte, error) {
		diskReads++
		return original(path)
	}
	t.Cleanup(func() { v4ReadConfigFile = original })

	li, err := readLayerIntent(rootPath, rootCanonical, rootBytes, repoLayer, map[string][]byte{includeCanonical: includeBytes})
	if err != nil {
		t.Fatal(err)
	}
	if diskReads != 0 {
		t.Fatalf("overlaid include hit disk %d times", diskReads)
	}
	var includeRead *ConfigRead
	for i := range li.Reads {
		if li.Reads[i].Path == includeCanonical {
			includeRead = &li.Reads[i]
		}
	}
	if includeRead == nil || !includeRead.Overlay || includeRead.Digest != fmt.Sprintf("%x", sha256.Sum256(includeBytes)) {
		t.Fatalf("overlaid include read = %#v in %#v", includeRead, li.Reads)
	}
}

func TestReadLayerIntentV4KeepsCapturedCanonicalAfterRemove(t *testing.T) {
	dir := t.TempDir()
	rootPath, _, rootBytes, _ := writeIntentV4Files(t, dir)
	canonical, data, err := readEffectiveLayer(rootPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rootPath); err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(rootPath, canonical, data, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(li.Reads) == 0 || li.Reads[len(li.Reads)-1].Path != canonical {
		t.Fatalf("root read path = %#v, want captured canonical %q", li.Reads, canonical)
	}
	rootAbs, err := filepath.Abs(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	rootRead := li.Reads[len(li.Reads)-1]
	if rootRead.RequestedPath != rootAbs || rootRead.Digest != fmt.Sprintf("%x", sha256.Sum256(rootBytes)) {
		t.Fatalf("root read = %#v, want requested %q digest of captured bytes", rootRead, rootAbs)
	}
}

// spec: configuration-intent/captured-configuration-provenance
func TestReadLayerIntentV4SymlinkRetargetKeepsCapturedProvenance(t *testing.T) {
	dir := t.TempDir()
	targetA := filepath.Join(dir, "a")
	targetB := filepath.Join(dir, "b")
	if err := os.MkdirAll(targetA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetB, 0o700); err != nil {
		t.Fatal(err)
	}
	aBytes := []byte(`apiVersion: agent-marketplace/v4
sources:
  main: {directory: ./regs-a}
resources:
  main: {all: true}
`)
	bBytes := []byte(`apiVersion: agent-marketplace/v4
sources:
  main: {directory: ./regs-b}
resources:
  main: {all: true}
`)
	aPath := filepath.Join(targetA, "config.yaml")
	bPath := filepath.Join(targetB, "config.yaml")
	if err := os.WriteFile(aPath, aBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, bBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(aPath, link); err != nil {
		t.Fatal(err)
	}
	canonical, data, err := readEffectiveLayer(link, nil)
	if err != nil {
		t.Fatal(err)
	}
	aCanonical, err := canonicalConfigPath(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if canonical != aCanonical {
		t.Fatalf("captured canonical = %q, want %q", canonical, aCanonical)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bPath, link); err != nil {
		t.Fatal(err)
	}
	li, err := readLayerIntent(link, canonical, data, repoLayer, nil)
	if err != nil {
		t.Fatal(err)
	}
	linkAbs, err := filepath.Abs(link)
	if err != nil {
		t.Fatal(err)
	}
	if len(li.Reads) != 1 {
		t.Fatalf("reads = %#v, want exactly the root read", li.Reads)
	}
	read := li.Reads[0]
	if read.Path != aCanonical || read.RequestedPath != linkAbs || read.Digest != fmt.Sprintf("%x", sha256.Sum256(aBytes)) {
		t.Fatalf("read = %#v, want canonical %q requested %q digest of A bytes", read, aCanonical, linkAbs)
	}
	if len(li.v4.Sources) != 1 {
		t.Fatalf("sources = %#v, want A document sources", li.v4.Sources)
	}
	if src, ok := li.v4.Sources["main"]; !ok || src.File != aCanonical {
		t.Fatalf("parsed sources = %#v, want A bytes", li.v4.Sources)
	}
}

func TestOrganizationIntentReadsKeepDistinctRequestedAliases(t *testing.T) {
	writeOrgFixture := func(t *testing.T, root string) (fragmentPath string) {
		t.Helper()
		fragmentPath = filepath.Join(filepath.Dir(root), "fragment.yaml")
		layerEditorWrite(t, fragmentPath, "telemetry: {level: off}\n")
		layerEditorWrite(t, root, "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nharnesses: [claude]\n")
		return fragmentPath
	}
	organizationLayerReads := func(t *testing.T, orgPath string) []ConfigRead {
		t.Helper()
		intent := ReadIntent(context.Background(), LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       filepath.Join(t.TempDir(), "missing-global.yaml"),
			RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
			LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
		})
		if len(intent.Errors) != 0 {
			t.Fatalf("read organization fixture: %v", intent.Errors)
		}
		li := intent.Layers[Organization]
		if err := li.Err(); err != nil {
			t.Fatal(err)
		}
		if len(li.Reads) != 2 {
			t.Fatalf("organization reads = %#v, want root and fragment", li.Reads)
		}
		return li.Reads
	}
	assertDistinctAliases := func(t *testing.T, orgPath, fragmentPath string) {
		t.Helper()
		reads := organizationLayerReads(t, orgPath)
		rootCanonical, err := canonicalConfigPath(orgPath)
		if err != nil {
			t.Fatal(err)
		}
		fragmentCanonical, err := canonicalConfigPath(fragmentPath)
		if err != nil {
			t.Fatal(err)
		}
		orgAbs, err := filepath.Abs(orgPath)
		if err != nil {
			t.Fatal(err)
		}
		fragmentRequested, err := filepath.Abs(filepath.Join(filepath.Dir(orgPath), "fragment.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		byRequested := make(map[string]ConfigRead, len(reads))
		for _, read := range reads {
			if _, dup := byRequested[read.RequestedPath]; dup {
				t.Fatalf("requested alias %q shared by multiple reads %#v", read.RequestedPath, reads)
			}
			byRequested[read.RequestedPath] = read
		}
		rootRead, ok := byRequested[orgAbs]
		if !ok || rootRead.Path != rootCanonical {
			t.Fatalf("root read = %#v, want canonical %q requested %q in %#v", rootRead, rootCanonical, orgAbs, reads)
		}
		fragmentRead, ok := byRequested[fragmentRequested]
		if !ok || fragmentRead.Path != fragmentCanonical {
			t.Fatalf("fragment read = %#v, want canonical %q requested %q in %#v", fragmentRead, fragmentCanonical, fragmentRequested, reads)
		}
	}

	t.Run("v4 root include", func(t *testing.T) {
		orgPath := filepath.Join(t.TempDir(), "organization.yaml")
		assertDistinctAliases(t, orgPath, writeOrgFixture(t, orgPath))
	})
	t.Run("v4 root through logical path", func(t *testing.T) {
		orgPath := filepath.Join(t.TempDir(), "organization.yaml")
		writeOrgFixture(t, orgPath)
		orgAbs, err := filepath.Abs(orgPath)
		if err != nil {
			t.Fatal(err)
		}
		rootCanonical, err := canonicalConfigPath(orgPath)
		if err != nil {
			t.Fatal(err)
		}
		if rootCanonical == orgAbs {
			t.Skip("fixture path is already canonical; no logical alias to exercise")
		}
		assertDistinctAliases(t, orgPath, filepath.Join(filepath.Dir(orgPath), "fragment.yaml"))
		reads := organizationLayerReads(t, orgPath)
		for _, read := range reads {
			if read.Path == rootCanonical && read.RequestedPath != orgAbs {
				t.Fatalf("logical root alias lost: read = %#v, want requested %q", read, orgAbs)
			}
		}
	})
	t.Run("legacy root include", func(t *testing.T) {
		dir := t.TempDir()
		orgPath := filepath.Join(dir, "organization.yaml")
		fragmentPath := filepath.Join(dir, "fragment.yaml")
		layerEditorWrite(t, fragmentPath, "harnesses: [claude]\n")
		layerEditorWrite(t, orgPath, "apiVersion: agent-marketplace/v2\ninclude: fragment.yaml\nharnesses: [devin]\n")
		assertDistinctAliases(t, orgPath, fragmentPath)
	})
}
