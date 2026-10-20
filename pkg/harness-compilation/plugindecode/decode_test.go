package plugindecode_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/plugindecode"
)

const agentSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
const agentMcpSchemaID = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

func writePackage(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func decode(t *testing.T, files map[string]string, format string) (*ir.Plugin, []*diag.Diagnostic, error) {
	t.Helper()
	return plugindecode.Decode(writePackage(t, files), format)
}

func memberKeys(p *ir.Plugin) []string {
	var keys []string
	for _, m := range p.Members {
		keys = append(keys, m.Key())
	}
	return keys
}

func hasCode(diags []*diag.Diagnostic, code string) bool {
	for _, d := range diags {
		if d != nil && d.Code == code {
			return true
		}
	}
	return false
}

// spec: plugin-portable-format/unsupported-agent-plugins-version-is-refused
func TestDecodeAgentPluginUnsupportedVersionRefused(t *testing.T) {
	plugin, diags, err := decode(t, map[string]string{
		"plugin.json":       `{"$schema": "https://agent-plugins.org/schemas/1.1.0/plugin.schema.json", "name": "demo"}`,
		"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\nbody\n",
	}, "agent-plugin")
	if err == nil && plugin != nil {
		t.Fatal("an unsupported Agent Plugins version must be refused")
	}
	if plugin != nil {
		t.Fatal("a refused package must not produce a plugin")
	}
	found := err != nil && strings.Contains(err.Error(), "plugin.format.version-unsupported")
	if !found {
		found = hasCode(diags, "plugin.format.version-unsupported")
	}
	if !found {
		t.Fatalf("want plugin.format.version-unsupported, err=%v diags=%v", err, diags)
	}
}

// spec: plugin-portable-format/invalid-required-manifest-field-rejects-the-package
func TestDecodeAgentPluginInvalidRequiredFieldRejects(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		"plugin.json":       `{"$schema": "` + agentSchemaID + `"}`,
		"skills/s/SKILL.md": "---\nname: s\ndescription: d\n---\nbody\n",
	}, "agent-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a manifest without a valid required name must be rejected, plugin=%v err=%v", plugin, err)
	}
	if !strings.Contains(err.Error(), "plugin.format.manifest-invalid") {
		t.Fatalf("want plugin.format.manifest-invalid, got %v", err)
	}
}

// spec: plugin-portable-format/nested-skill-directories-are-not-discovered
func TestDecodeAgentPluginNestedSkillsNotDiscovered(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		"plugin.json":             `{"$schema": "` + agentSchemaID + `", "name": "demo"}`,
		"skills/a/SKILL.md":       "---\nname: a\ndescription: d\n---\nbody\n",
		"skills/group/b/SKILL.md": "---\nname: b\ndescription: d\n---\nbody\n",
	}, "agent-plugin")
	if err != nil {
		t.Fatal(err)
	}
	keys := memberKeys(plugin)
	if len(keys) != 1 || keys[0] != "skill/a" {
		t.Fatalf("member keys = %v, want [skill/a]", keys)
	}
}

// spec: plugin-portable-format/invalid-skill-is-skipped-while-siblings-remain
func TestDecodeAgentPluginInvalidSkillSkipped(t *testing.T) {
	plugin, diags, err := decode(t, map[string]string{
		"plugin.json":          `{"$schema": "` + agentSchemaID + `", "name": "demo"}`,
		"skills/good/SKILL.md": "---\nname: good\ndescription: d\n---\nbody\n",
		"skills/bad/SKILL.md":  "---\nname: \"Not A Valid Name!!\"\ndescription: d\n---\nbody\n",
	}, "agent-plugin")
	if err != nil {
		t.Fatal(err)
	}
	keys := memberKeys(plugin)
	if len(keys) != 1 || keys[0] != "skill/good" {
		t.Fatalf("member keys = %v, want [skill/good]", keys)
	}
	if len(plugin.Invalid) == 0 {
		t.Fatal("the invalid skill must be recorded in Invalid")
	}
	if len(diags) == 0 {
		t.Fatal("the invalid skill must be reported with a diagnostic")
	}
}

func TestDecodeAgentPluginMcpServers(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		"plugin.json": `{"$schema": "` + agentSchemaID + `", "name": "demo"}`,
		"mcp.json":    `{"$schema": "` + agentMcpSchemaID + `", "mcpServers": {"srv": {"type": "stdio", "command": "./bin/s", "args": ["${PLUGIN_ROOT}/c"], "cwd": "${PLUGIN_ROOT}", "env": {"K": "v"}}}}`,
		"bin/s":       "#!/bin/sh\n",
	}, "agent-plugin")
	if err != nil {
		t.Fatal(err)
	}
	keys := memberKeys(plugin)
	if len(keys) != 1 || keys[0] != "mcpserver/srv" {
		t.Fatalf("member keys = %v, want [mcpserver/srv]", keys)
	}
	srv, ok := plugin.Members[0].Resource.(*ir.McpServer)
	if !ok {
		t.Fatalf("member resource is %T, want *ir.McpServer", plugin.Members[0].Resource)
	}
	if srv.Cwd != "${PLUGIN_ROOT}" {
		t.Fatalf("Cwd = %q, want literal ${PLUGIN_ROOT} (expansion is a projection concern)", srv.Cwd)
	}
}

func TestDecodeAgentPluginInvalidMcpEntrySkipped(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		"plugin.json": `{"$schema": "` + agentSchemaID + `", "name": "demo"}`,
		"mcp.json":    `{"$schema": "` + agentMcpSchemaID + `", "mcpServers": {"good": {"type": "stdio", "command": "run"}, "bad": {"type": "stdio"}}}`,
	}, "agent-plugin")
	if err != nil {
		t.Fatal(err)
	}
	keys := memberKeys(plugin)
	if len(keys) != 1 || keys[0] != "mcpserver/good" {
		t.Fatalf("member keys = %v, want [mcpserver/good]", keys)
	}
	if len(plugin.Invalid) != 1 {
		t.Fatalf("Invalid records = %v, want exactly the bad server", plugin.Invalid)
	}
}

func TestDecodeAgentPluginUnknownFieldAndBadExtensionsWarned(t *testing.T) {
	plugin, diags, err := decode(t, map[string]string{
		"plugin.json": `{"$schema": "` + agentSchemaID + `", "name": "demo", "surprise": true, "extensions": 42}`,
	}, "agent-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if plugin == nil {
		t.Fatal("unknown fields and a non-object extensions must not reject the package")
	}
	if len(diags) < 2 {
		t.Fatalf("want warnings for the unknown field and non-object extensions, got %v", diags)
	}
	if len(plugin.Extensions) != 0 {
		t.Fatalf("a non-object extensions field must be ignored, got %v", plugin.Extensions)
	}
}

func TestDecodeUnsupportedFormat(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/marketplace.json": `{"name": "m"}`,
	}, "claude-marketplace")
	if err == nil || plugin != nil {
		t.Fatalf("a catalog format is not a package, plugin=%v err=%v", plugin, err)
	}
	if !strings.Contains(err.Error(), "plugin.format.unsupported") {
		t.Fatalf("want plugin.format.unsupported, got %v", err)
	}
}

func TestDecodeClaudePlugin(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo", "version": "1.2.3", "custom": {"x": 1}}`,
		"skills/a/SKILL.md":          "---\nname: a\ndescription: d\n---\nbody\n",
		"agents/helper.md":           "---\nname: helper\ndescription: d\npermissionMode: plan\n---\nbody\n",
		"commands/run.md":            "---\ndescription: d\n---\nrun it\n",
		"hooks/hooks.json":           `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}]}]}`,
		".mcp.json":                  `{"mcpServers": {"srv": {"command": "npx", "args": ["-y", "srv"]}}}`,
	}, "claude-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if plugin.Format != "claude-plugin" || plugin.Name() != "demo" || plugin.Manifest.Version != "1.2.3" {
		t.Fatalf("plugin identity = %q %q %q", plugin.Format, plugin.Name(), plugin.Manifest.Version)
	}
	keys := memberKeys(plugin)
	want := []string{"subagent/helper", "command/run", "hook/PreToolUse-command", "mcpserver/srv", "skill/a"}
	if len(keys) != 5 {
		t.Fatalf("member keys = %v, want five members", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	for _, k := range want {
		if !seen[k] {
			t.Fatalf("member keys = %v, missing %q", keys, k)
		}
	}
	// Manifest extra fields are preserved for native formats.
	if plugin.Manifest.Extra["custom"] == nil {
		t.Fatalf("manifest extra lost: %v", plugin.Manifest.Extra)
	}
	if len(plugin.Files) == 0 {
		t.Fatal("the contained file set must not be empty")
	}
}

func TestDecodePluginRecordsDeclaredDependenciesInPackageIR(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name":"review-kit","dependencies":["base-kit"]}`,
		"skills/review/SKILL.md":     "---\nname: review\ndescription: d\n---\nbody\n",
	}, "claude-plugin")
	if err != nil {
		t.Fatalf("dependency-declaring plugin must decode for inspection: %v", err)
	}
	dependencies := reflect.ValueOf(plugin).Elem().FieldByName("Dependencies")
	if !dependencies.IsValid() {
		t.Fatal("plugin IR does not record declared dependencies")
	}
	if got := dependencies.Interface(); !reflect.DeepEqual(got, []any{"base-kit"}) {
		t.Fatalf("plugin dependencies = %#v, want []any{\"base-kit\"}", got)
	}
}

func TestDecodeDuplicateJSONKeyRejected(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo", "name": "evil"}`,
	}, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a duplicate manifest key must reject the package, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeMissingManifestRejected(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		"skills/a/SKILL.md": "---\nname: a\ndescription: d\n---\nbody\n",
	}, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a package without its manifest must be rejected, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeCaseFoldCollisionRejected(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"data/Readme.txt":            "a",
		"data/README.txt":            "b",
	}, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a case-fold collision must reject the package, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeSymlinkEscapeRejected(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
	})
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a symlink resolving outside the root must reject the package, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeSymlinkCycleRejected(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
	})
	if err := os.Symlink("loop", filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a symlink cycle must reject the package, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeOversizedManifestRejected(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo", "pad": "` + strings.Repeat("x", 1<<20) + `"}`,
	}, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("an oversized manifest read must reject the package, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeSpecialFileRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo creation is a unix facility")
	}
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
	})
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a FIFO inside the package must reject it, plugin=%v err=%v", plugin, err)
	}
}

func TestDecodeComponentDuplicateKeySkipped(t *testing.T) {
	plugin, diags, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"hooks/hooks.json":           `{"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}], "matcher": "evil"}]}`,
		".mcp.json":                  `{"mcpServers": {"srv": {"command": "npx"}}}`,
	}, "claude-plugin")
	if err != nil {
		t.Fatal(err)
	}
	keys := memberKeys(plugin)
	if len(keys) != 1 || keys[0] != "mcpserver/srv" {
		t.Fatalf("member keys = %v, want [mcpserver/srv]", keys)
	}
	if !hasCode(diags, "plugin.format.component-invalid") {
		t.Fatalf("want a plugin.format.component-invalid diagnostic, got %v", diags)
	}
	found := false
	for _, rec := range plugin.Invalid {
		if rec.Path == "hooks/hooks.json" && rec.Kind == "component" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the duplicate-key hooks file must be recorded in Invalid, got %v", plugin.Invalid)
	}
}

func TestDecodeSymlinkedFileRecorded(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"bin/real.sh":                "#!/bin/sh\n",
	})
	if err := os.Symlink("bin/real.sh", filepath.Join(root, "run.sh")); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err != nil {
		t.Fatal(err)
	}
	var link, target *ir.PluginFile
	for i := range plugin.Files {
		switch plugin.Files[i].Path {
		case "run.sh":
			link = &plugin.Files[i]
		case "bin/real.sh":
			target = &plugin.Files[i]
		}
	}
	if link == nil || target == nil {
		t.Fatalf("file set = %v, want both run.sh and bin/real.sh", plugin.Files)
	}
	if link.Digest != target.Digest {
		t.Fatalf("the link's digest %q must be the resolved content digest %q", link.Digest, target.Digest)
	}
}

func TestDecodeSymlinkedDirRecorded(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"adir/f.txt":                 "a\n",
		"zdir/g.txt":                 "z\n",
	})
	// Both link orders must decode: a link that sorts before its target and a
	// link that sorts after it are equally valid under Agent Plugins §4.1.
	for link, dst := range map[string]string{"linked": "adir", "alink": "zdir", "zlink": "adir"} {
		if err := os.Symlink(dst, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"linked/f.txt": false,
		"alink/g.txt":  false,
		"zlink/f.txt":  false,
		"adir/f.txt":   false,
		"zdir/g.txt":   false,
	}
	for _, f := range plugin.Files {
		if _, ok := want[f.Path]; ok {
			want[f.Path] = true
		}
	}
	for p, seen := range want {
		if !seen {
			t.Fatalf("file set = %v, missing %q", plugin.Files, p)
		}
	}
}

func TestDecodeSymlinkToAncestorRejected(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"skills/s/x.txt":             "x\n",
	})
	if err := os.Symlink("..", filepath.Join(root, "skills", "loop")); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a link to an ancestor directory must reject the package, plugin=%v err=%v", plugin, err)
	}
	if !strings.Contains(err.Error(), "plugin.package.symlink-cycle") {
		t.Fatalf("want plugin.package.symlink-cycle, got %v", err)
	}
}

func TestDecodeSymlinkToRootRejected(t *testing.T) {
	root := writePackage(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
	})
	if err := os.Symlink(".", filepath.Join(root, "back")); err != nil {
		t.Fatal(err)
	}
	plugin, _, err := plugindecode.Decode(root, "claude-plugin")
	if err == nil || plugin != nil {
		t.Fatalf("a link to the package root must reject the package, plugin=%v err=%v", plugin, err)
	}
	if !strings.Contains(err.Error(), "plugin.package.symlink-cycle") {
		t.Fatalf("want plugin.package.symlink-cycle, got %v", err)
	}
}

func TestDecodeContainedFileSet(t *testing.T) {
	plugin, _, err := decode(t, map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo"}`,
		"bin/run.sh":                 "#!/bin/sh\n",
	}, "claude-plugin")
	if err != nil {
		t.Fatal(err)
	}
	var foundManifest, foundScript bool
	for _, f := range plugin.Files {
		if strings.Contains(f.Path, "..") || strings.HasPrefix(f.Path, "/") {
			t.Fatalf("file path %q is not clean and slash-relative", f.Path)
		}
		if len(f.Digest) != 64 {
			t.Fatalf("digest %q is not a sha256 hex digest", f.Digest)
		}
		if f.Path == ".claude-plugin/plugin.json" {
			foundManifest = true
		}
		if f.Path == "bin/run.sh" {
			foundScript = true
		}
	}
	if !foundManifest || !foundScript {
		t.Fatalf("file set = %v, want the manifest and the script", plugin.Files)
	}
}
