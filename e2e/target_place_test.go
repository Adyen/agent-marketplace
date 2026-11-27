package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/gitidentity"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/creack/pty"
)

func TestTargetPlacePublicLifecycle(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	tree, err := readCommandTree(env.binary)
	if err != nil {
		t.Fatal(err)
	}
	for _, hidden := range []string{"target approve --yes", "target retire --yes"} {
		if slices.Contains(tree.Flags, hidden) {
			t.Fatalf("unsupported confirmation flag %q is public", hidden)
		}
	}

	profile := []byte(`apiVersion: agent-marketplace.io/target-profile/v1
id: example-app
label: Example App
platforms: [macos, linux]
roots: {user: {override: EXAMPLE_APP_HOME, default: {macos: {base: home, path: .config/example-app}, linux: {base: xdg-config, path: example-app}}, allowed_bases: [home, xdg-config]}}
host: {executable: example-app-check, version_probe: {argv: [--version], pattern: '(\d+\.\d+\.\d+)'}, verified: [{range: ">=1.0.0 <2.0.0", date: 2026-11-12, evidence: fixture}]}
checks: {initial-load: {scope: eager-package-load, argv: [check, "{subject}"], subject: {under: mods, component: package-name}}}
kinds: {mod: {deliver: place, into: [mods/], verify: initial-load}}
`)
	const packageRevision = `target_id: example-app
kind: example-app/mod
name: sample
files:
  - source_path: guide.md
    destination_path: mods/sample/guide.md
`
	const profilePath = "-example-app.yaml"
	env.CreateBareRepo("place-e2e", map[string]string{
		profilePath: strings.TrimSuffix(string(profile), "\n"),
		"packages/example-app/mod/sample/agent-marketplace.yaml": packageRevision,
		"packages/example-app/mod/sample/payload/guide.md":       "place package guide",
		"skills/place-fixture/SKILL.md":                          "---\nname: place-fixture\ndescription: E2E source layout fixture\n---\n",
	})
	gitServer := newGitHTTPServer(t, env.repos)
	defer gitServer.Close()
	origin := gitServer.URL + "/place-e2e.git"
	canonicalOrigin, err := gitidentity.CanonicalV1(origin)
	if err != nil {
		t.Fatal(err)
	}
	profileDigest := sha256.Sum256(profile)
	profileRevision := "sha256:" + hex.EncodeToString(profileDigest[:])
	profileSourceID := "git:" + url.QueryEscape(canonicalOrigin) + "@main#" + url.QueryEscape(profilePath)

	marketplaceConfig := writeMarketplaceConfig(t, t.TempDir(), "place-e2e", "place-source", "place-source-id", origin)
	configFile, err := os.OpenFile(marketplaceConfig, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := fmt.Fprintf(configFile, `
host_package_assessment_profiles:
  - registry_id: place-source-id
    kind: example-app/mod
    name: sample
    profile_identity:
      target_id: example-app
      source_id: %q
      revision_digest: %q
`, profileSourceID, profileRevision)
	closeErr := configFile.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	reindex := env.RunAM("serve", "reindex", "--config", marketplaceConfig)
	if reindex.ExitCode != 0 {
		t.Fatalf("Marketplace reindex failed (exit %d):\n%s", reindex.ExitCode, reindex.Combined)
	}
	marketplaceConfigValue, meta, blobs := openPublicGateStores(t, marketplaceConfig)
	t.Cleanup(func() { _ = meta.Close() })
	marketplace := httptest.NewServer(mpapi.NewServer(marketplaceConfigValue, meta, blobs).PublicHandler())
	defer marketplace.Close()

	latestPackageRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		marketplace.URL+"/api/v1/resources/place-source/example-app%2Fmod/sample", nil)
	if err != nil {
		t.Fatal(err)
	}
	latestPackageRequest.Header.Set("X-Agent-Marketplace-Features", mpapi.TargetProfilePlaceFeature)
	latestPackage, err := http.DefaultClient.Do(latestPackageRequest)
	if err != nil {
		t.Fatal(err)
	}
	latestPackageBytes, err := io.ReadAll(latestPackage.Body)
	latestPackage.Body.Close()
	if err != nil || latestPackage.StatusCode != http.StatusOK {
		t.Fatalf("latest package status/body = %d/%s (%v)", latestPackage.StatusCode, latestPackageBytes, err)
	}
	var packageIdentity struct {
		ContentDigest string `json:"content_digest"`
	}
	if err := json.Unmarshal(latestPackageBytes, &packageIdentity); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(packageIdentity.ContentDigest, "sha256:") {
		t.Fatalf("latest package content digest = %q", packageIdentity.ContentDigest)
	}

	env.WriteFile("/root/.agents/config.yaml", fmt.Sprintf(`apiVersion: agent-marketplace/v4
targets: [factory]
sources:
  place-source: {marketplace: %q}
resources:
  place-source:
    include:
      - place-source/target-profile/example-app: {revision: %q}
      - place-source/example-app%%2Fmod/sample: {revision: %q}
`, marketplace.URL, profileRevision, packageIdentity.ContentDigest))
	targetGroup := env.RunAM("target")
	env.AssertExitCode(targetGroup, 0)
	for _, command := range []string{"approve", "enable", "disable", "verify", "retire"} {
		if !strings.Contains(targetGroup.Combined, command) {
			t.Fatalf("target group output omits %q:\n%s", command, targetGroup.Combined)
		}
	}
	initialSync := env.RunAM("sync")
	env.AssertExitCode(initialSync, 0)

	canonicalHome, err := filepath.EvalSymlinks(env.home)
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(canonicalHome, ".config", "example-app")
	if err := os.MkdirAll(filepath.Join(targetRoot, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(targetRoot, "mods", "unowned.txt")
	if err := os.WriteFile(unowned, []byte("keep this file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(targetRoot, "mods", "sample", "guide.md")
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("disabled target package stat error = %v, want absent", err)
	}

	binDir := t.TempDir()
	executable := filepath.Join(binDir, "example-app-check")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$1\" = check ] && [ -f \"$2/guide.md\" ]\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	commandEnv := map[string]string{
		"EXAMPLE_APP_HOME": targetRoot,
		"PATH":             strings.Join([]string{binDir, env.path}, string(os.PathListSeparator)),
	}
	approveRefusal := env.RunAMWithEnv(commandEnv, "target", "approve", "example-app", "--yes")
	if approveRefusal.ExitCode == 0 || !strings.Contains(approveRefusal.Combined, "target.confirmation-required") {
		t.Fatalf("target approve --yes = %+v, want explicit refusal", approveRefusal)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("approve refusal changed installation: %v", err)
	}

	stdout := runTargetPlaceConfirmation(t, env, commandEnv, "target", "approve", "example-app", "--json")
	var approval struct {
		Status  string `json:"status"`
		Binding struct {
			InstallationID string `json:"installation_id"`
		} `json:"binding"`
	}
	if err := json.Unmarshal(stdout, &approval); err != nil {
		t.Fatalf("decode approval stdout: %v\n%s", err, stdout)
	}
	installationID := approval.Binding.InstallationID
	if approval.Status != "approved" || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(installationID) {
		t.Fatalf("want approved status and a 64-character lowercase hex binding.installation_id; got:\n%s", stdout)
	}

	enable := env.RunAM("target", "enable", "example-app")
	env.AssertExitCode(enable, 0)
	sync := env.RunAMWithEnv(commandEnv, "sync")
	if sync.ExitCode != 0 {
		t.Fatalf("sync enabled target failed (exit %d):\n%s", sync.ExitCode, sync.Combined)
	}
	if got, err := os.ReadFile(installed); err != nil || string(got) != "place package guide\n" {
		t.Fatalf("installed package = %q, err=%v", got, err)
	}

	verify := env.RunAMWithEnv(commandEnv, "target", "verify", "example-app%2Fmod/sample", "--installation", installationID)
	if verify.ExitCode != 0 || !strings.Contains(verify.Combined, "Verified example-app/mod/sample") {
		t.Fatalf("target verify failed (exit %d):\n%s", verify.ExitCode, verify.Combined)
	}

	disable := env.RunAM("target", "disable", "example-app")
	env.AssertExitCode(disable, 0)
	if got, err := os.ReadFile(installed); err != nil || string(got) != "place package guide\n" {
		t.Fatalf("disable changed installed package = %q, err=%v", got, err)
	}

	retireRefusal := env.RunAMWithEnv(commandEnv, "target", "retire", installationID, "--yes")
	if retireRefusal.ExitCode == 0 || !strings.Contains(retireRefusal.Combined, "target.confirmation-required") {
		t.Fatalf("target retire --yes = %+v, want explicit refusal", retireRefusal)
	}
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("retire refusal changed installed package: %v", err)
	}
	retirement := runTargetPlaceConfirmation(t, env, commandEnv, "target", "retire", installationID)
	if !bytes.Contains(retirement, []byte("Retired "+installationID)) {
		t.Fatalf("retirement output does not confirm installation ID:\n%s", retirement)
	}
	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("retired package stat error = %v, want absent", err)
	}
	if got, err := os.ReadFile(unowned); err != nil || string(got) != "keep this file\n" {
		t.Fatalf("retirement changed unowned file = %q, err=%v", got, err)
	}
	assertTargetPlaceInvocationCoverage(t)
}

func assertTargetPlaceInvocationCoverage(t *testing.T) {
	t.Helper()
	invocations, err := readCommandInvocations(e2eInvocationDir)
	if err != nil {
		t.Fatal(err)
	}
	covered := coveredRequirements(invocations)
	for _, requirement := range []string{
		"target [v4]",
		"target approve [v4]",
		"target approve --json [v4]",
		"target enable [v4]",
		"target disable [v4]",
		"target verify [v4]",
		"target verify --installation [v4]",
		"target retire [v4]",
	} {
		if !covered[requirement] {
			t.Errorf("public lifecycle did not record successful invocation for %q", requirement)
		}
	}
}

func runTargetPlaceConfirmation(t *testing.T, env *Env, extra map[string]string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, env.binary, args...)
	cmd.Dir = env.workspace
	cmd.Env = subprocessEnvWithOverrides(env.subprocessEnv(), extra)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout // Leave stdin and stderr on the PTY for confirmation.
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 160})
	if err != nil {
		t.Fatalf("start interactive %s: %v", strings.Join(args, " "), err)
	}
	var output synchronizedBuffer
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		_, _ = io.Copy(&output, terminal)
	}()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			<-wait
		}
		_ = terminal.Close()
	}()

	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(output.String(), "Type yes to") {
		if time.Now().After(deadline) {
			t.Fatalf("interactive prompt did not appear for %s:\n%s", strings.Join(args, " "), output.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := terminal.Write([]byte("yes\n")); err != nil {
		t.Fatalf("confirm %s: %v", strings.Join(args, " "), err)
	}
	select {
	case err := <-wait:
		finished = true
		if err != nil {
			t.Fatalf("interactive %s failed: %v\n%s", strings.Join(args, " "), err, output.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("interactive %s timed out:\n%s", strings.Join(args, " "), output.String())
	}
	select {
	case <-drained:
	case <-ctx.Done():
		t.Fatalf("interactive %s exited before PTY output drained:\n%s", strings.Join(args, " "), output.String())
	}
	return stdout.Bytes()
}
