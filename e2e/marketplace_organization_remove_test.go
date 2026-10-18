package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// spec: resource-removal/remove-an-optional-organization-marketplace-collection-member
func TestOrganizationMarketplaceCollectionMemberRemove(t *testing.T) {
	fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("organization member", true))
	for _, choice := range []string{"default", "user", "from-user", "command"} {
		t.Run(choice, func(t *testing.T) {
			consumer := newEnv(t)
			consumer.InitGitRepo()
			consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n")
			consumer.WriteOrgConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org: {marketplace: %s}
resources:
  org:
    include: [behavior-source/collection/behavior-bundle]
`, fixture.server.URL))
			result := consumer.RunAM("sync")
			if result.ExitCode != 0 {
				t.Fatalf("initial sync (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "organization member")
			consumer.AssertFileContains("/root/.factory/commands/command-one.md", "echo behavior")
			organization := consumer.ReadFile("/etc/agent-marketplace/config.yaml")
			kind, name := "skill", "skill-one"
			removed, kept := "/root/.factory/skills/skill-one", "/root/.factory/commands/command-one.md"
			if choice == "command" {
				kind, name = "command", "command-one"
				removed, kept = "/root/.factory/commands/command-one.md", "/root/.factory/skills/skill-one/SKILL.md"
			}
			args := []string{"remove", name, "--kind", kind}
			if choice != "default" {
				args = append(args, "--scope", "user")
			}
			if choice == "from-user" {
				args = append(args, "--from", fixture.server.URL)
			}
			result = consumer.RunAM(args...)
			if result.ExitCode != 0 {
				t.Fatalf("remove member (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			if !strings.Contains(result.Combined, "Excluded "+name) {
				t.Fatalf("missing exclusion confirmation:\n%s", result.Combined)
			}
			var user struct {
				APIVersion string `yaml:"apiVersion"`
				Sources    map[string]struct {
					Marketplace string `yaml:"marketplace"`
				} `yaml:"sources"`
				Resources map[string]struct {
					Exclude []string `yaml:"exclude"`
				} `yaml:"resources"`
			}
			if err := yaml.Unmarshal([]byte(consumer.ReadFile("/root/.agents/config.yaml")), &user); err != nil {
				t.Fatal(err)
			}
			want := "behavior-source/" + kind + "/" + name
			if user.APIVersion != "agent-marketplace/v4" || len(user.Sources) != 1 {
				t.Fatalf("unexpected user config: %#v", user)
			}
			for alias, source := range user.Sources {
				exclusions := user.Resources[alias].Exclude
				if source.Marketplace != fixture.server.URL || len(exclusions) != 1 || exclusions[0] != want {
					t.Fatalf("unexpected exclusion source=%#v resources=%#v", source, user.Resources)
				}
			}
			consumer.AssertFileAbsent(removed)
			consumer.AssertFileExists(kept)
			if after := consumer.ReadFile("/etc/agent-marketplace/config.yaml"); after != organization {
				t.Fatal("member removal changed organization configuration")
			}
			consumer.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")
			for _, args := range [][]string{{"sync"}, {"sync", "--check"}} {
				result = consumer.RunAM(args...)
				if result.ExitCode != 0 {
					t.Fatalf("%v after removal (exit %d):\n%s", args, result.ExitCode, result.Combined)
				}
				consumer.AssertFileAbsent(removed)
				consumer.AssertFileExists(kept)
			}
			consumer.WriteOrgConfig(organization + "\n    required: true\n")
			result = consumer.RunAM("sync")
			if result.ExitCode != 0 {
				t.Fatalf("required collection sync (exit %d):\n%s", result.ExitCode, result.Combined)
			}
			consumer.AssertFileExists(removed)
			before := consumer.ReadFile("/root/.agents/config.yaml")
			result = consumer.RunAM(args...)
			if result.ExitCode == 0 {
				t.Fatalf("required member removal succeeded:\n%s", result.Combined)
			}
			if after := consumer.ReadFile("/root/.agents/config.yaml"); after != before {
				t.Fatal("required member removal changed user configuration")
			}
			consumer.AssertFileExists(removed)
		})
	}
}

// spec: resource-removal/organization-member-opt-out-disambiguates-kind-and-scope
func TestOrganizationMarketplaceMemberKindAmbiguity(t *testing.T) {
	files := marketplaceBehaviorFiles("organization member", true)
	files["commands/skill-one.md"] = (Command{Name: "skill-one", Command: "echo same-name-command"}).CommandFile()
	files["collections/behavior-bundle/COLLECTION.yaml"] += "  - name: skill-one\n    kind: command\n"
	fixture := newMarketplaceBehaviorFixture(t, files)
	for _, from := range []bool{false, true} {
		t.Run(fmt.Sprintf("from=%t", from), func(t *testing.T) {
			consumer := fixture.newConsumer(t, fixture.server.URL)
			consumer.WriteOrgConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org: {marketplace: %s}\nresources:\n  org: [behavior-source/collection/behavior-bundle]\n", fixture.server.URL))
			if result := consumer.RunAM("sync"); result.ExitCode != 0 {
				t.Fatalf("initial sync:\n%s", result.Combined)
			}
			userBefore := consumer.ReadFile("/root/.agents/config.yaml")
			args := []string{"remove", "skill-one", "--scope", "user"}
			if from {
				args = append(args, "--from", fixture.server.URL)
			}
			if result := consumer.RunAM(args...); result.ExitCode == 0 || !strings.Contains(result.Combined, "--kind") {
				t.Fatalf("expected kind ambiguity:\n%s", result.Combined)
			}
			if after := consumer.ReadFile("/root/.agents/config.yaml"); after != userBefore {
				t.Fatal("ambiguous removal changed user configuration")
			}
			consumer.AssertFileExists("/root/.factory/skills/skill-one/SKILL.md")
			consumer.AssertFileExists("/root/.factory/commands/skill-one.md")
			args = append(args, "--kind", "command")
			if result := consumer.RunAM(args...); result.ExitCode != 0 {
				t.Fatalf("kind-qualified removal:\n%s", result.Combined)
			}
			consumer.AssertFileAbsent("/root/.factory/commands/skill-one.md")
			consumer.AssertFileContains("/root/.factory/skills/skill-one/SKILL.md", "organization member")
		})
	}
}
