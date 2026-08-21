package materialize

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/registryfs"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/diag"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// The exploit these tests reproduce: a registry controls the bytes of its own
// support files, and materialization copies those bytes into the skill cache and
// into generated harness output. Reading them by path followed a
// registry-planted symlink out of the registry, so `sync` published host files
// the user could read into a directory their agent harness reads.

const sentinelText = "SENTINEL-PRIVATE-KEY-MATERIAL"

// hostSecret creates a file outside any registry, standing in for whatever the
// invoking user can read (an SSH key, a token file, /etc/shadow on a lax host).
func hostSecret(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sentinel")
	if err := os.WriteFile(path, []byte(sentinelText), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertSentinelAbsent fails when the sentinel bytes appear anywhere under the
// given roots, which cover both the generated output (repo) and the skill cache
// (HOME). Asserting absence rather than "the copy failed" is what makes this a
// disclosure regression: any future code path that reads the source by name
// again would leak into one of these trees.
func assertSentinelAbsent(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil //nolint:nilerr // an unreadable path cannot hold a leaked copy
			}
			if d.IsDir() {
				return nil
			}
			info, err := os.Lstat(path)
			if err != nil {
				return nil //nolint:nilerr
			}
			if info.Mode()&os.ModeSymlink != 0 {
				// A dangling or escaping output symlink is itself a finding.
				if target, rerr := os.Readlink(path); rerr == nil && strings.Contains(target, "sentinel") {
					t.Errorf("output %s links to the host sentinel (%s)", path, target)
				}
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil //nolint:nilerr
			}
			if bytes.Contains(data, []byte(sentinelText)) {
				t.Errorf("host sentinel content was materialized into %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// assertRefused requires that a run failed closed for containment reasons.
// materialize.Run reports most failures as error diagnostics (which
// pipeline.Sync promotes to command errors) and only a few as a returned error,
// so both are accepted as long as the reason is containment.
func assertRefused(t *testing.T, report *Report, err error, want string) {
	t.Helper()
	if err != nil {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Run error = %v, want it to mention %q", err, want)
		}
		return
	}
	if report == nil {
		t.Fatal("Run returned neither a report nor an error")
	}
	if !diag.HasErrors(report.Diags) {
		t.Fatalf("run succeeded without an error diagnostic; diags = %v", report.Diags)
	}
	for _, d := range report.Diags {
		if d.Severity == diag.SeverityError && strings.Contains(d.Message, want) {
			return
		}
	}
	t.Fatalf("no error diagnostic mentioning %q; diags = %v", want, report.Diags)
}

// registrySkill writes a registry containing one skill and returns the registry
// root and the skill directory.
func registrySkill(t *testing.T, name string) (registryRoot, skillDir string) {
	t.Helper()
	registryRoot = t.TempDir()
	skillDir = filepath.Join(registryRoot, "skills", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\nBody of " + name
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return registryRoot, skillDir
}

func resolvedSkillFromRegistry(t *testing.T, name, registryRoot, skillDir string, target hcc.Target) []resolver.ResolvedArtifact {
	t.Helper()
	irSkill := ir.NewSkill(name, name+" skill", "Body of "+name, nil, nil,
		ir.SourceRef{File: filepath.Join(skillDir, "SKILL.md"), Line: 1})
	return []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{
			Name:         name,
			Path:         skillDir,
			Resource:     irSkill,
			SourcePolicy: registryfs.Local(registryRoot),
		},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{target},
	}}
}

func TestRunRefusesSkillSupportSymlinkEscapingRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	registryRoot, skillDir := registrySkill(t, "leaky")
	if err := os.Symlink(sentinel, filepath.Join(skillDir, "notes.md")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, resolvedSkillFromRegistry(t, "leaky", registryRoot, skillDir, hcc.TargetClaude))
	assertRefused(t, report, err, "registry root")
	assertSentinelAbsent(t, repoRoot, homeDir)
}

// TestRunRefusesSkillSupportSymlinkInNestedDir covers the same attack one level
// down, where the escaping link is not visible in the skill's top directory.
func TestRunRefusesSkillSupportSymlinkInNestedDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	registryRoot, skillDir := registrySkill(t, "nested")
	scripts := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "ok.sh"), []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A relative traversal, so the exploit does not depend on absolute paths.
	rel, err := filepath.Rel(scripts, sentinel)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(scripts, "helper.sh")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, resolvedSkillFromRegistry(t, "nested", registryRoot, skillDir, hcc.TargetClaude))
	assertRefused(t, report, err, "registry root")
	assertSentinelAbsent(t, repoRoot, homeDir)
}

// TestCheckRefusesSkillSupportSymlinkEscapingRegistry pins the dry-run path:
// `sync --check` hashes the source tree it would copy, so it has to refuse the
// same content rather than reading it to compute a digest.
func TestCheckRefusesSkillSupportSymlinkEscapingRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	registryRoot, skillDir := registrySkill(t, "checked")
	if err := os.Symlink(sentinel, filepath.Join(skillDir, "notes.md")); err != nil {
		t.Fatal(err)
	}

	fileModes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repoRoot,
		resolvedSkillFromRegistry(t, "checked", registryRoot, skillDir, hcc.TargetClaude),
		WithMaterializationModes(fileModes), WithDryRun())
	assertRefused(t, report, err, "registry root")
	assertSentinelAbsent(t, repoRoot, homeDir)
}

// TestRunFileModeRefusesSkillSupportSymlinkEscapingRegistry covers file
// (non-symlink) materialization, where support bytes are copied twice: registry
// to cache, then cache to output.
func TestRunFileModeRefusesSkillSupportSymlinkEscapingRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	registryRoot, skillDir := registrySkill(t, "copied")
	if err := os.Symlink(sentinel, filepath.Join(skillDir, "notes.md")); err != nil {
		t.Fatal(err)
	}

	fileModes := config.MaterializationModes{Project: config.MaterializationModeFile, Machine: config.MaterializationModeFile}
	report, err := Run(context.Background(), repoRoot,
		resolvedSkillFromRegistry(t, "copied", registryRoot, skillDir, hcc.TargetClaude),
		WithMaterializationModes(fileModes))
	assertRefused(t, report, err, "registry root")
	assertSentinelAbsent(t, repoRoot, homeDir)
}

// TestRunAllowsContainedRelativeSupportSymlink is the counterweight: sharing one
// support tree between resources in the same registry stays supported, so the
// fix is containment rather than a blanket symlink ban on local registries.
func TestRunAllowsContainedRelativeSupportSymlink(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	registryRoot, skillDir := registrySkill(t, "shared")
	if err := os.WriteFile(filepath.Join(registryRoot, "common.sh"), []byte("#!/bin/sh\necho common\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../common.sh", filepath.Join(skillDir, "common.sh")); err != nil {
		t.Fatal(err)
	}

	report, err := Run(context.Background(), repoRoot, resolvedSkillFromRegistry(t, "shared", registryRoot, skillDir, hcc.TargetClaude))
	if err != nil {
		t.Fatalf("sync refused an intra-registry support symlink: %v", err)
	}
	if diag.HasErrors(report.Diags) {
		t.Fatalf("unexpected diagnostics: %v", report.Diags)
	}
	link := filepath.Join(repoRoot, ".claude", "skills", "shared")
	target, ok := existingSymlinkTarget(link)
	if !ok {
		t.Fatalf("%s is not a symlink", link)
	}
	data, err := os.ReadFile(filepath.Join(target, "common.sh"))
	if err != nil || !strings.Contains(string(data), "echo common") {
		t.Fatalf("shared support file not materialized: %q %v", data, err)
	}
}

// TestRunRefusesContainedSupportSymlinkFromRemoteRegistry is the fixture above
// with one field changed: the tree is remote. Nothing in a clone this tool
// produced needs a symlink, so a remote registry is refused one even when it is
// contained — and the refusal has to happen here, at the copy, because the eager
// walk that ran at discovery time cannot speak for the bytes `sync` reads
// afterwards. That is what carrying the policy on the resolved artifact buys.
func TestRunRefusesContainedSupportSymlinkFromRemoteRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	registryRoot, skillDir := registrySkill(t, "shared")
	if err := os.WriteFile(filepath.Join(registryRoot, "common.sh"), []byte("#!/bin/sh\necho common\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../common.sh", filepath.Join(skillDir, "common.sh")); err != nil {
		t.Fatal(err)
	}

	resolved := resolvedSkillFromRegistry(t, "shared", registryRoot, skillDir, hcc.TargetClaude)
	resolved[0].SourcePolicy = registryfs.Remote(registryRoot)

	report, err := Run(context.Background(), repoRoot, resolved)
	assertRefused(t, report, err, "remote registries do not allow symlinks")
}

// TestRunRefusesSupportTreeReachedThroughSymlinkedResourceDir pins the
// no-provenance fallback at the layer that uses it: an artifact with no registry
// root confines reads to its own directory, and a symlinked directory would make
// that "confinement" resolve to the link's target instead.
func TestRunRefusesSupportTreeReachedThroughSymlinkedResourceDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	_, realDir := registrySkill(t, "aliased")
	if err := os.WriteFile(filepath.Join(realDir, "notes.md"), []byte(sentinelText), 0o600); err != nil {
		t.Fatal(err)
	}
	// The resource is addressed through a link to its directory, and nothing
	// vouches for a registry root that contains it.
	linkDir := filepath.Join(t.TempDir(), "aliased")
	if err := os.Symlink(filepath.Dir(sentinel), linkDir); err != nil {
		t.Fatal(err)
	}

	resolved := resolvedSkillFromRegistry(t, "aliased", "", linkDir, hcc.TargetClaude)
	resolved[0].SourcePolicy = registryfs.Policy{}

	report, err := Run(context.Background(), repoRoot, resolved)
	assertRefused(t, report, err, "symlink")
	assertSentinelAbsent(t, repoRoot, homeDir)
}

func TestRunSkipsHookSupportSymlinkEscapingRegistry(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()
	sentinel := hostSecret(t)

	registryRoot := t.TempDir()
	hookDir := filepath.Join(registryRoot, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDir, "keep.sh"), []byte("#!/bin/sh\necho keep\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(hookDir, "id_rsa")); err != nil {
		t.Fatal(err)
	}

	hook := ir.NewHookWithMatcher("post-tool-use-leak", "PostToolUse", "Edit",
		"${DROID_PLUGIN_ROOT}/hooks/keep.sh", "command", nil,
		ir.SourceRef{File: filepath.Join(hookDir, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{
			Name:         "post-tool-use-leak",
			Resource:     hook,
			Path:         filepath.Join(hookDir, "hooks.json"),
			SourcePolicy: registryfs.Local(registryRoot),
		},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}

	report, err := Run(context.Background(), repoRoot, resolved)
	if err != nil {
		t.Fatalf("hook sync failed: %v", err)
	}
	if !diag.HasCode(report.Diags, diag.CodeMaterializeRegistrySourceRefused) {
		t.Fatalf("no %s diagnostic; got %v", diag.CodeMaterializeRegistrySourceRefused, report.Diags)
	}
	// The legitimate support file still lands, so one refused entry does not
	// silently disable the hook's own scripts.
	hooksDir := filepath.Join(repoRoot, ".factory", "hooks")
	if _, err := os.Lstat(filepath.Join(hooksDir, "keep.sh")); err != nil {
		t.Fatalf("legitimate hook support file was not materialized: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hooksDir, "id_rsa")); !os.IsNotExist(err) {
		t.Fatalf("refused hook support file was materialized: %v", err)
	}
	assertSentinelAbsent(t, repoRoot, homeDir)
}

// TestRunDoesNotHangOnSkillSupportFIFO is the regression for the confirmed hang:
// reading a FIFO with no writer blocks in open(2) forever, so a registry could
// wedge every sync until the process was killed.
func TestRunDoesNotHangOnSkillSupportFIFO(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	registryRoot, skillDir := registrySkill(t, "trap")
	mkfifo(t, filepath.Join(skillDir, "pipe"))

	resolved := resolvedSkillFromRegistry(t, "trap", registryRoot, skillDir, hcc.TargetClaude)
	type outcome struct {
		report *Report
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := Run(context.Background(), repoRoot, resolved)
		done <- outcome{report: report, err: err}
	}()
	select {
	case got := <-done:
		assertRefused(t, got.report, got.err, "unsupported file type")
	case <-time.After(30 * time.Second):
		t.Fatal("sync blocked on a FIFO in a skill support tree")
	}
}

// TestRunDoesNotHangOnHookSupportFIFO covers the same trap on the hook path,
// where a read failure is a diagnostic rather than a hard failure.
func TestRunDoesNotHangOnHookSupportFIFO(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	repoRoot := t.TempDir()

	registryRoot := t.TempDir()
	hookDir := filepath.Join(registryRoot, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mkfifo(t, filepath.Join(hookDir, "pipe"))

	hook := ir.NewHookWithMatcher("pre-tool-use-trap", "PreToolUse", "Edit",
		"echo hi", "command", nil, ir.SourceRef{File: filepath.Join(hookDir, "hooks.json")})
	resolved := []resolver.ResolvedArtifact{{
		Entry: catalog.Entry{
			Name:         "pre-tool-use-trap",
			Resource:     hook,
			Path:         filepath.Join(hookDir, "hooks.json"),
			SourcePolicy: registryfs.Local(registryRoot),
		},
		Source:    config.RepoCommitted,
		Harnesses: []hcc.Target{hcc.TargetFactory},
	}}

	done := make(chan *Report, 1)
	errs := make(chan error, 1)
	go func() {
		report, err := Run(context.Background(), repoRoot, resolved)
		errs <- err
		done <- report
	}()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("hook sync failed: %v", err)
		}
		report := <-done
		if !diag.HasCode(report.Diags, diag.CodeMaterializeRegistrySourceRefused) {
			t.Fatalf("no %s diagnostic; got %v", diag.CodeMaterializeRegistrySourceRefused, report.Diags)
		}
		if _, err := os.Lstat(filepath.Join(repoRoot, ".factory", "hooks", "pipe")); !os.IsNotExist(err) {
			t.Fatalf("a FIFO was materialized into the hooks directory: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("sync blocked on a FIFO in a hook support directory")
	}
}
