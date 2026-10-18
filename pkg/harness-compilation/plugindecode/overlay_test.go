package plugindecode_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

// decodeCatalogEntry decodes the catalog rooted in files and decodes the
// named entry's plugin root through the catalog's entry-overlay policy.
func decodeCatalogEntry(t *testing.T, files map[string]string, format, entryName string) (*plugindecode.CatalogEntry, *ir.Plugin, []*diag.Diagnostic, error) {
	t.Helper()
	root := writePackage(t, files)
	catalog, _, err := plugindecode.DecodeCatalog(root, format)
	if err != nil {
		t.Fatalf("DecodeCatalog() error = %v", err)
	}
	for i := range catalog.Entries {
		entry := &catalog.Entries[i]
		if entry.Name != entryName {
			continue
		}
		pluginRoot := root
		if entry.Source.Kind == "path" && entry.Source.Path != "." {
			pluginRoot = filepath.Join(root, filepath.FromSlash(entry.Source.Path))
		}
		plugin, diags, err := plugindecode.DecodeEntry(pluginRoot, format, *entry)
		return entry, plugin, diags, err
	}
	t.Fatalf("catalog entry %q not found", entryName)
	return nil, nil, nil, nil
}

func memberKeySet(p *ir.Plugin) map[string]bool {
	set := map[string]bool{}
	for _, m := range p.Members {
		set[m.Key()] = true
	}
	return set
}

func hookCommands(p *ir.Plugin, event string) []string {
	var commands []string
	for _, m := range p.Members {
		if h, ok := m.Resource.(*ir.Hook); ok && h.Event == event {
			commands = append(commands, h.Command)
		}
	}
	return commands
}

func invalidRecords(p *ir.Plugin) []ir.PluginRecord {
	return p.Invalid
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestDecodeEntryStrictAppendsComponentsAndReplacesHookEvents(t *testing.T) {
	_, plugin, diags, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "kit", "source": "./kit",
			 "description": "entry description",
			 "skills": ["./more/extra"],
			 "commands": "./more/cmd.md",
			 "agents": ["./more/agents"],
			 "mcpServers": {"srv": {"command": "x"}},
			 "outputStyles": ["./styles"],
			 "hooks": {"PostToolUse": [{"matcher": "Y", "hooks": [{"type": "command", "command": "entry"}]}]}}]}`,
		"kit/.claude-plugin/plugin.json": `{"name": "kit"}`,
		"kit/skills/base/SKILL.md":       "---\nname: base\ndescription: d\n---\nbody\n",
		"kit/more/extra/SKILL.md":        "---\nname: extra\ndescription: d\n---\nbody\n",
		"kit/more/cmd.md":                "---\ndescription: d\n---\nrun it\n",
		"kit/more/agents/helper.md":      "---\nname: helper\ndescription: d\npermissionMode: plan\n---\nbody\n",
		"kit/hooks/hooks.json": `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "keep"}]}],
			"PostToolUse": [{"matcher": "X", "hooks": [{"type": "command", "command": "orig"}]}]}`,
	}, "claude-marketplace", "kit")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	keys := memberKeySet(plugin)
	for _, want := range []string{"skill/base", "skill/extra", "command/cmd", "subagent/helper"} {
		if !keys[want] {
			t.Fatalf("member keys = %v, missing %q", keys, want)
		}
	}
	if keys["mcpserver/srv"] {
		t.Fatalf("entry mcpServers must not apply when plugin.json is present: %v", keys)
	}
	if got := hookCommands(plugin, "PostToolUse"); len(got) != 1 || got[0] != "entry" {
		t.Fatalf("PostToolUse hooks = %v, want only the entry's matcher", got)
	}
	if got := hookCommands(plugin, "PreToolUse"); len(got) != 1 || got[0] != "keep" {
		t.Fatalf("PreToolUse hooks = %v, want the manifest hook preserved", got)
	}
	var sawStyle bool
	for _, rec := range invalidRecords(plugin) {
		if rec.Path == "outputStyles" {
			sawStyle = true
		}
	}
	if !sawStyle {
		t.Fatalf("outputStyles must be recorded in Invalid, got %v", plugin.Invalid)
	}
	if !hasCode(diags, "plugin.format.component-invalid") {
		t.Fatalf("unsupported outputStyles must produce a diagnostic, got %v", diags)
	}
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestDecodeEntryWithoutManifestEntryIsTheManifest(t *testing.T) {
	_, plugin, _, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "nomani", "source": "./nomani",
			 "description": "entry description",
			 "version": "9.9.9",
			 "skills": ["./more/extra"],
			 "mcpServers": {"srv": {"command": "x"}},
			 "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "entry"}]}]}}]}`,
		"nomani/more/extra/SKILL.md": "---\nname: extra\ndescription: d\n---\nbody\n",
	}, "claude-marketplace", "nomani")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	if plugin == nil {
		t.Fatal("an entry without plugin.json must still decode through the entry manifest")
	}
	if plugin.Manifest.Name != "nomani" || plugin.Manifest.Description != "entry description" || plugin.Manifest.Version != "9.9.9" {
		t.Fatalf("manifest = %+v, want the entry's manifest fields", plugin.Manifest)
	}
	keys := memberKeySet(plugin)
	for _, want := range []string{"skill/extra", "mcpserver/srv"} {
		if !keys[want] {
			t.Fatalf("member keys = %v, missing %q", keys, want)
		}
	}
	if got := hookCommands(plugin, "PreToolUse"); len(got) != 1 || got[0] != "entry" {
		t.Fatalf("PreToolUse hooks = %v, want the entry's hook", got)
	}
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestDecodeEntryStrictFalseWithoutComponentsKeepsManifest(t *testing.T) {
	_, plugin, _, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "plain", "source": "./plain", "strict": false, "description": "entry description"}]}`,
		"plain/.claude-plugin/plugin.json": `{"name": "plain", "description": "manifest description"}`,
		"plain/skills/a/SKILL.md":          "---\nname: a\ndescription: d\n---\nbody\n",
	}, "claude-marketplace", "plain")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	keys := memberKeySet(plugin)
	if len(keys) != 1 || !keys["skill/a"] {
		t.Fatalf("member keys = %v, want only skill/a", keys)
	}
}

// spec: plugin-packages/relative-entry-resolves-against-the-catalog-root
func TestDecodeEntryStrictFalseWithComponentsConflicts(t *testing.T) {
	_, plugin, _, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "conflict", "source": "./conflict", "strict": false, "skills": ["./more/extra"]},
			{"name": "ok", "source": "./ok"}]}`,
		"conflict/.claude-plugin/plugin.json": `{"name": "conflict"}`,
		"conflict/more/extra/SKILL.md":        "---\nname: extra\ndescription: d\n---\nbody\n",
		"ok/.claude-plugin/plugin.json":       `{"name": "ok"}`,
	}, "claude-marketplace", "conflict")
	if err == nil {
		t.Fatal("a strict:false entry with component fields must refuse")
	}
	if plugin != nil {
		t.Fatal("a conflicting entry must not produce a plugin")
	}
	coded, ok := diag.AsCodedError(err)
	if !ok || coded.Code != "plugin.catalog.manifest-conflict" {
		t.Fatalf("error = %v, want plugin.catalog.manifest-conflict", err)
	}
	// The sibling entry must stay decodable.
	_, sibling, _, siblingErr := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "conflict", "source": "./conflict", "strict": false, "skills": ["./more/extra"]},
			{"name": "ok", "source": "./ok"}]}`,
		"conflict/.claude-plugin/plugin.json": `{"name": "conflict"}`,
		"ok/.claude-plugin/plugin.json":       `{"name": "ok"}`,
		"ok/skills/a/SKILL.md":                "---\nname: a\ndescription: d\n---\nbody\n",
	}, "claude-marketplace", "ok")
	if siblingErr != nil || sibling == nil {
		t.Fatalf("the non-conflicting sibling must stay installable: plugin=%v err=%v", sibling, siblingErr)
	}
}

func TestDecodeEntryComponentPathEscapeRefused(t *testing.T) {
	_, plugin, diags, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "kit", "source": "./kit", "skills": ["../outside"]}]}`,
		"kit/.claude-plugin/plugin.json": `{"name": "kit"}`,
		"kit/skills/base/SKILL.md":       "---\nname: base\ndescription: d\n---\nbody\n",
	}, "claude-marketplace", "kit")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	keys := memberKeySet(plugin)
	if keys["skill/base"] != true {
		t.Fatalf("member keys = %v, want the manifest skill to survive", keys)
	}
	var sawEscape bool
	for _, rec := range invalidRecords(plugin) {
		if strings.Contains(rec.Path, "..") {
			sawEscape = true
		}
	}
	if !sawEscape {
		t.Fatalf("the escaping entry path must be recorded in Invalid, got %v", plugin.Invalid)
	}
	if len(diags) == 0 {
		t.Fatal("the escaping entry path must produce a diagnostic")
	}
}

func TestDecodeEntryNonObjectHooksRecorded(t *testing.T) {
	_, plugin, _, err := decodeCatalogEntry(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "kit", "source": "./kit", "hooks": "./hooks/hooks.json"}]}`,
		"kit/.claude-plugin/plugin.json": `{"name": "kit"}`,
		"kit/hooks/hooks.json":           `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "x"}]}]}`,
	}, "claude-marketplace", "kit")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	var sawHooks bool
	for _, rec := range invalidRecords(plugin) {
		if rec.Path == "hooks" {
			sawHooks = true
		}
	}
	if !sawHooks {
		t.Fatalf("a file-path entry hooks value must be recorded in Invalid, got %v", plugin.Invalid)
	}
}

func TestDecodeEntryFormatWithoutOverlayPolicyIgnoresEntryFields(t *testing.T) {
	entry, plugin, _, err := decodeCatalogEntry(t, map[string]string{
		".factory-plugin/marketplace.json": `{"name": "team", "plugins": [
			{"name": "kit", "source": "./kit", "strict": false, "skills": ["./more/extra"]}]}`,
		"kit/.factory-plugin/plugin.json": `{"name": "kit"}`,
		"kit/skills/a/SKILL.md":           "---\nname: a\ndescription: d\n---\nbody\n",
		"kit/more/extra/SKILL.md":         "---\nname: extra\ndescription: d\n---\nbody\n",
	}, "factory-marketplace", "kit")
	if err != nil {
		t.Fatalf("DecodeEntry() error = %v", err)
	}
	if entry == nil || plugin == nil {
		t.Fatal("a format without an overlay policy must decode plainly")
	}
	keys := memberKeySet(plugin)
	if len(keys) != 1 || !keys["skill/a"] {
		t.Fatalf("member keys = %v, want only the package skill; entry fields must not apply", keys)
	}
}
