package cache

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBranchCredentialRedaction(t *testing.T) {
	isolatedGitAuth(t)
	marker := filepath.Join(t.TempDir(), "invoked")
	t.Setenv("CREDENTIAL_PROBE_MARKER", marker)
	wrapper := authFile(t, "git", "#!/bin/sh\nprintf 'invoked' > \"$CREDENTIAL_PROBE_MARKER\"\nprintf '%s\\n' \"$*\" >&2\nexit 128\n", 0o700)
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	raw := "https://YOUR_USER_HERE:YOUR_TOKEN_HERE@example.invalid/repo.git?token=YOUR_QUERY_HERE#YOUR_FRAGMENT_HERE"
	_, err := DefaultBranch(raw)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 128 {
		t.Fatalf("wrong Git error: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "invoked" {
		t.Fatal("Git fixture was not reached")
	}
	for _, text := range []string{err.Error(), logs.String()} {
		if !strings.Contains(text, "example.invalid/repo.git") {
			t.Fatal("redaction discarded diagnostic context")
		}
		for _, secret := range []string{"YOUR_USER_HERE", "YOUR_TOKEN_HERE", "YOUR_QUERY_HERE", "YOUR_FRAGMENT_HERE"} {
			if strings.Contains(text, secret) {
				t.Errorf("Git diagnostic exposed %s", secret)
			}
		}
	}
}

func TestGitLegacyCredentialURLPersistence(t *testing.T) {
	isolatedGitAuth(t)
	f := newGitAuthServer(t)
	t.Setenv("GIT_SSL_CAINFO", f.ca)
	u, err := url.Parse(f.url)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("fixture-user", "synthetic-auth-secret")
	source := u.String()
	path, err := CloneOrPull(source, "main")
	if err != nil {
		t.Fatalf("credentialed clone failed: %v", err)
	}
	check := func() {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(path, ".git", "config"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "synthetic-auth-secret") || strings.Contains(string(data), "fixture-user") {
			t.Error("URL credentials persisted in Git config")
		}
		if !strings.Contains(string(data), f.url) {
			t.Error("public origin missing")
		}
		if _, err := os.Lstat(filepath.Join(path, ".git", "FETCH_HEAD")); !os.IsNotExist(err) {
			t.Errorf("FETCH_HEAD retained: %v", err)
		}
	}
	check()
	before := f.accepted.Load()
	want := commitExactGitFile(t, f.repo, "second\n")
	gitTest(t, f.repo, "update-server-info")
	if _, err := CloneOrPull(source, "main"); err != nil {
		t.Fatalf("credentialed refresh failed: %v", err)
	}
	assertExactHead(t, path, want)
	if f.accepted.Load() <= before {
		t.Fatal("refresh did not authenticate")
	}
	check()
	gitTest(t, path, "config", "remote.origin.url", source)
	if err := os.WriteFile(filepath.Join(path, ".git", "FETCH_HEAD"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CloneOrPull(source, "main"); err != nil {
		t.Fatalf("legacy migration failed: %v", err)
	}
	check()
	f.server.Close()
	if stale, err := CloneOrPull(source, "main"); err != nil || stale != path {
		t.Fatalf("stale fallback failed: %v", err)
	}
	assertExactHead(t, path, want)
	check()
}
