package lockfile

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/testutil"
)

const sha1a = "sha1:0123456789abcdef0123456789abcdef01234567"
const sha256a = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestParseGitOID(t *testing.T) {
	for _, tc := range []struct {
		in, algorithm, hex string
		valid              bool
	}{
		{sha1a, "sha1", strings.TrimPrefix(sha1a, "sha1:"), true},
		{"sha256:" + strings.Repeat("b", 64), "sha256", strings.Repeat("b", 64), true},
		{"sha1:" + strings.Repeat("A", 40), "", "", false},
		{"sha512:" + strings.Repeat("a", 128), "", "", false},
		{"sha1:abc", "", "", false},
	} {
		oid, err := ParseGitOID(tc.in)
		if tc.valid != (err == nil) {
			t.Fatalf("ParseGitOID(%q) error = %v", tc.in, err)
		}
		if tc.valid && (oid.Algorithm() != tc.algorithm || oid.Hex() != tc.hex || oid.String() != tc.in) {
			t.Fatalf("ParseGitOID(%q) = %#v", tc.in, oid)
		}
	}
}

func TestCanonicalGitURLSharedFixtures(t *testing.T) {
	for _, tc := range testutil.CanonicalGitURLFixtures {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := CanonicalGitURL(tc.Raw)
			if tc.Canonical == "" {
				var codedErr *Error
				if err == nil || !errors.As(err, &codedErr) || codedErr.Code != CodeInvalidGitURL {
					t.Fatalf("CanonicalGitURL(%q) = %q, %v; want coded rejection", tc.Raw, got, err)
				}
				return
			}
			if err != nil || got != tc.Canonical {
				t.Fatalf("CanonicalGitURL(%q) = %q, %v; want %q", tc.Raw, got, err, tc.Canonical)
			}
		})
	}
}

// spec: acquisition-lock-codec/registry-inputs-retain-the-preceding-git-suffix
func TestRegistryRepeatedGitSuffix(t *testing.T) {
	const raw = "https://git.example/team/tools.git.git"
	const want = "https://git.example/team/tools.git"
	t.Run("configured registry", func(t *testing.T) {
		if got, err := CanonicalGitConfigURL(raw); err != nil || got != want {
			t.Fatalf("CanonicalGitConfigURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	})
	for _, version := range []string{APIVersionV1, APIVersionV2} {
		t.Run(version, func(t *testing.T) {
			lock := "apiVersion: " + version + "\nscope: repo\nregistries:\n  - url: " + raw + "\n    ref: main\n    commit: " + sha1a + "\n"
			doc, err := Decode([]byte(lock))
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Registries) != 1 || doc.Registries[0].URL != want {
				t.Fatalf("decoded registries = %#v; want one registry at %q", doc.Registries, want)
			}
		})
	}
}

// spec: acquisition-lock-codec/repeated-suffix-identities-round-trip-through-v1-and-v2
func TestRepeatedSuffixGitIdentitiesRoundTrip(t *testing.T) {
	for _, version := range []string{APIVersionV1, APIVersionV2} {
		t.Run(version, func(t *testing.T) {
			const base = "https://git.example/team/tools"
			wireURLs := []string{base + ".git", base + ".git.git", base + ".git.git.git"}
			encodedURLs := []string{base, base + ".git.git", base + ".git.git.git"}
			wantURLs := []string{base, base + ".git", base + ".git.git"}
			var input strings.Builder
			fmt.Fprintf(&input, "apiVersion: %s\nscope: repo\nregistries:\n", version)
			for _, raw := range wireURLs {
				fmt.Fprintf(&input, "  - url: %s\n    ref: main\n    commit: %s\n", raw, sha1a)
			}

			doc, err := Decode([]byte(input.String()))
			if err != nil {
				t.Fatal(err)
			}
			assertRegistryURLs := func(doc Document) {
				t.Helper()
				if len(doc.Registries) != len(wantURLs) {
					t.Fatalf("registry count = %d, want %d: %#v", len(doc.Registries), len(wantURLs), doc.Registries)
				}
				for i, want := range wantURLs {
					if got := doc.Registries[i].URL; got != want {
						t.Fatalf("registry %d identity = %q, want %q", i, got, want)
					}
				}
			}
			assertRegistryURLs(doc)

			var canonicalBytes []byte
			for pass := 0; pass < 3; pass++ {
				normalized, err := Normalize(doc)
				if err != nil {
					t.Fatalf("normalize pass %d: %v", pass, err)
				}
				assertRegistryURLs(normalized)
				before := cloneDocument(doc)
				encoded, err := Encode(doc)
				if err != nil {
					t.Fatalf("encode pass %d: %v", pass, err)
				}
				if !reflect.DeepEqual(doc, before) {
					t.Fatalf("encode pass %d mutated its input", pass)
				}
				for _, want := range encodedURLs {
					if !strings.Contains(string(encoded), "url: "+want+"\n") {
						t.Fatalf("encode pass %d lost wire URL %q:\n%s", pass, want, encoded)
					}
				}
				if canonicalBytes == nil {
					canonicalBytes = encoded
				} else if !reflect.DeepEqual(encoded, canonicalBytes) {
					t.Fatalf("encode pass %d changed canonical bytes:\nbefore:\n%s\nafter:\n%s", pass, canonicalBytes, encoded)
				}
				decoded, err := Decode(encoded)
				if err != nil {
					t.Fatalf("decode pass %d: %v", pass, err)
				}
				if !reflect.DeepEqual(decoded, normalized) {
					t.Fatalf("decode pass %d = %#v, want normalized %#v", pass, decoded, normalized)
				}
				doc = decoded
			}
		})
	}
}

// spec: acquisition-lock-codec/repeated-suffix-identities-round-trip-through-v1-and-v2
func TestNewGitRegistryPreservesCanonicalSuffixIdentity(t *testing.T) {
	const canonical = "https://git.example/team/tools.git"
	registry := NewGitRegistry(canonical, "main", mustOIDForGolden(sha1a))
	if got, want := registry.FetchURL(), canonical+".git"; got != want {
		t.Fatalf("fetch URL = %q, want %q", got, want)
	}
	encoded, err := Encode(Document{APIVersion: APIVersionV2, Scope: ScopeRepo, Registries: []GitRegistry{registry}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "url: "+canonical+".git\n") {
		t.Fatalf("encoded lock lost canonical suffix identity:\n%s", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Registries) != 1 || decoded.Registries[0].URL != canonical {
		t.Fatalf("decoded registry = %#v, want canonical URL %q", decoded.Registries, canonical)
	}
}

// spec: acquisition-lock-codec/a-changed-normalized-url-is-validated-independently
func TestChangedNormalizedGitURLIsRevalidated(t *testing.T) {
	const raw = "https://git.example/team/tools.git.git"
	doc, err := Decode([]byte("apiVersion: " + APIVersionV2 + "\nscope: repo\nregistries:\n  - url: " + raw + "\n    ref: main\n    commit: " + sha1a + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc.Registries[0].URL, "https://git.example/team/tools.git"; got != want {
		t.Fatalf("decoded URL = %q, want %q", got, want)
	}

	doc.Registries[0].URL = "https://git.example/team/other.git"
	normalized, err := Normalize(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := normalized.Registries[0].URL, "https://git.example/team/other"; got != want {
		t.Fatalf("changed URL normalized to %q, want %q", got, want)
	}

	normalized.Registries[0].URL = "https://git.example/team/tools.git"
	revalidated, err := Normalize(normalized)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := revalidated.Registries[0].URL, "https://git.example/team/tools"; got != want {
		t.Fatalf("URL matching a stale suffix identity normalized to %q, want %q", got, want)
	}

	revalidated.Registries[0].URL = "://invalid"
	if _, err := Normalize(revalidated); err == nil {
		t.Fatal("Normalize accepted an invalid URL after a repeated-suffix identity change")
	}
}

// spec: acquisition-lock-codec/ordinary-single-suffix-lock-bytes-remain-stable
func TestOrdinaryGitURLLockBytesRemainStable(t *testing.T) {
	doc := Document{
		APIVersion: APIVersionV1,
		Scope:      ScopeRepo,
		Registries: []GitRegistry{{URL: "https://git.example/team/tools.git", Ref: "main", Commit: mustOIDForGolden(sha1a)}},
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := "apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries:\n  - url: https://git.example/team/tools\n    ref: main\n    commit: " + sha1a + "\n"
	if string(got) != want {
		t.Fatalf("ordinary Git lock bytes:\n%s\nwant:\n%s", got, want)
	}
}

func TestCanonicalGitURLSharedNonEquivalentFixtures(t *testing.T) {
	for _, tc := range testutil.NonEquivalentGitURLPairs {
		left, err := CanonicalGitURL(tc.Left)
		if err != nil {
			t.Fatal(err)
		}
		right, err := CanonicalGitURL(tc.Right)
		if err != nil {
			t.Fatal(err)
		}
		if left == right {
			t.Fatalf("%s: canonical identities collapsed: %q", tc.Name, left)
		}
	}
}

func TestStrictDecodeRejectsUnsafeOrAmbiguousYAML(t *testing.T) {
	valid := "apiVersion: agent-marketplace-lock/v1\nscope: repo\n"
	cases := map[string]string{
		"unknown": valid + "surprise: true\n", "duplicate": valid + "scope: repo\n",
		"alias": valid + "registries: &r []\ncopy: *r\n", "merge": valid + "registries:\n  - <<: {url: https://x/a, ref: main}\n",
		"multi document": valid + "---\n" + valid, "conflict": valid + "<<<<<<< ours\n=======\n>>>>>>> theirs\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(data))
			var lockErr *Error
			if !errors.As(err, &lockErr) || lockErr.Code == "" {
				t.Fatalf("error = %T %v, want coded Error", err, err)
			}
		})
	}
}

// spec: acquisition-lock-codec/registry-input-order-does-not-change-emitted-bytes
func TestNormalizeEncodeDeterministic(t *testing.T) {
	doc := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://B.example/r.git", Ref: "z", Commit: mustOID(t, sha1a)}, {URL: "https://a.example/r.git", Ref: "a", Commit: mustOID(t, sha1a)}}}
	first, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Registries[0], doc.Registries[1] = doc.Registries[1], doc.Registries[0]
	second, err := Encode(doc)
	if err != nil || string(first) != string(second) {
		t.Fatalf("ordering changed bytes: %v\n%s\n---\n%s", err, first, second)
	}
}

// spec: acquisition-lock-codec/emitted-bytes-decode-to-the-normalized-document
func TestEncodeSizeBoundary(t *testing.T) {
	const prefix = "apiVersion: agent-marketplace-lock/v1\nscope: repo\nregistries:\n  - url: https://example.com/repo\n    ref: "
	const suffix = "\n    commit: " + sha1a + "\n"
	for _, size := range []int{4194303, 4194304, 4194305} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ref := strings.Repeat("x", size-len(prefix)-len(suffix))
			doc := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://EXAMPLE.com/repo.git", Ref: ref, Commit: mustOID(t, sha1a)}}}
			before := doc
			before.Registries = append([]GitRegistry(nil), doc.Registries...)
			data, err := Encode(doc)
			if !reflect.DeepEqual(doc, before) {
				t.Fatal("Encode mutated its input")
			}
			if size > 4194304 {
				if !errors.Is(err, ErrFileTooLarge) || data != nil {
					t.Fatalf("Encode returned %d bytes, error = %v, want nil bytes and ErrFileTooLarge", len(data), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != prefix+ref+suffix {
				t.Fatalf("encoded boundary fixture differs: got %d bytes, want %d", len(data), size)
			}
			decoded, err := Decode(data)
			before.Registries[0].URL = "https://example.com/repo"
			if err != nil || !reflect.DeepEqual(decoded, before) {
				t.Fatalf("boundary round trip failed: %v", err)
			}
		})
	}
}

func TestPathMapping(t *testing.T) {
	for _, tc := range []struct{ config, lock string }{{"/etc/agent-marketplace/config.yaml", "/etc/agent-marketplace/lock.yaml"}, {"/home/me/.agents/config.yaml", "/home/me/.agents/lock.yaml"}, {"/repo/.agents/config.local.yaml", "/repo/.agents/lock.local.yaml"}} {
		got, err := PathForConfig(tc.config)
		if err != nil || got != tc.lock {
			t.Errorf("PathForConfig(%q) = %q, %v", tc.config, got, err)
		}
	}
	if _, err := PathForConfig("relative/config.yaml"); err == nil {
		t.Fatal("relative path accepted")
	}
}

func mustOID(t *testing.T, value string) GitOID {
	t.Helper()
	oid, err := ParseGitOID(value)
	if err != nil {
		t.Fatal(err)
	}
	return oid
}
