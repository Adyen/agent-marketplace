package acquire

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// spec: marketplace-source-observation/a-branch-resolves-to-its-tip-commit
func TestResolveRemoteRefReturnsAdvancedBranchTip(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "first"})
	bare := bareGitFixture(t, repo)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo, "second")
	want := gitTestOutput(t, repo, "rev-parse", "HEAD")
	runBareGitTestCommand(t, bare, "fetch", "--quiet", repo, "+refs/heads/main:refs/heads/main")

	got, err := ResolveRemoteRef(context.Background(), "file://"+bare, "main", Credentials{}, time.Minute)
	if err != nil {
		t.Fatalf("ResolveRemoteRef: %v", err)
	}
	if got != want {
		t.Fatalf("resolved commit = %q, want advanced branch tip %q", got, want)
	}
}

// spec: marketplace-source-observation/an-annotated-tag-resolves-to-the-peeled-commit
func TestResolveRemoteRefResolvesAnnotatedAndLightweightTags(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "tag target"})
	want := gitTestOutput(t, repo, "rev-parse", "HEAD")
	runGitTestCommand(t, repo, "git", "tag", "-a", "annotated", "-m", "annotated release", "HEAD")
	runGitTestCommand(t, repo, "git", "tag", "lightweight", "HEAD")
	bare := bareGitFixture(t, repo)

	for _, ref := range []string{"annotated", "lightweight"} {
		t.Run(ref, func(t *testing.T) {
			got, err := ResolveRemoteRef(context.Background(), "file://"+bare, ref, Credentials{}, time.Minute)
			if err != nil {
				t.Fatalf("ResolveRemoteRef(%q): %v", ref, err)
			}
			if got != want {
				t.Fatalf("resolved commit = %q, want tag target %q", got, want)
			}
		})
	}
}

func TestResolveRemoteRefIgnoresTagsSharingAPrefix(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "release"})
	want := gitTestOutput(t, repo, "rev-parse", "HEAD")
	runGitTestCommand(t, repo, "git", "tag", "-a", "v1", "-m", "release", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("later"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repo, "later")
	runGitTestCommand(t, repo, "git", "tag", "v1.0.0", "HEAD")
	runGitTestCommand(t, repo, "git", "tag", "v10", "HEAD")
	bare := bareGitFixture(t, repo)

	got, err := ResolveRemoteRef(context.Background(), "file://"+bare, "v1", Credentials{}, time.Minute)
	if err != nil {
		t.Fatalf("ResolveRemoteRef: %v", err)
	}
	if got != want {
		t.Fatalf("resolved commit = %q, want annotated v1 peeled commit %q", got, want)
	}
}

func TestRemoteRefPatternsMatchExactNames(t *testing.T) {
	patterns, _, _, _ := remoteRefPatterns("v1")
	if got, want := strings.Join(patterns, " "), "refs/heads/v1 refs/tags/v1 refs/tags/v1^{}"; got != want {
		t.Fatalf("remoteRefPatterns(v1) = %q, want %q", got, want)
	}
	patterns, _, _, _ = remoteRefPatterns("refs/tags/v1")
	if got, want := strings.Join(patterns, " "), "refs/tags/v1 refs/tags/v1^{}"; got != want {
		t.Fatalf("remoteRefPatterns(refs/tags/v1) = %q, want %q", got, want)
	}
}

// spec: marketplace-source-observation/a-full-commit-resolves-without-contacting-the-origin
func TestResolveRemoteRefReturnsFullCommitWithoutGitProcess(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	for _, length := range []int{40, 64} {
		commit := strings.Repeat("a", length)
		got, err := ResolveRemoteRef(context.Background(), "http://127.0.0.1:1/unreachable.git", commit, Credentials{}, time.Second)
		if err != nil {
			t.Fatalf("ResolveRemoteRef(%d-char full commit): %v", length, err)
		}
		if got != commit {
			t.Fatalf("resolved commit = %q, want %q", got, commit)
		}
	}
}

// spec: marketplace-source-observation/a-short-ref-naming-both-a-branch-and-a-tag-fails-as-ambiguous
func TestResolveRemoteRefRejectsAmbiguousShortRef(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "branch and tag"})
	runGitTestCommand(t, repo, "git", "tag", "main", "HEAD")
	bare := bareGitFixture(t, repo)

	got, err := ResolveRemoteRef(context.Background(), "file://"+bare, "main", Credentials{}, time.Minute)
	if !errors.Is(err, ErrAmbiguousRef) {
		t.Fatalf("ResolveRemoteRef error = %v, want ErrAmbiguousRef", err)
	}
	if got != "" {
		t.Fatalf("resolved commit = %q, want empty on ambiguity", got)
	}
}

func TestResolveRemoteRefReturnsNotFoundSentinel(t *testing.T) {
	repo := createGitRepo(t, map[string]string{"README.md": "available"})
	bare := bareGitFixture(t, repo)

	got, err := ResolveRemoteRef(context.Background(), "file://"+bare, "missing", Credentials{}, time.Minute)
	if !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("ResolveRemoteRef error = %v, want ErrRefNotFound", err)
	}
	if got != "" {
		t.Fatalf("resolved commit = %q, want empty for a missing ref", got)
	}
}

// spec: marketplace-source-observation/ref-resolution-errors-and-logs-never-contain-credentials
func TestResolveRemoteRefRedactsCredentialErrorsAndLogs(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	const token = "probe-token-secret"
	const password = "url-password-secret"
	origin := "http://probe-user:" + password + "@" + address + "/unreachable.git"
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	_, err = ResolveRemoteRef(context.Background(), origin, "main", Credentials{Token: token}, time.Second)
	if err == nil {
		t.Fatal("ResolveRemoteRef unexpectedly succeeded against an unreachable origin")
	}
	if !strings.Contains(err.Error(), "ls-remote --") || !strings.Contains(err.Error(), "***@") {
		t.Fatalf("error did not prove redacted Git failure: %v", err)
	}
	for _, secret := range []string{token, password} {
		if strings.Contains(err.Error(), secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("credential %q leaked in error or log: error=%v logs=%q", secret, err, logs.String())
		}
	}
}

func TestSourceHostNormalizesHostAndLocalOrigins(t *testing.T) {
	for _, tc := range []struct {
		origin string
		want   string
	}{
		{origin: "https://User:password@EXAMPLE.COM:8443/group/repo.git", want: "example.com:8443"},
		{origin: "git@GitHub.COM:group/repo.git", want: "github.com"},
		{origin: "git@[2001:DB8:0:0:0:0:0:1]:repo.git", want: "2001:db8:0:0:0:0:0:1"},
		{origin: "[2001:db8:0:0:0:0:0:1]:repo.git", want: "2001:db8:0:0:0:0:0:1"},
		{origin: "ssh://user:password@Code.Example:22/group/repo.git", want: "code.example:22"},
		{origin: "file:///tmp/registry.git", want: "local"},
		{origin: "/tmp/registry.git", want: "local"},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			got, err := SourceHost(tc.origin)
			if err != nil {
				t.Fatalf("SourceHost: %v", err)
			}
			if got != tc.want {
				t.Fatalf("SourceHost = %q, want %q", got, tc.want)
			}
		})
	}
}

func bareGitFixture(t *testing.T, repo string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	cmd := exec.Command("git", "clone", "--bare", "--quiet", repo, bare)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone bare fixture: %v\n%s", err, output)
	}
	return bare
}

func gitTestOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func runBareGitTestCommand(t *testing.T, bare string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"--git-dir", bare}, args...)
	cmd := exec.Command("git", cmdArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
