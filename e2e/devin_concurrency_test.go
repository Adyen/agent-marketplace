package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	codeMaterializeDevinAggregateWrite = "materialize.devin.aggregate-write"
	codeOwnershipLockTimeout           = "ownership.lock-timeout"
)

// decision: The design document imported internal/ownership and
// internal/harness into the e2e test, but the e2e module is deliberately
// dependency-free (e2e/go.mod has no require block). This test therefore
// verifies the durable record by running sync --check with the winning org
// config and by inspecting the filesystem directly, using only the stdlib.
func TestE2EDevinConcurrentSync(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	// Use a sandbox state directory so the real binaries write durable state
	// under the temp sandbox.
	t.Setenv("XDG_STATE_HOME", filepath.Join(env.home, ".local", "state"))

	// Use two independent registries so the concurrent syncs do not contend on
	// the git cache of a single registry URL. The property under test is the
	// aggregate-file ownership lock, not clone-or-pull concurrency.
	alphaURL := env.CreateBareRepo("devin-b6-alpha", RegistryFilesEx(nil, nil, nil, []MCPServer{
		{Name: "alpha", Command: "alpha-bin"},
	}))
	betaURL := env.CreateBareRepo("devin-b6-beta", RegistryFilesEx(nil, nil, nil, []MCPServer{
		{Name: "beta", Command: "beta-bin"},
	}))

	env.WriteGlobalConfig(`
harnesses:
  - devin
`)

	baseOrg := `apiVersion: agent-marketplace/v3
harnesses:
  - devin
registries:
  - url: %s
    ref: main
    mcp:
      - %s
`
	env.WriteFile("/etc/agent-marketplace/devin-alpha/config.yaml", fmt.Sprintf(baseOrg, alphaURL, "alpha"))
	env.WriteFile("/etc/agent-marketplace/devin-beta/config.yaml", fmt.Sprintf(baseOrg, betaURL, "beta"))

	// Given: A FIFO makes the first sync block while reading the aggregate, which is
	// inside the ownership critical section. Once its writer opens, replace the
	// pathname with a regular file so the later backup and CAS reads do not block.
	// This constructs contention instead of hoping two process startups happen to
	// reach a millisecond-long section together.
	const seed = "{\n  \"mcpServers\": {\n    \"handwritten\": { \"command\": \"user-owned-binary\" }\n  }\n}\n"
	mcpPath, err := env.resolvePath("/root/.config/devin/mcp_config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(mcpPath, 0o600); err != nil {
		t.Fatal(err)
	}

	// When: Run the alpha sync into the FIFO, then overlap it with a fail-fast beta sync.
	results := make([]ExecResult, 2)
	orgAlpha, err := env.resolvePath("/etc/agent-marketplace/devin-alpha/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	orgBeta, err := env.resolvePath("/etc/agent-marketplace/devin-beta/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	orgs := []string{orgAlpha, orgBeta}
	for _, org := range orgs {
		env.MustLockOrganizationConfig(org)
	}
	alphaDone := make(chan struct{})
	go func() {
		defer close(alphaDone)
		results[0] = env.RunAMWithEnv(map[string]string{
			"AGENT_MARKETPLACE_V3_ORG_CONFIG": orgs[0],
		}, "sync")
	}()

	fifoWriter := openFIFOWriter(t, mcpPath, alphaDone, &results[0])
	seedPath := mcpPath + ".seed"
	if err := os.WriteFile(seedPath, []byte(seed), 0o600); err != nil {
		_ = fifoWriter.Close()
		t.Fatal(err)
	}
	if err := os.Rename(seedPath, mcpPath); err != nil {
		_ = fifoWriter.Close()
		t.Fatal(err)
	}

	// Alpha now holds the root lock and is blocked on fifoWriter. A fail-fast
	// beta sync must therefore reach the same section and be refused by that lock.
	contended := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_V3_ORG_CONFIG":          orgs[1],
		"AGENT_MARKETPLACE_OWNERSHIP_LOCK_TIMEOUT": "0",
	}, "sync")
	if contended.ExitCode == 0 || !strings.Contains(contended.Combined, codeOwnershipLockTimeout) {
		_ = fifoWriter.Close()
		t.Fatalf("overlapping sync was not refused with %s (exit=%d):\n%s",
			codeOwnershipLockTimeout, contended.ExitCode, contended.Combined)
	}
	if _, err := fifoWriter.Write([]byte(seed)); err != nil {
		_ = fifoWriter.Close()
		t.Fatal(err)
	}
	if err := fifoWriter.Close(); err != nil {
		t.Fatal(err)
	}
	waitForProcess(t, "alpha", alphaDone)

	// After the forced overlap proves exclusion, beta succeeds normally and
	// exercises the serialized stale-entry prune that the original test covered.
	results[1] = env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_V3_ORG_CONFIG": orgs[1],
	}, "sync")

	// Then: Both serialized syncs succeed, preserve user data, and leave consistent state.
	for i, r := range results {
		t.Logf("sync %d exit=%d output:\n%s", i, r.ExitCode, r.Combined)
		if r.ExitCode != 0 {
			t.Fatalf("sync %d exited %d:\n%s", i, r.ExitCode, r.Combined)
		}
		if strings.Contains(r.Combined, codeMaterializeDevinAggregateWrite) {
			t.Fatalf("sync %d emitted %s", i, codeMaterializeDevinAggregateWrite)
		}
	}

	got := env.ReadFile("/root/.config/devin/mcp_config.json")
	if !strings.Contains(got, "user-owned-binary") {
		t.Fatal("handwritten entry was not preserved")
	}
	hasAlpha := strings.Contains(got, "alpha-bin")
	hasBeta := strings.Contains(got, "beta-bin")
	if !hasAlpha && !hasBeta {
		t.Fatal("neither managed entry present")
	}
	if hasAlpha && hasBeta {
		t.Fatal("both managed entries present; the loser should have pruned the winner's stale owned entry")
	}

	// Verify the durable record describes the live file by running sync --check
	// with the winning org config: a mismatch would report drift and exit 1.
	winnerOrg := orgs[0]
	if hasBeta {
		winnerOrg = orgs[1]
	}
	check := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_V3_ORG_CONFIG": winnerOrg,
	}, "sync", "--check")
	if check.ExitCode != 0 {
		t.Fatalf("sync --check with the winning org exited %d:\n%s", check.ExitCode, check.Combined)
	}
	if strings.Contains(check.Combined, codeMaterializeDevinAggregateWrite) {
		t.Fatalf("sync --check emitted %s", codeMaterializeDevinAggregateWrite)
	}

	// The winning org's sync should now be a no-op because the record already
	// describes the file.
	hashBefore := sha256OfFile(t, mcpPath)
	noop := env.RunAMWithEnv(map[string]string{
		"AGENT_MARKETPLACE_V3_ORG_CONFIG": winnerOrg,
	}, "sync")
	if noop.ExitCode != 0 {
		t.Fatalf("winning-org sync exited %d:\n%s", noop.ExitCode, noop.Combined)
	}
	if strings.Contains(noop.Combined, codeMaterializeDevinAggregateWrite) {
		t.Fatalf("winning-org sync emitted %s", codeMaterializeDevinAggregateWrite)
	}
	hashAfter := sha256OfFile(t, mcpPath)
	if hashBefore != hashAfter {
		t.Fatalf("winning-org sync rewrote mcp_config.json; the durable record did not describe the live file")
	}

	// No staged files or left-over journals should survive.
	staged, err := filepath.Glob(mcpPath + ".am-staged-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("staged files survived: %v", staged)
	}
	stateRoot := filepath.Join(env.home, ".local", "state", "agent-marketplace")
	if found := findAnyFile(t, stateRoot, "journal.json"); found != "" {
		t.Fatalf("completed sync left a journal: %s", found)
	}
}

func sha256OfFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func findAnyFile(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if filepath.Base(path) == name {
			found = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return found
}
