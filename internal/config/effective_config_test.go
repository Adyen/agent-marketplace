package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loadEffectiveForTest(ctx context.Context, opts LoadEffectiveOptions) EffectiveConfig {
	if opts.RepoRoot == "" {
		for _, path := range []string{opts.RepoPath, opts.LocalPath, opts.GlobalPath, opts.OrganizationPath} {
			if path != "" {
				opts.RepoRoot = filepath.Dir(path)
				break
			}
		}
	}
	if opts.LocalConfigDir == "" {
		opts.LocalConfigDir = opts.RepoRoot
	}
	intent := ReadIntent(ctx, opts)
	ev := Evidence{}
	for _, source := range intentLayerOrder {
		doc, reads, errs := deployedLayerEvidence(intent.Layers[source], source, opts)
		ev.Locks[source] = doc
		ev.errors[source] = errs
		ev.reads[source] = reads
		ev.Reads = append(ev.Reads, reads...)
	}
	ec := bindLayers(intent, ev)
	if ec.Organization.Config != nil && len(ec.Organization.Errors) == 0 && organizationHasRemoteRequirements(ec.Organization.Config) {
		if trustErr := ValidateOrganizationTrustUnit(ec.Organization.Path, true, true); trustErr != nil {
			ev.errors[Organization] = append(ev.errors[Organization], trustErr)
			ec.Organization.Config = nil
			ec.Organization.Errors = append(ec.Organization.Errors, trustErr)
		}
	}
	return finishBind(intent, ev, ec)
}

func TestLoadEffectiveRejectsOversizedConfigBeforeYAMLParsing(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	data := append([]byte("apiVersion: agent-marketplace/v4\n"), make([]byte, MaxEffectiveConfigSize)...)
	if err := os.WriteFile(userPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})
	if ec.User.Config != nil {
		t.Fatal("oversized user config was parsed")
	}
	if len(ec.User.Errors) != 1 || !errors.Is(ec.User.Errors[0], ErrConfigTooLarge) {
		t.Fatalf("user errors = %v, want one ErrConfigTooLarge", ec.User.Errors)
	}
}

func TestLoadEffectiveReturnsOrganizationProvenance(t *testing.T) {
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  org: {git: git@gitlab.example.com:example-org/org.git, ref: main}
resources:
  org: {all: true, required: true}
`)
	writeTrustedOrganizationLock(t, orgPath)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: orgPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if ec.Organization.Config == nil {
		t.Fatalf("expected organization config to be loaded")
	}
	if ec.Organization.Source != Organization {
		t.Fatalf("expected organization source %v, got %v", Organization, ec.Organization.Source)
	}
	if !ec.Managed {
		t.Fatalf("expected Managed to be true for a valid v4 organization layer")
	}
	if len(ec.Organization.Config.Registries) != 1 {
		t.Fatalf("expected 1 org registry, got %d", len(ec.Organization.Config.Registries))
	}
	if !ec.Organization.Config.Registries[0].Required {
		t.Fatalf("expected org registry to be required")
	}
}

func TestLoadEffectiveReturnsErrorsBeforeCatalog(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
allowed_registries: []
sources:
  blocked: {git: https://example.com/blocked.git, ref: main}
resources:
  blocked: [skill/review]
`)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if len(ec.Errors) == 0 {
		t.Fatalf("expected config errors for blocked registry URL")
	}
	var blocked *RegistryURLBlockedError
	if !errors.As(errors.Join(ec.Errors...), &blocked) || !equalStrings(blocked.BlockedURLs, []string{"https://example.com/blocked.git"}) {
		t.Fatalf("expected typed URL blocked error, got: %v", ec.Errors)
	}
}

func TestLoadEffectiveResolvesAllowedRegistriesFirstAuthority(t *testing.T) {
	t.Run("org wins over user and repo", func(t *testing.T) {
		dir := t.TempDir()
		orgPath := filepath.Join(dir, "org.yaml")
		userPath := filepath.Join(dir, "user.yaml")
		repoPath := filepath.Join(dir, "repo.yaml")
		writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
allowed_registries:
  - ".*"
sources:
  org: {git: git@example.com:org.git, ref: main}
resources:
  org: [skill/review]
`)
		writeTrustedOrganizationLock(t, orgPath)
		writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
allowed_registries:
  - ^example\.com/
sources:
  user: {git: git@example.com:user.git, ref: main}
resources:
  user: [skill/review]
`)
		writeFile(t, repoPath, `apiVersion: agent-marketplace/v4
allowed_registries:
  - ^github\.com/
sources:
  repo: {git: git@example.com:repo.git, ref: main}
resources:
  repo: [skill/review]
`)

		ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
			OrganizationPath: orgPath,
			GlobalPath:       userPath,
			RepoPath:         repoPath,
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		})

		if len(ec.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", ec.Errors)
		}
		if len(ec.Allowed) != 1 || ec.Allowed[0] != ".*" {
			t.Fatalf("expected org allowlist to win, got %v", ec.Allowed)
		}
	})

	t.Run("user wins when org missing", func(t *testing.T) {
		dir := t.TempDir()
		userPath := filepath.Join(dir, "user.yaml")
		repoPath := filepath.Join(dir, "repo.yaml")
		writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
allowed_registries:
  - ^example\.com/
sources:
  user: {git: git@example.com:user.git, ref: main}
resources:
  user: [skill/review]
`)
		writeFile(t, repoPath, `apiVersion: agent-marketplace/v4
allowed_registries:
  - ^github\.com/
sources:
  repo: {git: git@example.com:repo.git, ref: main}
resources:
  repo: [skill/review]
`)

		ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
			OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
			GlobalPath:       userPath,
			RepoPath:         repoPath,
			LocalPath:        filepath.Join(dir, "missing-local.yaml"),
		})

		if len(ec.Errors) > 0 {
			t.Fatalf("unexpected errors: %v", ec.Errors)
		}
		if len(ec.Allowed) != 1 || ec.Allowed[0] != `^example\.com/` {
			t.Fatalf("expected user allowlist to win and repo to be ignored, got %v", ec.Allowed)
		}
	})
}

// spec: consumer-config-format/explicit-non-v4-root-versions-are-refused
func TestLoadEffectiveRefusesLegacyUserAndRepoRoots(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			userPath := filepath.Join(dir, "user.yaml")
			repoPath := filepath.Join(dir, "repo.yaml")
			before := "apiVersion: " + version + "\nregistries:\n  - url: https://example.test/shared.git\n    ref: main\n"
			writeFile(t, userPath, before)
			writeFile(t, repoPath, before)
			ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
				OrganizationPath: filepath.Join(dir, "missing-org.yaml"), GlobalPath: userPath,
				RepoPath: repoPath, LocalPath: filepath.Join(dir, "missing-local.yaml"),
			})
			if len(ec.Errors) != 2 || ec.User.Config != nil || ec.Repo.Config != nil || len(ec.Merged.Registries) != 0 {
				t.Fatalf("unsupported roots were interpreted: errors=%v merged=%#v", ec.Errors, ec.Merged)
			}
			for i, path := range []string{userPath, repoPath} {
				var validation *V4ValidationError
				if !errors.As(ec.Errors[i], &validation) || validation.Code != "v4.api-version" || validation.Path != path || validation.Line != 1 {
					t.Fatalf("error %d = %v, want path-specific format refusal", i, ec.Errors[i])
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != before {
					t.Fatalf("refusal changed %s: %q %v", path, got, err)
				}
			}
		})
	}
}

func TestLoadEffectiveAllowsRepoHarnesses(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo.yaml")
	writeFile(t, repoPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		RepoPath:         repoPath,
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if len(ec.Errors) != 0 {
		t.Fatalf("expected repo harnesses to be accepted, got: %v", ec.Errors)
	}
	if !equalStrings(ec.TargetHarnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses to be the effective target set, got %v", ec.TargetHarnesses)
	}
	if ec.TargetHarnessSource != RepoCommitted {
		t.Fatalf("expected repo harness source %v, got %v", RepoCommitted, ec.TargetHarnessSource)
	}
}

func TestLoadEffectiveNoLegacyOrgFallbackSkipsMissingOrg(t *testing.T) {
	dir := t.TempDir()
	opts := LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	}

	ec := loadEffectiveForTest(context.Background(), opts)

	if ec.Organization.Config != nil {
		t.Fatalf("expected no organization config, got %v", ec.Organization.Config)
	}
	if len(ec.Errors) != 0 {
		t.Fatalf("expected no errors for missing v4 org config, got %v", ec.Errors)
	}
	if ec.Managed {
		t.Fatalf("expected unmanaged when v4 org config is missing")
	}
	if ec.Organization.Source != SourceUnset {
		t.Fatalf("expected unset source, got %v", ec.Organization.Source)
	}
}

func TestLoadEffectiveNoLegacyOrgFallbackIgnoresEnvVar(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "legacy-org.yaml")
	writeFile(t, legacyPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)

	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("AGENT_MARKETPLACE_ORG_CONFIG", legacyPath)
	SetTestOrganizationConfigPath(t, filepath.Join(dir, "missing-org.yaml"))

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		GlobalPath: filepath.Join(dir, "missing-global.yaml"),
		RepoPath:   filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:  filepath.Join(dir, "missing-local.yaml"),
	})

	if ec.Organization.Config != nil {
		t.Fatalf("legacy organization environment variable should be ignored, got %v", ec.Organization.Config)
	}
	if len(ec.Errors) != 0 {
		t.Fatalf("expected no errors for missing org config, got %v", ec.Errors)
	}
	if ec.Managed {
		t.Fatalf("expected unmanaged when selected org config is missing")
	}
}

func TestLoadEffectiveOrganizationRefusesLegacyRoot(t *testing.T) {
	for _, version := range []string{"agent-marketplace/v2", "agent-marketplace/v3"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			orgPath := filepath.Join(dir, "org.yaml")
			writeFile(t, orgPath, "apiVersion: "+version+"\nharnesses: [factory]\nsources: {}\n")
			ec := loadEffectiveForTest(context.Background(), isolatedEffectiveOptions(orgPath))
			var validation *V4ValidationError
			if !errors.As(errors.Join(ec.Errors...), &validation) || validation.Code != "v4.api-version" || validation.Path != orgPath || validation.Line != 1 {
				t.Fatalf("organization refusal = %v", ec.Errors)
			}
			if ec.Organization.Config != nil || ec.Managed || ec.Organization.Source != Organization || ec.Diagnostics.Validation != "invalid" {
				t.Fatalf("legacy root treated as managed or absent: %#v", ec.Organization)
			}
		})
	}
}

func TestLoadEffectiveNoLegacyOrgFallbackLoadsV4Org(t *testing.T) {
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  org: {git: git@gitlab.example.com:example-org/org.git, ref: main}
resources:
  org: {all: true, required: true}
`)
	writeTrustedOrganizationLock(t, orgPath)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: orgPath,
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if ec.Organization.Config == nil {
		t.Fatalf("expected v4 organization config to be loaded")
	}
	if len(ec.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", ec.Errors)
	}
	if !ec.Managed {
		t.Fatalf("expected managed for valid v4 organization layer")
	}
	if ec.Organization.Source != Organization {
		t.Fatalf("expected organization source %v, got %v", Organization, ec.Organization.Source)
	}
	if len(ec.Organization.Config.Registries) != 1 || !ec.Organization.Config.Registries[0].Required {
		t.Fatalf("expected required org registry")
	}
}

func TestLoadEffectiveNoLegacyOrgFallbackRejectsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping unreadable file test on windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping unreadable file test when running as root")
	}
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	if err := os.Chmod(orgPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() {
		_ = os.Chmod(orgPath, 0o644)
	}()

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: orgPath,
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if ec.Organization.Config != nil {
		t.Fatalf("expected no organization config for unreadable file")
	}
	if len(ec.Errors) == 0 {
		t.Fatalf("expected a read error for unreadable organization config")
	}
	if !strings.Contains(ec.Errors[0].Error(), "read organization config") {
		t.Fatalf("expected read error, got: %v", ec.Errors[0])
	}
}

func TestLoadEffectiveTargetHarnessesUserWinsOverOrg(t *testing.T) {
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.yaml")
	userPath := filepath.Join(dir, "user.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`)
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
harnesses:
  - cursor
sources: {}
`)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: orgPath,
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if len(ec.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", ec.Errors)
	}
	if !equalStrings(ec.TargetHarnesses, []string{"cursor"}) {
		t.Fatalf("expected user harnesses to win, got %v", ec.TargetHarnesses)
	}
	if ec.TargetHarnessSource != UserPersonal {
		t.Fatalf("expected target source %v, got %v", UserPersonal, ec.TargetHarnessSource)
	}
}

func TestLoadEffectiveTargetHarnessesFallsBackToOrg(t *testing.T) {
	dir := t.TempDir()
	orgPath := filepath.Join(dir, "org.yaml")
	userPath := filepath.Join(dir, "user.yaml")
	writeFile(t, orgPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
  - windsurf
sources: {}
`)
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
sources: {}
`)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: orgPath,
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if len(ec.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", ec.Errors)
	}
	if !equalStrings(ec.TargetHarnesses, []string{"factory", "windsurf"}) {
		t.Fatalf("expected org harnesses, got %v", ec.TargetHarnesses)
	}
	if ec.TargetHarnessSource != Organization {
		t.Fatalf("expected target source %v, got %v", Organization, ec.TargetHarnessSource)
	}
}

func TestLoadEffectiveAllowsMissingUserHarnesses(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "user.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
sources:
  local: {directory: /local/skills}
resources:
  local: [skill/review]
`)

	// Avoid falling back to embedded org defaults which carry harnesses.
	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if len(ec.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", ec.Errors)
	}
	if ec.User.Config == nil {
		t.Fatalf("expected user config to load")
	}
	if len(ec.TargetHarnesses) != 0 {
		t.Fatalf("expected no target harnesses, got %v", ec.TargetHarnesses)
	}
}

func TestLoadEffectiveAllowsV4RepoHarnesses(t *testing.T) {
	dir := t.TempDir()
	repoPath := filepath.Join(dir, "repo.yaml")
	writeFile(t, repoPath, `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources:
  local: {directory: /local/skills}
resources:
  local: [skill/review]
`)

	ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       filepath.Join(dir, "missing-global.yaml"),
		RepoPath:         repoPath,
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	})

	if ec.Repo.Config == nil {
		t.Fatalf("expected repo config to load")
	}
	if len(ec.Errors) != 0 {
		t.Fatalf("expected repo harnesses to be accepted, got: %v", ec.Errors)
	}
	if !equalStrings(ec.Repo.Config.Harnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses to be preserved, got %v", ec.Repo.Config.Harnesses)
	}
	if !equalStrings(ec.TargetHarnesses, []string{"factory"}) {
		t.Fatalf("expected repo harnesses to be effective, got %v", ec.TargetHarnesses)
	}
}

func TestLoadEffectiveOrganizationDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file ownership/permission diagnostics are Unix-specific")
	}

	validV4 := `apiVersion: agent-marketplace/v4
harnesses:
  - factory
sources: {}
`
	cases := []struct {
		name        string
		setup       func(t *testing.T, p string)
		wantWarn    string
		wantErr     string
		wantManaged bool
	}{
		{
			name: "valid",
			setup: func(t *testing.T, p string) {
				writeFile(t, p, validV4)
				if err := os.Chmod(p, 0o444); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			wantManaged: true,
		},
		{
			name:        "missing",
			setup:       func(t *testing.T, p string) {},
			wantManaged: false,
		},
		{
			name: "invalid",
			setup: func(t *testing.T, p string) {
				writeFile(t, p, "apiVersion: agent-marketplace/v4\nunknown_field: x\nsources: {}\n")
			},
			wantErr:     "unknown top-level field",
			wantManaged: false,
		},
		{
			name: "writable",
			setup: func(t *testing.T, p string) {
				writeFile(t, p, validV4)
				if err := os.Chmod(p, 0o666); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			wantWarn:    "writable",
			wantManaged: false,
		},
		{
			name: "wrong-owner",
			setup: func(t *testing.T, p string) {
				writeFile(t, p, validV4)
				if err := os.Chmod(p, 0o444); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			wantWarn:    "wrong owner",
			wantManaged: true,
		},
		{
			name: "non-regular",
			setup: func(t *testing.T, p string) {
				if err := os.Mkdir(p, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			wantWarn:    "not a regular file",
			wantManaged: false,
		},
		{
			name: "symlink",
			setup: func(t *testing.T, p string) {
				if err := os.Symlink("/dev/null", p); err != nil {
					t.Fatalf("symlink: %v", err)
				}
			},
			wantWarn:    "symlink",
			wantManaged: false,
		},
		{
			name: "unreadable",
			setup: func(t *testing.T, p string) {
				writeFile(t, p, validV4)
				if err := os.Chmod(p, 0o000); err != nil {
					t.Fatalf("chmod: %v", err)
				}
			},
			wantErr:     "read organization config",
			wantManaged: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "unreadable" && os.Getuid() == 0 {
				t.Skip("root can read 000 files")
			}

			oldTrusted := trustedOwnerFn
			defer func() { trustedOwnerFn = oldTrusted }()
			switch tc.name {
			case "valid":
				trustedOwnerFn = func(os.FileInfo) bool { return true }
			case "wrong-owner":
				trustedOwnerFn = func(os.FileInfo) bool { return false }
			}

			p := filepath.Join(t.TempDir(), "org.yaml")
			tc.setup(t, p)

			ec := loadEffectiveForTest(context.Background(), LoadEffectiveOptions{
				OrganizationPath: p,
				GlobalPath:       filepath.Join(t.TempDir(), "missing-global.yaml"),
				RepoPath:         filepath.Join(t.TempDir(), "missing-repo.yaml"),
				LocalPath:        filepath.Join(t.TempDir(), "missing-local.yaml"),
			})

			var msgs []string
			for _, e := range ec.Organization.Errors {
				msgs = append(msgs, e.Error())
			}
			for _, w := range ec.Warnings {
				msgs = append(msgs, w.Error())
			}
			got := strings.Join(msgs, "; ")

			if ec.Managed != tc.wantManaged {
				t.Fatalf("Managed=%v, want %v", ec.Managed, tc.wantManaged)
			}
			if tc.wantErr != "" && !strings.Contains(got, tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %s", tc.wantErr, got)
			}
			if tc.wantWarn != "" && !strings.Contains(got, tc.wantWarn) {
				t.Fatalf("expected warning containing %q, got: %s", tc.wantWarn, got)
			}
			if tc.wantErr == "" && tc.wantWarn == "" && got != "" {
				t.Fatalf("expected no diagnostics for valid state, got: %s", got)
			}
		})
	}
}

func writeTrustedOrganizationLock(t *testing.T, configPath string) {
	t.Helper()
	writeFile(t, OrganizationLockPath(configPath), "apiVersion: agent-marketplace-lock/v1\nscope: organization\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// spec: configuration-intent/reading-authored-selections-without-acquisition
func TestReadIntentPerformsNoLockIO(t *testing.T) {
	dir := t.TempDir()
	userDir := filepath.Join(dir, "user")
	userPath := filepath.Join(userDir, "config.yaml")
	writeFile(t, userPath, `apiVersion: agent-marketplace/v4
sources: {m: {marketplace: https://market.example}}
resources:
  m:
    include: [main/collection/onboarding]
`)
	if err := os.MkdirAll(filepath.Join(userDir, "lock.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := LoadEffectiveOptions{
		RepoRoot:         dir,
		LocalConfigDir:   dir,
		OrganizationPath: filepath.Join(dir, "missing-org.yaml"),
		GlobalPath:       userPath,
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	}
	intent := ReadIntent(context.Background(), opts)
	if len(intent.Errors) != 0 {
		t.Fatalf("intent errors = %v", intent.Errors)
	}
	if !intent.Layers[UserPersonal].NeedsMarketplaceLock() {
		t.Fatal("expected user layer to need a Marketplace lock")
	}
	for _, read := range intent.Layers[UserPersonal].Reads {
		if read.AcquisitionLock {
			t.Fatalf("ReadIntent recorded acquisition lock read %#v", read)
		}
	}
	effective := Bind(intent, Evidence{})
	var validation *V4ValidationError
	if !errors.As(errors.Join(effective.Errors...), &validation) || validation.Code != "v4.marketplace-lock-required" {
		t.Fatalf("errors = %v, want v4.marketplace-lock-required", effective.Errors)
	}
}
