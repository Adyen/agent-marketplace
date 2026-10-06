package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditSourceSelectionV4SelectAllAndIdempotent(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	other := filepath.Join(dir, "plugins", "other")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.yaml")
	writeSourceExclusionFixture(t, path, "apiVersion: agent-marketplace/v4\n"+
		"sources:\n"+
		"  tools: {directory: "+source+"}\n"+
		"  other: {directory: "+other+"}\n"+
		"resources:\n"+
		"  tools:\n"+
		"    include: [skill/alpha]\n"+
		"    tags: [stable]\n"+
		"    exclude: [skill/beta]\n"+
		"  other: {all: true}\n")
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	changed, err := EditSourceSelection(path, path, entry, true)
	if err != nil || !changed {
		t.Fatalf("select all = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	group := loaded.Groups["tools"]
	if !group.HasAll || group.All == nil || !*group.All || len(group.Include) != 0 || len(group.Tags) != 0 || len(group.Exclude) != 0 {
		t.Fatalf("selected group = %+v", group)
	}
	if otherGroup := loaded.Groups["other"]; !otherGroup.HasAll || otherGroup.All == nil || !*otherGroup.All {
		t.Fatalf("other group changed: %+v", otherGroup)
	}
	runtime, err := LoadConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Registries) != 2 || !runtime.Registries[0].V4All {
		t.Fatalf("runtime registries = %+v", runtime.Registries)
	}

	data := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceSelection(path, path, entry, true); err != nil || changed {
		t.Fatalf("repeated select all = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != data {
		t.Fatalf("repeated select all rewrote the file:\n%s", got)
	}
}

func TestEditSourceSelectionV4AddsSourceAndRejectsGlobalRelative(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	original := "apiVersion: agent-marketplace/v4\n"
	writeSourceExclusionFixture(t, path, original)
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	changed, err := EditSourceSelection(path, path, entry, true)
	if err != nil || !changed {
		t.Fatalf("select all undeclared source = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 || len(loaded.Groups) != 1 {
		t.Fatalf("loaded config = sources %v groups %v", loaded.Sources, loaded.Groups)
	}
	for name, group := range loaded.Groups {
		if !group.HasAll || group.All == nil || !*group.All || loaded.Sources[name].Directory != source {
			t.Fatalf("added source/group = %q, %+v, %+v", name, loaded.Sources[name], group)
		}
	}

	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	global := filepath.Join(os.Getenv("HOME"), ".agents", "config.yaml")
	globalOriginal := "apiVersion: agent-marketplace/v4\n"
	writeSourceExclusionFixture(t, global, globalOriginal)
	relative := RegistryEntry{URL: "./plugins/tools", V4Kind: V4SourceDirectory}
	changed, err = EditSourceSelection(global, global, relative, true)
	var relativeErr *GlobalRelativeRegistryError
	if changed || !errors.As(err, &relativeErr) {
		t.Fatalf("global relative selection = changed %v, err %T %v", changed, err, err)
	}
	if got := readSourceExclusionFixture(t, global); got != globalOriginal {
		t.Fatalf("refused global edit changed the file:\n%s", got)
	}
}

func TestEditSourceSelectionV4RefusesInheritedExclusion(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(dir, ".agents", "fragment.yaml")
	writeSourceExclusionFixture(t, fragment, "sources:\n"+
		"  tools: {directory: "+source+"}\n"+
		"resources:\n"+
		"  tools: {exclude: [skill/alpha]}\n")
	path := filepath.Join(dir, ".agents", "config.yaml")
	original := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\n"
	writeSourceExclusionFixture(t, path, original)

	changed, err := EditSourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, true)
	var validation *V4ValidationError
	if changed || !errors.As(err, &validation) || validation.Code != "v4.edit-exclusion" {
		t.Fatalf("error = changed %v, %T %v", changed, err, err)
	}
	if !strings.Contains(err.Error(), "fragment.yaml") {
		t.Fatalf("error does not identify inherited fragment: %v", err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("refused edit changed the root:\n%s", got)
	}
}

// spec: tui-source-toggle/inherited-selection-cannot-be-silently-removed
func TestEditSourceSelectionV4RequiredAndInheritedSelection(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, selectAll := range []bool{true, false} {
		t.Run(map[bool]string{true: "select", false: "deselect"}[selectAll], func(t *testing.T) {
			path := filepath.Join(dir, ".agents", "required-"+map[bool]string{true: "select", false: "deselect"}[selectAll]+".yaml")
			original := "apiVersion: agent-marketplace/v4\n" +
				"sources: {tools: {directory: " + source + "}}\n" +
				"resources:\n" +
				"  tools: {required: true, all: true}\n"
			writeSourceExclusionFixture(t, path, original)
			changed, err := EditSourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, selectAll)
			var validation *V4ValidationError
			if changed || !errors.As(err, &validation) || validation.Code != "v4.edit-required" {
				t.Fatalf("error = changed %v, %T %v", changed, err, err)
			}
			if got := readSourceExclusionFixture(t, path); got != original {
				t.Fatalf("required edit changed the file:\n%s", got)
			}
		})
	}

	fragment := filepath.Join(dir, ".agents", "selected.yaml")
	writeSourceExclusionFixture(t, fragment, "sources: {tools: {directory: "+source+"}}\n"+
		"resources: {tools: {all: true}}\n")
	path := filepath.Join(dir, ".agents", "config.yaml")
	original := "apiVersion: agent-marketplace/v4\ninclude: selected.yaml\n"
	writeSourceExclusionFixture(t, path, original)
	changed, err := EditSourceSelection(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, false)
	if changed || err == nil || !strings.Contains(err.Error(), "v4.edit-selection") {
		t.Fatalf("inherited selection = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("refused inherited deselection changed the root:\n%s", got)
	}
}

func TestEditSourceSelectionV4DeselectsAndKeepsSourceDeclarations(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	keptSource := filepath.Join(dir, "plugins", "kept")
	droppedSource := filepath.Join(dir, "plugins", "dropped")
	if err := os.MkdirAll(keptSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(droppedSource, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	writeSourceExclusionFixture(t, path, "apiVersion: agent-marketplace/v4\n"+
		"sources:\n"+
		"  kept: {directory: "+keptSource+"}\n"+
		"  dropped: {directory: "+droppedSource+"}\n"+
		"resources:\n"+
		"  kept: {all: true, tags: [stable], include: [skill/alpha], exclude: [skill/beta]}\n"+
		"  dropped: {all: true, tags: [stable], include: [skill/alpha]}\n")

	if changed, err := EditSourceSelection(path, path, RegistryEntry{URL: keptSource, V4Kind: V4SourceDirectory}, false); err != nil || !changed {
		t.Fatalf("deselect kept source = changed %v, err %v", changed, err)
	}
	if changed, err := EditSourceSelection(path, path, RegistryEntry{URL: droppedSource, V4Kind: V4SourceDirectory}, false); err != nil || !changed {
		t.Fatalf("deselect dropped source = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	kept := loaded.Groups["kept"]
	if len(kept.Include) != 0 || len(kept.Tags) != 0 || len(kept.Exclude) != 1 || kept.Exclude[0] != "skill/beta" {
		t.Fatalf("kept group = %+v", kept)
	}
	if _, ok := loaded.Groups["dropped"]; ok {
		t.Fatalf("dropped group remains: %+v", loaded.Groups["dropped"])
	}
	if _, ok := loaded.Sources["dropped"]; !ok {
		t.Fatal("deselected source declaration was removed")
	}
	if _, ok := loaded.Sources["kept"]; !ok {
		t.Fatal("source with an exclusion was removed")
	}
	if data := readSourceExclusionFixture(t, path); !strings.Contains(data, droppedSource) {
		t.Fatalf("deselected source declaration was removed:\n%s", data)
	}

	data := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceSelection(path, path, RegistryEntry{URL: droppedSource, V4Kind: V4SourceDirectory}, false); err != nil || changed {
		t.Fatalf("repeated deselect = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != data {
		t.Fatalf("repeated deselect rewrote the file:\n%s", got)
	}
}

func TestEditSourceSelectionV4DeselectEmptyGroupAndUndeclaredSource(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	original := "apiVersion: agent-marketplace/v4\n" +
		"sources: {tools: {directory: " + source + "}}\n" +
		"resources: {tools: {all: true, tags: [stable], include: [skill/alpha]}}\n"
	writeSourceExclusionFixture(t, path, original)
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	if changed, err := EditSourceSelection(path, path, entry, false); err != nil || !changed {
		t.Fatalf("deselect empty group = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Groups) != 0 {
		t.Fatalf("empty resource group remains: %+v", loaded.Groups)
	}
	if _, ok := loaded.Sources["tools"]; !ok {
		t.Fatal("deselect removed the source declaration")
	}
	if data := readSourceExclusionFixture(t, path); strings.Contains(data, "resources:") {
		t.Fatalf("empty resources mapping remains:\n%s", data)
	}

	undeclared := RegistryEntry{URL: filepath.Join(dir, "plugins", "other"), V4Kind: V4SourceDirectory}
	before := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceSelection(path, path, undeclared, false); err != nil || changed {
		t.Fatalf("deselect undeclared source = changed %v, err %v", changed, err)
	}
	if after := readSourceExclusionFixture(t, path); after != before {
		t.Fatalf("deselect undeclared source rewrote the file:\n%s", after)
	}
}

func TestEditSourceSelectionV4StagedCopy(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "apiVersion: agent-marketplace/v4\n"
	declaring := filepath.Join(dir, ".agents", "config.yaml")
	writeSourceExclusionFixture(t, declaring, original)
	stage := filepath.Join(t.TempDir(), "config.yaml")
	writeSourceExclusionFixture(t, stage, original)
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	if changed, err := EditSourceSelection(stage, declaring, entry, true); err != nil || !changed {
		t.Fatalf("staged select all = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, declaring); got != original {
		t.Fatalf("staged edit wrote the real root:\n%s", got)
	}
	staged := readSourceExclusionFixture(t, stage)
	if staged == original || !strings.Contains(staged, "all: true") {
		t.Fatalf("staged copy was not selected:\n%s", staged)
	}
	if changed, err := EditSourceSelection(stage, declaring, entry, false); err != nil || !changed {
		t.Fatalf("staged deselect = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, stage); got == original || !strings.Contains(got, source) {
		t.Fatalf("staged deselect removed the source declaration:\n%s", got)
	}
}

func TestEditSourceSelectionLegacySelectAllAndAppend(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	source := "https://example.test/team.git"
	writeSourceExclusionFixture(t, path, "apiVersion: agent-marketplace/v2\nregistries:\n"+
		"  - url: "+source+"\n"+
		"    ref: main\n"+
		"    path: plugins/tools\n"+
		"    default: false\n"+
		"    tags: [stable]\n"+
		"    exclude_all: true\n"+
		"    skills: {include: [alpha], exclude: [beta], exclude_all: true}\n"+
		"    commands: {include: [run], exclude: [stop], exclude_all: true}\n"+
		"    subagents: [agent]\n"+
		"  - url: "+source+"\n"+
		"    ref: main\n"+
		"    path: plugins/other\n"+
		"    skills: {include: [keep]}\n")
	entry := RegistryEntry{URL: source, Ref: "main", Path: "plugins/tools"}
	if changed, err := EditSourceSelection(path, path, entry, true); err != nil || !changed {
		t.Fatalf("legacy select all = changed %v, err %v", changed, err)
	}
	got, ok := loadLocalEntry(t, path, source)
	if !ok || !got.Default || got.ExcludeAll || len(got.Tags) != 0 ||
		len(got.Skills.Include) != 0 || len(got.Skills.Exclude) != 0 || got.Skills.ExcludeAll ||
		len(got.Commands.Include) != 0 || len(got.Commands.Exclude) != 0 || got.Commands.ExcludeAll ||
		len(got.Subagents.Include) != 0 {
		t.Fatalf("selected legacy entry = %+v (found %v)", got, ok)
	}
	loaded, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var sibling RegistryEntry
	var foundSibling bool
	for _, candidate := range loaded.Registries {
		if candidate.URL == source && candidate.Path == "plugins/other" {
			sibling, foundSibling = candidate, true
			break
		}
	}
	if !foundSibling || len(sibling.Skills.Include) != 1 || sibling.Skills.Include[0] != "keep" {
		t.Fatalf("sibling entry unexpectedly changed = %+v (found %v)", sibling, foundSibling)
	}
	data := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceSelection(path, path, entry, true); err != nil || changed {
		t.Fatalf("repeated legacy select all = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != data {
		t.Fatalf("repeated legacy select all rewrote the file:\n%s", got)
	}

	missing := filepath.Join(dir, "missing", "config.local.yaml")
	newEntry := RegistryEntry{URL: "https://example.test/other.git", Ref: "dev", Path: "skills"}
	if changed, err := EditSourceSelection(missing, missing, newEntry, true); err != nil || !changed {
		t.Fatalf("append missing legacy config = changed %v, err %v", changed, err)
	}
	appended, ok := loadLocalEntry(t, missing, newEntry.URL)
	if !ok || !appended.Default || appended.Ref != "dev" || appended.Path != "skills" {
		t.Fatalf("appended entry = %+v (found %v)", appended, ok)
	}

	absent := filepath.Join(dir, "absent", "config.local.yaml")
	if changed, err := EditSourceSelection(absent, absent, newEntry, false); err != nil || changed {
		t.Fatalf("deselect missing config = changed %v, err %v", changed, err)
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deselect missing config created file: %v", err)
	}
}

func TestEditSourceSelectionLegacyDeselectKeepsExclusions(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	source := "https://example.test/team.git"
	original := "registries:\n" +
		"  - url: " + source + "\n" +
		"    ref: main\n" +
		"    path: plugins/tools\n" +
		"    default: true\n" +
		"    tags: [stable]\n" +
		"    exclude_all: true\n" +
		"    skills: {include: [alpha], exclude: [beta]}\n" +
		"    commands: [run]\n"
	writeSourceExclusionFixture(t, path, original)
	entry := RegistryEntry{URL: source, Ref: "main", Path: "plugins/tools"}
	if changed, err := EditSourceSelection(path, path, entry, false); err != nil || !changed {
		t.Fatalf("legacy deselect = changed %v, err %v", changed, err)
	}
	got, ok := loadLocalEntry(t, path, source)
	if !ok || got.Default || len(got.Tags) != 0 || !got.ExcludeAll || len(got.Skills.Include) != 0 || len(got.Skills.Exclude) != 1 || got.Skills.Exclude[0] != "beta" || len(got.Commands.Include) != 0 {
		t.Fatalf("deselected legacy entry = %+v (found %v)", got, ok)
	}
	data := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceSelection(path, path, entry, false); err != nil || changed {
		t.Fatalf("repeated legacy deselect = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != data {
		t.Fatalf("repeated legacy deselect rewrote the file:\n%s", got)
	}
}

func TestEditSourceSelectionLegacyDeselectKeepsEntryWithoutSelections(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := LocalConfigPath(dir)
	source := "https://example.test/team.git"
	original := "registries:\n" +
		"  - url: " + source + "\n" +
		"    ref: main\n" +
		"    path: plugins/tools\n" +
		"    default: true\n" +
		"    tags: [stable]\n" +
		"    skills: [alpha]\n"
	writeSourceExclusionFixture(t, path, original)

	entry := RegistryEntry{URL: source, Ref: "main", Path: "plugins/tools"}
	if changed, err := EditSourceSelection(path, path, entry, false); err != nil || !changed {
		t.Fatalf("legacy deselect = changed %v, err %v", changed, err)
	}
	got, ok := loadLocalEntry(t, path, source)
	if !ok {
		t.Fatal("deselect dropped the registry entry")
	}
	if got.Default || len(got.Tags) != 0 || got.Skills.Include != nil {
		t.Fatalf("deselected entry still selects resources: %+v", got)
	}
	if data := readSourceExclusionFixture(t, path); !strings.Contains(data, "url: "+source) ||
		!strings.Contains(data, "ref: main") || !strings.Contains(data, "path: plugins/tools") {
		t.Fatalf("deselected entry declaration was not kept:\n%s", data)
	}
}

func TestEditSourceSelectionLegacyRequiredAndEmptyURL(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	original := "registries:\n" +
		"  - url: https://example.test/team.git\n" +
		"    ref: main\n" +
		"    required: true\n" +
		"    default: true\n"
	writeSourceExclusionFixture(t, path, original)
	entry := RegistryEntry{URL: "https://example.test/team.git", Ref: "main"}
	for _, selectAll := range []bool{true, false} {
		changed, err := EditSourceSelection(path, path, entry, selectAll)
		if changed || err == nil {
			t.Fatalf("required edit selectAll=%v = changed %v, err %v", selectAll, changed, err)
		}
		if got := readSourceExclusionFixture(t, path); got != original {
			t.Fatalf("required edit selectAll=%v changed the file:\n%s", selectAll, got)
		}
	}

	empty := filepath.Join(dir, ".agents", "empty.yaml")
	if changed, err := EditSourceSelection(empty, empty, RegistryEntry{}, true); changed || err == nil {
		t.Fatalf("empty URL = changed %v, err %v", changed, err)
	}
	if _, err := os.Stat(empty); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty URL created a file: %v", err)
	}
}

func TestEditSourceSelectionLegacySelectionInheritance(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := "https://example.test/team.git"
	entry := RegistryEntry{URL: source, Ref: "main", Path: "plugins/tools"}

	t.Run("select rejects inherited tags", func(t *testing.T) {
		path := ProjectConfigPath(dir)
		original := "apiVersion: agent-marketplace/v2\n" +
			"tags: [stable]\n" +
			"registries:\n" +
			"  - url: " + source + "\n" +
			"    ref: main\n" +
			"    path: plugins/tools\n"
		writeSourceExclusionFixture(t, path, original)

		changed, err := EditSourceSelection(path, path, entry, true)
		if changed || err == nil || !strings.Contains(err.Error(), "top-level tags") {
			t.Fatalf("select with inherited tags = changed %v, err %v", changed, err)
		}
		if got := readSourceExclusionFixture(t, path); got != original {
			t.Fatalf("rejected inherited-tag edit changed the file:\n%s", got)
		}
	})

	for _, selector := range []string{
		"skills: {include: [alpha]}",
		"skills: [alpha]",
	} {
		t.Run("select overrides inherited "+strings.ReplaceAll(selector, " ", "-"), func(t *testing.T) {
			path := filepath.Join(dir, "select-"+strings.ReplaceAll(strings.Trim(selector, "[]{}: "), " ", "-")+".yaml")
			original := "apiVersion: agent-marketplace/v2\n" +
				selector + "\n" +
				"registries:\n" +
				"  - url: " + source + "\n" +
				"    ref: main\n" +
				"    path: plugins/tools\n"
			writeSourceExclusionFixture(t, path, original)

			if changed, err := EditSourceSelection(path, path, entry, true); err != nil || !changed {
				t.Fatalf("select with inherited include = changed %v, err %v", changed, err)
			}
			loaded, err := LoadConfigFromPath(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Registries) != 1 || !loaded.Registries[0].Default ||
				loaded.Registries[0].Skills.Include == nil || len(loaded.Registries[0].Skills.Include) != 0 {
				t.Fatalf("selected entry did not override inherited include: %+v", loaded.Registries)
			}
			data := readSourceExclusionFixture(t, path)
			if changed, err := EditSourceSelection(path, path, entry, true); err != nil || changed {
				t.Fatalf("repeated select with inherited include = changed %v, err %v", changed, err)
			}
			if got := readSourceExclusionFixture(t, path); got != data {
				t.Fatalf("repeated select with inherited include rewrote the file:\n%s", got)
			}
		})
	}

	t.Run("deselect overrides inherited tags and include", func(t *testing.T) {
		path := filepath.Join(dir, "deselect-inherited.yaml")
		original := "apiVersion: agent-marketplace/v2\n" +
			"tags: [stable]\n" +
			"skills: {include: [alpha]}\n" +
			"registries:\n" +
			"  - url: " + source + "\n" +
			"    ref: main\n" +
			"    path: plugins/tools\n" +
			"    default: true\n"
		writeSourceExclusionFixture(t, path, original)

		if changed, err := EditSourceSelection(path, path, entry, false); err != nil || !changed {
			t.Fatalf("deselect with inherited selectors = changed %v, err %v", changed, err)
		}
		loaded, err := LoadConfigFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Registries) != 1 || loaded.Registries[0].Default ||
			loaded.Registries[0].Tags == nil || len(loaded.Registries[0].Tags) != 0 ||
			loaded.Registries[0].Skills.Include == nil || len(loaded.Registries[0].Skills.Include) != 0 {
			t.Fatalf("deselected entry did not override inherited selectors: %+v", loaded.Registries)
		}
		data := readSourceExclusionFixture(t, path)
		if changed, err := EditSourceSelection(path, path, entry, false); err != nil || changed {
			t.Fatalf("repeated deselect with inherited selectors = changed %v, err %v", changed, err)
		}
		if got := readSourceExclusionFixture(t, path); got != data {
			t.Fatalf("repeated deselect with inherited selectors rewrote the file:\n%s", got)
		}
	})
}

func TestEditSourceSelectionLegacyLocalSelectorsAreNotInherited(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := LocalConfigPath(dir)
	source := "https://example.test/team.git"
	original := "skills: {include: [alpha]}\n" +
		"registries:\n" +
		"  - url: " + source + "\n" +
		"    ref: main\n" +
		"    path: plugins/tools\n" +
		"    tags: [entry-tag]\n" +
		"    skills: {include: [entry-skill]}\n"
	writeSourceExclusionFixture(t, path, original)
	entry := RegistryEntry{URL: source, Ref: "main", Path: "plugins/tools"}

	if changed, err := EditSourceSelection(path, path, entry, true); err != nil || !changed {
		t.Fatalf("local select all = changed %v, err %v", changed, err)
	}
	selected, ok := loadLocalEntry(t, path, source)
	if !ok || !selected.Default || len(selected.Tags) != 0 || selected.Skills.Include != nil {
		t.Fatalf("local selectors were treated as inherited: %+v (found %v)", selected, ok)
	}
	selectedData := readSourceExclusionFixture(t, path)
	if !strings.Contains(selectedData, "skills: {include: [alpha]}") {
		t.Fatalf("local top-level selectors changed:\n%s", selectedData)
	}

	if changed, err := EditSourceSelection(path, path, entry, false); err != nil || !changed {
		t.Fatalf("local deselect = changed %v, err %v", changed, err)
	}
	deselected, ok := loadLocalEntry(t, path, source)
	if !ok || deselected.Default || deselected.Tags != nil || deselected.Skills.Include != nil {
		t.Fatalf("local deselect wrote inherited-selector overrides: %+v (found %v)", deselected, ok)
	}
}
