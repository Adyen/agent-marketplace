package e2e_test

import (
	"fmt"
	"strings"
	"testing"
)

// newSelectionEnv is a v4 workspace with a registry offering a k8s-tagged
// review skill, a deploy skill, and a command.
func newSelectionEnv(t *testing.T, schema string) (*Env, string) {
	t.Helper()
	env := newEnv(t)
	env.InitGitRepo()
	env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
	url := env.CreateBareRepo("selection-"+schema, RegistryFiles(
		[]Skill{{Name: "review", Description: "Review changes", Tags: []string{"k8s"}}, {Name: "deploy", Description: "Deploy"}},
		[]Command{{Name: "cmd-one", Description: "Command one", Command: "echo one"}},
		nil,
	))
	return env, url
}

// Removing an explicitly installed resource from a --from registry deletes
// its selection, exactly like plain remove, instead of also recording an
// exclusion that later tag or bundle installs silently obey.
func TestRemoveFromDropsAnExplicitSelectionWithoutExcluding(t *testing.T) {
	for _, schema := range []string{"v4"} {
		for _, withRef := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ref=%t", schema, withRef), func(t *testing.T) {
				env, url := newSelectionEnv(t, schema)
				env.AssertExitCode(env.RunAM("install", "review", "--from", url, "--ref", "main"), 0)
				env.AssertFileExists("/workspace/.factory/skills/review/SKILL.md")

				remove := []string{"remove", "review", "--from", url}
				if withRef {
					remove = append(remove, "--ref", "main")
				}
				env.AssertExitCode(env.RunAM(remove...), 0)
				env.AssertFileAbsent("/workspace/.factory/skills/review")
				if config := env.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "review") || !strings.Contains(config, "apiVersion: agent-marketplace/"+schema) {
					t.Fatalf("remove --from left a selection or exclusion for review, or changed the schema:\n%s", config)
				}

				tagged := env.RunAM("install", "--tag", "k8s")
				if tagged.ExitCode != 0 || !strings.Contains(tagged.Combined, "Installed review") {
					t.Fatalf("install --tag after the removal did not install review (exit %d):\n%s", tagged.ExitCode, tagged.Combined)
				}
				env.AssertFileExists("/workspace/.factory/skills/review/SKILL.md")
				env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			})
		}
	}
}

// When the configuration deliberately excludes a resource, tag and bundle
// installs keep the exclusion and say so instead of reporting the resource
// as installed.
func TestInstallReportsResourcesTheConfigurationStillExcludes(t *testing.T) {
	for _, schema := range []string{"v4"} {
		for _, tc := range []struct {
			name, installed string
			args            func(url string) []string
			materialized    []string
		}{
			{"tag", "", func(string) []string { return []string{"install", "--tag", "k8s"} }, nil},
			{"bundle", "Installed cmd-one, deploy ", func(url string) []string { return []string{"install", "--from", url, "--ref", "main"} }, []string{"/workspace/.factory/skills/deploy/SKILL.md", "/workspace/.factory/commands/cmd-one.md"}},
		} {
			t.Run(schema+"/"+tc.name, func(t *testing.T) {
				env, url := newSelectionEnv(t, schema)
				env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {exclude: [skill/review]}\n", url))
				result := env.RunAM(tc.args(url)...)
				if result.ExitCode != 0 || strings.Contains(result.Combined, "Installed review") || !strings.Contains(result.Combined, "Not installed: review;") || !strings.Contains(result.Combined, tc.installed) {
					t.Fatalf("install reported an excluded resource as installed (exit %d):\n%s", result.ExitCode, result.Combined)
				}
				for _, path := range tc.materialized {
					env.AssertFileExists(path)
				}
				env.AssertFileAbsent("/workspace/.factory/skills/review")
				if config := env.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, "review") {
					t.Fatalf("install removed the deliberate exclusion:\n%s", config)
				}
				env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			})
		}
	}
}

// A resource selected both explicitly and through a collection of the same
// entry is removed in two steps on both remove paths: the first remove drops
// only the direct selection and the collection keeps providing it, as for
// hosted pins; removing it again excludes the collection member.
func TestRemoveOfACollectionMemberIsTwoStepOnBothPaths(t *testing.T) {
	for _, schema := range []string{"v4"} {
		for _, viaFrom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/from=%t", schema, viaFrom), func(t *testing.T) {
				env := newEnv(t)
				env.InitGitRepo()
				files := RegistryFiles([]Skill{{Name: "review", Description: "Review"}, {Name: "other", Description: "Other"}}, nil, nil)
				files["collections/bundle/COLLECTION.yaml"] = "name: bundle\nversion: \"1.0\"\nresources:\n  - name: review\n  - name: other\n"
				url := env.CreateBareRepo("collection-overlap-"+schema, files)
				env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
				env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review, collection/bundle]}\n", url))
				env.AssertExitCode(env.RunAM("sync"), 0)
				remove := []string{"remove", "review"}
				if viaFrom {
					remove = append(remove, "--from", url)
				}

				env.AssertExitCode(env.RunAM(remove...), 0)
				env.AssertFileExists("/workspace/.factory/skills/review/SKILL.md")
				if config := env.ReadFile("/workspace/.agents/config.yaml"); strings.Contains(config, "review") || !strings.Contains(config, "bundle") {
					t.Fatalf("first remove must drop only the direct selection and keep the collection:\n%s", config)
				}

				env.AssertExitCode(env.RunAM(remove...), 0)
				env.AssertFileAbsent("/workspace/.factory/skills/review")
				env.AssertFileExists("/workspace/.factory/skills/other/SKILL.md")
				if config := env.ReadFile("/workspace/.agents/config.yaml"); !strings.Contains(config, "review") || !strings.Contains(config, "bundle") {
					t.Fatalf("second remove must exclude the collection member and keep the collection:\n%s", config)
				}
				env.AssertExitCode(env.RunAM("sync", "--check"), 0)
			})
		}
	}
}
