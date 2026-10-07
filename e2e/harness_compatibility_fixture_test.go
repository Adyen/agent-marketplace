package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type compatibilityFixture struct {
	registry   string
	prefix     string
	server     string
	hookScript string
	hookMarker string
}

func buildCompatibilityHelper(t *testing.T, name string) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), name)
	root := filepath.Dir(mustGetwd(t))
	cmd := exec.Command("go", "build", "-o", binary, filepath.Join(root, "e2e", "testdata", name, "main.go"))
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	return binary
}

func newCompatibilityFixture(e *Env, target, server string) compatibilityFixture {
	f := compatibilityFixture{
		registry:   filepath.Join(e.repos, "canonical"),
		prefix:     "amcompat-" + target,
		server:     server,
		hookScript: filepath.Join(e.repos, "harmless-hook.sh"),
		hookMarker: filepath.Join(e.repos, "hook-events"),
	}
	e.WriteFile(f.hookScript, fmt.Sprintf("printf '%%s\\n' \"$1\" >> %q\n", f.hookMarker))
	files := RegistryFilesEx(
		[]Skill{{Name: f.name("skill"), Description: f.description("skill"), Tags: []string{"compatibility"}}},
		[]Command{{Name: f.name("command"), Description: f.description("command"), Command: "printf compatibility"}},
		[]Subagent{{Name: f.name("subagent"), Description: f.description("subagent")}},
		[]MCPServer{
			{Name: f.name("mcp-a"), Command: server, Args: []string{"--label=" + f.name("mcp-a")}, Env: map[string]string{"AMCOMPAT_SETTING": "alpha"}},
			{Name: f.name("mcp-b"), Command: server, Args: []string{"--label=" + f.name("mcp-b")}, Env: map[string]string{"AMCOMPAT_SETTING": "beta"}},
		},
	)
	files[".claude-plugin/plugin.json"] = fmt.Sprintf(`{"name":%q,"version":"1.0.0"}`, f.name("registry"))
	files["skills/"+f.name("skill")+"/SKILL.md"] += "\nRead [reference](assets/reference.txt).\n"
	files["skills/"+f.name("skill")+"/assets/reference.txt"] = f.name("support") + "\n"
	files["skills/"+f.name("skill")+"/scripts/check.sh"] = "printf 'compatibility-support\\n'\n"
	files["hooks/hooks.json"] = fmt.Sprintf(`{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":%q}],"UserPromptSubmit":[{"type":"command","command":%q}]}}`, f.hookCommand("hook-a"), f.hookCommand("hook-b"))
	files["collections/"+f.name("collection")+"/COLLECTION.yaml"] = fmt.Sprintf(`name: %s
version: "1.0"
description: Compatibility fixture collection
resources:
  - name: %s
    kind: skill
  - name: %s
    kind: command
  - name: %s
    kind: subagent
  - name: PreToolUse-command
    kind: hook
  - name: UserPromptSubmit-command
    kind: hook
  - name: %s
    kind: mcp
  - name: %s
    kind: mcp
`, f.name("collection"), f.name("skill"), f.name("command"), f.name("subagent"), f.name("mcp-a"), f.name("mcp-b"))
	files["rule-source/.factory/rules/"+f.name("rule")+".md"] = "Always preserve " + f.name("rule-body") + ".\n"
	for path, content := range files {
		e.WriteFile(filepath.Join(f.registry, filepath.FromSlash(path)), content)
	}
	return f
}

func (f compatibilityFixture) name(kind string) string { return f.prefix + "-" + kind }
func (f compatibilityFixture) description(kind string) string {
	return "Compatibility fixture " + f.name(kind)
}
func (f compatibilityFixture) hookCommand(kind string) string {
	return fmt.Sprintf("/bin/sh %q %s", f.hookScript, f.name(kind))
}

func compatibilityPaths(e *Env, target, scope string, f compatibilityFixture) map[string][]string {
	base := e.workspace
	if scope == "user" {
		base = e.home
	}
	root := "." + target
	if scope == "user" && target == "devin" {
		root = ".config/devin"
	}
	if scope == "user" && target == "windsurf" {
		root = ".codeium/windsurf"
	}
	paths := map[string][]string{
		"skill":    {root + "/skills/" + f.name("skill") + "/SKILL.md"},
		"command":  {root + "/commands/" + f.name("command") + ".md"},
		"subagent": {root + "/agents/" + f.name("subagent") + ".md"},
		"hook":     {root + "/hooks/hooks.json"},
		"mcp":      {root + "/mcp.json"},
	}
	switch target {
	case "claude":
		paths["hook"] = []string{root + "/settings.json"}
		paths["mcp"] = []string{".mcp.json"}
		if scope == "user" {
			paths["mcp"] = []string{".claude.json"}
		}
	case "factory":
		paths["hook"] = []string{root + "/hooks.json"}
		paths["subagent"] = []string{root + "/droids/" + f.name("subagent") + ".md"}
	case "cursor":
		paths["hook"] = []string{root + "/hooks.json"}
	case "gemini":
		paths["command"] = []string{root + "/commands/" + f.name("command") + ".toml"}
		paths["hook"] = []string{root + "/settings.json"}
		paths["mcp"] = []string{root + "/settings.json"}
	case "windsurf":
		workflow := "workflows"
		if scope == "user" {
			workflow = "global_workflows"
		}
		paths["command"] = []string{root + "/" + workflow + "/" + f.name("command") + ".md"}
		paths["subagent"] = nil
		if scope == "user" {
			paths["hook"] = []string{root + "/hooks.json"}
		} else {
			paths["hook"] = []string{".devin/hooks.json"}
		}
		paths["mcp"] = []string{root + "/mcp_config.json"}
	case "devin":
		paths["command"] = []string{root + "/skills/" + f.name("command") + "/SKILL.md"}
		paths["hook"] = []string{root + "/hooks.v1.json"}
		if scope == "user" {
			paths["hook"] = []string{root + "/config.json"}
		}
		paths["mcp"] = []string{root + "/mcp_config.json"}
	case "codex":
		paths["skill"] = []string{".agents/skills/" + f.name("skill") + "/SKILL.md"}
		paths["command"] = []string{".agents/skills/" + f.name("command") + "/SKILL.md"}
		paths["subagent"] = []string{".codex/agents/" + f.name("subagent") + ".toml"}
		paths["hook"] = []string{".codex/hooks.json"}
		paths["mcp"] = []string{".codex/config.toml"}
	}
	for kind, entries := range paths {
		for i, path := range entries {
			paths[kind][i] = filepath.Join(base, filepath.FromSlash(path))
		}
	}
	return paths
}

func captureCompatibilityFile(t *testing.T, e *Env, r *compatibilityResult, path string, values ...string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(e.home))
	if err != nil || !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		t.Fatalf("generated path escaped sandbox: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if r.Files == nil {
		r.Files = make(map[string]string)
	}
	rel, err := filepath.Rel(filepath.Dir(e.home), path)
	if err != nil {
		t.Fatal(err)
	}
	r.Files[filepath.ToSlash(rel)] = strings.ReplaceAll(text, filepath.Dir(e.home), "<sandbox>")
	for _, value := range values {
		if !strings.Contains(text, value) {
			t.Errorf("%s missing fixture value %q", rel, value)
		}
	}
	if strings.HasSuffix(path, ".json") && !json.Valid(data) {
		t.Errorf("%s is not valid JSON", rel)
	}
	return text
}

func assertCompatibilityShape(t *testing.T, path, mode string, directory bool) {
	t.Helper()
	if directory {
		path = filepath.Dir(path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if (info.Mode()&os.ModeSymlink != 0) != (mode == "symlink") {
		t.Errorf("%s mode = %s; expected %s materialization", path, info.Mode(), mode)
	}
	if mode == "file" && directory {
		if err := filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				t.Errorf("file-mode support tree contains a symlink: %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
