package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func (n *compatibilityNative) prepareFixtureTrust(e *Env, f compatibilityFixture, rows map[string]*compatibilityResult) (*compatibilityResult, error) {
	setup := rows["native-cli"]
	if !n.trustFixtures || setup == nil {
		return nil, nil
	}
	if setup.Harness == "cursor" && setup.Scope == "repo" {
		return n.prepareCursorFixtureApproval(e, f, rows)
	}
	if setup.Harness != "claude" && setup.Harness != "codex" && setup.Harness != "gemini" {
		return nil, nil
	}
	if setup.Scope != "repo" && setup.Harness != "gemini" {
		return nil, nil
	}
	var checked []*compatibilityResult
	for _, probe := range n.probes {
		row := rows[probe.kind]
		if setup.Harness != "gemini" && probe.kind != "mcp" {
			continue
		}
		if row == nil || row.Status != "config-only-verified" || probe.before.Error != "" || setup.Scope == "user" && probe.kind == "skill" {
			continue
		}
		command := runCompatibilityCommand(e, n.binary, probe.list, 20*time.Second)
		absent := setup.Harness == "codex" || probe.kind == "skill"
		err := requireCompatibilityObservation(&command, absent, probe.names...)
		if err == nil && setup.Harness == "gemini" && probe.kind == "skill" && !compatibilityTrustBlocksDiscovery(*row, command) {
			err = fmt.Errorf("native skill listing did not report the expected workspace trust gate")
		}
		if err == nil && !absent {
			connection := compatibilityMCPConnection(&command, setup.Harness)
			if connection.Status != "blocked" || regexp.MustCompile(`(?i)\bconnected\b`).MatchString(command.output) {
				err = fmt.Errorf("untrusted fixture unexpectedly connected or did not expose a native approval gate")
			}
			if setup.Harness == "claude" && !strings.Contains(strings.ToLower(command.output), "pending approval") {
				err = fmt.Errorf("project MCP fixture did not report pending approval before test-only approval")
			}
		}
		if err == nil {
			command.Observed = append(command.Observed, "untrusted fixture gate verified before any test-only trust grant")
		}
		row.Commands = append(row.Commands, command)
		if err != nil {
			return row, err
		}
		checked = append(checked, row)
	}
	if len(checked) == 0 {
		return nil, nil
	}
	if err := grantCompatibilityFixtureTrust(e, setup.Harness, f); err != nil {
		return checked[0], fmt.Errorf("configure isolated fixture trust: %w", err)
	}
	for _, probe := range n.probes {
		if row := rows[probe.kind]; row != nil && row.Status == "config-only-verified" {
			row.Trust = "repository-only"
		}
	}
	return nil, nil
}

func (n *compatibilityNative) prepareCursorFixtureApproval(e *Env, f compatibilityFixture, rows map[string]*compatibilityResult) (*compatibilityResult, error) {
	row := rows["mcp"]
	if row == nil || row.Status != "config-only-verified" {
		return nil, nil
	}
	root, err := openCompatibilityTrustRoot(e)
	if err != nil {
		return row, err
	}
	defer root.Close()
	for _, name := range []string{f.name("mcp-a"), f.name("mcp-b")} {
		before := runCompatibilityCommand(e, n.binary, []string{"mcp", "list-tools", name}, 20*time.Second)
		row.Commands = append(row.Commands, before)
		if before.Error == "" || before.TimedOut || !slices.Contains(before.Diagnostics, "cli.mcp-approval-required") {
			return row, fmt.Errorf("Cursor did not reject the unapproved fixture MCP server")
		}
		approved := runCompatibilityCommand(e, n.binary, []string{"mcp", "enable", name}, 20*time.Second)
		row.Commands = append(row.Commands, approved)
		if err := compatibilityInspectionError(approved); err != nil {
			return row, err
		}
	}
	row.Trust = "repository-only"
	return nil, nil
}

func openCompatibilityTrustRoot(e *Env) (*os.Root, error) {
	base := filepath.Dir(e.home)
	if e.home != filepath.Join(base, "home") || e.workspace != filepath.Join(base, "workspace") {
		return nil, fmt.Errorf("trust requires sibling fixture home and workspace directories")
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	temporary, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(temporary, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fmt.Errorf("trust is restricted to temporary fixture roots")
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	for _, path := range []string{"home", "workspace", "workspace/.git"} {
		info, err := root.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = root.Close()
			return nil, fmt.Errorf("trust requires a real disposable repository and home")
		}
	}
	return root, nil
}

func grantCompatibilityFixtureTrust(e *Env, target string, f compatibilityFixture) error {
	root, err := openCompatibilityTrustRoot(e)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	switch target {
	case "codex":
		const path = "home/.codex/config.toml"
		original, err := root.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if strings.Contains(string(original), "[projects") || strings.Contains(string(original), "trust_level") {
			return fmt.Errorf("fixture already carries Codex project trust state")
		}
		data := string(original) + fmt.Sprintf("\n[projects.%q]\ntrust_level = \"trusted\"\n", e.workspace)
		if err := root.MkdirAll("home/.codex", 0o700); err != nil {
			return err
		}
		return root.WriteFile(path, []byte(data), 0o600)
	case "gemini":
		return patchCompatibilityTrustJSON(root, "home/.gemini/trustedFolders.json", func(folders map[string]json.RawMessage) error {
			if len(folders) != 0 {
				return fmt.Errorf("fixture already carries Gemini folder trust state")
			}
			folders[e.workspace] = json.RawMessage(`"TRUST_FOLDER"`)
			return nil
		})
	case "claude":
		if err := patchCompatibilityTrustJSON(root, "home/.claude.json", func(config map[string]json.RawMessage) error {
			projects, err := compatibilityTrustObject(config["projects"])
			if err != nil {
				return err
			}
			project, err := compatibilityTrustObject(projects[e.workspace])
			if err != nil {
				return err
			}
			if raw := project["hasTrustDialogAccepted"]; len(raw) > 0 && string(raw) != "false" {
				return fmt.Errorf("fixture already carries Claude workspace trust state")
			}
			project["hasTrustDialogAccepted"] = json.RawMessage("true")
			projects[e.workspace], err = json.Marshal(project)
			if err == nil {
				config["projects"], err = json.Marshal(projects)
			}
			return err
		}); err != nil {
			return err
		}
		return patchCompatibilityTrustJSON(root, "workspace/.claude/settings.local.json", func(settings map[string]json.RawMessage) error {
			if _, ok := settings["enabledMcpjsonServers"]; ok {
				return fmt.Errorf("fixture already carries Claude MCP approvals")
			}
			if string(settings["enableAllProjectMcpServers"]) == "true" {
				return fmt.Errorf("fixture carries a broad MCP approval")
			}
			servers, err := json.Marshal([]string{f.name("mcp-a"), f.name("mcp-b")})
			settings["enabledMcpjsonServers"] = servers
			return err
		})
	default:
		return fmt.Errorf("fixture trust is not implemented for target %q", target)
	}
}

func compatibilityTrustObject(data []byte) (map[string]json.RawMessage, error) {
	value := make(map[string]json.RawMessage)
	if len(data) != 0 {
		if err := json.Unmarshal(data, &value); err != nil || value == nil {
			return nil, fmt.Errorf("fixture trust requires a valid JSON object")
		}
	}
	return value, nil
}

func patchCompatibilityTrustJSON(root *os.Root, path string, patch func(map[string]json.RawMessage) error) error {
	data, err := root.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	value, err := compatibilityTrustObject(data)
	if err != nil {
		return err
	}
	if err := patch(value); err != nil {
		return err
	}
	data, err = json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return root.WriteFile(path, append(data, '\n'), 0o600)
}

func TestCompatibilityCursorApprovalOnlyEnablesFixtureServers(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	f := compatibilityFixture{prefix: "amcompat-cursor"}
	binary := filepath.Join(e.repos, "internal-fake-cursor")
	e.WriteFile(binary, `#!/bin/sh
case "$3" in amcompat-cursor-mcp-a|amcompat-cursor-mcp-b) ;; *) exit 75;; esac
case "$2" in
  list-tools) if [ -f "$HOME/$3-enabled" ]; then printf '%s\n' amcompat_echo; else printf '%s\n' 'MCP server has not been approved'; exit 1; fi;;
  enable) printf '%s' approved > "$HOME/$3-enabled";;
  *) exit 76;;
esac
`)
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	native := compatibilityNative{binary: binary, trustFixtures: true}
	row := &compatibilityResult{Harness: "cursor", Resource: "mcp", Scope: "repo", Status: "config-only-verified"}
	rows := map[string]*compatibilityResult{"native-cli": {Harness: "cursor", Scope: "repo"}, "mcp": row}
	if _, err := native.prepareFixtureTrust(e, f, rows); err != nil {
		t.Fatal(err)
	}
	if row.Trust != "repository-only" || len(row.Commands) != 4 {
		t.Fatal("fixture approval did not preserve its negative-control and enable evidence")
	}
	for _, name := range []string{f.name("mcp-a"), f.name("mcp-b")} {
		if _, err := os.Stat(filepath.Join(e.home, name+"-enabled")); err != nil {
			t.Fatal("fixture server was not individually approved")
		}
	}
}

func TestCompatibilityFixtureTrustRequiresObservedGate(t *testing.T) {
	for _, gated := range []bool{false, true} {
		t.Run(fmt.Sprint(gated), func(t *testing.T) {
			e := newEnv(t)
			e.InitGitRepo()
			f := compatibilityFixture{prefix: "amcompat-gemini"}
			output := f.name("skill")
			if gated {
				output = "Untrusted workspace; No skills found"
			}
			binary := filepath.Join(e.repos, "internal-fake-native")
			e.WriteFile(binary, "#!/bin/sh\nprintf '%s\\n' '"+output+"'\n")
			if err := os.Chmod(binary, 0o755); err != nil {
				t.Fatal(err)
			}
			native := compatibilityNative{binary: binary, trustFixtures: true, probes: []compatibilityNativeProbe{{kind: "skill", list: []string{"skills", "list"}, names: []string{f.name("skill")}}}}
			rows := map[string]*compatibilityResult{
				"native-cli": {Harness: "gemini", Resource: "native-cli", Scope: "repo"},
				"skill":      {Harness: "gemini", Resource: "skill", Scope: "repo", Status: "config-only-verified"},
			}
			_, err := native.prepareFixtureTrust(e, f, rows)
			if (err == nil) != gated {
				t.Fatalf("trust transition did not require observed untrusted gating: %v", err)
			}
			_, statErr := os.Stat(filepath.Join(e.home, ".gemini", "trustedFolders.json"))
			if gated && (statErr != nil || rows["skill"].Trust != "repository-only" || len(rows["skill"].Commands[0].Observed) != 2) {
				t.Fatal("successful fixture-only transition lost its native boundary evidence")
			}
			if !gated && (!os.IsNotExist(statErr) || rows["skill"].Trust != "") {
				t.Fatal("trust was granted without its negative control")
			}
		})
	}
}

func TestCompatibilityGeminiUserMCPStillRequiresWorkspaceTrust(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	f := compatibilityFixture{prefix: "amcompat-gemini"}
	binary := filepath.Join(e.repos, "internal-fake-native")
	e.WriteFile(binary, "#!/bin/sh\nprintf '%s\\n' 'Warning: folder is untrusted' '"+f.name("mcp-a")+": Disabled' '"+f.name("mcp-b")+": Disabled'\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	native := compatibilityNative{binary: binary, trustFixtures: true, probes: []compatibilityNativeProbe{{kind: "mcp", list: []string{"mcp", "list"}, names: []string{f.name("mcp-a"), f.name("mcp-b")}}}}
	rows := map[string]*compatibilityResult{
		"native-cli": {Harness: "gemini", Resource: "native-cli", Scope: "user"},
		"mcp":        {Harness: "gemini", Resource: "mcp", Scope: "user", Status: "config-only-verified"},
	}
	if _, err := native.prepareFixtureTrust(e, f, rows); err != nil {
		t.Fatal(err)
	}
	if rows["mcp"].Scope != "user" || rows["mcp"].Trust != "repository-only" {
		t.Fatal("installation scope and execution-workspace trust were conflated")
	}
	if _, err := os.Stat(filepath.Join(e.home, ".gemini", "trustedFolders.json")); err != nil {
		t.Fatal("user MCP fixture did not establish execution-workspace trust")
	}
}

func TestCompatibilityFixtureTrustRejectsEscapingPaths(t *testing.T) {
	for _, target := range []string{"codex", "gemini", "claude"} {
		t.Run(target, func(t *testing.T) {
			e := newEnv(t)
			e.InitGitRepo()
			outside := filepath.Join(t.TempDir(), "native-config")
			const original = `{"keep":"unchanged"}`
			e.WriteFile(outside, original)
			rel := map[string]string{"codex": ".codex/config.toml", "gemini": ".gemini/trustedFolders.json", "claude": ".claude.json"}[target]
			path := filepath.Join(e.home, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if err := grantCompatibilityFixtureTrust(e, target, compatibilityFixture{}); err == nil {
				t.Fatal("fixture trust escaped its temporary filesystem root")
			}
			if got, err := os.ReadFile(outside); err != nil || string(got) != original {
				t.Fatal("escaping trust path modified another configuration file")
			}
		})
	}
}

func TestCompatibilityJSONFixtureTrustIsNarrow(t *testing.T) {
	for _, target := range []string{"gemini", "claude"} {
		t.Run(target, func(t *testing.T) {
			e := newEnv(t)
			e.InitGitRepo()
			f := compatibilityFixture{prefix: "amcompat-" + target}
			if target == "claude" {
				e.WriteFile(filepath.Join(e.home, ".claude.json"), `{"counter":9007199254740993}`)
				e.WriteFile(filepath.Join(e.workspace, ".claude", "settings.local.json"), `{"permissions":{"deny":["Exec(*)"]}}`)
			}
			if err := grantCompatibilityFixtureTrust(e, target, f); err != nil {
				t.Fatal(err)
			}
			if target == "gemini" {
				data, err := os.ReadFile(filepath.Join(e.home, ".gemini", "trustedFolders.json"))
				if err != nil {
					t.Fatal(err)
				}
				var folders map[string]string
				if err := json.Unmarshal(data, &folders); err != nil || len(folders) != 1 || folders[e.workspace] != "TRUST_FOLDER" {
					t.Fatalf("trust must target exactly one fixture, not its parent: %s", data)
				}
			} else {
				data, err := os.ReadFile(filepath.Join(e.home, ".claude.json"))
				if err != nil {
					t.Fatal(err)
				}
				var config struct {
					Projects map[string]struct{ HasTrustDialogAccepted bool }
				}
				if err := json.Unmarshal(data, &config); err != nil || len(config.Projects) != 1 || !config.Projects[e.workspace].HasTrustDialogAccepted || !strings.Contains(string(data), "9007199254740993") {
					t.Fatalf("wrong fixture trust or damaged unrelated configuration: %s", data)
				}
				data, err = os.ReadFile(filepath.Join(e.workspace, ".claude", "settings.local.json"))
				if err != nil {
					t.Fatal(err)
				}
				var settings struct {
					EnabledMcpjsonServers []string
					Permissions           struct{ Deny []string }
				}
				if err := json.Unmarshal(data, &settings); err != nil || len(settings.EnabledMcpjsonServers) != 2 || settings.EnabledMcpjsonServers[0] != f.name("mcp-a") || settings.EnabledMcpjsonServers[1] != f.name("mcp-b") || len(settings.Permissions.Deny) != 1 || settings.Permissions.Deny[0] != "Exec(*)" {
					t.Fatalf("fixture approval broadened permissions or lost existing settings: %s", data)
				}
			}
		})
	}
}

func TestCompatibilityCodexFixtureTrustPreservesNativeConfig(t *testing.T) {
	e := newEnv(t)
	e.InitGitRepo()
	userPath := filepath.Join(e.home, ".codex", "config.toml")
	repoPath := filepath.Join(e.workspace, ".codex", "config.toml")
	const user = "model = \"fixture-model\"\n"
	const repo = "[mcp_servers.fixture]\ncommand = 'fixture-server'\n"
	e.WriteFile(userPath, user)
	e.WriteFile(repoPath, repo)
	if err := grantCompatibilityFixtureTrust(e, "codex", compatibilityFixture{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), user) || !strings.Contains(string(got), "[projects.\""+e.workspace+"\"]") || strings.Count(string(got), "trust_level") != 1 || !strings.Contains(string(got), "trust_level = \"trusted\"") {
		t.Fatalf("fixture trust did not preserve user config or scope trust to one workspace: %s", got)
	}
	if got, err := os.ReadFile(repoPath); err != nil || string(got) != repo {
		t.Fatal("fixture trust modified generated repository configuration")
	}
	if err := grantCompatibilityFixtureTrust(e, "codex", compatibilityFixture{}); err == nil {
		t.Fatal("a pre-trusted fixture was accepted as a fresh trust transition")
	}
}
