package lockfile

import (
	"errors"
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
		"version": "apiVersion: agent-marketplace-lock/v2\nscope: repo\n",
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

func TestPlanMinimalRetainsAndRemoves(t *testing.T) {
	old := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://example/a", Ref: "main", Commit: mustOID(t, sha1a), Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}}, {URL: "https://example/orphan", Ref: "main", Commit: mustOID(t, sha1a)}}}
	reqs := Requirements{Scope: ScopeRepo, Git: []GitRequirement{{URL: "https://example/a.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills", SourceHarness: "claude"}}}, {URL: "https://example/new", Ref: "main"}}}
	plan, err := Plan(reqs, &old, OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Retained) != 1 || len(plan.Added) != 1 || len(plan.Removed) != 1 || len(plan.Acquisitions) != 1 || !plan.Changed() {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanUnchangedAndUpgradeSelection(t *testing.T) {
	locked := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://example/a", Ref: "main", Commit: mustOID(t, sha1a), Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}}}}
	requirements := Requirements{Scope: ScopeRepo, Git: []GitRequirement{{URL: "https://example/a.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills", SourceHarness: "claude"}}}}}
	plan, err := Plan(requirements, &locked, OperationSync)
	if err != nil || plan.Changed() || len(plan.Retained) != 1 || !reflect.DeepEqual(plan.Desired, locked) {
		t.Fatalf("unchanged plan = %#v, %v", plan, err)
	}
	requirements.Upgrade = UpgradeSelection{Git: []GitUpgradeSelector{{URL: "https://example/a", Path: "skills"}}}
	plan, err = Plan(requirements, &locked, OperationUpgrade)
	if err != nil || len(plan.Upgraded) != 1 || len(plan.Acquisitions) != 1 {
		t.Fatalf("targeted upgrade plan = %#v, %v", plan, err)
	}
}

func TestPlanAutodetectedGitHarnessMatchesExistingRoot(t *testing.T) {
	current := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{URL: "https://example.com/repo", Ref: "main", Commit: mustOID(t, sha1a), Roots: []GitRoot{{Path: "skills", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}}}}
	requirements := Requirements{Scope: ScopeRepo, Git: []GitRequirement{{URL: "https://example.com/repo.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills"}}}}}
	plan, err := Plan(requirements, &current, OperationSync)
	if err != nil || plan.Changed() || len(plan.Retained) != 1 || plan.Desired.Registries[0].Roots[0].SourceHarness != "factory" {
		t.Fatalf("autodetected plan = %#v, %v", plan, err)
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
