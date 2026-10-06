package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeSourceExclusionFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o640); err != nil {
		t.Fatal(err)
	}
}

func readSourceExclusionFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func loadLocalEntry(t *testing.T, path, url string) (RegistryEntry, bool) {
	t.Helper()
	local, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("load local config: %v\n%s", err, readSourceExclusionFixture(t, path))
	}
	if local == nil {
		return RegistryEntry{}, false
	}
	for _, entry := range local.Registries {
		if entry.URL == url {
			return entry, true
		}
	}
	return RegistryEntry{}, false
}

func TestEditSourceExclusionLegacyRoundTripKeepsSelections(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	other := filepath.Join(dir, "plugins", "other")
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	writeSourceExclusionFixture(t, path, "registries:\n"+
		"  - url: "+source+"\n    skills:\n      include: [alpha]\n      exclude: [beta]\n    commands:\n      exclude_all: true\n"+
		"  - url: "+other+"\n    skills:\n      exclude: [beta]\n")
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll)
	if err != nil || !changed {
		t.Fatalf("exclude all = changed %v, err %v", changed, err)
	}
	got, ok := loadLocalEntry(t, path, source)
	if !ok || !got.ExcludeAll || !slices.Equal(got.Skills.Include, []string{"alpha"}) || !slices.Equal(got.Skills.Exclude, []string{"beta"}) {
		t.Fatalf("excluded entry = %+v (found %v)", got, ok)
	}
	excluded := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll); err != nil || changed {
		t.Fatalf("repeated exclude all = changed %v, err %v", changed, err)
	}
	if again := readSourceExclusionFixture(t, path); again != excluded {
		t.Fatalf("repeated exclude all rewrote the file:\n%s", again)
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("clear exclude all = changed %v, err %v", changed, err)
	}
	got, _ = loadLocalEntry(t, path, source)
	if got.ExcludeAll || !slices.Equal(got.Skills.Include, []string{"alpha"}) || !slices.Equal(got.Skills.Exclude, []string{"beta"}) || !got.Commands.ExcludeAll {
		t.Fatalf("clearing the source-wide exclusion changed other selections: %+v", got)
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceClearResourceExclusions); err != nil || !changed {
		t.Fatalf("clear resource exclusions = changed %v, err %v", changed, err)
	}
	got, _ = loadLocalEntry(t, path, source)
	if len(got.Skills.Exclude) != 0 || got.Commands.ExcludeAll || !slices.Equal(got.Skills.Include, []string{"alpha"}) {
		t.Fatalf("resource exclusions not cleared or include lost: %+v", got)
	}
	if strings.Contains(readSourceExclusionFixture(t, path), "commands") {
		t.Fatalf("emptied kind selector left behind:\n%s", readSourceExclusionFixture(t, path))
	}
	sibling, _ := loadLocalEntry(t, path, other)
	if !slices.Equal(sibling.Skills.Exclude, []string{"beta"}) {
		t.Fatalf("another source's exclusion changed: %+v", sibling)
	}
}

func TestEditSourceExclusionLegacyCreatesAndDropsShadowEntry(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	entry := RegistryEntry{URL: "https://example.test/team.git", Ref: "main", Path: "plugins/tools"}

	for _, edit := range []SourceExclusionEdit{SourceClearExcludeAll, SourceClearResourceExclusions} {
		if changed, err := EditSourceExclusion(path, path, entry, edit); err != nil || changed {
			t.Fatalf("%s on a missing file = changed %v, err %v", edit, changed, err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s created the missing file: %v", edit, err)
		}
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude all = changed %v, err %v", changed, err)
	}
	got, ok := loadLocalEntry(t, path, entry.URL)
	if !ok || !got.ExcludeAll || got.Ref != "main" || got.Path != "plugins/tools" {
		t.Fatalf("created entry = %+v (found %v)", got, ok)
	}
	// A different path of the same repository is another source.
	if changed, err := EditSourceExclusion(path, path, RegistryEntry{URL: entry.URL, Ref: "main", Path: "plugins/other"}, SourceClearExcludeAll); err != nil || changed {
		t.Fatalf("clearing a sibling path = changed %v, err %v", changed, err)
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("clear exclude all = changed %v, err %v", changed, err)
	}
	if data := readSourceExclusionFixture(t, path); strings.Contains(data, "registries") {
		t.Fatalf("shadow entry left behind:\n%s", data)
	}
	if _, ok := loadLocalEntry(t, path, entry.URL); ok {
		t.Fatal("shadow entry still loads")
	}
}

func TestEditSourceExclusionV4RoundTripKeepsSelections(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	writeSourceExclusionFixture(t, path, "apiVersion: agent-marketplace/v4\n# keep this comment\nsources:\n  tools: {directory: "+source+"}\nresources:\n  tools:\n    include: [skill/alpha]\n    exclude: [skill/beta]\n")
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	if changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude all = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if group := loaded.Groups["tools"]; !slices.Equal(group.Exclude, []string{"skill/beta", "*"}) || !groupIncludes(group, "skill/alpha") {
		t.Fatalf("excluded group = include %v exclude %v", group.Include, group.Exclude)
	}
	if got, _ := loadLocalEntry(t, path, source); !got.ExcludeAll {
		t.Fatalf("runtime entry does not exclude the source: %+v", got)
	}
	if !strings.Contains(readSourceExclusionFixture(t, path), "# keep this comment") {
		t.Fatal("comment lost")
	}
	excluded := readSourceExclusionFixture(t, path)
	if changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll); err != nil || changed {
		t.Fatalf("repeated exclude all = changed %v, err %v", changed, err)
	}
	if again := readSourceExclusionFixture(t, path); again != excluded {
		t.Fatalf("repeated exclude all rewrote the file:\n%s", again)
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("clear exclude all = changed %v, err %v", changed, err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if group := loaded.Groups["tools"]; !slices.Equal(group.Exclude, []string{"skill/beta"}) || !groupIncludes(group, "skill/alpha") {
		t.Fatalf("cleared group = include %v exclude %v", group.Include, group.Exclude)
	}

	if changed, err := EditSourceExclusion(path, path, entry, SourceClearResourceExclusions); err != nil || !changed {
		t.Fatalf("clear resource exclusions = changed %v, err %v", changed, err)
	}
	loaded, err = LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if group := loaded.Groups["tools"]; len(group.Exclude) != 0 || !groupIncludes(group, "skill/alpha") {
		t.Fatalf("resource exclusions not cleared or include lost: include %v exclude %v", group.Include, group.Exclude)
	}
	if _, ok := loaded.Sources["tools"]; !ok {
		t.Fatal("source that still selects resources was dropped")
	}
}

func TestEditSourceExclusionV4DeclaresAndDropsSourceThroughStagedCopy(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "apiVersion: agent-marketplace/v4\n"
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	writeSourceExclusionFixture(t, path, original)
	stage := filepath.Join(t.TempDir(), "config.local.yaml")
	writeSourceExclusionFixture(t, stage, original)
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory}

	if changed, err := EditSourceExclusion(stage, path, entry, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude all = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("staged edit wrote the real root:\n%s", got)
	}
	staged := readSourceExclusionFixture(t, stage)
	if !strings.Contains(staged, "directory: "+source) || !strings.Contains(staged, `- '*'`) {
		t.Fatalf("staged root did not declare and exclude the source:\n%s", staged)
	}

	if changed, err := EditSourceExclusion(stage, path, entry, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("clear exclude all = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, stage); got != original {
		t.Fatalf("clearing left the declaration behind:\n%s", got)
	}
}

func TestEditSourceExclusionV4RefusesInheritedWildcard(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSourceExclusionFixture(t, filepath.Join(dir, ".agents", "fragment.yaml"), "sources:\n  tools: {directory: "+source+"}\nresources:\n  tools: {exclude: ['*']}\n")
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	root := "apiVersion: agent-marketplace/v4\ninclude: fragment.yaml\nresources:\n  tools: {exclude: ['*', skill/beta]}\n"
	writeSourceExclusionFixture(t, path, root)

	_, err := EditSourceExclusion(path, path, RegistryEntry{URL: source, V4Kind: V4SourceDirectory}, SourceClearExcludeAll)
	var validation *V4ValidationError
	if !errors.As(err, &validation) || validation.Code != "v4.edit-exclusion" || !strings.Contains(err.Error(), "fragment.yaml") {
		t.Fatalf("error = %T %v", err, err)
	}
	if got := readSourceExclusionFixture(t, path); got != root {
		t.Fatalf("refused edit changed the root:\n%s", got)
	}
}

func loadLocalEntryForLayout(t *testing.T, path, url, harness string) (RegistryEntry, bool) {
	t.Helper()
	local, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatalf("load local config: %v\n%s", err, readSourceExclusionFixture(t, path))
	}
	if local == nil {
		return RegistryEntry{}, false
	}
	for _, entry := range local.Registries {
		if entry.URL == url && entry.SourceHarness == harness {
			return entry, true
		}
	}
	return RegistryEntry{}, false
}

// spec: tui-source-toggle/source-layouts-retain-separate-menu-identities
func TestEditSourceExclusionLegacyKeepsSourceLayoutsSeparate(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	original := "registries:\n  - url: " + source + "\n    source_harness: factory\n    exclude_all: true\n"
	writeSourceExclusionFixture(t, path, original)
	claude := RegistryEntry{URL: source, V4Kind: V4SourceDirectory, SourceHarness: "claude"}

	if changed, err := EditSourceExclusion(path, path, claude, SourceClearExcludeAll); err != nil || changed {
		t.Fatalf("clearing another layout = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("clearing another layout rewrote the factory exclusion:\n%s", got)
	}

	if changed, err := EditSourceExclusion(path, path, claude, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude claude layout = changed %v, err %v", changed, err)
	}
	if got, ok := loadLocalEntryForLayout(t, path, source, "claude"); !ok || !got.ExcludeAll {
		t.Fatalf("claude layout not excluded: %+v (found %v)\n%s", got, ok, readSourceExclusionFixture(t, path))
	}
	if got, ok := loadLocalEntryForLayout(t, path, source, "factory"); !ok || !got.ExcludeAll {
		t.Fatalf("factory exclusion changed: %+v (found %v)", got, ok)
	}

	if changed, err := EditSourceExclusion(path, path, claude, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("restore claude layout = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("restoring the claude layout left other changes:\n%s", got)
	}
}

// spec: tui-source-toggle/source-layouts-retain-separate-menu-identities
func TestEditSourceExclusionLegacyAutodetectedEntryMatchesAnyLayout(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	writeSourceExclusionFixture(t, path, "registries:\n  - url: "+source+"\n    skills:\n      include: [alpha]\n")
	entry := RegistryEntry{URL: source, V4Kind: V4SourceDirectory, SourceHarness: "claude"}

	if changed, err := EditSourceExclusion(path, path, entry, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude all = changed %v, err %v", changed, err)
	}
	local, err := LoadLocalConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(local.Registries) != 1 || !local.Registries[0].ExcludeAll || local.Registries[0].SourceHarness != "" {
		t.Fatalf("autodetected entry was not edited in place: %+v", local.Registries)
	}
}

// spec: tui-source-toggle/source-layouts-retain-separate-menu-identities
func TestEditSourceExclusionV4KeepsSourceLayoutsSeparate(t *testing.T) {
	dir := canonicalDirectoryTestRoot(t)
	source := filepath.Join(dir, "plugins", "tools")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".agents", "config.local.yaml")
	original := "apiVersion: agent-marketplace/v4\nsources:\n  tools: {directory: " + source + ", source_harness: factory}\nresources:\n  tools: {exclude: ['*']}\n"
	writeSourceExclusionFixture(t, path, original)
	claude := RegistryEntry{URL: source, V4Kind: V4SourceDirectory, SourceHarness: "claude"}

	if changed, err := EditSourceExclusion(path, path, claude, SourceClearExcludeAll); err != nil || changed {
		t.Fatalf("clearing another layout = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("clearing another layout rewrote the factory exclusion:\n%s", got)
	}

	if changed, err := EditSourceExclusion(path, path, claude, SourceExcludeAll); err != nil || !changed {
		t.Fatalf("exclude claude layout = changed %v, err %v", changed, err)
	}
	loaded, err := LoadV4ConfigFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	var claudeSource string
	for name, declared := range loaded.Sources {
		if declared.SourceHarness == "claude" {
			claudeSource = name
		}
	}
	if claudeSource == "" || claudeSource == "tools" || !slices.Equal(loaded.Groups[claudeSource].Exclude, []string{"*"}) {
		t.Fatalf("claude layout not excluded as its own source:\n%s", readSourceExclusionFixture(t, path))
	}
	if !slices.Equal(loaded.Groups["tools"].Exclude, []string{"*"}) || loaded.Sources["tools"].SourceHarness != "factory" {
		t.Fatalf("factory exclusion changed:\n%s", readSourceExclusionFixture(t, path))
	}

	if changed, err := EditSourceExclusion(path, path, claude, SourceClearExcludeAll); err != nil || !changed {
		t.Fatalf("restore claude layout = changed %v, err %v", changed, err)
	}
	if got := readSourceExclusionFixture(t, path); got != original {
		t.Fatalf("restoring the claude layout left other changes:\n%s", got)
	}
}

func groupIncludes(group V4ResourceGroup, locator string) bool {
	return slices.ContainsFunc(group.Include, func(request V4ResourceRequest) bool { return request.Locator == locator })
}
