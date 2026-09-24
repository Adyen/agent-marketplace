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

const v4MarketplaceOrganizationFixture = `apiVersion: agent-marketplace/v4
sources:
  hosted: {marketplace: https://marketplace.example}
`

const v4MarketplaceOrganizationLock = `apiVersion: agent-marketplace-lock/v2
scope: organization
marketplaces:
  - url: https://marketplace.example
    instance_id: marketplace
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

func TestOrganizationV4MarketplaceLockTrustUnit(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		code string
	}{
		{name: "trusted", mode: 0o644},
		{name: "writable", mode: 0o666, code: "organization.trust.lock.writable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			lockPath := filepath.Join(dir, "lock.yaml")
			writeFile(t, configPath, v4MarketplaceOrganizationFixture)
			writeFile(t, lockPath, v4MarketplaceOrganizationLock)
			if err := os.Chmod(lockPath, test.mode); err != nil {
				t.Fatal(err)
			}

			ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
			if test.code != "" {
				assertOrganizationTrustFailure(t, ec, test.code)
				return
			}
			if len(ec.Errors) != 0 || ec.Organization.Config == nil || !ec.Managed {
				t.Fatalf("trusted v4 Marketplace organization lock was blocked: managed=%v config=%v errors=%v", ec.Managed, ec.Organization.Config != nil, ec.Errors)
			}
		})
	}
}

func TestOrganizationRemoteRequirementsLoadWithoutLock(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeFile(t, configPath, remoteOrganizationFixture)
	ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
	if len(ec.Errors) != 0 || ec.Organization.Config == nil || !ec.Managed {
		t.Fatalf("organization config without lock was blocked: managed=%v config=%v errors=%v", ec.Managed, ec.Organization.Config != nil, ec.Errors)
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

	t.Run("symlinked existing ancestor", func(t *testing.T) {
		root := t.TempDir()
		external := t.TempDir()
		authority := filepath.Join(external, "authority")
		if err := os.Mkdir(authority, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(authority, "config.yaml"), remoteOrganizationFixture)
		link := filepath.Join(root, "authority-parent")
		if err := os.Symlink(external, link); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(link, "authority", "config.yaml")
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(configPath))
		assertOrganizationTrustFailure(t, ec, "organization.trust.ancestor.symlink")
	})

	t.Run("writable existing ancestor", func(t *testing.T) {
		root := t.TempDir()
		parent := filepath.Join(root, "parent")
		authority := filepath.Join(parent, "authority")
		if err := os.MkdirAll(authority, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(authority, "config.yaml"), remoteOrganizationFixture)
		if err := os.Chmod(parent, 0o777); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		ec := LoadEffective(context.Background(), isolatedEffectiveOptions(filepath.Join(authority, "config.yaml")))
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

func TestOrganizationIncludesRequireTrustedLexicalPaths(t *testing.T) {
	for _, withLock := range []bool{false, true} {
		for _, test := range []struct {
			name string
			code string
			set  func(t *testing.T, external, leaf string) string
		}{
			{name: "trusted nested relative"},
			{name: "trusted nested absolute", set: func(t *testing.T, _, leaf string) string { return leaf }},
			{name: "nonregular fragment", code: "organization.trust.include.non-regular", set: func(t *testing.T, _, _ string) string { return "nested" }},
			{name: "writable fragment", code: "organization.trust.include.writable", set: func(t *testing.T, _, leaf string) string {
				if err := os.Chmod(leaf, 0o666); err != nil {
					t.Fatal(err)
				}
				return "nested/leaf.yaml"
			}},
			{name: "writable ancestor", code: "organization.trust.ancestor.writable", set: func(t *testing.T, _, leaf string) string {
				if err := os.Chmod(filepath.Dir(leaf), 0o777); err != nil {
					t.Fatal(err)
				}
				return "nested/leaf.yaml"
			}},
			{name: "symlink fragment", code: "organization.trust.include.symlink", set: func(t *testing.T, external, leaf string) string {
				if err := os.Symlink(leaf, filepath.Join(external, "alias.yaml")); err != nil {
					t.Fatal(err)
				}
				return "alias.yaml"
			}},
			{name: "symlink ancestor", code: "organization.trust.ancestor.symlink", set: func(t *testing.T, external, leaf string) string {
				if err := os.Symlink(filepath.Dir(leaf), filepath.Join(external, "alias")); err != nil {
					t.Fatal(err)
				}
				return "alias/leaf.yaml"
			}},
			{name: "symlink before dotdot", code: "organization.trust.ancestor.symlink", set: func(t *testing.T, external, leaf string) string {
				if err := os.Symlink(filepath.Dir(leaf), filepath.Join(external, "alias")); err != nil {
					t.Fatal(err)
				}
				return "alias/../nested/leaf.yaml"
			}},
			{name: "writable ancestor before dotdot", code: "organization.trust.ancestor.writable", set: func(t *testing.T, external, _ string) string {
				dir := filepath.Join(external, "writable")
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0o777); err != nil {
					t.Fatal(err)
				}
				return "writable/../nested/leaf.yaml"
			}},
			{name: "duplicate through symlink", code: "organization.trust.include.symlink", set: func(t *testing.T, external, leaf string) string {
				if err := os.Symlink(leaf, filepath.Join(external, "alias.yaml")); err != nil {
					t.Fatal(err)
				}
				return "nested/leaf.yaml, alias.yaml"
			}},
			{name: "wrong owner", code: "organization.trust.include.owner", set: func(t *testing.T, _, leaf string) string {
				if os.Geteuid() != 0 {
					t.Skip("changing fixture ownership requires root")
				}
				if err := os.Chown(leaf, 65534, -1); err != nil {
					t.Skipf("cannot create wrong-owner fixture: %v", err)
				}
				return "nested/leaf.yaml"
			}},
		} {
			name := test.name + "/local-only"
			if withLock {
				name = test.name + "/with-lock"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				root := filepath.Join(dir, "authority", "config.yaml")
				external := filepath.Join(dir, "external")
				middle := filepath.Join(external, "middle.yaml")
				leaf := filepath.Join(external, "nested", "leaf.yaml")
				writeIncludeConfig(t, leaf, "registries:\n  - path: /trusted-live-registry\n    required: true\n    skills: {include: [review]}\n")
				include := "nested/leaf.yaml"
				if test.set != nil {
					include = test.set(t, external, leaf)
				}
				writeIncludeConfig(t, middle, "include: ["+include+"]\n")
				rootInclude := "../external/middle.yaml"
				if test.name == "trusted nested absolute" {
					rootInclude = middle
				}
				data := "apiVersion: agent-marketplace/v2\nharnesses: [factory]\ninclude: " + rootInclude + "\n"
				if withLock {
					data += "registries:\n  - url: https://example.com/org.git\n    ref: main\n"
				}
				writeIncludeConfig(t, root, data)
				paths := []string{root, middle, leaf}
				if withLock {
					writeTrustedOrganizationLock(t, root)
					paths = append(paths, OrganizationLockPath(root))
				}
				for _, path := range paths {
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						after, err := os.ReadFile(path)
						if err != nil || string(after) != string(before) {
							t.Errorf("user bytes changed at %s: %v", path, err)
						}
					})
				}
				ec := LoadEffective(context.Background(), isolatedEffectiveOptions(root))
				if test.code != "" {
					assertOrganizationTrustFailure(t, ec, test.code)
					var trustErr *OrganizationTrustError
					if !errors.As(errors.Join(ec.Errors...), &trustErr) || trustErr.Path == "" {
						t.Fatalf("missing unsafe include path: %v", ec.Errors)
					}
					return
				}
				if len(ec.Errors) != 0 || !ec.Managed || ec.Organization.Config == nil {
					t.Fatalf("trusted external includes blocked: %v", ec.Errors)
				}
				entry := ec.Organization.Config.Registries[0]
				if entry.URL != "/trusted-live-registry" || !entry.Required || len(entry.Skills.Include) != 1 || entry.Skills.Include[0] != "review" {
					t.Fatalf("trusted fragment policy not applied: %#v", entry)
				}
				if len(ec.Organization.Reads) != 3 {
					t.Fatalf("include read assertions lost: %#v", ec.Organization.Reads)
				}
				if !withLock {
					if _, err := os.Lstat(OrganizationLockPath(root)); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("local-only load created lock: %v", err)
					}
				}
			})
		}
	}
}

func TestV4OrganizationIncludesRequireTrustedAuthority(t *testing.T) {
	for _, test := range []struct {
		name string
		mode os.FileMode
		code string
	}{
		{name: "trusted", mode: 0o600},
		{name: "writable", mode: 0o666, code: "organization.trust.include.writable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "config.yaml")
			fragment := filepath.Join(dir, "fragment.yaml")
			writeIncludeConfig(t, fragment, "sources: {shared: {directory: /trusted-live-registry}}\nresources: {shared: {include: [skill/review]}}\n")
			if err := os.Chmod(fragment, test.mode); err != nil {
				t.Fatal(err)
			}
			writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v4\nharnesses: [factory]\ninclude: fragment.yaml\n")

			ec := LoadEffective(context.Background(), isolatedEffectiveOptions(root))
			if test.code != "" {
				assertOrganizationTrustFailure(t, ec, test.code)
				return
			}
			if len(ec.Errors) != 0 || !ec.Managed || ec.Organization.Config == nil {
				t.Fatalf("trusted v4 include blocked: managed=%v config=%v errors=%v", ec.Managed, ec.Organization.Config != nil, ec.Errors)
			}
			if got := ec.Organization.Config.Registries; len(got) != 1 || got[0].URL != "/trusted-live-registry" || len(got[0].Skills.Include) != 1 || got[0].Skills.Include[0] != "review" {
				t.Fatalf("trusted v4 fragment policy not applied: %#v", got)
			}
		})
	}
}

func TestOrganizationIncludeRetainsRootLexicalPath(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "authority", "config.yaml")
	writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: leaf.yaml\n")
	writeIncludeConfig(t, filepath.Join(dir, "authority", "leaf.yaml"), "registries:\n  - path: /live-registry\n")
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	requested := dir + "/alias/../authority/config.yaml"
	ec := LoadEffective(context.Background(), isolatedEffectiveOptions(requested))
	assertOrganizationTrustFailure(t, ec, "organization.trust.ancestor.symlink")
}

func TestNonOrganizationIncludesKeepSymlinkAndWritableCompatibility(t *testing.T) {
	for _, kind := range []layerKind{userLayer, repoLayer, localLayer} {
		dir := t.TempDir()
		root := filepath.Join(dir, "config.yaml")
		leaf := filepath.Join(dir, "nested", "leaf.yaml")
		writeIncludeConfig(t, leaf, "registries:\n  - path: /live-registry\n")
		if err := os.Chmod(leaf, 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Dir(leaf), filepath.Join(dir, "alias")); err != nil {
			t.Fatal(err)
		}
		writeIncludeConfig(t, root, "apiVersion: agent-marketplace/v2\ninclude: alias/../nested/leaf.yaml\n")
		data, err := readConfigFile(root)
		if err != nil {
			t.Fatal(err)
		}
		li, err := readLayerIntent(root, "", data, kind, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := li.bind(nil)
		if err != nil || len(cfg.Registries) != 1 || cfg.Registries[0].URL != "/live-registry" {
			t.Fatalf("non-organization layer %v changed: cfg=%v err=%v", kind, cfg, err)
		}
	}
}

func TestOrganizationIncludeCannotEnableTestTrustFromFragmentPath(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires a non-root fixture owner")
	}
	dir := t.TempDir()
	fragment := filepath.Join(dir, "fragment.yaml")
	writeIncludeConfig(t, fragment, "registries: []\n")
	if err := validateOrganizationInclude(fragment, filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("existing fixture trust override blocked: %v", err)
	}
	err := validateOrganizationInclude(fragment, defaultOrganizationConfigPath)
	var trustErr *OrganizationTrustError
	if !errors.As(err, &trustErr) || trustErr.Code != "organization.trust.ancestor.owner" || trustErr.Path != dir {
		t.Fatalf("production authority accepted fixture owner: %v", err)
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
