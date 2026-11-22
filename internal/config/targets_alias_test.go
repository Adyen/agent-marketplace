package config_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func targetAliasConfig(listKey, rootTargets, groupTargets string) []byte {
	return []byte("apiVersion: agent-marketplace/v4\n" +
		listKey + ": " + rootTargets + "\n" +
		"sources:\n  local: {directory: /fixture}\n" +
		"resources:\n  local:\n    include: [skill/review]\n" +
		"    " + listKey + ": " + groupTargets + "\n")
}

func targetAliasIntent(t *testing.T, org, user, repo string) config.Intent {
	t.Helper()
	dir := t.TempDir()
	paths := []struct {
		name string
		data string
	}{
		{"org.yaml", org},
		{"user.yaml", user},
		{"repo.yaml", repo},
	}
	for _, item := range paths {
		if item.data == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, item.name), []byte(item.data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return config.ReadIntent(context.Background(), config.LoadEffectiveOptions{
		RepoRoot:         dir,
		OrganizationPath: filepath.Join(dir, "org.yaml"),
		GlobalPath:       filepath.Join(dir, "user.yaml"),
		RepoPath:         filepath.Join(dir, "repo.yaml"),
		LocalPath:        filepath.Join(dir, "local.yaml"),
	})
}

func assertTargetAliasError(t *testing.T, err error, code, file string, fields ...string) {
	t.Helper()
	validationErr := err
	var validation *config.V4ValidationError
	if !errors.As(err, &validation) || validation == nil {
		t.Fatalf("error = %v, want V4ValidationError %s", err, code)
	}
	canonicalFile, pathErr := filepath.EvalSymlinks(file)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if validation.Code != code || validation.File != canonicalFile {
		t.Fatalf("validation = %#v, want code %q and file %q", validation, code, canonicalFile)
	}
	for _, field := range fields {
		if !strings.Contains(validationErr.Error(), field) {
			t.Errorf("error %q does not name %q", validationErr, field)
		}
	}
}

// spec: consumer-config-format/root-and-group-targets-resolve-like-harnesses
// spec: consumer-config-format/unknown-target-ids-under-targets-are-refused
func TestTargetsAliasRootAndGroupScopes(t *testing.T) {
	for _, key := range []string{"targets", "harnesses"} {
		t.Run(key, func(t *testing.T) {
			parsed, err := config.ParseV4Config(targetAliasConfig(key, "[claude, factory]", "[claude]"), "/fixture/config.yaml", true)
			if err != nil {
				t.Fatalf("ParseV4Config: %v", err)
			}
			if !reflect.DeepEqual(parsed.Harnesses, []string{"claude", "factory"}) {
				t.Fatalf("root target list = %v, want [claude factory]", parsed.Harnesses)
			}
			group := parsed.Groups["local"]
			if !group.HasHarnesses || !reflect.DeepEqual(group.Harnesses, []string{"claude"}) {
				t.Fatalf("group target list = %#v, want [claude]", group)
			}
		})
	}

	unknownPath := filepath.Join(t.TempDir(), "unknown.yaml")
	if err := os.WriteFile(unknownPath, []byte("apiVersion: agent-marketplace/v4\ntargets: [future-tool]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadV4ConfigIfPresent(unknownPath, config.UserPersonal)
	assertTargetAliasError(t, err, "v4.harness", unknownPath, "future-tool")
}

// spec: consumer-config-format/both-spellings-in-one-file-are-refused
// spec: consumer-config-format/empty-targets-outside-the-user-root-is-refused
func TestTargetsAliasRejectsBothSpellingsBeforeWrite(t *testing.T) {
	tests := []struct {
		name   string
		source config.Source
		data   string
		code   string
		fields []string
	}{
		{
			name:   "root spellings",
			source: config.UserPersonal,
			data:   "apiVersion: agent-marketplace/v4\ntargets: [factory]\nharnesses: [claude]\n",
			code:   "v4.targets-alias",
			fields: []string{"targets", "harnesses"},
		},
		{
			name:   "group spellings",
			source: config.UserPersonal,
			data: "apiVersion: agent-marketplace/v4\nsources: {local: {directory: /fixture}}\n" +
				"resources:\n  local: {include: [skill/review], targets: [factory], harnesses: [claude]}\n",
			code:   "v4.targets-alias",
			fields: []string{"targets", "harnesses"},
		},
		{
			name:   "organization root empty",
			source: config.Organization,
			data:   "apiVersion: agent-marketplace/v4\ntargets: []\n",
			code:   "v4.harness",
		},
		{
			name:   "repository root empty",
			source: config.RepoCommitted,
			data:   "apiVersion: agent-marketplace/v4\ntargets: []\n",
			code:   "v4.harness",
		},
		{
			name:   "group empty",
			source: config.UserPersonal,
			data: "apiVersion: agent-marketplace/v4\nsources: {local: {directory: /fixture}}\n" +
				"resources:\n  local: {include: [skill/review], targets: []}\n",
			code: "v4.harness",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = config.LoadV4ConfigIfPresent(path, test.source)
			assertTargetAliasError(t, err, test.code, path, test.fields...)
			after, readErr := os.ReadFile(path)
			if readErr != nil || string(after) != string(before) {
				t.Fatalf("refusal changed config bytes: got %q, err=%v", after, readErr)
			}
		})
	}
}

// spec: resource-selection/explicit-empty-user-target-list-selects-no-machine-targets
// spec: config-mutation/target-list-edits-keep-the-authored-spelling
// spec: config-mutation/editing-an-inherited-user-list-copies-the-effective-list
func TestUserTargetsAbsentSnapshotAndExplicitEmpty(t *testing.T) {
	org := "apiVersion: agent-marketplace/v4\nharnesses: [factory, claude]\n"
	absent := targetAliasIntent(t, org, "apiVersion: agent-marketplace/v4\n", "")
	got, source := absent.TargetHarnesses()
	if !reflect.DeepEqual(got, []string{"factory", "claude"}) || source != config.Organization {
		t.Fatalf("absent user list resolves to %v from %v, want organization [factory claude]", got, source)
	}

	emptyHarnesses := targetAliasIntent(t, org, "apiVersion: agent-marketplace/v4\nharnesses: []\n", "")
	got, source = emptyHarnesses.TargetHarnesses()
	if !reflect.DeepEqual(got, []string{"factory", "claude"}) || source != config.Organization {
		t.Fatalf("empty harnesses list resolves to %v from %v, want organization [factory claude]", got, source)
	}

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\ntargets: [factory]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetHarnessesInConfig(userPath, nil); err != nil {
		t.Fatalf("SetHarnessesInConfig for targets: []: %v", err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "targets: []") || strings.Contains(string(data), "harnesses:") {
		t.Fatalf("empty target edit changed spelling: %s", data)
	}

	emptyTargets := targetAliasIntent(t, org, string(data), "")
	got, source = emptyTargets.TargetHarnesses()
	if len(got) != 0 || source != config.UserPersonal {
		t.Fatalf("explicit empty user targets resolve to %v from %v, want empty from user", got, source)
	}

	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetHarnessesInConfig(userPath, []string{"factory", "claude", "cursor"}); err != nil {
		t.Fatalf("SetHarnessesInConfig snapshot: %v", err)
	}
	data, err = os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "harnesses:") || strings.Contains(string(data), "targets:") {
		t.Fatalf("editing an absent user list did not default to harnesses: %s", data)
	}

	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetHarnessesInConfig(userPath, []string{"cursor"}); err != nil {
		t.Fatalf("SetHarnessesInConfig for authored harnesses: %v", err)
	}
	data, err = os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "harnesses: [cursor]") || strings.Contains(string(data), "targets:") {
		t.Fatalf("editing harnesses spelling changed the authored key: %s", data)
	}

	restored := targetAliasIntent(t, org, "apiVersion: agent-marketplace/v4\n", "")
	got, source = restored.TargetHarnesses()
	if !reflect.DeepEqual(got, []string{"factory", "claude"}) || source != config.Organization {
		t.Fatalf("deleting the user list resolves to %v from %v, want organization [factory claude]", got, source)
	}

	if err := os.WriteFile(userPath, []byte(`apiVersion: agent-marketplace/v4
harnesses: []
sources:
  market: {marketplace: https://marketplace.example}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetUserMarketplaceExclusion(userPath, "https://marketplace.example", "team", ir.KindSkill, "review", true); err != nil {
		t.Fatalf("unrelated exclusion edit: %v", err)
	}
	data, err = os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "harnesses: []") || strings.Contains(string(data), "targets:") {
		t.Fatalf("unrelated edit changed an authored empty harnesses list: %s", data)
	}
}

// spec: config-mutation/removing-the-last-target-without-targets-spelling-is-refused
func TestTargetsEditorRefusesEmptyHarnessesSpelling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	const before = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
	if err := os.WriteFile(userPath, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(userPath), "lock.yaml")
	if err := os.WriteFile(lockPath, []byte("pinned lock fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = config.SetHarnessesInConfig(userPath, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "upgrade every reader") ||
		!strings.Contains(err.Error(), "targets:") {
		t.Fatalf("empty harnesses edit error = %v, want reader-upgrade guidance to rename the key to targets:", err)
	}
	after, readErr := os.ReadFile(userPath)
	if readErr != nil || string(after) != before {
		t.Fatalf("refusing empty harnesses edit changed config: %q, %v", after, readErr)
	}
	lockAfter, readErr := os.ReadFile(lockPath)
	if readErr != nil || string(lockAfter) != "pinned lock fixture\n" {
		t.Fatalf("refusing empty harnesses edit changed lock: %q, %v", lockAfter, readErr)
	}
}

// spec: consumer-config-format/empty-targets-outside-the-user-root-is-refused
func TestTargetsAliasEmptyIncludedFragmentIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(filepath.Dir(userPath), "targets.yaml")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("apiVersion: agent-marketplace/v4\ninclude: [targets.yaml]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragment, []byte("targets: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = config.LoadConfigFromPath(userPath)
	assertTargetAliasError(t, err, "v4.harness", fragment, "empty targets")
}

// spec: config-mutation/unrelated-edit-leaves-an-empty-harnesses-list-as-written
func TestUnrelatedEditPreservesEmptyHarnessesSpelling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	userPath, err := config.GlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(userPath), 0o700); err != nil {
		t.Fatal(err)
	}
	before := "apiVersion: agent-marketplace/v4\nharnesses: []\nsources:\n  tools:\n    marketplace: https://market.example\nresources:\n  tools: [tools/skill/review]\n"
	if err := os.WriteFile(userPath, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetUserMarketplaceExclusion(userPath, "https://market.example", "tools", ir.KindSkill, "review", true); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "harnesses: []") || strings.Contains(string(after), "targets:") {
		t.Fatalf("unrelated exclusion edit changed the empty harnesses spelling:\n%s", after)
	}
}

// spec: resource-selection/group-target-list-cannot-add-a-harness
func TestTargetsGroupCannotWiden(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	source := filepath.Join(dir, "source")
	configPath := filepath.Join(repo, ".agents", "config.yaml")
	for _, path := range []string{filepath.Dir(configPath), filepath.Join(source, "skills", "review")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "skills", "review", "SKILL.md"), []byte("---\nname: review\ndescription: Review\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`apiVersion: agent-marketplace/v4
targets: [factory]
sources:
  local: {directory: `+source+`}
resources:
  local:
    include: [skill/review]
    targets: [factory, claude]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("LoadConfigFromPath: %v", err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("registry count = %d, want 1", len(cfg.Registries))
	}
	entry := cfg.Registries[0]
	entry.CatalogKey = "local"
	cfg.Registries[0] = entry
	resolved, errs := resolver.Resolve(cfg, map[string][]catalog.Entry{
		"local": {{Kind: ir.KindSkill, Name: "review", Path: "/cache/review"}},
	}, nil, resolver.WithTargetHarnesses([]hcc.Target{hcc.TargetFactory}))
	if len(errs) != 0 {
		t.Fatalf("Resolve errors = %v", errs)
	}
	if len(resolved) != 1 || !reflect.DeepEqual(resolved[0].Harnesses, []hcc.Target{hcc.TargetFactory}) {
		t.Fatalf("resolved group targets = %#v, want only Factory", resolved)
	}
}
