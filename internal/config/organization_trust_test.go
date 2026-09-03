//go:build !windows

package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const remoteOrganizationFixture = `apiVersion: agent-marketplace/v2
harnesses: [factory]
registries:
  - url: https://example.com/org.git
    ref: main
    required: true
`

func TestOrganizationConfigLockTrustUnit(t *testing.T) {
	tests := []struct {
		name string
		set  func(t *testing.T, configPath, lockPath string)
		code string
	}{
		{name: "symlink lock", code: "organization.trust.lock.symlink", set: func(t *testing.T, _, lockPath string) {
			target := filepath.Join(t.TempDir(), "target")
			writeFile(t, target, "apiVersion: agent-marketplace-lock/v1\nscope: organization\n")
			if err := os.Symlink(target, lockPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nonregular lock", code: "organization.trust.lock.non-regular", set: func(t *testing.T, _, lockPath string) {
			if err := os.Mkdir(lockPath, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "writable lock", code: "organization.trust.lock.writable", set: func(t *testing.T, _, lockPath string) {
			writeFile(t, lockPath, "apiVersion: agent-marketplace-lock/v1\nscope: organization\n")
			if err := os.Chmod(lockPath, 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed lock", code: "organization.trust.lock.malformed", set: func(t *testing.T, _, lockPath string) {
			writeFile(t, lockPath, "not: [yaml")
		}},
		{name: "oversized lock", code: "organization.trust.lock.read", set: func(t *testing.T, _, lockPath string) {
			file, err := os.Create(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(4<<20 + 1); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "scope mismatch", code: "organization.trust.lock.scope", set: func(t *testing.T, _, lockPath string) {
			writeFile(t, lockPath, "apiVersion: agent-marketplace-lock/v1\nscope: user\n")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			lockPath := filepath.Join(dir, "lock.yaml")
			writeFile(t, configPath, remoteOrganizationFixture)
			if test.set != nil {
				test.set(t, configPath, lockPath)
			}
			ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
			assertOrganizationTrustFailure(t, ec, test.code)
		})
	}
}

func TestOrganizationRemoteRequirementsLoadWithoutLock(t *testing.T) {
	for _, fixture := range []string{
		remoteOrganizationFixture,
		`apiVersion: agent-marketplace/v2
marketplaces:
  - url: https://marketplace.example.com
    instance_id: org
`,
	} {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		writeFile(t, configPath, fixture)
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
		if len(ec.Errors) != 0 || ec.Organization.Config == nil || !ec.Managed {
			t.Fatalf("organization config without lock was blocked: managed=%v config=%v errors=%v", ec.Managed, ec.Organization.Config != nil, ec.Errors)
		}
	}
}

func TestOrganizationAuthorityRejectsConfigAndAncestorSymlinks(t *testing.T) {
	t.Run("config symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.yaml")
		writeFile(t, target, remoteOrganizationFixture)
		configPath := filepath.Join(dir, "config.yaml")
		if err := os.Symlink(target, configPath); err != nil {
			t.Fatal(err)
		}
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
		assertOrganizationTrustFailure(t, ec, "organization.trust.config.symlink")
	})

	t.Run("authority directory symlink", func(t *testing.T) {
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(realDir, "config.yaml"), remoteOrganizationFixture)
		linkDir := filepath.Join(root, "authority")
		if err := os.Symlink(realDir, linkDir); err != nil {
			t.Fatal(err)
		}
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(filepath.Join(linkDir, "config.yaml")))
		assertOrganizationTrustFailure(t, ec, "organization.trust.ancestor.symlink")
	})

	t.Run("writable authority directory", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		writeFile(t, configPath, remoteOrganizationFixture)
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
		assertOrganizationTrustFailure(t, ec, "organization.trust.ancestor.writable")
	})
}

func TestOrganizationTrustRejectsWrongOwnerWhenFeasible(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing fixture ownership requires root")
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeFile(t, configPath, remoteOrganizationFixture)
	writeTrustedOrganizationLock(t, configPath)
	if err := os.Chown(filepath.Join(dir, "lock.yaml"), 65534, -1); err != nil {
		t.Skipf("cannot create wrong-owner fixture: %v", err)
	}
	ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
	assertOrganizationTrustFailure(t, ec, "organization.trust.lock.owner")
}

func TestOrganizationTrustedFixtureLoadsAsManaged(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeFile(t, configPath, remoteOrganizationFixture)
	writeTrustedOrganizationLock(t, configPath)

	ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
	if len(ec.Errors) != 0 || ec.Organization.Config == nil || !ec.Managed {
		t.Fatalf("trusted organization fixture was blocked: managed=%v config=%v errors=%v", ec.Managed, ec.Organization.Config != nil, ec.Errors)
	}
}

func isolatedEffectiveOptions(configPath string) LoadEffectiveOptions {
	dir := filepath.Dir(configPath)
	return LoadEffectiveOptions{
		OrganizationPath: configPath,
		GlobalPath:       filepath.Join(dir, "missing-user.yaml"),
		RepoPath:         filepath.Join(dir, "missing-repo.yaml"),
		LocalPath:        filepath.Join(dir, "missing-local.yaml"),
	}
}

func assertOrganizationTrustFailure(t *testing.T, ec EffectiveConfig, code string) {
	t.Helper()
	if ec.Organization.Config != nil || ec.Managed {
		t.Fatalf("untrusted organization pair was applied: managed=%v config=%v", ec.Managed, ec.Organization.Config != nil)
	}
	for _, err := range ec.Errors {
		var trustErr *OrganizationTrustError
		if errors.As(err, &trustErr) && trustErr.Code == code {
			return
		}
	}
	t.Fatalf("missing typed trust error %q in %v", code, ec.Errors)
}
