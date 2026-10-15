package e2e_test

// TestFilesystemConfigDestinations is the isolated CLI evidence for the
// descriptor-bound config/lock boundary (VAL-FS-001): the six user,
// repository, and local config and lock destinations honor layer authority.
// Approved user dotfile links and contained repository/local links stay
// links while their targets are atomically replaced; escapes, multiply
// linked targets, and cross-layer object aliases fail before any config,
// lock, journal, or staging mutation. Each phase records before/after
// bytes, mode, mtime, inode, and link text, and checks for staging or
// journal residue.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// destEvidence captures the observable state of a destination path: the
// link itself (when present) and the effective target behind it.
type destEvidence struct {
	path      string
	isLink    bool
	linkText  string
	linkInode uint64
	bytes     []byte
	mode      os.FileMode
	modTime   time.Time
	inode     uint64
}

func captureDest(t *testing.T, path string) destEvidence {
	t.Helper()
	ev := destEvidence{path: path}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		ev.isLink = true
		ev.linkText, err = os.Readlink(path)
		if err != nil {
			t.Fatalf("readlink %s: %v", path, err)
		}
		ev.linkInode = info.Sys().(*syscall.Stat_t).Ino
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	ev.bytes = data
	target, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	ev.mode = target.Mode()
	ev.modTime = target.ModTime()
	ev.inode = target.Sys().(*syscall.Stat_t).Ino
	return ev
}

// requireLinkPreserved asserts the destination is still the captured link:
// same link text and same link inode, so publication never detached it.
func requireLinkPreserved(t *testing.T, before, after destEvidence) {
	t.Helper()
	if !after.isLink {
		t.Fatalf("%s is no longer a link", before.path)
	}
	if after.linkText != before.linkText || after.linkInode != before.linkInode {
		t.Fatalf("link %s replaced: text %q→%q inode %d→%d",
			before.path, before.linkText, after.linkText, before.linkInode, after.linkInode)
	}
}

// requireTargetReplaced asserts the effective content moved to new bytes on
// a new object (atomic rename) while the mode stayed as captured.
func requireTargetReplaced(t *testing.T, before, after destEvidence) {
	t.Helper()
	if string(after.bytes) == string(before.bytes) {
		t.Fatalf("%s target bytes never changed", before.path)
	}
	if after.inode == before.inode {
		t.Fatalf("%s target inode %d unchanged; publication was not atomic", before.path, before.inode)
	}
	if after.mode != before.mode {
		t.Fatalf("%s target mode changed: %v→%v", before.path, before.mode, after.mode)
	}
	if after.modTime.Equal(before.modTime) {
		t.Fatalf("%s target mtime %v unchanged after replacement", before.path, before.modTime)
	}
}

// requireDestUntouched asserts byte, mode, mtime, and inode identity, the
// strongest refusal evidence: the object was never opened for writing.
func requireDestUntouched(t *testing.T, before, after destEvidence) {
	t.Helper()
	if string(after.bytes) != string(before.bytes) ||
		after.mode != before.mode ||
		!after.modTime.Equal(before.modTime) ||
		after.inode != before.inode {
		t.Fatalf("%s changed: bytes %q→%q mode %v→%v mtime %v→%v inode %d→%d",
			before.path, before.bytes, after.bytes, before.mode, after.mode,
			before.modTime, after.modTime, before.inode, after.inode)
	}
	if before.isLink && (after.linkText != before.linkText || after.linkInode != before.linkInode) {
		t.Fatalf("link %s touched: text %q→%q", before.path, before.linkText, after.linkText)
	}
}

// requireNoResidue fails when staging temp files or dependency-transaction
// journals survive below any of the given directories.
func requireNoResidue(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			name := info.Name()
			if strings.Contains(name, ".tmp") || strings.HasPrefix(name, ".dependencytxn-") {
				t.Errorf("residue after transaction: %s", path)
			}
			return nil
		})
	}
}

func mustRelink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// spec: config-mutation/approved-layer-links-receive-real-updates
func TestFilesystemConfigDestinations(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()
	regURL := env.CreateBareRepo("boundary-reg", SkillsOnly(
		Skill{Name: "alpha", Description: "alpha skill"},
		Skill{Name: "beta", Description: "beta skill"},
		Skill{Name: "gamma", Description: "gamma skill"},
	))

	userConfig := env.mustResolvePath("/root/.agents/config.yaml")
	userLock := env.mustResolvePath("/root/.agents/lock.yaml")
	userTarget := env.mustResolvePath("/root/dotfiles/agents-user.yaml")
	repoConfig := env.mustResolvePath("/workspace/.agents/config.yaml")
	repoLock := env.mustResolvePath("/workspace/.agents/lock.yaml")
	localConfig := env.mustResolvePath("/workspace/.agents/config.local.yaml")
	localLock := env.mustResolvePath("/workspace/.agents/lock.local.yaml")
	stateRoot := env.mustResolvePath("/root/.local/state")

	t.Run("user dotfile link honored by install and lock reconciliation", func(t *testing.T) {
		env.WriteFile("/root/dotfiles/agents-user.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# keep this comment\nsources: {team: {git: '"+regURL+"', ref: main}}\n")
		if err := os.Chmod(userTarget, 0o640); err != nil {
			t.Fatal(err)
		}
		mustRelink(t, userTarget, userConfig)
		before := captureDest(t, userConfig)

		env.MustRunAM("install", "alpha", "--scope", "user")

		after := captureDest(t, userConfig)
		requireLinkPreserved(t, before, after)
		requireTargetReplaced(t, before, after)
		if !strings.Contains(string(after.bytes), "alpha") || !strings.Contains(string(after.bytes), "boundary-reg") {
			t.Fatalf("install did not edit the dotfile target:\n%s", after.bytes)
		}
		if !strings.Contains(string(after.bytes), "# keep this comment") {
			t.Fatalf("install dropped the authored comment:\n%s", after.bytes)
		}
		// The user lock is a direct managed leaf written through the same
		// boundary: a regular file carrying the reconciled pin.
		lockInfo, err := os.Lstat(userLock)
		if err != nil || !lockInfo.Mode().IsRegular() {
			t.Fatalf("user lock is not a regular file: %v, %v", lockInfo, err)
		}
		lockBytes := env.ReadFile("/root/.agents/lock.yaml")
		if !strings.Contains(lockBytes, "commit: sha1:") {
			t.Fatalf("user lock was not reconciled:\n%s", lockBytes)
		}
		requireNoResidue(t, filepath.Dir(userConfig), filepath.Dir(userTarget), stateRoot)
	})

	t.Run("v4 selection edit publishes through the dotfile link", func(t *testing.T) {
		// An ordinary supported edit must replace the approved target, not
		// its link, while preserving the independently authored comment.
		if err := os.WriteFile(userTarget, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\n# keep this comment\nsources: {team: {git: '"+regURL+"', ref: main}}\nresources: {team: {include: [skill/alpha]}}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		before := captureDest(t, userConfig)

		env.MustRunAM("install", "beta", "--scope", "user")

		after := captureDest(t, userConfig)
		requireLinkPreserved(t, before, after)
		requireTargetReplaced(t, before, after)
		if !strings.Contains(string(after.bytes), "include:") || !strings.Contains(string(after.bytes), "skill/beta") {
			t.Fatalf("supported selection edit was not published:\n%s", after.bytes)
		}
		if !strings.Contains(string(after.bytes), "# keep this comment") {
			t.Fatalf("selection edit dropped the authored comment:\n%s", after.bytes)
		}
		// Restore a clean v4 user config for the remaining phases.
		if err := os.WriteFile(userTarget, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '"+regURL+"', ref: main}}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		requireNoResidue(t, filepath.Dir(userConfig), filepath.Dir(userTarget), stateRoot)
	})

	t.Run("contained repository and local links honored by lock and install", func(t *testing.T) {
		shared := env.mustResolvePath("/workspace/.agents/shared")
		env.WriteFile("/workspace/.agents/shared/repo-config.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '"+regURL+"', ref: main}}\nresources: {team: {include: [skill/alpha]}}\n")
		env.WriteFile("/workspace/.agents/shared/repo-lock.yaml", "apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries: []\n")
		env.WriteFile("/workspace/.agents/shared/local-config.yaml", "apiVersion: agent-marketplace/v4\nsources: {team: {git: '"+regURL+"', ref: main}}\nresources: {team: {include: [skill/beta]}}\n")
		env.WriteFile("/workspace/.agents/shared/local-lock.yaml", "apiVersion: agent-marketplace-lock/v1\nscope: local\nregistries: []\n")
		mustRelink(t, filepath.Join("shared", "repo-config.yaml"), repoConfig)
		mustRelink(t, filepath.Join("shared", "repo-lock.yaml"), repoLock)
		mustRelink(t, filepath.Join("shared", "local-config.yaml"), localConfig)
		mustRelink(t, filepath.Join("shared", "local-lock.yaml"), localLock)
		beforeRepoLock := captureDest(t, repoLock)
		beforeLocalLock := captureDest(t, localLock)

		env.MustRunAM("lock")

		for _, before := range []destEvidence{beforeRepoLock, beforeLocalLock} {
			after := captureDest(t, before.path)
			requireLinkPreserved(t, before, after)
			requireTargetReplaced(t, before, after)
			if !strings.Contains(string(after.bytes), "commit: sha1:") {
				t.Fatalf("lock %s lost the registry pin:\n%s", before.path, after.bytes)
			}
		}

		// A staged config edit publishes through the contained config link.
		beforeRepoConfig := captureDest(t, repoConfig)
		env.MustRunAM("install", "gamma", "--scope", "repo")
		afterRepoConfig := captureDest(t, repoConfig)
		requireLinkPreserved(t, beforeRepoConfig, afterRepoConfig)
		requireTargetReplaced(t, beforeRepoConfig, afterRepoConfig)
		if !strings.Contains(string(afterRepoConfig.bytes), "gamma") {
			t.Fatalf("staged install did not edit the repo config target:\n%s", afterRepoConfig.bytes)
		}
		requireNoResidue(t, env.mustResolvePath("/workspace/.agents"), shared, stateRoot)
	})

	t.Run("cross-layer object alias fails before mutation", func(t *testing.T) {
		// The user layer resolves to the repo config object, so one
		// transaction would publish two layer paths to a single file. The
		// install stages a user config edit, so the transaction is
		// non-empty when the alias preflight fires.
		repoLockBefore := captureDest(t, repoLock)
		repoConfigBefore := captureDest(t, repoConfig)
		mustRelink(t, filepath.Join(env.workspace, ".agents", "shared", "repo-config.yaml"), userConfig)
		userConfigBefore := captureDest(t, userConfig)

		aliased := env.RunAM("install", "beta", "--scope", "user")
		env.AssertExitCode(aliased, 1)
		env.AssertOutputContains(aliased, "unsafe")
		requireDestUntouched(t, repoConfigBefore, captureDest(t, repoConfig))
		requireDestUntouched(t, repoLockBefore, captureDest(t, repoLock))
		requireDestUntouched(t, userConfigBefore, captureDest(t, userConfig))

		mustRelink(t, userTarget, userConfig)
		requireNoResidue(t, env.mustResolvePath("/workspace/.agents"), filepath.Dir(userConfig), stateRoot)
	})

	t.Run("v4 removal publishes through the contained repository link", func(t *testing.T) {
		before := captureDest(t, repoConfig)
		beforeRepoLock := captureDest(t, repoLock)

		env.AssertFileExists("/workspace/.factory/skills/gamma/SKILL.md")
		env.MustRunAM("remove", "gamma", "--scope", "repo")
		env.AssertFileAbsent("/workspace/.factory/skills/gamma")
		env.AssertFileExists("/workspace/.factory/skills/alpha/SKILL.md")

		after := captureDest(t, repoConfig)
		requireLinkPreserved(t, before, after)
		requireTargetReplaced(t, before, after)
		if !strings.Contains(string(after.bytes), "apiVersion: agent-marketplace/v4") || !strings.Contains(string(after.bytes), "sources:") || !strings.Contains(string(after.bytes), "include: [skill/alpha]") {
			t.Fatalf("repo removal was not published through the approved target:\n%s", after.bytes)
		}
		// Selection-only removal leaves exact acquisition locks unchanged.
		afterRepoLock := captureDest(t, repoLock)
		requireDestUntouched(t, beforeRepoLock, afterRepoLock)
		requireNoResidue(t, env.mustResolvePath("/workspace/.agents"), stateRoot)
	})

	t.Run("escapes and multiply linked targets fail before mutation", func(t *testing.T) {
		// Escaping repository config link: capturing the destination the
		// reconcile transaction would rewrite refuses before any mutation.
		env.WriteFile("/root/outside-escape.yaml", "apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources: {team: {git: '"+regURL+"', ref: main}}\nresources: {team: {include: [skill/alpha, skill/beta]}}\n")
		outside := env.mustResolvePath("/root/outside-escape.yaml")
		mustRelink(t, outside, repoConfig)
		outsideBefore := captureDest(t, outside)
		repoLockBefore := captureDest(t, repoLock)

		escaped := env.RunAM("lock")
		env.AssertExitCode(escaped, 1)
		env.AssertOutputContains(escaped, "unsafe")
		requireDestUntouched(t, outsideBefore, captureDest(t, outside))
		requireDestUntouched(t, repoLockBefore, captureDest(t, repoLock))

		// Restore the contained repository config link, then multiply link
		// the user lock from a second name while a new registry reference
		// dirties it.
		mustRelink(t, filepath.Join("shared", "repo-config.yaml"), repoConfig)
		env.CreateBareRepo("boundary-reg-2", SkillsOnly(Skill{Name: "delta", Description: "secondary registry skill"}))
		regURL2 := "file://" + filepath.Join(env.repos, "boundary-reg-2.git")
		if err := os.WriteFile(userTarget, []byte("apiVersion: agent-marketplace/v4\nharnesses: [factory]\nsources:\n  team: {git: '"+regURL+"', ref: main}\n  other: {git: '"+regURL2+"', ref: main}\nresources: {team: {include: [skill/alpha]}}\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		userLockBefore := captureDest(t, userLock)
		secondName := env.mustResolvePath("/root/lock-copy.yaml")
		if err := os.Link(userLock, secondName); err != nil {
			t.Fatal(err)
		}
		multiplied := env.RunAM("lock")
		env.AssertExitCode(multiplied, 1)
		env.AssertOutputContains(multiplied, "unsafe")
		requireDestUntouched(t, userLockBefore, captureDest(t, userLock))
		if err := os.Remove(secondName); err != nil {
			t.Fatal(err)
		}
		// With the second name removed the very same transaction succeeds,
		// proving the refusal left no journal or staging residue behind.
		env.MustRunAM("lock")
		requireNoResidue(t, env.mustResolvePath("/workspace/.agents"), filepath.Dir(userConfig), stateRoot)
	})

	t.Run("deinit purge removes the dotfile link and preserves its target", func(t *testing.T) {
		before := captureDest(t, userConfig)

		env.MustRunAM("deinit", "--scope", "user", "--purge-config")

		if _, err := os.Lstat(userConfig); !os.IsNotExist(err) {
			t.Fatalf("dotfile link survived purge: %v", err)
		}
		info, err := os.Stat(userTarget)
		if err != nil {
			t.Fatalf("dotfile target was removed: %v", err)
		}
		data, err := os.ReadFile(userTarget)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(before.bytes) ||
			info.Mode() != before.mode ||
			!info.ModTime().Equal(before.modTime) ||
			info.Sys().(*syscall.Stat_t).Ino != before.inode {
			t.Fatalf("purge touched the dotfile target: bytes %q→%q mode %v→%v mtime %v→%v inode %d→%d",
				before.bytes, data, before.mode, info.Mode(), before.modTime, info.ModTime(), before.inode, info.Sys().(*syscall.Stat_t).Ino)
		}
		if _, err := os.Lstat(userLock); !os.IsNotExist(err) {
			t.Fatalf("user lock survived purge: %v", err)
		}
		requireNoResidue(t, env.mustResolvePath("/root/.agents"), stateRoot)
	})
}
