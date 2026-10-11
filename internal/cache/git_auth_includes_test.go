package cache

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func writeGitAuthConfig(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// spec: git-authentication/included-authentication-reaches-clone-and-fetch
// spec: git-authentication/included-settings-do-not-activate-unrelated-git-behavior
// spec: git-authentication/worktree-authentication-does-not-gain-authority
func TestGitPrivateHTTPSIncludedAuthentication(t *testing.T) {
	for _, scope := range []string{"system", "global"} {
		for _, acquisition := range []string{"exact", "legacy"} {
			t.Run(scope+"/"+acquisition, func(t *testing.T) {
				isolatedGitAuth(t)
				f := newGitAuthServer(t)
				marker := filepath.Join(t.TempDir(), "unrelated-command")
				t.Setenv("AUTH_MARKER", marker)
				complete := `!f() { test "$1" != get || printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; f`
				auth := fmt.Sprintf("[credential]\n\thelper = %q\n[http]\n\tsslCAInfo = %q\n\tsslVerify = false\n[core]\n\thooksPath = %q\n[url \"ext::false\"]\n\tinsteadOf = %q\n", complete, f.ca, filepath.Dir(marker), f.server.URL)
				if err := os.WriteFile(filepath.Join(filepath.Dir(marker), "post-checkout"), []byte("#!/bin/sh\necho hook >> \"$AUTH_MARKER\"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				root := filepath.Join(os.Getenv("HOME"), "git auth", "root")
				nested := filepath.Join(filepath.Dir(root), "nested", "config")
				leaf := filepath.Join(filepath.Dir(root), "auth")
				rootData := "[include]\n\tpath = \"nested/config\"\n"
				nestedData := "[include]\n\tpath = ../auth\n"
				writeGitAuthConfig(t, root, rootData)
				writeGitAuthConfig(t, nested, nestedData)
				writeGitAuthConfig(t, leaf, auth)
				t.Setenv("GIT_CONFIG_"+strings.ToUpper(scope), root)

				work := filepath.Join(filepath.Dir(f.repo), "work")
				gitTest(t, work, "config", "credential.helper", `!echo caller >> "$AUTH_MARKER"`)
				linked := filepath.Join(t.TempDir(), "linked")
				gitTest(t, work, "worktree", "add", "--detach", linked)
				t.Chdir(linked)
				canonical := "https://example.invalid/included-auth"
				acquire := func() string {
					t.Helper()
					if acquisition == "legacy" {
						path, err := CloneOrPull(f.url, "main")
						if err != nil {
							t.Fatal(err)
						}
						return path
					}
					oid, err := ResolveExactGitRef(canonical, f.url, "main")
					if err != nil {
						t.Fatal(err)
					}
					path, err := AcquireExactGit(canonical, f.url, oid)
					if err != nil {
						t.Fatal(err)
					}
					return path
				}
				path := acquire()
				assertExactHead(t, path, strings.TrimSpace(gitTest(t, f.repo, "rev-parse", "HEAD")))
				before := f.accepted.Load()
				want := commitExactGitFile(t, f.repo, "included second\n")
				gitTest(t, f.repo, "update-server-info")
				path = acquire()
				assertExactHead(t, path, want)
				if f.accepted.Load() <= before || f.challenged.Load() == 0 {
					t.Fatal("included authentication did not reach both clone and fetch")
				}
				configPaths := []string{filepath.Join(path, ".git", "config")}
				if acquisition == "exact" {
					configPaths = append(configPaths, filepath.Join(exactGitObjectStorePath(canonical), "config"))
				}
				for _, configPath := range configPaths {
					data, err := os.ReadFile(configPath)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(data), "synthetic-auth-secret") || strings.Contains(string(data), "[credential") {
						t.Fatal("included authentication persisted in registry cache configuration")
					}
				}
				for path, want := range map[string]string{root: rootData, nested: nestedData, leaf: auth} {
					if got, err := os.ReadFile(path); err != nil || string(got) != want {
						t.Fatalf("configuration changed: %s", path)
					}
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("caller authentication or an unrelated included command executed")
				}
			})
		}
	}
}

// spec: git-authentication/included-helper-order-and-resets-are-preserved
// spec: git-authentication/missing-optional-includes-contribute-no-authentication
func TestGitAuthIncludeOrderAndPaths(t *testing.T) {
	isolatedGitAuth(t)
	home := os.Getenv("HOME")
	shared := filepath.Join(home, "shared")
	writeGitAuthConfig(t, shared, "[credential \"https://example.invalid\"]\n\thelper = shared\n")
	system := authFile(t, "system", "[credential]\n\thelper = system-before\n[include]\n\tpath = ~/shared\n[credential]\n\thelper = system-after\n", 0o600)
	t.Setenv("GIT_CONFIG_SYSTEM", system)
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	xdg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git", "config")
	writeGitAuthConfig(t, xdg, fmt.Sprintf("[credential]\n\thelper = xdg\n[include]\n\tpath = %q\n", shared))
	global := filepath.Join(home, ".gitconfig")
	target := authFile(t, "symlink-target", "[include]\n\tpath = authored-child\n[credential]\n\thelper = global-after\n", 0o600)
	writeGitAuthConfig(t, filepath.Join(home, "authored-child"), "[credential]\n\thelper =\n\thelper = global-child\n[include]\n\tpath = missing-optional\n\tpath = /dev/null\n\tpath = shared\n")
	// A symlinked config must resolve relative includes beside the authored
	// symlink, not beside its physical target.
	if err := os.Symlink(target, global); err != nil {
		t.Fatal(err)
	}
	env, _, err := gitAuthEnvironment(context.Background(), "", gitEnv(), "https://example.invalid/fixture")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.HasPrefix(key, "GIT_CONFIG_KEY_") {
			index, err := strconv.Atoi(strings.TrimPrefix(key, "GIT_CONFIG_KEY_"))
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range env {
				if v, ok := strings.CutPrefix(candidate, fmt.Sprintf("GIT_CONFIG_VALUE_%d=", index)); ok {
					got = append(got, value+"="+v)
				}
			}
		}
	}
	want := []string{
		"credential.helper=system-before",
		"credential.https://example.invalid.helper=shared",
		"credential.helper=system-after",
		"credential.helper=xdg",
		"credential.https://example.invalid.helper=shared",
		"credential.helper=",
		"credential.helper=global-child",
		"credential.https://example.invalid.helper=shared",
		"credential.helper=global-after",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("authentication order = %q, want %q", got, want)
	}
}

// spec: git-authentication/include-paths-resolve-as-git-resolves-them
func TestGitAuthIncludePathsResolveLikeGit(t *testing.T) {
	isolatedGitAuth(t)
	home := os.Getenv("HOME")
	writeGitAuthConfig(t, filepath.Join(home, "real", "etc", "gitconfig"), fmt.Sprintf("[include]\n\tpath = ../share/relative\n\tpath = ~/etc/../share/home\n\tpath = %%(prefix)/%s\n", filepath.Join(home, "prefixed")))
	for _, name := range []string{"relative", "home"} {
		writeGitAuthConfig(t, filepath.Join(home, "real", "share", name), "[credential]\n\thelper = physical-"+name+"\n")
		writeGitAuthConfig(t, filepath.Join(home, "share", name), "[credential]\n\thelper = lexical-"+name+"\n")
	}
	writeGitAuthConfig(t, filepath.Join(home, "prefixed"), "[credential]\n\thelper = prefixed\n")
	// Git joins include paths without lexical cleaning, so ".." leaves the
	// symlinked directory through its physical parent.
	if err := os.Symlink(filepath.Join(home, "real", "etc"), filepath.Join(home, "etc")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "etc", "gitconfig"))
	env, _, err := gitAuthEnvironment(context.Background(), "", gitEnv(), "https://example.invalid/fixture")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "GIT_CONFIG_VALUE_"); ok {
			_, value, _ = strings.Cut(value, "=")
			got = append(got, value)
		}
	}
	native, err := exec.Command("git", "config", "--global", "--includes", "--get-all", "credential.helper").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"physical-relative", "physical-home", "prefixed"}
	if !reflect.DeepEqual(strings.Fields(string(native)), want) || !reflect.DeepEqual(got, want) {
		t.Fatalf("helpers = %q, Git = %q, want %q", got, strings.Fields(string(native)), want)
	}
}

// spec: git-authentication/unsafe-include-chains-fail-before-transport
// spec: git-authentication/conditional-includes-remain-explicitly-refused
func TestGitAuthUnsafeIncludeChains(t *testing.T) {
	for _, kind := range []string{"group-writable", "other-writable", "malformed", "cycle", "symlink-cycle", "fifo", "directory", "hardlink", "oversized", "expanded-size", "parser-output", "depth", "visits", "empty-path", "valueless-path", "conditional"} {
		t.Run(kind, func(t *testing.T) {
			isolatedGitAuth(t)
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)
			marker := filepath.Join(t.TempDir(), "helper")
			t.Setenv("AUTH_MARKER", marker)
			dir := t.TempDir()
			root := filepath.Join(dir, "root")
			child := filepath.Join(dir, "child")
			rootData := "[credential]\n\thelper = \"!echo synthetic-auth-secret >> \\\"$AUTH_MARKER\\\"\"\n[include]\n\tpath = child\n"
			childData := "[credential]\n\tusername = synthetic-auth-secret\n"
			switch kind {
			case "malformed":
				childData = "[synthetic-auth-secret\n"
			case "cycle":
				childData = "[include]\n\tpath = root\n"
			case "symlink-cycle":
				childData = "[include]\n\tpath = alias\n"
				if err := os.Symlink(root, filepath.Join(dir, "alias")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				childData = "#" + strings.Repeat("x", 4<<20)
			case "expanded-size":
				childData = "#" + strings.Repeat("x", (4<<20)-2) + "\n"
				rootData += strings.Repeat("\tpath = child\n", 4)
			case "parser-output":
				// Native normalized output repeats the section on each record.
				// Small file input must not allow unbounded subprocess output.
				childData = "[" + strings.Repeat("x", 1000) + "]\n" + strings.Repeat("a=\n", 20000)
			case "depth":
				for i := range 11 {
					next := fmt.Sprintf("depth-%d", i+1)
					writeGitAuthConfig(t, filepath.Join(dir, fmt.Sprintf("depth-%d", i)), "[include]\n\tpath = "+next+"\n")
				}
				childData = "[include]\n\tpath = depth-0\n"
			case "visits":
				rootData += strings.Repeat("\tpath = child\n", 128)
			case "empty-path":
				rootData = "[include]\n\tpath =\n"
			case "valueless-path":
				rootData = "[include]\n\tpath\n"
			case "conditional":
				childData = "[includeIf \"gitdir:/never-matches/\"]\n\tpath = synthetic-auth-secret\n"
			}
			writeGitAuthConfig(t, root, rootData)
			switch kind {
			case "fifo":
				if err := syscall.Mkfifo(child, 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(child, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				writeGitAuthConfig(t, child, childData)
			}
			switch kind {
			case "group-writable", "other-writable":
				mode := os.FileMode(0o620)
				if kind == "other-writable" {
					mode = 0o602
				}
				if err := os.Chmod(child, mode); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(child, filepath.Join(dir, "alias")); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GIT_CONFIG_GLOBAL", root)
			timeout := 5 * time.Second
			if kind == "visits" {
				// The budget case deliberately starts 128 native parsers; race
				// instrumentation and concurrent gates need more than the FIFO bound.
				timeout = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			_, err := execGitContext(ctx, "", []string{"clone", "--", server.URL + "/fixture", filepath.Join(t.TempDir(), "clone")})
			if !errors.Is(err, ErrGitAuthConfigUntrusted) {
				t.Fatalf("unsafe %s chain error = %v, want authentication trust rejection", kind, err)
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatal("unsafe include blocked instead of rejecting")
			}
			if requests.Load() != 0 {
				t.Fatal("unsafe include chain reached the transport")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("credential helper executed before chain validation")
			}
			if strings.Contains(err.Error(), "synthetic-auth-secret") {
				t.Fatal("trust error exposed configuration data")
			}
			if data, err := os.ReadFile(root); err != nil || string(data) != rootData {
				t.Fatal("refusal changed the root configuration")
			}
		})
	}
}

func TestGitAuthIncludesKeepTLSVerification(t *testing.T) {
	isolatedGitAuth(t)
	f := newGitAuthServer(t)
	complete := `!f() { test "$1" != get || printf 'username=fixture-user\npassword=synthetic-auth-secret\n'; }; f`
	child := authFile(t, "child", fmt.Sprintf("[credential]\n\thelper = %q\n[http]\n\tsslVerify = false\n", complete), 0o600)
	root := authFile(t, "root", fmt.Sprintf("[include]\n\tpath = %q\n", child), 0o600)
	t.Setenv("GIT_CONFIG_GLOBAL", root)
	canonical := "https://example.invalid/included-tls"
	if _, err := ResolveExactGitRef(canonical, f.url, "main"); !isGitTransportFailure(err) {
		t.Fatalf("untrusted certificate error = %v, want transport rejection after valid include loading", err)
	}
	if f.accepted.Load() != 0 {
		t.Fatal("included sslVerify=false disabled certificate verification")
	}
	t.Setenv("GIT_SSL_CAINFO", f.ca)
	if _, err := ResolveExactGitRef(canonical, f.url, "main"); err != nil {
		t.Fatalf("valid CA control failed: %v", err)
	}
	if f.accepted.Load() == 0 {
		t.Fatal("valid CA control did not reach authenticated transport")
	}
}

func TestGitAuthIncludeParserUsesValidatedBytes(t *testing.T) {
	isolatedGitAuth(t)
	root := authFile(t, "root", "[credential]\n\tusername = validated\n", 0o600)
	t.Setenv("GIT_CONFIG_GLOBAL", root)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_REAL_GIT", realGit)
	t.Setenv("AUTH_CONFIG_PATH", root)
	wrapper := authFile(t, "git", "#!/bin/sh\nif [ \"$1\" = config ] && [ \"$2\" = --file ]; then\n  printf '[credential]\\n username = replacement\\n' > \"$AUTH_CONFIG_PATH\"\nfi\nexec \"$AUTH_REAL_GIT\" \"$@\"\n", 0o700)
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	env, _, err := gitAuthEnvironment(t.Context(), "", gitEnv(), "https://example.invalid/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(env, "\n"), "GIT_CONFIG_VALUE_0=validated\n") {
		t.Fatal("native parser reopened changed configuration instead of parsing the validated bytes")
	}
	if got, err := os.ReadFile(root); err != nil || !strings.Contains(string(got), "replacement") {
		t.Fatal("replacement control did not change the configuration at the parser boundary")
	}
}

func TestGitAuthDiscoveredSystemInclude(t *testing.T) {
	isolatedGitAuth(t)
	child := authFile(t, "child", "[credential]\n\tusername = discovered\n", 0o600)
	root := authFile(t, "system", fmt.Sprintf("[include]\n\tpath = %q\n", child), 0o600)
	if err := os.Unsetenv("GIT_CONFIG_SYSTEM"); err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUTH_REAL_GIT", realGit)
	t.Setenv("AUTH_SYSTEM_ROOT", root)
	wrapper := authFile(t, "git", "#!/bin/sh\nif [ \"$1\" = config ] && [ \"$2\" = --system ]; then\n  printf 'file:%s\\000include.path\\000' \"$AUTH_SYSTEM_ROOT\"\n  exit 0\nfi\nexec \"$AUTH_REAL_GIT\" \"$@\"\n", 0o700)
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))
	env, _, err := gitAuthEnvironment(t.Context(), "", gitEnv(), "https://example.invalid/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(env, "\n"), "GIT_CONFIG_VALUE_0=discovered\n") {
		t.Fatal("default system-path discovery did not load its included authentication")
	}
}

func TestGitAuthIncludeDepthBoundary(t *testing.T) {
	isolatedGitAuth(t)
	dir := t.TempDir()
	for i := range 10 {
		writeGitAuthConfig(t, filepath.Join(dir, fmt.Sprintf("config-%d", i)), fmt.Sprintf("[include]\n\tpath = config-%d\n", i+1))
	}
	writeGitAuthConfig(t, filepath.Join(dir, "config-10"), "[credential]\n\tuseHttpPath\n[http]\n\tsslCertPasswordProtected\n")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "config-0"))
	env, _, err := gitAuthEnvironment(t.Context(), "", gitEnv(), "https://example.invalid/fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"GIT_CONFIG_VALUE_0=true\n", "GIT_CONFIG_VALUE_1=true\n"} {
		if !strings.Contains(strings.Join(env, "\n"), setting) {
			t.Fatalf("ten-level chain lost the valueless boolean %q", setting)
		}
	}
}
