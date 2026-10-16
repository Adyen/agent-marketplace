package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// spec: materialization/hook-support-source-refusal-preserves-installed-native-content
func TestFilesystemHookSupportSourceAdmission(t *testing.T) {
	for _, fixture := range []struct{ name, root string }{
		{"ad-hoc", "apiVersion: agent-marketplace/v4\n"},
		{"configured", "apiVersion: agent-marketplace/v4\nsources: {local: {directory: ./vendor/registry}}\nresources: {local: {include: [hook/PostToolUse-command]}}\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newEnv(t)
			env.InitGitRepo()
			env.MustRunAM("init", "--harnesses", "factory")
			source := filepath.Join(env.workspace, "vendor", "registry", "hooks")
			definitionPath := filepath.Join(source, "hooks.json")
			helperSource := filepath.Join(source, "helper.txt")
			aliasSource := filepath.Join(source, "alias.txt")
			env.WriteFile(definitionPath, `{"hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"inert original hook"}]}]}}`)
			env.WriteFile(helperSource, "inert CLI hook support")
			if err := os.Chmod(helperSource, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("helper.txt", aliasSource); err != nil {
				t.Fatal(err)
			}
			sourceDefinitionInitial := captureDest(t, definitionPath)
			sourceHelperInitial := captureDest(t, helperSource)
			sourceAliasInitial := captureDest(t, aliasSource)
			env.WriteRepoConfig(fixture.root)
			env.MustRunAM("install", "PostToolUse-command", "--kind", "hook", "--from", "./vendor/registry", "--scope", "repo")
			requireDestUntouched(t, sourceDefinitionInitial, captureDest(t, definitionPath))
			requireDestUntouched(t, sourceHelperInitial, captureDest(t, helperSource))
			requireDestUntouched(t, sourceAliasInitial, captureDest(t, aliasSource))

			nativePath := filepath.Join(env.workspace, ".factory", "hooks.json")
			nativeBytes, err := os.ReadFile(nativePath)
			if err != nil {
				t.Fatal(err)
			}
			var native map[string][]struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(nativeBytes, &native); err != nil {
				t.Fatalf("Factory native hook output is not a direct event map: %v", err)
			}
			groups, ok := native["PostToolUse"]
			if len(native) != 1 || !ok || len(groups) != 1 || groups[0].Matcher != "^(Create|Edit|ApplyPatch)$" ||
				len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" || groups[0].Hooks[0].Command != "inert original hook" {
				t.Fatalf("Factory native hook output = %s", nativeBytes)
			}
			if bytes.Contains(nativeBytes, []byte("_managed_by")) {
				t.Fatalf("Factory native hook output contains an in-file marker: %s", nativeBytes)
			}
			aliasMatches, err := filepath.Glob(filepath.Join(env.workspace, ".factory", "hook-support", "*", "PostToolUse-command", "alias.txt"))
			if err != nil || len(aliasMatches) != 1 {
				t.Fatalf("Factory alias support matches = %v / %v", aliasMatches, err)
			}
			helperMatches, err := filepath.Glob(filepath.Join(env.workspace, ".factory", "hook-support", "*", "PostToolUse-command", "helper.txt"))
			if err != nil || len(helperMatches) != 1 || filepath.Dir(helperMatches[0]) != filepath.Dir(aliasMatches[0]) {
				t.Fatalf("Factory helper support matches = %v / %v", helperMatches, err)
			}
			supportDir := filepath.Dir(aliasMatches[0])
			if entries, err := os.ReadDir(supportDir); err != nil || len(entries) != 2 {
				t.Fatalf("Factory support files = %v / %v, want alias and helper only", entries, err)
			}
			for _, path := range []string{aliasMatches[0], helperMatches[0]} {
				info, err := os.Lstat(path)
				data, readErr := os.ReadFile(path)
				if err != nil || readErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o755 || !bytes.Equal(data, []byte("inert CLI hook support")) {
					t.Fatalf("Factory support copy %s = %q / %v / %v", path, data, err, readErr)
				}
			}
			flatLegacyPath := filepath.Join(env.workspace, ".factory", "hooks", "hooks.json")
			env.AssertFileAbsent(flatLegacyPath)

			nativeBefore := captureDest(t, nativePath)
			aliasBefore := captureDest(t, aliasMatches[0])
			helperBefore := captureDest(t, helperMatches[0])
			sourceDefinitionBefore := sourceDefinitionInitial
			sourceHelperBefore := sourceHelperInitial
			sourceAliasBefore := sourceAliasInitial
			assertStable := func() {
				t.Helper()
				requireDestUntouched(t, nativeBefore, captureDest(t, nativePath))
				requireDestUntouched(t, aliasBefore, captureDest(t, aliasMatches[0]))
				requireDestUntouched(t, helperBefore, captureDest(t, helperMatches[0]))
				requireDestUntouched(t, sourceDefinitionBefore, captureDest(t, definitionPath))
				requireDestUntouched(t, sourceHelperBefore, captureDest(t, helperSource))
				requireDestUntouched(t, sourceAliasBefore, captureDest(t, aliasSource))
			}
			env.MustRunAM("sync")
			assertStable()
			env.MustRunAM("sync", "--check")
			assertStable()

			external := filepath.Join(env.home, "foreign-inert.txt")
			env.WriteFile(external, "foreign inert bytes")
			if err := os.Chmod(external, 0o640); err != nil {
				t.Fatal(err)
			}
			externalBefore := captureDest(t, external)
			badSupport := filepath.Join(source, "bad.txt")
			if err := os.Symlink(external, badSupport); err != nil {
				t.Fatal(err)
			}
			badLinkBefore, err := os.Lstat(badSupport)
			if err != nil {
				t.Fatal(err)
			}
			badLinkTarget, err := os.Readlink(badSupport)
			if err != nil {
				t.Fatal(err)
			}
			env.WriteFile(definitionPath, `{"hooks":{"PostToolUse":[{"matcher":"Write|Edit","hooks":[{"type":"command","command":"inert changed hook"}]}]}}`)
			changedDefinitionBefore := captureDest(t, definitionPath)
			sourceHelperBeforeRefusal := captureDest(t, helperSource)
			sourceAliasBeforeRefusal := captureDest(t, aliasSource)
			refused := env.RunAM("sync")
			env.AssertExitCode(refused, 1)
			env.AssertOutputContains(refused, "materialize.compile.error")
			env.AssertOutputContains(refused, "materialize.hook.support-refused")
			env.AssertOutputNotContains(refused, "foreign inert bytes")
			requireDestUntouched(t, nativeBefore, captureDest(t, nativePath))
			requireDestUntouched(t, aliasBefore, captureDest(t, aliasMatches[0]))
			requireDestUntouched(t, helperBefore, captureDest(t, helperMatches[0]))
			requireDestUntouched(t, changedDefinitionBefore, captureDest(t, definitionPath))
			requireDestUntouched(t, sourceHelperBeforeRefusal, captureDest(t, helperSource))
			requireDestUntouched(t, sourceAliasBeforeRefusal, captureDest(t, aliasSource))
			requireDestUntouched(t, externalBefore, captureDest(t, external))
			if after, err := os.Lstat(badSupport); err != nil || !os.SameFile(badLinkBefore, after) || badLinkBefore.Mode() != after.Mode() || !badLinkBefore.ModTime().Equal(after.ModTime()) {
				t.Fatalf("unsafe source link changed: %v", err)
			}
			if target, err := os.Readlink(badSupport); err != nil || target != badLinkTarget {
				t.Fatalf("unsafe source link target changed: %q / %v", target, err)
			}
			if entries, err := os.ReadDir(supportDir); err != nil || len(entries) != 2 {
				t.Fatalf("refused Factory support files = %v / %v", entries, err)
			}
			env.AssertFileAbsent(filepath.Join(supportDir, "bad.txt"))
			env.AssertFileAbsent(flatLegacyPath)
		})
	}
}
