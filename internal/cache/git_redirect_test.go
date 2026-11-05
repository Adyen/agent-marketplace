package cache

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	redirectFixtureUserinfo = "fixture-user:synthetic-auth-secret@"
	redirectFixtureHeader   = "Bearer synthetic-scoped-token"
)

// redirectFixture serves one repository from an origin that requires
// credentials and then redirects every request to a second origin.
type redirectFixture struct {
	repo, origin   string
	targetRequests atomic.Int64
}

func newRedirectFixture(t *testing.T) *redirectFixture {
	t.Helper()
	f := &redirectFixture{repo: newExactGitRepository(t, "sha1")}
	gitTest(t, f.repo, "symbolic-ref", "HEAD", "refs/heads/main")
	gitTest(t, f.repo, "update-server-info")
	files := http.FileServer(http.Dir(filepath.Dir(f.repo)))
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.targetRequests.Add(1)
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, basic := r.BasicAuth()
		authorized := basic && user == "fixture-user" && password == "synthetic-auth-secret" || r.Header.Get("Authorization") == redirectFixtureHeader
		if !authorized {
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, target.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	origin.Config.ErrorLog = log.New(io.Discard, "", 0)
	origin.Start()
	t.Cleanup(origin.Close)
	f.origin = origin.URL
	return f
}

// spec: git-authentication/acquisition-follows-the-default-git-redirect-behavior
// spec: git-authentication/trusted-redirect-policy-applies-to-acquisition
func TestGitAcquisitionFollowsGitRedirectPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, credential, policy string
		follow                   bool
	}{
		{name: "default with userinfo", credential: "userinfo", follow: true},
		{name: "default with scoped header", credential: "header", follow: true},
		{name: "bare true policy", credential: "userinfo", policy: "[http]\n\tfollowRedirects\n", follow: true},
		{name: "global false policy", credential: "userinfo", policy: "[http]\n\tfollowRedirects = false\n"},
		{name: "origin false policy", credential: "header", policy: "[http %q]\n\tfollowRedirects = false\n"},
		{name: "other host false policy", credential: "header", policy: "[http \"http://other.invalid/\"]\n\tfollowRedirects = false\n", follow: true},
	} {
		for _, consumer := range []struct {
			name    string
			acquire func(source string) error
		}{
			{"exact", func(source string) error {
				_, err := ResolveExactGitRef("https://example.invalid/redirect-fixture", source, "main")
				return err
			}},
			{"mutable", func(source string) error {
				_, err := CloneOrPull(source, "main")
				return err
			}},
		} {
			t.Run(tc.name+"/"+consumer.name, func(t *testing.T) {
				isolatedGitAuth(t)
				f := newRedirectFixture(t)
				source := strings.Replace(f.origin, "http://", "http://"+redirectFixtureUserinfo, 1) + "/" + filepath.Base(f.repo)
				config := strings.ReplaceAll(tc.policy, "%q", fmt.Sprintf("%q", f.origin+"/"))
				if tc.credential == "header" {
					source = f.origin + "/" + filepath.Base(f.repo)
					config += fmt.Sprintf("[http %q]\n\textraHeader = Authorization: %s\n", f.origin+"/", redirectFixtureHeader)
				}
				if config != "" {
					t.Setenv("GIT_CONFIG_GLOBAL", authFile(t, "global", config, 0o600))
				}
				err := consumer.acquire(source)
				if tc.follow {
					if err != nil || f.targetRequests.Load() == 0 {
						t.Fatalf("redirect not followed: err=%v target requests=%d", err, f.targetRequests.Load())
					}
					return
				}
				if !isGitTransportFailure(err) || errors.Is(err, ErrGitAuthConfigUntrusted) || !strings.Contains(err.Error(), "git.auth-transport-failed: transport diagnostics withheld") {
					t.Fatalf("redirect policy did not fail the transport: %v", err)
				}
				if strings.Contains(err.Error(), "synthetic-") {
					t.Fatalf("diagnostic leaked a credential: %v", err)
				}
				if got := f.targetRequests.Load(); got != 0 {
					t.Fatalf("redirect target received %d requests", got)
				}
			})
		}
	}
}
