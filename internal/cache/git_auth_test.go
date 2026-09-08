package cache

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func isolatedGitAuth(t testing.TB) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "SSH_") || upper == "HTTP_PROXY" || upper == "HTTPS_PROXY" || upper == "ALL_PROXY" || upper == "NO_PROXY" {
			t.Setenv(key, "")
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func authFile(t testing.TB, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

type gitAuthServer struct {
	repo, url, ca        string
	accepted, challenged atomic.Int64
	server               *httptest.Server
}

func newGitAuthServer(t *testing.T) *gitAuthServer {
	t.Helper()
	f := &gitAuthServer{repo: newExactGitRepository(t, "sha1")}
	gitTest(t, f.repo, "symbolic-ref", "HEAD", "refs/heads/main")
	gitTest(t, f.repo, "update-server-info")
	files := http.FileServer(http.Dir(filepath.Dir(f.repo)))
	f.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "fixture-user" || password != "synthetic-auth-secret" {
			f.challenged.Add(1)
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.accepted.Add(1)
		files.ServeHTTP(w, r)
	}))
	f.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	f.url = f.server.URL + "/" + filepath.Base(f.repo)
	f.ca = authFile(t, "ca.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.server.Certificate().Raw})), 0o600)
	return f
}

func TestGitAcquisitionSourceOperands(t *testing.T) {
	for _, tt := range []struct {
		args        []string
		source      string
		acquisition bool
	}{
		{[]string{"clone", "--", "https://example.invalid/repo", "/cache"}, "https://example.invalid/repo", true},
		{[]string{"-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "clone", "--mirror", "--no-tags", "--", "file:///remote.git", "/cache"}, "file:///remote.git", true},
		{[]string{"fetch", "--tags", "--prune", "--", "origin"}, "origin", true},
		{[]string{"fetch", "--force", "--no-tags", "--no-write-fetch-head", "--", "https://example.invalid/repo", "main:resolved"}, "https://example.invalid/repo", true},
		{[]string{"-c", "core.fsmonitor=false", "fetch", "--force", "--no-tags", "--no-write-fetch-head", "--", "/store", "0123"}, "/store", true},
		{[]string{"fetch", "origin"}, "origin", true},
		{[]string{"fetch"}, "origin", true},
		{[]string{"init", "--bare"}, "", false},
	} {
		if source, acquisition := gitAcquisition(tt.args); source != tt.source || acquisition != tt.acquisition {
			t.Fatalf("incorrect operand selection for %v", tt.args)
		}
	}
}

func TestGitAuthSkipsReadersForLocalTransports(t *testing.T) {
	for _, transport := range []string{"path", "file"} {
		t.Run(transport, func(t *testing.T) {
			isolatedGitAuth(t)
			repo := newExactGitRepository(t, "sha1")
			gitTest(t, repo, "symbolic-ref", "HEAD", "refs/heads/main")
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "readers")
			t.Setenv("AUTH_READER_MARKER", marker)
			t.Setenv("AUTH_REAL_GIT", realGit)
			wrapper := authFile(t, "git", "#!/bin/sh\nfor arg do\n  if [ \"$arg\" = --show-origin ]; then echo reader >> \"$AUTH_READER_MARKER\"; fi\ndone\nexec \"$AUTH_REAL_GIT\" \"$@\"\n", 0o700)
			t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
			for _, key := range []string{"GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL"} {
				t.Setenv(key, authFile(t, "config", "[credential]\n\thelper = fixture-helper\n", 0o600))
			}
			source := repo
			if transport == "file" {
				source = "file://" + filepath.ToSlash(repo)
			}
			canonical := "https://example.invalid/local-transport"
			for generation := 0; generation < 2; generation++ {
				if generation > 0 {
					commitExactGitFile(t, repo, "second\n")
				}
				oid, err := ResolveExactGitRef(canonical, source, "main")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := AcquireExactGit(canonical, source, oid); err != nil {
					t.Fatal(err)
				}
				path, err := CloneOrPull(source, "main")
				if err != nil {
					t.Fatal(err)
				}
				if generation == 0 {
					gitTest(t, path, "config", "remote.origin.url", "https://example.invalid/local-alias")
					gitTest(t, path, "config", "url."+source+".insteadOf", "https://example.invalid/local-alias")
				}
			}
			mutable := RegistryCachePath(source, "main")
			gitTest(t, mutable, "config", "credential.helper", "!exit 1")
			if _, err := execGit(mutable, []string{"fetch", "--", "origin"}); !errors.Is(err, ErrGitAuthConfigUntrusted) {
				t.Fatal("local transport bypassed repository authentication validation")
			}
			for _, source := range []string{"ssh://git@example.invalid/repo", "git@example.invalid:repo"} {
				if _, err := gitAuthEnvironment(context.Background(), "", gitEnv(), source); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("non-HTTP transport invoked a trusted authentication reader")
			}
			for _, source := range []string{"https://example.invalid/fixture", "http://example.invalid", "https://example.invalid/fixture?query=synthetic"} {
				if _, err := gitAuthEnvironment(context.Background(), "", gitEnv(), source); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(marker); err != nil {
					t.Fatal("positive reader-invocation control did not execute")
				}
			}
		})
	}
}

func TestGitPrivateHTTPSAuthentication(t *testing.T) {
	for _, mode := range []string{"caller-askpass-ca", "system-helper-global-ca", "ordered-url-helpers", "helper-reset", "default-global-order"} {
		t.Run(mode, func(t *testing.T) {
			isolatedGitAuth(t)
			f := newGitAuthServer(t)
			marker := filepath.Join(t.TempDir(), "helper-order")
			t.Setenv("AUTH_MARKER", marker)
			complete := `!f() { test "$1" != get || { printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; }; f`
			switch mode {
			case "caller-askpass-ca":
				askpass := authFile(t, "askpass", "#!/bin/sh\nprintf 'askpass\\n' >> \"$AUTH_MARKER\"\ncase \"$1\" in *Username*) echo fixture-user;; *) echo synthetic-auth-secret;; esac\n", 0o700)
				t.Setenv("GIT_ASKPASS", askpass)
				t.Setenv("GIT_SSL_CAINFO", f.ca)
			case "system-helper-global-ca":
				t.Setenv("GIT_CONFIG_SYSTEM", authFile(t, "system", "[credential]\n\thelper = "+fmt.Sprintf("%q", complete)+"\n", 0o600))
				t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", fmt.Sprintf("[http %q]\n\tsslCAInfo = %q\n", f.server.URL, f.ca), 0o600))
			case "ordered-url-helpers", "helper-reset", "default-global-order":
				first := `!f() { test "$1" != get || { echo first >> "$AUTH_MARKER"; printf 'username=fixture-user\n'; }; }; f`
				second := `!f() { test "$1" != get || { echo second >> "$AUTH_MARKER"; printf 'password=synthetic-auth-secret\n'; }; }; f`
				global := fmt.Sprintf("[credential %q]\n\thelper = %q\n", f.server.URL, second)
				if mode == "helper-reset" {
					first = `!f() { echo unexpected >> "$AUTH_MARKER"; }; f`
					global = fmt.Sprintf("[credential %q]\n\thelper =\n\thelper = %q\n", f.server.URL, complete)
				}
				global += "[credential \"https://example.invalid\"]\n\thelper = \"!echo wrong-url >> \\\"$AUTH_MARKER\\\"\"\n"
				system := fmt.Sprintf("[credential]\n\thelper = %q\n[http %q]\n\tsslCAInfo = %q\n", first, f.server.URL, f.ca)
				if mode == "default-global-order" {
					xdg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git")
					if err := os.MkdirAll(xdg, 0o700); err != nil {
						t.Fatal(err)
					}
					for path, data := range map[string]string{filepath.Join(xdg, "config"): system, filepath.Join(os.Getenv("HOME"), ".gitconfig"): global} {
						if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
						t.Fatal(err)
					}
				} else {
					t.Setenv("GIT_CONFIG_SYSTEM", authFile(t, "system", system, 0o600))
					t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", global, 0o600))
				}
			}
			canonical := "https://example.invalid/auth-fixture"
			oid, err := ResolveExactGitRef(canonical, f.url, "main")
			if err != nil {
				t.Fatalf("authenticated clone: %v", err)
			}
			snapshot, err := AcquireExactGit(canonical, f.url, oid)
			if err != nil {
				t.Fatalf("authenticated snapshot: %v", err)
			}
			for _, path := range []string{filepath.Join(exactGitObjectStorePath(canonical), "config"), filepath.Join(snapshot, ".git", "config")} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "synthetic-auth-secret") || strings.Contains(string(data), "[credential") {
					t.Fatal("authentication configuration persisted in the cache")
				}
			}
			before := f.accepted.Load()
			want := commitExactGitFile(t, f.repo, "second\n")
			gitTest(t, f.repo, "update-server-info")
			got, err := ResolveExactGitRef(canonical, f.url, "main")
			if err != nil || got.Hex() != want {
				t.Fatalf("authenticated fetch did not advance: %v", err)
			}
			if f.accepted.Load() <= before || f.challenged.Load() == 0 {
				t.Fatal("fixture did not exercise authenticated clone and fetch")
			}
			ordered := mode == "ordered-url-helpers" || mode == "default-global-order"
			if ordered || mode == "caller-askpass-ca" {
				data, err := os.ReadFile(marker)
				if err != nil {
					t.Fatal(err)
				}
				if ordered && (strings.ReplaceAll(string(data), "first\nsecond\n", "") != "" || strings.Count(string(data), "second\n") < 2) {
					t.Fatal("URL matching or helper order was not preserved")
				}
			} else if mode == "helper-reset" {
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("reset or unmatched helper ran")
				}
			}
		})
	}
}

func TestGitPrivateHTTPSRejectsMissingCAAndLocalHelper(t *testing.T) {
	isolatedGitAuth(t)
	f := newGitAuthServer(t)
	marker := filepath.Join(t.TempDir(), "local-helper")
	t.Setenv("AUTH_MARKER", marker)
	complete := `!f() { test "$1" != get || printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; f`
	t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", fmt.Sprintf("[credential]\n\thelper = %q\n[http]\n\tsslVerify = false\n", complete), 0o600))
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	canonical := "https://example.invalid/negative-auth"
	if _, err := ResolveExactGitRef(canonical, f.url, "main"); err == nil || f.accepted.Load() != 0 {
		t.Fatal("HTTPS accepted an untrusted certificate")
	}
	t.Setenv("GIT_SSL_CAINFO", f.ca)
	if _, err := ResolveExactGitRef(canonical, f.url, "main"); err != nil {
		t.Fatalf("positive CA control failed: %v", err)
	}
	store := exactGitObjectStorePath(canonical)
	gitTest(t, store, "config", "credential."+f.server.URL+".helper", `!echo local >> "$AUTH_MARKER"`)
	before := f.accepted.Load()
	if _, err := ResolveExactGitRef(canonical, f.url, "main"); err == nil {
		t.Fatal("repository-local helper was accepted")
	}
	if f.accepted.Load() != before {
		t.Fatal("untrusted repository reached network")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("repository-local helper executed")
	}
}

func TestGitPrivateHTTPSLegacyAndUntrustedWorkingDirectory(t *testing.T) {
	isolatedGitAuth(t)
	f := newGitAuthServer(t)
	marker := filepath.Join(t.TempDir(), "untrusted-command")
	t.Setenv("AUTH_MARKER", marker)
	work := filepath.Join(filepath.Dir(f.repo), "work")
	gitTest(t, work, "config", "credential.helper", `!echo local >> "$AUTH_MARKER"`)
	t.Chdir(work)
	hookDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hookDir, "post-checkout"), []byte("#!/bin/sh\necho hook >> \"$AUTH_MARKER\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	complete := `!f() { test "$1" != get || printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; f`
	global := fmt.Sprintf("[credential]\n\thelper = %q\n[http]\n\tsslCAInfo = %q\n[core]\n\thooksPath = %q\n[protocol.ext]\n\tallow = always\n[url \"ext::false\"]\n\tinsteadOf = %q\n", complete, f.ca, hookDir, f.server.URL)
	t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", global, 0o600))
	path, err := CloneOrPull(f.url, "main")
	if err != nil {
		t.Fatalf("legacy authenticated clone: %v", err)
	}
	want := commitExactGitFile(t, f.repo, "second\n")
	gitTest(t, f.repo, "update-server-info")
	if _, err := CloneOrPull(f.url, "main"); err != nil {
		t.Fatalf("legacy authenticated fetch: %v", err)
	}
	assertExactHead(t, path, want)
	gitTest(t, path, "config", "remote.origin.url", "file:///synthetic-auth-alias")
	gitTest(t, path, "config", "url."+f.url+".insteadOf", "file:///synthetic-auth-alias")
	before := f.accepted.Load()
	if err := runGit(path, "fetch"); err != nil || f.accepted.Load() <= before {
		t.Fatalf("authenticated fetch through a rewritten local alias failed: %v", err)
	}
	for _, kind := range []string{"helper", "include", "worktree"} {
		t.Run(kind, func(t *testing.T) {
			local := filepath.Join(path, ".git", "config")
			original, err := os.ReadFile(local)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(local, original, 0o600); err != nil {
					t.Error(err)
				}
			})
			switch kind {
			case "helper":
				gitTest(t, path, "config", "credential."+f.server.URL+".helper", `!echo local >> "$AUTH_MARKER"`)
			case "include":
				included := authFile(t, "included", "[credential]\n\thelper = \"!echo included >> \\\"$AUTH_MARKER\\\"\"\n", 0o600)
				gitTest(t, path, "config", "include.path", included)
			case "worktree":
				gitTest(t, path, "config", "extensions.worktreeConfig", "true")
				gitTest(t, path, "config", "--worktree", "credential.helper", `!echo worktree >> "$AUTH_MARKER"`)
			}
			before := f.accepted.Load()
			_, err = execGit(path, []string{"fetch", "origin"})
			if !errors.Is(err, ErrGitAuthConfigUntrusted) || f.accepted.Load() != before {
				t.Fatal("untrusted local configuration did not fail closed before network")
			}
		})
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("repository-local helper or unrelated trusted command executed")
	}
}

func TestGitAuthRejectsUnsafeTrustedConfiguration(t *testing.T) {
	for _, kind := range []string{"include", "includeIf", "writable", "relative", "empty", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			isolatedGitAuth(t)
			config := "[credential]\n\thelper = synthetic-auth-secret\n"
			mode := os.FileMode(0o600)
			switch kind {
			case "include":
				config += "[include]\n\tpath = never-read\n"
			case "includeIf":
				config += "[includeIf \"gitdir:/never-matches/\"]\n\tpath = never-read\n"
			case "writable":
				mode = 0o666
			case "malformed":
				config = "[synthetic-auth-secret\n"
			}
			path := authFile(t, "global", config, mode)
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "relative":
				path = "synthetic-auth-secret"
			case "empty":
				path = ""
			}
			t.Setenv("GIT_CONFIG_GLOBAL", path)
			_, err := execGit("", []string{"clone", "--", "https://127.0.0.1:1/never", filepath.Join(t.TempDir(), "clone")})
			if !errors.Is(err, ErrGitAuthConfigUntrusted) {
				t.Fatal("unsafe configuration was not rejected explicitly")
			}
			if strings.Contains(err.Error(), "synthetic-auth-secret") {
				t.Fatal("configuration diagnostic leaked a secret")
			}
		})
	}
}

func TestGitAuthEnvironmentPreservesOnlyApprovedInputs(t *testing.T) {
	isolatedGitAuth(t)
	t.Setenv("GIT_ASKPASS", "/synthetic/askpass")
	t.Setenv("GIT_SSL_CAINFO", "/synthetic/ca.pem")
	t.Setenv("GIT_SSL_CAPATH", "/synthetic/certs")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "synthetic-auth-secret")
	t.Setenv("GIT_SSL_NO_VERIFY", "1")
	t.Setenv("GIT_TRACE", "1")
	env := strings.Join(gitEnv(), "\n")
	for _, value := range []string{"GIT_ASKPASS=/synthetic/askpass", "GIT_SSL_CAINFO=/synthetic/ca.pem", "GIT_SSL_CAPATH=/synthetic/certs", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"} {
		if !strings.Contains(env, value+"\n") && !strings.HasSuffix(env, value) {
			t.Fatal("approved caller input or hardening override was lost")
		}
	}
	for _, value := range []string{"synthetic-auth-secret", "GIT_CONFIG_COUNT", "GIT_SSL_NO_VERIFY", "GIT_TRACE", "GIT_ASKPASS=echo"} {
		if strings.Contains(env, value) {
			t.Fatal("unsafe caller configuration was retained")
		}
	}
}

func TestGitPrivateHTTPSDoesNotExposeHelperDiagnostics(t *testing.T) {
	for _, kind := range []string{"exact", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			isolatedGitAuth(t)
			f := newGitAuthServer(t)
			t.Setenv("GIT_SSL_CAINFO", f.ca)
			marker := filepath.Join(t.TempDir(), "failed-helper")
			t.Setenv("AUTH_MARKER", marker)
			helper := `!f() { echo invoked >> "$AUTH_MARKER"; echo synthetic-auth-secret >&2; exit 1; }; f`
			t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", fmt.Sprintf("[credential]\n\thelper = %q\n", helper), 0o600))
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			var err error
			if kind == "exact" {
				err = runExactGitWithContext(context.Background(), "", "clone", "--mirror", "--", f.url, filepath.Join(t.TempDir(), "clone"))
			} else {
				err = runGit("", "clone", f.url, filepath.Join(t.TempDir(), "clone"))
				if !strings.Contains(logs.String(), "git command end") {
					t.Fatal("Git failure logging was not exercised")
				}
			}
			if err == nil {
				t.Fatal("invalid authentication succeeded")
			}
			if _, statErr := os.Stat(marker); statErr != nil {
				t.Fatal("failing helper was not exercised")
			}
			if strings.Contains(err.Error()+logs.String(), "synthetic-auth-secret") {
				t.Fatal("helper diagnostic leaked a secret")
			}
		})
	}
}
