package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

var repairSchemas = []string{"v2", "v4"}

// repairConflict is a repository whose Codex MCP entry collides with a
// handwritten value, so doctor --repair can resolve it only after an
// interactive confirmation.
type repairConflict struct {
	env       *Env
	registry  string
	aggregate string
	original  string
}

func newRepairConflict(t *testing.T, schema string) repairConflict {
	t.Helper()
	env := newCodexEnv(t)
	url := mcpRegistry(env, "repair-policy", "managed", "locked-binary")
	switch schema {
	case "v2":
		env.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [codex]\nregistries: []")
		env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v2\nregistries:\n  - url: %s\n    ref: main\n    source_harness: claude\n    mcp:\n      - managed\n", url))
	case "v4":
		env.WriteGlobalConfig("apiVersion: agent-marketplace/v4\nharnesses: [codex]\n")
		env.WriteRepoConfig(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  tools: {git: '%s', ref: main, source_harness: claude}\nresources:\n  tools: {include: [mcp/managed]}\n", url))
	default:
		t.Fatalf("unknown schema %q", schema)
	}
	aggregate := filepath.Join(env.workspace, ".codex", "config.toml")
	original := "[mcp_servers.managed]\ncommand = 'handwritten-binary'\n"
	if err := os.MkdirAll(filepath.Dir(aggregate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aggregate, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	return repairConflict{env: env, registry: strings.TrimPrefix(url, "file://"), aggregate: aggregate, original: original}
}

func (c repairConflict) assertAggregateUnchanged(t *testing.T, when string) {
	t.Helper()
	if got := readFile(t, c.aggregate); got != c.original {
		t.Fatalf("%s changed the Codex aggregate:\n%s", when, got)
	}
}

// advance pushes a registry commit that changes the managed command, so the
// branch no longer matches the locked revision.
func (c repairConflict) advance(t *testing.T, command string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	gitRun(t, "", "clone", "-q", c.registry, work)
	if err := os.WriteFile(filepath.Join(work, ".mcp.json"), []byte(MergeMCPFile([]MCPServer{{Name: "managed", Command: command}})), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "-c", "user.name=E2E", "-c", "user.email=e2e@example.com", "commit", "-qam", "move managed command")
	gitRun(t, work, "push", "-q", "origin", "main")
}

// doctor --repair builds its desired state only from an exact, valid Git lock;
// it never falls back to the mutable registry cache.
// spec: owned-entry-reconciliation/missing-or-invalid-git-acquisition-evidence-refuses-repair
func TestE2EDoctorRepairRequiresValidGitLock(t *testing.T) {
	for _, schema := range repairSchemas {
		t.Run(schema, func(t *testing.T) {
			c := newRepairConflict(t, schema)
			lock := filepath.Join(c.env.workspace, ".agents", "lock.yaml")

			missing := c.env.RunAM("doctor", "--repair")
			if missing.ExitCode == 0 || !strings.Contains(missing.Combined, "lock.missing") {
				t.Fatalf("doctor --repair accepted a missing Git lock (exit %d):\n%s", missing.ExitCode, missing.Combined)
			}
			if _, err := os.Lstat(lock); !os.IsNotExist(err) {
				t.Fatalf("doctor --repair created the missing lock: %v", err)
			}

			const malformed = "not: [valid\n"
			if err := os.WriteFile(lock, []byte(malformed), 0o644); err != nil {
				t.Fatal(err)
			}
			invalid := c.env.RunAM("doctor", "--repair")
			if invalid.ExitCode == 0 || !strings.Contains(invalid.Combined, "lock.invalid") {
				t.Fatalf("doctor --repair accepted a malformed Git lock (exit %d):\n%s", invalid.ExitCode, invalid.Combined)
			}
			if got := readFile(t, lock); got != malformed {
				t.Fatalf("doctor --repair rewrote the malformed lock:\n%s", got)
			}
			c.assertAggregateUnchanged(t, "refused repair")
		})
	}
}

// A confirmed replacement installs the locked revision's value even after the
// registry branch moved, and leaves the lock as it was.
// spec: owned-entry-reconciliation/moving-a-git-ref-does-not-change-repair-values
func TestE2EDoctorRepairReplacesFromLockedRevision(t *testing.T) {
	for _, schema := range repairSchemas {
		t.Run(schema, func(t *testing.T) {
			c := newRepairConflict(t, schema)
			c.env.MustRunAM("lock", "--scope", "repo")
			lock := filepath.Join(c.env.workspace, ".agents", "lock.yaml")
			locked := readFile(t, lock)
			c.advance(t, "moved-binary")

			repair := runAMInTTY(t, c.env, "doctor", "--repair", "[y/N]", "y\n")
			assertRepairResult(t, repair, 0, `replaced mcp-server "managed"`)
			if got := readFile(t, c.aggregate); !strings.Contains(got, "locked-binary") || strings.Contains(got, "moved-binary") || strings.Contains(got, "handwritten-binary") {
				t.Fatalf("repair did not install the locked revision's value:\n%s", got)
			}
			if got := readFile(t, lock); got != locked {
				t.Fatalf("repair changed the Git lock:\nbefore:\n%s\nafter:\n%s", locked, got)
			}
		})
	}
}

// Policy inputs that change while doctor --repair waits for confirmation
// abort the repair without touching the aggregate.
// spec: owned-entry-reconciliation/configuration-changes-during-confirmation
// spec: owned-entry-reconciliation/organization-acquisition-lock-appears-during-confirmation
func TestE2EDoctorRepairRefusesPolicyChangeDuringConfirmation(t *testing.T) {
	for _, schema := range repairSchemas {
		t.Run(schema+"/repository config edited", func(t *testing.T) {
			c := newRepairConflict(t, schema)
			c.env.MustRunAM("lock", "--scope", "repo")
			repoConfig := filepath.Join(c.env.workspace, ".agents", "config.yaml")

			repair := runAMInTTYAtPrompt(t, c.env, "doctor", "--repair", "[y/N]", "y\n", func() {
				if err := os.WriteFile(repoConfig, []byte(readFile(t, repoConfig)+"harnesses: [codex, factory]\n"), 0o644); err != nil {
					t.Error(err)
				}
			})
			if repair.ExitCode == 0 || !strings.Contains(repair.Combined, "policy changed") {
				t.Fatalf("doctor --repair ignored a config edit during confirmation (exit %d):\n%s", repair.ExitCode, repair.Combined)
			}
			c.assertAggregateUnchanged(t, "repair after a config edit")
		})
		t.Run(schema+"/organization lock deployed", func(t *testing.T) {
			fixture := newMarketplaceBehaviorFixture(t, marketplaceBehaviorFiles("Organization body", true))
			c := newRepairConflict(t, schema)
			c.env.WriteOrgConfigWithMode(fmt.Sprintf(`apiVersion: agent-marketplace/v4
sources:
  org:
    marketplace: %s
allowed_marketplaces:
  - '^http://127\.0\.0\.1:[0-9]+$'
resources:
  org:
    include:
      - behavior-source/collection/behavior-bundle
`, fixture.server.URL), 0o644)
			c.env.MustRunAM("lock", "--scope", "repo")
			orgLock := c.env.mustResolvePath("/etc/agent-marketplace/lock.yaml")
			const deployed = "present: invalid\n"

			repair := runAMInTTYAtPrompt(t, c.env, "doctor", "--repair", "[y/N]", "y\n", func() {
				if err := os.WriteFile(orgLock, []byte(deployed), 0o644); err != nil {
					t.Error(err)
				}
			})
			if repair.ExitCode == 0 || !strings.Contains(repair.Combined, "policy changed") {
				t.Fatalf("doctor --repair ignored an organization lock deployed during confirmation (exit %d):\n%s", repair.ExitCode, repair.Combined)
			}
			c.assertAggregateUnchanged(t, "repair after an organization lock appeared")
			if got := readFile(t, orgLock); got != deployed {
				t.Fatalf("doctor --repair changed the deployed organization lock:\n%s", got)
			}
		})
	}
}

// When the organization's Marketplace cannot be reached and no organization
// lock is deployed, doctor reports the acquisition failure, doctor --repair
// refuses to act on an incomplete selection, while gc uses local reachability.
// spec: owned-entry-reconciliation/organization-acquisition-failure-refuses-repair
func TestE2EOrganizationMarketplaceAcquisitionFailure(t *testing.T) {
	for _, schema := range repairSchemas {
		t.Run(schema, func(t *testing.T) {
			var requests atomic.Int32
			unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			t.Cleanup(unavailable.Close)
			env := newEnv(t)
			env.InitGitRepo()
			registry := env.CreateBareRepo("gc-source", RegistryFilesEx([]Skill{{Name: "review", Description: "Review changes"}}, nil, nil, nil))
			selected := fmt.Sprintf("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  tools: {git: '%s', ref: main}\nresources:\n  tools: {include: [skill/review]}\n", registry)
			dropped := "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n"
			if schema == "v2" {
				selected = fmt.Sprintf("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries:\n  - url: %s\n    ref: main\n    skills:\n      include: [review]\n", registry)
				dropped = "apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []\n"
			}
			env.WriteGlobalConfig(selected)
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.WriteGlobalConfig(dropped)
			env.AssertExitCode(env.RunAM("sync"), 0)
			env.WriteOrgConfigWithMode(fmt.Sprintf("apiVersion: agent-marketplace/v4\nsources:\n  org:\n    marketplace: %s\nresources:\n  org:\n    include:\n      - behavior-source/collection/behavior-bundle\n", unavailable.URL), 0o644)

			doctor := env.RunAM("doctor")
			if !strings.Contains(doctor.Combined, "acquire transient organization Marketplace lock") || strings.Contains(doctor.Combined, "v4.marketplace-lock-required") {
				t.Fatalf("doctor did not report the organization acquisition failure (exit %d):\n%s", doctor.ExitCode, doctor.Combined)
			}
			repair := env.RunAM("doctor", "--repair")
			if repair.ExitCode == 0 || !strings.Contains(repair.Combined, "acquire transient organization Marketplace lock") {
				t.Fatalf("doctor --repair acted without the organization's selection (exit %d):\n%s", repair.ExitCode, repair.Combined)
			}
			beforeGC := requests.Load()
			gc := env.RunAM("gc")
			env.AssertExitCode(gc, 0)
			if requests.Load() != beforeGC {
				t.Fatal("gc contacted the unavailable Marketplace instead of using local reachability")
			}
			env.AssertFileAbsent("/etc/agent-marketplace/lock.yaml")

			if err := os.Remove(env.mustResolvePath("/etc/agent-marketplace/config.yaml")); err != nil {
				t.Fatal(err)
			}
			preview := env.RunAM("gc", "--dry-run")
			env.AssertExitCode(preview, 0)
			env.AssertOutputContains(preview, "Would remove 0 cache entries")
		})
	}
}
