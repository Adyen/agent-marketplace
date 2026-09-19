package cache

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTransientGitCredentialEnvironment(t *testing.T) {
	isolatedGitAuth(t)
	raw := "https://fixture-user:YOUR=TOKEN@example.invalid/repo.git?token=YOUR_QUERY#YOUR_FRAGMENT"
	env := append(gitEnv(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.username", "GIT_CONFIG_VALUE_0=fixture-user", "GIT_CONFIG_KEY_1=http.extraHeader", "GIT_CONFIG_VALUE_1=X-Fixture: synthetic")
	before := append([]string(nil), env...)
	got, public, err := gitCredentialEnvironment(env, raw)
	if err != nil || public != "https://example.invalid/repo.git" || !reflect.DeepEqual(env, before) {
		t.Fatalf("credential environment construction failed: %v", err)
	}
	values := map[string]string{}
	for _, entry := range got {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	if values["GIT_CONFIG_COUNT"] != "3" || values["GIT_CONFIG_KEY_2"] != "url."+raw+".insteadOf" || values["GIT_CONFIG_VALUE_2"] != public || values["GIT_CONFIG_VALUE_1"] != "X-Fixture: synthetic" {
		t.Fatal("credential rewrite overwrote trusted auth or used the wrong config count")
	}
	cmd := exec.Command("git", "ls-remote", "--get-url", "--", public)
	cmd.Dir, cmd.Env = t.TempDir(), got
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != raw {
		t.Fatalf("native Git did not reconstruct the source: %v", err)
	}
	for _, value := range []string{"https://example.invalid/repo.git", "ssh://git@example.invalid/repo.git", "git@example.invalid:repo.git", "file:///tmp/repo", "/tmp/repo"} {
		result, public, err := gitCredentialEnvironment(env, value)
		if err != nil || public != value || !reflect.DeepEqual(result, env) {
			t.Errorf("nonsecret source changed: %s (%v)", value, err)
		}
	}
	for _, count := range []string{"-1", "invalid"} {
		if _, _, err := gitCredentialEnvironment([]string{"GIT_CONFIG_COUNT=" + count}, raw); !errors.Is(err, ErrGitAuthConfigUntrusted) {
			t.Errorf("invalid count accepted: %s", count)
		}
	}
	if _, err := credentialFreeGitURL("https://YOUR_TOKEN_HERE%ZZ@example.invalid"); err == nil || strings.Contains(err.Error(), "YOUR_TOKEN_HERE") {
		t.Fatal("malformed source was accepted or exposed")
	}
}

func TestLegacyGitOriginRefusesUnsafeConfig(t *testing.T) {
	for _, kind := range []string{"config-symlink", "config-hardlink", "git-directory-symlink"} {
		t.Run(kind, func(t *testing.T) {
			repo, external := t.TempDir(), t.TempDir()
			original := filepath.Join(external, "config")
			want := []byte("external sentinel\n")
			if err := os.WriteFile(original, want, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(original)
			if err != nil {
				t.Fatal(err)
			}
			gitDir := filepath.Join(repo, ".git")
			if kind == "git-directory-symlink" {
				if err := os.Symlink(external, gitDir); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(gitDir, 0o700); err != nil {
					t.Fatal(err)
				}
				link := os.Symlink
				if kind == "config-hardlink" {
					link = os.Link
				}
				if err := link(original, filepath.Join(gitDir, "config")); err != nil {
					t.Fatal(err)
				}
			}
			if err := sanitizeLegacyGitOrigin(repo, "https://YOUR_TOKEN_HERE@example.invalid/repo.git"); !errors.Is(err, ErrGitAuthConfigUntrusted) {
				t.Fatalf("unsafe config accepted: %v", err)
			}
			got, err := os.ReadFile(original)
			if err != nil || string(got) != string(want) {
				t.Fatal("external config changed")
			}
			after, err := os.Stat(original)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("external config metadata changed")
			}
		})
	}
}
