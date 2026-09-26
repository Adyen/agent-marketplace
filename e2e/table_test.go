package e2e_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- declarative test types ----------

// TestCase defines a single E2E test scenario declaratively.
type TestCase struct {
	Name       string        // subtest name (e.g. "collision/same_name_different_registries")
	Desc       string        // Given/When/Then description of the test scenario
	Registries []RegistryDef // bare repos to seed inside the container
	GlobalCfg  string        // ~/.agents/config.yaml content ({{REG:name}} placeholders resolved); always written (empty → auto-injected defaults)
	RepoCfg    string        // .agents/config.yaml content; omitted if empty
	LocalCfg   string        // .agents/config.local.yaml content; omitted if empty
	OrgCfg     string        // /etc/agent-marketplace/config.yaml content; omitted if empty
	Steps      []Step        // sequence of actions and assertions
}

// RegistryDef describes a bare git repo to create inside the container.
// Either use Skills/Commands/Subagents/MCPServers for standard layouts,
// or Files for arbitrary file trees (path -> content).
type RegistryDef struct {
	Name       string
	Skills     []Skill
	Commands   []Command
	Subagents  []Subagent
	MCPServers []MCPServer
	Files      map[string]string // raw files; overrides structured fields if set
}

// FilesMap returns the map[string]string suitable for CreateBareRepo.
// Structured resources (Skills, Commands, Subagents, MCPServers) are always
// included; raw Files entries overlay on top so COLLECTION.yaml and custom
// files coexist with auto-generated resource stubs.
func (r RegistryDef) FilesMap() map[string]string {
	files := RegistryFilesEx(r.Skills, r.Commands, r.Subagents, r.MCPServers)
	for p, c := range r.Files {
		files[p] = c
	}
	return files
}

// StepAction enumerates the side-effect actions a Step can perform before
// running a command (or without running a command at all).
type StepAction int

const (
	ActionNone StepAction = iota
	ActionWriteGlobalConfig
	ActionWriteRepoConfig
	ActionWriteLocalConfig
	ActionWriteOrgConfig
	ActionWriteFile
)

// Step is a single operation within a test case. A step either runs an
// agent-marketplace command (Args set) or performs a side-effect action
// (Action set), or both (action first, then command). Assertions are
// checked after the command runs.
type Step struct {
	// Action is a side-effect to perform before running Args.
	Action StepAction
	// Body is the content for Action writes (config content, file content).
	Body string
	// Path is the container path for ActionWriteFile.
	Path string

	// Args are the arguments to agent-marketplace. If empty and Action is set,
	// the step only performs the action without running a command.
	Args []string

	// AssertExitCode checks the exit code of the command (default: not checked).
	// Only effective when Args is set.
	AssertExitCode *int
	// MustPass is a shorthand: if true, AssertExitCode is expected to be 0
	// and the test fails immediately on mismatch (no subsequent assertions run).
	MustPass bool

	// Output substring assertions (only effective when Args is set).
	OutContains    []string
	OutNotContains []string

	// Filesystem assertions (run after the step's command/action).
	FilesExist      []string
	FilesAbsent     []string
	PathsUnchanged  []string // files or trees whose bytes and metadata must be unchanged by this step
	Symlinks        []string
	RegularFiles    []string          // paths that must exist and not be symbolic links
	FileContains    map[string]string // path -> substring
	FileNotContains map[string]string // path -> substring that must NOT appear
}

// ---------- placeholder resolution ----------

// resolvePlaceholders replaces {{REG:name}} tokens in config templates with
// the actual file:// URLs returned by CreateBareRepo.
func resolvePlaceholders(tmpl string, urls map[string]string) string {
	for name, url := range urls {
		tmpl = strings.ReplaceAll(tmpl, "{{REG:"+name+"}}", url)
	}
	return tmpl
}

func pathFingerprint(root string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%q\x00%s\x00%d\x00%d\x00", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_, _ = h.Write([]byte(target))
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = h.Write(data)
		}
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func TestPathFingerprintDetectsSameByteRewrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "artifact")
	if err := os.WriteFile(path, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	firstTime := time.Unix(1, 0)
	if err := os.Chtimes(path, firstTime, firstTime); err != nil {
		t.Fatal(err)
	}
	before, err := pathFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("same bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondTime := time.Unix(2, 0)
	if err := os.Chtimes(path, secondTime, secondTime); err != nil {
		t.Fatal(err)
	}
	after, err := pathFingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("path fingerprint ignored a same-byte rewrite")
	}
}

// ---------- test table ----------

var testTable = []TestCase{
	{
		Name:       "install/missing_repo_config_is_v4",
		Desc:       "Given a v4 user registry and no repo config, When installing a skill to repo scope, Then the newly created repo root uses v4 and selects the skill",
		Registries: []RegistryDef{{Name: "new-repo-config", Skills: []Skill{{Name: "new-skill"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {shared: {git: '{{REG:new-repo-config}}', ref: main}}\n",
		Steps: []Step{
			{Args: []string{"install", "new-skill", "--scope", "repo"}, MustPass: true,
				FileContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v4", "/workspace/.factory/skills/new-skill/SKILL.md": "name: new-skill"},
				FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v2"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "skill/new-skill"}},
		},
	},
	{
		Name:      "migration/startup_preserves_legacy_repo_layers",
		Desc:      "Given a v4 user config and legacy repo/local configs, When listing and explicitly migrating the repo, Then startup preserves both legacy layers and only explicit migration upgrades the repo",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v2\nregistries: []\n",
		LocalCfg:  "registries: []\n",
		Steps: []Step{
			{Args: []string{"list"}, MustPass: true, OutNotContains: []string{"Migrate these configuration files"}, PathsUnchanged: []string{"/workspace/.agents"}},
			{Args: []string{"migrate", "--scope", "repo"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v4"}, PathsUnchanged: []string{"/workspace/.agents/config.local.yaml"}},
		},
	},
	{
		Name:      "local_registry/v4_portable_directory",
		Desc:      "Given a repository-local registry, When migrating and syncing v4 dot and nested directory sources, Then live skills materialize without absolute config paths or Git locks",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v2\nregistries:\n  - url: .\n    ref: main\n    path: resources/skills\n    default: true\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/local-skill/SKILL.md", Body: (Skill{Name: "local-skill", Description: "Live local skill"}).SkillFile()},
			{Args: []string{"migrate", "--scope", "repo"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "directory: ./resources/skills"}},
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/local-skill"}, FilesAbsent: []string{"/workspace/.agents/lock.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Action: ActionWriteFile, Path: "/workspace/skills/root-skill/SKILL.md", Body: (Skill{Name: "root-skill", Description: "Repository root skill"}).SkillFile()},
			{Action: ActionWriteRepoConfig, Body: "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ., source_harness: claude}}\nresources: {registry: {include: [skill/root-skill]}}\n", Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/root-skill"}},
		},
	},
	{
		Name:      "security/cli_warning_controls",
		Desc:      "Given a selected missing resource with a control-bearing local registry ref, When syncing and listing, Then warning display is escaped and selection remains unchanged",
		GlobalCfg: "harnesses: [factory]\nregistries: []\n",
		RepoCfg:   "harnesses: [factory]\nregistries:\n  - url: ~/.agents/warning-fixture\n    ref: \"REF\\u001b[31m\\u202e\"\n    skills: [missing]\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/root/.agents/warning-fixture/skills/control/SKILL.md", Body: "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"warning:", `REF\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{`REF\x1b[31m\u202e`, "control"}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
		},
	},
	{
		Name:      "security/telemetry_failure_controls",
		Desc:      "Given a persisted delivery failure containing terminal controls, When displaying telemetry status, Then output is escaped without rewriting the stored record",
		GlobalCfg: "harnesses: [factory]\nregistries: []\ntelemetry:\n  level: off\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/root/.local/state/agent-marketplace/telemetry/last-delivery-failure", Body: `{"time":"2026-09-01T00:00:00Z","error":"SERVER\u001b[31m\r\n\u202e"}`},
			{Args: []string{"telemetry", "status"}, MustPass: true, OutContains: []string{`SERVER\x1b[31m\r\n\u202e`}, OutNotContains: []string{"\x1b", "\r", "\u202e"}, PathsUnchanged: []string{"/root/.local/state/agent-marketplace/telemetry/last-delivery-failure"}},
		},
	},
	{
		Name:      "security/cli_table_collection_controls",
		Desc:      "Given local registry and collection metadata containing terminal controls, When listing checking status and installing a collection, Then display is escaped while selection and installation remain intact",
		GlobalCfg: "harnesses: [factory]\nregistries: []\n",
		RepoCfg:   "harnesses: [factory]\nregistries:\n  - url: ~/.agents/terminal-fixture\n    ref: \"REF\\u001b[31m\\u202e\"\n    skills: [control]\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/root/.agents/terminal-fixture/skills/control/SKILL.md", Body: "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"},
			{Action: ActionWriteFile, Path: "/root/.agents/terminal-fixture/collections/bundle/COLLECTION.yaml", Body: "name: bundle\nversion: \"VERSION\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"},
			{Action: ActionWriteFile, Path: "/root/.agents/terminal-fixture/collections/display/COLLECTION.yaml", Body: "name: \"DISPLAY\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"},
			{Args: []string{"list", "--kind", "collection"}, MustPass: true, OutContains: []string{`DISPLAY\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{`REF\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"install", "bundle", "--kind", "collection", "--scope", "repo"}, MustPass: true, OutContains: []string{`Installed collection bundle@VERSION\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, FilesExist: []string{"/workspace/.factory/skills/control/SKILL.md"}},
		},
	},
	{
		Name: "security/mcp_parse_error_not_absence",
		Desc: "Given an MCP duplicate key whose text resembles a missing-file error, When converting, Then malformed input is refused without changing output and a repaired file converts normally",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/.windsurf/skills/control/SKILL.md", Body: "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"},
			{Action: ActionWriteFile, Path: "/workspace/source/.windsurf/mcp_config.json", Body: `{"mcpServers":{"fixture":{"command":"fixture","file does not exist":1,"file does not exist":2}}}`},
			{Action: ActionWriteFile, Path: "/workspace/output/keep.txt", Body: "HANDWRITTEN OUTPUT\n"},
			{Args: []string{"convert", "--from", "windsurf", "--to", "factory", "--out", "./output", "./source"}, AssertExitCode: intPtr(1), OutContains: []string{"json.duplicate-key"}, PathsUnchanged: []string{"/workspace/output"}},
			{Action: ActionWriteFile, Path: "/workspace/source/.windsurf/mcp_config.json", Body: `{"mcpServers":{"fixture":{"command":"fixture"}}}`},
			{Args: []string{"convert", "--from", "windsurf", "--to", "factory", "--out", "./output", "./source"}, MustPass: true, FileContains: map[string]string{"/workspace/output/mcp.json": "fixture", "/workspace/output/skills/control/SKILL.md": "CONTROL BODY"}, PathsUnchanged: []string{"/workspace/output/keep.txt"}},
		},
	},
	{
		Name: "security/windsurf_direct_root_layout",
		Desc: "Given direct-root Windsurf resources with root agent notes and MCP configuration, When converting, Then resource output is not hidden by a nonexistent workspace layout",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/AGENTS.md", Body: "# Notes\n"},
			{Action: ActionWriteFile, Path: "/workspace/source/workflows/deploy.md", Body: "---\ndescription: Deploy\n---\nWORKFLOW BODY\n"},
			{Action: ActionWriteFile, Path: "/workspace/source/hooks.json", Body: `{"hooks":{"pre_run_command":[{"command":"guard-command"}]}}`},
			{Action: ActionWriteFile, Path: "/workspace/source/mcp_config.json", Body: `{"mcpServers":{"native-server":{"command":"fixture-server"}}}`},
			{Args: []string{"convert", "--from", "windsurf", "--to", "windsurf", "--out", "./converted", "./source"}, MustPass: true, FileContains: map[string]string{"/workspace/converted/.windsurf/workflows/deploy.md": "WORKFLOW BODY", "/workspace/converted/.windsurf/hooks.json": "guard-command", "/workspace/converted/.windsurf/mcp_config.json": "fixture-server"}},
		},
	},
	{
		Name: "security/factory_declared_identity",
		Desc: "Given mixed Factory layouts with colliding declared names and a distinct filename collision, When converting, Then the first resource survives and the distinct resource is retained",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/commands/source.md", Body: "---\nname: review\ndescription: First\n---\nFIRST RESOURCE\n"},
			{Action: ActionWriteFile, Path: "/workspace/source/.factory/commands/alias.md", Body: "---\nname: review\ndescription: Shadow\n---\nSHADOW RESOURCE\n"},
			{Action: ActionWriteFile, Path: "/workspace/source/.factory/commands/review.md", Body: "---\nname: other\ndescription: Unique\n---\nUNIQUE RESOURCE\n"},
			{Args: []string{"convert", "--from", "factory", "--to", "factory", "--out", "./converted", "./source"}, MustPass: true, FileContains: map[string]string{"/workspace/converted/commands/review.md": "FIRST RESOURCE", "/workspace/converted/commands/other.md": "UNIQUE RESOURCE"}, FileNotContains: map[string]string{"/workspace/converted/commands/review.md": "SHADOW RESOURCE"}},
		},
	},
	{
		Name: "security/command_marker_body_lifecycle",
		Desc: "Given a Factory command with marker text in its body and a handwritten sibling, When installing removing and reinstalling, Then real frontmatter ownership enables cleanup without adopting the handwritten file",
		Registries: []RegistryDef{{Name: "marker-body", Files: map[string]string{
			"commands/marker-command.md": "---\nname: marker-command\ndescription: Marker command\n---\n```yaml\n_managed_by: agent-marketplace\n```\n",
			"commands/keep.md":           "---\nname: keep\ndescription: Keep\n---\nKeep body\n",
		}}},
		GlobalCfg: "harnesses: [factory]\nregistries: []\n",
		RepoCfg:   "harnesses: [factory]\nregistries:\n  - url: {{REG:marker-body}}\n    ref: main\n    commands: [marker-command, keep]\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.factory/commands/manual.md", Body: "---\ndescription: Personal\n---\nExample: _managed_by: agent-marketplace\n"},
			{Args: []string{"sync"}, MustPass: true, RegularFiles: []string{"/workspace/.factory/commands/marker-command.md"}, FileContains: map[string]string{"/workspace/.factory/commands/marker-command.md": "```yaml\n_managed_by: agent-marketplace\n```"}, PathsUnchanged: []string{"/workspace/.factory/commands/manual.md"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/commands/marker-command.md", "/workspace/.factory/commands/keep.md", "/workspace/.factory/commands/manual.md"}},
			{Args: []string{"remove", "marker-command", "--kind", "command", "--scope", "repo"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/commands/marker-command.md"}, PathsUnchanged: []string{"/workspace/.factory/commands/keep.md", "/workspace/.factory/commands/manual.md"}},
			{Args: []string{"install", "marker-command", "--kind", "command", "--scope", "repo"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/commands/marker-command.md": "---\n_managed_by: agent-marketplace\n"}, PathsUnchanged: []string{"/workspace/.factory/commands/keep.md", "/workspace/.factory/commands/manual.md"}},
		},
	},
	{
		Name:       "security/hook_merge_opaque_members",
		Desc:       "Given handwritten hook-container metadata and managed hooks, When sync updates hooks, Then unrelated members survive byte-for-byte while stale managed hooks are removed",
		Registries: []RegistryDef{{Name: "opaque-hooks", Files: map[string]string{"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo generated"}]}]}}`}}},
		GlobalCfg:  "harnesses: [claude]\nregistries: []\n",
		RepoCfg:    "harnesses: [claude]\nregistries:\n  - url: {{REG:opaque-hooks}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.claude/hooks/hooks.json", Body: "{\n  \"hooks\": {\n    // handwritten metadata\n    \"notes\" : \"keep\",\n    \"options\" : {\"enabled\":false},\n    \"disabled\" : false,\n    \"value\" : 1.2300,\n    \"nothing\" : null,\n    \"empty\" : [],\n    \"PostToolUse\": [{\"type\":\"command\",\"command\":\"manual-hook\"}],\n    \"Stop\": [{\"type\":\"command\",\"command\":\"stale-owned\",\"_managed_by\":\"agent-marketplace\"}]\n  }\n}\n"},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.claude/hooks/hooks.json": "echo generated"}, FileNotContains: map[string]string{"/workspace/.claude/hooks/hooks.json": "stale-owned"}},
			{FileContains: map[string]string{"/workspace/.claude/hooks/hooks.json": "// handwritten metadata\n    \"notes\" : \"keep\",\n    \"options\" : {\"enabled\":false},\n    \"disabled\" : false,\n    \"value\" : 1.2300,\n    \"nothing\" : null,\n    \"empty\" : [],"}},
			{FileContains: map[string]string{"/workspace/.claude/hooks/hooks.json": "manual-hook"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.claude/hooks/hooks.json"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{"/workspace/.claude/hooks/hooks.json"}},
		},
	},
	{
		Name: "security/codex_literal_header",
		Desc: "Given a Codex literal that resembles a bare environment reference and a safe sibling, When converting to Windsurf, Then the unsafe reinterpretation is refused while literal-preserving conversion remains available",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/.codex/config.toml", Body: "[mcp_servers.literal]\nurl = 'https://example.invalid/mcp'\n[mcp_servers.literal.http_headers]\nX-Literal = '$SYNTHETIC_KEY'\n[mcp_servers.safe]\nurl = 'https://example.invalid/mcp'\n[mcp_servers.safe.http_headers]\nX-Literal = 'ordinary'\n"},
			{Args: []string{"convert", "--from", "codex", "--to", "windsurf", "--out", "./converted", "./source"}, AssertExitCode: intPtr(1), OutContains: []string{"mcp.security-restriction-unsupported"}, FileContains: map[string]string{"/workspace/converted/.windsurf/mcp_config.json": "ordinary"}, FileNotContains: map[string]string{"/workspace/converted/.windsurf/mcp_config.json": "SYNTHETIC_KEY"}},
			{Args: []string{"convert", "--from", "codex", "--to", "codex", "--out", "./native", "./source"}, MustPass: true, FileContains: map[string]string{"/workspace/native/.codex/config.toml": "$SYNTHETIC_KEY"}},
		},
	},
	{
		Name: "security/native_metadata_authority",
		Desc: "Given foreign execution metadata and native workflow controls, When converting harnesses, Then only typed native controls remain active and ordinary metadata is preserved",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/agent-source/droids/guard.md", Body: "---\nname: guard\ndescription: Guard\nx-note: preserved\nmcpServers:\n  hidden:\n    command: must-not-activate\n---\nBody\n"},
			{Args: []string{"convert", "--from", "factory", "--to", "gemini", "--out", "./gemini-output", "./agent-source"}, MustPass: true, FileContains: map[string]string{"/workspace/gemini-output/.gemini/agents/guard.md": "x-note: preserved"}, FileNotContains: map[string]string{"/workspace/gemini-output/.gemini/agents/guard.md": "mcpServers:"}},
			{Action: ActionWriteFile, Path: "/workspace/command-source/commands/guard.md", Body: "---\nname: guard\ndescription: Guard\nx-note: preserved\nauto_execution_mode: always\n---\nBody\n"},
			{Args: []string{"convert", "--from", "factory", "--to", "windsurf", "--out", "./windsurf-output", "./command-source"}, MustPass: true, FileContains: map[string]string{"/workspace/windsurf-output/.windsurf/workflows/guard.md": "x-note: preserved"}, FileNotContains: map[string]string{"/workspace/windsurf-output/.windsurf/workflows/guard.md": "auto_execution_mode:"}},
			{Action: ActionWriteFile, Path: "/workspace/native/.windsurf/workflows/guard.md", Body: "---\ndescription: Guard\nauto_execution_mode: always\n---\nBody\n"},
			{Args: []string{"convert", "--from", "windsurf", "--to", "windsurf", "--out", "./native-output", "./native"}, MustPass: true, FileContains: map[string]string{"/workspace/native-output/.windsurf/workflows/guard.md": "auto_execution_mode: always"}},
		},
	},
	{
		Name: "security/codex_command_restriction",
		Desc: "Given a restricted command and an unrestricted sibling, When syncing to Codex, Then the unsupported restriction suppresses only that command",
		Registries: []RegistryDef{{Name: "codex-restrictions", Files: map[string]string{
			"commands/restricted.md": "---\nname: restricted\ndescription: Restricted\nallowed-tools: []\n---\nBody\n",
			"commands/plain.md":      "---\nname: plain\ndescription: Plain\n---\nBody\n",
		}}},
		GlobalCfg: "harnesses: [codex]\nregistries:\n  - url: {{REG:codex-restrictions}}\n    ref: main\n    commands: [restricted, plain]\n",
		Steps:     []Step{{Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{"skill.codex.security-restriction-unsupported"}, FilesAbsent: []string{"/root/.agents/skills/restricted"}, FileContains: map[string]string{"/root/.agents/skills/plain/SKILL.md": "x-agent-marketplace-kind: command"}}},
	},
	{
		Name: "security/factory_hook_event",
		Desc: "Given a Devin permission hook and a valid tool hook, When converting to Factory, Then unsupported authorization behavior is reported and omitted without suppressing the valid sibling",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/.devin/hooks.v1.json", Body: `{"PermissionRequest":[{"hooks":[{"type":"command","command":"permission-check"}]}],"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"valid-check"}]}]}`},
			{Args: []string{"convert", "--from", "devin", "--to", "factory", "--out", "./converted", "./source"}, AssertExitCode: intPtr(1), OutContains: []string{"hook.event-unmappable"}, FileContains: map[string]string{"/workspace/converted/hooks/hooks.json": "valid-check"}, FileNotContains: map[string]string{"/workspace/converted/hooks/hooks.json": "PermissionRequest"}},
		},
	},
	{
		Name: "security/windsurf_hook_scope_conversion",
		Desc: "Given scoped hooks, When converting between Windsurf and Claude, Then Write and Edit coverage survives and subagent hooks do not become global response hooks",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/source/.windsurf/hooks.json", Body: `{"hooks":{"pre_write_code":[{"command":"guard-write-and-edit"}]}}`},
			{Args: []string{"convert", "--from", "windsurf", "--to", "claude", "--out", "./converted", "./source"}, MustPass: true, FileContains: map[string]string{"/workspace/converted/hooks/hooks.json": "Write|Edit"}},
			{Args: []string{"convert", "--from", "claude", "--to", "windsurf", "--out", "./roundtrip", "./converted"}, MustPass: true, FileContains: map[string]string{"/workspace/roundtrip/.windsurf/hooks.json": "pre_write_code"}},
			{Action: ActionWriteFile, Path: "/workspace/scoped/hooks/hooks.json", Body: `{"Stop":[{"hooks":[{"type":"command","command":"global-stop"}]}],"SubagentStop":[{"hooks":[{"type":"command","command":"subagent-only"}]}]}`},
			{Args: []string{"convert", "--from", "claude", "--to", "windsurf", "--out", "./scope-output", "./scoped"}, MustPass: true, OutContains: []string{"Diagnostics 1"}, FileContains: map[string]string{"/workspace/scope-output/.windsurf/hooks.json": "global-stop"}, FileNotContains: map[string]string{"/workspace/scope-output/.windsurf/hooks.json": "subagent-only"}},
		},
	},
	{
		Name: "security/nested_bundle_lock_inventory",
		Desc: "Given an empty parent plugin with nested hook and MCP resources, When lock and locked sync run, Then both resources are inventoried before materialization",
		Registries: []RegistryDef{{Name: "nested-inventory", Files: map[string]string{
			".claude-plugin/plugin.json":     `{"name":"container","version":"1.0.0"}`,
			"plugins/audit/.mcp.json":        `{"mcpServers":{"nested-server":{"command":"fixture-server"}}}`,
			"plugins/audit/hooks/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo audit"}]}]}}`,
		}}},
		RepoCfg: "harnesses: [factory]\nregistries:\n  - url: {{REG:nested-inventory}}\n    ref: main\n    source_harness: claude\n    default: true\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/lock.yaml": "kind: hook"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/lock.yaml": "name: nested-server", "/workspace/.factory/mcp.json": "fixture-server"}},
		},
	},
	{
		Name:      "security/blocked_registry_credentials_redacted",
		Desc:      "Given a denied credential-bearing registry, When sync loads effective config, Then error output and the persisted log redact credentials without changing the configured identity",
		GlobalCfg: "harnesses: [factory]\nallowed_registries: []\n",
		RepoCfg:   "registries:\n  - url: https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE\n    ref: main\n    default: true\n",
		Steps: []Step{{
			Args: []string{"sync"}, AssertExitCode: intPtr(1),
			OutContains:     []string{"blocked by allowed_registries", "example.invalid/repo.git"},
			OutNotContains:  []string{"YOUR_USER_HERE", "YOUR_TOKEN_HERE", "YOUR_QUERY_HERE", "YOUR_FRAGMENT_HERE"},
			FilesExist:      []string{"/root/.agent-marketplace/logs/agent-marketplace.log"},
			FileContains:    map[string]string{"/root/.agent-marketplace/logs/agent-marketplace.log": "blocked by allowed_registries"},
			FileNotContains: map[string]string{"/root/.agent-marketplace/logs/agent-marketplace.log": "YOUR_"},
			FilesAbsent:     []string{"/workspace/.agents/lock.yaml", "/workspace/.factory"},
			PathsUnchanged:  []string{"/workspace/.agents/config.yaml"},
		}},
	},
	{
		Name:       "security/blocked_command_link_cleanup",
		Desc:       "Given locked commands with an edited sibling, When one blocked command is removed, Then only its directory link is unlinked",
		Registries: []RegistryDef{{Name: "blocked-command-links", Commands: []Command{{Name: "remove"}, {Name: "keep"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v2\nharnesses: [devin]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries:\n  - url: {{REG:blocked-command-links}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.devin/skills/remove/SKILL.md", "/workspace/.devin/skills/keep/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/skills/keep/SKILL.md", Body: "COMMAND SIBLING EDITED\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "command", "--from", "{{REG:blocked-command-links}}"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.devin/skills/remove"}, FileContains: map[string]string{"/workspace/.devin/skills/keep/SKILL.md": "COMMAND SIBLING EDITED"}, PathsUnchanged: []string{"/workspace/.devin/skills/keep/SKILL.md", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name:       "security/blocked_subagent_link_cleanup",
		Desc:       "Given locked subagents with an edited sibling, When one blocked subagent is removed, Then only its file link is unlinked",
		Registries: []RegistryDef{{Name: "blocked-subagent-links", Subagents: []Subagent{{Name: "remove"}, {Name: "keep"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v2\nharnesses: [devin]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries:\n  - url: {{REG:blocked-subagent-links}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.devin/agents/remove.md", "/workspace/.devin/agents/keep.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/agents/keep.md", Body: "SUBAGENT SIBLING EDITED\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "subagent", "--from", "{{REG:blocked-subagent-links}}"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.devin/agents/remove.md"}, FileContains: map[string]string{"/workspace/.devin/agents/keep.md": "SUBAGENT SIBLING EDITED"}, PathsUnchanged: []string{"/workspace/.devin/agents/keep.md", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name:       "security/blocked_skill_link_cleanup",
		Desc:       "Given locked skill symlinks and an edited installed sibling, When a blocked registry skill is removed, Then only its link is unlinked without rewriting siblings",
		Registries: []RegistryDef{{Name: "blocked-skill-links", Skills: []Skill{{Name: "remove", Description: "Remove original"}, {Name: "keep", Description: "Keep original"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v2\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: {{REG:blocked-skill-links}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/remove/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.factory/skills/keep/SKILL.md", Body: "KEEP EDITED BY USER\n"},
			{Action: ActionWriteFile, Path: "/workspace/.factory/skills/manual/SKILL.md", Body: "HANDWRITTEN\n"},
			{Action: ActionWriteFile, Path: "/workspace/.gitignore", Body: "handwritten-ignore\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "skill", "--from", "{{REG:blocked-skill-links}}", "--ref", "main"}, MustPass: true,
				FilesAbsent:    []string{"/workspace/.factory/skills/remove"},
				FileContains:   map[string]string{"/workspace/.factory/skills/keep/SKILL.md": "KEEP EDITED BY USER", "/workspace/.factory/skills/manual/SKILL.md": "HANDWRITTEN"},
				PathsUnchanged: []string{"/workspace/.factory/skills/keep/SKILL.md", "/workspace/.factory/skills/manual", "/workspace/.agents/lock.yaml", "/workspace/.gitignore"}},
		},
	},
	{
		Name: "security/blocked_owned_mcp_cleanup",
		Desc: "Given an exact locked Devin MCP registry blocked after sync, When one named owned MCP is removed, Then edited and handwritten siblings remain unchanged",
		Registries: []RegistryDef{
			{Name: "blocked-owned-mcp", MCPServers: []MCPServer{
				{Name: "remove", Command: "remove-binary"},
				{Name: "keep", Command: "keep-binary"},
			}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v2\nharnesses: [devin]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nregistries:\n  - url: {{REG:blocked-owned-mcp}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.devin/mcp_config.json": "keep-binary"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/mcp_config.json", Body: "{\n  // retained comment\n  \"custom\": true,\n  \"mcpServers\": {\n    \"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"},\n    \"remove\": {\"command\": \"remove-binary\"}\n  }\n}\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v2\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "mcp", "--from", "{{REG:blocked-owned-mcp}}"}, MustPass: true,
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "\"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"}",
				},
				FileNotContains: map[string]string{"/workspace/.devin/mcp_config.json": "remove-binary"}},
		},
	},
	{
		Name:       "migration/source_free_local_selector_is_refused",
		Desc:       "Given a source-free personal-local selector that sync honors, When local v4 migration is requested, Then migration refuses without changing selection and the next sync still honors it",
		Registries: []RegistryDef{{Name: "local-selector", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "harnesses: [factory]\n",
		RepoCfg:    "registries:\n  - url: {{REG:local-selector}}\n    ref: main\n",
		LocalCfg:   "apiVersion: agent-marketplace/v2\nskills:\n  include: [review]\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"migrate", "--scope", "local"}, AssertExitCode: intPtr(1), OutContains: []string{"v4.migration-unsupported"}, PathsUnchanged: []string{"/workspace/.agents/config.local.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
		},
	},
	{
		Name:    "security/config_include_size",
		Desc:    "Given an oversized valid included config, When sync runs, Then it fails without mutating the inputs or materializing output",
		RepoCfg: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: [large.yaml]\nregistries: []\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.agents/large.yaml", Body: "registries: []\n#" + strings.Repeat("x", 4*1024*1024) + "\n"},
			{Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{"config file exceeds maximum size"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/large.yaml"}, FilesAbsent: []string{"/workspace/.factory"}},
		},
	},
	{
		Name:    "security/config_include_small",
		Desc:    "Given a small included registry and selector, When sync runs, Then the included skill materializes",
		RepoCfg: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: [small.yaml]\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/registry/skills/bounded-skill/SKILL.md", Body: (Skill{Name: "bounded-skill", Description: "bounded include skill"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/.agents/small.yaml", Body: "registries:\n  - url: ./registry\nskills:\n  include: [bounded-skill]\n"},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/bounded-skill/SKILL.md"}},
		},
	},
	{
		Name:       "locks/unlocked_organization_sync_parity",
		Desc:       "Given an organization registry without a lock, when syncing and checking repeatedly, then resources stay synced without creating an organization lock.",
		Registries: []RegistryDef{{Name: "org-parity", Skills: []Skill{{Name: "review"}}}},
		OrgCfg:     "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: {{REG:org-parity}}\n    ref: main\n    required: true\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/root/.factory/skills/review"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills"}},
			{Args: []string{"status"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills"}},
			{Args: []string{"sync", "--offline"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills"}, FilesAbsent: []string{"/etc/agent-marketplace/lock.yaml"}},
		},
	},
	{
		Name:       "locks/equivalent_auto_explicit_inspection",
		Desc:       "Given equivalent auto and explicit registry roots, When lock check, list and status run in either entry order, Then inspection agrees with reconciliation without changing config or locks",
		Registries: []RegistryDef{{Name: "equivalent-roots", Skills: []Skill{{Name: "review", Description: "Review changes"}}, Files: map[string]string{".claude-plugin/plugin.json": `{"name":"equivalent-roots"}`}}},
		RepoCfg:    "harnesses: [factory]\nregistries:\n  - url: {{REG:equivalent-roots}}\n    ref: main\n  - url: {{REG:equivalent-roots}}\n    ref: main\n    source_harness: claude\n",
		Steps: []Step{
			{Args: []string{"lock", "--scope", "repo"}, MustPass: true},
			{Args: []string{"lock", "--check", "--scope", "repo"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{"review"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"EXACT REGISTRY LOCKS", "available"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Action: ActionWriteRepoConfig, Body: "harnesses: [factory]\nregistries:\n  - url: {{REG:equivalent-roots}}\n    ref: main\n    source_harness: claude\n  - url: {{REG:equivalent-roots}}\n    ref: main\n"},
			{Args: []string{"lock", "--check", "--scope", "repo"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{"review"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"EXACT REGISTRY LOCKS", "available"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
		},
	},
	// ===== logging =====
	{
		Name: "logging/file_and_debug",
		Desc: "Given a configured registry, When list runs with and without --debug, Then operational logs are written to the sandbox cache log file and debug records appear only with the flag",
		Registries: []RegistryDef{
			{Name: "logging-registry", Skills: []Skill{
				{Name: "logging-skill", Description: "Logging test skill", Tags: []string{"ops"}},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
registries:
  - url: {{REG:logging-registry}}
    ref: main
    skills:
      - logging-skill
`,
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list"}, MustPass: true,
				FilesExist: []string{"/root/.agent-marketplace/logs/agent-marketplace.log"},
				FileContains: map[string]string{
					"/root/.agent-marketplace/logs/agent-marketplace.log": `"msg":"command start"`,
				},
				FileNotContains: map[string]string{
					"/root/.agent-marketplace/logs/agent-marketplace.log": `"level":"DEBUG"`,
				}},
			{Args: []string{"--debug", "list"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.agent-marketplace/logs/agent-marketplace.log": `"level":"DEBUG"`,
				}},
		},
	},

	{
		Name: "hooks/additive_cross_registry",
		Desc: "Given two Factory registries with same-named hooks, when install-all and removal run, then both providers materialize additively and removing one preserves the other",
		Registries: []RegistryDef{
			{Name: "first", Files: map[string]string{".factory/hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}`}},
			{Name: "second", Files: map[string]string{".factory/hooks/hooks.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo second"}]}]}`}},
		},
		RepoCfg: `harnesses: [factory]
registries:
  - url: {{REG:first}}
    ref: main
    source_harness: factory
  - url: {{REG:second}}
    ref: main
    source_harness: factory
`,
		Steps: []Step{
			{Args: []string{"install", "--all", "--kind", "hook"}, MustPass: true, OutContains: []string{"PreToolUse-command"}, OutNotContains: []string{"multiple registries"}, FileContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo first"}},
			{Args: []string{"list", "--kind", "hook"}, MustPass: true, OutContains: []string{"PreToolUse-command"}, FileContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json"}},
			{Args: []string{"remove", "PreToolUse-command", "--kind", "hook", "--from", "{{REG:first}}"}, MustPass: true, FileNotContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo first"}, FileContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name: "hooks/additive_native_definitions",
		Desc: "Given two Devin source registries with native hooks and a Windsurf target, when install-all and removal run, then native definitions are not materialized as hook support files and both providers remain additive",
		Registries: []RegistryDef{
			{Name: "first", Files: map[string]string{".devin/hooks.v1.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo first"}]}]}`}},
			{Name: "second", Files: map[string]string{".devin/hooks.v1.json": `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo second"}]}]}`}},
		},
		RepoCfg: `harnesses: [windsurf]
registries:
  - url: {{REG:first}}
    ref: main
    source_harness: devin
  - url: {{REG:second}}
    ref: main
    source_harness: devin
`,
		Steps: []Step{
			{Args: []string{"install", "--all", "--kind", "hook"}, MustPass: true, FileContains: map[string]string{"/workspace/.windsurf/hooks.json": "echo first"}, FilesAbsent: []string{"/workspace/.windsurf/hook-support/hooks.v1.json"}},
			{Args: []string{"list", "--kind", "hook"}, MustPass: true, FileContains: map[string]string{"/workspace/.windsurf/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.windsurf/hooks.json", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.windsurf/hooks.json"}},
			{Args: []string{"remove", "PreToolUse-command", "--kind", "hook", "--from", "{{REG:first}}"}, MustPass: true, FileNotContains: map[string]string{"/workspace/.windsurf/hooks.json": "echo first"}, FileContains: map[string]string{"/workspace/.windsurf/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.windsurf/hooks.json", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name:       "hooks/additive_session_start",
		Desc:       "Given one Factory registry with two SessionStart handlers, when both generated names are selected, then both handlers remain stable across sync and check",
		Registries: []RegistryDef{{Name: "session-start", Files: map[string]string{".factory/hooks/hooks.json": `{"SessionStart":[{"hooks":[{"type":"command","command":"echo first"}]},{"hooks":[{"type":"command","command":"echo second"}]}]}`}}},
		RepoCfg: `harnesses: [factory]
registries:
  - url: {{REG:session-start}}
    ref: main
    source_harness: factory
    hooks: [SessionStart-command, SessionStart-command-2]
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo first"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/lock.yaml"}, FileContains: map[string]string{"/workspace/.factory/hooks/hooks.json": "echo second"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json"}},
		},
	},

	// ===== collision =====
	{
		Name: "install/collision_warning_scoped",
		Desc: "Given two selected registries with skill and command collisions, When installing one skill, Then its collision is reported without unrelated warnings and the existing winner is installed",
		Registries: []RegistryDef{
			{Name: "registry-a", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry A"}, {Name: "other", Description: "Other from registry A"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry A", Command: "echo COMMAND A"}}},
			{Name: "registry-b", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry B"}, {Name: "other", Description: "Other from registry B"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry B", Command: "echo COMMAND B"}}},
		},
		GlobalCfg: "harnesses: [factory]\nregistries: []\n",
		RepoCfg:   "registries:\n  - url: {{REG:registry-a}}\n    ref: main\n    default: true\n  - url: {{REG:registry-b}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"install", "deploy", "--kind", "skill", "--from", "{{REG:registry-a}}", "--ref", "main", "--scope", "repo"}, MustPass: true,
				OutContains:    []string{`skill "deploy" is provided by multiple registries`, "registry-a", "registry-b", "agent-marketplace remove", "--from"},
				OutNotContains: []string{`skill "other" is provided by multiple registries`, `command "deploy" is provided by multiple registries`},
				FileContains:   map[string]string{"/workspace/.factory/skills/deploy/SKILL.md": "Deploy from registry A"}},
			{Args: []string{"sync"}, MustPass: true,
				OutContains:  []string{`skill "deploy"`, `skill "other"`, `command "deploy"`, "multiple registries"},
				FileContains: map[string]string{"/workspace/.factory/skills/deploy/SKILL.md": "Deploy from registry A"}},
		},
	},
	{
		Name: "collision/same_name_different_registries",
		Desc: "Given two registries each providing a skill named 'deploy', When sync is run, Then a collision warning is emitted but sync succeeds with the skill materialized from the first registry",
		Registries: []RegistryDef{
			{Name: "registry-a", Skills: []Skill{
				{Name: "deploy", Description: "Deploy from registry A", Tags: []string{"infra"}},
			}},
			{Name: "registry-b", Skills: []Skill{
				{Name: "deploy", Description: "Deploy from registry B", Tags: []string{"infra"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:registry-a}}
    ref: main
    skills:
      - deploy
  - url: {{REG:registry-b}}
    ref: main
    skills:
      - deploy
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{"deploy", "multiple registries"},
				FilesExist:  []string{"/root/.factory/skills/deploy/SKILL.md"}},
			// --check must explain a partial/ambiguous result for the same
			// reason `sync` does. It used to print only ineffective-subdir
			// warnings and silently drop collisions, overlaps and soft
			// selected-not-found, so a green-but-ambiguous check looked clean.
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0),
				OutContains: []string{"warning:", "multiple registries"}},
		},
	},
	{
		Name: "collision/same_url_different_paths",
		Desc: "Given one registry configured twice with different path values, When sync is run, Then both path-scoped resources materialize without collision",
		Registries: []RegistryDef{
			{Name: "mono-registry", Files: map[string]string{
				"team-a/skills/skill-a/SKILL.md": Skill{Name: "skill-a", Description: "Skill A", Tags: []string{"team-a"}}.SkillFile(),
				"team-b/skills/skill-b/SKILL.md": Skill{Name: "skill-b", Description: "Skill B", Tags: []string{"team-b"}}.SkillFile(),
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mono-registry}}
    ref: main
    path: team-a
    skills:
      - skill-a
  - url: {{REG:mono-registry}}
    ref: main
    path: team-b
    skills:
      - skill-b
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/skill-a/SKILL.md",
					"/root/.factory/skills/skill-b/SKILL.md",
				}},
			{Args: []string{"status"},
				OutContains: []string{"skill-a", "skill-b"}},
		},
	},
	{
		Name: "collision/overlapping_name_different_kinds",
		Desc: "Given a registry with a skill and a command sharing the name 'deploy', When sync is run, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "mixed-registry",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill", Tags: []string{"ops"}}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mixed-registry}}
    ref: main
    skills:
      - deploy
    commands:
      - deploy
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/deploy/SKILL.md",
					"/root/.factory/commands/deploy.md",
				}},
		},
	},

	// ===== materialize =====
	{
		Name: "materialize/multi_harness_output",
		Desc: "Given factory and windsurf harnesses configured, When sync is run, Then skills appear in both harness output directories",
		Registries: []RegistryDef{
			{Name: "multi-harness", Skills: []Skill{
				{Name: "universal-skill", Description: "Appears in all harnesses", Tags: []string{"core"}},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - windsurf
registries:
  - url: {{REG:multi-harness}}
    ref: main
    skills:
      - universal-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/universal-skill/SKILL.md",
					"/root/.codeium/windsurf/skills/universal-skill/SKILL.md",
				}},
		},
	},
	{
		Name: "materialize/preserves_structured_skill_metadata",
		Desc: "Given a skill with mapping-valued metadata, When sync materializes it for every harness, Then each SKILL.md retains valid nested YAML metadata",
		Registries: []RegistryDef{
			{Name: "metadata-registry", Files: map[string]string{
				"skills/owned-skill/SKILL.md": `---
name: owned-skill
description: A skill with ownership metadata
metadata:
  author: example-maintainer
  tags:
    - platform
---

# Owned skill
`,
			}},
		},
		GlobalCfg: `
harnesses:
  - claude
  - factory
  - cursor
  - windsurf
  - gemini
registries:
  - url: {{REG:metadata-registry}}
    ref: main
    skills:
      - owned-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.claude/skills/owned-skill/SKILL.md",
					"/root/.factory/skills/owned-skill/SKILL.md",
					"/root/.cursor/skills/owned-skill/SKILL.md",
					"/root/.codeium/windsurf/skills/owned-skill/SKILL.md",
					"/root/.gemini/skills/owned-skill/SKILL.md",
				},
				FileContains: map[string]string{
					"/root/.claude/skills/owned-skill/SKILL.md":           "metadata:\n  author: example-maintainer",
					"/root/.factory/skills/owned-skill/SKILL.md":          "metadata:\n  author: example-maintainer",
					"/root/.cursor/skills/owned-skill/SKILL.md":           "metadata:\n  author: example-maintainer",
					"/root/.codeium/windsurf/skills/owned-skill/SKILL.md": "metadata:\n  author: example-maintainer",
					"/root/.gemini/skills/owned-skill/SKILL.md":           "metadata:\n  author: example-maintainer",
				},
				FileNotContains: map[string]string{
					"/root/.claude/skills/owned-skill/SKILL.md":           "map[",
					"/root/.factory/skills/owned-skill/SKILL.md":          "map[",
					"/root/.cursor/skills/owned-skill/SKILL.md":           "map[",
					"/root/.codeium/windsurf/skills/owned-skill/SKILL.md": "map[",
					"/root/.gemini/skills/owned-skill/SKILL.md":           "map[",
				}},
		},
	},
	{
		Name: "materialize/harness_removal_prunes_artifacts",
		Desc: "Given a skill synced to factory and windsurf, When windsurf is removed from harnesses and re-synced, Then the windsurf artifact is pruned while factory remains",
		Registries: []RegistryDef{
			{Name: "harness-removal", Skills: []Skill{
				{Name: "prunable-skill", Description: "Will be pruned from windsurf"},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - windsurf
registries:
  - url: {{REG:harness-removal}}
    ref: main
    skills:
      - prunable-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/prunable-skill/SKILL.md",
					"/root/.codeium/windsurf/skills/prunable-skill/SKILL.md",
				}},
			{Action: ActionWriteGlobalConfig, Body: `
harnesses:
  - factory
registries:
  - url: {{REG:harness-removal}}
    ref: main
    skills:
      - prunable-skill
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/prunable-skill/SKILL.md"},
				FilesAbsent: []string{"/root/.codeium/windsurf/skills/prunable-skill"}},
		},
	},
	{
		Name: "materialize/skills_are_symlinks",
		Desc: "Given a skill synced from a registry, When the materialized path is inspected, Then it is a symlink pointing to the cache directory",
		Registries: []RegistryDef{
			{Name: "symlink-test", Skills: []Skill{
				{Name: "linked-skill", Description: "Should be a symlink"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:symlink-test}}
    ref: main
    skills:
      - linked-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				Symlinks: []string{"/root/.factory/skills/linked-skill"}},
		},
	},
	{
		Name: "materialize/file_mode_portable_project_output",
		Desc: "Given project file-mode configuration, When sync materializes a skill, Then it writes a regular portable tree and a commit-ready project manifest",
		Registries: []RegistryDef{
			{Name: "file-mode-test", Skills: []Skill{{Name: "portable-skill", Description: "Portable skill"}}},
		},
		GlobalCfg: `
	harnesses:
  - factory
	`,
		RepoCfg: `
	materialization:
  mode: file
registries:
  - url: {{REG:file-mode-test}}
    ref: main
    skills:
      - portable-skill
	`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				RegularFiles: []string{"/workspace/.factory/skills/portable-skill/SKILL.md"},
				FilesExist:   []string{"/workspace/.agents/materialization-manifest.yaml"},
				FileContains: map[string]string{"/workspace/.agents/materialization-manifest.yaml": "skills/portable-skill"}},
		},
	},
	{
		Name: "materialize/target_mode_override_supports_devin_with_file_mode",
		Desc: "Given file mode with a Devin symlink override, When the same skill targets Factory and Devin, Then Factory writes files and Devin writes a directory symlink",
		Registries: []RegistryDef{
			{Name: "mixed-mode-test", Skills: []Skill{{Name: "mixed-mode-skill", Description: "Mixed mode skill"}}},
		},
		GlobalCfg: "harnesses:\n  - factory\n  - devin\n",
		RepoCfg:   "materialization:\n  mode: file\n  targets:\n    devin: symlink\nregistries:\n  - url: {{REG:mixed-mode-test}}\n    ref: main\n    skills:\n      - mixed-mode-skill\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				RegularFiles: []string{"/workspace/.factory/skills/mixed-mode-skill/SKILL.md"},
				Symlinks:     []string{"/workspace/.devin/skills/mixed-mode-skill"}},
		},
	},
	{
		Name: "materialize/manual_skills_survive_sync",
		Desc: "Given a managed skill and a manually created skill, When sync is re-run, Then the manual skill survives and the managed skill stays",
		Registries: []RegistryDef{
			{Name: "manual-survive", Skills: []Skill{
				{Name: "managed-skill", Description: "Managed by agent-marketplace"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:manual-survive}}
    ref: main
    skills:
      - managed-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Action: ActionWriteFile,
				Path: "/root/.factory/skills/manual-skill/SKILL.md",
				Body: `---
name: manual-skill
description: Manually created
---

# manual-skill
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/managed-skill/SKILL.md",
					"/root/.factory/skills/manual-skill/SKILL.md",
				}},
		},
	},
	{
		Name: "materialize/manual_droids_survive_sync",
		Desc: "Given pre-existing user-authored droids in .factory/droids, When a managed droid is synced, Then the user droids survive and the managed droid is a symlink to the cache",
		Registries: []RegistryDef{
			{Name: "droid-survive", Subagents: []Subagent{
				{Name: "reviewer", Description: "Managed by agent-marketplace", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:droid-survive}}
    ref: main
    subagents:
      - reviewer
`,
		Steps: []Step{
			// Seed three user-authored droids (regular files, no ownership marker).
			{Action: ActionWriteFile,
				Path: "/root/.factory/droids/worker.md",
				Body: `---
name: worker
description: my hand-written worker droid
---

# worker (user-owned)
`},
			{Action: ActionWriteFile,
				Path: "/root/.factory/droids/planner.md",
				Body: `---
name: planner
description: my hand-written planner droid
---

# planner (user-owned)
`},
			{Action: ActionWriteFile,
				Path: "/root/.factory/droids/explorer.md",
				Body: `---
name: explorer
description: my hand-written explorer droid
---

# explorer (user-owned)
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/droids/worker.md",
					"/root/.factory/droids/planner.md",
					"/root/.factory/droids/explorer.md",
					"/root/.factory/droids/reviewer.md",
				},
				Symlinks: []string{"/root/.factory/droids/reviewer.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/worker.md":   "# worker (user-owned)",
					"/root/.factory/droids/planner.md":  "# planner (user-owned)",
					"/root/.factory/droids/explorer.md": "# explorer (user-owned)",
				},
				// User droids must remain unmanaged (no ownership marker injected).
				FileNotContains: map[string]string{
					"/root/.factory/droids/worker.md": "_managed_by",
				}},
			// Re-sync must be idempotent: user droids still survive.
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/droids/worker.md",
					"/root/.factory/droids/planner.md",
					"/root/.factory/droids/explorer.md",
				}},
		},
	},
	{
		Name: "materialize/manual_droid_survives_managed_droid_removal",
		Desc: "Given a synced managed droid alongside a user droid, When the managed droid is removed from config and sync re-runs, Then the managed droid is pruned but the user droid survives",
		Registries: []RegistryDef{
			{Name: "droid-remove-survive", Subagents: []Subagent{
				{Name: "reviewer", Description: "Managed by agent-marketplace", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:droid-remove-survive}}
    ref: main
    subagents:
      - reviewer
`,
		Steps: []Step{
			{Action: ActionWriteFile,
				Path: "/root/.factory/droids/worker.md",
				Body: `---
name: worker
description: my hand-written worker droid
---

# worker (user-owned)
`},
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/root/.factory/droids/reviewer.md"},
				FilesExist: []string{"/root/.factory/droids/worker.md"}},
			// Drop the managed droid from config, then re-sync.
			{Action: ActionWriteGlobalConfig,
				Body: `
registries:
  - url: {{REG:droid-remove-survive}}
    ref: main
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/droids/reviewer.md"},
				FilesExist:  []string{"/root/.factory/droids/worker.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/worker.md": "# worker (user-owned)",
				}},
		},
	},
	{
		Name: "materialize/sync_check_detects_drift",
		Desc: "Given a config with one skill, When sync --check is run before any sync, Then it exits 1; after sync exits 0; after adding a skill to config exits 1 again",
		Registries: []RegistryDef{
			{Name: "drift-check", Skills: []Skill{
				{Name: "drift-skill", Description: "Tests drift detection"},
			}},
			{Name: "drift-check-2", Skills: []Skill{
				{Name: "new-skill", Description: "Causes drift"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:drift-check}}
    ref: main
    skills:
      - drift-skill
`,
		Steps: []Step{
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			{Action: ActionWriteGlobalConfig, Body: `
registries:
  - url: {{REG:drift-check}}
    ref: main
    skills:
      - drift-skill
  - url: {{REG:drift-check-2}}
    ref: main
    skills:
      - new-skill
`},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
		},
	},
	{
		Name: "materialize/tag_based_selection",
		Desc: "Given a registry with k8s-tagged and python-tagged skills and tags: [k8s] in config, When sync is run, Then only k8s-tagged skills are materialized",
		Registries: []RegistryDef{
			{Name: "tag-selection", Skills: []Skill{
				{Name: "k8s-deploy", Description: "Kubernetes deployment", Tags: []string{"k8s", "infra"}},
				{Name: "k8s-debug", Description: "Kubernetes debugging", Tags: []string{"k8s", "debug"}},
				{Name: "python-lint", Description: "Python linting", Tags: []string{"python", "lint"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:tag-selection}}
    ref: main
    tags:
      - k8s
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/k8s-deploy/SKILL.md",
					"/root/.factory/skills/k8s-debug/SKILL.md",
				},
				FilesAbsent: []string{"/root/.factory/skills/python-lint"}},
		},
	},
	{
		Name:      "materialize/empty_sync",
		Desc:      "Given no registries configured, When sync is run, Then it exits cleanly with 'Installed 0'",
		GlobalCfg: ``,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{"Installed 0"}},
		},
	},
	{
		Name: "materialize/commands_materialize_as_files",
		Desc: "Given a registry with a command resource, When sync is run, Then the command is compiled as a .md file (not symlinked)",
		Registries: []RegistryDef{
			{Name: "commands-test",
				Commands: []Command{{Name: "greet", Description: "Greeting command", Command: "echo hello"}}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:commands-test}}
    ref: main
    commands:
      - greet
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/greet.md"}},
		},
	},

	// ===== precedence =====
	{
		Name: "precedence/repo_exclude_overrides_repo_include",
		Desc: "Given repo config with both include and exclude for skill-a, When sync is run, Then exclude wins and skill-a is absent while skill-b is present",
		Registries: []RegistryDef{
			{Name: "precedence-registry", Skills: []Skill{
				{Name: "skill-a", Description: "Skill A"},
				{Name: "skill-b", Description: "Skill B"},
				{Name: "skill-c", Description: "Skill C"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:precedence-registry}}
    ref: main
    skills:
      include:
        - skill-a
        - skill-b
      exclude:
        - skill-a
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.factory/skills/skill-a"},
				FilesExist:  []string{"/workspace/.factory/skills/skill-b/SKILL.md"}},
		},
	},
	{
		Name: "precedence/local_exclude_overrides_repo_include",
		Desc: "Given repo config including two skills and local config excluding one, When sync is run, Then the locally excluded skill is absent while the other remains",
		Registries: []RegistryDef{
			{Name: "local-precedence", Skills: []Skill{
				{Name: "wanted", Description: "Wanted skill"},
				{Name: "unwanted", Description: "Unwanted skill"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:local-precedence}}
    ref: main
    skills:
      include:
        - wanted
        - unwanted
`,
		LocalCfg: `
skills:
  exclude:
    - unwanted
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/workspace/.factory/skills/wanted/SKILL.md"},
				FilesAbsent: []string{"/workspace/.factory/skills/unwanted"}},
		},
	},
	{
		Name: "precedence/required_immune_to_excludes",
		Desc: "Given a required:true registry skill, When repo and local configs try to exclude it, Then the skill remains materialized",
		Registries: []RegistryDef{
			{Name: "required-registry", Skills: []Skill{
				{Name: "security-audit", Description: "Mandatory security audit", Tags: []string{"security"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:required-registry}}
    ref: main
    required: true
    default: true
`,
		RepoCfg: `
skills:
  exclude:
    - security-audit
`,
		LocalCfg: `
skills:
  exclude:
    - security-audit
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/skills/security-audit/SKILL.md"}},
		},
	},
	{
		Name: "precedence/org_config_sets_floor",
		Desc: "Given an unlocked org config with a required registry, When locked sync runs without a global registry, Then the org skill is materialized without creating an org lock",
		Registries: []RegistryDef{
			{Name: "org-registry", Skills: []Skill{
				{Name: "compliance-check", Description: "Org-mandated compliance"},
			}},
		},
		OrgCfg: `
apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: {{REG:org-registry}}
    ref: main
    required: true
    default: true
`,
		GlobalCfg: ``,
		Steps: []Step{
			{Args: []string{"sync", "--locked"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/compliance-check/SKILL.md"},
				FilesAbsent: []string{"/etc/agent-marketplace/lock.yaml"}},
		},
	},
	{
		Name: "precedence/include_exclude_same_level",
		Desc: "Given a registry entry with both include and exclude for the same resource, When sync is run, Then exclude wins",
		Registries: []RegistryDef{
			{Name: "same-level", Skills: []Skill{
				{Name: "included", Description: "Should be included"},
				{Name: "conflicted", Description: "Include and exclude at same level"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:same-level}}
    ref: main
    skills:
      include:
        - included
        - conflicted
      exclude:
        - conflicted
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/included/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/skills/conflicted"}},
		},
	},
	{
		Name: "precedence/default_true_auto_selects_all",
		Desc: "Given a registry with default:true, When sync is run, Then all discovered resources are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "default-registry", Skills: []Skill{
				{Name: "auto-a", Description: "Auto-selected A"},
				{Name: "auto-b", Description: "Auto-selected B"},
				{Name: "auto-c", Description: "Auto-selected C"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:default-registry}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/auto-a/SKILL.md",
					"/root/.factory/skills/auto-b/SKILL.md",
					"/root/.factory/skills/auto-c/SKILL.md",
				}},
		},
	},
	{
		Name: "precedence/default_true_with_exclude",
		Desc: "Given a default:true registry with an explicit exclude, When sync is run, Then excluded resources are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "default-exclude", Skills: []Skill{
				{Name: "keep-me", Description: "Should stay"},
				{Name: "drop-me", Description: "Should be excluded"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:default-exclude}}
    ref: main
    default: true
    skills:
      exclude:
        - drop-me
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/keep-me/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/skills/drop-me"}},
		},
	},

	// ===== install command =====
	{
		Name: "install/basic_skill_then_list",
		Desc: "Given a registry with a skill, When install and sync are run, Then list and status show the skill as enabled",
		Registries: []RegistryDef{
			{Name: "install-reg", Skills: []Skill{
				{Name: "code-review", Tags: []string{"review"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:install-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "code-review"}, MustPass: true,
				OutContains: []string{"code-review"}},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"list"},
				OutContains: []string{"code-review"}},
			{Args: []string{"status"},
				OutContains: []string{"code-review"}},
		},
	},
	{
		Name: "install/tag_selects_matching",
		Desc: "Given a registry with k8s-tagged skills, When install --tag k8s is run, Then list shows the k8s-tagged skills",
		Registries: []RegistryDef{
			{Name: "tag-install", Skills: []Skill{
				{Name: "k8s-setup", Tags: []string{"k8s"}},
				{Name: "k8s-teardown", Tags: []string{"k8s"}},
				{Name: "python-fmt", Tags: []string{"python"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:tag-install}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--tag", "k8s"}, MustPass: true,
				OutContains: []string{"k8s"}},
			// Install writes to repo config; re-read config to verify tag was added.
			{Args: []string{"list"}, MustPass: true,
				OutContains: []string{"k8s-setup", "k8s-teardown"}},
		},
	},
	{
		Name: "install/all_with_multi_kind",
		Desc: "Given a registry with skills, commands, and subagents explicitly selected in config, When sync is run, Then all three kinds materialize",
		Registries: []RegistryDef{
			{Name: "multi-kind", Skills: []Skill{
				{Name: "deploy-skill", Tags: []string{"infra"}},
			}, Commands: []Command{
				{Name: "greet", Command: "echo hello"},
			}, Subagents: []Subagent{
				{Name: "worker", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:multi-kind}}
    ref: main
    skills:
      - deploy-skill
    commands:
      - greet
    subagents:
      - worker
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/deploy-skill/SKILL.md",
					"/root/.factory/commands/greet.md",
					"/root/.factory/droids/worker.md",
				}},
		},
	},
	{
		Name: "install/from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from pointing to a registry, Then the skill appears in list output",
		Registries: []RegistryDef{
			{Name: "adhoc-reg", Skills: []Skill{
				{Name: "adhoc-skill"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"install", "adhoc-skill", "--from", "{{REG:adhoc-reg}}", "--ref", "main"}, MustPass: true,
				OutContains: []string{"adhoc-skill"}},
			// Verify the ad-hoc entry was written to repo config.
			{Args: []string{"list"}, MustPass: true,
				OutContains: []string{"adhoc-skill"}},
		},
	},
	{
		Name: "install/missing_skill_exits_nonzero",
		Desc: "Given a registry that does not contain the requested skill, When install nonexistent-skill is run, Then it exits 1 with an error",
		Registries: []RegistryDef{
			{Name: "missing-reg", Skills: []Skill{
				{Name: "exists-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:missing-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "nonexistent-skill"}, AssertExitCode: intPtr(1),
				OutContains: []string{"nonexistent"}},
		},
	},
	{
		Name: "install/scope_user_rejects_repo_relative_registry",
		Desc: "Given a repo-relative local registry, When install --scope user is run, Then it fails without changing the user config",
		GlobalCfg: `
registries: []
`,
		RepoCfg: `
registries:
  - url: ./plugins/local
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/plugins/local/skills/local-skill/SKILL.md", Body: "---\nname: local-skill\ndescription: Local skill\n---\n"},
			{Args: []string{"install", "--scope", "user", "local-skill"}, AssertExitCode: intPtr(1),
				OutContains: []string{"repo-relative", "global config"},
				FileNotContains: map[string]string{
					"/root/.agents/config.yaml": "plugins/local",
				}},
		},
	},
	{
		Name:      "install/from_local_missing_path_does_not_persist",
		Desc:      "Given an existing local registry root with a missing bundle path, When ad-hoc install runs, Then it fails before changing repo config",
		GlobalCfg: ``,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/local-registry/README.md", Body: "local registry\n"},
			{Args: []string{"install", "some-skill", "--from", "./local-registry", "--path", "missing"}, AssertExitCode: intPtr(1),
				OutContains: []string{"local registry path does not exist"},
				FileNotContains: map[string]string{
					"/workspace/.agents/config.yaml": "local-registry",
				}},
		},
	},
	{
		Name: "install/scope_user_writes_to_global",
		Desc: "Given a registry in global config, When install --scope user is run and synced, Then the skill materializes at machine scope",
		Registries: []RegistryDef{
			{Name: "scope-reg", Skills: []Skill{
				{Name: "global-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:scope-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--scope", "user", "global-skill"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/skills/global-skill/SKILL.md"}},
		},
	},
	{
		Name: "install/scope_local_writes_to_local",
		Desc: "Given a registry in repo config, When install --scope local is run and synced, Then the skill materializes at repo scope",
		Registries: []RegistryDef{
			{Name: "local-reg", Skills: []Skill{
				{Name: "local-skill"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:local-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--scope", "local", "local-skill"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/skills/local-skill/SKILL.md"}},
		},
	},

	// ===== remove command =====
	{
		Name: "remove/install_then_remove_roundtrip",
		Desc: "Given a skill synced to machine scope, When remove --scope user is run and re-synced, Then the skill is gone from the filesystem",
		Registries: []RegistryDef{
			{Name: "roundtrip-reg", Skills: []Skill{
				{Name: "roundtrip-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:roundtrip-reg}}
    ref: main
    skills:
      - roundtrip-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/skills/roundtrip-skill/SKILL.md"}},
			{Args: []string{"remove", "--scope", "user", "roundtrip-skill"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/skills/roundtrip-skill"}},
		},
	},
	{
		Name: "remove/required_skill_fails",
		Desc: "Given a required:true registry skill, When remove is attempted, Then it exits 1 with 'required' in the error",
		Registries: []RegistryDef{
			{Name: "required-reg", Skills: []Skill{
				{Name: "mandatory-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:required-reg}}
    ref: main
    required: true
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"remove", "mandatory-skill"}, AssertExitCode: intPtr(1),
				OutContains: []string{"required"}},
		},
	},
	{
		Name: "remove/default_true_adds_exclude",
		Desc: "Given default:true auto-selected skills, When remove --scope user is run for one skill and re-synced, Then that skill is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "default-rm", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "drop-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:default-rm}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/keep-skill/SKILL.md",
					"/root/.factory/skills/drop-skill/SKILL.md",
				}},
			{Args: []string{"remove", "--scope", "user", "drop-skill"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/keep-skill/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/skills/drop-skill"}},
		},
	},
	{
		Name: "install/v4_reinstall_clears_only_provider_local_exclude",
		Desc: "Given a v4 default-selected repo skill removed into a local source exclusion, When configured install reinstalls it, Then only that provider's exclusion is cleared and sibling exclusions remain",
		Registries: []RegistryDef{
			{Name: "v4-reinstall-main", Skills: []Skill{{Name: "foo"}, {Name: "keep"}}},
			{Name: "v4-reinstall-other", Skills: []Skill{{Name: "other"}}},
		},
		RepoCfg: `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  main: {git: "{{REG:v4-reinstall-main}}", ref: main}
resources:
  main: {all: true}
`,
		LocalCfg: `
apiVersion: agent-marketplace/v4
sources:
  other: {git: "{{REG:v4-reinstall-other}}", ref: main}
resources:
  other: {exclude: [skill/foo]}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/foo/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"}},
			{Args: []string{"remove", "foo"}, MustPass: true,
				FilesAbsent:  []string{"/workspace/.factory/skills/foo"},
				FilesExist:   []string{"/workspace/.factory/skills/keep/SKILL.md"},
				FileContains: map[string]string{"/workspace/.agents/config.local.yaml": "skill/foo"}},
			{Args: []string{"install", "foo"}, MustPass: true,
				FilesExist:   []string{"/workspace/.factory/skills/foo/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"},
				FileContains: map[string]string{"/workspace/.agents/config.local.yaml": "skill/foo"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:     []string{"/workspace/.factory/skills/foo/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"},
				PathsUnchanged: []string{"/workspace/.agents/config.local.yaml"}},
		},
	},
	{
		Name:       "install/from_named_reinstall_clears_legacy_local_exclude",
		Desc:       "Given a default-selected skill disabled by a legacy local exclude, When named ad-hoc install reinstalls it, Then it materializes and only that exclude is cleared",
		Registries: []RegistryDef{{Name: "adhoc-reinstall-named", Skills: []Skill{{Name: "foo"}, {Name: "keep"}}}},
		RepoCfg: `
apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: {{REG:adhoc-reinstall-named}}
    ref: main
    default: true
`,
		LocalCfg: "apiVersion: agent-marketplace/v2\nskills: {exclude: [foo, keep]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/foo", "/workspace/.factory/skills/keep"}},
			{Args: []string{"install", "foo", "--from", "{{REG:adhoc-reinstall-named}}", "--ref", "main"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/skills/foo/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/keep"},
				FileNotContains: map[string]string{"/workspace/.agents/config.local.yaml": "foo"}, FileContains: map[string]string{"/workspace/.agents/config.local.yaml": "keep"}},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/foo/SKILL.md"}, PathsUnchanged: []string{"/workspace/.agents/config.local.yaml"}},
		},
	},
	{
		Name: "install/from_inferred_reinstall_clears_provider_local_exclude",
		Desc: "Given a v4 default-selected nested skill removed into a provider-scoped local exclude, When inferred-name ad-hoc install reinstalls it, Then only its provider exclusion is cleared",
		Registries: []RegistryDef{
			{Name: "adhoc-reinstall-inferred", Skills: []Skill{{Name: "foo"}}},
			{Name: "adhoc-reinstall-other", Skills: []Skill{{Name: "other"}}},
		},
		RepoCfg: `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  main: {git: "{{REG:adhoc-reinstall-inferred}}", ref: main, path: skills/foo}
resources:
  main: {all: true}
`,
		LocalCfg: `
apiVersion: agent-marketplace/v4
sources:
  other: {git: "{{REG:adhoc-reinstall-other}}", ref: main}
resources:
  other: {exclude: [skill/foo]}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/foo/SKILL.md"}},
			{Args: []string{"remove", "foo"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/foo"}},
			{Args: []string{"install", "--from", "{{REG:adhoc-reinstall-inferred}}", "--ref", "main", "--path", "skills/foo"}, MustPass: true,
				FilesExist:   []string{"/workspace/.factory/skills/foo/SKILL.md"},
				FileContains: map[string]string{"/workspace/.agents/config.local.yaml": "skill/foo"}},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/foo/SKILL.md"}, PathsUnchanged: []string{"/workspace/.agents/config.local.yaml"}},
		},
	},
	{
		Name: "install/reinstall_preserves_default_auto_select",
		Desc: "Given a default:true registry with one skill excluded, When that skill is re-installed, Then the exclude is dropped WITHOUT adding an explicit include so default auto-select keeps every other skill enabled",
		Registries: []RegistryDef{
			{Name: "reinstall-default", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "cohort-analysis"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:reinstall-default}}
    ref: main
    default: true
`,
		Steps: []Step{
			// default:true auto-selects every skill.
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/keep-skill/SKILL.md",
					"/root/.factory/skills/cohort-analysis/SKILL.md",
				}},
			// Remove one skill → recorded as an exclude; others remain.
			{Args: []string{"remove", "--scope", "user", "cohort-analysis"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/keep-skill/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/skills/cohort-analysis"}},
			// Re-install the excluded skill. This must simply drop the exclude
			// and keep default:true — NOT add skills.include (which would flip
			// the entry out of auto-select-all and silently deselect keep-skill).
			{Args: []string{"install", "--scope", "user", "cohort-analysis"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.agents/config.yaml": "default: true",
				},
				FileNotContains: map[string]string{
					"/root/.agents/config.yaml": "include",
				}},
			// Both skills materialize again: proof that default auto-select
			// survived the re-install instead of collapsing to just one skill.
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/keep-skill/SKILL.md",
					"/root/.factory/skills/cohort-analysis/SKILL.md",
				}},
		},
	},
	{
		Name: "remove/from_removes_named_resource",
		Desc: "Given two skills from one registry, When remove --scope user removes one and re-syncs, Then only the removed skill is pruned",
		Registries: []RegistryDef{
			{Name: "from-rm", Skills: []Skill{
				{Name: "from-skill-a"},
				{Name: "from-skill-b"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:from-rm}}
    ref: main
    skills:
      - from-skill-a
      - from-skill-b
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/from-skill-a/SKILL.md",
					"/root/.factory/skills/from-skill-b/SKILL.md",
				}},
			{Args: []string{"remove", "--scope", "user", "from-skill-a"}, MustPass: true,
				OutContains: []string{"from-skill-a"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/skills/from-skill-a"},
				FilesExist:  []string{"/root/.factory/skills/from-skill-b/SKILL.md"}},
		},
	},
	{
		Name: "remove/from_no_name_removes_entry",
		Desc: "Given a registry entry in global config, When remove --scope user --from removes the entire entry and re-syncs, Then the skill is gone",
		Registries: []RegistryDef{
			{Name: "from-entry-rm", Skills: []Skill{
				{Name: "entry-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:from-entry-rm}}
    ref: main
    skills:
      - entry-skill
`,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/skills/entry-skill/SKILL.md"}},
			{Args: []string{"remove", "--scope", "user", "--from", "{{REG:from-entry-rm}}"}, MustPass: true,
				OutContains: []string{"Removed"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/skills/entry-skill"}},
		},
	},

	// ===== multi-resource kinds =====
	{
		Name: "multi_kind/subagents_materialize_as_droids",
		Desc: "Given a registry with a subagent explicitly selected, When sync is run, Then the subagent is compiled as droids/<name>.md",
		Registries: []RegistryDef{
			{Name: "subagent-reg", Subagents: []Subagent{
				{Name: "worker", Tools: []string{"Read", "Edit"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:subagent-reg}}
    ref: main
    subagents:
      - worker
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/worker.md"}},
		},
	},
	{
		Name: "multi_kind/mcp_servers_materialize",
		Desc: "Given a registry with an MCP server explicitly selected, When sync is run, Then mcp.json is written containing the server definition",
		Registries: []RegistryDef{
			{Name: "mcp-reg", MCPServers: []MCPServer{
				{Name: "my-server", Command: "npx", Args: []string{"-y", "@my/mcp-server"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-reg}}
    ref: main
    mcp:
      - my-server
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "my-server",
				}},
		},
	},
	{
		Name: "multi_kind/mcp_windsurf_env_normalized_to_factory",
		Desc: "Given a registry with a Windsurf-format mcp_config.json using ${env:VAR} interpolation, When sync is run with factory harness, Then the MCP server materializes with ${VAR} (canonical form) in the factory mcp.json",
		Registries: []RegistryDef{
			{Name: "windsurf-mcp-reg", Files: map[string]string{
				"mcp_config.json": `{"mcpServers":{"env-server":{"command":"npx","args":["-y","@my/env-server"],"env":{"API_KEY":"${env:MY_API_KEY}","LITERAL_VAL":"keep-me"}}}}`,
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:windsurf-mcp-reg}}
    ref: main
    mcp:
      - env-server
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "env-server",
				},
				OutNotContains: []string{"${env:MY_API_KEY}"},
			},
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"env-server"},
			},
		},
	},
	{
		Name: "multi_kind/mcp_windsurf_env_round_trip_to_windsurf",
		Desc: "Given a registry with a Windsurf-format mcp_config.json using ${env:VAR} interpolation, When sync is run with factory and windsurf harnesses, Then the windsurf mcp_config.json contains ${env:VAR} (re-expanded from canonical form)",
		Registries: []RegistryDef{
			{Name: "windsurf-rt-reg", Files: map[string]string{
				"mcp_config.json": `{"mcpServers":{"rt-server":{"command":"npx","args":["-y","@my/rt-server"],"env":{"TOKEN":"${env:MY_TOKEN}"}}}}`,
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - windsurf
registries:
  - url: {{REG:windsurf-rt-reg}}
    ref: main
    mcp:
      - rt-server
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/mcp.json",
					"/root/.codeium/windsurf/mcp_config.json",
				},
				FileContains: map[string]string{
					"/root/.factory/mcp.json":                 "${MY_TOKEN}",
					"/root/.codeium/windsurf/mcp_config.json": "${env:MY_TOKEN}",
				},
			},
		},
	},
	{
		Name: "multi_kind/install_kind_filter_subagent",
		Desc: "Given a registry with skills and subagents, When only subagents are selected in config, Then subagents materialize but skills do not",
		Registries: []RegistryDef{
			{Name: "kind-filter", Skills: []Skill{
				{Name: "not-this-skill"},
			}, Subagents: []Subagent{
				{Name: "only-this-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:kind-filter}}
    ref: main
    subagents:
      - only-this-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/droids/only-this-agent.md"},
				FilesAbsent: []string{"/root/.factory/skills/not-this-skill"}},
		},
	},

	// ===== subagent renormalization & regression =====

	{
		Name: "subagent/model_mapping_to_factory",
		Desc: "Given a registry with a subagent using model=sonnet, When sync is run, Then the droid file contains the mapped Factory model ID",
		Registries: []RegistryDef{
			{Name: "sub-model", Subagents: []Subagent{
				{Name: "model-agent", Model: "sonnet", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-model}}
    ref: main
    subagents:
      - model-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/model-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/model-agent.md": "model: claude-sonnet-4-5-20250929",
				}},
		},
	},
	{
		Name: "subagent/model_passthrough_to_factory",
		Desc: "Given a registry with a subagent using an unknown model, When sync is run, Then the model value is passed through unchanged",
		Registries: []RegistryDef{
			{Name: "sub-model-pt", Subagents: []Subagent{
				{Name: "passthrough-agent", Model: "custom-model-v2", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-model-pt}}
    ref: main
    subagents:
      - passthrough-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/passthrough-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/passthrough-agent.md": "model: custom-model-v2",
				}},
		},
	},
	{
		Name: "subagent/write_tool_expanded_to_edit_create",
		Desc: "Given a registry with a subagent using Write tool, When sync is run for factory, Then Write is expanded to Edit and Create",
		Registries: []RegistryDef{
			{Name: "sub-tools", Subagents: []Subagent{
				{Name: "write-agent", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-tools}}
    ref: main
    subagents:
      - write-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/write-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/write-agent.md": "Edit",
				}},
			{FileContains: map[string]string{
				"/root/.factory/droids/write-agent.md": "Create",
			}},
		},
	},
	{
		Name: "subagent/unknown_tool_refuses_output",
		Desc: "Given an unrepresentable Factory tool restriction and a valid sibling, When sync runs, Then the restricted droid is suppressed with an error while the valid sibling is installed",
		Registries: []RegistryDef{
			{Name: "sub-tool-pt", Subagents: []Subagent{
				{Name: "unk-tool-agent", Tools: []string{"Read", "CustomTool"}},
				{Name: "known-tool-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-tool-pt}}
    ref: main
    subagents:
      - unk-tool-agent
      - known-tool-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains:  []string{"subagent.factory.tool-unknown"},
				FilesAbsent:  []string{"/root/.factory/droids/unk-tool-agent.md"},
				FilesExist:   []string{"/root/.factory/droids/known-tool-agent.md"},
				FileContains: map[string]string{"/root/.factory/droids/known-tool-agent.md": "- Read"},
			},
		},
	},
	{
		Name: "subagent/effort_renamed_to_reasoning_effort",
		Desc: "Given a registry with a subagent using effort=high, When sync is run for factory, Then the droid file contains reasoningEffort: high and not effort:",
		Registries: []RegistryDef{
			{Name: "sub-effort", Subagents: []Subagent{
				{Name: "effort-agent", Effort: "high", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-effort}}
    ref: main
    subagents:
      - effort-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/effort-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/effort-agent.md": "reasoningEffort: high",
				}},
		},
	},
	{
		// disallowedTools and isolation are RESTRICTIONS. Factory has no way to
		// express either, so emitting the droid with the field merely omitted
		// would hand the agent BROADER permissions than the author wrote. The
		// compiler fails closed instead (compile/factory/translator.go), and
		// these two cases pin that: artifact suppressed, sync exits 1.
		Name: "subagent/disallowed_tools_fails_closed",
		Desc: "Given a registry with a subagent having disallowedTools, When sync is run for factory, Then the artifact is suppressed and sync exits 1 rather than silently granting the disallowed tool",
		Registries: []RegistryDef{
			{Name: "sub-disallowed", Subagents: []Subagent{
				{Name: "disallowed-agent", Tools: []string{"Read"}, DisallowedTools: []string{"Bash"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-disallowed}}
    ref: main
    subagents:
      - disallowed-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"subagent.factory.disallowed-tools-dropped"},
				FilesAbsent: []string{"/root/.factory/droids/disallowed-agent.md"},
			},
		},
	},
	{
		Name: "subagent/isolation_fails_closed",
		Desc: "Given a registry with a subagent having isolation=worktree, When sync is run for factory, Then the artifact is suppressed and sync exits 1 rather than emitting an unisolated droid",
		Registries: []RegistryDef{
			{Name: "sub-isolation", Subagents: []Subagent{
				{Name: "isolation-agent", Tools: []string{"Read"}, Isolation: "worktree"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-isolation}}
    ref: main
    subagents:
      - isolation-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"subagent.factory.isolation-dropped"},
				FilesAbsent: []string{"/root/.factory/droids/isolation-agent.md"},
			},
		},
	},
	{
		Name: "subagent/install_kind_subagent_disambiguation",
		Desc: "Given a registry with a skill and a subagent sharing the same name, When install --kind subagent is run, Then the subagent is installed and the skill is not",
		Registries: []RegistryDef{
			{Name: "sub-disambig", Skills: []Skill{
				{Name: "shared-name"},
			}, Subagents: []Subagent{
				{Name: "shared-name", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries: []
`,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"install", "--kind", "subagent", "--scope", "user", "shared-name", "--from", "{{REG:sub-disambig}}", "--ref", "main"}, MustPass: true,
				OutContains: []string{"shared-name"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/droids/shared-name.md"},
				FilesAbsent: []string{"/root/.factory/skills/shared-name"}},
		},
	},
	{
		Name: "subagent/remove_round_trip",
		Desc: "Given a subagent installed in global config, When remove is run and sync follows, Then the subagent is removed from both config and filesystem",
		Registries: []RegistryDef{
			{Name: "sub-remove", Subagents: []Subagent{
				{Name: "removable-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-remove}}
    ref: main
    subagents:
      - removable-agent
`,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/removable-agent.md"}},
			{Args: []string{"remove", "--scope", "user", "removable-agent"}, MustPass: true,
				OutContains: []string{"Removed"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/droids/removable-agent.md"}},
		},
	},
	{
		Name: "subagent/remove_from_v4",
		Desc: "Given v4 user Git selections, When a named subagent is removed with --from, Then only that subagent is removed and the exact lock survives",
		Registries: []RegistryDef{{Name: "v4-sub-remove", Subagents: []Subagent{
			{Name: "removable-agent", Tools: []string{"Read"}},
			{Name: "keep-agent", Tools: []string{"Read"}},
		}}},
		GlobalCfg: `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  agents: {git: "{{REG:v4-sub-remove}}", ref: main}
resources:
  agents: {include: [subagent/removable-agent, subagent/keep-agent]}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/removable-agent.md", "/root/.factory/droids/keep-agent.md"}},
			{Args: []string{"remove", "--kind", "subagent", "--scope", "user", "--from", "{{REG:v4-sub-remove}}", "removable-agent"}, MustPass: true,
				FilesAbsent:    []string{"/root/.factory/droids/removable-agent.md"},
				PathsUnchanged: []string{"/root/.agents/lock.yaml", "/root/.factory/droids/keep-agent.md"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/droids/removable-agent.md"},
				FilesExist:  []string{"/root/.factory/droids/keep-agent.md"}},
		},
	},
	{
		Name: "subagent/remove_from_no_name_removes_entry",
		Desc: "Given a registry entry in global config with a subagent, When remove --scope user --from removes the entire entry and re-syncs, Then the subagent is gone",
		Registries: []RegistryDef{
			{Name: "sub-from-entry-rm", Subagents: []Subagent{
				{Name: "entry-agent", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-from-entry-rm}}
    ref: main
    subagents:
      - entry-agent
`,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/entry-agent.md"}},
			{Args: []string{"remove", "--scope", "user", "--from", "{{REG:sub-from-entry-rm}}"}, MustPass: true,
				OutContains: []string{"Removed"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/droids/entry-agent.md"}},
		},
	},
	{
		Name: "subagent/multi_harness_output",
		Desc: "Given factory and cursor harnesses configured with a subagent, When sync is run, Then the subagent materializes in both harness output directories",
		Registries: []RegistryDef{
			{Name: "sub-multi", Subagents: []Subagent{
				{Name: "multi-agent", Model: "sonnet", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - cursor
registries:
  - url: {{REG:sub-multi}}
    ref: main
    subagents:
      - multi-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/droids/multi-agent.md",
					"/root/.cursor/agents/multi-agent.md",
				},
				FileContains: map[string]string{
					"/root/.factory/droids/multi-agent.md": "model: claude-sonnet-4-5-20250929",
					"/root/.cursor/agents/multi-agent.md":  "model: claude-sonnet-4-5-20250929",
				}},
		},
	},
	{
		// The fixture deliberately carries NO disallowedTools and NO isolation.
		// Factory fails closed on both (see subagent/disallowed_tools_fails_closed
		// and subagent/isolation_fails_closed), which suppresses the artifact
		// entirely — so including them here would leave every renormalization
		// assertion below with no file to inspect, and the test would pass on an
		// absent artifact rather than on a correctly rewritten one.
		Name: "subagent/full_renormalization_end_to_end",
		Desc: "Given a registry with a subagent using model=sonnet, effort=high, tools=[Read,Write], When sync is run for factory, Then all renormalizations apply: model mapped, effort→reasoningEffort, Write→Edit+Create",
		Registries: []RegistryDef{
			{Name: "sub-full", Subagents: []Subagent{
				{Name: "full-agent", Model: "sonnet", Tools: []string{"Read", "Write"}, Effort: "high"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:sub-full}}
    ref: main
    subagents:
      - full-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/full-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "model: claude-sonnet-4-5-20250929",
				},
				FileNotContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "isolation:",
				},
			},
			{FileContains: map[string]string{
				"/root/.factory/droids/full-agent.md": "reasoningEffort: high",
			},
				FileNotContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "\neffort:",
				}},
			{FileContains: map[string]string{
				"/root/.factory/droids/full-agent.md": "Edit",
			}},
			{FileContains: map[string]string{
				"/root/.factory/droids/full-agent.md": "Create",
			},
				FileNotContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "\nWrite",
				}},
		},
	},

	// ===== MCP server parity =====

	{
		Name: "mcp/multi_server_merge",
		Desc: "Given a registry with two MCP servers explicitly selected, When sync is run, Then mcp.json contains both server entries merged into a single file",
		Registries: []RegistryDef{
			{Name: "mcp-multi", MCPServers: []MCPServer{
				{Name: "server-a", Command: "npx", Args: []string{"-y", "@a/server"}},
				{Name: "server-b", Command: "node", Args: []string{"server-b.js"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-multi}}
    ref: main
    mcp:
      - server-a
      - server-b
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "server-a",
				}},
			// Verify second server in same file against the same post-sync state.
			{FileContains: map[string]string{
				"/root/.factory/mcp.json": "server-b",
			}},
		},
	},
	{
		Name: "mcp/multi_registry_merge",
		Desc: "Given two registries each providing an MCP server, When both are selected in config, Then mcp.json contains both servers without clobbering",
		Registries: []RegistryDef{
			{Name: "mcp-reg-a", MCPServers: []MCPServer{
				{Name: "server-alpha", Command: "npx", Args: []string{"-y", "@alpha/server"}},
			}},
			{Name: "mcp-reg-b", MCPServers: []MCPServer{
				{Name: "server-beta", Command: "node", Args: []string{"beta.js"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-reg-a}}
    ref: main
    mcp:
      - server-alpha
  - url: {{REG:mcp-reg-b}}
    ref: main
    mcp:
      - server-beta
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "server-alpha",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "server-beta",
				}},
		},
	},
	{
		Name: "mcp/preserve_existing_entries",
		Desc: "Given a pre-existing user-authored mcp.json entry, When sync adds a managed MCP server, Then the pre-existing entry is preserved alongside the managed one",
		Registries: []RegistryDef{
			{Name: "mcp-preserve", MCPServers: []MCPServer{
				{Name: "managed-server", Command: "npx", Args: []string{"-y", "@my/mcp-server"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-preserve}}
    ref: main
    mcp:
      - managed-server
`,
		Steps: []Step{
			// Pre-seed mcp.json with a user-authored server.
			{Action: ActionWriteFile,
				Path: "/root/.factory/mcp.json",
				Body: `{"mcpServers":{"user-server":{"command":"node","args":["user-server.js"]}}}`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "managed-server",
				}},
			// User entry must survive the sync.
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "user-server",
				}},
		},
	},
	{
		Name: "mcp/args_env_roundtrip",
		Desc: "Given an MCP server with args and env, When sync is run, Then mcp.json contains the exact args and env values",
		Registries: []RegistryDef{
			{Name: "mcp-roundtrip", MCPServers: []MCPServer{
				{Name: "env-server", Command: "npx", Args: []string{"-y", "@my/env-server"}, Env: map[string]string{
					"API_KEY":    "test-value-placeholder",
					"DEBUG_MODE": "true",
				}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-roundtrip}}
    ref: main
    mcp:
      - env-server
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "env-server",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "test-value-placeholder",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "@my/env-server",
				}},
		},
	},
	{
		Name: "mcp/install_kind_mcp",
		Desc: "Given a registry with a skill and an MCP server sharing no name conflict, When install --kind mcp --scope user is run, Then the MCP server is installed and the skill is not",
		Registries: []RegistryDef{
			{Name: "mcp-kind-filter", Skills: []Skill{
				{Name: "not-this-skill"},
			}, MCPServers: []MCPServer{
				{Name: "only-this-mcp", Command: "npx", Args: []string{"-y", "@my/only-mcp"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-kind-filter}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--kind", "mcp", "--scope", "user", "only-this-mcp"}, MustPass: true,
				OutContains: []string{"only-this-mcp"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/mcp.json"},
				FilesAbsent: []string{"/root/.factory/skills/not-this-skill"}},
		},
	},
	{
		Name: "mcp/install_skill_prerequisite",
		Desc: "Given a skill that requires an unselected MCP server, When the skill is installed, Then installation warns without selecting or materializing the MCP prerequisite",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite",
				Skills: []Skill{
					{
						Name:        "smoke-test-kb",
						Description: "Answer smoke test questions from Confluence",
						Requires:    []string{"mcp:genai-atlassian"},
					},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: `
harnesses:
  - factory
registries:
  - url: {{REG:mcp-prerequisite}}
    ref: main
`,
		Steps: []Step{
			{
				Args:     []string{"install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite}}", "--ref", "main", "--scope", "user"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					`agent-marketplace install "genai-atlassian" --kind mcp`,
				},
				FilesExist: []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FileNotContains: map[string]string{
					"/root/.agents/config.yaml": "genai-atlassian",
				},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
		},
	},
	{
		Name: "mcp/install_skill_preserves_excluded_prerequisite",
		Desc: "Given an explicitly excluded MCP prerequisite, When the requiring skill is installed, Then installation warns and preserves the explicit exclusion",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-excluded",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: `
harnesses:
  - factory
registries:
  - url: {{REG:mcp-prerequisite-excluded}}
    ref: main
    mcp:
      exclude:
        - genai-atlassian
`,
		Steps: []Step{
			{
				Args:     []string{"install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite-excluded}}", "--ref", "main", "--scope", "user"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					`agent-marketplace install "genai-atlassian" --kind mcp`,
				},
				FilesExist: []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FileContains: map[string]string{
					"/root/.agents/config.yaml": "genai-atlassian",
				},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
		},
	},
	{
		Name: "mcp/sync_reports_unmet_prerequisite",
		Desc: "Given a selected skill with an unselected MCP prerequisite, When sync runs, Then the skill is materialized and a remediable warning is emitted",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-unselected",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: `
harnesses:
  - factory
registries:
  - url: {{REG:mcp-prerequisite-unselected}}
    ref: main
    skills:
      include:
        - smoke-test-kb
`,
		Steps: []Step{
			{
				Args:        []string{"sync"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install "genai-atlassian" --kind mcp`},
				FilesExist:  []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
			{
				Args:        []string{"status"},
				MustPass:    true,
				OutContains: []string{"UNMET REQUIREMENTS", "skill:smoke-test-kb", "mcp:genai-atlassian", `agent-marketplace install "genai-atlassian" --kind mcp`},
			},
			{
				Args:        []string{"sync", "--check"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install "genai-atlassian" --kind mcp`},
			},
			{
				Args:        []string{"doctor"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install "genai-atlassian" --kind mcp`},
			},
		},
	},
	{
		Name: "mcp/install_all_kind_mcp",
		Desc: "Given a registry with skills and MCP servers, When install --all --kind mcp is run and synced, Then MCP servers are selected in the registry entry",
		Registries: []RegistryDef{
			{Name: "mcp-all-kind", Skills: []Skill{
				{Name: "not-this-skill"},
			}, MCPServers: []MCPServer{
				{Name: "batch-mcp-a", Command: "npx", Args: []string{"-y", "@a/mcp"}},
				{Name: "batch-mcp-b", Command: "node", Args: []string{"b.js"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-all-kind}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--all", "--kind", "mcp"}, MustPass: true,
				OutContains: []string{"batch-mcp-a"}},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"batch-mcp-a"}},
		},
	},
	{
		Name: "mcp/remove_roundtrip",
		Desc: "Given an MCP server synced to machine scope, When remove --scope user is run and re-synced, Then the MCP server is gone from mcp.json",
		Registries: []RegistryDef{
			{Name: "mcp-roundtrip-rm", MCPServers: []MCPServer{
				{Name: "roundtrip-mcp", Command: "npx", Args: []string{"-y", "@rm/mcp"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-roundtrip-rm}}
    ref: main
    mcp:
      - roundtrip-mcp
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "roundtrip-mcp",
				}},
			{Args: []string{"remove", "--scope", "user", "roundtrip-mcp"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": `"mcpServers": {}`,
				},
				FileNotContains: map[string]string{
					"/root/.factory/mcp.json": "_managed_by",
				},
				OutNotContains: []string{"roundtrip-mcp"}},
		},
	},
	{
		Name: "mcp/remove_default_true_adds_exclude",
		Desc: "Given default:true auto-selected MCP servers, When remove --scope user is run for one MCP server and re-synced, Then that server is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "mcp-default-rm", MCPServers: []MCPServer{
				{Name: "keep-mcp", Command: "npx", Args: []string{"-y", "@keep/mcp"}},
				{Name: "drop-mcp", Command: "node", Args: []string{"drop.js"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-default-rm}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "keep-mcp",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "drop-mcp",
				}},
			{Args: []string{"remove", "--scope", "user", "drop-mcp"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "keep-mcp",
				},
				OutNotContains: []string{"drop-mcp"}},
		},
	},
	{
		Name: "mcp/install_from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from and --kind mcp, Then the MCP server appears in list output and sync materializes it",
		Registries: []RegistryDef{
			{Name: "mcp-adhoc", MCPServers: []MCPServer{
				{Name: "adhoc-mcp", Command: "npx", Args: []string{"-y", "@adhoc/mcp"}},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
`,
		Steps: []Step{
			{Args: []string{"install", "adhoc-mcp", "--from", "{{REG:mcp-adhoc}}", "--ref", "main", "--kind", "mcp", "--scope", "user"}, MustPass: true,
				OutContains: []string{"adhoc-mcp"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/mcp.json"},
				FileContains: map[string]string{
					"/root/.factory/mcp.json": "adhoc-mcp",
				}},
		},
	},
	{
		Name: "mcp/collision_same_name_different_registries",
		Desc: "Given two registries each providing an MCP server named 'shared-mcp', When sync is run, Then a collision warning is emitted",
		Registries: []RegistryDef{
			{Name: "mcp-reg-a", MCPServers: []MCPServer{
				{Name: "shared-mcp", Command: "npx", Args: []string{"-y", "@a/mcp"}},
			}},
			{Name: "mcp-reg-b", MCPServers: []MCPServer{
				{Name: "shared-mcp", Command: "node", Args: []string{"b.js"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-reg-a}}
    ref: main
    mcp:
      - shared-mcp
  - url: {{REG:mcp-reg-b}}
    ref: main
    mcp:
      - shared-mcp
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{"collision"}},
		},
	},
	{
		Name: "mcp/malformed_degradation",
		Desc: "Given a registry with a malformed MCP file and a valid skill, When sync is run, Then the skill still materializes and the malformed MCP produces an error diagnostic",
		Registries: []RegistryDef{
			{Name: "mcp-malformed",
				Skills: []Skill{{Name: "survivor-skill"}},
				Files: map[string]string{
					// Add Factory plugin marker so Factory ingester handles MCP gracefully.
					".factory-plugin/plugin.json": `{"skills":[{"name":"survivor-skill"}]}`,
					"mcp.json":                    `{"mcpServers":{bad-json`,
				}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-malformed}}
    ref: main
    skills:
      - survivor-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/survivor-skill/SKILL.md"},
				OutContains: []string{"Malformed"},
			},
		},
	},
	{
		Name: "mcp/sync_check_detects_drift",
		Desc: "Given a config with one MCP server, When sync --check is run before any sync, Then it exits 1; after sync exits 0; after overwriting mcp.json exits 1 again",
		Registries: []RegistryDef{
			{Name: "mcp-drift", MCPServers: []MCPServer{
				{Name: "drift-mcp", Command: "npx", Args: []string{"-y", "@drift/mcp"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:mcp-drift}}
    ref: main
    mcp:
      - drift-mcp
`,
		Steps: []Step{
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			// Overwrite mcp.json to introduce drift.
			{Action: ActionWriteFile, Path: "/root/.factory/mcp.json", Body: `{"mcpServers":{}}`},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
		},
	},

	// ===== command parity with skills =====

	{
		Name: "command/tag_based_selection",
		Desc: "Given a registry with k8s-tagged and python-tagged commands and tags: [k8s] in config, When sync is run, Then only k8s-tagged commands are materialized",
		Registries: []RegistryDef{
			{Name: "cmd-tag-selection", Commands: []Command{
				{Name: "k8s-rollout", Description: "Kubernetes rollout", Command: "kubectl rollout", Tags: []string{"k8s", "infra"}},
				{Name: "k8s-debug", Description: "Kubernetes debug", Command: "kubectl debug", Tags: []string{"k8s", "debug"}},
				{Name: "python-lint", Description: "Python linting", Command: "ruff check", Tags: []string{"python", "lint"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-tag-selection}}
    ref: main
    tags:
      - k8s
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/k8s-rollout.md",
					"/root/.factory/commands/k8s-debug.md",
				},
				FilesAbsent: []string{"/root/.factory/commands/python-lint.md"}},
		},
	},
	{
		Name: "command/install_by_name",
		Desc: "Given a registry with a command, When install is run for the command name, Then the command appears in list output",
		Registries: []RegistryDef{
			{Name: "cmd-install", Commands: []Command{
				{Name: "greet", Description: "Greeting command", Command: "echo hello"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-install}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "greet"}, MustPass: true,
				OutContains: []string{"greet"}},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"list"}, MustPass: true,
				OutContains: []string{"greet"}},
		},
	},
	{
		Name: "command/install_kind_filter",
		Desc: "Given a registry with a skill and command sharing a name, When only the command is selected in config, Then only the command materializes (not the skill)",
		Registries: []RegistryDef{
			{Name: "cmd-kind-install",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill"}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-kind-install}}
    ref: main
    commands:
      - deploy
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/commands/deploy.md"},
				FilesAbsent: []string{"/root/.factory/skills/deploy"}},
		},
	},
	{
		Name: "command/install_scope_user",
		Desc: "Given a registry with a command, When install --scope user is run and synced, Then the command materializes at machine scope",
		Registries: []RegistryDef{
			{Name: "cmd-scope-user", Commands: []Command{
				{Name: "global-cmd", Description: "Global command", Command: "echo global"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-scope-user}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--scope", "user", "global-cmd"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/global-cmd.md"}},
		},
	},
	{
		Name: "command/install_scope_local",
		Desc: "Given a registry in repo config with a command, When install --scope local is run and synced, Then the command materializes at repo scope",
		Registries: []RegistryDef{
			{Name: "cmd-scope-local", Commands: []Command{
				{Name: "local-cmd", Description: "Local command", Command: "echo local"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:cmd-scope-local}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--scope", "local", "local-cmd"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/commands/local-cmd.md"}},
		},
	},
	{
		Name: "command/remove_roundtrip",
		Desc: "Given a command synced to machine scope, When remove --scope user is run and re-synced, Then the command is gone from the filesystem",
		Registries: []RegistryDef{
			{Name: "cmd-roundtrip", Commands: []Command{
				{Name: "roundtrip-cmd", Description: "Roundtrip command", Command: "echo roundtrip"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-roundtrip}}
    ref: main
    commands:
      - roundtrip-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/roundtrip-cmd.md"}},
			{Args: []string{"remove", "--scope", "user", "roundtrip-cmd"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/commands/roundtrip-cmd.md"}},
		},
	},
	{
		Name: "command/remove_from_registry",
		Desc: "Given two commands from one registry, When remove --scope user removes one and re-syncs, Then only the removed command is pruned",
		Registries: []RegistryDef{
			{Name: "cmd-from-rm", Commands: []Command{
				{Name: "cmd-a", Description: "Command A", Command: "echo a"},
				{Name: "cmd-b", Description: "Command B", Command: "echo b"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-from-rm}}
    ref: main
    commands:
      - cmd-a
      - cmd-b
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/cmd-a.md",
					"/root/.factory/commands/cmd-b.md",
				}},
			{Args: []string{"remove", "--scope", "user", "cmd-a"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/commands/cmd-a.md"},
				FilesExist:  []string{"/root/.factory/commands/cmd-b.md"}},
		},
	},
	{
		Name: "command/required_immune_to_excludes",
		Desc: "Given a required:true registry with commands, When repo and local configs try to exclude a command, Then the command remains materialized",
		Registries: []RegistryDef{
			{Name: "cmd-required", Commands: []Command{
				{Name: "security-scan", Description: "Mandatory security scan", Command: "scan --deep", Tags: []string{"security"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-required}}
    ref: main
    required: true
    default: true
`,
		RepoCfg: `
commands:
  exclude:
    - security-scan
`,
		LocalCfg: `
commands:
  exclude:
    - security-scan
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/security-scan.md"}},
		},
	},
	{
		Name: "command/default_true_auto_selects",
		Desc: "Given a registry with default:true containing commands, When sync is run, Then all commands are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "cmd-default", Commands: []Command{
				{Name: "auto-cmd-a", Description: "Auto-selected A", Command: "echo a"},
				{Name: "auto-cmd-b", Description: "Auto-selected B", Command: "echo b"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-default}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/auto-cmd-a.md",
					"/root/.factory/commands/auto-cmd-b.md",
				}},
		},
	},
	{
		Name: "command/collision_same_name_different_registries",
		Desc: "Given two registries each providing a command named 'deploy', When sync is run, Then a collision warning is emitted and one command materializes",
		Registries: []RegistryDef{
			{Name: "cmd-registry-a", Commands: []Command{
				{Name: "deploy", Description: "Deploy from registry A", Command: "echo a"},
			}},
			{Name: "cmd-registry-b", Commands: []Command{
				{Name: "deploy", Description: "Deploy from registry B", Command: "echo b"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-registry-a}}
    ref: main
    commands:
      - deploy
  - url: {{REG:cmd-registry-b}}
    ref: main
    commands:
      - deploy
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{"deploy", "multiple registries"},
				FilesExist:  []string{"/root/.factory/commands/deploy.md"}},
		},
	},
	{
		Name: "command/multi_harness_output",
		Desc: "Given factory and claude harnesses configured with a command, When sync is run, Then the command appears in both harness output directories",
		Registries: []RegistryDef{
			{Name: "cmd-multi-harness", Commands: []Command{
				{Name: "universal-cmd", Description: "Appears in all harnesses", Command: "echo universal"},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - claude
registries:
  - url: {{REG:cmd-multi-harness}}
    ref: main
    commands:
      - universal-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/universal-cmd.md",
					"/root/.claude/commands/universal-cmd.md",
				}},
		},
	},
	{
		Name: "command/drift_detection",
		Desc: "Given a config with one command, When sync --check is run before any sync, Then it exits 1; after sync exits 0",
		Registries: []RegistryDef{
			{Name: "cmd-drift", Commands: []Command{
				{Name: "drift-cmd", Description: "Tests drift detection", Command: "echo drift"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-drift}}
    ref: main
    commands:
      - drift-cmd
`,
		Steps: []Step{
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
		},
	},
	{
		Name: "command/idempotent_sync",
		Desc: "Given a command synced once, When sync is run again, Then output is stable and no drift is detected",
		Registries: []RegistryDef{
			{Name: "cmd-idem", Commands: []Command{
				{Name: "idem-cmd", Description: "Idempotent command", Command: "echo idem"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-idem}}
    ref: main
    commands:
      - idem-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
		},
	},
	{
		Name: "command/precedence_local_exclude",
		Desc: "Given repo config including two commands and local config excluding one, When sync is run, Then the locally excluded command is absent while the other remains",
		Registries: []RegistryDef{
			{Name: "cmd-precedence", Commands: []Command{
				{Name: "wanted-cmd", Description: "Wanted command", Command: "echo wanted"},
				{Name: "unwanted-cmd", Description: "Unwanted command", Command: "echo unwanted"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:cmd-precedence}}
    ref: main
    commands:
      include:
        - wanted-cmd
        - unwanted-cmd
`,
		LocalCfg: `
commands:
  exclude:
    - unwanted-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/workspace/.factory/commands/wanted-cmd.md"},
				FilesAbsent: []string{"/workspace/.factory/commands/unwanted-cmd.md"}},
		},
	},
	{
		Name: "command/harness_removal_prunes_command_artifacts",
		Desc: "Given a command synced to factory and claude, When claude is removed from harnesses and re-synced, Then the claude command artifact is pruned while factory remains",
		Registries: []RegistryDef{
			{Name: "cmd-harness-rm", Commands: []Command{
				{Name: "prunable-cmd", Description: "Will be pruned from claude", Command: "echo prune"},
			}},
		},
		GlobalCfg: `
harnesses:
  - factory
  - claude
registries:
  - url: {{REG:cmd-harness-rm}}
    ref: main
    commands:
      - prunable-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/prunable-cmd.md",
					"/root/.claude/commands/prunable-cmd.md",
				}},
			{Action: ActionWriteGlobalConfig, Body: `
harnesses:
  - factory
registries:
  - url: {{REG:cmd-harness-rm}}
    ref: main
    commands:
      - prunable-cmd
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/commands/prunable-cmd.md"},
				FilesAbsent: []string{"/root/.claude/commands/prunable-cmd.md"}},
		},
	},
	{
		Name: "command/install_from_adhoc_registry",
		Desc: "Given no pre-configured registries, When install is run with --from pointing to a registry and --kind command, Then the command appears in list output and sync materializes it",
		Registries: []RegistryDef{
			{Name: "cmd-adhoc", Commands: []Command{
				{Name: "adhoc-cmd", Description: "Ad-hoc command", Command: "echo adhoc"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"install", "adhoc-cmd", "--from", "{{REG:cmd-adhoc}}", "--ref", "main", "--kind", "command"}, MustPass: true,
				OutContains: []string{"adhoc-cmd"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/commands/adhoc-cmd.md"}},
			{Args: []string{"list"}, MustPass: true,
				OutContains: []string{"adhoc-cmd"}},
		},
	},
	{
		Name: "command/strict_mode_exits_nonzero_on_missing",
		Desc: "Given a config selecting a command absent from the registry, When sync --strict is run, Then it exits 1 mentioning the missing command",
		Registries: []RegistryDef{
			{Name: "cmd-strict", Commands: []Command{
				{Name: "present-cmd", Description: "Present command", Command: "echo present"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-strict}}
    ref: main
    commands:
      - present-cmd
      - absent-cmd
`,
		Steps: []Step{
			{Args: []string{"sync", "--strict"}, AssertExitCode: intPtr(1),
				OutContains: []string{"absent-cmd"}},
		},
	},
	{
		Name: "command/full_lifecycle",
		Desc: "Given a command in global config, When sync→status→remove→sync cycle is run, Then the command appears, is visible in status, and is pruned after removal",
		Registries: []RegistryDef{
			{Name: "cmd-lifecycle", Commands: []Command{
				{Name: "lifecycle-cmd", Description: "Lifecycle command", Command: "echo lifecycle"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-lifecycle}}
    ref: main
    commands:
      - lifecycle-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/lifecycle-cmd.md"}},
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"lifecycle-cmd"}},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			{Args: []string{"remove", "--scope", "user", "lifecycle-cmd"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/commands/lifecycle-cmd.md"}},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
		},
	},

	{
		Name: "command/install_missing_exits_nonzero",
		Desc: "Given a registry that does not contain the requested command, When install nonexistent-cmd is run, Then it exits 1 with an error",
		Registries: []RegistryDef{
			{Name: "cmd-missing", Commands: []Command{
				{Name: "exists-cmd", Description: "Exists", Command: "echo exists"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-missing}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "nonexistent-cmd"}, AssertExitCode: intPtr(1),
				OutContains: []string{"nonexistent"}},
		},
	},
	{
		Name: "command/remove_required_fails",
		Desc: "Given a required:true registry command, When remove is attempted, Then it exits 1 with 'required' in the error",
		Registries: []RegistryDef{
			{Name: "cmd-required-rm", Commands: []Command{
				{Name: "mandatory-cmd", Description: "Mandatory", Command: "echo mandatory"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-required-rm}}
    ref: main
    required: true
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"remove", "mandatory-cmd"}, AssertExitCode: intPtr(1),
				OutContains: []string{"required"}},
		},
	},
	{
		Name: "command/remove_default_true_adds_exclude",
		Desc: "Given default:true auto-selected commands, When remove --scope user is run for one command and re-synced, Then that command is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "cmd-default-rm", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-default-rm}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/keep-cmd.md",
					"/root/.factory/commands/drop-cmd.md",
				}},
			{Args: []string{"remove", "--scope", "user", "drop-cmd"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/commands/keep-cmd.md"},
				FilesAbsent: []string{"/root/.factory/commands/drop-cmd.md"}},
		},
	},
	{
		Name: "command/remove_from_no_name_removes_entry",
		Desc: "Given a registry entry in global config with a command, When remove --scope user --from removes the entire entry and re-syncs, Then the command is gone",
		Registries: []RegistryDef{
			{Name: "cmd-from-entry-rm", Commands: []Command{
				{Name: "entry-cmd", Description: "Entry command", Command: "echo entry"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-from-entry-rm}}
    ref: main
    commands:
      - entry-cmd
`,
		RepoCfg: `
registries: []
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/entry-cmd.md"}},
			{Args: []string{"remove", "--scope", "user", "--from", "{{REG:cmd-from-entry-rm}}"}, MustPass: true,
				OutContains: []string{"Removed"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/commands/entry-cmd.md"}},
		},
	},
	{
		Name: "command/precedence_repo_exclude_overrides_repo_include",
		Desc: "Given repo config with both include and exclude for cmd-a, When sync is run, Then exclude wins and cmd-a is absent while cmd-b is present",
		Registries: []RegistryDef{
			{Name: "cmd-repo-precedence", Commands: []Command{
				{Name: "cmd-a", Description: "Command A", Command: "echo a"},
				{Name: "cmd-b", Description: "Command B", Command: "echo b"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:cmd-repo-precedence}}
    ref: main
    commands:
      include:
        - cmd-a
        - cmd-b
      exclude:
        - cmd-a
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.factory/commands/cmd-a.md"},
				FilesExist:  []string{"/workspace/.factory/commands/cmd-b.md"}},
		},
	},
	{
		Name: "command/precedence_include_exclude_same_level",
		Desc: "Given a registry entry with both include and exclude for the same command, When sync is run, Then exclude wins",
		Registries: []RegistryDef{
			{Name: "cmd-same-level", Commands: []Command{
				{Name: "included-cmd", Description: "Should be included", Command: "echo in"},
				{Name: "conflicted-cmd", Description: "Include and exclude at same level", Command: "echo conflict"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-same-level}}
    ref: main
    commands:
      include:
        - included-cmd
        - conflicted-cmd
      exclude:
        - conflicted-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/commands/included-cmd.md"},
				FilesAbsent: []string{"/root/.factory/commands/conflicted-cmd.md"}},
		},
	},
	{
		Name: "command/precedence_org_config_sets_floor",
		Desc: "Given org config with a required registry containing commands, When global config has no registries, Then the org-mandated command is still materialized",
		Registries: []RegistryDef{
			{Name: "cmd-org", Commands: []Command{
				{Name: "compliance-cmd", Description: "Org-mandated compliance command", Command: "echo compliance"},
			}},
		},
		OrgCfg: `
apiVersion: agent-marketplace/v3
harnesses:
  - factory
registries:
  - url: {{REG:cmd-org}}
    ref: main
    required: true
    default: true
`,
		GlobalCfg: ``,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/compliance-cmd.md"}},
		},
	},
	{
		Name: "command/install_tag_selects_matching",
		Desc: "Given a registry with k8s-tagged commands explicitly configured, When install --tag k8s is run, Then the k8s-tagged commands are reported",
		Registries: []RegistryDef{
			{Name: "cmd-tag-install", Skills: []Skill{
				{Name: "k8s-setup-skill", Tags: []string{"k8s"}},
			}, Commands: []Command{
				{Name: "k8s-setup", Description: "Kubernetes setup", Command: "kubectl apply", Tags: []string{"k8s"}},
				{Name: "python-fmt", Description: "Python formatting", Command: "black .", Tags: []string{"python"}},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-tag-install}}
    ref: main
`,
		Steps: []Step{
			// install --tag discovers all kinds with matching tags
			{Args: []string{"install", "--tag", "k8s"}, MustPass: true,
				OutContains: []string{"k8s"}},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"list", "--kind", "command"}, MustPass: true,
				OutContains: []string{"k8s-setup"}},
		},
	},
	{
		Name: "command/install_all_kind_filter",
		Desc: "Given a registry with skills and commands, When install --all --kind command is run and synced, Then commands are selected in the registry entry",
		Registries: []RegistryDef{
			{Name: "cmd-all-kind", Skills: []Skill{
				{Name: "not-this-skill"},
			}, Commands: []Command{
				{Name: "only-this-cmd", Description: "Only command", Command: "echo only"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-all-kind}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"install", "--all", "--kind", "command"}, MustPass: true,
				OutContains: []string{"only-this-cmd"}},
			{Args: []string{"sync"}, MustPass: true},
			// install --all --kind command creates a default:true entry;
			// verify the command materialized (skills may also appear due to default:true).
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"only-this-cmd"}},
		},
	},
	{
		Name: "command/precedence_default_true_with_exclude",
		Desc: "Given a default:true registry with an explicit command exclude, When sync is run, Then excluded commands are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "cmd-default-exclude", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-default-exclude}}
    ref: main
    default: true
    commands:
      exclude:
        - drop-cmd
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/commands/keep-cmd.md"},
				FilesAbsent: []string{"/root/.factory/commands/drop-cmd.md"}},
		},
	},
	{
		Name: "command/collision_same_name_different_kinds",
		Desc: "Given a registry with a skill and a command sharing the name 'deploy', When both are selected in config, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "cmd-skill-same-name",
				Skills:   []Skill{{Name: "deploy", Description: "Deploy skill"}},
				Commands: []Command{{Name: "deploy", Description: "Deploy command", Command: "echo deploy"}}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:cmd-skill-same-name}}
    ref: main
    skills:
      - deploy
    commands:
      - deploy
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/deploy/SKILL.md",
					"/root/.factory/commands/deploy.md",
				}},
		},
	},

	// ===== sync edge cases =====
	{
		Name: "sync/strict_mode_exits_nonzero_on_missing",
		Desc: "Given a config selecting a skill absent from the registry, When sync --strict is run, Then it exits 1 mentioning the missing skill",
		Registries: []RegistryDef{
			{Name: "strict-reg", Skills: []Skill{
				{Name: "present-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:strict-reg}}
    ref: main
    skills:
      - present-skill
      - absent-skill
`,
		Steps: []Step{
			{Args: []string{"sync", "--strict"}, AssertExitCode: intPtr(1),
				OutContains: []string{"absent-skill"}},
		},
	},
	{
		Name: "sync/idempotent_double_sync",
		Desc: "Given a skill synced once, When sync and sync --check are run again, Then both succeed with no drift",
		Registries: []RegistryDef{
			{Name: "idem-reg", Skills: []Skill{
				{Name: "idem-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:idem-reg}}
    ref: main
    skills:
      - idem-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
		},
	},

	// ===== other commands =====
	{
		Name: "version/flag_prints_version",
		Desc: "Given the e2e-test binary, When --version is run, Then it prints the version string and exits 0",
		Steps: []Step{
			{Args: []string{"--version"}, MustPass: true,
				OutContains: []string{"e2e-test"}},
		},
	},
	{
		Name: "list/shows_available_resources",
		Desc: "Given a locked registry with a skill, When list is run, Then the skill name appears in the output",
		Registries: []RegistryDef{
			{Name: "list-reg", Skills: []Skill{
				{Name: "visible-skill", Description: "A visible skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:list-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list"}, MustPass: true,
				OutContains: []string{"visible-skill"}},
		},
	},
	{
		Name: "list/kind_filter",
		Desc: "Given a locked registry with skills and commands, When list --kind command is run, Then only commands appear (no skills)",
		Registries: []RegistryDef{
			{Name: "kind-list", Skills: []Skill{
				{Name: "listed-skill"},
			}, Commands: []Command{
				{Name: "listed-cmd", Command: "echo hi"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:kind-list}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list", "--kind", "command"}, MustPass: true,
				OutContains:    []string{"listed-cmd"},
				OutNotContains: []string{"listed-skill"}},
		},
	},
	{
		Name: "doctor/reports_harness",
		Desc: "Given a repo with synced factory artifacts, When doctor is run, Then it reports 'factory' in the output",
		Registries: []RegistryDef{
			{Name: "doc-reg", Skills: []Skill{
				{Name: "doc-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:doc-reg}}
    ref: main
    skills:
      - doc-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"doctor"}, MustPass: true,
				OutContains: []string{"factory"}},
		},
	},

	// ===== complex multi-step =====
	{
		Name: "lifecycle/full_workflow",
		Desc: "Given a skill in global config, When sync→status→remove→sync cycle is run, Then the skill appears, is visible in status, and is pruned after removal",
		Registries: []RegistryDef{
			{Name: "lifecycle-reg", Skills: []Skill{
				{Name: "lifecycle-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:lifecycle-reg}}
    ref: main
    skills:
      - lifecycle-skill
`,
		Steps: []Step{
			// Sync materializes
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/skills/lifecycle-skill/SKILL.md"}},
			// Status shows it
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"lifecycle-skill"}},
			// Check shows in sync
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			// Remove
			{Args: []string{"remove", "--scope", "user", "lifecycle-skill"}, MustPass: true},
			// Sync prunes
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/skills/lifecycle-skill"}},
			// Check shows in sync (no drift)
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
		},
	},
	{
		Name: "config_layer/global_repo_local_merge",
		Desc: "Given global config with global-skill, repo config with repo-skill, and local config with local-skill, When sync is run, Then skills materialize at their respective scopes",
		Registries: []RegistryDef{
			{Name: "merge-reg", Skills: []Skill{
				{Name: "global-skill"},
				{Name: "repo-skill"},
				{Name: "local-skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:merge-reg}}
    ref: main
    skills:
      - global-skill
`,
		RepoCfg: `
registries:
  - url: {{REG:merge-reg}}
    ref: main
    skills:
      - repo-skill
`,
		LocalCfg: `
skills:
  include:
    - local-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/global-skill/SKILL.md",
					"/workspace/.factory/skills/repo-skill/SKILL.md",
					"/workspace/.factory/skills/local-skill/SKILL.md",
				}},
		},
	},
	{
		Name: "collision/allowed_registries_blocks_unauthorized",
		Desc: "Given allowed_registries restricting to internal domains, When sync is run against a file:// registry, Then it exits 1 with an allowed_registries warning",
		Registries: []RegistryDef{
			{Name: "blocked-reg", Skills: []Skill{
				{Name: "blocked-skill"},
			}},
		},
		GlobalCfg: `
allowed_registries:
  - internal.company.com
registries:
  - url: {{REG:blocked-reg}}
    ref: main
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"allowed_registries", "blocked"}},
		},
	},

	// ===== collections =====
	{
		Name: "collection/multi_kind_resources_materialize",
		Desc: "Given a registry with a collection referencing skills, commands, and subagents, When sync is run, Then all resource kinds materialize correctly",
		Registries: []RegistryDef{
			{Name: "collection-reg",
				Skills:    []Skill{{Name: "threat-model", Description: "Threat modeling skill"}},
				Commands:  []Command{{Name: "security-scan", Description: "Security scan command", Command: "echo scan"}},
				Subagents: []Subagent{{Name: "reviewer", Description: "Code reviewer agent", Tools: []string{"Read"}}},
				Files: map[string]string{
					"collections/security/COLLECTION.yaml": `name: security
version: "1.0"
description: Security tooling collection
resources:
  - name: threat-model
  - name: security-scan
    kind: command
  - name: reviewer
    kind: subagent
`,
				}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:collection-reg}}
    ref: main
collections:
  include:
    - security
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/threat-model/SKILL.md",
					"/root/.factory/commands/security-scan.md",
					"/root/.factory/droids/reviewer.md",
				}},
			{Args: []string{"status"},
				OutContains: []string{"threat-model", "security-scan", "reviewer", "collection:security"}},
		},
	},
	{
		Name: "collection/cross_kind_same_name_no_collision",
		Desc: "Given a collection with a skill and a command sharing the name 'build', When sync is run, Then both materialize without collision",
		Registries: []RegistryDef{
			{Name: "same-name-reg",
				Skills:   []Skill{{Name: "build", Description: "Build skill"}},
				Commands: []Command{{Name: "build", Description: "Build command", Command: "echo build"}},
				Files: map[string]string{
					"collections/dev/COLLECTION.yaml": `name: dev
version: "1.0"
resources:
  - name: build
  - name: build
    kind: command
`,
				}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:same-name-reg}}
    ref: main
collections:
  include:
    - dev
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/build/SKILL.md",
					"/root/.factory/commands/build.md",
				}},
		},
	},
	{
		Name: "collection/unknown_kind_errors",
		Desc: "Given a collection with an invalid kind value, When sync is run, Then an error mentions the unsupported kind",
		Registries: []RegistryDef{
			{Name: "bad-kind-reg",
				Skills: []Skill{{Name: "good-skill"}},
				Files: map[string]string{
					"collections/bad/COLLECTION.yaml": `name: bad
version: "1.0"
resources:
  - name: good-skill
  - name: broken
    kind: commnad
`,
				}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:bad-kind-reg}}
    ref: main
collections:
  include:
    - bad
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"unsupported collection member kind", "commnad"}},
		},
	},

	// ===== local config registries =====
	{
		Name: "local_registry/lifecycle",
		Desc: "Given a global registry with skill A and a local config with a registries entry for a second registry with skill B, When sync is run, Then both A and B are materialized",
		Registries: []RegistryDef{
			{Name: "first", Skills: []Skill{
				{Name: "alpha", Description: "Alpha skill"},
			}},
			{Name: "second", Skills: []Skill{
				{Name: "beta", Description: "Beta skill"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:first}}
    ref: main
    default: true
`,
		LocalCfg: `
registries:
  - url: {{REG:second}}
    ref: main
    default: true
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/alpha/SKILL.md",
					"/workspace/.factory/skills/beta/SKILL.md",
				}},
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"alpha", "beta"}},
			{Args: []string{"remove", "--scope", "local", "beta"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.factory/skills/beta"},
				FilesExist:  []string{"/root/.factory/skills/alpha/SKILL.md"}},
		},
	},
	{
		Name: "local_registry/per_registry_exclude",
		Desc: "Given a local config with a registries entry that has skills.exclude, When sync is run, Then the excluded skill is not materialized",
		Registries: []RegistryDef{
			{Name: "exclude-reg", Skills: []Skill{
				{Name: "kept", Description: "Kept skill"},
				{Name: "dropped", Description: "Dropped skill"},
			}},
		},
		GlobalCfg: ``,
		RepoCfg: `
registries:
  - url: {{REG:exclude-reg}}
    ref: main
    default: true
`,
		LocalCfg: `
registries:
  - url: {{REG:exclude-reg}}
    ref: main
    skills:
      exclude:
        - dropped
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/workspace/.factory/skills/kept/SKILL.md"},
				FilesAbsent: []string{"/workspace/.factory/skills/dropped"}},
		},
	},

	// ===== codex =====
	{
		Name: "codex/repo_all_kinds_deterministic",
		Desc: "Given repository-scoped resources of every Codex kind, When sync and check run, Then native Codex outputs are complete and deterministic",
		Registries: []RegistryDef{
			{Name: "codex-repo", Skills: []Skill{
				{Name: "repo-skill", Description: "Codex repository skill"},
			}, Commands: []Command{
				{Name: "repo-command", Description: "Codex repository command", Command: "echo repo-command"},
			}, Subagents: []Subagent{
				{Name: "repo-agent", Description: "Codex repository agent"},
			}, MCPServers: []MCPServer{
				{Name: "zeta-server", Command: "zeta-mcp"},
				{Name: "alpha-server", Command: "alpha-mcp"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo repo-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: `
harnesses:
  - codex
registries: []
`,
		RepoCfg: `
registries:
  - url: {{REG:codex-repo}}
    ref: main
    source_harness: claude
    skills:
      - repo-skill
    commands:
      - repo-command
    subagents:
      - repo-agent
    hooks:
      - PreToolUse-command
    mcp:
      - zeta-server
      - alpha-server
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/workspace/.agents/skills/repo-skill/SKILL.md",
					"/workspace/.agents/skills/repo-command/SKILL.md",
					"/workspace/.codex/agents/repo-agent.toml",
					"/workspace/.codex/hooks.json",
					"/workspace/.codex/config.toml",
				},
				FileContains: map[string]string{
					"/workspace/.agents/skills/repo-command/SKILL.md": "x-agent-marketplace-kind: command",
					"/workspace/.codex/agents/repo-agent.toml":        "name = 'repo-agent'",
					"/workspace/.codex/hooks.json":                    "echo repo-hook",
					"/workspace/.codex/config.toml":                   "[mcp_servers.\"zeta-server\"]\ncommand = 'zeta-mcp'\n[mcp_servers.\"alpha-server\"]\ncommand = 'alpha-mcp'",
				}},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0),
				PathsUnchanged: []string{
					"/workspace/.agents",
					"/workspace/.agents/skills/repo-skill/SKILL.md",
					"/workspace/.agents/skills/repo-command/SKILL.md",
					"/workspace/.codex",
					"/root/.local/state/agent-marketplace",
				}},
			{Args: []string{"sync"}, MustPass: true,
				PathsUnchanged: []string{
					"/workspace/.agents",
					"/workspace/.agents/skills/repo-skill/SKILL.md",
					"/workspace/.agents/skills/repo-command/SKILL.md",
					"/workspace/.codex",
				},
				FileContains: map[string]string{
					"/workspace/.codex/config.toml": "[mcp_servers.\"zeta-server\"]\ncommand = 'zeta-mcp'\n[mcp_servers.\"alpha-server\"]\ncommand = 'alpha-mcp'",
				}},
		},
	},
	{
		Name: "codex/repo_shared_file_collisions_fail_closed",
		Desc: "Given handwritten Codex hook and MCP entries, When sync finds a same-key MCP conflict, Then it preserves that value while the distinct hooks coexist",
		Registries: []RegistryDef{
			{Name: "codex-collisions", MCPServers: []MCPServer{
				{Name: "collision-server", Command: "generated-mcp"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo generated-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: `
harnesses:
  - codex
registries: []
`,
		RepoCfg: `
registries:
  - url: {{REG:codex-collisions}}
    ref: main
    source_harness: claude
    hooks:
      - PreToolUse-command
    mcp:
      - collision-server
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.codex/hooks.json",
				Body: `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo handwritten-hook"}]}]}}`},
			{Action: ActionWriteFile, Path: "/workspace/.codex/config.toml",
				Body: "[mcp_servers.\"collision-server\"]\ncommand = 'handwritten-mcp'\n"},
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"ownership.unmanaged-key"},
				FileContains: map[string]string{
					"/workspace/.codex/hooks.json":  "echo generated-hook",
					"/workspace/.codex/config.toml": "command = 'handwritten-mcp'",
				},
				FileNotContains: map[string]string{
					"/workspace/.codex/config.toml": "generated-mcp",
				}},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1),
				OutContains: []string{"ownership.unmanaged-key"},
				FileContains: map[string]string{
					"/workspace/.codex/hooks.json":  "echo handwritten-hook",
					"/workspace/.codex/config.toml": "command = 'handwritten-mcp'",
				},
				FileNotContains: map[string]string{
					"/workspace/.codex/config.toml": "generated-mcp",
				}},
		},
	},
	{
		Name: "codex/machine_lifecycle_multi_harness",
		Desc: "Given machine-scoped Codex resources selected with Factory, When sync, check, prune, restore, and deinit run, Then both Codex root partitions and Factory follow the same lifecycle without check-mode mutation",
		Registries: []RegistryDef{
			{Name: "codex-machine", Skills: []Skill{
				{Name: "machine-skill", Description: "Codex machine skill"},
			}, Subagents: []Subagent{
				{Name: "machine-agent", Description: "Codex machine agent"},
			}, MCPServers: []MCPServer{
				{Name: "machine-mcp", Command: "machine-mcp-bin"},
			}, Files: map[string]string{
				"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"type":"command","command":"echo machine-hook","matcher":"Bash"}]}}`,
			}},
		},
		GlobalCfg: `
harnesses:
  - codex
  - factory
registries:
  - url: {{REG:codex-machine}}
    ref: main
    source_harness: claude
    harnesses:
      - codex
      - factory
    skills:
      - machine-skill
    subagents:
      - machine-agent
    hooks:
      - PreToolUse-command
    mcp:
      - machine-mcp
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.agents/skills/machine-skill/SKILL.md",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.codex/hooks.json",
					"/root/.codex/config.toml",
					"/root/.factory/skills/machine-skill/SKILL.md",
					"/root/.factory/droids/machine-agent.md",
					"/root/.factory/hooks/hooks.json",
					"/root/.factory/mcp.json",
				},
				FileContains: map[string]string{
					"/root/.codex/agents/machine-agent.toml": "name = 'machine-agent'",
					"/root/.codex/hooks.json":                "echo machine-hook",
					"/root/.codex/config.toml":               "machine-mcp-bin",
					"/root/.factory/droids/machine-agent.md": "name: machine-agent",
					"/root/.factory/hooks/hooks.json":        "echo machine-hook",
					"/root/.factory/mcp.json":                "machine-mcp-bin",
				}},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0),
				PathsUnchanged: []string{
					"/root/.agents",
					"/root/.agents/skills/machine-skill/SKILL.md",
					"/root/.codex",
					"/root/.factory",
					"/root/.factory/skills/machine-skill/SKILL.md",
					"/root/.local/state/agent-marketplace",
				}},
			{Action: ActionWriteGlobalConfig, Body: `
harnesses:
  - codex
  - factory
registries: []
`},
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1),
				OutContains: []string{
					"out of sync",
					"machine-agents", "machine-skill",
					"machine-codex", "machine-agent.toml", "hooks.json", "config.toml",
					".factory",
				},
				PathsUnchanged: []string{
					"/root/.agents",
					"/root/.agents/skills/machine-skill/SKILL.md",
					"/root/.codex",
					"/root/.factory",
					"/root/.factory/skills/machine-skill/SKILL.md",
					"/root/.local/state/agent-marketplace",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{
					"/root/.agents/skills/machine-skill",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.factory/skills/machine-skill",
					"/root/.factory/droids/machine-agent.md",
				},
				FileNotContains: map[string]string{
					"/root/.codex/hooks.json":         "echo machine-hook",
					"/root/.codex/config.toml":        "machine-mcp-bin",
					"/root/.factory/hooks/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":         "machine-mcp-bin",
				}},
			{Action: ActionWriteGlobalConfig, Body: `
harnesses:
  - codex
  - factory
registries:
  - url: {{REG:codex-machine}}
    ref: main
    source_harness: claude
    harnesses:
      - codex
      - factory
    skills:
      - machine-skill
    subagents:
      - machine-agent
    hooks:
      - PreToolUse-command
    mcp:
      - machine-mcp
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.agents/skills/machine-skill/SKILL.md",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.codex/hooks.json",
					"/root/.codex/config.toml",
					"/root/.factory/skills/machine-skill/SKILL.md",
					"/root/.factory/droids/machine-agent.md",
					"/root/.factory/hooks/hooks.json",
					"/root/.factory/mcp.json",
				},
				FileContains: map[string]string{
					"/root/.codex/hooks.json":         "echo machine-hook",
					"/root/.codex/config.toml":        "machine-mcp-bin",
					"/root/.factory/hooks/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":         "machine-mcp-bin",
				}},
			{Args: []string{"deinit"}, MustPass: true,
				FilesAbsent: []string{
					"/root/.agents/skills/machine-skill",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.factory/skills/machine-skill",
					"/root/.factory/droids/machine-agent.md",
				},
				FileNotContains: map[string]string{
					"/root/.codex/hooks.json":         "echo machine-hook",
					"/root/.codex/config.toml":        "machine-mcp-bin",
					"/root/.factory/hooks/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":         "machine-mcp-bin",
				}},
		},
	},

	// ===== deinit command =====
	{
		Name:       "deinit/manifest_failure_preserves_config_and_output",
		Desc:       "Given installed output and an invalid manifest, When deinit attempts config purge, Then it reports failure and preserves state until the manifest is repaired",
		Registries: []RegistryDef{{Name: "deinit-invalid-manifest", Skills: []Skill{{Name: "teardown-skill", Description: "Deinit preservation"}}}},
		RepoCfg:    "registries:\n  - url: {{REG:deinit-invalid-manifest}}\n    ref: main\n    skills:\n      - teardown-skill\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: "version: 99\nentries: []\n"},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config", "--dry-run"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Would remove config", "Would remove lock", "No managed artifacts to remove"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.factory"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Removed config", "Removed lock", "No managed artifacts to remove"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.factory"}},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: "version: 1\nentries: []\n", Args: []string{"deinit", "--scope", "repo", "--purge-config"}, MustPass: true, OutContains: []string{"Removed config", "Removed lock"}, FilesAbsent: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.factory/skills/teardown-skill/SKILL.md"}},
		},
	},
	{
		Name: "deinit/repo_scope_removes_repo_artifacts",
		Desc: "Given a synced repo with artifacts, When deinit --scope repo is run, Then repo-scoped artifacts are removed and machine-scope artifacts are untouched",
		Registries: []RegistryDef{
			{Name: "deinit-reg", Skills: []Skill{
				{Name: "teardown-skill", Description: "Skill for deinit test"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:deinit-reg}}
    ref: main
    skills:
      - teardown-skill
`,
		RepoCfg: `
registries:
  - url: {{REG:deinit-reg}}
    ref: main
    skills:
      - teardown-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md"}},
			{Args: []string{"deinit", "--scope", "repo"}, MustPass: true,
				OutContains: []string{"managed artifact"},
				FilesAbsent: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md"}},
		},
	},
	{
		Name: "deinit/user_scope_removes_artifacts",
		Desc: "Given a synced environment with an exact locked cache, When deinit is run, Then machine-scoped artifacts are removed while reachable cache remains managed by GC",
		Registries: []RegistryDef{
			{Name: "deinit-cache-reg", Skills: []Skill{
				{Name: "cached-skill", Description: "Skill to populate cache"},
			}},
		},
		GlobalCfg: `
registries:
  - url: {{REG:deinit-cache-reg}}
    ref: main
    skills:
      - cached-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"deinit"}, MustPass: true,
				OutContains: []string{"managed artifact"},
				FilesAbsent: []string{"/root/.factory/skills/cached-skill/SKILL.md"}},
		},
	},
	{
		Name: "deinit/dry_run_preserves_artifacts",
		Desc: "Given a synced repo with artifacts, When deinit --dry-run is run, Then artifacts remain on disk",
		Registries: []RegistryDef{
			{Name: "deinit-dry-reg", Skills: []Skill{
				{Name: "dry-skill", Description: "Skill for dry-run test"},
			}},
		},
		RepoCfg: `
registries:
  - url: {{REG:deinit-dry-reg}}
    ref: main
    skills:
      - dry-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/skills/dry-skill/SKILL.md"}},
			{Args: []string{"deinit", "--scope", "repo", "--dry-run"}, MustPass: true,
				OutContains: []string{"Would remove"},
				FilesExist:  []string{"/workspace/.factory/skills/dry-skill/SKILL.md"}},
		},
	},
	{
		Name: "deinit/purge_config_removes_repo_config",
		Desc: "Given a synced repo with config and a generated lock, When deinit --scope repo --purge-config is run, Then both files are deleted without transaction coordination files in the repository",
		Registries: []RegistryDef{
			{Name: "deinit-purge-reg", Skills: []Skill{
				{Name: "purge-skill", Description: "Skill for purge test"},
			}},
		},
		RepoCfg: `
registries:
  - url: {{REG:deinit-purge-reg}}
    ref: main
    skills:
      - purge-skill
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, MustPass: true,
				OutContains: []string{"Removed config", "Removed lock"},
				FilesAbsent: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/.config.yaml.dependencytxn.lock", "/workspace/.agents/.lock.yaml.dependencytxn.lock"}},
		},
	},
	// ===== devin =====
	{
		Name: "devin/project_mode_lifecycle_and_aggregate_preservation",
		Desc: "Given all five Devin resource kinds and handwritten shared entries, When sync switches between symlink and file modes and then prunes, Then resources follow the configured mode while shared files retain handwritten content and ownership-safe reconciliation",
		Registries: []RegistryDef{
			{Name: "devin-reg",
				Skills:     []Skill{{Name: "devin-skill", Description: "A Devin skill"}},
				Commands:   []Command{{Name: "devin-command", Description: "A Devin command"}},
				Subagents:  []Subagent{{Name: "devin-agent", Description: "A Devin subagent"}},
				MCPServers: []MCPServer{{Name: "devin-mcp", Command: "devin-mcp-bin"}},
				Files:      map[string]string{"hooks/hooks.json": `{"hooks":{"PostToolUse":[{"type":"command","command":"echo generated-hook"}]}}`},
			},
		},
		// harnesses is required on the global layer and is inherited by the
		// project layer; the registry itself lives in the repo config so its
		// Source is repo scope and materialization targets <repo>/.devin.
		GlobalCfg: `
harnesses:
  - devin
`,
		RepoCfg: `
registries:
  - url: {{REG:devin-reg}}
    ref: main
    skills:
      - devin-skill
    commands:
      - devin-command
    subagents:
      - devin-agent
    hooks:
      - PostToolUse-command
    mcp:
      - devin-mcp
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.devin/hooks.v1.json",
				Body: "{\n  \"PostToolUse\": [{\"type\":\"command\",\"command\":\"echo handwritten-hook\"}]\n}\n"},
			{Action: ActionWriteFile, Path: "/workspace/.devin/mcp_config.json",
				Body: "{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"},
			{Args: []string{"sync"}, MustPass: true,
				Symlinks: []string{
					"/workspace/.devin/skills/devin-skill",
					"/workspace/.devin/skills/devin-command",
					"/workspace/.devin/agents/devin-agent.md",
				},
				RegularFiles: []string{"/workspace/.devin/mcp_config.json"},
				FileContains: map[string]string{
					"/workspace/.devin/skills/devin-command/SKILL.md": "x-agent-marketplace-kind: command",
					"/workspace/.devin/mcp_config.json":               "devin-mcp",
					"/workspace/.devin/hooks.v1.json":                 "echo generated-hook",
				},
				FileNotContains: map[string]string{
					// Devin ownership is proven from the durable state root, never
					// inferred from a marker inside the user's file.
					"/workspace/.devin/mcp_config.json": "_managed_by",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					// The handwritten entry survives reconciliation.
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
				}},
			{Action: ActionWriteLocalConfig, Body: "materialization:\n  mode: file\n",
				Args: []string{"sync"}, MustPass: true,
				RegularFiles:   []string{"/workspace/.devin/skills/devin-skill/SKILL.md", "/workspace/.devin/skills/devin-command/SKILL.md", "/workspace/.devin/agents/devin-agent.md"},
				PathsUnchanged: []string{"/workspace/.devin/mcp_config.json", "/workspace/.devin/hooks.v1.json"}},
			{Args: []string{"sync", "--check"}, MustPass: true,
				PathsUnchanged: []string{"/workspace/.devin"}},
			{Action: ActionWriteLocalConfig, Body: "materialization:\n  mode: symlink\n",
				Args: []string{"sync"}, MustPass: true,
				Symlinks:       []string{"/workspace/.devin/skills/devin-skill", "/workspace/.devin/skills/devin-command", "/workspace/.devin/agents/devin-agent.md"},
				PathsUnchanged: []string{"/workspace/.devin/mcp_config.json", "/workspace/.devin/hooks.v1.json"}},
			{Action: ActionWriteLocalConfig, Body: "materialization:\n  mode: file\n",
				Args: []string{"sync"}, MustPass: true},
			{Action: ActionWriteRepoConfig, Body: "registries: []\n",
				Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.devin/skills/devin-skill", "/workspace/.devin/skills/devin-command", "/workspace/.devin/agents/devin-agent.md"},
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
					"/workspace/.devin/hooks.v1.json":   "echo handwritten-hook",
				},
				FileNotContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "devin-mcp",
					"/workspace/.devin/hooks.v1.json":   "echo generated-hook",
				}},
		},
	},
	{
		// Bead agent-marketplace-pu6b. This is the case that used to take the
		// whole run down: three skills selected by this repo's own committed
		// .agents/config.yaml carry `allowed-tools: Bash(openspec:*)`, which
		// Devin cannot represent (five bare tool names, no per-command scoping).
		// The compiler emitted skill.devin.tool-unknown at ERROR severity and
		// produced no artifact, so sync exited non-zero and installed nothing.
		//
		// It now omits the allowed-tools key, warns, and installs. The exit code
		// is the assertion that matters most: a non-zero sync also gates off
		// --prune-stale, so the old behaviour broke unrelated cleanup too.
		Name: "devin/openspec_scoped_bash_allowed_tools_still_installs",
		Desc: "Given a Devin harness with an openspec-style skill whose allowed-tools is the scoped specifier Bash(openspec:*) and a subagent whose tools name a tool with no Devin equivalent, When sync runs, Then it exits ZERO, both resources are installed, the compiled subagent profile carries no allowed-tools key, and no diagnostic quotes the offending specifier",
		Registries: []RegistryDef{
			{Name: "devin-scoped-reg",
				Subagents: []Subagent{{Name: "scoped-agent", Description: "A Devin subagent", Model: "sonnet", Tools: []string{"Read", "WebSearch"}}},
				Files: map[string]string{
					// Written raw rather than through the Skill builder because
					// the scoped specifier IS the fixture.
					"skills/openspec-apply-change/SKILL.md": "---\nname: openspec-apply-change\ndescription: Apply an openspec change\nallowed-tools: Bash(openspec:*)\n---\n\n# openspec-apply-change\n\nRun openspec.\n",
				},
			},
		},
		GlobalCfg: `
harnesses:
  - devin
`,
		RepoCfg: `
registries:
  - url: {{REG:devin-scoped-reg}}
    ref: main
    skills:
      - openspec-apply-change
    subagents:
      - scoped-agent
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				// MustPass is the acceptance criterion: exit ZERO, not a
				// suppressed resource and a non-zero run.
				Symlinks: []string{
					"/workspace/.devin/skills/openspec-apply-change",
					"/workspace/.devin/agents/scoped-agent.md",
				},
				FileContains: map[string]string{
					"/workspace/.devin/agents/scoped-agent.md": "name: scoped-agent",
				},
				FileNotContains: map[string]string{
					// The compiled profile inherits Devin's default tool set
					// rather than carrying a restriction Devin cannot express.
					"/workspace/.devin/agents/scoped-agent.md": "allowed-tools",
				},
				// A silent widening would be worse than the original error, so
				// the loss must be REPORTED and ATTRIBUTED: both codes, and the
				// name of each resource that lost its restriction. The message
				// itself is generic by design, so without the resource name the
				// warning is unactionable (adversarial review of the bead).
				OutContains: []string{
					"warning: skill.devin.tool-unknown",
					"openspec-apply-change",
					"warning: subagent.devin.tool-unknown",
					"scoped-agent",
				},
				// The reported loss never echoes the specifier: allowed-tools
				// entries carry command strings.
				OutNotContains: []string{"Bash(openspec", "WebSearch"}},
		},
	},
	{
		Name: "devin/machine_file_mode_routes_to_user_root",
		Desc: "Given file mode and a Devin registry in the global config, When sync runs, Then skills, commands, subagents and the MCP aggregate are regular outputs under ~/.config/devin and nothing is written into the repo's .devin",
		Registries: []RegistryDef{
			{Name: "devin-machine-reg",
				Skills:     []Skill{{Name: "machine-skill", Description: "A machine-scope Devin skill"}},
				Commands:   []Command{{Name: "machine-command", Description: "A machine-scope Devin command"}},
				Subagents:  []Subagent{{Name: "machine-agent", Description: "A machine-scope Devin subagent"}},
				MCPServers: []MCPServer{{Name: "machine-mcp", Command: "machine-mcp-bin"}},
			},
		},
		// A registry declared in ~/.agents/config.yaml carries a machine-scope
		// Source, which routes materialization to the target's machine root.
		GlobalCfg: `
harnesses:
  - devin
materialization:
  mode: file
registries:
  - url: {{REG:devin-machine-reg}}
    ref: main
    skills:
      - machine-skill
    commands:
      - machine-command
    subagents:
      - machine-agent
    mcp:
      - machine-mcp
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				RegularFiles: []string{
					"/root/.config/devin/skills/machine-skill/SKILL.md",
					"/root/.config/devin/skills/machine-command/SKILL.md",
					"/root/.config/devin/agents/machine-agent.md",
					"/root/.config/devin/mcp_config.json",
				},
				FileContains: map[string]string{
					"/root/.config/devin/skills/machine-command/SKILL.md": "x-agent-marketplace-kind: command",
					"/root/.config/devin/mcp_config.json":                 "machine-mcp",
				},
				// Machine scope never leaks into the repo root.
				FilesAbsent: []string{"/workspace/.devin"}},
		},
	},
	{
		Name: "devin/doctor_reports_target_and_repair_is_preview_only_off_tty",
		Desc: "Given a repo-scope Devin harness synced over a handwritten .devin/mcp_config.json, When doctor runs, Then it reports Devin's canonical roots and bidirectional capabilities; When doctor --repair runs off a terminal, Then it previews only, exits 0, and leaves the reconciled repo aggregate (generated entry plus handwritten entry) as found; And doctor --yes without --repair is rejected",
		Registries: []RegistryDef{
			{Name: "devin-doctor-reg",
				MCPServers: []MCPServer{{Name: "devin-doctor-mcp", Command: "devin-doctor-bin"}},
			},
		},
		// harnesses is required on the global layer (and WriteGlobalConfig would
		// otherwise inject `factory`), but the registry itself must live in the
		// repo config: a machine-scope Source routes materialization to
		// ~/.config/devin, which would leave the handwritten
		// /workspace/.devin/mcp_config.json below untouched and every assertion
		// on it vacuous.
		GlobalCfg: `
harnesses:
  - devin
`,
		RepoCfg: `
registries:
  - url: {{REG:devin-doctor-reg}}
    ref: main
    mcp:
      - devin-doctor-mcp
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.devin/mcp_config.json",
				Body: "{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"},
			{Args: []string{"sync"}, MustPass: true,
				// Proof that this is the file under reconciliation: the run read
				// it, merged the generated entry in, and rewrote it in place.
				RegularFiles: []string{"/workspace/.devin/mcp_config.json"},
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "devin-doctor-mcp",
				}},
			{Args: []string{"doctor"}, MustPass: true,
				OutContains: []string{
					"devin repo=",
					"capabilities=compile,ingest,convert,tui",
					".config/devin",
					"devin harness: selected (global)",
					// No Devin binary is resolvable from the sandbox PATH (the
					// framework shadows it out, so this holds even on a host
					// with Devin installed), and that never blocks anything:
					// "Sync does not require a Devin binary."
					"devin cli: not installed (sync does not require it)",
				}},
			{Args: []string{"doctor", "--repair"}, MustPass: true,
				OutContains: []string{"repair: devin shared-aggregate ownership"},
				// A freshly synced root is clean: every generated entry is
				// proven owned, so there is nothing to adopt, and nothing is
				// undesired, so there is nothing to nominate.
				OutNotContains: []string{"adopt ", "orphan candidate "},
				// Preview-only off a TTY: the reconciled repo aggregate still
				// holds both the handwritten entry and the generated one.
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
				},
				FileNotContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "_managed_by",
				}},
			// FileContains is keyed by path, so the generated entry needs its own
			// step; a second preview-only repair also proves repair is idempotent.
			{Args: []string{"doctor", "--repair"}, MustPass: true,
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "devin-doctor-mcp",
				}},
			{Args: []string{"doctor", "--yes"}, AssertExitCode: intPtr(1),
				OutContains: []string{"--yes only applies to --repair"}},
		},
	},
	{
		Name: "devin/mcp_key_conflict_is_previewed_as_replaceable",
		Desc: "Given a hand-written .devin/mcp_config.json entry whose key equals a selected MCP server's key but whose value differs, When sync runs, Then it reports the collision, preserves the hand-written entry and installs nothing; When doctor --repair runs off a terminal, Then it previews the entry as a replaceable conflict with both value digests and neither value, exits 0, and mutates nothing; And doctor --repair --yes still mutates nothing because replacing destroys user-written content",
		Registries: []RegistryDef{
			{Name: "devin-conflict-reg",
				MCPServers: []MCPServer{{Name: "genai-atlassian", Command: "generated-atlassian-bin"}},
			},
		},
		// The registry must be repo-scope so materialization targets
		// /workspace/.devin, which is where the hand-written aggregate below
		// lives; harnesses is still required on the global layer.
		GlobalCfg: `
harnesses:
  - devin
`,
		RepoCfg: `
registries:
  - url: {{REG:devin-conflict-reg}}
    ref: main
    mcp:
      - genai-atlassian
`,
		Steps: []Step{
			// The tester's exact starting state: same KEY, different VALUE, and
			// no ownership state anywhere because nothing ever wrote one.
			{Action: ActionWriteFile, Path: "/workspace/.devin/mcp_config.json",
				Body: "{\n  \"mcpServers\": {\n    \"genai-atlassian\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"},
			// Collide: the hand-written entry is preserved and only the
			// generated entry fails. This is the loop the user was stuck in.
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{
					// The code is the stable handle; its catalogued hint in
					// diag/codes.md now names both repair paths (adopt when it
					// matches exactly, replace when the value differs).
					"ownership.unmanaged-key",
					"already exists and is not proven owned",
				},
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
				},
				FileNotContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "generated-atlassian-bin",
				}},
			// Repair PREVIEWS the third class instead of answering "nothing to
			// repair": the offer, the two digests, and the reason it needs a
			// terminal.
			{Args: []string{"doctor", "--repair"}, MustPass: true,
				OutContains: []string{
					"repair: devin shared-aggregate ownership",
					"replace mcp-server \"genai-atlassian\" (resource \"genai-atlassian\"): the existing entry has the same key but a different value",
					"existing value digest sha256:",
					"generated value digest sha256:",
					"doctor.repair.conflict-replaceable",
					"replacing DELETES the existing value; it needs an interactive per-entry confirmation and is never done by --yes",
					"preview only: not a terminal",
				},
				OutNotContains: []string{
					// Neither value is ever printed, and this is not adoption.
					"user-owned-binary", "generated-atlassian-bin",
					"nothing to repair", "adopt mcp-server",
				},
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
				}},
			// --yes covers adoption only. It reports the conflict and leaves the
			// user's value exactly where it was.
			{Args: []string{"doctor", "--repair", "--yes"}, MustPass: true,
				OutContains: []string{
					"left unchanged: replacing it deletes user-written content and requires an interactive per-entry confirmation",
				},
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "user-owned-binary",
				},
				FileNotContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "generated-atlassian-bin",
				}},
		},
	},
	// ===== init =====
	{
		Name: "init/is_idempotent",
		Desc: "Given an existing user personal config, When init is run again, Then it exits 0 and does not modify the file",
		GlobalCfg: `
harnesses:
  - factory
`,
		Steps: []Step{
			{Args: []string{"init", "--harnesses", "claude"}, MustPass: true,
				OutContains: []string{"already initialized"}},
			{Args: []string{"init", "--harnesses", "claude"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.agents/config.yaml": "- factory",
				},
				FileNotContains: map[string]string{
					"/root/.agents/config.yaml": "- claude",
				}},
		},
	},
	// ===== telemetry =====
	{
		Name: "telemetry/commands",
		Desc: "Given a minimal user personal config, When the telemetry control commands are exercised, Then status, enable, preview, disable, and purge behave as expected",
		GlobalCfg: `
harnesses:
  - factory
`,
		Steps: []Step{
			{Args: []string{"telemetry", "status"}, MustPass: true,
				OutContains: []string{"Telemetry status", "Effective level:"}},
			{Args: []string{"telemetry", "enable", "--level", "basic"}, MustPass: true,
				OutContains: []string{"Telemetry enabled", "basic"}},
			{Args: []string{"telemetry", "status"}, MustPass: true,
				OutContains: []string{"Effective level:", "basic", "(user)"}},
			{Args: []string{"telemetry", "preview", "--level", "basic"}, MustPass: true,
				OutContains: []string{"Preview payload written to", "level:", "basic"}},
			{Args: []string{"telemetry", "disable"}, MustPass: true,
				OutContains: []string{"Telemetry disabled"}},
			{Args: []string{"telemetry", "status"}, MustPass: true,
				OutContains:    []string{"Effective level:", "off"},
				OutNotContains: []string{"basic"}},
			{Args: []string{"telemetry", "purge"}, MustPass: true},
		},
	},
	{
		Name: "ingest/bare_bundle_mcp",
		Desc: "Given a bare Claude bundle with skills, a command, and undotted MCP configuration, when list and sync run, then MCP is discovered and materialized without changing the native definition.",
		Registries: []RegistryDef{{Name: "bare-bundle-mcp", Skills: []Skill{{Name: "demo"}}, Commands: []Command{{Name: "hi"}}, Files: map[string]string{
			"mcp.json": (MCPServer{Name: "probe", Command: "npx", Args: []string{"-y", "x"}}).MCPFile(),
		}}},
		RepoCfg: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: {{REG:bare-bundle-mcp}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list", "--kind", "skill"}, MustPass: true, OutContains: []string{"demo"}},
			{Args: []string{"list", "--kind", "command"}, MustPass: true, OutContains: []string{"hi"}},
			{Args: []string{"list", "--kind", "mcp"}, MustPass: true, OutContains: []string{"probe"}},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/mcp.json": "\"probe\": {"}},
			{FileContains: map[string]string{"/workspace/.factory/mcp.json": "\"command\": \"npx\""}},
			{FileContains: map[string]string{"/workspace/.factory/mcp.json": "\"-y\""}},
			{FileContains: map[string]string{"/workspace/.factory/mcp.json": "\"x\""}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"probe"}},
		},
	},
	{
		Name: "ingest/unconsumed_container",
		Desc: "Given a diagnostic-only Factory container with a nested skill, when lock, list, sync, and status run, then the child resource is discovered while the container audit remains visible.",
		Registries: []RegistryDef{{Name: "unconsumed-container", Files: map[string]string{
			"group/.factory-plugin/plugin.json": `{"name":"group"}`,
			"group/.mcp.json":                   `{"mcpServers":{"foreign":{"command":"npx"}}}`,
			"group/child/skills/demo/SKILL.md":  "---\nname: demo\ndescription: Demo\n---\nDemo\n",
		}}},
		RepoCfg: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: {{REG:unconsumed-container}}\n    ref: main\n    default: true\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list", "--kind", "skill"}, MustPass: true, OutContains: []string{"demo", "ingest.factory.unconsumed-config"}},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/demo/SKILL.md": "Demo"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"demo", "ingest.factory.unconsumed-config"}},
		},
	},
	{
		Name: "ingest/unconsumed_config",
		Desc: "Given an explicit Claude plugin with native MCP and foreign configurations, when list, sync, and status run, then the foreign files are warned about while the selected native MCP remains materializable.",
		Registries: []RegistryDef{{Name: "unconsumed-config", Skills: []Skill{{Name: "demo"}}, Files: map[string]string{
			".claude-plugin/plugin.json": `{"name":"unconsumed-config"}`,
			".mcp.json":                  (MCPServer{Name: "native", Command: "npx", Args: []string{"-y", "native"}}).MCPFile(),
			"mcp.json":                   `{"mcpServers":{"foreign":{"command":"npx"}}}`,
			"gemini-extension.json":      `{"name":"gemini","mcpServers":{"foreign-gemini":{"command":"npx"}}}`,
		}}},
		RepoCfg: "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: {{REG:unconsumed-config}}\n    ref: main\n    source_harness: claude\n    default: true\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{"ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"}},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/mcp.json": "native"}, FileNotContains: map[string]string{"/workspace/.factory/mcp.json": "foreign"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"}},
		},
	},
}

// ---------- parallelism configuration ----------

// e2eParallelism returns the maximum number of concurrent test containers.
// Controlled by E2E_PARALLEL env var; defaults to 4. Set to 1 for serial.
func e2eParallelism() int {
	if v := os.Getenv("E2E_PARALLEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	return 4
}

// ---------- helper ----------

func intPtr(v int) *int { return &v }

// ---------- test report ----------

// TestResult captures the outcome of a single test case.
type TestResult struct {
	Name       string `json:"name"`
	Desc       string `json:"description"`
	Status     string `json:"status"` // "pass", "fail", "skip"
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// TestReport captures the full E2E suite outcome.
type TestReport struct {
	Timestamp  time.Time    `json:"timestamp"`
	Total      int          `json:"total"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped"`
	DurationMs int64        `json:"duration_ms"`
	Results    []TestResult `json:"results"`
}

// reportWriter is a goroutine-safe collector for test results.
type reportWriter struct {
	mu      sync.Mutex
	results []TestResult
}

func (rw *reportWriter) add(r TestResult) {
	rw.mu.Lock()
	rw.results = append(rw.results, r)
	rw.mu.Unlock()
}

func (rw *reportWriter) write(reportPath string, suiteStart time.Time) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	report := TestReport{
		Timestamp:  time.Now().UTC(),
		Total:      len(rw.results),
		DurationMs: time.Since(suiteStart).Milliseconds(),
		Results:    rw.results,
	}
	for _, r := range rw.results {
		switch r.Status {
		case "pass":
			report.Passed++
		case "fail":
			report.Failed++
		case "skip":
			report.Skipped++
		}
	}

	// Write JSON report.
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e report: marshal: %v\n", err)
		return
	}
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "e2e report: write: %v\n", err)
	}

	// Print summary to stdout.
	fmt.Println("\n========== E2E Test Report ==========")
	fmt.Printf("  Total:   %d\n", report.Total)
	fmt.Printf("  Passed:  %d\n", report.Passed)
	fmt.Printf("  Failed:  %d\n", report.Failed)
	fmt.Printf("  Skipped: %d\n", report.Skipped)
	fmt.Printf("  Duration: %s\n", time.Duration(report.DurationMs*int64(time.Millisecond)).Round(time.Second))
	fmt.Println("======================================")
	if report.Failed > 0 {
		fmt.Println("\nFailed tests:")
		for _, r := range report.Results {
			if r.Status == "fail" {
				fmt.Printf("  - %s (%dms)\n", r.Name, r.DurationMs)
				if r.Desc != "" {
					fmt.Printf("    %s\n", r.Desc)
				}
				if r.Error != "" {
					firstLine := strings.SplitN(r.Error, "\n", 2)[0]
					fmt.Printf("    %s\n", firstLine)
				}
			}
		}
	}
	fmt.Printf("\nFull report: %s\n", reportPath)
}

// ---------- table-driven test runner ----------

// TestE2E runs the full declarative test table in isolated temp directories.
// By default up to 4 tests run in parallel (each in its own sandbox);
// set E2E_PARALLEL=1 for serial execution, or any positive integer for
// a different concurrency limit.
func TestE2E(t *testing.T) {

	reportPath := os.Getenv("E2E_REPORT")
	if reportPath == "" {
		reportPath = "e2e-report.json"
	}

	parallelism := e2eParallelism()
	// Token-pool semaphore: a buffered channel pre-filled with `parallelism`
	// tokens. Each parallel test receives a token to acquire and sends it
	// back to release. This avoids the deadlock that occurs when t.Parallel()
	// resumes all subtests at once and they race to send into an empty
	// channel — with a token pool, receiving blocks (correctly) until a
	// token is available, and the first `parallelism` tests proceed
	// immediately.
	sem := make(chan struct{}, parallelism)
	for i := 0; i < parallelism; i++ {
		sem <- struct{}{}
	}

	suiteStart := time.Now()
	rw := &reportWriter{}

	// Ensure report is written even if tests are interrupted.
	t.Cleanup(func() {
		rw.write(reportPath, suiteStart)
	})

	t.Logf("parallelism: %d (set E2E_PARALLEL to change)", parallelism)

	for _, tc := range testTable {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			// Acquire a token — blocks if `parallelism` containers are
			// already running. Released via defer after test body completes.
			<-sem
			defer func() { sem <- struct{}{} }()

			start := time.Now()

			// Record result for report via defer so fatal failures are still captured.
			var firstErr string
			defer func() {
				status := "pass"
				if t.Failed() {
					status = "fail"
				}
				rw.add(TestResult{
					Name:       tc.Name,
					Desc:       tc.Desc,
					Status:     status,
					DurationMs: time.Since(start).Milliseconds(),
					Error:      firstErr,
				})
			}()

			env := newEnv(t)
			env.InitGitRepo()

			// Seed registries and collect their file:// URLs.
			urls := make(map[string]string)
			for _, reg := range tc.Registries {
				urls[reg.Name] = env.CreateBareRepo(reg.Name, reg.FilesMap())
			}

			// Write initial config layers (placeholders resolved).
			// Global config is always written — even an empty value triggers
			// auto-injection of apiVersion, harnesses, and allowed_registries.
			env.WriteGlobalConfig(resolvePlaceholders(tc.GlobalCfg, urls))
			if tc.RepoCfg != "" {
				env.WriteRepoConfig(resolvePlaceholders(tc.RepoCfg, urls))
			}
			if tc.LocalCfg != "" {
				env.WriteLocalConfig(resolvePlaceholders(tc.LocalCfg, urls))
			}
			if tc.OrgCfg != "" {
				env.WriteOrgConfig(resolvePlaceholders(tc.OrgCfg, urls))
			}

			// Execute steps sequentially; collect first error for the report.
			for i, step := range tc.Steps {
				// Perform side-effect action first.
				switch step.Action {
				case ActionWriteGlobalConfig:
					env.WriteGlobalConfig(resolvePlaceholders(step.Body, urls))
				case ActionWriteRepoConfig:
					env.WriteRepoConfig(resolvePlaceholders(step.Body, urls))
				case ActionWriteLocalConfig:
					env.WriteLocalConfig(resolvePlaceholders(step.Body, urls))
				case ActionWriteOrgConfig:
					env.WriteOrgConfig(resolvePlaceholders(step.Body, urls))
				case ActionWriteFile:
					env.WriteFile(step.Path, step.Body)
				}

				unchanged := make(map[string]string, len(step.PathsUnchanged))
				for _, p := range step.PathsUnchanged {
					fingerprint, err := pathFingerprint(env.mustResolvePath(p))
					if err != nil {
						t.Fatalf("step %d: snapshot path %q: %v", i, p, err)
					}
					unchanged[p] = fingerprint
				}

				// Run command if Args is set (resolve {{REG:...}} placeholders).
				var r ExecResult
				if len(step.Args) > 0 {
					args := make([]string, len(step.Args))
					for j, a := range step.Args {
						args[j] = resolvePlaceholders(a, urls)
					}
					r = env.RunAM(args...)
					if step.MustPass && r.ExitCode != 0 {
						errMsg := fmt.Sprintf("step %d: agent-marketplace %s failed (exit %d): %s",
							i, strings.Join(step.Args, " "), r.ExitCode, r.Combined)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Fatalf("%s", errMsg)
					}
					if step.AssertExitCode != nil && r.ExitCode != *step.AssertExitCode {
						errMsg := fmt.Sprintf("step %d: exit code got %d want %d",
							i, r.ExitCode, *step.AssertExitCode)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s\noutput:\n%s", errMsg, r.Combined)
					}

					// Output assertions.
					for _, s := range step.OutContains {
						if !strings.Contains(r.Combined, s) {
							errMsg := fmt.Sprintf("step %d: output missing %q", i, s)
							if firstErr == "" {
								firstErr = errMsg
							}
							t.Errorf("%s\nfull output:\n%s", errMsg, r.Combined)
						}
					}
					for _, s := range step.OutNotContains {
						if strings.Contains(r.Combined, s) {
							errMsg := fmt.Sprintf("step %d: output unexpectedly contains %q", i, s)
							if firstErr == "" {
								firstErr = errMsg
							}
							t.Errorf("%s\nfull output:\n%s", errMsg, r.Combined)
						}
					}
				}

				// Filesystem assertions.
				for _, p := range step.FilesExist {
					if !env.CheckFileExists(p) {
						errMsg := fmt.Sprintf("step %d: file %q does not exist", i, p)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for _, p := range step.FilesAbsent {
					if env.CheckFileExists(p) {
						errMsg := fmt.Sprintf("step %d: file %q unexpectedly exists", i, p)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for p, want := range unchanged {
					got, err := pathFingerprint(env.mustResolvePath(p))
					if err != nil {
						errMsg := fmt.Sprintf("step %d: unchanged path %q is not readable: %v", i, p, err)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
						continue
					}
					if got != want {
						errMsg := fmt.Sprintf("step %d: path %q changed", i, p)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for _, p := range step.Symlinks {
					if !env.CheckSymlink(p) {
						errMsg := fmt.Sprintf("step %d: path %q is not a symlink", i, p)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for _, p := range step.RegularFiles {
					if !env.CheckRegularFile(p) {
						errMsg := fmt.Sprintf("step %d: path %q is not a regular file", i, p)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for p, substr := range step.FileContains {
					if !env.CheckFileContains(p, substr) {
						errMsg := fmt.Sprintf("step %d: file %q does not contain %q", i, p, substr)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for p, substr := range step.FileNotContains {
					if env.CheckFileContains(p, substr) {
						errMsg := fmt.Sprintf("step %d: file %q unexpectedly contains %q", i, p, substr)
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
			}
		})
	}
}
