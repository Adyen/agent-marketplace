package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/config"
)

func TestWebURLHTTPSFallback(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/anthropics/skills", "https://github.com/anthropics/skills.git"},
		{"https://github.com/org/repo/tree/main/skills/x", "https://github.com/org/repo.git"},
		{"https://gitlab.example.com/group/sub/project/-/tree/main/skills", "https://gitlab.example.com/group/sub/project.git"},
		{"http://git.internal/org/repo", "https://git.internal/org/repo.git"},
		{"git@github.com:org/repo.git", ""},
		{"https://github.com/org/repo.git", ""},
		{"./local/registry", ""},
		{"github.com/org/repo", ""},
		{"https://git.example.com:8443/org/repo", "https://git.example.com:8443/org/repo.git"},
	}
	for _, tt := range tests {
		if got := WebURLHTTPSFallback(tt.input); got != tt.want {
			t.Errorf("WebURLHTTPSFallback(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

const probedCommit = "0123456789abcdef0123456789abcdef01234567"

// stubRemoteProbe makes every URL in reachable advertise the branch "main" at
// probedCommit and every other URL fail; it returns the probed URLs.
func stubRemoteProbe(t *testing.T, reachable map[string]bool) *[]string {
	t.Helper()
	var probed []string
	orig := remoteProbe
	remoteProbe = func(_ context.Context, gitURL string) (string, string, error) {
		probed = append(probed, gitURL)
		if reachable[gitURL] {
			return "main", probedCommit, nil
		}
		return "", "", errors.New("unreachable")
	}
	t.Cleanup(func() { remoteProbe = orig })
	return &probed
}

func allowAll(string) error { return nil }

func TestRegistryURLPolicy(t *testing.T) {
	policy := RegistryURLPolicy(config.RegistryEntry{Ref: "main", Path: "skills"}, []string{`^github\.com/allowed-org/`})
	if err := policy("https://github.com/allowed-org/repo.git"); err != nil {
		t.Fatalf("allowed URL refused: %v", err)
	}
	if err := policy("https://github.com/other-org/repo.git"); err == nil {
		t.Fatal("blocked URL accepted")
	}
}

// spec: resource-installation/unreachable-ssh-web-url-uses-https
// spec: resource-installation/reachable-ssh-web-url-keeps-ssh
// spec: resource-installation/unreachable-web-url-keeps-the-ssh-diagnostic
// spec: resource-installation/non-web-sources-are-not-probed
// spec: resource-installation/blocked-https-fallback-is-not-probed
func TestPreferReachableTransport(t *testing.T) {
	const raw = "https://github.com/org/repo"
	const ssh = "git@github.com:org/repo"
	const https = "https://github.com/org/repo.git"

	t.Run("ssh reachable stays ssh and reuses its branch and commit", func(t *testing.T) {
		probed := stubRemoteProbe(t, map[string]bool{ssh: true, https: true})
		want := TransportChoice{URL: ssh, DefaultBranch: "main", Commit: probedCommit}
		if got := PreferReachableTransport(context.Background(), raw, ssh, allowAll); got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		if len(*probed) != 1 {
			t.Fatalf("probed %v, want only the SSH URL", *probed)
		}
	})
	t.Run("ssh unreachable falls back to https with a notice", func(t *testing.T) {
		stubRemoteProbe(t, map[string]bool{https: true})
		got := PreferReachableTransport(context.Background(), raw, ssh, allowAll)
		want := TransportChoice{URL: https, DefaultBranch: "main", Commit: probedCommit, FellBackFrom: ssh}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		if notice, want := got.Notice("recorded"), "SSH could not reach "+ssh+"; recorded "+https+" instead"; notice != want {
			t.Fatalf("notice %q, want %q", notice, want)
		}
	})
	t.Run("both unreachable keeps ssh for its diagnostics", func(t *testing.T) {
		stubRemoteProbe(t, nil)
		got := PreferReachableTransport(context.Background(), raw, ssh, allowAll)
		if got != (TransportChoice{URL: ssh}) || got.Notice("recorded") != "" {
			t.Fatalf("got %+v, want SSH without branch or notice", got)
		}
	})
	t.Run("fallback refused by policy is never probed", func(t *testing.T) {
		probed := stubRemoteProbe(t, map[string]bool{https: true})
		refuse := func(string) error { return errors.New("blocked") }
		if got := PreferReachableTransport(context.Background(), raw, ssh, refuse); got != (TransportChoice{URL: ssh}) {
			t.Fatalf("got %+v, want %q", got, ssh)
		}
		if len(*probed) != 0 {
			t.Fatalf("probed %v, want none", *probed)
		}
	})
	t.Run("non-web URL is never probed", func(t *testing.T) {
		probed := stubRemoteProbe(t, nil)
		for _, u := range []string{ssh, https, "./local"} {
			if got := PreferReachableTransport(context.Background(), u, u, allowAll); got != (TransportChoice{URL: u}) {
				t.Fatalf("got %+v, want %q", got, u)
			}
		}
		if len(*probed) != 0 {
			t.Fatalf("probed %v, want none", *probed)
		}
	})
}

// spec: resource-installation/configured-form-of-a-web-url-is-reused
func TestSelectAdHocTransport(t *testing.T) {
	const raw = "https://github.com/org/repo"
	const ssh = "git@github.com:org/repo"
	const https = "https://github.com/org/repo.git"
	merged := func(url string) *config.Config {
		return &config.Config{Registries: []config.RegistryEntry{{URL: url, Path: "skills", Source: config.RepoCommitted}}}
	}

	t.Run("configured https is reused even when ssh works", func(t *testing.T) {
		probed := stubRemoteProbe(t, map[string]bool{ssh: true, https: true})
		got := SelectAdHocTransport(context.Background(), merged(https), config.RepoCommitted, raw, ssh, "skills", "", allowAll)
		if got != (TransportChoice{URL: https}) {
			t.Fatalf("got %+v, want the configured HTTPS URL", got)
		}
		if len(*probed) != 0 {
			t.Fatalf("probed %v, want none", *probed)
		}
	})
	t.Run("configured ssh is reused even when ssh fails", func(t *testing.T) {
		probed := stubRemoteProbe(t, map[string]bool{https: true})
		got := SelectAdHocTransport(context.Background(), merged(ssh), config.RepoCommitted, raw, ssh, "skills", "", allowAll)
		if got != (TransportChoice{URL: ssh}) || len(*probed) != 0 {
			t.Fatalf("got %+v after probing %v, want the configured SSH URL without probes", got, *probed)
		}
	})
	t.Run("a different path is not a configured form", func(t *testing.T) {
		stubRemoteProbe(t, map[string]bool{https: true})
		got := SelectAdHocTransport(context.Background(), merged(ssh), config.RepoCommitted, raw, ssh, "other", "", allowAll)
		if got.URL != https || got.FellBackFrom != ssh {
			t.Fatalf("got %+v, want the probed HTTPS fallback", got)
		}
	})
	t.Run("a configured form refused by policy is ignored", func(t *testing.T) {
		// The refused HTTPS form is neither reused nor probed as a fallback.
		probed := stubRemoteProbe(t, map[string]bool{ssh: true})
		refuseHTTPS := func(u string) error {
			if u == https {
				return errors.New("blocked")
			}
			return nil
		}
		got := SelectAdHocTransport(context.Background(), merged(https), config.RepoCommitted, raw, ssh, "skills", "", refuseHTTPS)
		if got != (TransportChoice{URL: ssh}) || len(*probed) != 0 {
			t.Fatalf("got %+v after probing %v, want SSH without probes", got, *probed)
		}
	})
}

// spec: resource-removal/web-url-removes-a-source-stored-in-https-form
// spec: resource-removal/web-url-prefers-a-source-stored-in-ssh-form
// spec: resource-removal/web-url-removal-follows-the-entry-matching-the-request
func TestConfiguredWebURLForm(t *testing.T) {
	const raw = "https://github.com/org/repo"
	const ssh = "git@github.com:org/repo"
	const https = "https://github.com/org/repo.git"
	type entry struct {
		url, path, ref string
		source         config.Source
	}
	cfg := func(entries ...entry) *config.Config {
		c := &config.Config{}
		for _, e := range entries {
			c.Registries = append(c.Registries, config.RegistryEntry{URL: e.url, Path: e.path, Ref: e.ref, Source: e.source})
		}
		return c
	}
	tests := []struct {
		name      string
		target    *config.Config
		merged    *config.Config
		path, ref string
		want      string
	}{
		{name: "only https configured", merged: cfg(entry{url: "https://github.com/org/repo"}), want: https},
		{name: "ssh configured", merged: cfg(entry{url: ssh}), want: ssh},
		{name: "both configured prefers ssh", merged: cfg(entry{url: ssh}, entry{url: https}), want: ssh},
		{name: "neither configured", merged: cfg(entry{url: "https://github.com/other/repo"}), want: ssh},
		{name: "nil configs", want: ssh},
		{
			name:   "target scope https wins over ssh in another scope",
			target: cfg(entry{url: https}),
			merged: cfg(entry{url: ssh}, entry{url: https}),
			want:   https,
		},
		{
			name:   "target scope ssh wins over https in another scope",
			target: cfg(entry{url: ssh}),
			merged: cfg(entry{url: ssh}, entry{url: https}),
			want:   ssh,
		},
		{
			// The repo-scope HTTPS entry lives in an included file, so the
			// target root file has no match; it still beats user-scope SSH.
			name:   "included target-scope https wins over ssh in another scope",
			target: cfg(),
			merged: cfg(entry{url: ssh, source: config.UserPersonal}, entry{url: https, source: config.RepoCommitted}),
			want:   https,
		},
		{
			name:   "target-scope ssh wins over target-scope https",
			merged: cfg(entry{url: https, source: config.RepoCommitted}, entry{url: ssh, source: config.RepoCommitted}),
			want:   ssh,
		},
		{
			name:   "only the https entry matches the requested ref",
			merged: cfg(entry{url: ssh, ref: "dev"}, entry{url: https, ref: "main"}),
			ref:    "main",
			want:   https,
		},
		{
			name:   "only the https entry matches the requested path",
			merged: cfg(entry{url: ssh, path: "other"}, entry{url: https, path: "skills"}),
			path:   "skills",
			want:   https,
		},
	}
	for _, tt := range tests {
		if got := configuredWebURLForm(tt.target, tt.merged, config.RepoCommitted, raw, ssh, tt.path, tt.ref); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
