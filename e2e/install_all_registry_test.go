package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

func newInstallAllRegistryEnv(t *testing.T, schema string) (*Env, string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	url := env.CreateBareRepo("all-registry-"+schema, RegistryFiles(
		[]Skill{{Name: "review", Description: "Review changes"}, {Name: "deploy", Description: "Deploy"}},
		[]Command{{Name: "cmd-one", Description: "Command one", Command: "echo one"}},
		nil,
	))
	return env, url
}

// install --all --registry targets one physical source: the same registry in
// several layers is not ambiguous, any URL spelling that update and lock
// accept works, and a registry only the user declares still installs into
// the repository.
func TestInstallAllRegistryTargetsOnePhysicalSource(t *testing.T) {
	for _, schema := range []string{"v4"} {
		for _, repoDeclares := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/repo-declares=%t", schema, repoDeclares), func(t *testing.T) {
				env, url := newInstallAllRegistryEnv(t, schema)
				env.WriteGlobalConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  personal: {git: '%s', ref: main}\n", url))
				if repoDeclares {
					env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  team: {git: '%s', ref: main}\nresources:\n  team: {include: [skill/review]}\n", url))
				}
				user := env.ReadFile("/root/.agents/config.yaml")

				for _, spelling := range []string{url, strings.TrimSuffix(url, ".git"), url + "/"} {
					result := env.RunAM("install", "--scope", "repo", "--all", "--kind", "command", "--registry", spelling)
					if result.ExitCode != 0 || !strings.Contains(result.Combined, "Installed cmd-one") {
						t.Fatalf("install --all --registry %s failed (exit %d):\n%s", spelling, result.ExitCode, result.Combined)
					}
				}
				env.AssertFileExists("/workspace/.factory/commands/cmd-one.md")
				if got := env.ReadFile("/root/.agents/config.yaml"); got != user {
					t.Fatalf("install --all changed the user config:\n%s", got)
				}
				repo := env.ReadFile("/workspace/.agents/config.yaml")
				if !strings.Contains(repo, "cmd-one") || !strings.Contains(repo, "apiVersion: agent-marketplace/"+schema) || strings.Count(repo, strings.TrimPrefix(strings.TrimSuffix(url, ".git"), "file://")) != 1 {
					t.Fatalf("install --all must add cmd-one to one repository source and keep the schema:\n%s", repo)
				}
				env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			})
		}
	}
}

// When one URL names two different sources (here the repository root and its
// skills directory), install --all --registry refuses and names both
// candidates instead of pointing at a duplicate that does not exist.
func TestInstallAllRegistryNamesAmbiguousSources(t *testing.T) {
	for _, schema := range []string{"v4"} {
		t.Run(schema, func(t *testing.T) {
			env, url := newInstallAllRegistryEnv(t, schema)
			env.WriteGlobalConfig("apiVersion: agent-marketplace/" + schema + "\nharnesses: [factory]\n")
			env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  root: {git: '%s', ref: main}\n  nested: {git: '%s', ref: main, path: skills}\nresources:\n  root: {include: [skill/review]}\n  nested: {include: [skill/deploy]}\n", url, url))
			before := env.ReadFile("/workspace/.agents/config.yaml")

			result := env.RunAM("install", "--all", "--registry", url)
			if result.ExitCode == 0 || !strings.Contains(result.Combined, "path skills") || !strings.Contains(result.Combined, "repository root") || strings.Contains(result.Combined, "keep only one matching repo registry entry") {
				t.Fatalf("ambiguous install --all --registry did not name both sources (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			if got := env.ReadFile("/workspace/.agents/config.yaml"); got != before {
				t.Fatalf("refused install --all changed the config:\n%s", got)
			}
		})
	}
}
