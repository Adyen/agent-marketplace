package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGitURLRejectsDangerousInput(t *testing.T) {
	cases := []struct {
		url        string
		wantSubstr string
	}{
		{"", "registry url is required"},
		{"  ", "registry url is required"},
		{"-option https://example.com/repo.git", "option-like"},
		{"https://example.com/repo.git\n", "control character"},
		{"git://example.com/repo.git", "git:// protocol is not allowed"},
		{"ftp://example.com/repo.git", "unsupported protocol"},
		{"ext::sh -c 'pwned'", "unsupported transport helper"},
		{"remote::/usr/bin/evil", "unsupported transport helper"},
		{"helper::/usr/bin/evil", "unsupported transport helper"},
		{"https://-example.com/repo.git", "host starts with '-'"},
		{"ssh://-oProxyCommand=evil.com/repo.git", "host starts with '-'"},
		{"git@-example.com:repo.git", "host starts with '-'"},
		{"git@example.com:-repo.git", "path starts with '-'"},
		{"../etc/passwd", "path traversal"},
		{"./../escape/repo.git", "path traversal"},
		{"~/.ssh/id_rsa", "tilde path is not allowed"},
	}

	for _, tc := range cases {
		err := validateGitURL(tc.url)
		if err == nil {
			t.Errorf("validateGitURL(%q) expected error containing %q, got nil", tc.url, tc.wantSubstr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Errorf("validateGitURL(%q) error = %q, want substring %q", tc.url, err.Error(), tc.wantSubstr)
		}
	}
}

func TestValidateGitURLAllowsExpectedURLs(t *testing.T) {
	cases := []string{
		"https://example.com/org/repo.git",
		"http://example.com/org/repo.git",
		"ssh://git@example.com/org/repo.git",
		"git@example.com:org/repo.git",
		"https://user:pass@example.com/org/repo.git",
		"file:///tmp/repo.git",
		"/var/lib/repo.git",
		"./local/repo.git",
	}

	for _, u := range cases {
		if err := validateGitURL(u); err != nil {
			t.Errorf("validateGitURL(%q) unexpected error: %v", u, err)
		}
	}
}

func TestValidateGitRefRejectsDangerousInput(t *testing.T) {
	cases := []struct {
		ref        string
		wantSubstr string
	}{
		{"", "registry ref is required"},
		{"  ", "registry ref is required"},
		{"-force", "option-like"},
		{"/etc/passwd", "absolute or tilde path"},
		{"~main", "absolute or tilde path"},
		{"../main", "contains '..' or '@{'"},
		{"main@{1.day.ago}", "contains '..' or '@{'"},
		{"main\x00", "control character"},
		{"main.lock", "invalid suffix"},
		{"main.", "invalid suffix"},
		{"main/", "invalid suffix"},
		{"main with space", "invalid character"},
	}

	for _, tc := range cases {
		err := validateGitRef(tc.ref)
		if err == nil {
			t.Errorf("validateGitRef(%q) expected error containing %q, got nil", tc.ref, tc.wantSubstr)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSubstr) {
			t.Errorf("validateGitRef(%q) error = %q, want substring %q", tc.ref, err.Error(), tc.wantSubstr)
		}
	}
}

func TestValidateGitRefAllowsExpectedRefs(t *testing.T) {
	cases := []string{
		"main",
		"v1.2.3",
		"feature/foo-bar_baz",
		"refs/tags/v1.0.0",
		"origin/main",
		"deadbeef0123456789abcdefdeadbeef01234567",
	}

	for _, ref := range cases {
		if err := validateGitRef(ref); err != nil {
			t.Errorf("validateGitRef(%q) unexpected error: %v", ref, err)
		}
	}
}

func TestGitEnvAllowlist(t *testing.T) {
	t.Setenv("GIT_FOO", "bar")
	t.Setenv("GIT_PROTOCOL_HELPER", "/usr/bin/evil")
	t.Setenv("GIT_SSH_COMMAND", "ssh -o ProxyCommand=evil")
	t.Setenv("HOME", t.TempDir())
	env := gitEnv()

	got := make(map[string]string)
	for _, e := range env {
		key, val, _ := strings.Cut(e, "=")
		got[key] = val
	}

	for _, disallowed := range []string{"GIT_FOO", "GIT_PROTOCOL_HELPER"} {
		if _, ok := got[disallowed]; ok {
			t.Errorf("gitEnv() included disallowed variable %q", disallowed)
		}
	}

	if got["GIT_SSH_COMMAND"] != "ssh -o ProxyCommand=evil" {
		t.Errorf("gitEnv() GIT_SSH_COMMAND = %q, want %q", got["GIT_SSH_COMMAND"], "ssh -o ProxyCommand=evil")
	}

	for _, required := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"} {
		key, val, _ := strings.Cut(required, "=")
		if got[key] != val {
			t.Errorf("gitEnv() %s = %q, want %q", key, got[key], val)
		}
	}

	if _, ok := got["GIT_ALLOW_PROTOCOL"]; ok {
		t.Errorf("gitEnv() should not set GIT_ALLOW_PROTOCOL; protocol policy is enforced per-command via -c")
	}
}

func TestCloneOrPullRejectsDangerousURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := CloneOrPull("ext::sh -c 'evil'", "main")
	if err == nil || !strings.Contains(err.Error(), "unsupported transport helper") {
		t.Fatalf("CloneOrPull with ext URL should be rejected; got err = %v", err)
	}

	_, err = CloneOrPull("git://example.com/repo.git", "main")
	if err == nil || !strings.Contains(err.Error(), "git:// protocol is not allowed") {
		t.Fatalf("CloneOrPull with git:// URL should be rejected; got err = %v", err)
	}

	_, err = CloneOrPull("-option", "main")
	if err == nil || !strings.Contains(err.Error(), "option-like") {
		t.Fatalf("CloneOrPull with option-like URL should be rejected; got err = %v", err)
	}
}

func TestCloneOrPullRejectsDangerousRef(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// The URL is harmless; the ref is the attack vector.
	_, err := CloneOrPull("/dev/null", "--force")
	if err == nil || !strings.Contains(err.Error(), "option-like") {
		t.Fatalf("CloneOrPull with option-like ref should be rejected; got err = %v", err)
	}

	_, err = CloneOrPull("/dev/null", "main@{1.day.ago}")
	if err == nil || !strings.Contains(err.Error(), "contains '..' or '@{'") {
		t.Fatalf("CloneOrPull with reflog ref should be rejected; got err = %v", err)
	}
}

func TestCloneOrPullRejectsSymlinkedCacheRootAndPaths(t *testing.T) {
	tests := []struct {
		name  string
		setup func(cacheRoot, external, cachePath string) error
	}{
		{
			name: "cache root ancestor",
			setup: func(cacheRoot, external, _ string) error {
				return os.Symlink(external, filepath.Dir(cacheRoot))
			},
		},
		{
			name: "cache root",
			setup: func(cacheRoot, external, _ string) error {
				if err := os.MkdirAll(filepath.Dir(cacheRoot), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, cacheRoot)
			},
		},
		{
			name: "registry hash directory",
			setup: func(cacheRoot, external, cachePath string) error {
				if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
					return err
				}
				hashDir := filepath.Dir(cachePath)
				return os.Symlink(external, hashDir)
			},
		},
		{
			name: "registry ref directory",
			setup: func(cacheRoot, external, cachePath string) error {
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
					return err
				}
				return os.Symlink(external, cachePath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			cacheRoot := CacheDir()
			origin := filepath.Join(t.TempDir(), "origin.git")
			cachePath := RegistryCachePath(origin, "main")
			external := t.TempDir()
			if err := tt.setup(cacheRoot, external, cachePath); err != nil {
				t.Fatalf("setup symlinked cache path: %v", err)
			}

			if got, err := CloneOrPull(origin, "main"); err == nil {
				t.Fatalf("expected symlinked cache path to be rejected, got %q", got)
			}
			entries, err := os.ReadDir(external)
			if err != nil {
				t.Fatalf("read external directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("registry acquisition reached external cache target: %v", entries)
			}
		})
	}
}

func TestResolveRemoteRefIsResolvedToCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	origin, working := createBareRepoWithWorkingClone(t)
	writeAndPushCommit(t, working, "skill.txt", "version one\n", "initial commit")

	cachePath, err := CloneOrPull(origin, "main")
	if err != nil {
		t.Fatalf("CloneOrPull: %v", err)
	}

	resolved, err := resolveRemoteRef(cachePath, "main")
	if err != nil {
		t.Fatalf("resolveRemoteRef: %v", err)
	}

	// Verify the resolved value is a 40-character hex commit and is checked out.
	if len(resolved) != 40 {
		t.Fatalf("resolved ref should be a 40-character hex commit, got %q", resolved)
	}

	// confirm checkout used the resolved hash (no symbolic ref remains on disk as HEAD)
	out := runGitTestCommandForSecurity(t, cachePath, "rev-parse", "HEAD")
	if strings.TrimSpace(out) != resolved {
		t.Fatalf("HEAD = %q, want %q", out, resolved)
	}
}

func runGitTestCommandForSecurity(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
