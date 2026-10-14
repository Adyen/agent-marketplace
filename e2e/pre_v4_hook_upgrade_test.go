package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec: hook-resources/owned-factory-entries-move-to-the-native-file
// spec: hook-resources/check-reports-a-pending-migration
// spec: hook-resources/marked-gemini-flat-hooks-transition-at-both-scopes
func TestPreV4HookOwnershipUpgrade(t *testing.T) {
	if os.Getenv("AM_E2E_PRE_V4_BINARY") == "" {
		t.Skip("historical executable is supplied by the dedicated upgrade gate")
	}
	historical, arch := verifiedExecutable(t, "AM_E2E_PRE_V4_BINARY", preV4ReleaseArch)
	world := newHistoricalWorld(t, upgradeImage(t))
	candidate := world.buildCandidate(arch, "e2e")
	home, workspace, registry := world.path("home"), world.path("workspace"), world.path("registry")
	for _, dir := range []string{home, filepath.Join(home, ".agents"), filepath.Join(workspace, ".agents"), filepath.Join(registry, ".claude-plugin"), filepath.Join(registry, "hooks")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pluginManifest := `{"name":"historical-hooks","version":"1.0.0"}`
	hookSource := `{"hooks":{"PreToolUse":[{"type":"command","matcher":"Bash","command":"echo historical-pre-tool-use"}],"UserPromptSubmit":[{"type":"command","command":"echo historical-prompt-submit"}]}}`
	write(filepath.Join(registry, ".claude-plugin", "plugin.json"), pluginManifest)
	write(filepath.Join(registry, "hooks", "hooks.json"), hookSource)
	selection := fmt.Sprintf("  - path: %q\n    hooks:\n      include: [PreToolUse-command, UserPromptSubmit-command]\n", registry)
	write(filepath.Join(workspace, ".agents", "config.yaml"), "apiVersion: agent-marketplace/v2\nregistries:\n"+selection)

	must := func(executable string, args ...string) string {
		t.Helper()
		return world.must(home, workspace, executable, args...)
	}
	run := func(executable string, args ...string) (string, error) {
		t.Helper()
		return world.run(home, workspace, executable, args...)
	}

	must(historical, "init", "--harnesses", "claude,cursor,factory,gemini,devin,codex,windsurf")
	userConfig := filepath.Join(home, ".agents", "config.yaml")
	userData, err := os.ReadFile(userConfig)
	if err != nil || !strings.Contains(string(userData), "harnesses") {
		t.Fatalf("v3.1.1 init did not write the user config: %v\n%s", err, userData)
	}
	write(userConfig, string(userData)+"registries:\n"+selection)
	must(historical, "sync")

	preToolCommand := "echo historical-pre-tool-use"
	promptCommand := "echo historical-prompt-submit"
	selectedCommands := []string{preToolCommand, promptCommand}

	type snapEntry struct {
		kind      string
		mode      os.FileMode
		size      int64
		mtime     int64
		data      []byte
		target    string
		ephemeral bool
	}
	skippedSubtree := func(slash string) bool {
		if slash == "home/.cache" || strings.HasPrefix(slash, "home/.cache/") {
			return true
		}
		for _, root := range []string{"workspace", "home"} {
			for _, sub := range []string{"/.agent-marketplace/backups", "/.local/state/agent-marketplace/safety-backups"} {
				if slash == root+sub || strings.HasPrefix(slash, root+sub+"/") {
					return true
				}
			}
		}
		return false
	}
	lockDescriptor := func(slash string) bool {
		for _, root := range []string{"workspace", "home"} {
			if strings.HasPrefix(slash, root+"/.local/state/agent-marketplace/locks/") {
				return true
			}
		}
		return false
	}
	snapshot := func() map[string]snapEntry {
		t.Helper()
		entries := map[string]snapEntry{}
		for _, base := range []string{workspace, home} {
			err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(world.root, path)
				if err != nil {
					return err
				}
				slash := filepath.ToSlash(rel)
				if skippedSubtree(slash) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				info, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if d.IsDir() {
					entries[slash] = snapEntry{kind: "dir", mode: info.Mode()}
					return nil
				}
				e := snapEntry{mode: info.Mode(), size: info.Size(), mtime: info.ModTime().UnixNano()}
				switch {
				case lockDescriptor(slash):
					e.kind = "lock-descriptor"
					e.ephemeral = true
				case info.Mode()&os.ModeSymlink != 0:
					e.kind = "symlink"
					if e.target, err = os.Readlink(path); err != nil {
						return err
					}
				case info.Mode().IsRegular():
					e.kind = "file"
					if e.data, err = os.ReadFile(path); err != nil {
						return err
					}
				default:
					e.kind = "other"
				}
				entries[slash] = e
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return entries
	}
	entryEqual := func(want, got snapEntry, checkMtime bool) string {
		if got.kind != want.kind {
			return "kind " + got.kind + " != " + want.kind
		}
		if got.mode != want.mode {
			return fmt.Sprintf("mode %v != %v", got.mode, want.mode)
		}
		if want.ephemeral || want.kind == "dir" {
			return ""
		}
		if got.size != want.size {
			return fmt.Sprintf("size %d != %d", got.size, want.size)
		}
		if checkMtime && got.mtime != want.mtime {
			return "mtime changed"
		}
		if want.kind == "symlink" {
			if got.target != want.target {
				return "target " + got.target + " != " + want.target
			}
			return ""
		}
		if !bytes.Equal(got.data, want.data) {
			return "content changed"
		}
		return ""
	}
	assertState := func(stage string, want map[string]snapEntry, checkMtime bool) {
		t.Helper()
		got := snapshot()
		for rel := range want {
			if _, ok := got[rel]; !ok {
				t.Errorf("%s: %s disappeared", stage, rel)
			}
		}
		for rel := range got {
			if _, ok := want[rel]; !ok {
				t.Errorf("%s: %s appeared", stage, rel)
			}
		}
		for rel, w := range want {
			g, ok := got[rel]
			if !ok {
				continue
			}
			if diff := entryEqual(w, g, checkMtime); diff != "" {
				t.Errorf("%s: %s: %s", stage, rel, diff)
			}
		}
	}

	windsurfDirs := []string{
		"workspace/.windsurf/hooks",
		"home/.codeium/windsurf/hooks",
	}
	assertWindsurfPreserved := func(stage string, want map[string]snapEntry) {
		t.Helper()
		got := snapshot()
		for _, prefix := range windsurfDirs {
			for rel, g := range got {
				if !strings.HasPrefix(rel, prefix+"/") {
					continue
				}
				w, ok := want[rel]
				if !ok {
					t.Errorf("%s: %s appeared under a preserved Windsurf directory", stage, rel)
					continue
				}
				if diff := entryEqual(w, g, true); diff != "" {
					t.Errorf("%s: preserved Windsurf entry %s: %s", stage, rel, diff)
				}
			}
			for rel := range want {
				if strings.HasPrefix(rel, prefix+"/") {
					if _, ok := got[rel]; !ok {
						t.Errorf("%s: preserved Windsurf entry %s removed", stage, rel)
					}
				}
			}
		}
	}

	legacyMarked := []string{
		"workspace/.claude/hooks/hooks.json",
		"workspace/.cursor/hooks/hooks.json",
		"workspace/.factory/hooks/hooks.json",
		"home/.claude/hooks/hooks.json",
		"home/.cursor/hooks/hooks.json",
		"home/.factory/hooks/hooks.json",
	}
	legacyOwned := append([]string{
		"workspace/.gemini/settings.json",
		"workspace/.devin/hooks.v1.json",
		"workspace/.codex/hooks.json",
		"home/.gemini/settings.json",
		"home/.config/devin/config.json",
		"home/.codex/hooks.json",
	}, legacyMarked...)
	for _, rel := range legacyOwned {
		data, err := os.ReadFile(filepath.Join(world.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("v3.1.1 did not write %s: %v", rel, err)
		}
		for _, command := range selectedCommands {
			if !strings.Contains(string(data), command) {
				t.Fatalf("v3.1.1 output %s lacks %q:\n%s", rel, command, data)
			}
		}
	}
	for _, rel := range legacyMarked {
		data, err := os.ReadFile(filepath.Join(world.root, filepath.FromSlash(rel)))
		if err != nil || !strings.Contains(string(data), "_managed_by") {
			t.Fatalf("v3.1.1 marked file %s lost its marker: %v", rel, err)
		}
	}
	for _, dir := range windsurfDirs {
		for _, event := range []string{"pre_run_command", "pre_user_prompt"} {
			link := filepath.Join(world.root, filepath.FromSlash(dir), event+".json")
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("v3.1.1 did not write per-event Windsurf link %s: %v", link, err)
			}
		}
	}

	handwritten := "echo handwritten-control"
	addControl := func(path string, mutate func(map[string]any)) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("v3.1.1 did not write %s: %v", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("v3.1.1 output %s is not JSON: %v", path, err)
		}
		mutate(doc)
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		write(path, string(out))
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("control %s: %v", path, err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("control %s mode = %v, want regular 0600", path, info.Mode())
		}
	}
	flatEntry := func() any {
		return map[string]any{"type": "command", "command": handwritten, "matcher": "Notes"}
	}
	groupedEntry := func() any {
		return map[string]any{"hooks": []any{map[string]any{"type": "command", "command": handwritten}}}
	}
	controlExpected := map[string]string{}
	inject := func(rel string, entry any, mutate func(map[string]any)) {
		t.Helper()
		addControl(filepath.Join(world.root, filepath.FromSlash(rel)), mutate)
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		controlExpected[rel] = string(raw)
	}
	for _, rel := range []string{
		"workspace/.claude/hooks/hooks.json",
		"workspace/.cursor/hooks/hooks.json",
		"home/.claude/hooks/hooks.json",
		"home/.cursor/hooks/hooks.json",
	} {
		inject(rel, flatEntry(), func(doc map[string]any) {
			doc["hooks"].(map[string]any)["SessionStart"] = []any{flatEntry()}
		})
	}
	for _, rel := range []string{
		"workspace/.factory/hooks/hooks.json",
		"home/.factory/hooks/hooks.json",
	} {
		inject(rel, groupedEntry(), func(doc map[string]any) {
			doc["SessionStart"] = []any{groupedEntry()}
		})
	}
	for _, rel := range []string{
		"workspace/.gemini/settings.json",
		"home/.gemini/settings.json",
	} {
		inject(rel, groupedEntry(), func(doc map[string]any) {
			doc["theme"] = "Default"
			doc["hooks"].(map[string]any)["SessionStart"] = []any{groupedEntry()}
		})
	}
	for _, rel := range []string{
		"workspace/.devin/hooks.v1.json",
		"home/.config/devin/config.json",
	} {
		inject(rel, groupedEntry(), func(doc map[string]any) {
			hooks, _ := doc["hooks"].(map[string]any)
			if hooks == nil {
				hooks = doc
			}
			hooks["SessionStart"] = []any{groupedEntry()}
		})
	}
	for _, rel := range []string{
		"workspace/.codex/hooks.json",
		"home/.codex/hooks.json",
	} {
		inject(rel, groupedEntry(), func(doc map[string]any) {
			doc["hooks"].(map[string]any)["SessionStart"] = []any{groupedEntry()}
		})
	}
	controlPaths := []string{
		"workspace/.claude/hooks/hooks.json",
		"workspace/.cursor/hooks/hooks.json",
		"workspace/.factory/hooks/hooks.json",
		"workspace/.gemini/settings.json",
		"workspace/.devin/hooks.v1.json",
		"workspace/.codex/hooks.json",
		"home/.claude/hooks/hooks.json",
		"home/.cursor/hooks/hooks.json",
		"home/.factory/hooks/hooks.json",
		"home/.gemini/settings.json",
		"home/.config/devin/config.json",
		"home/.codex/hooks.json",
	}
	assertControls := func(stage string) {
		t.Helper()
		for _, rel := range controlPaths {
			full := filepath.Join(world.root, filepath.FromSlash(rel))
			info, err := os.Lstat(full)
			if err != nil {
				t.Errorf("%s: handwritten control %s vanished: %v", stage, rel, err)
				continue
			}
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
				t.Errorf("%s: control %s mode = %v, want regular 0600", stage, rel, info.Mode())
			}
			data, err := os.ReadFile(full)
			if err != nil {
				t.Errorf("%s: read control %s: %v", stage, rel, err)
				continue
			}
			if got := strings.Count(string(data), handwritten); got != 1 {
				t.Errorf("%s: control %s holds %d handwritten entries, want 1:\n%s", stage, rel, got, data)
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Errorf("%s: control %s is not JSON: %v", stage, rel, err)
				continue
			}
			hooks, _ := doc["hooks"].(map[string]any)
			if hooks == nil {
				hooks = doc
			}
			list, _ := hooks["SessionStart"].([]any)
			if len(list) != 1 {
				t.Errorf("%s: control %s SessionStart has %d entries, want 1:\n%s", stage, rel, len(list), data)
				continue
			}
			raw, err := json.Marshal(list[0])
			if err != nil || string(raw) != controlExpected[rel] {
				t.Errorf("%s: control %s SessionStart entry = %s, want %s", stage, rel, raw, controlExpected[rel])
			}
		}
		for _, rel := range []string{"workspace/.gemini/settings.json", "home/.gemini/settings.json"} {
			data, err := os.ReadFile(filepath.Join(world.root, filepath.FromSlash(rel)))
			if err != nil || !strings.Contains(string(data), `"theme"`) {
				t.Errorf("%s: %s lost the unrelated setting: %v", stage, rel, err)
			}
		}
	}
	assertControls("before candidate")
	controlled := snapshot()

	output, checkErr := run(candidate, "sync", "--check")
	if checkErr == nil {
		t.Fatalf("candidate sync --check on v3.1.1 state unexpectedly succeeded:\n%s", output)
	}
	for _, want := range []string{
		"Hook changes",
		"materialize.hook.review-pending",
		`- command: "` + preToolCommand + `"`,
		`+ command: "` + preToolCommand + `"`,
		`- command: "` + promptCommand + `"`,
		`+ command: "` + promptCommand + `"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("pending-migration check output lacks %q:\n%s", want, output)
		}
	}
	assertState("read-only sync --check", controlled, true)

	must(candidate, "sync")

	type flatRule struct{ matcher, typ string }
	type groupRule struct{ matcher string }
	type nativeFile struct {
		path    string
		member  string
		version bool
		flat    map[string]flatRule
		grouped map[string]groupRule
	}
	groupedPre := func(matcher string) map[string]groupRule {
		return map[string]groupRule{"PreToolUse": {matcher: matcher}, "UserPromptSubmit": {}}
	}
	native := []nativeFile{
		{filepath.Join(workspace, ".claude", "settings.json"), "hooks", false, nil, groupedPre("Bash")},
		{filepath.Join(workspace, ".cursor", "hooks.json"), "hooks", true, map[string]flatRule{
			"preToolUse":         {matcher: "^Shell$", typ: "command"},
			"beforeSubmitPrompt": {typ: "command"},
		}, nil},
		{filepath.Join(workspace, ".factory", "hooks.json"), "", false, nil, groupedPre("Execute")},
		{filepath.Join(workspace, ".devin", "hooks.json"), "hooks", false, map[string]flatRule{
			"pre_run_command": {}, "pre_user_prompt": {},
		}, nil},
		{filepath.Join(workspace, ".devin", "hooks.v1.json"), "", false, nil, groupedPre("^exec$")},
		{filepath.Join(workspace, ".codex", "hooks.json"), "hooks", false, nil, groupedPre("^Bash$")},
		{filepath.Join(workspace, ".gemini", "settings.json"), "hooks", false, nil, map[string]groupRule{
			"BeforeTool": {matcher: "^run_shell_command$"}, "BeforeAgent": {},
		}},
		{filepath.Join(home, ".claude", "settings.json"), "hooks", false, nil, groupedPre("Bash")},
		{filepath.Join(home, ".cursor", "hooks.json"), "hooks", true, map[string]flatRule{
			"preToolUse":         {matcher: "^Shell$", typ: "command"},
			"beforeSubmitPrompt": {typ: "command"},
		}, nil},
		{filepath.Join(home, ".factory", "hooks.json"), "", false, nil, groupedPre("Execute")},
		{filepath.Join(home, ".codeium", "windsurf", "hooks.json"), "hooks", false, map[string]flatRule{
			"pre_run_command": {}, "pre_user_prompt": {},
		}, nil},
		{filepath.Join(home, ".config", "devin", "config.json"), "hooks", false, nil, groupedPre("^exec$")},
		{filepath.Join(home, ".codex", "hooks.json"), "hooks", false, nil, groupedPre("^Bash$")},
		{filepath.Join(home, ".gemini", "settings.json"), "hooks", false, nil, map[string]groupRule{
			"BeforeTool": {matcher: "^run_shell_command$"}, "BeforeAgent": {},
		}},
	}
	eventCommand := func(event string) string {
		switch event {
		case "PreToolUse", "preToolUse", "pre_run_command", "BeforeTool":
			return preToolCommand
		default:
			return promptCommand
		}
	}
	promptEvents := map[string]bool{
		"UserPromptSubmit": true, "beforeSubmitPrompt": true, "pre_user_prompt": true, "BeforeAgent": true,
	}
	assertNative := func(stage string, promptSelected bool) {
		t.Helper()
		for _, want := range native {
			data, err := os.ReadFile(want.path)
			if err != nil {
				t.Errorf("%s: native hook file missing %s: %v", stage, want.path, err)
				continue
			}
			text := string(data)
			if strings.Contains(text, "_managed_by") {
				t.Errorf("%s: %s carries a legacy marker:\n%s", stage, want.path, text)
			}
			var doc map[string]any
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Errorf("%s: %s is not JSON: %v\n%s", stage, want.path, err, text)
				continue
			}
			if want.version {
				if doc["version"] != float64(1) {
					t.Errorf("%s: %s lacks version 1 envelope:\n%s", stage, want.path, text)
				}
			}
			hookMap, _ := doc["hooks"].(map[string]any)
			if want.member == "" {
				hookMap = doc
			} else if hookMap == nil {
				t.Errorf("%s: %s lacks the hooks member:\n%s", stage, want.path, text)
				continue
			}
			for _, command := range selectedCommands {
				wantCount := 1
				if !promptSelected && command == promptCommand && strings.HasPrefix(want.path, workspace) {
					wantCount = 0
				}
				if got := strings.Count(text, command); got != wantCount {
					t.Errorf("%s: %s holds %d of %q, want %d:\n%s", stage, want.path, got, command, wantCount, text)
				}
			}
			checkMatcher := func(event string, entry map[string]any, matcher string) {
				got, present := entry["matcher"]
				if matcher == "" {
					if present {
						t.Errorf("%s: %s %s carries unexpected matcher %v", stage, want.path, event, got)
					}
					return
				}
				if !present || got != matcher {
					t.Errorf("%s: %s %s matcher = %v, want %q", stage, want.path, event, got, matcher)
				}
			}
			for event, rule := range want.flat {
				list, _ := hookMap[event].([]any)
				if !promptSelected && promptEvents[event] && strings.HasPrefix(want.path, workspace) {
					for _, item := range list {
						entry, _ := item.(map[string]any)
						if entry["command"] == promptCommand {
							t.Errorf("%s: %s %s still holds the deselected command", stage, want.path, event)
						}
					}
					continue
				}
				if len(list) != 1 {
					t.Errorf("%s: %s %s has %d entries, want 1:\n%s", stage, want.path, event, len(list), text)
					continue
				}
				entry, _ := list[0].(map[string]any)
				if entry["command"] != eventCommand(event) {
					t.Errorf("%s: %s %s command = %v", stage, want.path, event, entry["command"])
				}
				checkMatcher(event, entry, rule.matcher)
				typ, typed := entry["type"]
				if rule.typ == "" && typed {
					t.Errorf("%s: %s %s carries unexpected type %v", stage, want.path, event, typ)
				}
				if rule.typ != "" && typ != rule.typ {
					t.Errorf("%s: %s %s type = %v, want %q", stage, want.path, event, typ, rule.typ)
				}
			}
			for event, rule := range want.grouped {
				list, _ := hookMap[event].([]any)
				if !promptSelected && promptEvents[event] && strings.HasPrefix(want.path, workspace) {
					for _, item := range list {
						group, _ := item.(map[string]any)
						if inner, _ := group["hooks"].([]any); len(inner) > 0 {
							if handler, _ := inner[0].(map[string]any); handler["command"] == promptCommand {
								t.Errorf("%s: %s %s still holds the deselected command", stage, want.path, event)
							}
						}
					}
					continue
				}
				if len(list) != 1 {
					t.Errorf("%s: %s %s has %d groups, want 1:\n%s", stage, want.path, event, len(list), text)
					continue
				}
				group, _ := list[0].(map[string]any)
				checkMatcher(event, group, rule.matcher)
				inner, _ := group["hooks"].([]any)
				if len(inner) != 1 {
					t.Errorf("%s: %s %s has %d handlers, want 1", stage, want.path, event, len(inner))
					continue
				}
				handler, _ := inner[0].(map[string]any)
				if handler["command"] != eventCommand(event) || handler["type"] != "command" {
					t.Errorf("%s: %s %s handler = %v", stage, want.path, event, handler)
				}
			}
		}
	}
	assertNative("candidate sync", true)
	assertWindsurfPreserved("candidate sync", controlled)
	assertControls("candidate sync")

	nativePaths := map[string]bool{}
	for _, want := range native {
		nativePaths[want.path] = true
	}
	for _, base := range []string{workspace, home} {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(world.root, path)
			slash := filepath.ToSlash(rel)
			if skippedSubtree(slash) || lockDescriptor(slash) {
				return nil
			}
			preserved := false
			for _, prefix := range windsurfDirs {
				if strings.HasPrefix(slash, prefix+"/") {
					preserved = true
				}
			}
			if preserved {
				return nil
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || nativePaths[path] {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, command := range selectedCommands {
				if strings.Contains(string(data), command) {
					t.Errorf("%q leaked into %s", command, slash)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range legacyMarked {
		data, err := os.ReadFile(filepath.Join(world.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("legacy file with a handwritten entry was removed: %s: %v", rel, err)
		}
		text := string(data)
		for _, command := range selectedCommands {
			if strings.Contains(text, command) {
				t.Errorf("%s still holds a migrated entry:\n%s", rel, text)
			}
		}
	}
	stateFiles, err := filepath.Glob(filepath.Join(home, ".local", "state", "agent-marketplace", "ownership", "*", "state.json"))
	if err != nil || len(stateFiles) == 0 {
		t.Fatalf("v3.1.1 ownership state missing: %v", err)
	}
	assertStateVersion := func(stage string) {
		t.Helper()
		states, err := filepath.Glob(filepath.Join(home, ".local", "state", "agent-marketplace", "ownership", "*", "state.json"))
		if err != nil || len(states) == 0 {
			t.Fatalf("%s: ownership state missing: %v", stage, err)
		}
		for _, state := range states {
			var doc struct {
				Version int    `json:"version"`
				Root    string `json:"canonical_root"`
			}
			data, err := os.ReadFile(state)
			if err != nil || json.Unmarshal(data, &doc) != nil || doc.Version != 1 {
				t.Fatalf("%s: ownership state %s was rewritten or corrupted: %v\n%s", stage, state, err, data)
			}
			if !strings.HasPrefix(doc.Root, workspace) && !strings.HasPrefix(doc.Root, home) {
				t.Fatalf("%s: ownership state %s points outside the fixture roots: %s", stage, state, doc.Root)
			}
		}
	}
	assertStateVersion("candidate sync")
	if data, err := os.ReadFile(filepath.Join(workspace, ".agents", "config.yaml")); err != nil ||
		!strings.Contains(string(data), "agent-marketplace/v2") ||
		!strings.Contains(string(data), "PreToolUse-command") {
		t.Fatalf("repository config lost the historical v2 selection: %v\n%s", err, data)
	}
	if data, err := os.ReadFile(filepath.Join(registry, "hooks", "hooks.json")); err != nil || string(data) != hookSource {
		t.Fatalf("candidate rewrote the source registry: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(registry, ".claude-plugin", "plugin.json")); err != nil || string(data) != pluginManifest {
		t.Fatalf("candidate rewrote the source registry manifest: %v", err)
	}

	if out := must(candidate, "sync"); strings.Contains(out, "Hook changes") || strings.Contains(out, "materialize.hook.review") {
		t.Fatalf("repeat sync is not a silent no-op:\n%s", out)
	}
	beforeCheck := snapshot()
	must(candidate, "sync", "--check")
	assertState("converged sync --check", beforeCheck, true)
	converged := snapshot()
	must(candidate, "sync")
	assertState("repeat sync", converged, false)
	assertWindsurfPreserved("repeat sync", controlled)

	must(candidate, "migrate", "--scope", "repo")
	repoMigrated, err := os.ReadFile(filepath.Join(workspace, ".agents", "config.yaml"))
	if err != nil || !strings.Contains(string(repoMigrated), "agent-marketplace/v4") ||
		!strings.Contains(string(repoMigrated), "PreToolUse-command") ||
		!strings.Contains(string(repoMigrated), "UserPromptSubmit-command") {
		t.Fatalf("repository migration lost the historical selections: %v\n%s", err, repoMigrated)
	}
	must(candidate, "sync")
	must(candidate, "sync", "--check")
	assertNative("after repository migration", true)
	assertControls("after repository migration")
	assertWindsurfPreserved("after repository migration", controlled)
	assertStateVersion("after repository migration")
	userScopeFiles := map[string]snapEntry{}
	for _, want := range native {
		if strings.HasPrefix(want.path, home) {
			info, err := os.Lstat(want.path)
			if err != nil {
				t.Fatalf("stat user native file %s: %v", want.path, err)
			}
			data, err := os.ReadFile(want.path)
			if err != nil {
				t.Fatalf("read user native file %s: %v", want.path, err)
			}
			userScopeFiles[want.path] = snapEntry{kind: "file", mode: info.Mode(), mtime: info.ModTime().UnixNano(), data: data}
		}
	}

	must(candidate, "remove", "UserPromptSubmit-command", "--kind", "hook", "--scope", "repo")
	must(candidate, "sync")
	assertNative("after repo deselection", false)
	assertControls("after repo deselection")
	assertWindsurfPreserved("after repo deselection", controlled)
	assertStateVersion("after repo deselection")
	for path, want := range userScopeFiles {
		info, err := os.Lstat(path)
		if err != nil {
			t.Errorf("user-scope native file %s vanished on repo deselection: %v", path, err)
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want.data) ||
			info.Mode() != want.mode || info.ModTime().UnixNano() != want.mtime {
			t.Errorf("user-scope native file %s changed on repo deselection: %v", path, err)
		}
	}
	repoConfig, err := os.ReadFile(filepath.Join(workspace, ".agents", "config.yaml"))
	if err != nil || !strings.Contains(string(repoConfig), "PreToolUse-command") ||
		strings.Contains(string(repoConfig), "UserPromptSubmit-command") {
		t.Fatalf("repo deselection left wrong selections: %v\n%s", err, repoConfig)
	}
	userConfigAfter, err := os.ReadFile(userConfig)
	if err != nil || !strings.Contains(string(userConfigAfter), "UserPromptSubmit-command") {
		t.Fatalf("user config lost its own selection: %v\n%s", err, userConfigAfter)
	}
}
