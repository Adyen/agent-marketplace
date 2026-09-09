package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResolveLatestStable(t *testing.T) {
	for target, npm := range map[string]string{"claude": "@anthropic-ai/claude-code", "factory": "droid", "gemini": "@google/gemini-cli", "codex": "@openai/codex"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			got, err := resolveVersion(target, func(endpoint string) ([]byte, error) {
				calls++
				if endpoint != "https://registry.npmjs.org/"+url.PathEscape(npm)+"/latest" {
					t.Fatalf("not using the public latest dist-tag endpoint: %s", endpoint)
				}
				return []byte(`{"name":"` + npm + `","version":"9.8.7","time":"published one second ago"}`), nil
			})
			if err != nil || calls != 1 || got.Version != "9.8.7" || got.NPMPackage != npm || got.Target != target || got.BundleURL != "" {
				t.Fatalf("new stable release not selected immediately: %+v (%v)", got, err)
			}
		})
	}
	for _, version := range []string{"0.0.0", "3000.3.27", "1.2.3+build.42"} {
		if !stableVersion(version) {
			t.Errorf("valid stable version rejected: %s", version)
		}
	}
	for _, version := range []string{"", "latest", "v1.2.3", "01.2.3", "1.2", "1.2.3-beta.1", "1.2.3; touch /tmp/x", "1.2.3\n", "1.2.3+"} {
		if stableVersion(version) {
			t.Errorf("unsafe or unstable version accepted: %q", version)
		}
	}
}

func TestResolveMetadataFailsClosed(t *testing.T) {
	for _, body := range []string{`{`, `{}`, `{"name":"wrong-package","version":"1.2.3"}`, `{"name":"droid","version":"1.2.3-preview"}`, `{"name":"droid","version":42}`} {
		if _, err := resolveVersion("factory", func(string) ([]byte, error) { return []byte(body), nil }); err == nil {
			t.Errorf("invalid metadata accepted: %s", body)
		}
	}
	if _, err := resolveVersion("factory", func(string) ([]byte, error) { return nil, errors.New("private response must not leak") }); err == nil || strings.Contains(err.Error(), "private response") {
		t.Fatalf("fetch failure was hidden or leaked unsafe details: %v", err)
	}
	for _, target := range []string{"cursor", "windsurf", "unknown"} {
		if _, err := resolveVersion(target, func(string) ([]byte, error) { t.Fatal("unexpected fetch"); return nil, nil }); err == nil {
			t.Errorf("non-native target accepted: %s", target)
		}
	}
}

func TestResolveDevinTrustedBundle(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, tc := range []struct {
		url, sha string
		valid    bool
	}{
		{"https://static.devin.ai/cli/3000.3.27/linux-x86_64.tar.gz", sha, true},
		{"http://static.devin.ai/cli/bundle.tar.gz", sha, false},
		{"https://static.devin.ai.evil.test/cli/bundle.tar.gz", sha, false},
		{(&url.URL{Scheme: "https", Host: "static.devin.ai", Path: "/cli/bundle.tar.gz", User: url.UserPassword("fixture-user", "fixture-secret")}).String(), sha, false},
		{"https://static.devin.ai/cli/bundle.tar.gz?token=secret", sha, false},
		{"https://static.devin.ai/cli/bundle.tar.gz#secret", sha, false},
		{"https://static.devin.ai/other/bundle.tar.gz", sha, false},
		{"https://static.devin.ai/cli/../other/bundle.tar.gz", sha, false},
		{"https://static.devin.ai/cli/%2e%2e/other", sha, false},
		{"https://static.devin.ai/cli/bundle;evil", sha, false},
		{"https://static.devin.ai/cli/bundle.tar.gz", "not-a-hash", false},
	} {
		body := `{"version":"3000.3.27","platforms":{"x86_64-unknown-linux":{"url":"` + tc.url + `","sha256":"` + tc.sha + `"}}}`
		got, err := resolveVersion("devin", func(endpoint string) ([]byte, error) {
			if endpoint != "https://static.devin.ai/cli/current/manifest.json" {
				t.Fatal(endpoint)
			}
			return []byte(body), nil
		})
		if (err == nil) != tc.valid {
			t.Errorf("bundle validation = %v for %s", err, tc.url)
		}
		if tc.valid && (got.Version != "3000.3.27" || got.CLI != "devin" || got.BundleSHA256 != sha || got.BundleURL != tc.url) {
			t.Fatalf("wrong release: %+v", got)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe metadata leaked into error")
		}
	}
}

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMetadataHTTPBoundaries(t *testing.T) {
	client := metadataClient()
	if client.Timeout <= 0 {
		t.Fatal("metadata requests have no timeout")
	}
	endpoint := "https://registry.npmjs.org/droid/latest"
	for _, tc := range []struct {
		status int
		body   string
		valid  bool
	}{
		{200, `{}`, true}, {401, "do-not-print", false}, {503, "do-not-print", false}, {200, strings.Repeat("x", maxMetadataBytes+1), false},
	} {
		client.Transport = metadataTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
		})
		_, err := fetchMetadata(client, endpoint)
		if (err == nil) != tc.valid || err != nil && strings.Contains(err.Error(), "do-not-print") {
			t.Errorf("bad HTTP boundary decision: %v", err)
		}
	}
	client.Transport = metadataTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("untrusted endpoint reached HTTP transport")
		return nil, errors.New("unreachable")
	})
	for _, address := range []string{"http://registry.npmjs.org/droid/latest", "https://registry.npmjs.org/droid/latest?token=secret", (&url.URL{Scheme: "https", Host: "registry.npmjs.org", Path: "/droid/latest", User: url.UserPassword("fixture-user", "fixture-secret")}).String(), "https://registry.npmjs.org/unapproved/latest", "https://static.devin.ai/cli/other.json"} {
		if _, err := fetchMetadata(client, address); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("untrusted metadata URL accepted or leaked: %v", err)
		}
	}
	original, _ := http.NewRequest("GET", endpoint, nil)
	for _, address := range []string{"https://evil.test/droid/latest", "http://registry.npmjs.org/droid/latest", "https://registry.npmjs.org/droid/latest?token=secret", "https://static.devin.ai/cli/current/manifest.json"} {
		next, _ := http.NewRequest("GET", address, nil)
		if err := client.CheckRedirect(next, []*http.Request{original}); err == nil {
			t.Errorf("unsafe redirect accepted: %s", address)
		}
	}
	if err := client.CheckRedirect(original, []*http.Request{original}); err != nil {
		t.Fatal("same-origin metadata redirect rejected")
	}
}
