package hookpolicy

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/target"
)

const validCatalog = `schema: 1
harness: devin
evidence:
  checked: "2026-09-29"
  refs: [synthetic-test-fixture]
env:
  project_dir: [DEVIN_PROJECT_DIR]
plugin_env:
  plugin_root: [DEVIN_PLUGIN_ROOT]
repository_base: {env: project_dir}
timeout: {unit: seconds}
matcher: {syntax: regex, anchor: true}
loads_hooks_of: []
events:
  PreToolUse: {canonical: PreToolUse, matcher: tool, types: [command, prompt]}
  SessionEnd: {canonical: SessionEnd, matcher: none, types: [command]}
`

func load(t *testing.T, catalog string) error {
	t.Helper()
	name := "devin"
	if strings.Contains(catalog, "harness: windsurf") {
		name = "windsurf"
	}
	_, err := loadCatalogs(fstest.MapFS{
		"catalogs/" + name + ".yaml": {Data: []byte(catalog)},
	}, "catalogs/*.yaml")
	return err
}

// spec: hook-resources/shipped-catalogs-pass-the-check
func TestShippedCatalogsValidate(t *testing.T) {
	if err := ValidateCatalogs(); err != nil {
		t.Fatal(err)
	}
}

func TestEveryTargetShipsACatalog(t *testing.T) {
	r, err := shipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, tg := range target.Targets() {
		if _, ok := r.catalogs[tg]; !ok {
			t.Fatalf("no hook catalog for target %s", tg)
		}
	}
}

// spec: hook-resources/invalid-catalog-fails-the-check
func TestMalformedCatalogsRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		catalog string
		want    string
	}{
		{
			name: "overlapping split events",
			catalog: `schema: 1
harness: windsurf
evidence:
  checked: "2026-09-29"
  refs: [synthetic-test-fixture]
env: {}
plugin_env: {}
repository_base: {cwd: repository}
timeout: {unit: none}
matcher: {syntax: none}
loads_hooks_of: []
events:
  pre_read_code: {canonical: PreToolUse, tools: [file.read], types: [command]}
  pre_read_more: {canonical: PreToolUse, tools: [file.read, file.list], types: [command]}
`,
			want: "overlap",
		},
		{
			name: "type on an event that allows none",
			catalog: strings.Replace(validCatalog,
				"types: [command, prompt]}", "types: [bogus_handler]}", 1),
			want: "bogus_handler",
		},
		{
			name: "bad timeout unit",
			catalog: strings.Replace(validCatalog,
				"timeout: {unit: seconds}", "timeout: {unit: minutes}", 1),
			want: "minutes",
		},
		{
			name: "event timeout bounds with unit none",
			catalog: strings.Replace(strings.Replace(validCatalog,
				"timeout: {unit: seconds}", "timeout: {unit: none}", 1),
				"matcher: none, types: [command]}", "matcher: none, types: [command], timeout: {min: 1, max: 3}}", 1),
			want: "timeout",
		},
		{
			name: "bad repository base env concept",
			catalog: strings.Replace(validCatalog,
				"repository_base: {env: project_dir}", "repository_base: {env: not_a_concept}", 1),
			want: "not_a_concept",
		},
		{
			name: "malformed repository base",
			catalog: strings.Replace(validCatalog,
				"repository_base: {env: project_dir}", "repository_base: {cwd: somewhere}", 1),
			want: "repository",
		},
		{
			name: "invalid env name",
			catalog: strings.Replace(validCatalog,
				"project_dir: [DEVIN_PROJECT_DIR]", "project_dir: [9BAD]", 1),
			want: "9BAD",
		},
		{
			name: "duplicate env name",
			catalog: strings.Replace(validCatalog,
				"plugin_root: [DEVIN_PLUGIN_ROOT]", "plugin_root: [DEVIN_PROJECT_DIR]", 1),
			want: "DEVIN_PROJECT_DIR",
		},
		{
			name: "env events reference unknown event",
			catalog: strings.Replace(validCatalog,
				"project_dir: [DEVIN_PROJECT_DIR]",
				"project_dir: {names: [DEVIN_PROJECT_DIR], events: [NopeEvent]}", 1),
			want: "NopeEvent",
		},
		{
			name: "unknown canonical event",
			catalog: strings.Replace(validCatalog,
				"canonical: PreToolUse", "canonical: NotARealEvent", 1),
			want: "NotARealEvent",
		},
		{
			name: "unknown split tool concept",
			catalog: strings.Replace(validCatalog,
				"PreToolUse: {canonical: PreToolUse, matcher: tool, types: [command, prompt]}",
				"PreToolUse: {canonical: PreToolUse, tools: [bogus.concept], types: [command]}", 1),
			want: "bogus.concept",
		},
		{
			name: "matcher kind on a matcher-less harness",
			catalog: strings.Replace(validCatalog,
				"matcher: {syntax: regex, anchor: true}", "matcher: {syntax: none}", 1),
			want: "matcher",
		},
		{
			name: "missing evidence",
			catalog: strings.Replace(validCatalog,
				"evidence:\n  checked: \"2026-09-29\"\n  refs: [synthetic-test-fixture]\n", "", 1),
			want: "evidence",
		},
		{
			name: "self loads_hooks_of",
			catalog: strings.Replace(validCatalog,
				"loads_hooks_of: []", "loads_hooks_of: [devin]", 1),
			want: "devin",
		},
		{
			name: "unknown key",
			catalog: strings.Replace(validCatalog,
				"timeout: {unit: seconds}", "bogus_field: 1\ntimeout: {unit: seconds}", 1),
			want: "bogus_field",
		},
		{
			name: "duplicate key",
			catalog: strings.Replace(validCatalog,
				"schema: 1", "schema: 1\nschema: 1", 1),
			want: "duplicate",
		},
		{
			name: "anchor without regex syntax",
			catalog: strings.Replace(validCatalog,
				"matcher: {syntax: regex, anchor: true}", "matcher: {syntax: exact-or-regex, anchor: true}", 1),
			want: "anchor",
		},
		{
			name: "tools and mcp on one event",
			catalog: strings.Replace(validCatalog,
				"PreToolUse: {canonical: PreToolUse, matcher: tool, types: [command, prompt]}",
				"PreToolUse: {canonical: PreToolUse, tools: [shell], mcp: true, types: [command]}", 1),
			want: "mcp",
		},
		{
			name: "inverted event timeout bounds",
			catalog: strings.Replace(validCatalog,
				"matcher: none, types: [command]}",
				"matcher: none, types: [command], timeout: {min: 5, max: 2}}", 1),
			want: "timeout",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := load(t, tc.catalog)
			if err == nil {
				t.Fatalf("invalid catalog accepted; want failure containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %q", err, tc.want)
			}
		})
	}
}

func TestSupportReferenceRefusesWithoutRepositoryBase(t *testing.T) {
	ref, refusal, ok := supportReference(catalog{}, ScopeRepository, "hook-support/source/hook", 0)
	if ok || ref != "" {
		t.Fatalf("supportReference succeeded without a repository base: %q", ref)
	}
	if refusal.Code != diag.CodeHookSupportPathUnrepresentable {
		t.Fatalf("refusal code = %q, want %q", refusal.Code, diag.CodeHookSupportPathUnrepresentable)
	}
}

func TestSupportReferenceUsesDeclaredRepositoryBase(t *testing.T) {
	ref, _, ok := supportReference(catalog{RepositoryBase: &repositoryBase{Cwd: "repository"}}, ScopeRepository, "hook-support/source/hook", 0)
	if !ok || ref != "hook-support/source/hook" {
		t.Fatalf("supportReference = %q, want repository-relative support path", ref)
	}
}
