package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// spec: consumer-config-format/writable-layer-version-refusals-name-the-older-migrator
// spec: consumer-config-format/organization-version-refusals-direct-users-to-their-administrator
// spec: consumer-config-format/other-diagnostics-keep-their-existing-guidance
func TestV4VersionRefusalRemediationUsesOwningLayer(t *testing.T) {
	for _, tc := range []struct {
		name, scope string
		source      Source
		admin       bool
	}{
		{name: "user", scope: "user", source: UserPersonal},
		{name: "repo", scope: "repo", source: RepoCommitted},
		{name: "local", scope: "local", source: RepoPersonal},
		{name: "organization", source: Organization, admin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			writeIncludeConfig(t, root, "sources: {}\n")
			_, err := LoadV4ConfigIfPresent(root, tc.source)
			assertV4Remediation(t, err, tc.scope, tc.admin, "v4.api-version-required")

			fragment := filepath.Join(dir, "fragment.yaml")
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\ninclude: [fragment.yaml]\n")
			writeIncludeConfig(t, fragment, "apiVersion: agent-marketplace/v2\n")
			_, err = LoadV4ConfigIfPresent(root, tc.source)
			assertV4Remediation(t, err, tc.scope, tc.admin, "v4.api-version")
		})
	}
}

func TestV4OtherDiagnosticsHaveNoVersionRefusalRemediation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		code string
	}{
		{name: "legacy key", data: []byte("apiVersion: agent-marketplace/v4\nregistries: []\n"), code: "v4.legacy-key"},
		{name: "parse", data: []byte("sources: [\n"), code: "v4.parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseV4Config(tc.data, "config.yaml", true)
			var validation *V4ValidationError
			if !errors.As(err, &validation) || validation.Code != tc.code {
				t.Fatalf("error = %v, want %s", err, tc.code)
			}
			remediation, ok := any(validation).(interface{ Remediation() string })
			if !ok || remediation.Remediation() != "" {
				t.Fatalf("%s remediation = %v, want empty", tc.code, validation)
			}
		})
	}
}

// spec: command-error-output/validation-errors-name-the-authored-file
func TestV4ValidationErrorNamesAuthoredFileAndLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".agents", "config.yaml")
	_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nsources:\n  bad name: {directory: ./registry}\n"), path, true)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.source-name" {
		t.Fatalf("error = %v, want v4.source-name validation error", err)
	}
	want := "v4.source-name: " + path + ":3:"
	if !strings.HasPrefix(validation.Error(), want) {
		t.Fatalf("validation message = %q, want prefix %q", validation, want)
	}
}

func TestV4ValidationErrorIncludesPathUnlessMessageContainsIt(t *testing.T) {
	t.Run("parse message already starts with authored file", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		message := file + ": yaml: line 1: did not find expected ',' or ']'"
		validation := &V4ValidationError{Code: "v4.parse", File: file, Msg: message}
		want := "v4.parse: " + file + ": yaml: line 1: did not find expected ',' or ']'"
		if got := validation.Error(); got != want {
			t.Fatalf("validation error = %q, want %q", got, want)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		_, err := ParseV4Config([]byte("apiVersion: agent-marketplace/v4\nunknown_field: true\n"), file, true)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.unknown-field" {
			t.Fatalf("error = %v, want v4.unknown-field validation error", err)
		}
		want := "v4.unknown-field: " + file + ":2: unknown_field: unknown top-level field"
		if got := validation.Error(); got != want {
			t.Fatalf("validation error = %q, want %q", got, want)
		}
	})

	t.Run("marketplace lock mismatch does not duplicate locator", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), ".agents", "config.yaml")
		locator := "awesome-ai/collection/coderabbit-skills"
		validation := &V4ValidationError{
			Code: v4MarketplaceLockMismatchCode, File: file, Line: 6,
			Path: locator, LockFile: ".agents/lock.yaml", Variant: "resource-missing",
		}
		want := "v4.marketplace-lock-mismatch: " + file + ":6: resource \"" + locator +
			"\" is missing from the lock file .agents/lock.yaml"
		if got := validation.Error(); got != want {
			t.Fatalf("validation error = %q, want %q", got, want)
		}
		if got := strings.Count(validation.Error(), locator); got != 1 {
			t.Fatalf("locator appears %d times in %q, want exactly once", got, validation.Error())
		}
	})
}

func TestV4MarketplaceLockMismatchNamesLockFile(t *testing.T) {
	lockPath := ".agents/lock.yaml"
	for _, tc := range []struct {
		name, variant, message, want string
	}{
		{
			name: "missing resource", variant: "resource-missing",
			message: `resource "awesome-ai/collection/coderabbit-skills" is missing from the acquisition lock`,
			want:    `resource "awesome-ai/collection/coderabbit-skills" is missing from the lock file .agents/lock.yaml`,
		},
		{
			name: "unrecorded endpoint", variant: "",
			message: `Marketplace source "hosted" has no lock record for endpoint "https://marketplace.example"`,
			want:    `Marketplace source "hosted" has no lock record for endpoint "https://marketplace.example" (lock file .agents/lock.yaml)`,
		},
		{
			name: "configuration mismatch", variant: "configuration",
			message: `duplicate Marketplace source declaration for endpoint "https://marketplace.example"`,
			want:    `duplicate Marketplace source declaration for endpoint "https://marketplace.example" (lock file .agents/lock.yaml)`,
		},
		{
			name: "duplicate request", variant: "duplicate-request",
			message: `requests "tools/skill/review" and "tools/skill/review-copy" refer to the same locked resource`,
			want:    `requests "tools/skill/review" and "tools/skill/review-copy" refer to the same locked resource (lock file .agents/lock.yaml)`,
		},
		{
			name: "lock restore", variant: "lock-restore",
			message: `lock scope "user" does not match "repo"`,
			want:    `lock scope "user" does not match "repo" (lock file .agents/lock.yaml)`,
		},
		{
			name: "lock read", variant: "lock-read",
			message: `the acquisition lock could not be read: invalid document`,
			want:    `the acquisition lock could not be read: invalid document (lock file .agents/lock.yaml)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := (&V4ValidationError{
				Code: v4MarketplaceLockMismatchCode, File: "/workspace/.agents/config.yaml", Path: "awesome-ai/collection/coderabbit-skills",
				Line: 6, LockFile: lockPath, Variant: tc.variant, Msg: tc.message,
			}).Error()
			if !strings.HasSuffix(err, tc.want) {
				t.Fatalf("mismatch error = %q, want suffix %q", err, tc.want)
			}
		})
	}
}

func assertV4Remediation(t *testing.T, err error, scope string, admin bool, code string) {
	t.Helper()
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
	remediation, ok := any(validation).(interface{ Remediation() string })
	if !ok {
		t.Fatalf("%s does not expose Remediation()", code)
	}
	hint := remediation.Remediation()
	required := []string{"https://github.com/Adyen/agent-marketplace/blob/main/site/docs/how-to/migrate-configuration.md"}
	if admin {
		required = append(required, "Ask your administrator", "convert and deploy")
	} else {
		required = append(required, "agent-marketplace v4.1.4", "older released migrator", "with that older binary",
			"agent-marketplace migrate --to v4 --scope "+scope+" --dry-run", "include trees or unrepresentable input")
	}
	for _, want := range required {
		if !strings.Contains(hint, want) {
			t.Fatalf("%s remediation lacks %q: %s", code, want, hint)
		}
	}
	lower := strings.ToLower(hint)
	if strings.Contains(lower, "add apiversion") || strings.Contains(lower, "change apiversion") {
		t.Fatalf("%s remediation advises merely editing apiVersion: %s", code, hint)
	}
	if strings.Contains(hint, "agent-marketplace migrate") == admin {
		t.Fatalf("remediation has the wrong migration-command guidance: %s", hint)
	}
}

func TestV4OrderedChildExcludeThenRootIncludeSurvivesRuntimeAdapter(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "config.yaml")
	fragment := filepath.Join(dir, "fragment.yaml")
	if err := os.WriteFile(fragment, []byte(`sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {exclude: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(`apiVersion: agent-marketplace/v4
include: fragment.yaml
sources:
  shared: {git: https://example.test/agents.git, ref: main}
resources:
  shared: {include: [skill/review]}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	v4, err := LoadV4ConfigFromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if len(entry.Skills.Include) != 1 || entry.Skills.Include[0] != "review" {
		t.Fatalf("effective include = %#v", entry.Skills.Include)
	}
	if len(entry.Skills.Exclude) != 0 {
		t.Fatalf("stale exclusion survived later include: %#v", entry.Skills.Exclude)
	}
	if len(entry.V4Decisions) != 2 || entry.V4Decisions[0].Action != V4SelectionExclude || entry.V4Decisions[1].Action != V4SelectionExplicit {
		t.Fatalf("ordered decisions = %#v", entry.V4Decisions)
	}
}

// spec: resource-selection/required-policy-alone-selects-no-resources
func TestV4RequiredOnlyDoesNotAutoSelectCatalog(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.test/agents.git, ref: main}}
resources: {shared: {required: true}}
`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if entry.Required || entry.Default || len(entry.V4Decisions) != 0 {
		t.Fatalf("required-only adapter = %#v", entry)
	}
	if len(entry.Skills.Include) != 0 && entry.Skills.Include != nil {
		t.Fatalf("unexpected selected skills: %#v", entry.Skills.Include)
	}
}

func TestV4AllSelectionSurvivesRuntimeAdapter(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
sources: {shared: {git: https://example.test/agents.git, ref: main}}
resources:
  shared:
    include: [skill/keep]
    tags: [platform]
    all: true
    exclude: [skill/blocked]
`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if !entry.V4All || !entry.Default {
		t.Fatalf("all-selection adapter lost v4 all marker: %#v", entry)
	}
	if len(entry.V4Decisions) != 2 || entry.V4Decisions[0].Action != V4SelectionExplicit || entry.V4Decisions[1].Action != V4SelectionExclude {
		t.Fatalf("all-selection decisions = %#v", entry.V4Decisions)
	}
	merged := MergeLayers([]Layer{{Source: RepoCommitted, Config: legacy}})
	if len(merged.Registries) != 1 || !merged.Registries[0].V4All {
		t.Fatalf("all-selection marker lost during layer merge: %#v", merged.Registries)
	}
}

func TestV4DirectoryKindSurvivesGitSuffixedResolvedPath(t *testing.T) {
	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "registry.git")
	if err := os.Mkdir(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v4, err := ParseV4Config([]byte(`apiVersion: agent-marketplace/v4
sources:
  local: {directory: registry.git}
resources:
  local: [skill/review]
`), filepath.Join(dir, "config.yaml"), true)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if !entry.IsLocal() || entry.V4Kind != V4SourceDirectory {
		t.Fatalf("directory source reclassified: %#v", entry)
	}
	resolved, err := entry.ResolveLocalPath(dir)
	if err != nil || resolved != sourceDir {
		t.Fatalf("resolved directory = %q, %v", resolved, err)
	}
}

func TestV4LocalRejectsAuxiliaryLayerFields(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
logging: {level: debug}
sources: {shared: {git: https://example.test/agents.git, ref: main}}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfig(localLayer)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.layer-field" {
		t.Fatalf("error = %v, want v4.layer-field", err)
	}
}

// spec: upgrade-notice/repository-configuration-cannot-set-the-release-check
func TestV4RepositoryLayersRejectUpdateCheck(t *testing.T) {
	v4, err := ParseV4([]byte("apiVersion: agent-marketplace/v4\nupdate_check: {enabled: false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []layerKind{repoLayer, localLayer} {
		_, err = v4.ToLegacyConfig(kind)
		var validation *V4ValidationError
		if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || validation.Path != "update_check" {
			t.Fatalf("layer %v: error = %v, want v4.layer-field for update_check", kind, err)
		}
	}
}

func TestV4OrganizationRejectsMaterializationLayerField(t *testing.T) {
	v4, err := ParseV4([]byte(`apiVersion: agent-marketplace/v4
materialization: {mode: file}
sources: {shared: {git: https://example.test/agents.git, ref: main}}
`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v4.ToLegacyConfig(organizationLayer)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.layer-field" || validation.Path != "materialization" {
		t.Fatalf("error = %v, want v4.layer-field for materialization", err)
	}
}

// spec: consumer-config-format/mutations-refuse-before-new-writes-or-prunes
func TestLayerEditorRefusesLegacyRootWithoutChangingFiles(t *testing.T) {
	for _, source := range []Source{UserPersonal, RepoCommitted, RepoPersonal} {
		for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
			t.Run(source.String()+"/"+version, func(t *testing.T) {
				dir := t.TempDir()
				declaring := filepath.Join(dir, "config.yaml")
				staged := filepath.Join(dir, "staged.yaml")
				before := []byte("apiVersion: " + version + "\nregistries: []\n")
				for _, path := range []string{declaring, staged} {
					if err := os.WriteFile(path, before, 0o640); err != nil {
						t.Fatal(err)
					}
				}
				info, err := os.Stat(staged)
				if err != nil {
					t.Fatal(err)
				}
				backup := filepath.Join(dir, "config.yaml.pre-v4.existing.bak")
				writeIncludeConfig(t, backup, "foreign backup\n")
				_, err = OpenLayerEditor(LayerTarget{Source: source, StagedPath: staged, DeclaringPath: declaring})
				var validation *V4ValidationError
				if !errors.As(err, &validation) || validation.Code != "v4.api-version" || validation.Path != declaring || validation.Line != 1 {
					t.Fatalf("editor format refusal = %v", err)
				}
				for _, path := range []string{declaring, staged} {
					if got, err := os.ReadFile(path); err != nil || string(got) != string(before) {
						t.Fatalf("refusal changed %s: %q %v", path, got, err)
					}
				}
				after, err := os.Stat(staged)
				if err != nil || !os.SameFile(info, after) || info.Mode() != after.Mode() || !info.ModTime().Equal(after.ModTime()) {
					t.Fatalf("refusal changed metadata: %v", err)
				}
				if got := readConfigString(t, backup); got != "foreign backup\n" {
					t.Fatalf("foreign backup changed: %q", got)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 3 {
					t.Fatalf("refusal left residue: %v %v", entries, err)
				}
			})
		}
	}
}

func TestV4ReaderPreservesExplicitEmptyTagsAndHarnesses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeIncludeConfig(t, path, `apiVersion: agent-marketplace/v4
sources: {agents: {git: https://example.test/agents.git, ref: main}}
resources: {agents: {tags: [], harnesses: []}}
`)
	v4, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	group := v4.Groups["agents"]
	if !group.HasTags || group.Tags == nil || len(group.Tags) != 0 || !group.HasHarnesses || group.Harnesses == nil || len(group.Harnesses) != 0 {
		t.Fatalf("empty overrides lost: %#v", group)
	}
	legacy, err := v4.ToLegacyConfig(repoLayer)
	if err != nil {
		t.Fatal(err)
	}
	entry := legacy.Registries[0]
	if entry.Tags == nil || entry.Harnesses == nil {
		t.Fatalf("empty overrides became nil: tags=%#v harnesses=%#v", entry.Tags, entry.Harnesses)
	}
}

func TestV4EditorRejectsForeignSourceBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	before := "apiVersion: agent-marketplace/v4\nsources: {shared: {git: https://example.test/shared.git, ref: main}}\nresources: {shared: [skill/review]}\n"
	writeIncludeConfig(t, path, before)
	first, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenLayerEditor(LayerTarget{Source: RepoCommitted, StagedPath: path, DeclaringPath: path})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := first.EnsureRegistry(RegistryEntry{URL: "https://example.test/shared.git", Ref: "main"})
	if err != nil {
		t.Fatal(err)
	}
	change := SelectionChange{Operation: SelectionExclude, Locators: []ResourceLocator{{Kind: ir.KindSkill, Name: "review"}}}
	err = second.Apply(binding, change)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "config.edit.invalid-binding" {
		t.Fatalf("foreign binding accepted: %v", err)
	}
	candidate, err := second.Render()
	if err != nil || candidate.Changed || string(candidate.Bytes) != before {
		t.Fatalf("foreign binding mutated candidate: %#v %v", candidate, err)
	}
	if err := first.Apply(binding, change); err != nil {
		t.Fatal(err)
	}
	candidate, err = first.Render()
	if err != nil || !candidate.Changed || string(candidate.Bytes) == before {
		t.Fatalf("own binding positive control failed: %#v %v", candidate, err)
	}
	view, err := ParseV4Config(candidate.Bytes, path, true)
	if err != nil || len(view.Groups["shared"].Include) != 0 || len(view.Groups["shared"].Exclude) != 1 || view.Groups["shared"].Exclude[0] != "skill/review" {
		t.Fatalf("own binding did not exclude review: view=%#v err=%v", view, err)
	}
	if got := readConfigString(t, path); got != before {
		t.Fatalf("candidate render wrote root: %q", got)
	}
}
