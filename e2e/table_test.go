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
	GlobalCfg  string        // ~/.agents/config.yaml content ({{REG:name}} placeholders resolved); always written (empty → explicit v4 fixture defaults)
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
	GlobsExist      []string          // container path glob patterns that must each match at least one path
	GlobsAbsent     []string          // container path glob patterns that must match nothing
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

const handwrittenFactoryHooks = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo handwritten"}]}]}}`

var testTable = []TestCase{
	// spec: resource-removal/disambiguate-collections-before-removal
	{
		Name:      "selection/remove_collection_stale_kind_ambiguity",
		Desc:      "Given a selected collection and a missing skill with the same name, When removing without a kind, Then config and active output stay unchanged until kind disambiguation",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {directory: ./resources, source_harness: factory}}\nresources: {team: [collection/team, skill/team, skill/keep]}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/member/SKILL.md", Body: (Skill{Name: "member", Description: "Collection member"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/keep/SKILL.md", Body: (Skill{Name: "keep", Description: "Keep selected"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/resources/collections/team/COLLECTION.yaml", Body: "name: team\nresources:\n  - name: member\n    kind: skill\n"},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/member/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"}},
			{Args: []string{"remove", "team"}, AssertExitCode: intPtr(1), OutContains: []string{"ambiguous across kinds", "--kind"}, PathsUnchanged: []string{"/workspace/.agents", "/workspace/.factory"}},
			{Args: []string{"remove", "team", "--from", "./resources"}, AssertExitCode: intPtr(1), OutContains: []string{"ambiguous across kinds", "--kind"}, PathsUnchanged: []string{"/workspace/.agents", "/workspace/.factory"}},
			{Args: []string{"remove", "team", "--from", "./resources", "--kind", "skill"}, MustPass: true, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "skill/team"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "collection/team"}, PathsUnchanged: []string{"/workspace/.factory/skills/member", "/workspace/.factory/skills/keep"}},
			{Args: []string{"remove", "team"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/member"}, PathsUnchanged: []string{"/workspace/.factory/skills/keep"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: resource-removal/remove-a-selection-after-its-resource-disappears
	{
		Name:       "selection/remove_undiscoverable_git_selection",
		Desc:       "Given a stale explicit Git selection and a healthy sibling, When running the removal suggested by sync, Then the stale warning disappears and the sibling remains selected",
		Registries: []RegistryDef{{Name: "missing-selection", Skills: []Skill{{Name: "keep", Description: "Keep selected"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:missing-selection}}', ref: main}}\nresources: {team: [skill/gone, skill/keep]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{`skill "gone" was selected but not found`}, FilesExist: []string{"/workspace/.factory/skills/keep/SKILL.md"}},
			{Args: []string{"remove", "gone"}, MustPass: true, OutContains: []string{"Removed gone"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "skill/gone"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "skill/keep"}},
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{`"gone"`}, FilesExist: []string{"/workspace/.factory/skills/keep/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: resource-removal/remove-a-malformed-explicit-selection
	{
		Name:      "selection/remove_malformed_command",
		Desc:      "Given a selected command that becomes malformed, When removing it by source without a kind, Then its command selection is cleared and its source bytes remain untouched",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {directory: ./resources, source_harness: factory}}\nresources: {team: [command/gone, skill/keep]}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/commands/gone.md", Body: (Command{Name: "gone", Description: "Will become malformed"}).CommandFile()},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/keep/SKILL.md", Body: (Skill{Name: "keep", Description: "Keep selected"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/commands/gone.md"}},
			{Action: ActionWriteFile, Path: "/workspace/resources/commands/gone.md", Body: "---\ndescription: [invalid\n---\n"},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{`command "gone" was selected and found`, "failed to load"}},
			{Args: []string{"remove", "gone", "--from", "./resources"}, MustPass: true, PathsUnchanged: []string{"/workspace/resources"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "command/gone"}},
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{`command "gone" was selected`}, FilesExist: []string{"/workspace/.factory/skills/keep/SKILL.md"}},
		},
	},
	// spec: deployed-acquisition-locks/check-reports-every-selected-unrecorded-registration-scope
	{
		Name:      "lock/check_all_unrecorded_registration_scopes",
		Desc:      "Given unrecorded Marketplace registrations in every writable scope, When checking locks, Then every selected scope is reported without changing configuration or creating locks",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {hosted: {marketplace: https://unrecorded.invalid}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {hosted: {marketplace: https://unrecorded.invalid}}\n",
		LocalCfg:  "apiVersion: agent-marketplace/v4\nsources: {hosted: {marketplace: https://unrecorded.invalid}}\n",
		Steps: []Step{
			{
				Args:           []string{"lock", "--check"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{`lock.change-required: user lock "`, `lock.change-required: repo lock "`, `lock.change-required: local lock "`, "https://unrecorded.invalid"},
				PathsUnchanged: []string{"/root/.agents", "/workspace/.agents"},
				FilesAbsent:    []string{"/root/.agents/lock.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/lock.local.yaml"},
			},
			{
				Args:           []string{"lock", "--check", "--scope", "repo"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{`lock.change-required: repo lock "`},
				OutNotContains: []string{`lock.change-required: user lock "`, `lock.change-required: local lock "`},
				PathsUnchanged: []string{"/root/.agents", "/workspace/.agents"},
				FilesAbsent:    []string{"/root/.agents/lock.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/lock.local.yaml"},
			},
		},
	},
	// spec: regression-gates/v4-selection-workflows-remain-usable
	{
		Name: "selection/v4_bulk_selection_lifecycle",
		Desc: "Given a populated v4 selection, When installing all resources, Then removal persists and unrelated selected output remains",
		Registries: []RegistryDef{{Name: "migrated-bulk", Skills: []Skill{
			{Name: "review", Description: "Review fixture"},
			{Name: "deploy", Description: "Deploy fixture"},
		}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:migrated-bulk}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}},
			{Args: []string{ "install", "--scope", "repo",  "--all"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/review/SKILL.md": "Review fixture", "/workspace/.factory/skills/deploy/SKILL.md": "Deploy fixture"}},
			{Args: []string{"remove", "deploy"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: regression-gates/v4-selection-workflows-remain-usable
	{
		Name: "selection/v4_tag_selection_lifecycle",
		Desc: "Given a populated v4 selection, When installing by tag, Then only matching additional resources install and removal persists",
		Registries: []RegistryDef{{Name: "migrated-tag", Skills: []Skill{
			{Name: "review", Description: "Review fixture"},
			{Name: "deploy", Description: "Deploy fixture", Tags: []string{"ops"}},
			{Name: "unselected", Description: "Unselected fixture"},
		}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:migrated-tag}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/deploy", "/workspace/.factory/skills/unselected"}},
			{Args: []string{ "install", "--scope", "repo",  "--tag", "ops"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/review/SKILL.md": "Review fixture", "/workspace/.factory/skills/deploy/SKILL.md": "Deploy fixture"}, FilesAbsent: []string{"/workspace/.factory/skills/unselected"}},
			{Args: []string{"remove", "deploy"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy", "/workspace/.factory/skills/unselected"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: regression-gates/v4-selection-workflows-remain-usable
	{
		Name: "selection/v4_ad_hoc_selection_lifecycle",
		Desc: "Given a populated v4 selection, When installing from another registry, Then coordinate-based removal preserves the original resource",
		Registries: []RegistryDef{
			{Name: "migrated-original", Skills: []Skill{{Name: "review", Description: "Review fixture"}}},
			{Name: "migrated-ad-hoc", Skills: []Skill{{Name: "deploy", Description: "Deploy fixture"}}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:migrated-original}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}},
			{Args: []string{"install", "deploy", "--from", "{{REG:migrated-ad-hoc}}", "--ref", "main", "--scope", "repo"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/skills/review/SKILL.md": "Review fixture", "/workspace/.factory/skills/deploy/SKILL.md": "Deploy fixture"}},
			{Args: []string{"remove", "deploy", "--from", "{{REG:migrated-ad-hoc}}", "--ref", "main", "--scope", "repo"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "install/missing_repo_config_is_v4",
		Desc:       "Given a v4 user registry and no repo config, When installing a skill to repo scope, Then the newly created repo root uses v4 and selects the skill",
		Registries: []RegistryDef{{Name: "new-repo-config", Skills: []Skill{{Name: "new-skill"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {shared: {git: '{{REG:new-repo-config}}', ref: main}}\n",
		Steps: []Step{
			{Args: []string{"install", "new-skill", "--scope", "repo"}, MustPass: true,
				FileContains:    map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v4", "/workspace/.factory/skills/new-skill/SKILL.md": "name: new-skill"},
				FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v2"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "skill/new-skill"}},
		},
	},
	{
		Name:      "format/startup_refuses_unsupported_repo_layers",
		Desc:      "Given a v4 user root and an unsupported repository root, When listing, Then startup refuses without conversion or edits",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v2\nsources: {}\n",
		LocalCfg:  "apiVersion: agent-marketplace/v4\n",
		Steps: []Step{
			{Args: []string{"list"}, AssertExitCode: intPtr(1), OutContains: []string{"v4.api-version", "apiVersion"}, OutNotContains: []string{"Migrate these configuration files"}, PathsUnchanged: []string{"/workspace/.agents"}},
		},
	},
	{
		Name:      "local_registry/v4_portable_directory",
		Desc:      "Given repository-local sources, When syncing v4 dot and nested directory sources, Then live skills materialize without absolute config paths or Git locks",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./resources/skills}}\nresources: {local: {all: true}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/local-skill/SKILL.md", Body: (Skill{Name: "local-skill", Description: "Live local skill"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/local-skill"}, FilesAbsent: []string{"/workspace/.agents/lock.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Action: ActionWriteFile, Path: "/workspace/skills/root-skill/SKILL.md", Body: (Skill{Name: "root-skill", Description: "Repository root skill"}).SkillFile()},
			{Action: ActionWriteRepoConfig, Body: "apiVersion: agent-marketplace/v4\nsources: {registry: {directory: ., source_harness: claude}}\nresources: {registry: {include: [skill/root-skill]}}\n", Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/root-skill"}},
		},
	},
	{
		Name:      "security/cli_warning_controls",
		Desc:      "Given a selected missing resource with a control-bearing v4 directory source identity, When syncing and listing, Then warning display is escaped and selection remains unchanged",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {directory: \"./warning-fixture/REF\\u001b[31m\\u202e\"}}\nresources: {team: {include: [skill/missing]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/warning-fixture/REF\x1b[31m\u202e/skills/control/SKILL.md", Body: "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"warning:", `REF\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{`REF\x1b[31m\u202e`, "control"}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
		},
	},
	{
		Name:      "security/telemetry_failure_controls",
		Desc:      "Given a persisted delivery failure containing terminal controls, When displaying telemetry status, Then output is escaped without rewriting the stored record",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\ntelemetry:\n  level: off\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/root/.local/state/agent-marketplace/telemetry/last-delivery-failure", Body: `{"time":"2026-09-01T00:00:00Z","error":"SERVER\u001b[31m\r\n\u202e"}`},
			{Args: []string{"telemetry", "status"}, MustPass: true, OutContains: []string{`SERVER\x1b[31m\r\n\u202e`}, OutNotContains: []string{"\x1b", "\r", "\u202e"}, PathsUnchanged: []string{"/root/.local/state/agent-marketplace/telemetry/last-delivery-failure"}},
		},
	},
	{
		Name:    "security/command_error_controls",
		Desc:    "Given a repository source name carrying terminal controls, When status fails and doctor reports configuration errors, Then both print visible escapes without changing the configuration",
		RepoCfg: "apiVersion: agent-marketplace/v4\nsources:\n  \"\\e]0;TITLE\\a\\e[2J\\u202EEVIL\\N\":\n    directory: ./missing\n",
		Steps: []Step{
			{Args: []string{"status"}, AssertExitCode: intPtr(1), OutContains: []string{`Error: validate config "`, `/workspace/.agents/config.yaml": v4.source-name:`, `/workspace/.agents/config.yaml:3: sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85: source name "\x1b]0;TITLE\a\x1b[2J\u202eEVIL\u0085" must contain only letters, numbers, dots, underscores, or hyphens`, "hint: In ", "rename the source"}, OutNotContains: []string{"\x1b", "\a", "\u202e", "\u0085"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"doctor"}, MustPass: true, OutContains: []string{`error: validate config "`, `/workspace/.agents/config.yaml": v4.source-name:`, `/workspace/.agents/config.yaml:3: sources.\x1B]0;TITLE\x07\x1B[2J\u202EEVIL\x85: source name "\x1b]0;TITLE\a\x1b[2J\u202eEVIL\u0085" must contain only letters, numbers, dots, underscores, or hyphens`, "hint: In ", "rename the source"}, OutNotContains: []string{"\x1b", "\a", "\u202e", "\u0085"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
		},
	},
	{
		Name:      "security/cli_table_collection_controls",
		Desc:      "Given local registry and collection metadata containing terminal controls, When listing checking status and installing a collection, Then display is escaped while selection and installation remain intact",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {directory: \"./terminal-fixture/REF\\u001b[31m\\u202e\"}}\nresources: {team: {include: [skill/control]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/terminal-fixture/REF\x1b[31m\u202e/skills/control/SKILL.md", Body: "---\nname: control\ndescription: Control\n---\nCONTROL BODY\n"},
			{Action: ActionWriteFile, Path: "/workspace/terminal-fixture/REF\x1b[31m\u202e/collections/bundle/COLLECTION.yaml", Body: "name: bundle\nversion: \"VERSION\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"},
			{Action: ActionWriteFile, Path: "/workspace/terminal-fixture/REF\x1b[31m\u202e/collections/display/COLLECTION.yaml", Body: "name: \"DISPLAY\\u001b[31m\\u202e\"\nresources:\n  - name: control\n"},
			{Args: []string{"list", "--kind", "collection"}, MustPass: true, OutContains: []string{`DISPLAY\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{`REF\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml"}},
			{Args: []string{"install", "bundle", "--kind", "collection", "--scope", "repo"}, MustPass: true, OutContains: []string{`Installed collection bundle@VERSION\x1b[31m\u202e`}, OutNotContains: []string{"\x1b", "\u202e"}, FilesExist: []string{"/workspace/.factory/skills/control/SKILL.md"}},
		},
	},
	{
		Name: "security/command_marker_body_lifecycle",
		Desc: "Given a Factory command with marker text in its body and a handwritten sibling, When installing removing and reinstalling, Then real frontmatter ownership enables cleanup without adopting the handwritten file",
		Registries: []RegistryDef{{Name: "marker-body", Files: map[string]string{
			"commands/marker-command.md": "---\nname: marker-command\ndescription: Marker command\n---\n```yaml\n_managed_by: agent-marketplace\n```\n",
			"commands/keep.md":           "---\nname: keep\ndescription: Keep\n---\nKeep body\n",
		}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:marker-body}}', ref: main}}\nresources: {team: {include: [command/marker-command, command/keep]}}\n",
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
		Desc:       "Given handwritten Claude settings with opaque root members and a native hook group alongside managed hooks, When sync updates hooks and one managed hook is removed, Then unrelated members survive byte-for-byte while only the owned hook is removed",
		Registries: []RegistryDef{{Name: "opaque-hooks", Files: map[string]string{"hooks/hooks.json": `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo generated"}]}],"Stop":[{"hooks":[{"type":"command","command":"stale-owned"}]}]}}`}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources: {}\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources: {team: {git: '{{REG:opaque-hooks}}', ref: main}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.claude/settings.json", Body: "{\n  // handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],\n  \"hooks\": {\n    \"PostToolUse\": [{\"hooks\":[{\"type\":\"command\",\"command\":\"manual-hook\"}]}]\n  }\n}\n"},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.claude/settings.json": "echo generated"}, FileNotContains: map[string]string{"/workspace/.claude/settings.json": "_managed_by"}},
			{FileContains: map[string]string{"/workspace/.claude/settings.json": "stale-owned"}},
			{FileContains: map[string]string{"/workspace/.claude/settings.json": "// handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],"}},
			{FileContains: map[string]string{"/workspace/.claude/settings.json": "manual-hook"}},
			{Args: []string{"remove", "Stop-command", "--kind", "hook", "--scope", "repo"}, MustPass: true, FileNotContains: map[string]string{"/workspace/.claude/settings.json": "stale-owned"}, FileContains: map[string]string{"/workspace/.claude/settings.json": "echo generated"}},
			{FileContains: map[string]string{"/workspace/.claude/settings.json": "// handwritten metadata\n  \"notes\" : \"keep\",\n  \"options\" : {\"enabled\":false},\n  \"disabled\" : false,\n  \"value\" : 1.2300,\n  \"nothing\" : null,\n  \"empty\" : [],"}},
			{FileContains: map[string]string{"/workspace/.claude/settings.json": "manual-hook"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.claude/settings.json"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{"/workspace/.claude/settings.json"}},
		},
	},
	{
		Name: "security/codex_command_restriction",
		Desc: "Given a restricted command and an unrestricted sibling, When syncing to Codex, Then the unsupported restriction suppresses only that command",
		Registries: []RegistryDef{{Name: "codex-restrictions", Files: map[string]string{
			"commands/restricted.md": "---\nname: restricted\ndescription: Restricted\nallowed-tools: []\n---\nBody\n",
			"commands/plain.md":      "---\nname: plain\ndescription: Plain\n---\nBody\n",
		}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [codex]\nsources: {team: {git: '{{REG:codex-restrictions}}', ref: main}}\nresources: {team: {include: [command/restricted, command/plain]}}\n",
		Steps:     []Step{{Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{"skill.codex.security-restriction-unsupported"}, FilesAbsent: []string{"/root/.agents/skills/restricted"}, FileContains: map[string]string{"/root/.agents/skills/plain/SKILL.md": "x-agent-marketplace-kind: command"}}},
	},
	{
		Name: "security/nested_bundle_lock_inventory",
		Desc: "Given an empty parent plugin with nested hook and MCP resources, When lock and locked sync run, Then both resources are inventoried before materialization",
		Registries: []RegistryDef{{Name: "nested-inventory", Files: map[string]string{
			".claude-plugin/plugin.json":     `{"name":"container","version":"1.0.0"}`,
			"plugins/audit/.mcp.json":        `{"mcpServers":{"nested-server":{"command":"fixture-server"}}}`,
			"plugins/audit/hooks/hooks.json": `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo audit"}]}]}}`,
		}}},
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:nested-inventory}}', ref: main, source_harness: claude}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/lock.yaml": "kind: hook"}},
			{Args: []string{"sync", "--locked"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/lock.yaml": "name: nested-server", "/workspace/.factory/mcp.json": "fixture-server"}},
		},
	},
	{
		Name:      "security/blocked_registry_credentials_redacted",
		Desc:      "Given a denied credential-bearing registry, When sync loads effective config, Then error output and the persisted log redact credentials without changing the configured identity",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: []\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: 'https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE', ref: main}}\nresources: {team: {all: true}}\n",
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
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {team: {git: '{{REG:blocked-command-links}}', ref: main}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.devin/skills/remove/SKILL.md", "/workspace/.devin/skills/keep/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/skills/keep/SKILL.md", Body: "COMMAND SIBLING EDITED\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "command", "--from", "{{REG:blocked-command-links}}"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.devin/skills/remove"}, FileContains: map[string]string{"/workspace/.devin/skills/keep/SKILL.md": "COMMAND SIBLING EDITED"}, PathsUnchanged: []string{"/workspace/.devin/skills/keep/SKILL.md", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name:       "security/blocked_subagent_link_cleanup",
		Desc:       "Given locked subagents with an edited sibling, When one blocked subagent is removed, Then only its file link is unlinked",
		Registries: []RegistryDef{{Name: "blocked-subagent-links", Subagents: []Subagent{{Name: "remove"}, {Name: "keep"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {team: {git: '{{REG:blocked-subagent-links}}', ref: main}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.devin/agents/remove.md", "/workspace/.devin/agents/keep.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/agents/keep.md", Body: "SUBAGENT SIBLING EDITED\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "subagent", "--from", "{{REG:blocked-subagent-links}}"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.devin/agents/remove.md"}, FileContains: map[string]string{"/workspace/.devin/agents/keep.md": "SUBAGENT SIBLING EDITED"}, PathsUnchanged: []string{"/workspace/.devin/agents/keep.md", "/workspace/.agents/lock.yaml"}},
		},
	},
	{
		Name:       "security/blocked_skill_link_cleanup",
		Desc:       "Given locked skill symlinks and an edited installed sibling, When a blocked registry skill is removed, Then only its link is unlinked without rewriting siblings",
		Registries: []RegistryDef{{Name: "blocked-skill-links", Skills: []Skill{{Name: "remove", Description: "Remove original"}, {Name: "keep", Description: "Keep original"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:blocked-skill-links}}', ref: main}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/remove/SKILL.md", "/workspace/.factory/skills/keep/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.factory/skills/keep/SKILL.md", Body: "KEEP EDITED BY USER\n"},
			{Action: ActionWriteFile, Path: "/workspace/.factory/skills/manual/SKILL.md", Body: "HANDWRITTEN\n"},
			{Action: ActionWriteFile, Path: "/workspace/.gitignore", Body: "handwritten-ignore\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nallowed_registries: []\n"},
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nsources: {team: {git: '{{REG:blocked-owned-mcp}}', ref: main}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.devin/mcp_config.json": "keep-binary"}},
			{Action: ActionWriteFile, Path: "/workspace/.devin/mcp_config.json", Body: "{\n  // retained comment\n  \"custom\": true,\n  \"mcpServers\": {\n    \"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"},\n    \"remove\": {\"command\": \"remove-binary\"}\n  }\n}\n"},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [devin]\nallowed_registries: []\n"},
			{Args: []string{"remove", "remove", "--kind", "mcp", "--from", "{{REG:blocked-owned-mcp}}"}, MustPass: true,
				FileContains: map[string]string{
					"/workspace/.devin/mcp_config.json": "\"handwritten\": {\"command\": \"manual\"},\n    \"keep\": {\"command\": \"keep-edited\"}",
				},
				FileNotContains: map[string]string{"/workspace/.devin/mcp_config.json": "remove-binary"}},
		},
	},
	{
		Name:       "selection/local_resources_bind_repository_source",
		Desc:       "Given matching v4 repository and personal-local source declarations, When syncing the local selection, Then the shared provider materializes without editing either root",
		Registries: []RegistryDef{{Name: "local-selector", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:local-selector}}', ref: main}}\n",
		LocalCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:local-selector}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/config.local.yaml", "/workspace/.agents/config.yaml"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
		},
	},
	{
		Name:    "security/config_include_size",
		Desc:    "Given an oversized valid included config, When sync runs, Then it fails without mutating the inputs or materializing output",
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: [large.yaml]\nsources: {}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.agents/large.yaml", Body: "sources: {}\n#" + strings.Repeat("x", 4*1024*1024) + "\n"},
			{Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{"config file exceeds maximum size"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/large.yaml"}, FilesAbsent: []string{"/workspace/.factory"}},
		},
	},
	{
		Name:    "security/config_include_small",
		Desc:    "Given a small included registry and selector, When sync runs, Then the included skill materializes",
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: [small.yaml]\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.agents/registry/skills/bounded-skill/SKILL.md", Body: (Skill{Name: "bounded-skill", Description: "bounded include skill"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/.agents/small.yaml", Body: "sources: {team: {directory: ./registry}}\nresources: {team: {include: [skill/bounded-skill]}}\n"},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/bounded-skill/SKILL.md"}},
		},
	},
	{
		Name:       "locks/unlocked_organization_sync_parity",
		Desc:       "Given an organization registry without a lock, when syncing and checking repeatedly, then resources stay synced without creating an organization lock.",
		Registries: []RegistryDef{{Name: "org-parity", Skills: []Skill{{Name: "review"}}}},
		OrgCfg:     "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:org-parity}}', ref: main}}\nresources: {team: {all: true, required: true}}\n",
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
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  auto: {git: '{{REG:equivalent-roots}}', ref: main}\n  explicit: {git: '{{REG:equivalent-roots}}', ref: main, source_harness: claude}\n",
		Steps: []Step{
			{Args: []string{"lock", "--scope", "repo"}, MustPass: true},
			{Args: []string{"lock", "--check", "--scope", "repo"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{"review"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"EXACT REGISTRY LOCKS", "available"}, PathsUnchanged: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Action: ActionWriteRepoConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  explicit: {git: '{{REG:equivalent-roots}}', ref: main, source_harness: claude}\n  auto: {git: '{{REG:equivalent-roots}}', ref: main}\n"},
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:logging-registry}}', ref: main}}
resources: {team: {include: [skill/logging-skill]}}
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

	// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
	{
		Name: "hooks/additive_cross_registry",
		Desc: "Given two Factory registries with same-named hooks, when install-all and removal run, then both providers materialize additively and removing one preserves the other",
		Registries: []RegistryDef{
			{Name: "first", Files: map[string]string{".factory/hooks.json": `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo first"}]}]}`}},
			{Name: "second", Files: map[string]string{".factory/hooks.json": `{"PreToolUse":[{"matcher":"Execute","hooks":[{"type":"command","command":"echo second"}]}]}`}},
		},
		RepoCfg: `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources:
  first: {git: '{{REG:first}}', ref: main, source_harness: factory}
  second: {git: '{{REG:second}}', ref: main, source_harness: factory}
`,
		Steps: []Step{
			{Args: []string{ "install", "--scope", "repo",  "--all", "--kind", "hook"}, MustPass: true, OutContains: []string{"PreToolUse-command"}, OutNotContains: []string{"multiple registries"}, RegularFiles: []string{"/workspace/.factory/hooks.json"}, FileContains: map[string]string{"/workspace/.factory/hooks.json": "echo first"}},
			{Args: []string{"list", "--kind", "hook"}, MustPass: true, OutContains: []string{"PreToolUse-command"}, FileContains: map[string]string{"/workspace/.factory/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks.json", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks.json"}},
			{Args: []string{"remove", "PreToolUse-command", "--kind", "hook", "--from", "{{REG:first}}"}, MustPass: true, FileNotContains: map[string]string{"/workspace/.factory/hooks.json": "echo first"}, FileContains: map[string]string{"/workspace/.factory/hooks.json": "echo second"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks.json", "/workspace/.agents/lock.yaml"}},
		},
	},
	// spec: hook-resources/native-only-git-hook-sources-retain-frozen-projection
	{
		Name:       "hooks/additive_session_start",
		Desc:       "Given one Factory registry with two SessionStart handlers, when both generated names are selected, then both handlers remain stable across sync and check",
		Registries: []RegistryDef{{Name: "session-start", Files: map[string]string{".factory/hooks.json": `{"SessionStart":[{"hooks":[{"type":"command","command":"echo first"}]},{"hooks":[{"type":"command","command":"echo second"}]}]}`}}},
		RepoCfg: `apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {team: {git: '{{REG:session-start}}', ref: main, source_harness: factory}}
resources: {team: {include: [hook/SessionStart-command, hook/SessionStart-command-2]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/hooks.json": "echo first"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks.json", "/workspace/.agents/lock.yaml"}, FileContains: map[string]string{"/workspace/.factory/hooks.json": "echo second"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/hooks.json"}},
		},
	},

	// ===== collision =====
	// spec: resource-selection/repository-winner-does-not-displace-a-user-scope-selection
	{
		Name:       "collision/cross_scope_sync_keeps_both_skills",
		Desc:       "Given user and repository sources selecting different skills with the same installed name, When syncing in the repository, Then both scopes retain their own skill and sync --check succeeds",
		Registries: []RegistryDef{{Name: "user-skill", Skills: []Skill{{Name: "shared", Description: "User selection"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:user-skill}}', ref: main}}\nresources: {user: {include: [skill/shared]}}\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/shared/SKILL.md", Body: (Skill{Name: "shared", Description: "Repository selection"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"multiple registries"},
				Symlinks: []string{"/root/.factory/skills/shared", "/workspace/.factory/skills/shared"},
				FileContains: map[string]string{
					"/root/.factory/skills/shared/SKILL.md":      "User selection",
					"/workspace/.factory/skills/shared/SKILL.md": "Repository selection",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: resource-selection/required-user-scope-selection-suppresses-a-repository-selection
	{
		Name:       "collision/required_machine_prevents_repo_shadow",
		Desc:       "Given a required user skill and another repository skill with the same installed name, When syncing in the repository, Then the required machine skill remains and the repository cannot shadow it",
		Registries: []RegistryDef{{Name: "required-user", Skills: []Skill{{Name: "shared", Description: "Mandatory user selection"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:required-user}}', ref: main}}\nresources: {team: {include: [skill/shared], required: true}}\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/shared/SKILL.md", Body: (Skill{Name: "shared", Description: "Unreviewed repository selection"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"multiple registries"},
				Symlinks:     []string{"/root/.factory/skills/shared"},
				FilesAbsent:  []string{"/workspace/.factory/skills/shared"},
				FileContains: map[string]string{"/root/.factory/skills/shared/SKILL.md": "Mandatory user selection"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills/shared"}, FilesAbsent: []string{"/workspace/.factory/skills/shared"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/collection-member-is-not-displaced-by-a-selection-in-the-other-scope
	{
		Name: "collision/cross_scope_collection_member_keeps_both_skills",
		Desc: "Given a user collection member and a repository skill with the same installed name, When syncing in the repository, Then both scopes retain their own skill without a collision warning and sync --check succeeds",
		Registries: []RegistryDef{{Name: "user-bundle", Skills: []Skill{{Name: "shared", Description: "User collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:user-bundle}}', ref: main}}\nresources: {user: {include: [collection/bundle]}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/shared/SKILL.md", Body: (Skill{Name: "shared", Description: "Repository selection"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"multiple registries"},
				Symlinks: []string{"/root/.factory/skills/shared", "/workspace/.factory/skills/shared"},
				FileContains: map[string]string{
					"/root/.factory/skills/shared/SKILL.md":      "User collection member",
					"/workspace/.factory/skills/shared/SKILL.md": "Repository selection",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true, OutNotContains: []string{"multiple registries"}, PathsUnchanged: []string{"/root/.factory/skills/shared", "/workspace/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/unqualified-member-retains-the-collection-owner
	{
		Name: "collection/unqualified_repo_member_keeps_scope",
		Desc: "Given an unselected user entry and a repository collection at the same registry location, When syncing, Then the member is installed only in the repository",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Repository collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:shared-bundle}}', ref: main}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {repo: {git: '{{REG:shared-bundle}}', ref: main}}\nresources: {repo: {include: [collection/bundle]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"multiple registries"},
				Symlinks:     []string{"/workspace/.factory/skills/shared"},
				FilesAbsent:  []string{"/root/.factory/skills/shared"},
				FileContains: map[string]string{"/workspace/.factory/skills/shared/SKILL.md": "Repository collection member"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.factory/skills/shared"},
				FilesAbsent: []string{"/root/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/same-collection-remains-selected-in-both-install-scopes
	{
		Name: "collection/unqualified_members_keep_both_scopes",
		Desc: "Given the same collection selected in user and repository configuration, When syncing, Then both scopes materialize its member without collisions",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Shared collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:shared-bundle}}', ref: main}}\nresources: {user: {include: [collection/bundle]}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {repo: {git: '{{REG:shared-bundle}}', ref: main}}\nresources: {repo: {include: [collection/bundle]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"multiple registries"},
				Symlinks: []string{"/root/.factory/skills/shared", "/workspace/.factory/skills/shared"},
				FileContains: map[string]string{
					"/root/.factory/skills/shared/SKILL.md":      "Shared collection member",
					"/workspace/.factory/skills/shared/SKILL.md": "Shared collection member",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true, OutNotContains: []string{"multiple registries"},
				PathsUnchanged: []string{"/root/.factory/skills/shared", "/workspace/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/unqualified-required-member-retains-required-policy
	{
		Name: "collection/unqualified_required_member_keeps_policy",
		Desc: "Given an optional organization entry and a required user collection at the same location, When syncing with a competing repository resource, Then required policy prevents repository shadowing",
		Registries: []RegistryDef{{Name: "shared-bundle", Skills: []Skill{{Name: "shared", Description: "Required collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nresources:\n  - name: shared\n"}}},
		OrgCfg:    "apiVersion: agent-marketplace/v4\nsources: {org: {git: '{{REG:shared-bundle}}', ref: main}}\n",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:shared-bundle}}', ref: main}}\nresources: {user: {include: [collection/bundle], required: true}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/shared/SKILL.md", Body: (Skill{Name: "shared", Description: "Optional repository selection"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"multiple registries"},
				Symlinks:     []string{"/root/.factory/skills/shared"},
				FilesAbsent:  []string{"/workspace/.factory/skills/shared"},
				FileContains: map[string]string{"/root/.factory/skills/shared/SKILL.md": "Required collection member"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills/shared"},
				FilesAbsent: []string{"/workspace/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/required-selection-through-a-collection-suppresses-a-repository-selection
	{
		Name: "collision/required_collection_member_prevents_repo_shadow",
		Desc: "Given a required organization collection member and a repository skill with the same installed name, When syncing in the repository, Then the required member remains in the machine harness, the repository skill is not installed, and the collision is reported",
		Registries: []RegistryDef{{Name: "org-bundle", Skills: []Skill{{Name: "shared", Description: "Mandatory collection member"}},
			Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}}},
		OrgCfg:    "apiVersion: agent-marketplace/v4\nsources: {org: {git: '{{REG:org-bundle}}', ref: main}}\nresources: {org: {include: [collection/bundle], required: true}}\n",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {repo: {directory: ./resources}}\nresources: {repo: {include: [skill/shared]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/shared/SKILL.md", Body: (Skill{Name: "shared", Description: "Unreviewed repository selection"}).SkillFile()},
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"multiple registries"},
				Symlinks:     []string{"/root/.factory/skills/shared"},
				FilesAbsent:  []string{"/workspace/.factory/skills/shared"},
				FileContains: map[string]string{"/root/.factory/skills/shared/SKILL.md": "Mandatory collection member"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills/shared"}, FilesAbsent: []string{"/workspace/.factory/skills/shared"}},
		},
	},
	// spec: resource-selection/required-collection-member-displaces-an-optional-selection-in-the-same-scope
	{
		Name: "collision/required_collection_member_displaces_user_selection",
		Desc: "Given a required organization collection member and a non-required user skill with the same installed name, When syncing in the repository, Then the required member is installed in the machine harness, the user skill is not, and the collision is reported",
		Registries: []RegistryDef{
			{Name: "org-bundle", Skills: []Skill{{Name: "shared", Description: "Mandatory collection member"}},
				Files: map[string]string{"collections/bundle/COLLECTION.yaml": "name: bundle\nversion: \"1.0\"\nresources:\n  - name: shared\n"}},
			{Name: "user-skill", Skills: []Skill{{Name: "shared", Description: "Optional user selection"}}},
		},
		OrgCfg:    "apiVersion: agent-marketplace/v4\nsources: {org: {git: '{{REG:org-bundle}}', ref: main}}\nresources: {org: {include: [collection/bundle], required: true}}\n",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {user: {git: '{{REG:user-skill}}', ref: main}}\nresources: {user: {include: [skill/shared]}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"multiple registries"},
				Symlinks:     []string{"/root/.factory/skills/shared"},
				FilesAbsent:  []string{"/workspace/.factory/skills/shared"},
				FileContains: map[string]string{"/root/.factory/skills/shared/SKILL.md": "Mandatory collection member"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/skills/shared"}},
		},
	},
	// spec: harness-compilation/required-resource-keeps-a-case-only-collision
	{
		Name: "collision/required_command_keeps_case_only_path",
		Desc: "Given an organization-required command deploy and a user-selected OpenCode command Deploy, When syncing for Claude Code, Then the required command is installed with a shared-path warning, the user command is not, and the output stays in sync",
		Registries: []RegistryDef{
			{Name: "org-command", Commands: []Command{{Name: "deploy", Description: "Mandatory organization command", Command: "echo org"}}},
			{Name: "user-opencode", Files: map[string]string{".opencode/commands/Deploy.md": "---\ndescription: User selected command\n---\nRun the user command.\n"}},
		},
		OrgCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources: {org: {git: '{{REG:org-command}}', ref: main}}\nresources: {org: {all: true, required: true}}\n",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [claude]\nsources: {user: {git: '{{REG:user-opencode}}', ref: main, source_harness: opencode}}\nresources: {user: {include: [command/Deploy]}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {}\n",
		Steps: []Step{
			// Content checks rather than an absent Deploy.md keep the case
			// portable: on a case-insensitive host both names are one file.
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"materialize.collision.shared-path", `command "Deploy"`},
				OutNotContains:  []string{"bundle.conflict.duplicate-artifact-path"},
				FileContains:    map[string]string{"/root/.claude/commands/deploy.md": "Mandatory organization command"},
				FileNotContains: map[string]string{"/root/.claude/commands/deploy.md": "Run the user command."}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.claude/commands"}},
		},
	},
	{
		Name: "collision/required_skill_keeps_shared_native_path",
		Desc: "Given an organization-required skill and a user-selected command with the same name, When syncing for Devin CLI and Claude Code, Then Devin's shared skills path keeps the required skill with a shared-path warning, and Claude Code installs both",
		Registries: []RegistryDef{
			{Name: "org-skill", Skills: []Skill{{Name: "audit", Description: "Mandatory organization skill"}}},
			{Name: "user-command", Commands: []Command{{Name: "audit", Description: "User selected command", Command: "echo user"}}},
		},
		OrgCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [devin, claude]\nsources: {org: {git: '{{REG:org-skill}}', ref: main}}\nresources: {org: {all: true, required: true}}\n",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [devin, claude]\nsources: {user: {git: '{{REG:user-command}}', ref: main}}\nresources: {user: {include: [command/audit]}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, OutContains: []string{"materialize.collision.shared-path"},
				FileContains: map[string]string{
					"/root/.config/devin/skills/audit/SKILL.md": "Mandatory organization skill",
					"/root/.claude/skills/audit/SKILL.md":       "Mandatory organization skill",
					"/root/.claude/commands/audit.md":           "User selected command",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/root/.config/devin/skills/audit"}},
		},
	},
	{
		Name: "install/collision_warning_scoped",
		Desc: "Given two selected registries with skill and command collisions, When installing one skill, Then its collision is reported without unrelated warnings and the existing winner is installed",
		Registries: []RegistryDef{
			{Name: "registry-a", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry A"}, {Name: "other", Description: "Other from registry A"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry A", Command: "echo COMMAND A"}}},
			{Name: "registry-b", Skills: []Skill{{Name: "deploy", Description: "Deploy from registry B"}, {Name: "other", Description: "Other from registry B"}}, Commands: []Command{{Name: "deploy", Description: "Command from registry B", Command: "echo COMMAND B"}}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources:\n  first: {git: '{{REG:registry-a}}', ref: main}\n  second: {git: '{{REG:registry-b}}', ref: main}\nresources:\n  first: {all: true}\n  second: {all: true}\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:registry-a}}', ref: main}
  second: {git: '{{REG:registry-b}}', ref: main}
resources:
  first: {include: [skill/deploy]}
  second: {include: [skill/deploy]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:mono-registry}}', ref: main, path: team-a}
  second: {git: '{{REG:mono-registry}}', ref: main, path: team-b}
resources:
  first: {include: [skill/skill-a]}
  second: {include: [skill/skill-b]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mixed-registry}}', ref: main}}
resources: {team: {include: [skill/deploy, command/deploy]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources: {team: {git: '{{REG:multi-harness}}', ref: main}}
resources: {team: {include: [skill/universal-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - claude
  - factory
  - cursor
  - windsurf
  - gemini
sources: {team: {git: '{{REG:metadata-registry}}', ref: main}}
resources: {team: {include: [skill/owned-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources: {team: {git: '{{REG:harness-removal}}', ref: main}}
resources: {team: {include: [skill/prunable-skill]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/skills/prunable-skill/SKILL.md",
					"/root/.codeium/windsurf/skills/prunable-skill/SKILL.md",
				}},
			{Action: ActionWriteGlobalConfig, Body: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:harness-removal}}', ref: main}}
resources: {team: {include: [skill/prunable-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:symlink-test}}', ref: main}}
resources: {team: {include: [skill/linked-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [factory]
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
materialization: {mode: file}
sources: {team: {git: '{{REG:file-mode-test}}', ref: main}}
resources: {team: {include: [skill/portable-skill]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses:\n  - factory\n  - devin\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nmaterialization:\n  mode: file\n  targets:\n    devin: symlink\nsources: {team: {git: '{{REG:mixed-mode-test}}', ref: main}}\nresources: {team: {include: [skill/mixed-mode-skill]}}\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:manual-survive}}', ref: main}}
resources: {team: {include: [skill/managed-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:droid-survive}}', ref: main}}
resources: {team: {include: [subagent/reviewer]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:droid-remove-survive}}', ref: main}}
resources: {team: {include: [subagent/reviewer]}}
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
				Body: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:droid-remove-survive}}', ref: main}}
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.factory/droids/reviewer.md"},
				FilesExist:  []string{"/root/.factory/droids/worker.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/worker.md": "# worker (user-owned)",
				}},
		},
	},
	// spec: materialization/deselected-gemini-subagents-are-removed
	{
		Name: "materialize/gemini_last_subagent_deselection_prunes_user_output",
		Desc: "Given a synced user-scope Gemini subagent that is the only managed Gemini subagent, alongside a user-authored one, When it is dropped from the selection and sync re-runs, Then the managed subagent is pruned, the user-authored one survives and check reports no drift",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-deselect", Subagents: []Subagent{
				{Name: "reviewer", Description: "Managed by agent-marketplace"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [gemini]
sources: {team: {git: '{{REG:gemini-subagent-deselect}}', ref: main}}
resources: {team: {include: [subagent/reviewer]}}
`,
		Steps: []Step{
			{Action: ActionWriteFile,
				Path: "/root/.gemini/agents/worker.md",
				Body: `---
name: worker
description: my hand-written worker subagent
---

# worker (user-owned)
`},
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/root/.gemini/agents/reviewer.md"},
				FilesExist: []string{"/root/.gemini/agents/worker.md"}},
			{Action: ActionWriteGlobalConfig,
				Body: `apiVersion: agent-marketplace/v4
harnesses: [gemini]
sources: {team: {git: '{{REG:gemini-subagent-deselect}}', ref: main}}
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.gemini/agents/reviewer.md"},
				FilesExist:  []string{"/root/.gemini/agents/worker.md"},
				FileContains: map[string]string{
					"/root/.gemini/agents/worker.md": "# worker (user-owned)",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: materialization/deselected-gemini-subagents-are-removed
	{
		Name: "remove/gemini_last_subagent_removes_repo_output",
		Desc: "Given a Gemini subagent and skill installed in the repository, When the subagent is removed and sync re-runs, Then its output is gone, the skill stays and check reports no drift",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-remove",
				Skills:    []Skill{{Name: "keep-skill", Description: "Stays installed"}},
				Subagents: []Subagent{{Name: "helper", Description: "Removed by the test"}}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [gemini]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:gemini-subagent-remove}}', ref: main}}
resources: {team: {include: [skill/keep-skill, subagent/helper]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/workspace/.gemini/agents/helper.md"},
				FilesExist: []string{"/workspace/.gemini/skills/keep-skill/SKILL.md"}},
			{Args: []string{"remove", "--scope", "repo", "helper"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.gemini/agents/helper.md"},
				FilesExist:  []string{"/workspace/.gemini/skills/keep-skill/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: materialization/deselected-gemini-subagents-are-removed
	{
		Name: "remove/gemini_last_subagent_removes_user_output",
		Desc: "Given a Gemini subagent and skill installed user-wide, When the subagent is removed with --scope user and sync re-runs, Then its output is gone, the skill stays and check reports no drift",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-remove-user",
				Skills:    []Skill{{Name: "keep-skill", Description: "Stays installed"}},
				Subagents: []Subagent{{Name: "helper", Description: "Removed by the test"}}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [gemini]
sources: {team: {git: '{{REG:gemini-subagent-remove-user}}', ref: main}}
resources: {team: {include: [skill/keep-skill, subagent/helper]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/root/.gemini/agents/helper.md"},
				FilesExist: []string{"/root/.gemini/skills/keep-skill/SKILL.md"}},
			{Args: []string{"remove", "--scope", "user", "helper"}, MustPass: true},
			{Args: []string{"sync"}, MustPass: true,
				FilesAbsent: []string{"/root/.gemini/agents/helper.md"},
				FilesExist:  []string{"/root/.gemini/skills/keep-skill/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: materialization/deinit-removes-gemini-subagents
	{
		Name: "deinit/gemini_repo_subagent_output_is_removed",
		Desc: "Given a Gemini subagent installed in the repository next to a user-authored one, When deinit runs for the repository, Then the managed subagent is removed and the user-authored one survives",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-deinit", Subagents: []Subagent{
				{Name: "helper", Description: "Removed by deinit"},
			}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [gemini]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:gemini-subagent-deinit}}', ref: main}}
resources: {team: {include: [subagent/helper]}}
`,
		Steps: []Step{
			{Action: ActionWriteFile,
				Path: "/workspace/.gemini/agents/worker.md",
				Body: `---
name: worker
description: my hand-written worker subagent
---

# worker (user-owned)
`},
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/workspace/.gemini/agents/helper.md"},
				FilesExist: []string{"/workspace/.gemini/agents/worker.md"}},
			{Args: []string{"deinit", "--scope", "repo"}, MustPass: true,
				FilesAbsent: []string{"/workspace/.gemini/agents/helper.md"},
				FilesExist:  []string{"/workspace/.gemini/agents/worker.md"},
				FileContains: map[string]string{
					"/workspace/.gemini/agents/worker.md": "# worker (user-owned)",
				}},
		},
	},
	// spec: materialization/deinit-removes-gemini-subagents
	{
		Name: "deinit/gemini_user_subagent_output_is_removed",
		Desc: "Given a Gemini subagent installed user-wide next to a user-authored one, When deinit runs for the user scope, Then the managed subagent is removed and the user-authored one survives",
		Registries: []RegistryDef{
			{Name: "gemini-subagent-deinit-user", Subagents: []Subagent{
				{Name: "helper", Description: "Removed by deinit"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [gemini]
sources: {team: {git: '{{REG:gemini-subagent-deinit-user}}', ref: main}}
resources: {team: {include: [subagent/helper]}}
`,
		Steps: []Step{
			{Action: ActionWriteFile,
				Path: "/root/.gemini/agents/worker.md",
				Body: `---
name: worker
description: my hand-written worker subagent
---

# worker (user-owned)
`},
			{Args: []string{"sync"}, MustPass: true,
				Symlinks:   []string{"/root/.gemini/agents/helper.md"},
				FilesExist: []string{"/root/.gemini/agents/worker.md"}},
			{Args: []string{"deinit", "--scope", "user"}, MustPass: true,
				FilesAbsent: []string{"/root/.gemini/agents/helper.md"},
				FilesExist:  []string{"/root/.gemini/agents/worker.md"},
				FileContains: map[string]string{
					"/root/.gemini/agents/worker.md": "# worker (user-owned)",
				}},
		},
	},
	{
		// spec: materialization/cli-check-refuses-stale-linked-content-without-repairing-it
		Name:      "materialize/sync_check_detects_linked_content_drift",
		Desc:      "Given linked resources from a live directory source, When their command, skill and support content changes, Then check exits 1 without repairing caches; sync repairs them and check exits 0",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [claude, opencode]\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./resources, source_harness: claude}}\nresources: {local: {include: [command/check, skill/review]}}\n",
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/resources/commands/check.md", Body: (Command{Name: "check"}).CommandFile() + "\noriginal command\n"},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/review/SKILL.md", Body: (Skill{Name: "review", Description: "Original skill"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/review/reference.txt", Body: "original support"},
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.claude/commands/check.md", "/workspace/.claude/skills/review", "/workspace/.opencode/commands/check.md", "/workspace/.opencode/skills/review"}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents", "/workspace/.claude", "/workspace/.opencode", "/root/.cache/agent-marketplace"}},
			{Action: ActionWriteFile, Path: "/workspace/resources/commands/check.md", Body: (Command{Name: "check"}).CommandFile() + "\nupdated command\n"},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/review/SKILL.md", Body: (Skill{Name: "review", Description: "Updated skill"}).SkillFile()},
			{Action: ActionWriteFile, Path: "/workspace/resources/skills/review/reference.txt", Body: "updated support"},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1),
				PathsUnchanged: []string{"/workspace/.agents", "/workspace/.claude", "/workspace/.opencode", "/root/.cache/agent-marketplace"},
				FilesAbsent:    []string{"/workspace/.agents/lock.yaml"},
				FileContains: map[string]string{
					"/workspace/.claude/commands/check.md":             "original command",
					"/workspace/.opencode/commands/check.md":           "original command",
					"/workspace/.claude/skills/review/SKILL.md":        "Original skill",
					"/workspace/.opencode/skills/review/SKILL.md":      "Original skill",
					"/workspace/.claude/skills/review/reference.txt":   "original support",
					"/workspace/.opencode/skills/review/reference.txt": "original support",
				}},
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/workspace/.claude/commands/check.md":             "updated command",
					"/workspace/.opencode/commands/check.md":           "updated command",
					"/workspace/.claude/skills/review/SKILL.md":        "Updated skill",
					"/workspace/.opencode/skills/review/SKILL.md":      "Updated skill",
					"/workspace/.claude/skills/review/reference.txt":   "updated support",
					"/workspace/.opencode/skills/review/reference.txt": "updated support",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{"/workspace/.agents", "/workspace/.claude", "/workspace/.opencode", "/root/.cache/agent-marketplace"}},
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:drift-check}}', ref: main}}
resources: {team: {include: [skill/drift-skill]}}
`,
		Steps: []Step{
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1)},
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(0)},
			{Action: ActionWriteGlobalConfig, Body: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:drift-check}}', ref: main}
  second: {git: '{{REG:drift-check-2}}', ref: main}
resources:
  first: {include: [skill/drift-skill]}
  second: {include: [skill/new-skill]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:tag-selection}}', ref: main}}
resources: {team: {tags: [k8s]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:commands-test}}', ref: main}}
resources: {team: {include: [command/greet]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:precedence-registry}}', ref: main}}
resources: {team: {include: [skill/skill-a, skill/skill-b], exclude: [skill/skill-a]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:local-precedence}}', ref: main}}
resources: {team: {include: [skill/wanted, skill/unwanted]}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:local-precedence}}', ref: main}}
resources: {team: {exclude: [skill/unwanted]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:required-registry}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:required-registry}}', ref: main}}
resources: {team: {exclude: [skill/security-audit]}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:required-registry}}', ref: main}}
resources: {team: {exclude: [skill/security-audit]}}
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
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {team: {git: '{{REG:org-registry}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:same-level}}', ref: main}}
resources: {team: {include: [skill/included, skill/conflicted], exclude: [skill/conflicted]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/skills/included/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/skills/conflicted"}},
		},
	},
	{
		Name: "precedence/default_true_auto_selects_all",
		Desc: "Given a registry with all:true, When sync is run, Then all discovered resources are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "default-registry", Skills: []Skill{
				{Name: "auto-a", Description: "Auto-selected A"},
				{Name: "auto-b", Description: "Auto-selected B"},
				{Name: "auto-c", Description: "Auto-selected C"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:default-registry}}', ref: main}}
resources: {team: {all: true}}
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
		Desc: "Given an all:true registry with an explicit exclude, When sync is run, Then excluded resources are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "default-exclude", Skills: []Skill{
				{Name: "keep-me", Description: "Should stay"},
				{Name: "drop-me", Description: "Should be excluded"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:default-exclude}}', ref: main}}
resources: {team: {all: true, exclude: [skill/drop-me]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:install-reg}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:tag-install}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:multi-kind}}', ref: main}}
resources: {team: {include: [skill/deploy-skill, command/greet, subagent/worker]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		Name: "install/multiple_names_from_git_source",
		// spec: resource-installation/install-several-names-from-one-explicit-source
		Desc: "Given a git registry with a bundle path and an unrelated configured selection, When install names two resources with shared --from/--ref/--path, Then both materialize, unrelated state is retained, and sync --check passes",
		Registries: []RegistryDef{
			{Name: "multi-src", Files: map[string]string{
				"bundles/skills/first/SKILL.md":  (Skill{Name: "first", Description: "First"}).SkillFile(),
				"bundles/skills/second/SKILL.md": (Skill{Name: "second", Description: "Second"}).SkillFile(),
				"skills/root-only/SKILL.md":      (Skill{Name: "root-only", Description: "Root only"}).SkillFile(),
			}},
			{Name: "multi-other", Skills: []Skill{{Name: "keep"}}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [factory]
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:multi-other}}', ref: main}}
resources: {team: {include: [skill/keep]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/workspace/.factory/skills/keep/SKILL.md"}},
			{Args: []string{ "install", "--scope", "repo",  "first", "second", "--from", "{{REG:multi-src}}", "--ref", "main", "--path", "bundles"}, MustPass: true,
				OutContains: []string{"first", "second"},
				FilesExist: []string{
					"/workspace/.factory/skills/first/SKILL.md",
					"/workspace/.factory/skills/second/SKILL.md",
					"/workspace/.factory/skills/keep/SKILL.md",
				},
				FilesAbsent: []string{"/workspace/.factory/skills/root-only"},
				FileContains: map[string]string{
					"/workspace/.agents/config.yaml": "bundles",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true},
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:missing-reg}}', ref: main}}
`,
		Steps: []Step{
			{Args: []string{"install", "nonexistent-skill"}, AssertExitCode: intPtr(1),
				OutContains: []string{"nonexistent"}},
		},
	},
	{
		Name: "install/scope_user_rejects_repo_relative_registry",
		Desc: "Given a repo-relative local registry, When install --scope user is run, Then it fails without changing the user config",
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {directory: ./plugins/local}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/local-registry/README.md", Body: "local registry\n"},
			{Args: []string{"install", "--scope", "repo", "some-skill", "--from", "./local-registry", "--path", "missing"}, AssertExitCode: intPtr(1),
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:scope-reg}}', ref: main}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:local-reg}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:roundtrip-reg}}', ref: main}}
resources: {team: {include: [skill/roundtrip-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:required-reg}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"remove", "mandatory-skill"}, AssertExitCode: intPtr(1),
				OutContains: []string{"required"}},
		},
	},
	{
		Name: "remove/default_true_adds_exclude",
		Desc: "Given all:true auto-selected skills, When remove --scope user is run for one skill and re-synced, Then that skill is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "default-rm", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "drop-skill"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:default-rm}}', ref: main}}
resources: {team: {all: true}}
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
		Desc:       "Given a default-selected skill disabled by a provider-scoped v4 local exclude, When named ad-hoc install reinstalls it, Then it materializes and only that exclude is cleared",
		Registries: []RegistryDef{{Name: "adhoc-reinstall-named", Skills: []Skill{{Name: "foo"}, {Name: "keep"}}}},
		RepoCfg: `
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {team: {git: '{{REG:adhoc-reinstall-named}}', ref: main}}
resources: {team: {all: true}}
`,
		LocalCfg: "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:adhoc-reinstall-named}}', ref: main}}\nresources: {team: {exclude: [skill/foo, skill/keep]}}\n",
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
		Desc: "Given an all:true registry with one skill excluded, When that skill is re-installed, Then the exclude is dropped WITHOUT adding an explicit include so default auto-select keeps every other skill enabled",
		Registries: []RegistryDef{
			{Name: "reinstall-default", Skills: []Skill{
				{Name: "keep-skill"},
				{Name: "cohort-analysis"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:reinstall-default}}', ref: main}}
resources: {team: {all: true}}
`,
		Steps: []Step{
			// all:true auto-selects every skill.
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
			// and keep all:true — NOT add skills.include (which would flip
			// the entry out of auto-select-all and silently deselect keep-skill).
			{Args: []string{"install", "--scope", "user", "cohort-analysis"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.agents/config.yaml": "all: true",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:from-rm}}', ref: main}}
resources: {team: {include: [skill/from-skill-a, skill/from-skill-b]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:from-entry-rm}}', ref: main}}
resources: {team: {include: [skill/entry-skill]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:subagent-reg}}', ref: main}}
resources: {team: {include: [subagent/worker]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-reg}}', ref: main}}
resources: {team: {include: [mcp/my-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:windsurf-mcp-reg}}', ref: main}}
resources: {team: {include: [mcp/env-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources: {team: {git: '{{REG:windsurf-rt-reg}}', ref: main}}
resources: {team: {include: [mcp/rt-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:kind-filter}}', ref: main}}
resources: {team: {include: [subagent/only-this-agent]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-model}}', ref: main}}
resources: {team: {include: [subagent/model-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/model-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/model-agent.md": "model: claude-sonnet-5",
				}},
		},
	},
	{
		Name: "subagent/unknown_model_omitted_without_failing_sync",
		Desc: "Given an unknown cross-harness model, When sync runs, Then a warning is emitted while the droid body and tools survive without the model and repeated sync converges",
		Registries: []RegistryDef{
			{Name: "sub-model-pt", Subagents: []Subagent{
				{Name: "passthrough-agent", Model: "custom-model-v2", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-model-pt}}', ref: main}}
resources: {team: {include: [subagent/passthrough-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains: []string{"model.unresolved"},
				FilesExist:  []string{"/root/.factory/droids/passthrough-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/passthrough-agent.md": "# passthrough-agent",
				},
				FileNotContains: map[string]string{"/root/.factory/droids/passthrough-agent.md": "model:"}},
			{FileContains: map[string]string{"/root/.factory/droids/passthrough-agent.md": "- Read"}},
			{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{"/root/.factory/droids/passthrough-agent.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "subagent/inheritance_uses_devin_default_without_failing",
		Desc:       "Given an inherited model and effort preference, When compiling for Devin, Then sync warns about default selection without treating inherit as an unknown model and keeps the resource",
		Registries: []RegistryDef{{Name: "sub-inherit", Subagents: []Subagent{{Name: "inherited-agent", Model: "inherit", Effort: "medium", Tools: []string{"Read"}}}}},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [devin]
sources: {team: {git: '{{REG:sub-inherit}}', ref: main}}
resources: {team: {include: [subagent/inherited-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				OutContains:     []string{"subagent.devin.model-default", "subagent.devin.effort-dropped"},
				OutNotContains:  []string{"subagent.devin.model-unmappable"},
				FileContains:    map[string]string{"/root/.config/devin/agents/inherited-agent.md": "# inherited-agent"},
				FileNotContains: map[string]string{"/root/.config/devin/agents/inherited-agent.md": "model:"}},
			{FileContains: map[string]string{"/root/.config/devin/agents/inherited-agent.md": "- read"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name: "subagent/write_tool_maps_to_create",
		Desc: "Given a registry with a subagent using Write tool, When sync is run for factory, Then Write maps to Create alongside the existing Read tool",
		Registries: []RegistryDef{
			{Name: "sub-tools", Subagents: []Subagent{
				{Name: "write-agent", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-tools}}', ref: main}}
resources: {team: {include: [subagent/write-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/write-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/write-agent.md": "- Read",
				}},
			{FileContains: map[string]string{
				"/root/.factory/droids/write-agent.md": "- Create",
			},
				FileNotContains: map[string]string{
					"/root/.factory/droids/write-agent.md": "  - Edit",
				}},
			{FileNotContains: map[string]string{
				"/root/.factory/droids/write-agent.md": "  - Write",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-tool-pt}}', ref: main}}
resources: {team: {include: [subagent/unk-tool-agent, subagent/known-tool-agent]}}
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
	// spec: harness-compilation/comma-separated-subagent-tool-strings-become-separate-tools
	{
		Name: "subagent/comma_separated_tools_sync_as_separate_tools",
		Desc: "Given a Claude-layout subagent whose tools are the string \"Read, Grep\", When sync runs for Claude, Factory and Gemini, Then each native file lists the tools as separate entries and check reports no drift",
		Registries: []RegistryDef{
			{Name: "sub-tool-string", Files: map[string]string{
				".claude/agents/reader.md": "---\nname: reader\ndescription: Reader\ntools: Read, Grep\n---\nRead only.\n",
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [claude, factory, gemini]
sources: {team: {git: '{{REG:sub-tool-string}}', ref: main}}
resources: {team: {include: [subagent/reader]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.claude/agents/reader.md":  "tools:\n  - Read\n  - Grep\n",
					"/root/.factory/droids/reader.md": "tools:\n  - Read\n  - Grep\n",
					"/root/.gemini/agents/reader.md":  "tools:\n  - read_file\n  - read_many_files\n  - grep_search\n",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	// spec: harness-compilation/a-subagent-tool-string-that-is-not-well-formed-is-kept-as-a-single-entry
	{
		Name: "subagent/malformed_tools_string_stays_one_entry",
		Desc: "Given a Claude-layout subagent whose tools string has an unmatched parenthesis next to a valid sibling, When sync runs for Claude, Then both are installed, the malformed string stays one entry exactly as written, as in earlier versions, and check reports no drift",
		Registries: []RegistryDef{
			{Name: "sub-tool-malformed", Files: map[string]string{
				".claude/agents/malformed.md": "---\nname: malformed\ndescription: Malformed\ntools: Read, Bash(git diff\n---\nMalformed.\n",
				".claude/agents/valid.md":     "---\nname: valid\ndescription: Valid\ntools: [Read, Grep]\n---\nValid.\n",
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses: [claude]
sources: {team: {git: '{{REG:sub-tool-malformed}}', ref: main}}
resources: {team: {include: [subagent/malformed, subagent/valid]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FileContains: map[string]string{
					"/root/.claude/agents/malformed.md": "tools:\n  - Read, Bash(git diff\n",
					"/root/.claude/agents/valid.md":     "tools:\n  - Read\n  - Grep\n",
				}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name: "subagent/effort_renamed_to_reasoning_effort",
		Desc: "Given a registry with a subagent using effort=high, When sync is run for factory, Then the droid file contains reasoningEffort: high and not effort:",
		Registries: []RegistryDef{
			{Name: "sub-effort", Subagents: []Subagent{
				{Name: "effort-agent", Model: "gpt-5.6-terra", Effort: "high", Tools: []string{"Read"}},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-effort}}', ref: main}}
resources: {team: {include: [subagent/effort-agent]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-disallowed}}', ref: main}}
resources: {team: {include: [subagent/disallowed-agent]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-isolation}}', ref: main}}
resources: {team: {include: [subagent/isolation-agent]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-remove}}', ref: main}}
resources: {team: {include: [subagent/removable-agent]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-from-entry-rm}}', ref: main}}
resources: {team: {include: [subagent/entry-agent]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		Desc: "Given factory and cursor harnesses configured with a tool-restricted subagent, When sync is run, Then Cursor fails closed on the unrepresentable restriction while the valid Factory sibling is materialized",
		Registries: []RegistryDef{
			{Name: "sub-multi", Subagents: []Subagent{
				{Name: "multi-agent", Model: "sonnet", Tools: []string{"Read", "Write"}},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - cursor
sources: {team: {git: '{{REG:sub-multi}}', ref: main}}
resources: {team: {include: [subagent/multi-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"subagent.cursor.tool-unknown"},
				FilesExist:  []string{"/root/.factory/droids/multi-agent.md"},
				FilesAbsent: []string{"/root/.cursor/agents/multi-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/multi-agent.md": "model: claude-sonnet-5",
				}},
			{FileContains: map[string]string{
				"/root/.factory/droids/multi-agent.md": "- Read",
			}},
			{FileContains: map[string]string{
				"/root/.factory/droids/multi-agent.md": "- Create",
			},
				FileNotContains: map[string]string{
					"/root/.factory/droids/multi-agent.md": "  - Edit",
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
		Desc: "Given a registry with a subagent using model=sonnet, effort=high, tools=[Read,Write], When sync is run for factory, Then all renormalizations apply: model mapped, effort→reasoningEffort, Write→Create",
		Registries: []RegistryDef{
			{Name: "sub-full", Subagents: []Subagent{
				{Name: "full-agent", Model: "sonnet", Tools: []string{"Read", "Write"}, Effort: "high"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:sub-full}}', ref: main}}
resources: {team: {include: [subagent/full-agent]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/droids/full-agent.md"},
				FileContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "model: claude-sonnet-5",
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
				"/root/.factory/droids/full-agent.md": "- Read",
			}},
			{FileContains: map[string]string{
				"/root/.factory/droids/full-agent.md": "- Create",
			},
				FileNotContains: map[string]string{
					"/root/.factory/droids/full-agent.md": "  - Edit",
				}},
			{FileNotContains: map[string]string{
				"/root/.factory/droids/full-agent.md": "  - Write",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-multi}}', ref: main}}
resources: {team: {include: [mcp/server-a, mcp/server-b]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:mcp-reg-a}}', ref: main}
  second: {git: '{{REG:mcp-reg-b}}', ref: main}
resources:
  first: {include: [mcp/server-alpha]}
  second: {include: [mcp/server-beta]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-preserve}}', ref: main}}
resources: {team: {include: [mcp/managed-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-roundtrip}}', ref: main}}
resources: {team: {include: [mcp/env-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-kind-filter}}', ref: main}}
`,
		Steps: []Step{
			{Args: []string{"install", "--kind", "mcp", "--scope", "user", "only-this-mcp"}, MustPass: true,
				OutContains: []string{"only-this-mcp"}},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist:  []string{"/root/.factory/mcp.json"},
				FilesAbsent: []string{"/root/.factory/skills/not-this-skill"}},
		},
	},
	// spec: resource-prerequisites/install-finds-an-unmet-prerequisite
	// spec: resource-prerequisites/commands-succeed-with-unmet-prerequisites
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite}}', ref: main}}
`,
		Steps: []Step{
			{
				Args:     []string{"install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite}}", "--ref", "main", "--scope", "user"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					`agent-marketplace install genai-atlassian --kind mcp`,
				},
				FilesExist: []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FileNotContains: map[string]string{
					"/root/.agents/config.yaml": "genai-atlassian",
				},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
		},
	},
	// spec: resource-prerequisites/prerequisite-is-explicitly-excluded
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite-excluded}}', ref: main}}
resources: {team: {exclude: [mcp/genai-atlassian]}}
`,
		Steps: []Step{
			{
				Args:     []string{"install", "smoke-test-kb", "--from", "{{REG:mcp-prerequisite-excluded}}", "--ref", "main", "--scope", "user"},
				MustPass: true,
				OutContains: []string{
					`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`,
					`agent-marketplace install genai-atlassian --kind mcp`,
				},
				FilesExist: []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FileContains: map[string]string{
					"/root/.agents/config.yaml": "genai-atlassian",
				},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
		},
	},
	// spec: resource-prerequisites/sync-reports-without-mutation
	// spec: resource-prerequisites/status-and-doctor-diagnostics
	// spec: resource-prerequisites/commands-succeed-with-unmet-prerequisites
	// spec: resource-prerequisites/strict-sync-rejects-unmet-prerequisites
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite-unselected}}', ref: main}}
resources: {team: {include: [skill/smoke-test-kb]}}
`,
		Steps: []Step{
			{
				Args:        []string{"sync"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`},
				FilesExist:  []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
				FilesAbsent: []string{"/root/.factory/mcp.json"},
			},
			{
				Args:        []string{"status"},
				MustPass:    true,
				OutContains: []string{"UNMET REQUIREMENTS", "skill:smoke-test-kb", "mcp:genai-atlassian", `agent-marketplace install genai-atlassian --kind mcp`},
			},
			{
				Args:        []string{"sync", "--check"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`},
			},
			{
				Args:           []string{"sync", "--strict"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`},
			},
			{
				Args:           []string{"sync", "--strict", "--check"},
				AssertExitCode: intPtr(1),
				OutContains:    []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`},
			},
			{
				Args:        []string{"doctor"},
				MustPass:    true,
				OutContains: []string{`warning: skill "smoke-test-kb" requires mcp "genai-atlassian", but it is not selected`, `agent-marketplace install genai-atlassian --kind mcp`},
			},
		},
	},
	// spec: resource-prerequisites/strict-sync-succeeds-when-satisfied
	{
		Name: "mcp/sync_strict_succeeds_when_prerequisite_selected",
		Desc: "Given a selected skill whose MCP prerequisite is also selected, When sync --strict runs, Then it emits no unmet-prerequisite warning and passes",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-satisfied",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"mcp:genai-atlassian"}},
				},
				MCPServers: []MCPServer{
					{Name: "genai-atlassian", Command: "npx", Args: []string{"-y", "@example/atlassian"}},
				},
			},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite-satisfied}}', ref: main}}
resources: {team: {include: [skill/smoke-test-kb, mcp/genai-atlassian]}}
`,
		Steps: []Step{
			{
				Args:           []string{"sync", "--strict"},
				MustPass:       true,
				OutNotContains: []string{"but it is not selected"},
				FilesExist:     []string{"/root/.factory/skills/smoke-test-kb/SKILL.md", "/root/.factory/mcp.json"},
			},
		},
	},
	// spec: resource-prerequisites/invalid-item-among-valid-items
	// spec: resource-prerequisites/non-list-requires-value
	{
		Name: "mcp/invalid_prerequisite_items_warn_in_inspection",
		Desc: "Given a loadable registry skill mixing a valid prerequisite with malformed items and another with a non-list requires, When list --from and doctor run, Then catalog.requires.invalid warnings name the resources and sources while the valid item still parses",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-invalid",
				Files: map[string]string{
					"skills/broken-prereq/SKILL.md": "---\nname: broken-prereq\ndescription: Mixed prerequisites\nmetadata:\n  requires:\n    - mcp:docs-server\n    - kind: all\n      name: everything\n    - kind: 42\n      name: secret-user:secret-token@example.com\n---\nbody\n",
					"skills/scalar-prereq/SKILL.md": "---\nname: scalar-prereq\ndescription: Non-list prerequisites\nmetadata:\n  requires: secret-user:secret-token@example.com\n---\nbody\n",
				},
				Skills: []Skill{{Name: "sibling"}},
			},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite-invalid}}', ref: main}}
resources: {team: {include: [skill/sibling]}}
`,
		Steps: []Step{
			{
				Args:           []string{"list", "--from", "{{REG:mcp-prerequisite-invalid}}", "--ref", "main"},
				MustPass:       true,
				OutContains:    []string{"catalog.requires.invalid", "broken-prereq", "scalar-prereq", "SKILL.md", "sibling"},
				OutNotContains: []string{"secret-token", "secret-user"},
			},
			{
				Args:       []string{"sync"},
				MustPass:   true,
				FilesExist: []string{"/root/.factory/skills/sibling/SKILL.md"},
			},
			{
				Args:           []string{"doctor"},
				MustPass:       true,
				OutContains:    []string{"catalog.requires.invalid", "broken-prereq", "scalar-prereq", "SKILL.md"},
				OutNotContains: []string{"secret-token", "secret-user"},
			},
		},
	},
	// spec: resource-prerequisites/malformed-item-stays-silent
	{
		Name: "mcp/sync_malformed_prerequisite_stays_silent",
		Desc: "Given a selected skill whose only prerequisites are malformed, When sync runs, Then the skill is materialized and no prerequisite warning is printed",
		Registries: []RegistryDef{
			{
				Name: "mcp-prerequisite-malformed",
				Skills: []Skill{
					{Name: "smoke-test-kb", Requires: []string{"weird:thing", `"mcp: "`, "collection:team-bundle"}},
				},
			},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:mcp-prerequisite-malformed}}', ref: main}}
resources: {team: {include: [skill/smoke-test-kb]}}
`,
		Steps: []Step{
			{
				Args:           []string{"sync"},
				MustPass:       true,
				OutNotContains: []string{"but it is not selected"},
				FilesExist:     []string{"/root/.factory/skills/smoke-test-kb/SKILL.md"},
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-all-kind}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-roundtrip-rm}}', ref: main}}
resources: {team: {include: [mcp/roundtrip-mcp]}}
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
		Desc: "Given all:true auto-selected MCP servers, When remove --scope user is run for one MCP server and re-synced, Then that server is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "mcp-default-rm", MCPServers: []MCPServer{
				{Name: "keep-mcp", Command: "npx", Args: []string{"-y", "@keep/mcp"}},
				{Name: "drop-mcp", Command: "node", Args: []string{"drop.js"}},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-default-rm}}', ref: main}}
resources: {team: {all: true}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:mcp-reg-a}}', ref: main}
  second: {git: '{{REG:mcp-reg-b}}', ref: main}
resources:
  first: {include: [mcp/shared-mcp]}
  second: {include: [mcp/shared-mcp]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-malformed}}', ref: main}}
resources: {team: {include: [skill/survivor-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:mcp-drift}}', ref: main}}
resources: {team: {include: [mcp/drift-mcp]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-tag-selection}}', ref: main}}
resources: {team: {tags: [k8s]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-install}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-kind-install}}', ref: main}}
resources: {team: {include: [command/deploy]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-scope-user}}', ref: main}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-scope-local}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-roundtrip}}', ref: main}}
resources: {team: {include: [command/roundtrip-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-from-rm}}', ref: main}}
resources: {team: {include: [command/cmd-a, command/cmd-b]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-required}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-required}}', ref: main}}
resources: {team: {exclude: [command/security-scan]}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-required}}', ref: main}}
resources: {team: {exclude: [command/security-scan]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{"/root/.factory/commands/security-scan.md"}},
		},
	},
	{
		Name: "command/default_true_auto_selects",
		Desc: "Given a registry with all:true containing commands, When sync is run, Then all commands are auto-selected and materialized",
		Registries: []RegistryDef{
			{Name: "cmd-default", Commands: []Command{
				{Name: "auto-cmd-a", Description: "Auto-selected A", Command: "echo a"},
				{Name: "auto-cmd-b", Description: "Auto-selected B", Command: "echo b"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-default}}', ref: main}}
resources: {team: {all: true}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources:
  first: {git: '{{REG:cmd-registry-a}}', ref: main}
  second: {git: '{{REG:cmd-registry-b}}', ref: main}
resources:
  first: {include: [command/deploy]}
  second: {include: [command/deploy]}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - claude
sources: {team: {git: '{{REG:cmd-multi-harness}}', ref: main}}
resources: {team: {include: [command/universal-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-drift}}', ref: main}}
resources: {team: {include: [command/drift-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-idem}}', ref: main}}
resources: {team: {include: [command/idem-cmd]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-precedence}}', ref: main}}
resources: {team: {include: [command/wanted-cmd, command/unwanted-cmd]}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-precedence}}', ref: main}}
resources: {team: {exclude: [command/unwanted-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - claude
sources: {team: {git: '{{REG:cmd-harness-rm}}', ref: main}}
resources: {team: {include: [command/prunable-cmd]}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.factory/commands/prunable-cmd.md",
					"/root/.claude/commands/prunable-cmd.md",
				}},
			{Action: ActionWriteGlobalConfig, Body: `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {team: {git: '{{REG:cmd-harness-rm}}', ref: main}}
resources: {team: {include: [command/prunable-cmd]}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
`,
		Steps: []Step{
			{Args: []string{ "install", "--scope", "repo",  "adhoc-cmd", "--from", "{{REG:cmd-adhoc}}", "--ref", "main", "--kind", "command"}, MustPass: true,
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-strict}}', ref: main}}
resources: {team: {include: [command/present-cmd, command/absent-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-lifecycle}}', ref: main}}
resources: {team: {include: [command/lifecycle-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-missing}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-required-rm}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true},
			{Args: []string{"remove", "mandatory-cmd"}, AssertExitCode: intPtr(1),
				OutContains: []string{"required"}},
		},
	},
	{
		Name: "command/remove_default_true_adds_exclude",
		Desc: "Given all:true auto-selected commands, When remove --scope user is run for one command and re-synced, Then that command is pruned while others remain",
		Registries: []RegistryDef{
			{Name: "cmd-default-rm", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-default-rm}}', ref: main}}
resources: {team: {all: true}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-from-entry-rm}}', ref: main}}
resources: {team: {include: [command/entry-cmd]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-repo-precedence}}', ref: main}}
resources: {team: {include: [command/cmd-a, command/cmd-b], exclude: [command/cmd-a]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-same-level}}', ref: main}}
resources: {team: {include: [command/included-cmd, command/conflicted-cmd], exclude: [command/conflicted-cmd]}}
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
apiVersion: agent-marketplace/v4
harnesses: [factory]
sources: {team: {git: '{{REG:cmd-org}}', ref: main}}
resources: {team: {all: true, required: true}}
`,
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-tag-install}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-all-kind}}', ref: main}}
`,
		Steps: []Step{
			{Args: []string{"install", "--all", "--kind", "command"}, MustPass: true,
				OutContains: []string{"only-this-cmd"}},
			{Args: []string{"sync"}, MustPass: true},
			// install --all --kind command creates a all:true entry;
			// verify the command materialized (skills may also appear due to all:true).
			{Args: []string{"status"}, MustPass: true,
				OutContains: []string{"only-this-cmd"}},
		},
	},
	{
		Name: "command/precedence_default_true_with_exclude",
		Desc: "Given an all:true registry with an explicit command exclude, When sync is run, Then excluded commands are skipped while others materialize",
		Registries: []RegistryDef{
			{Name: "cmd-default-exclude", Commands: []Command{
				{Name: "keep-cmd", Description: "Should stay", Command: "echo keep"},
				{Name: "drop-cmd", Description: "Should be excluded", Command: "echo drop"},
			}},
		},
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-default-exclude}}', ref: main}}
resources: {team: {all: true, exclude: [command/drop-cmd]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:cmd-skill-same-name}}', ref: main}}
resources: {team: {include: [skill/deploy, command/deploy]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:strict-reg}}', ref: main}}
resources: {team: {include: [skill/present-skill, skill/absent-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:idem-reg}}', ref: main}}
resources: {team: {include: [skill/idem-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:list-reg}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:kind-list}}', ref: main}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:doc-reg}}', ref: main}}
resources: {team: {include: [skill/doc-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:lifecycle-reg}}', ref: main}}
resources: {team: {include: [skill/lifecycle-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:merge-reg}}', ref: main}}
resources: {team: {include: [skill/global-skill]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:merge-reg}}', ref: main}}
resources: {team: {include: [skill/repo-skill]}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:merge-reg}}', ref: main}}
resources: {team: {include: [skill/local-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
allowed_registries:
  - internal.company.com
sources: {team: {git: '{{REG:blocked-reg}}', ref: main}}
`,
		Steps: []Step{
			{Args: []string{"sync"}, AssertExitCode: intPtr(1),
				OutContains: []string{"allowed_registries", "blocked"}},
		},
	},

	// ===== collections =====
	// spec: resource-selection/collection-keeps-required-all-selection
	{
		Name: "collection/required_org_survives_collection_selection",
		Desc: "Given an organization v4 group with all and required, When a repository v4 group selects a source-bound collection, Then machine outputs survive and the repository collection expands",
		Registries: []RegistryDef{
			{Name: "mandatory-collection", Skills: []Skill{{Name: "mandatory-alpha"}, {Name: "mandatory-beta"}}},
			{Name: "optional-collection", Skills: []Skill{{Name: "optional-member"}}, Files: map[string]string{
				"collections/shared/COLLECTION.yaml": "name: shared\nversion: '1.0.0'\nresources:\n  - name: optional-member\n",
			}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		OrgCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {mandatory: {git: '{{REG:mandatory-collection}}', ref: main}}\nresources: {mandatory: {all: true, required: true}}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {optional: {git: '{{REG:optional-collection}}', ref: main}}\nresources: {optional: {include: [collection/shared]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{
				"/root/.factory/skills/mandatory-alpha/SKILL.md",
				"/root/.factory/skills/mandatory-beta/SKILL.md",
				"/workspace/.factory/skills/optional-member/SKILL.md",
			}},
			{Args: []string{"sync", "--locked"}, MustPass: true, PathsUnchanged: []string{
				"/root/.factory/skills/mandatory-alpha",
				"/root/.factory/skills/mandatory-beta",
				"/workspace/.factory/skills/optional-member",
			}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:collection-reg}}', ref: main}}
resources: {team: {include: [collection/security]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:same-name-reg}}', ref: main}}
resources: {team: {include: [collection/dev]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:bad-kind-reg}}', ref: main}}
resources: {team: {include: [collection/bad]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {personal: {git: '{{REG:first}}', ref: main}}
resources: {personal: {all: true}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {local: {git: '{{REG:second}}', ref: main}}
resources: {local: {all: true}}
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
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:exclude-reg}}', ref: main}}
resources: {team: {all: true}}
`,
		LocalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:exclude-reg}}', ref: main}}
resources: {team: {exclude: [skill/dropped]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - codex
sources: {}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:codex-repo}}', ref: main, source_harness: claude}}
resources: {team: {include: [skill/repo-skill, command/repo-command, subagent/repo-agent, hook/PreToolUse-command, mcp/zeta-server, mcp/alpha-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - codex
sources: {}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:codex-collisions}}', ref: main, source_harness: claude}}
resources: {team: {include: [hook/PreToolUse-command, mcp/collision-server]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - codex
  - factory
sources: {team: {git: '{{REG:codex-machine}}', ref: main, source_harness: claude}}
resources: {team: {harnesses: [codex, factory], include: [skill/machine-skill, subagent/machine-agent, hook/PreToolUse-command, mcp/machine-mcp]}}
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
					"/root/.factory/hooks.json",
					"/root/.factory/mcp.json",
				},
				FileContains: map[string]string{
					"/root/.codex/agents/machine-agent.toml": "name = 'machine-agent'",
					"/root/.codex/hooks.json":                "echo machine-hook",
					"/root/.codex/config.toml":               "machine-mcp-bin",
					"/root/.factory/droids/machine-agent.md": "name: machine-agent",
					"/root/.factory/hooks.json":              "echo machine-hook",
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
			{Action: ActionWriteGlobalConfig, Body: `apiVersion: agent-marketplace/v4
harnesses:
  - codex
  - factory
sources: {}
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
					"/root/.codex/hooks.json":   "echo machine-hook",
					"/root/.codex/config.toml":  "machine-mcp-bin",
					"/root/.factory/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":   "machine-mcp-bin",
				}},
			{Action: ActionWriteGlobalConfig, Body: `apiVersion: agent-marketplace/v4
harnesses:
  - codex
  - factory
sources: {team: {git: '{{REG:codex-machine}}', ref: main, source_harness: claude}}
resources: {team: {harnesses: [codex, factory], include: [skill/machine-skill, subagent/machine-agent, hook/PreToolUse-command, mcp/machine-mcp]}}
`},
			{Args: []string{"sync"}, MustPass: true,
				FilesExist: []string{
					"/root/.agents/skills/machine-skill/SKILL.md",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.codex/hooks.json",
					"/root/.codex/config.toml",
					"/root/.factory/skills/machine-skill/SKILL.md",
					"/root/.factory/droids/machine-agent.md",
					"/root/.factory/hooks.json",
					"/root/.factory/mcp.json",
				},
				FileContains: map[string]string{
					"/root/.codex/hooks.json":   "echo machine-hook",
					"/root/.codex/config.toml":  "machine-mcp-bin",
					"/root/.factory/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":   "machine-mcp-bin",
				}},
			{Args: []string{"deinit"}, MustPass: true,
				FilesAbsent: []string{
					"/root/.agents/skills/machine-skill",
					"/root/.codex/agents/machine-agent.toml",
					"/root/.factory/skills/machine-skill",
					"/root/.factory/droids/machine-agent.md",
				},
				FileNotContains: map[string]string{
					"/root/.codex/hooks.json":   "echo machine-hook",
					"/root/.codex/config.toml":  "machine-mcp-bin",
					"/root/.factory/hooks.json": "echo machine-hook",
					"/root/.factory/mcp.json":   "machine-mcp-bin",
				}},
		},
	},

	// ===== deinit command =====
	{
		Name:       "deinit/windsurf_manifest_cannot_nominate_repo_files",
		Desc:       "Given actual portable Windsurf output and a manifest nominating an unrelated repository file, When sync checking or config-purging deinit runs, Then refusal preserves repository bytes and metadata until the manifest is repaired and real native teardown succeeds",
		Registries: []RegistryDef{{Name: "windsurf-manifest-scope", Skills: []Skill{{Name: "teardown-skill", Description: "Scoped native control"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [windsurf]\nsources:\n  fixture:\n    git: {{REG:windsurf-manifest-scope}}\n    ref: main\nresources:\n  fixture: [skill/teardown-skill]\nmaterialization:\n  mode: file\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, RegularFiles: []string{"/workspace/.windsurf/skills/teardown-skill/SKILL.md"}},
			{Args: []string{"deinit", "--scope", "repo"}, MustPass: true, FilesAbsent: []string{"/workspace/.windsurf/skills/teardown-skill/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/README.md", Body: "body"},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: fmt.Sprintf("version: 1\nentries:\n  - target: windsurf\n    path: README.md\n    digest: sha256:%x\n", sha256.Sum256([]byte("file\x00\x000644\x004\x00body")))},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, FilesAbsent: []string{"/workspace/.windsurf"}, PathsUnchanged: []string{"/workspace/README.md", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config", "--dry-run"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Would remove config", "Would remove lock", "No managed artifacts to remove"}, FilesAbsent: []string{"/workspace/.windsurf"}, PathsUnchanged: []string{"/workspace/README.md", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Removed config", "Removed lock", "No managed artifacts to remove"}, FilesAbsent: []string{"/workspace/.windsurf"}, PathsUnchanged: []string{"/workspace/README.md", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: "version: 1\nentries: []\n", Args: []string{"sync"}, MustPass: true, RegularFiles: []string{"/workspace/.windsurf/skills/teardown-skill/SKILL.md"}, PathsUnchanged: []string{"/workspace/README.md"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, MustPass: true, FilesAbsent: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.windsurf/skills/teardown-skill/SKILL.md"}, PathsUnchanged: []string{"/workspace/README.md"}},
		},
	},
	{
		Name:       "deinit/portable_manifest_cannot_nominate_shared_hooks_file",
		Desc:       "Given actual portable Factory output and a manifest nominating the handwritten shared hooks file inside the hooks component, When sync checking or config-purging deinit runs, Then refusal preserves the hooks file bytes and metadata until the manifest is repaired and real native teardown succeeds",
		Registries: []RegistryDef{{Name: "factory-manifest-scope", Skills: []Skill{{Name: "teardown-skill", Description: "Scoped native control"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  fixture:\n    git: {{REG:factory-manifest-scope}}\n    ref: main\nresources:\n  fixture: [skill/teardown-skill]\nmaterialization:\n  mode: file\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, RegularFiles: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md"}},
			{Args: []string{"deinit", "--scope", "repo"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md"}},
			{Action: ActionWriteFile, Path: "/workspace/.factory/hooks/hooks.json", Body: handwrittenFactoryHooks},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: fmt.Sprintf("version: 1\nentries:\n  - target: factory\n    path: hooks/hooks.json\n    digest: sha256:%x\n", sha256.Sum256(fmt.Appendf(nil, "file\x00\x000644\x00%d\x00%s", len(handwrittenFactoryHooks), handwrittenFactoryHooks)))},
			{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, FilesAbsent: []string{"/workspace/.factory/skills/teardown-skill"}, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config", "--dry-run"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Would remove config", "Would remove lock", "No managed artifacts to remove"}, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, AssertExitCode: intPtr(1), OutContains: []string{"materialize.manifest.error"}, OutNotContains: []string{"Removed config", "Removed lock", "No managed artifacts to remove"}, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json", "/workspace/.agents/materialization-manifest.yaml", "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}},
			{Action: ActionWriteFile, Path: "/workspace/.agents/materialization-manifest.yaml", Body: "version: 1\nentries: []\n", Args: []string{"sync"}, MustPass: true, RegularFiles: []string{"/workspace/.factory/skills/teardown-skill/SKILL.md"}, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json"}},
			{Args: []string{"deinit", "--scope", "repo", "--purge-config"}, MustPass: true, FilesAbsent: []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.factory/skills/teardown-skill/SKILL.md"}, PathsUnchanged: []string{"/workspace/.factory/hooks/hooks.json"}},
		},
	},
	{
		Name:       "deinit/manifest_failure_preserves_config_and_output",
		Desc:       "Given installed output and an invalid manifest, When deinit attempts config purge, Then it reports failure and preserves state until the manifest is repaired",
		Registries: []RegistryDef{{Name: "deinit-invalid-manifest", Skills: []Skill{{Name: "teardown-skill", Description: "Deinit preservation"}}}},
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:deinit-invalid-manifest}}', ref: main}}\nresources: {team: {include: [skill/teardown-skill]}}\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:deinit-reg}}', ref: main}}
resources: {team: {include: [skill/teardown-skill]}}
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:deinit-reg}}', ref: main}}
resources: {team: {include: [skill/teardown-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:deinit-cache-reg}}', ref: main}}
resources: {team: {include: [skill/cached-skill]}}
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
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:deinit-dry-reg}}', ref: main}}
resources: {team: {include: [skill/dry-skill]}}
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
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:deinit-purge-reg}}', ref: main}}
resources: {team: {include: [skill/purge-skill]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - devin
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:devin-reg}}', ref: main}}
resources: {team: {include: [skill/devin-skill, command/devin-command, subagent/devin-agent, hook/PostToolUse-command, mcp/devin-mcp]}}
`,
		Steps: []Step{
			{Action: ActionWriteFile, Path: "/workspace/.devin/hooks.v1.json",
				Body: "{\n  \"PostToolUse\": [{\"hooks\":[{\"type\":\"command\",\"command\":\"echo handwritten-hook\"}]}]\n}\n"},
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
			{Action: ActionWriteLocalConfig, Body: "apiVersion: agent-marketplace/v4\nmaterialization:\n  mode: file\n",
				Args: []string{"sync"}, MustPass: true,
				RegularFiles:   []string{"/workspace/.devin/skills/devin-skill/SKILL.md", "/workspace/.devin/skills/devin-command/SKILL.md", "/workspace/.devin/agents/devin-agent.md"},
				PathsUnchanged: []string{"/workspace/.devin/mcp_config.json", "/workspace/.devin/hooks.v1.json"}},
			{Args: []string{"sync", "--check"}, MustPass: true,
				PathsUnchanged: []string{"/workspace/.devin"}},
			{Action: ActionWriteLocalConfig, Body: "apiVersion: agent-marketplace/v4\nmaterialization:\n  mode: symlink\n",
				Args: []string{"sync"}, MustPass: true,
				Symlinks:       []string{"/workspace/.devin/skills/devin-skill", "/workspace/.devin/skills/devin-command", "/workspace/.devin/agents/devin-agent.md"},
				PathsUnchanged: []string{"/workspace/.devin/mcp_config.json", "/workspace/.devin/hooks.v1.json"}},
			{Action: ActionWriteLocalConfig, Body: "apiVersion: agent-marketplace/v4\nmaterialization:\n  mode: file\n",
				Args: []string{"sync"}, MustPass: true},
			{Action: ActionWriteRepoConfig, Body: "apiVersion: agent-marketplace/v4\nsources: {}\n",
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - devin
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:devin-scoped-reg}}', ref: main}}
resources: {team: {include: [skill/openspec-apply-change, subagent/scoped-agent]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - devin
materialization:
  mode: file
sources: {team: {git: '{{REG:devin-machine-reg}}', ref: main}}
resources: {team: {include: [skill/machine-skill, command/machine-command, subagent/machine-agent, mcp/machine-mcp]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - devin
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:devin-doctor-reg}}', ref: main}}
resources: {team: {include: [mcp/devin-doctor-mcp]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
harnesses:
  - devin
`,
		RepoCfg: `apiVersion: agent-marketplace/v4
sources: {team: {git: '{{REG:devin-conflict-reg}}', ref: main}}
resources: {team: {include: [mcp/genai-atlassian]}}
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
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
		GlobalCfg: `apiVersion: agent-marketplace/v4
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
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:bare-bundle-mcp}}', ref: main}}\nresources: {team: {all: true}}\n",
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
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:unconsumed-container}}', ref: main}}\nresources: {team: {all: true}}\n",
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
		RepoCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:unconsumed-config}}', ref: main, source_harness: claude}}\nresources: {team: {all: true}}\n",
		Steps: []Step{
			{Args: []string{"lock"}, MustPass: true},
			{Args: []string{"list"}, MustPass: true, OutContains: []string{"ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"}},
			{Args: []string{"sync"}, MustPass: true, FileContains: map[string]string{"/workspace/.factory/mcp.json": "native"}, FileNotContains: map[string]string{"/workspace/.factory/mcp.json": "foreign"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"ingest.claude.unconsumed-config", "mcp.json", "gemini-extension.json", "For Claude plugins, rename"}},
		},
	},
	{
		Name:       "v4_user/inspect_lock_and_backup",
		Desc:       "Given a v4 user config that selects a Git skill, When status, doctor, lock, and backup run, Then each succeeds against the v4 layer, backup restore recovers a changed user-owned skill file, and the workspace stays in sync",
		Registries: []RegistryDef{{Name: "v4-user-ops", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '{{REG:v4-user-ops}}', ref: main}\nresources:\n  tools: {include: [skill/review]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/root/.factory/skills/review/SKILL.md", "/root/.agents/lock.yaml"}},
			{Args: []string{"status"}, MustPass: true, OutContains: []string{"SYNCED RESOURCES", "review", "USER"}},
			{Args: []string{"doctor"}, MustPass: true, OutContains: []string{"agent-marketplace doctor"}},
			{Args: []string{"lock", "--scope", "user"}, MustPass: true, OutContains: []string{"Locks: 0 changed"}},
			{Args: []string{"lock", "--check"}, MustPass: true},
			{Action: ActionWriteFile, Path: "/root/.factory/skills/handwritten/SKILL.md", Body: "backed up\n", Args: []string{"backup", "create"}, MustPass: true, OutContains: []string{"Backup created for: factory"}},
			{Args: []string{"backup", "list"}, MustPass: true, OutContains: []string{"factory (1)"}},
			{Action: ActionWriteFile, Path: "/root/.factory/skills/handwritten/SKILL.md", Body: "changed after backup\n", Args: []string{"backup", "restore", "--target", "factory"}, MustPass: true, FileContains: map[string]string{"/root/.factory/skills/handwritten/SKILL.md": "backed up"}, FilesExist: []string{"/root/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "v4_user/gc_and_deinit",
		Desc:       "Given a v4 user config that selects a Git skill, When deinit runs, the source is dropped, and gc runs, Then deinit keeps the config, gc removes only the orphaned cache, and deinit --purge-config removes the v4 config and its lock",
		Registries: []RegistryDef{{Name: "v4-user-gc", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '{{REG:v4-user-gc}}', ref: main}\nresources:\n  tools: {include: [skill/review]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/root/.factory/skills/review/SKILL.md"}},
			{Args: []string{"gc", "--dry-run"}, MustPass: true, OutContains: []string{"Would remove 0 cache entries"}},
			{Args: []string{"deinit", "--dry-run"}, MustPass: true, OutContains: []string{"Would remove 1 managed artifact(s)"}, FilesExist: []string{"/root/.factory/skills/review/SKILL.md"}},
			{Args: []string{"deinit"}, MustPass: true, OutContains: []string{"Removed 1 managed artifact(s)"}, FilesAbsent: []string{"/root/.factory/skills/review"}, FilesExist: []string{"/root/.agents/config.yaml"}},
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/root/.factory/skills/review/SKILL.md"}},
			{Action: ActionWriteGlobalConfig, Body: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n", Args: []string{"sync"}, MustPass: true, FilesAbsent: []string{"/root/.factory/skills/review"}},
			{Args: []string{"gc", "--dry-run"}, MustPass: true, OutNotContains: []string{"Would remove 0 cache entries"}},
			{Args: []string{"gc"}, MustPass: true, OutNotContains: []string{"Removed 0 cache entries"}},
			{Args: []string{"gc", "--dry-run"}, MustPass: true, OutContains: []string{"Would remove 0 cache entries"}, FilesExist: []string{"/root/.agents/config.yaml", "/root/.agents/lock.yaml"}},
			{Args: []string{"deinit", "--purge-config"}, MustPass: true, FilesAbsent: []string{"/root/.agents/config.yaml", "/root/.agents/lock.yaml"}},
		},
	},
	{
		Name:      "cli/help_and_completion",
		Desc:      "Given any configuration, When help and every completion script are requested, Then each prints its generated text",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		Steps: []Step{
			{Args: []string{"help"}, MustPass: true, OutContains: []string{"Usage:", "Available Commands:"}},
			{Args: []string{"completion"}, MustPass: true, OutContains: []string{"Usage:", "bash"}},
			{Args: []string{"completion", "bash"}, MustPass: true, OutContains: []string{"bash completion"}},
			{Args: []string{"completion", "zsh"}, MustPass: true, OutContains: []string{"#compdef agent-marketplace"}},
			{Args: []string{"completion", "fish"}, MustPass: true, OutContains: []string{"complete -c agent-marketplace"}},
			{Args: []string{"completion", "powershell"}, MustPass: true, OutContains: []string{"Register-ArgumentCompleter"}},
		},
	},
	{
		Name:      "v4_user/telemetry_preferences",
		Desc:      "Given a v4 user config, When telemetry is inspected, enabled with an endpoint, previewed, reset, purged, and disabled, Then each succeeds and the user config stays v4",
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		Steps: []Step{
			{Args: []string{"telemetry"}, MustPass: true, OutContains: []string{"Usage:"}},
			{Args: []string{"telemetry", "status"}, MustPass: true, OutContains: []string{"Effective level"}},
			{Args: []string{"telemetry", "enable", "--level", "basic", "--endpoint", "http://127.0.0.1:9"}, MustPass: true, FileContains: map[string]string{"/root/.agents/config.yaml": "endpoint: http://127.0.0.1:9"}},
			{Args: []string{"telemetry", "status"}, MustPass: true, OutContains: []string{"basic"}},
			{Args: []string{"telemetry", "preview", "--level", "basic"}, MustPass: true},
			{Args: []string{"telemetry", "reset-id"}, MustPass: true},
			{Args: []string{"telemetry", "purge"}, MustPass: true},
			{Args: []string{"telemetry", "disable"}, MustPass: true, FileContains: map[string]string{"/root/.agents/config.yaml": "apiVersion: agent-marketplace/v4"}, FileNotContains: map[string]string{"/root/.agents/config.yaml": "level: basic"}},
			{Args: []string{"backup"}, MustPass: true, OutContains: []string{"Usage:"}},
			{Args: []string{"init", "--harnesses", "factory"}, MustPass: true, OutContains: []string{"already initialized"}, PathsUnchanged: []string{"/root/.agents/config.yaml"}},
		},
	},
	{
		Name:       "v4_user/populated_telemetry_preferences_and_backup",
		Desc:       "Given a v4 user config that selects a Git skill, When telemetry is managed and a backup is created, listed, and restored, Then each succeeds, the config stays v4, and restore recovers a changed user-owned skill file",
		Registries: []RegistryDef{{Name: "v4-user-backup", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:v4-user-backup}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, FilesExist: []string{"/root/.factory/skills/review/SKILL.md"}},
			{Args: []string{"telemetry"}, MustPass: true, OutContains: []string{"Usage:"}},
			{Args: []string{"telemetry", "status"}, MustPass: true, OutContains: []string{"Effective level"}},
			{Args: []string{"telemetry", "enable", "--level", "basic", "--endpoint", "http://127.0.0.1:9"}, MustPass: true, FileContains: map[string]string{"/root/.agents/config.yaml": "endpoint: http://127.0.0.1:9"}},
			{Args: []string{"telemetry", "preview", "--level", "basic"}, MustPass: true},
			{Args: []string{"telemetry", "reset-id"}, MustPass: true},
			{Args: []string{"telemetry", "purge"}, MustPass: true},
			{Args: []string{"telemetry", "disable"}, MustPass: true, FileContains: map[string]string{"/root/.agents/config.yaml": "apiVersion: agent-marketplace/v4"}, FileNotContains: map[string]string{"/root/.agents/config.yaml": "level: basic"}},
			{Args: []string{"backup"}, MustPass: true, OutContains: []string{"Usage:"}},
			{Action: ActionWriteFile, Path: "/root/.factory/skills/handwritten/SKILL.md", Body: "backed up\n", Args: []string{"backup", "create"}, MustPass: true, OutContains: []string{"Backup created for: factory"}},
			{Args: []string{"backup", "list"}, MustPass: true, OutContains: []string{"factory (1)"}},
			{Action: ActionWriteFile, Path: "/root/.factory/skills/handwritten/SKILL.md", Body: "changed after backup\n", Args: []string{"backup", "restore", "--target", "factory"}, MustPass: true, FileContains: map[string]string{"/root/.factory/skills/handwritten/SKILL.md": "backed up"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "v4_repo/install_selectors_list_and_maintenance_flags",
		Desc:       "Given a v4 user config and a v4 repository config that declares a Git source with nothing selected, When listing the registry ad hoc and installing by tag, by kind with --all and --registry, and by name with --kind and --verbose, Then every selection lands in the v4 repository config, and the sync and deinit flags succeed against it",
		Registries: []RegistryDef{{Name: "v4-selectors", Skills: []Skill{{Name: "review", Description: "Review changes", Tags: []string{"k8s"}}, {Name: "deploy", Description: "Deploy"}}, Commands: []Command{{Name: "cmd-one", Description: "Command one", Command: "echo one"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '{{REG:v4-selectors}}', ref: main}\n",
		Steps: []Step{
			{Args: []string{"list", "--from", "{{REG:v4-selectors}}", "--ref", "main", "--path", "skills", "--kind", "skill"}, MustPass: true, OutContains: []string{"review", "deploy"}, OutNotContains: []string{"cmd-one"}},
			{Args: []string{"install", "--tag", "k8s", "--scope", "repo"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FilesAbsent: []string{"/workspace/.factory/skills/deploy"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "k8s"}},
			{Args: []string{"install", "--all", "--kind", "command", "--registry", "{{REG:v4-selectors}}", "--scope", "repo"}, MustPass: true, FilesExist: []string{"/workspace/.factory/commands/cmd-one.md"}},
			{Args: []string{"install", "deploy", "--kind", "skill", "--verbose", "--scope", "repo"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/deploy/SKILL.md"}},
			{Args: []string{"list", "--kind", "command"}, MustPass: true, OutContains: []string{"cmd-one"}, OutNotContains: []string{"deploy"}},
			{Args: []string{"sync", "--verbose", "--no-backup"}, MustPass: true},
			{Args: []string{"sync", "--check"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v4"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "registries:"}},
			{Args: []string{"deinit", "--scope", "repo", "--dry-run"}, MustPass: true, OutContains: []string{"Would remove"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"deinit", "--scope", "repo"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/review"}, FilesExist: []string{"/workspace/.agents/config.yaml"}},
		},
	},
	{
		Name: "v4_repo/adhoc_path_and_registry_flags",
		Desc: "Given a v4 user and repository config, When installing everything from one configured registry and installing, listing, and removing a resource from an ad hoc registry path, Then each step succeeds and the repository config stays v4",
		Registries: []RegistryDef{
			{Name: "v4-flags", Skills: []Skill{{Name: "review", Description: "Review changes"}, {Name: "deploy", Description: "Deploy"}}},
			{Name: "v4-adhoc", Skills: []Skill{{Name: "adhoc-skill", Description: "Ad hoc skill"}}},
		},
		GlobalCfg: "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:   "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:v4-flags}}', ref: main}}\n",
		Steps: []Step{
			{Args: []string{"install", "--all", "--registry", "{{REG:v4-flags}}", "--scope", "repo"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md", "/workspace/.factory/skills/deploy/SKILL.md"}},
			{Args: []string{"list", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills"}, MustPass: true, OutContains: []string{"adhoc-skill"}},
			{Args: []string{"install", "adhoc-skill", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills", "--scope", "repo"}, MustPass: true, FilesExist: []string{"/workspace/.factory/skills/adhoc-skill/SKILL.md"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "path: skills"}},
			{Args: []string{"remove", "adhoc-skill", "--from", "{{REG:v4-adhoc}}", "--ref", "main", "--path", "skills"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/adhoc-skill"}},
			{Args: []string{"sync", "--verbose"}, MustPass: true, FilesAbsent: []string{"/workspace/.factory/skills/adhoc-skill"}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}},
			{Args: []string{"sync", "--check"}, MustPass: true, FileContains: map[string]string{"/workspace/.agents/config.yaml": "apiVersion: agent-marketplace/v4"}},
		},
	},
	{
		Name:       "v4_repo/populated_prune_stale_strict",
		Desc:       "Given a v4 repository config that selects a skill the registry no longer has, When syncing with --prune-stale --strict --verbose, Then the stale selection is pruned from the config and the remaining skill materializes",
		Registries: []RegistryDef{{Name: "v4-prune", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {}\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:v4-prune}}', ref: main}}\nresources: {team: {include: [skill/review, skill/deleted-skill]}}\n",
		Steps: []Step{
			{Args: []string{"sync", "--prune-stale", "--strict", "--verbose"}, MustPass: true, OutContains: []string{`pruned stale skill "deleted-skill"`}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "review"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "deleted-skill"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "v4_repo/prune_stale_strict",
		Desc:       "Given a v4 repository config that selects a skill the registry no longer has, When syncing with --prune-stale --strict --verbose, Then the stale selection is pruned from the config and the remaining skill materializes",
		Registries: []RegistryDef{{Name: "v4-prune", Skills: []Skill{{Name: "review", Description: "Review changes"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '{{REG:v4-prune}}', ref: main}\nresources:\n  tools: {include: [skill/review, skill/deleted-skill]}\n",
		Steps: []Step{
			{Args: []string{"sync", "--prune-stale", "--strict", "--verbose"}, MustPass: true, OutContains: []string{`pruned stale skill "deleted-skill"`}, FilesExist: []string{"/workspace/.factory/skills/review/SKILL.md"}, FileContains: map[string]string{"/workspace/.agents/config.yaml": "skill/review"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": "deleted-skill"}},
			{Args: []string{"sync", "--check"}, MustPass: true},
		},
	},
	{
		Name:       "update/verbose_registry_summary",
		Desc:       "Given an unchanged Git registry, When updating with verbose output, Then the CLI reports the configured entry without claiming its revision changed",
		Registries: []RegistryDef{{Name: "update-output", Skills: []Skill{{Name: "review"}}}},
		RepoCfg:    "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '{{REG:update-output}}', ref: main}}\nresources: {team: {include: [skill/review]}}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/review"}},
			{Args: []string{"update", "--verbose"}, MustPass: true, OutContains: []string{"Configured registry entries: 1", "Marketplace pins updated: 0"}, OutNotContains: []string{"Organization-managed Marketplace pins:"}, Symlinks: []string{"/workspace/.factory/skills/review"}},
		},
	},
	{
		Name:       "update/verbose_registry_summary_v4",
		Desc:       "Given an unchanged Git source in v4 user and repository configs, When updating with verbose output, Then the CLI reports the configured entry without claiming its revision changed",
		Registries: []RegistryDef{{Name: "update-output-v4", Skills: []Skill{{Name: "review"}}}},
		GlobalCfg:  "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n",
		RepoCfg:    "apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '{{REG:update-output-v4}}', ref: main}\nresources:\n  tools: {include: [skill/review]}\n",
		Steps: []Step{
			{Args: []string{"sync"}, MustPass: true, Symlinks: []string{"/workspace/.factory/skills/review"}},
			{Args: []string{"update", "--verbose"}, MustPass: true, OutContains: []string{"Configured registry entries: 1", "Marketplace pins updated: 0"}, OutNotContains: []string{"Organization-managed Marketplace pins:"}, Symlinks: []string{"/workspace/.factory/skills/review"}},
		},
	},
}

func init() {
	// spec: resource-installation/bulk-selectors-install-into-each-writable-layer
	for _, scope := range []string{"user", "local"} {
		for _, selector := range []string{"tag", "all"} {
			for _, sourceScope := range []string{"repo", "user"} {
				globalCfg := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
				repoCfg := "apiVersion: agent-marketplace/v4\nsources: {team: {git: '{{REG:bulk-scopes}}', ref: main}}\n"
				unchanged := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml"}
				if sourceScope == "user" {
					globalCfg += "sources: {team: {git: '{{REG:bulk-scopes}}', ref: main}}\n"
					repoCfg, unchanged = "", nil
					if scope == "local" {
						unchanged = []string{"/root/.agents/config.yaml", "/root/.agents/lock.yaml"}
					}
				}
				targetConfig, targetLock, outputRoot, otherRoot, label := "/root/.agents/config.yaml", "/root/.agents/lock.yaml", "/root", "/workspace", "user-wide"
				if scope == "local" {
					targetConfig, targetLock, outputRoot, otherRoot, label = "/workspace/.agents/config.local.yaml", "/workspace/.agents/lock.local.yaml", "/workspace", "/root", "locally"
				}
				args := []string{"install", "--scope", scope}
				selection := "ops"
				absent := []string{otherRoot + "/.factory/skills/review"}
				if sourceScope == "user" {
					absent = append(absent, "/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml")
				}
				if selector == "tag" {
					args = append(args, "--tag", "ops")
					absent = append(absent, outputRoot+"/.factory/skills/unselected")
				} else {
					args = append(args, "--all", "--kind", "skill", "--registry", "{{REG:bulk-scopes}}")
					selection = "skill/review"
				}
				testTable = append(testTable, TestCase{
					Name: "install/bulk_scope_" + scope + "_" + selector + "_from_" + sourceScope,
					Desc: "Given a v4 " + sourceScope + " source, When bulk installing into " + scope + " scope, Then the requested config, lock and native output change without editing other layers",
					Registries: []RegistryDef{{Name: "bulk-scopes", Skills: []Skill{
						{Name: "review", Description: "Scoped review", Tags: []string{"ops"}},
						{Name: "unselected", Description: "Untagged skill"},
					}}},
					GlobalCfg: globalCfg,
					RepoCfg:   repoCfg,
					Steps: []Step{
						{Args: []string{"lock", "--scope", sourceScope}, MustPass: true},
						{Args: args, MustPass: true, OutContains: []string{label},
							PathsUnchanged: unchanged,
							FileContains:   map[string]string{targetConfig: selection, targetLock: "scope: " + scope, outputRoot + "/.factory/skills/review/SKILL.md": "Scoped review"},
							FilesAbsent:    absent},
						{Args: []string{"sync", "--check"}, MustPass: true},
					},
				})
			}
		}
	}
	for _, version := range []string{"agent-marketplace/v4"} {
		for _, prerequisite := range []struct {
			kind, name, sourcePath, source, output, code string
		}{
			{"mcp", "child", ".mcp.json", `{"mcpServers":{"child":{"command":"echo","args":["inert-child"],"oauthClientId":"fixture-client"}}}`, "/workspace/.codex/config.toml", "mcp.codex.security-unsupported"},
			{"hook", "PreToolUse-prompt", "hooks/hooks.json", `{"hooks":{"PreToolUse":[{"type":"prompt","prompt":"inert advisory prompt","matcher":"Bash"}]}}`, "/workspace/.codex/hooks.json", "hook.prompt.unsupported"},
		} {
			config := func(selected bool) string {
				include := "skill/parent"
				if selected {
					include += ", " + prerequisite.kind + "/" + prerequisite.name
				}
				return "apiVersion: " + version + "\nharnesses: [codex]\nsources:\n  tools: {git: '{{REG:advisory}}', ref: main}\nresources:\n  tools: {include: [" + include + "]}\n"
			}
			warning := `warning: skill "parent" requires ` + prerequisite.kind + ` "` + prerequisite.name + `", but it is not selected`
			unchanged := []string{"/workspace/.agents/config.yaml", "/workspace/.agents/lock.yaml", "/workspace/.agents/skills/parent"}
			testTable = append(testTable, TestCase{
				Name:       "requires/advisory_unsupported_" + prerequisite.kind + "/" + strings.TrimPrefix(version, "agent-marketplace/"),
				Desc:       "Given an unselected prerequisite that Codex cannot represent, When ordinary, locked and check synchronization run, Then the parent installs without compilation errors or prerequisite output; explicitly selecting the prerequisite exercises the refusal",
				Registries: []RegistryDef{{Name: "advisory", Skills: []Skill{{Name: "parent", Requires: []string{prerequisite.kind + ":" + prerequisite.name}}}, Files: map[string]string{prerequisite.sourcePath: prerequisite.source}}},
				GlobalCfg:  "apiVersion: " + version + "\nharnesses: [codex]\n",
				RepoCfg:    config(false),
				Steps: []Step{
					{Args: []string{"sync"}, MustPass: true, OutContains: []string{warning}, OutNotContains: []string{prerequisite.code}, FileContains: map[string]string{"/workspace/.agents/skills/parent/SKILL.md": "# parent"}, FileNotContains: map[string]string{"/workspace/.agents/config.yaml": prerequisite.name}, FilesAbsent: []string{prerequisite.output}},
					{Args: []string{"sync"}, MustPass: true, OutContains: []string{warning}, OutNotContains: []string{prerequisite.code}, PathsUnchanged: unchanged, FilesAbsent: []string{prerequisite.output}},
					{Args: []string{"sync", "--locked"}, MustPass: true, OutContains: []string{warning}, OutNotContains: []string{prerequisite.code}, PathsUnchanged: unchanged, FilesAbsent: []string{prerequisite.output}},
					{Args: []string{"sync", "--check"}, MustPass: true, OutContains: []string{warning}, OutNotContains: []string{prerequisite.code}, PathsUnchanged: unchanged, FilesAbsent: []string{prerequisite.output}},
					{Args: []string{"sync", "--strict", "--check"}, AssertExitCode: intPtr(1), OutContains: []string{warning}, OutNotContains: []string{prerequisite.code}, PathsUnchanged: unchanged, FilesAbsent: []string{prerequisite.output}},
					{Action: ActionWriteRepoConfig, Body: config(true), Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{prerequisite.code}, FilesAbsent: []string{prerequisite.output}, FileContains: map[string]string{"/workspace/.agents/skills/parent/SKILL.md": "# parent"}},
					{Args: []string{"sync", "--locked"}, AssertExitCode: intPtr(1), OutContains: []string{prerequisite.code}, FilesAbsent: []string{prerequisite.output}, PathsUnchanged: unchanged, FileContains: map[string]string{"/workspace/.agents/skills/parent/SKILL.md": "# parent"}},
					{Args: []string{"sync", "--check"}, AssertExitCode: intPtr(1), OutContains: []string{prerequisite.code}, FilesAbsent: []string{prerequisite.output}, PathsUnchanged: unchanged, FileContains: map[string]string{"/workspace/.agents/skills/parent/SKILL.md": "# parent"}},
					{Args: []string{"sync", "--strict", "--check"}, AssertExitCode: intPtr(1), OutContains: []string{prerequisite.code}, FilesAbsent: []string{prerequisite.output}, PathsUnchanged: unchanged, FileContains: map[string]string{"/workspace/.agents/skills/parent/SKILL.md": "# parent"}},
				},
			})
		}
	}
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

			// Write authored config layers after resolving fixture URLs.
			// Cases that omit the user fixture get an explicit v4 skeleton; no
			// authored legacy installation text is translated by setup.
			globalCfg := resolvePlaceholders(tc.GlobalCfg, urls)
			if globalCfg == "" {
				globalCfg = "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			}
			env.WriteGlobalConfig(globalCfg)
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
					absent, err := checkPathAbsent(env.mustResolvePath(p))
					if !absent || err != nil {
						errMsg := fmt.Sprintf("step %d: file %q unexpectedly exists", i, p)
						if err != nil {
							errMsg = fmt.Sprintf("step %d: cannot inspect %q: %v", i, p, err)
						}
						if firstErr == "" {
							firstErr = errMsg
						}
						t.Errorf("%s", errMsg)
					}
				}
				for _, globs := range []struct {
					patterns []string
					absent   bool
				}{{step.GlobsExist, false}, {step.GlobsAbsent, true}} {
					for _, pattern := range globs.patterns {
						matches, err := filepath.Glob(env.mustResolvePath(pattern))
						var errMsg string
						switch {
						case err != nil:
							errMsg = fmt.Sprintf("step %d: glob %q: %v", i, pattern, err)
						case globs.absent && len(matches) > 0:
							errMsg = fmt.Sprintf("step %d: glob %q matched %v", i, pattern, matches)
						case !globs.absent && len(matches) == 0:
							errMsg = fmt.Sprintf("step %d: glob %q matched no path", i, pattern)
						default:
							continue
						}
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
