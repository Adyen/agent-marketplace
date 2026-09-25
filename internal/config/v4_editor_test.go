package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

func TestEditV4ResourceSelectionDirectoryIdentityAndActions(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	registry := filepath.Join(dir, "registry.git")
	if err := os.MkdirAll(filepath.Join(registry, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "skills", "alpha", "SKILL.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".agents", "config.yaml")
	config := "apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin]\nsources:\n  local: {directory: " + registry + "}\n  other: {git: https://example.test/other.git, ref: main}\nresources:\n  local:\n    include:\n      - skill/other\n      - skill/alpha\n    exclude: [skill/beta]\n  other: [skill/alpha]\n"
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o640); err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: registry, V4Kind: V4SourceDirectory}
	if handled, err := EditV4ResourceSelection(configPath, configPath, entry, ir.KindSkill, "beta", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("enable = handled %v, err %v", handled, err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "skill/beta") || !strings.Contains(string(data), "skill/other") || !strings.Contains(string(data), "other: [skill/alpha]") {
		t.Fatalf("unexpected enabled document:\n%s", data)
	}
	if !strings.Contains(string(data), "harnesses: [factory, gemini, devin]") {
		t.Fatalf("harnesses changed:\n%s", data)
	}
	before := append([]byte(nil), data...)
	fixedTime := time.Unix(123, 456)
	if err := os.Chtimes(configPath, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if handled, err := EditV4ResourceSelection(configPath, configPath, entry, ir.KindSkill, "beta", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("idempotent enable = handled %v, err %v", handled, err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("idempotent edit changed bytes or mtime")
	}
	if handled, err := EditV4ResourceSelection(configPath, configPath, entry, ir.KindSkill, "alpha", V4SelectionDisable); !handled || err != nil {
		t.Fatalf("disable = handled %v, err %v", handled, err)
	}
	if handled, err := EditV4ResourceSelection(configPath, configPath, entry, ir.KindSkill, "alpha", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("re-enable = handled %v, err %v", handled, err)
	}
	final, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(final), "exclude: [skill/alpha]") {
		t.Fatalf("re-enable retained exact exclusion:\n%s", final)
	}
	if !strings.Contains(string(final), "skill/alpha") {
		t.Fatalf("re-enable lost locator:\n%s", final)
	}
}

func TestEditV4ResourceSelectionUsesOriginalIncludeRootAndPreservesFragment(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	root := filepath.Join(dir, ".agents", "config.yaml")
	fragment := filepath.Join(dir, ".agents", "fragments", "sources.yaml")
	registry := filepath.Join(dir, ".agents", "fragments", "registry")
	if err := os.MkdirAll(filepath.Join(registry, "skills", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registry, "skills", "alpha", "SKILL.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	fragmentData := []byte("sources:\n  local:\n    directory: ./registry\nresources:\n  local:\n    include: [skill/alpha]\n")
	if err := os.MkdirAll(filepath.Dir(fragment), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fragment, fragmentData, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("apiVersion: agent-marketplace/v4\ninclude: fragments/sources.yaml\nharnesses: [factory, gemini, devin]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(stage, []byte("apiVersion: agent-marketplace/v4\ninclude: fragments/sources.yaml\nharnesses: [factory, gemini, devin]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: registry, V4Kind: V4SourceDirectory}
	if handled, err := EditV4ResourceSelection(stage, root, entry, ir.KindSkill, "beta", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("staged enable = handled %v, err %v", handled, err)
	}
	if got, err := os.ReadFile(fragment); err != nil || string(got) != string(fragmentData) {
		t.Fatalf("fragment changed: %v\n%s", err, got)
	}
	got, err := os.ReadFile(stage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "resources:\n  local:\n    include:\n      - skill/beta\n") {
		t.Fatalf("staged root did not gain root selection:\n%s", got)
	}
}

func TestEditV4ResourceSelectionRejectsAmbiguousInvalidAndWildcard(t *testing.T) {
	cases := map[string]struct {
		entry RegistryEntry
		data  string
		name  string
		code  string
	}{
		"ambiguous": {
			entry: RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"},
			data:  "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n  two: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {include: [skill/alpha]}\n",
			name:  "beta", code: "v4.edit-ambiguous-source",
		},
		"wildcard": {
			entry: RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"},
			data:  "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\nresources:\n  one: {exclude: [skill/*]}\n",
			name:  "beta", code: "v4.edit-exclusion",
		},
		"invalid locator": {
			entry: RegistryEntry{URL: "https://example.test/repo.git", Ref: "main"},
			data:  "apiVersion: agent-marketplace/v4\nsources:\n  one: {git: https://example.test/repo.git, ref: main}\n",
			name:  "bad/name", code: "v4.locator",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			data := []byte(tc.data)
			if err := os.WriteFile(path, data, 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			handled, gotErr := EditV4ResourceSelection(path, path, tc.entry, ir.KindSkill, tc.name, V4SelectionEnable)
			if !handled || gotErr == nil {
				t.Fatalf("handled %v err %v", handled, gotErr)
			}
			var validation *V4ValidationError
			if !errors.As(gotErr, &validation) || validation.Code != tc.code {
				t.Fatalf("error = %T %v", gotErr, gotErr)
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
				t.Fatalf("failed edit changed config: %v\n%s", err, after)
			}
			if matches, err := filepath.Glob(path + ".tmp-*"); err != nil || len(matches) != 0 {
				t.Fatalf("temporary files after failed edit: %v %v", matches, err)
			}
		})
	}
}

func TestEditV4ResourceSelectionShorthandEnableIsByteNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  local: {directory: " + source + "}\nresources:\n  local: [skill/alpha]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(321, 654)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable)
	if !handled || err != nil {
		t.Fatalf("enable = handled %v err %v", handled, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(data) || afterInfo.Mode().Perm() != 0o640 || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("shorthand no-op changed document, mode, or mtime")
	}
	if matches, err := filepath.Glob(path + ".tmp-*"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files after no-op: %v %v", matches, err)
	}
}

func TestEditV4ResourceSelectionRejectsUnsupportedSourceKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: "https://example.test/market", V4Kind: V4SourceMarketplace}, ir.KindSkill, "alpha", V4SelectionEnable)
	if !handled || err == nil {
		t.Fatalf("handled %v err %v", handled, err)
	}
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.edit-source" {
		t.Fatalf("error = %T %v", err, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(data) {
		t.Fatalf("unsupported source changed bytes: %v %s", readErr, got)
	}
}

func TestEditV4ResourceSelectionPreservesMappingsOptionsCommentsAndKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory, gemini, devin] # targets\nsources:\n  local: {directory: " + source + "}\nresources:\n  local:\n    include:\n      - skill/beta: {as: beta}\n      - command/alpha\n    tags: [platform]\n    all: true\n    harnesses: [factory]\n    exclude: [skill/sibling]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(changed) == string(before) || !strings.Contains(string(changed), "# targets") {
		t.Fatalf("preservation edit did not change selection or retain comment:\n%s", changed)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 || len(loaded.Groups) != 1 {
		t.Fatalf("source/group counts = %d/%d", len(loaded.Sources), len(loaded.Groups))
	}
	group := loaded.Groups["local"]
	foundBeta, foundCommand, foundAlpha := false, false, false
	for _, request := range group.Include {
		switch request.Locator {
		case "skill/beta":
			foundBeta = request.Options.HasAs && request.Options.As == "beta"
		case "command/alpha":
			foundCommand = true
		case "skill/alpha":
			foundAlpha = true
		}
	}
	if !foundBeta || !foundCommand || !foundAlpha || !group.HasTags || !group.HasAll || !group.HasHarnesses || len(group.Exclude) != 1 || group.Exclude[0] != "skill/sibling" {
		t.Fatalf("unrelated resource policy changed: %#v", group)
	}
	if _, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionDisable); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range loaded.Groups["local"].Include {
		if request.Locator == "skill/alpha" {
			t.Fatal("disabled alpha remained included")
		}
	}
	if _, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups["local"].Include) != 3 {
		t.Fatalf("re-enabled include count = %d", len(loaded.Groups["local"].Include))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v err %v", info.Mode().Perm(), err)
	}
}

func TestEditV4ResourceSelectionGitIdentityUsesRefAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  mainSkills: {git: https://example.test/repo.git, ref: main, path: skills}\n  devSkills: {git: https://example.test/repo.git, ref: dev, path: skills}\n  mainCommands: {git: https://example.test/repo.git, ref: main, path: commands}\nresources:\n  mainSkills: [skill/alpha]\n  devSkills: [skill/dev]\n  mainCommands: [command/alpha]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "main", Path: "skills", V4Kind: V4SourceGit}
	if _, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "beta", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups["mainSkills"].Include) != 2 || len(loaded.Groups["devSkills"].Include) != 1 || len(loaded.Groups["mainCommands"].Include) != 1 {
		t.Fatalf("source-specific groups changed: %#v", loaded.Groups)
	}
	if loaded.Groups["devSkills"].Include[0].Locator != "skill/dev" || loaded.Groups["mainCommands"].Include[0].Locator != "command/alpha" {
		t.Fatalf("unrelated Git source selections changed")
	}
}

func TestLayerEditorEnsureRegistryDeclaresSourceOnceWithoutSelectingResources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	layerEditorWrite(t, path, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	// A trailing slash names the same subdirectory and is written normalized.
	entry := RegistryEntry{URL: "https://example.test/repo.git", Ref: "feature", Path: "teams/skills/daily-sync/"}
	ensure := func() bool {
		t.Helper()
		editor := openTestEditor(t, RepoCommitted, path, path)
		if _, err := editor.EnsureRegistry(entry); err != nil {
			t.Fatalf("ensure: %v", err)
		}
		candidate, err := editor.Render()
		if err != nil {
			t.Fatal(err)
		}
		if candidate.Changed {
			if err := os.WriteFile(path, candidate.Bytes, candidate.Mode); err != nil {
				t.Fatal(err)
			}
		}
		return candidate.Changed
	}
	if !ensure() {
		t.Fatal("first ensure did not declare the source")
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil || len(loaded.Sources) != 1 || len(loaded.Groups) != 0 {
		t.Fatalf("ensured source = %#v groups %#v err %v", loaded.Sources, loaded.Groups, err)
	}
	if declared, err := os.ReadFile(path); err != nil || !strings.Contains(string(declared), "path: teams/skills/daily-sync\n") {
		t.Fatalf("ensured source path not normalized: %s, %v", declared, err)
	}
	if ensure() {
		t.Fatal("repeat ensure changed config")
	}
	if _, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "daily-sync", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil || len(loaded.Sources) != 1 {
		t.Fatalf("selection duplicated the ensured source: %#v, %v", loaded.Sources, err)
	}
	for name := range loaded.Sources {
		if group := loaded.Groups[name]; len(group.Include) != 1 || group.Include[0].Locator != "skill/daily-sync" {
			t.Fatalf("selection group = %#v", group)
		}
	}
}

func TestEditV4ResourceSelectionKeepsSymlinkSourceIdentities(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	real := filepath.Join(dir, "version-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	currentA := filepath.Join(dir, "current-a")
	currentB := filepath.Join(dir, "current-b")
	if err := os.Symlink(real, currentA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, currentB); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  one: {directory: " + currentA + "}\n  two: {directory: " + currentB + "}\nresources:\n  two: [skill/beta]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: currentA, V4Kind: V4SourceDirectory}
	if handled, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("enable = handled %v err %v", handled, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 2 || loaded.Sources["one"].Directory != currentA || loaded.Sources["two"].Directory != currentB {
		t.Fatalf("source identities = %#v", loaded.Sources)
	}
	seen := map[string]string{}
	for _, request := range loaded.Requests {
		seen[request.SourceName+"/"+request.Locator] = request.Locator
	}
	if len(seen) != 2 || seen["one/skill/alpha"] == "" || seen["two/skill/beta"] == "" {
		t.Fatalf("requests = %#v", loaded.Requests)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Unix(888, 0)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if handled, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("repeat enable = handled %v err %v", handled, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || string(after) != string(before) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("repeat enable changed bytes/mtime: %v", err)
	}
}

func TestEditV4ResourceSelectionCreatesSeparateSymlinkBinding(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	real := filepath.Join(dir, "version-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	currentA := filepath.Join(dir, "current-a")
	currentB := filepath.Join(dir, "current-b")
	if err := os.Symlink(real, currentA); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, currentB); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\nsources:\n  one: {directory: " + currentA + "}\nresources:\n  one: [skill/beta]\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	if handled, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: currentB, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable); !handled || err != nil {
		t.Fatalf("separate binding = handled %v err %v", handled, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	newName := ""
	for name, source := range loaded.Sources {
		if source.Directory == currentB {
			newName = name
		}
	}
	if newName == "" || len(loaded.Sources) != 2 || loaded.Sources["one"].Directory != currentA {
		t.Fatalf("sources = %#v", loaded.Sources)
	}
	oneBeta, newAlpha := false, false
	for _, request := range loaded.Requests {
		if request.SourceName == "one" {
			if request.Locator != "skill/beta" {
				t.Fatalf("one source request = %#v", request)
			}
			oneBeta = true
		}
		if request.SourceName == newName {
			if request.Locator != "skill/alpha" {
				t.Fatalf("new source request = %#v", request)
			}
			newAlpha = true
		}
	}
	if !oneBeta || !newAlpha {
		t.Fatalf("requests did not preserve separate selections: %#v", loaded.Requests)
	}
}

func TestEditV4ResourceSelectionCreatesLexicalDirectoryBindingAndChecksRelativeContainment(t *testing.T) {
	dir := t.TempDir()
	realVersion := filepath.Join(dir, "version-1")
	if err := os.MkdirAll(realVersion, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "current")
	if err := os.Symlink(realVersion, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: link, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "directory: "+link) || strings.Contains(string(data), "directory: "+realVersion) {
		t.Fatalf("directory binding did not preserve lexical symlink path:\n%s", data)
	}
	fixedTime := time.Unix(777, 0)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EditV4ResourceSelection(path, path, RegistryEntry{URL: link, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || string(after) != string(data) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("repeated source insertion changed bytes/mtime: %v", err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil || len(loaded.Sources) != 1 {
		t.Fatalf("repeated source insertion sources = %d err %v", len(loaded.Sources), err)
	}

	repo := t.TempDir()
	repoConfig := filepath.Join(repo, ".agents", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(repoConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoConfig, []byte("apiVersion: agent-marketplace/v4\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	handled, err := EditV4ResourceSelection(repoConfig, repoConfig, RegistryEntry{URL: "./escape", V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable)
	if !handled || err == nil {
		t.Fatalf("escape handled %v err %v", handled, err)
	}
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.directory-path" {
		t.Fatalf("escape error = %T %v", err, err)
	}
	if got, readErr := os.ReadFile(repoConfig); readErr != nil || string(got) != "apiVersion: agent-marketplace/v4\n" {
		t.Fatalf("escape changed config: %v %s", readErr, got)
	}
	insideTarget := filepath.Join(repo, "inside-target")
	if err := os.MkdirAll(insideTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(insideTarget, filepath.Join(repo, "inside")); err != nil {
		t.Fatal(err)
	}
	handled, err = EditV4ResourceSelection(repoConfig, repoConfig, RegistryEntry{URL: "./inside", V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionEnable)
	if !handled || err != nil {
		t.Fatalf("inside symlink handled %v err %v", handled, err)
	}
	// A committed repository root keeps the portable relative spelling, and
	// the binding stays lexical rather than following the symlink.
	if got, readErr := os.ReadFile(repoConfig); readErr != nil || !strings.Contains(string(got), "directory: ./inside\n") {
		t.Fatalf("inside symlink binding = %v %s", readErr, got)
	}
	bound, err := LoadV4ConfigFromPath(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range bound.Sources {
		if want := filepath.Join(canonicalRepo, "inside"); source.CanonicalURL != want {
			t.Fatalf("inside symlink resolved to %s, want lexical %s", source.CanonicalURL, want)
		}
	}
}

func TestEditV4ResourceSelectionInheritedAndWildcardExclusionsFailClosed(t *testing.T) {
	for name, tc := range map[string]struct {
		fragment, rootGroup, wantErr, wantExclude string
	}{
		"inherited": {fragment: "resources:\n  local: {exclude: [skill/alpha]}\n", rootGroup: "resources:\n  local: {exclude: [skill/alpha, skill/sibling]}\n", wantErr: "v4.edit-exclusion", wantExclude: "skill/alpha, skill/sibling"},
		"wildcard":  {fragment: "", rootGroup: "resources:\n  local: {exclude: [skill/*, skill/alpha]}\n", wantErr: "v4.edit-exclusion", wantExclude: "skill/*, skill/alpha"},
	} {
		fragment, rootGroup, wantErr, wantExclude := tc.fragment, tc.rootGroup, tc.wantErr, tc.wantExclude
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "registry")
			if err := os.MkdirAll(source, 0o755); err != nil {
				t.Fatal(err)
			}
			fragmentPath := filepath.Join(dir, "fragment.yaml")
			if fragment != "" {
				if err := os.WriteFile(fragmentPath, []byte("sources:\n  local: {directory: "+source+"}\n"+fragment), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "config.yaml")
			root := "apiVersion: agent-marketplace/v4\n"
			if fragment != "" {
				root += "include: fragment.yaml\n"
			} else {
				root += "sources:\n  local: {directory: " + source + "}\n"
			}
			root += rootGroup
			if err := os.WriteFile(path, []byte(root), 0o640); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			handled, gotErr := EditV4ResourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, ir.KindSkill, "alpha", V4SelectionClearExclude)
			if !handled || gotErr == nil {
				t.Fatalf("handled %v err %v", handled, gotErr)
			}
			var validation *V4ValidationError
			if !errors.As(gotErr, &validation) || validation.Code != wantErr {
				t.Fatalf("error = %T %v", gotErr, gotErr)
			}
			if after, readErr := os.ReadFile(path); readErr != nil || string(after) != string(before) {
				t.Fatalf("failed clear changed config: %v\n%s", readErr, after)
			}
			if matches, globErr := filepath.Glob(path + ".tmp-*"); globErr != nil || len(matches) != 0 {
				t.Fatalf("temporary files after failed clear: %v %v", matches, globErr)
			}
			if !strings.Contains(rootGroup, wantExclude) {
				t.Fatal("test exclusion fixture is not non-vacuous")
			}
		})
	}
}

func TestEditV4ResourceSelectionRootExcludeClearsAndInheritedIncludeCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "registry")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(dir, "fragment.yaml")
	if err := os.WriteFile(fragment, []byte("sources:\n  local: {directory: "+source+"}\nresources:\n  local: [skill/alpha]\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}
	if _, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", V4SelectionDisable); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups["local"].Exclude) != 1 || loaded.Groups["local"].Exclude[0] != "skill/alpha" {
		t.Fatalf("disable exclusion = %#v", loaded.Groups["local"].Exclude)
	}
	if _, err := EditV4ResourceSelection(path, path, entry, ir.KindSkill, "alpha", V4SelectionEnable); err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups["local"].Exclude) != 0 {
		t.Fatalf("reenable retained exclusion = %#v", loaded.Groups["local"].Exclude)
	}
	if len(loaded.Groups["local"].Include) == 0 {
		t.Fatal("reenable removed inherited include")
	}
}

func TestEditV4ResourceSelectionLegacyWriterGuardRemains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte("apiVersion: agent-marketplace/v4\n")
	if err := os.WriteFile(path, data, 0o640); err != nil {
		t.Fatal(err)
	}
	err := AddSkillToConfig(path, "alpha")
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.writer-opt-in" {
		t.Fatalf("legacy writer error = %T %v", err, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(data) {
		t.Fatalf("legacy writer changed bytes: %v %s", readErr, got)
	}
}
