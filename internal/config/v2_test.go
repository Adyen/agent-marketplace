package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// These tests retain runtime selection/target contracts formerly exercised with
// v2 fixtures. Authored documents now enter through the supported v4 parser.
func TestV4ConfigParsesSourceAndResourceFields(t *testing.T) {
	cfg := parseRuntimeV4(t, `
apiVersion: agent-marketplace/v4
harnesses: [factory, cursor]
sources:
  infra:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
    source_harness: claude
resources:
  infra:
    include: [skill/vm-lifecycle, command/onboard-vm]
    tags: [k8s]
    harnesses: [factory]
`)
	if cfg.APIVersion != APIVersionV4 || !equalStrings(cfg.Harnesses, []string{"factory", "cursor"}) || len(cfg.Registries) != 1 {
		t.Fatalf("unexpected runtime settings: %#v", cfg)
	}
	e := cfg.Registries[0]
	if e.URL != "git@gitlab.example.com:example-org/example-infra-skills.git" || e.Ref != "main" || e.SourceHarness != "claude" || !equalStrings(e.Skills.Include, []string{"vm-lifecycle"}) || !equalStrings(e.Commands.Include, []string{"onboard-vm"}) || !equalStrings(e.Harnesses, []string{"factory"}) || !equalStrings(e.Tags, []string{"k8s"}) {
		t.Fatalf("source/resource fields were not retained: %#v", e)
	}
	if !e.Subagents.IsZero() || !e.Hooks.IsZero() || !e.MCP.IsZero() {
		t.Fatalf("absent kinds must remain empty: %#v", e)
	}
}

func TestRuntimeConfigValidateRejectsMissingAPIVersion(t *testing.T) {
	cfg := Config{Harnesses: []string{"factory"}, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	var validation *V4ValidationError
	err := cfg.Validate()
	if !errors.As(err, &validation) || validation.Code != "v4.api-version-required" || validation.Path != "apiVersion" {
		t.Fatalf("missing version error = %v, want v4.api-version-required", err)
	}
	cfg.APIVersion = APIVersionV4
	if err := cfg.Validate(); err != nil {
		t.Fatalf("v4 control failed: %v", err)
	}
}

func TestRuntimeConfigValidateRejectsWrongAPIVersion(t *testing.T) {
	cfg := Config{APIVersion: "agent-marketplace/v1", Harnesses: []string{"factory"}, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	var validation *V4ValidationError
	err := cfg.Validate()
	if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != "apiVersion" {
		t.Fatalf("wrong version error = %v, want v4.api-version", err)
	}
}

func TestRuntimeConfigValidateRejectsMissingHarnesses(t *testing.T) {
	cfg := Config{APIVersion: APIVersionV4, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("missing harnesses error = %v", err)
	}
}

func TestRuntimeConfigValidateRejectsEmptyHarnesses(t *testing.T) {
	cfg := Config{APIVersion: APIVersionV4, Harnesses: []string{}, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("empty harnesses error = %v", err)
	}
}

func TestRuntimeConfigValidateAcceptsValidV4(t *testing.T) {
	cfg := Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main", Skills: KindSelector{Include: []string{"vm-lifecycle"}, Exclude: []string{"deprecated-skill"}}}},
		Tags:       []string{"network"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid runtime config rejected: %v", err)
	}
}

func TestV4RegistryEntrySourceHarnessDefaultsEmpty(t *testing.T) {
	cfg := parseRuntimeV4(t, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
`)
	if len(cfg.Registries) != 1 || cfg.Registries[0].SourceHarness != "" {
		t.Fatalf("source_harness must default to empty: %#v", cfg.Registries)
	}
}

func TestV4RegistryEntryHarnessesOverride(t *testing.T) {
	cfg := parseRuntimeV4(t, `apiVersion: agent-marketplace/v4
harnesses: [factory, cursor]
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
resources:
  infra: {harnesses: [factory]}
`)
	if !equalStrings(cfg.Registries[0].Harnesses, []string{"factory"}) {
		t.Fatalf("per-source harnesses = %v, want [factory]", cfg.Registries[0].Harnesses)
	}
}

func TestV4RegistryEntryAllKindsAreParsed(t *testing.T) {
	cfg := parseRuntimeV4(t, `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
resources:
  infra: [skill/vm-lifecycle, command/deploy, subagent/reviewer, hook/pre-commit, mcp/k8s-api, collection/team]
`)
	e := cfg.Registries[0]
	for _, tc := range []struct {
		kind ir.ResourceKind
		name string
	}{
		{ir.KindSkill, "vm-lifecycle"}, {ir.KindCommand, "deploy"}, {ir.KindSubagent, "reviewer"},
		{ir.KindHook, "pre-commit"}, {ir.KindMcpServer, "k8s-api"}, {ir.KindCollection, "team"},
	} {
		if got := e.SelectorForKind(tc.kind).Include; !equalStrings(got, []string{tc.name}) {
			t.Errorf("%s includes = %v, want [%s]", tc.kind, got, tc.name)
		}
	}
}

func TestV4EditorRoundTripPreservesResourceFields(t *testing.T) {
	const input = `apiVersion: agent-marketplace/v4
harnesses: [factory, cursor]
sources:
  infra:
    git: git@gitlab.example.com:example-org/example-infra-skills.git
    ref: main
    source_harness: claude
resources:
  infra:
    include: [skill/vm-lifecycle, command/onboard-vm, subagent/reviewer, hook/pre-commit, mcp/k8s-api]
    exclude: [skill/firewall-rule-lifecycle]
    harnesses: [factory]
    tags: [k8s]
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	editor, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	binding, found, err := editor.LookupRegistry(RegistryEntry{URL: "git@gitlab.example.com:example-org/example-infra-skills.git", Ref: "main", SourceHarness: "claude"})
	if err != nil || !found {
		t.Fatalf("source lookup: found=%v, err=%v", found, err)
	}
	if err := editor.Apply(binding, SelectionChange{Operation: SelectionInclude, Locators: []ResourceLocator{{Kind: ir.KindCollection, Name: "team"}}}); err != nil {
		t.Fatal(err)
	}
	candidate, err := editor.Render()
	if err != nil {
		t.Fatal(err)
	}
	if !candidate.Changed {
		t.Fatal("positive edit did not change rendered document")
	}
	cfg := parseRuntimeV4(t, string(candidate.Bytes))
	if cfg.APIVersion != APIVersionV4 || !equalStrings(cfg.Harnesses, []string{"factory", "cursor"}) || len(cfg.Registries) != 1 {
		t.Fatalf("settings changed: %#v", cfg)
	}
	e := cfg.Registries[0]
	if e.URL != "git@gitlab.example.com:example-org/example-infra-skills.git" || e.Ref != "main" || e.SourceHarness != "claude" || !equalStrings(e.Harnesses, []string{"factory"}) || !equalStrings(e.Tags, []string{"k8s"}) || !equalStrings(e.Skills.Exclude, []string{"firewall-rule-lifecycle"}) {
		t.Fatalf("source/group fields changed: %#v", e)
	}
	for _, tc := range []struct {
		kind ir.ResourceKind
		name string
	}{
		{ir.KindSkill, "vm-lifecycle"}, {ir.KindCommand, "onboard-vm"}, {ir.KindSubagent, "reviewer"},
		{ir.KindHook, "pre-commit"}, {ir.KindMcpServer, "k8s-api"}, {ir.KindCollection, "team"},
	} {
		if got := e.SelectorForKind(tc.kind).Include; !equalStrings(got, []string{tc.name}) {
			t.Errorf("%s includes = %v, want [%s]", tc.kind, got, tc.name)
		}
	}
}

func TestLoadConfigRejectsVersionlessRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const input = "sources:\n  infra: {git: \"git@example.com:infra.git\", ref: main}\nresources:\n  infra: [skill/vm-lifecycle]\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfigFromPath(path)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.api-version-required" {
		t.Fatalf("versionless error = %v", err)
	}
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"+input), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfigFromPath(path)
	if err != nil || cfg == nil || len(cfg.Registries) != 1 || !equalStrings(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("v4 control: cfg=%#v, err=%v", cfg, err)
	}
}

func TestLoadConfigAcceptsV4Config(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
resources:
  infra: [skill/vm-lifecycle]
`))
	cfg, err := loadConfig(path, true)
	if err != nil {
		t.Fatalf("v4 load: %v", err)
	}
	if cfg.APIVersion != APIVersionV4 || !equalStrings(cfg.Registries[0].Skills.Include, []string{"vm-lifecycle"}) {
		t.Fatalf("unexpected runtime config: %#v", cfg)
	}
}

func TestLoadLocalConfigRequiresAPIVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	const input = "sources:\n  local: {directory: /local/skills}\nresources:\n  local: {include: [skill/local-skill], exclude: [skill/unwanted]}\n"
	mustWriteFile(t, path, []byte(input))
	_, err := LoadLocalConfigFromPath(path)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.api-version-required" {
		t.Fatalf("local missing version error = %v", err)
	}
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\n"+input))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("local v4 control: %v", err)
	}
	if len(cfg.Registries) != 1 || !equalStrings(cfg.Registries[0].Skills.Include, []string{"local-skill"}) || !equalStrings(cfg.Registries[0].Skills.Exclude, []string{"unwanted"}) {
		t.Fatalf("local selection not retained: %#v", cfg)
	}
}

// Programmatic runtime requirements deliberately retain inheritance semantics;
// they are not promises to accept top-level legacy selectors on disk.
func TestMergeResolvesHarnessesInheritance(t *testing.T) {
	global := &Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory", "cursor"},
		Registries: []RegistryEntry{
			{URL: "git@example.com:global.git", Ref: "main"},
			{URL: "git@example.com:override.git", Ref: "main", Harnesses: []string{"factory"}},
		},
	}
	repo := &Config{APIVersion: APIVersionV4, Harnesses: []string{"factory"}, Registries: []RegistryEntry{{URL: "git@example.com:repo.git", Ref: "stable"}}}
	got := Merge(global, repo)
	if !equalStrings(got.Registries[0].Harnesses, []string{"factory", "cursor"}) {
		t.Fatalf("global inherited harnesses = %v", got.Registries[0].Harnesses)
	}
	if !equalStrings(got.Registries[1].Harnesses, []string{"factory"}) {
		t.Fatalf("global override harnesses = %v", got.Registries[1].Harnesses)
	}
	if !equalStrings(got.Registries[2].Harnesses, []string{"factory"}) {
		t.Fatalf("repo inherited harnesses = %v", got.Registries[2].Harnesses)
	}
}

func TestMergeResolvesPerKindInheritance(t *testing.T) {
	global := &Config{
		APIVersion: APIVersionV4,
		Harnesses:  []string{"factory"},
		Skills:     KindSelector{Include: []string{"global-skill"}, Exclude: []string{"legacy-skill"}},
		Commands:   KindSelector{Include: []string{"global-cmd"}},
		Registries: []RegistryEntry{
			{URL: "git@example.com:inherit.git", Ref: "main"},
			{URL: "git@example.com:override.git", Ref: "main", Skills: KindSelector{Include: []string{"entry-skill"}}, Commands: KindSelector{Include: []string{"entry-cmd"}}},
			{URL: "git@example.com:explicit-empty.git", Ref: "main", Skills: KindSelector{Include: []string{}}},
		},
	}
	got := Merge(global, nil)
	if !equalStrings(got.Registries[0].Skills.Include, []string{"global-skill"}) {
		t.Fatalf("inherited skills = %v", got.Registries[0].Skills.Include)
	}
	if !equalStrings(got.Registries[0].Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("inherited excludes = %v", got.Registries[0].Skills.Exclude)
	}
	if !equalStrings(got.Registries[0].Commands.Include, []string{"global-cmd"}) {
		t.Fatalf("inherited commands = %v", got.Registries[0].Commands.Include)
	}
	if !equalStrings(got.Registries[1].Skills.Include, []string{"entry-skill"}) {
		t.Fatalf("overridden skills = %v", got.Registries[1].Skills.Include)
	}
	if !equalStrings(got.Registries[1].Skills.Exclude, []string{"legacy-skill"}) {
		t.Fatalf("override inherited excludes = %v", got.Registries[1].Skills.Exclude)
	}
	if !equalStrings(got.Registries[1].Commands.Include, []string{"entry-cmd"}) {
		t.Fatalf("overridden commands = %v", got.Registries[1].Commands.Include)
	}
	if got.Registries[2].Skills.Include == nil || len(got.Registries[2].Skills.Include) != 0 {
		t.Fatalf("explicit empty include = %#v", got.Registries[2].Skills.Include)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestValidateWithoutHarnessesAcceptsMissingHarnesses(t *testing.T) {
	cfg := Config{APIVersion: APIVersionV4, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	if err := cfg.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("without harnesses: %v", err)
	}
}

func TestValidateWithoutHarnessesAcceptsEmptyHarnesses(t *testing.T) {
	cfg := Config{APIVersion: APIVersionV4, Harnesses: []string{}, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	if err := cfg.ValidateWithoutHarnesses(); err != nil {
		t.Fatalf("explicit empty harnesses: %v", err)
	}
}

func TestValidateWithoutHarnessesStillRequiresApiVersion(t *testing.T) {
	cfg := Config{Harnesses: []string{"factory"}, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	var validation *V4ValidationError
	err := cfg.ValidateWithoutHarnesses()
	if !errors.As(err, &validation) || validation.Code != "v4.api-version-required" {
		t.Fatalf("missing version error = %v", err)
	}
}

func TestValidateStillRequiresHarnessesForGlobal(t *testing.T) {
	cfg := Config{APIVersion: APIVersionV4, Registries: []RegistryEntry{{URL: "git@example.com:infra.git", Ref: "main"}}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("global missing harnesses error = %v", err)
	}
}

func TestLoadConfigRepoLayerAcceptsMissingHarnesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
`))
	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatalf("repo config without harnesses: %v", err)
	}
	if cfg.APIVersion != APIVersionV4 || len(cfg.Harnesses) != 0 || len(cfg.Registries) != 1 {
		t.Fatalf("unexpected repo config: %#v", cfg)
	}
}

func TestLoadConfigGlobalLayerRejectsMissingHarnesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  infra: {git: "git@example.com:infra.git", ref: main}
`))
	if _, err := loadConfig(path, true); err == nil || !strings.Contains(err.Error(), "harnesses") {
		t.Fatalf("global missing harnesses error = %v", err)
	}
}

func TestLoadLocalConfigWithSourcesParsesRegistries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  a-remote: {git: "git@example.com:local-skills.git", ref: main}
  b-local: {directory: /local/path}
resources:
  a-remote: {tags: [security]}
  b-local: [skill/local-skill]
`))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Registries) != 2 {
		t.Fatalf("expected two runtime entries: %#v", cfg)
	}
	remote, local := cfg.Registries[0], cfg.Registries[1]
	if remote.URL != "git@example.com:local-skills.git" || remote.Ref != "main" || !equalStrings(remote.Tags, []string{"security"}) || remote.IsLocal() {
		t.Fatalf("remote source not retained: %#v", remote)
	}
	if local.URL != "/local/path" || !local.IsLocal() || !equalStrings(local.Skills.Include, []string{"local-skill"}) {
		t.Fatalf("local source not retained: %#v", local)
	}
}

func TestLoadLocalConfigWithoutSourcesStillWorks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\nmaterialization: {mode: file}\n"))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Registries) != 0 || cfg.Materialization == nil || cfg.Materialization.Mode != "file" {
		t.Fatalf("source-free settings not retained: %#v", cfg)
	}

	// Resource selections require a source binding in v4, rather than the
	// retired unbound top-level selectors. Preserve the selection assertions
	// through the supported bound form as a non-vacuous control.
	const selections = "resources:\n  local:\n    include: [skill/local-skill, command/local-cmd]\n    exclude: [skill/opt-out]\n"
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\n"+selections))
	_, err = LoadLocalConfigFromPath(path)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.undefined-source" {
		t.Fatalf("unbound resources error = %v, want v4.undefined-source", err)
	}
	mustWriteFile(t, path, []byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: /local/skills}\n"+selections))
	cfg, err = LoadLocalConfigFromPath(path)
	if err != nil || cfg == nil || len(cfg.Registries) != 1 {
		t.Fatalf("source-bound control: cfg=%#v, err=%v", cfg, err)
	}
	entry := cfg.Registries[0]
	if !equalStrings(entry.Skills.Include, []string{"local-skill"}) || !equalStrings(entry.Skills.Exclude, []string{"opt-out"}) || !equalStrings(entry.Commands.Include, []string{"local-cmd"}) {
		t.Fatalf("bound local selectors not retained: %#v", entry)
	}
}

func TestLoadLocalConfigWithPerKindSelectorsInRegistryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  local: {git: "git@example.com:local-skills.git", ref: main}
resources:
  local:
    include: [skill/needed-skill, command/deploy-cmd, hook/local-hook, collection/local-collection]
    exclude: [skill/unwanted-skill, subagent/restricted-subagent, mcp/blocked-mcp]
`))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Registries) != 1 {
		t.Fatalf("expected one entry: %#v", cfg)
	}
	e := cfg.Registries[0]
	if !equalStrings(e.Skills.Exclude, []string{"unwanted-skill"}) || !equalStrings(e.Skills.Include, []string{"needed-skill"}) || !equalStrings(e.Commands.Include, []string{"deploy-cmd"}) || !equalStrings(e.Subagents.Exclude, []string{"restricted-subagent"}) || !equalStrings(e.Hooks.Include, []string{"local-hook"}) || !equalStrings(e.MCP.Exclude, []string{"blocked-mcp"}) || !equalStrings(e.Collections.Include, []string{"local-collection"}) {
		t.Fatalf("per-kind selectors not retained: %#v", e)
	}
}

func TestLoadLocalConfigFromPathStrictParsingRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	const input = `apiVersion: agent-marketplace/v4
sources:
  local: {directory: /local/path}
resources:
  local: {exclude: [skill/opt-out]}
`
	for _, tc := range []struct{ field, code string }{{"harnesses: [factory]\n", "v4.layer-field"}, {"unknown: true\n", "v4.unknown-field"}} {
		t.Run(tc.code, func(t *testing.T) {
			mustWriteFile(t, path, []byte(input+tc.field))
			_, err := LoadLocalConfigFromPath(path)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("field refusal = %v, want %s", err, tc.code)
			}
		})
	}
	mustWriteFile(t, path, []byte(input))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil || cfg == nil || len(cfg.Registries) != 1 || !equalStrings(cfg.Registries[0].Skills.Exclude, []string{"opt-out"}) {
		t.Fatalf("valid control: cfg=%#v, err=%v", cfg, err)
	}
}

func TestParseLocalConfigRetainsAllAndTags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  local: {git: "git@example.com:strict.git", ref: main}
resources:
  local: {all: true, tags: [k8s]}
`))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Registries) != 1 {
		t.Fatalf("expected one entry: %#v", cfg)
	}
	e := cfg.Registries[0]
	if e.URL != "git@example.com:strict.git" || !e.Default || !e.V4All || !equalStrings(e.Tags, []string{"k8s"}) {
		t.Fatalf("all/tags not retained: %#v", e)
	}
}

func TestParseLocalConfigResourceListShorthand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.local.yaml")
	mustWriteFile(t, path, []byte(`apiVersion: agent-marketplace/v4
sources:
  local: {git: "git@example.com:list-selector.git", ref: main}
resources:
  local: [skill/vm-lifecycle, skill/k8s-agent]
`))
	cfg, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Registries) != 1 || !reflect.DeepEqual(cfg.Registries[0].Skills.Include, []string{"k8s-agent", "vm-lifecycle"}) {
		t.Fatalf("normalized shorthand includes not retained: %#v", cfg)
	}
	decisions := cfg.Registries[0].V4Decisions
	if len(decisions) != 2 || decisions[0].Locator != "skill/vm-lifecycle" || decisions[1].Locator != "skill/k8s-agent" {
		t.Fatalf("authored decision order not retained: %#v", decisions)
	}
}
