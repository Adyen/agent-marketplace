package e2e_test

import "encoding/json"

// bundleRootHandlers is the handler list for one hook group running command.
func bundleRootHandlers(command string) []any {
	return []any{map[string]any{"type": "command", "command": command}}
}

// bundleRootHooks returns a Claude-layout hooks/hooks.json with a PreToolUse hook and, when stop is set, a Stop hook.
// Stop has no matcher, so its group omits one.
func bundleRootHooks(pre, stop string) string {
	hooks := map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Bash", "hooks": bundleRootHandlers(pre)}}}
	if stop != "" {
		hooks["Stop"] = []any{map[string]any{"hooks": bundleRootHandlers(stop)}}
	}
	data, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		panic(err)
	}
	return string(data)
}

// bundleRootFiles returns a bundle with hooks/hooks.json and an executable-looking support script at each path.
func bundleRootFiles(hooks string, scripts ...string) map[string]string {
	files := map[string]string{"hooks/hooks.json": hooks}
	for _, rel := range scripts {
		files[rel] = "#!/bin/sh\necho bundle support\n"
	}
	return files
}

// bundleRootNative returns the native hook file for harness in scope and the scope root that holds the harness directory.
func bundleRootNative(harness, scope string) (native, root string) {
	root, file := "/workspace", "hooks.json"
	if scope == "user" {
		root = "/root"
	}
	if harness == "claude" {
		file = "settings.json"
	}
	return root + "/." + harness + "/" + file, root
}

// bundleRootCase builds a case whose bundle comes from a Git registry or, when git is false, from a directory source written by the case's first steps.
func bundleRootCase(name, desc, harness, scope string, git bool, files map[string]string, steps ...Step) TestCase {
	tc := TestCase{Name: "hooks/bundle_root/" + name, Desc: desc}
	cfg := "apiVersion: agent-marketplace/v4\nharnesses: [" + harness + "]\n"
	var writes []Step
	if git {
		tc.Registries = []RegistryDef{{Name: "bundle", Files: files}}
		cfg += "sources: {team: {git: '{{REG:bundle}}', ref: main}}\n"
	} else {
		writeRoot := "/workspace/resources"
		if scope == "user" {
			writeRoot = "/root/.agents/resources"
		}
		cfg += "sources: {team: {directory: ./resources}}\n"
		for rel, body := range files {
			writes = append(writes, Step{Action: ActionWriteFile, Path: writeRoot + "/" + rel, Body: body})
		}
	}
	cfg += "resources: {team: {all: true}}\n"
	if scope == "user" {
		tc.GlobalCfg = cfg
	} else {
		tc.RepoCfg = cfg
		tc.GlobalCfg = "apiVersion: agent-marketplace/v4\nharnesses: [" + harness + "]\n"
	}
	tc.Steps = append(writes, steps...)
	return tc
}

func bundleRootSourceName(git bool) string {
	if git {
		return "git"
	}
	return "directory"
}

// spec: hook-resources/bundle-root-references-resolve-on-every-source-path
// spec: hook-resources/support-files-keep-their-bundle-relative-paths
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh"`, ""), "hooks/scripts/check.sh", "hooks/scripts/nested/deep.sh")
	for _, git := range []bool{true, false} {
		for _, harness := range []string{"claude", "factory"} {
			for _, scope := range []string{"repo", "user"} {
				native, root := bundleRootNative(harness, scope)
				support := root + "/." + harness + "/hook-support"
				testTable = append(testTable, bundleRootCase(
					bundleRootSourceName(git)+"/"+harness+"/"+scope,
					"Given a Claude-layout bundle whose hook runs ${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh and ships a nested support script, "+
						"When sync, check, and a repeat sync run from a "+bundleRootSourceName(git)+" source for "+harness+" in "+scope+" scope, "+
						"Then the native hook references an installed support file, no reference diagnostic is emitted, and repeat syncs leave the files unchanged",
					harness, scope, git, files,
					Step{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"hook.support-reference"}, FileContains: map[string]string{native: "/PreToolUse-command/hooks/scripts/check.sh"}, FileNotContains: map[string]string{native: "CLAUDE_PLUGIN_ROOT"}, GlobsExist: []string{support + "/*/PreToolUse-command/hooks/scripts/check.sh", support + "/*/PreToolUse-command/hooks/scripts/nested/deep.sh"}, GlobsAbsent: []string{support + "/*/PreToolUse-command/scripts"}},
					Step{Args: []string{"sync", "--check"}, MustPass: true, PathsUnchanged: []string{native}},
					Step{Args: []string{"sync"}, MustPass: true, PathsUnchanged: []string{native, support}},
				))
			}
		}
	}
}

// spec: hook-resources/a-reference-to-a-missing-support-file-refuses-the-hook
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/absent.sh"`, "echo sibling-ok"), "hooks/scripts/check.sh")
	native, root := bundleRootNative("claude", "repo")
	for _, git := range []bool{true, false} {
		testTable = append(testTable, bundleRootCase(
			"missing-reference/"+bundleRootSourceName(git),
			"Given a PreToolUse hook that references a support file the bundle does not ship, and a sibling Stop hook, "+
				"When sync runs for claude in repository scope from a "+bundleRootSourceName(git)+" source, "+
				"Then sync fails with hook.support-reference-missing, the referenced path is not printed, the sibling installs, and no support directory is written",
			"claude", "repo", git, files,
			Step{Args: []string{"sync"}, AssertExitCode: intPtr(1), OutContains: []string{"hook.support-reference-missing"}, OutNotContains: []string{"absent.sh"}, FileContains: map[string]string{native: "echo sibling-ok"}, FileNotContains: map[string]string{native: "absent.sh"}, GlobsAbsent: []string{root + "/.claude/hook-support/*/PreToolUse-command"}},
		))
	}
}

// spec: hook-resources/bare-and-dynamic-plugin-root-references-are-not-checked
func init() {
	files := bundleRootFiles(bundleRootHooks(`cd "${CLAUDE_PLUGIN_ROOT}" && ./hooks/scripts/check.sh`, `"${CLAUDE_PLUGIN_ROOT}/hooks/$SCRIPT"`), "hooks/scripts/check.sh")
	native, _ := bundleRootNative("claude", "repo")
	testTable = append(testTable, bundleRootCase(
		"bare-and-dynamic/git",
		"Given a PreToolUse hook that changes into ${CLAUDE_PLUGIN_ROOT} and a Stop hook whose script name is a shell variable, "+
			"When sync runs for claude in repository scope from a git source, "+
			"Then both tokens are rewritten to the support directory, no reference diagnostic is emitted, and the token is not checked",
		"claude", "repo", true, files,
		Step{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"hook.support-reference-missing"}, FileContains: map[string]string{native: "/Stop-command/hooks/$SCRIPT"}, FileNotContains: map[string]string{native: "CLAUDE_PLUGIN_ROOT"}},
	))
}

// spec: hook-resources/legacy-folder-relative-references-keep-working-with-a-warning
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/scripts/check.sh"`, ""), "hooks/scripts/check.sh")
	native, root := bundleRootNative("claude", "repo")
	support := root + "/.claude/hook-support"
	for _, git := range []bool{true, false} {
		testTable = append(testTable, bundleRootCase(
			"legacy-reference/"+bundleRootSourceName(git),
			"Given a hook defined in hooks/hooks.json that references ${CLAUDE_PLUGIN_ROOT}/scripts/check.sh and ships only hooks/scripts/check.sh, "+
				"When sync and check run for claude in repository scope from a "+bundleRootSourceName(git)+" source, "+
				"Then the command references the installed hooks/scripts/check.sh, sync warns hook.support-reference-legacy, and check agrees",
			"claude", "repo", git, files,
			Step{Args: []string{"sync"}, MustPass: true, OutContains: []string{"hook.support-reference-legacy"}, FileContains: map[string]string{native: "/PreToolUse-command/hooks/scripts/check.sh"}, GlobsExist: []string{support + "/*/PreToolUse-command/hooks/scripts/check.sh"}},
			Step{Args: []string{"sync", "--check"}, MustPass: true},
		))
	}
}

// spec: hook-resources/references-that-resolve-under-both-meanings-use-the-bundle-root
func init() {
	files := bundleRootFiles(bundleRootHooks(`"${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh"`, ""), "hooks/scripts/check.sh", "hooks/hooks/scripts/check.sh")
	native, _ := bundleRootNative("claude", "repo")
	testTable = append(testTable, bundleRootCase(
		"both-meanings/git",
		"Given a hook that references ${CLAUDE_PLUGIN_ROOT}/hooks/scripts/check.sh and a bundle that ships that path under both the bundle-root and folder-relative meanings, "+
			"When sync runs for claude in repository scope from a git source, "+
			"Then the command uses the bundle-root path and no legacy warning is emitted",
		"claude", "repo", true, files,
		Step{Args: []string{"sync"}, MustPass: true, OutNotContains: []string{"hook.support-reference-legacy"}, FileContains: map[string]string{native: "/PreToolUse-command/hooks/scripts/check.sh"}, FileNotContains: map[string]string{native: "/PreToolUse-command/hooks/hooks/scripts/check.sh"}},
	))
}
