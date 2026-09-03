package lockfile

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestCanonicalGitURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{" HTTPS://Git.Example.COM:443//team/repo.git/ ", "https://git.example.com/team/repo"},
		{"http://git.example.com:80/team/repo.git", "http://git.example.com/team/repo"},
		{"ssh://Git@Git.Example.COM:22//team/repo.git", "ssh://Git@git.example.com/team/repo"},
		{"git@git.example.com:team//repo.git", "git+scp://git@git.example.com/team/repo"},
		{"git+scp://git@git.example.com/team/repo", "git+scp://git@git.example.com/team/repo"},
		{"file:///tmp/repos/example.git", "file:///tmp/repos/example"},
	} {
		got, err := CanonicalGitURL(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("CanonicalGitURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, in := range []string{"https://host/repo?token=x", "https://u:p@host/repo", "https://host/a/%2f/b", "https://host/a/../b", "file://host/tmp/a", "file:relative", "git@host:/absolute"} {
		if got, err := CanonicalGitURL(in); err == nil {
			t.Errorf("CanonicalGitURL(%q) = %q, want error", in, got)
		}
	}
}

func TestStrictDecodeRejectsUnsafeOrAmbiguousYAML(t *testing.T) {
	valid := "apiVersion: agent-marketplace-lock/v1\nscope: repo\n"
	cases := map[string]string{
		"unknown":        valid + "surprise: true\n",
		"duplicate":      "apiVersion: agent-marketplace-lock/v1\nscope: repo\nscope: repo\n",
		"alias":          valid + "registries: &r []\nmarketplaces: *r\n",
		"merge":          valid + "registries:\n  - <<: {url: https://x/a, ref: main}\n",
		"multi document": valid + "---\n" + valid,
		"conflict":       valid + "<<<<<<< ours\n=======\n>>>>>>> theirs\n",
		"version":        "apiVersion: agent-marketplace-lock/v2\nscope: repo\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(data))
			if err == nil {
				t.Fatal("Decode succeeded")
			}
			var lockErr *Error
			if !errors.As(err, &lockErr) || lockErr.Code == "" {
				t.Fatalf("error = %T %v, want coded Error", err, err)
			}
		})
	}
}

func TestNormalizeEncodeDeterministic(t *testing.T) {
	doc := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{
		{URL: "https://B.example/r.git", Ref: "z", Commit: mustOID(t, sha1a)},
		{URL: "https://a.example/r.git", Ref: "a", Commit: mustOID(t, sha1a)},
	}}
	first, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	doc.Registries[0], doc.Registries[1] = doc.Registries[1], doc.Registries[0]
	second, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("ordering changed bytes:\n%s\n---\n%s", first, second)
	}
	decoded, err := Decode(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Registries[0].URL; got != "https://a.example/r" {
		t.Fatalf("first URL = %q", got)
	}
}

func TestPathMapping(t *testing.T) {
	for _, tc := range []struct{ config, lock string }{
		{"/etc/agent-marketplace/config.yaml", "/etc/agent-marketplace/lock.yaml"},
		{"/home/me/.agents/config.yaml", "/home/me/.agents/lock.yaml"},
		{"/repo/.agents/config.local.yaml", "/repo/.agents/lock.local.yaml"},
	} {
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
	old := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{
		{URL: "https://example/a", Ref: "main", Commit: mustOID(t, sha1a), Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}}},
		{URL: "https://example/orphan", Ref: "main", Commit: mustOID(t, sha1a)},
	}}
	reqs := Requirements{Scope: ScopeRepo, Git: []GitRequirement{
		{URL: "https://example/a.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills", SourceHarness: "claude"}}},
		{URL: "https://example/new", Ref: "main"},
	}}
	plan, err := Plan(reqs, &old, OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Retained) != 1 || len(plan.Added) != 1 || len(plan.Removed) != 1 || len(plan.Acquisitions) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.Changed() {
		t.Fatal("plan with add/remove reported unchanged")
	}
}

func TestPlanUnchangedAndUpgradeSelection(t *testing.T) {
	locked := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{
		URL: "https://example/a", Ref: "main", Commit: mustOID(t, sha1a),
		Roots: []GitRoot{{Path: "skills", SourceHarness: "claude", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}},
	}}}
	requirements := Requirements{Scope: ScopeRepo, Git: []GitRequirement{{URL: "https://example/a.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills", SourceHarness: "claude"}}}}}
	plan, err := Plan(requirements, &locked, OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed() || len(plan.Retained) != 1 || !reflect.DeepEqual(plan.Desired, locked) {
		t.Fatalf("unchanged plan = %#v", plan)
	}

	requirements.Upgrade = UpgradeSelection{Git: []GitUpgradeSelector{{URL: "https://example/other"}}}
	plan, err = Plan(requirements, &locked, OperationUpgrade)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed() {
		t.Fatalf("non-matching targeted upgrade changed plan: %#v", plan)
	}

	requirements.Upgrade = UpgradeSelection{Git: []GitUpgradeSelector{{URL: "https://example/a", Path: "skills"}}}
	plan, err = Plan(requirements, &locked, OperationUpgrade)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Upgraded) != 1 || len(plan.Acquisitions) != 1 {
		t.Fatalf("targeted upgrade plan = %#v", plan)
	}
}

func TestMarketplaceUpgradeSelectionIncludesEndpointURL(t *testing.T) {
	selection := UpgradeSelection{Marketplace: []MarketplaceUpgradeSelector{{URL: "https://one.example", InstanceID: "mp", SourceID: "src", Kind: "skill", Name: "review"}}}
	requirement := MarketplaceRequirement{SourceID: "src", Kind: "skill", Name: "review"}
	if !selection.SelectsMarketplace(MarketplaceRequirements{URL: "https://one.example", InstanceID: "mp"}, requirement) {
		t.Fatal("matching endpoint was not selected")
	}
	if selection.SelectsMarketplace(MarketplaceRequirements{URL: "https://two.example", InstanceID: "mp"}, requirement) {
		t.Fatal("same instance and resource at another endpoint was selected")
	}
}

func TestMarketplaceUpgradeDoesNotRetainAcquiredRecord(t *testing.T) {
	for _, collection := range []bool{false, true} {
		t.Run(map[bool]string{false: "resource", true: "collection"}[collection], func(t *testing.T) {
			market := Marketplace{URL: "https://market.example", InstanceID: "mp"}
			kind := "skill"
			if collection {
				kind = "collection"
				market.Collections = []MarketplaceCollection{{SourceID: "src", Name: "root", Revision: Digest(sha256a), Direct: true}}
			} else {
				market.Resources = []MarketplaceResource{marketplaceTestResource("src", kind, "root", Digest(sha256a), true)}
			}
			current := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{market}}
			requirements := Requirements{Scope: ScopeRepo, Marketplaces: []MarketplaceRequirements{{URL: "https://market.example", InstanceID: "mp", Resources: []MarketplaceRequirement{{SourceID: "src", Kind: kind, Name: "root", Collection: collection}}}}}
			plan, err := Plan(requirements, &current, OperationUpgrade)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Acquisitions) != 1 || len(plan.Upgraded) != 1 || len(plan.Desired.Marketplaces) != 0 {
				t.Fatalf("upgrade retained the old record: %#v", plan)
			}
		})
	}
}

func TestReconcileMarketplaceGraphTransitionsAndSharedClosure(t *testing.T) {
	leafRevision := Digest(sha256a)
	collectionRevision := Digest("sha256:" + strings.Repeat("b", 64))
	rootARevision := Digest("sha256:" + strings.Repeat("c", 64))
	rootBRevision := Digest("sha256:" + strings.Repeat("d", 64))
	leaf := marketplaceTestResource("src", "skill", "leaf", leafRevision, true)
	orphan := marketplaceTestResource("src", "skill", "orphan", leafRevision, false)
	rootA := MarketplaceCollection{SourceID: "sets", Name: "a", Revision: rootARevision, Direct: true, Members: []CollectionEdge{
		{SourceID: "sets", Kind: "collection", Name: "shared", Revision: collectionRevision},
		{SourceID: "src", Kind: "skill", Name: "orphan", Revision: leafRevision},
	}}
	rootB := MarketplaceCollection{SourceID: "sets", Name: "b", Revision: rootBRevision, Direct: true, Members: []CollectionEdge{{SourceID: "sets", Kind: "collection", Name: "shared", Revision: collectionRevision}}}
	shared := MarketplaceCollection{SourceID: "sets", Name: "shared", Revision: collectionRevision, Direct: true, Members: []CollectionEdge{{SourceID: "src", Kind: "skill", Name: "leaf", Revision: leafRevision}}}
	base := Marketplace{URL: "https://market.example", InstanceID: "mp", Collections: []MarketplaceCollection{rootA, rootB, shared}, Resources: []MarketplaceResource{leaf, orphan}}

	tests := []struct {
		name         string
		requirements []MarketplaceRequirement
		wantRoots    []string
		leafDirect   bool
		selectors    []string
		wantOrphan   bool
	}{
		{name: "shared nested closure", requirements: []MarketplaceRequirement{{SourceID: "sets", Kind: "collection", Name: "a", Collection: true}, {SourceID: "sets", Kind: "collection", Name: "b", Collection: true}}, wantRoots: []string{"a", "b", "shared"}, selectors: []string{"a", "b"}, wantOrphan: true},
		{name: "resource to collection and nested direct demotion", requirements: []MarketplaceRequirement{{SourceID: "sets", Kind: "collection", Name: "a", Collection: true}}, wantRoots: []string{"a", "shared"}, selectors: []string{"a"}, wantOrphan: true},
		{name: "remove one shared root", requirements: []MarketplaceRequirement{{SourceID: "sets", Kind: "collection", Name: "b", Collection: true}}, wantRoots: []string{"b", "shared"}, selectors: []string{"b"}},
		{name: "collection to direct resource", requirements: []MarketplaceRequirement{{SourceID: "src", Kind: "skill", Name: "leaf"}}, leafDirect: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := cloneDocument(Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{base}})
			requirements := []MarketplaceRequirements{{URL: "https://market.example", InstanceID: "mp", Resources: tc.requirements}}
			if err := ReconcileMarketplaceGraph(&doc, requirements); err != nil {
				t.Fatal(err)
			}
			normalized, err := Normalize(doc)
			if err != nil {
				t.Fatal(err)
			}
			market := normalized.Marketplaces[0]
			gotRoots := make([]string, len(market.Collections))
			for i, collection := range market.Collections {
				gotRoots[i] = collection.Name
				if collection.Direct != (collection.Name == "a" && containsString(tc.wantRoots, "a") || collection.Name == "b" && containsString(tc.wantRoots, "b")) {
					t.Fatalf("collection %s direct = %v", collection.Name, collection.Direct)
				}
			}
			if !equalStrings(gotRoots, tc.wantRoots) {
				t.Fatalf("collections = %v, want %v", gotRoots, tc.wantRoots)
			}
			gotLeaf, ok := findMarketplaceTestResource(market.Resources, "leaf")
			if !ok || gotLeaf.Direct != tc.leafDirect {
				t.Fatalf("leaf = %#v, present %v", gotLeaf, ok)
			}
			gotSelectors := make([]string, len(gotLeaf.SelectedBy))
			for i, selector := range gotLeaf.SelectedBy {
				gotSelectors[i] = selector.Name
			}
			if !equalStrings(gotSelectors, tc.selectors) {
				t.Fatalf("leaf selectors = %v, want %v", gotSelectors, tc.selectors)
			}
			_, gotOrphan := findMarketplaceTestResource(market.Resources, "orphan")
			if gotOrphan != tc.wantOrphan {
				t.Fatalf("orphan present = %v, want %v", gotOrphan, tc.wantOrphan)
			}
		})
	}
}

func TestMarketplaceURLCanonicalizationUsesAuthoritativeSafetyRules(t *testing.T) {
	endpoint, err := normalizeMarketplaceRequirements(MarketplaceRequirements{URL: " HTTPS://Market.Example:443/api/ ", InstanceID: "mp"})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.URL != "https://market.example:443/api" {
		t.Fatalf("canonical URL = %q", endpoint.URL)
	}
	for _, raw := range []string{"https://market.example/a/../b", "https://market.example/a%2fb"} {
		if _, err := normalizeMarketplaceRequirements(MarketplaceRequirements{URL: raw, InstanceID: "mp"}); err == nil {
			t.Fatalf("unsafe URL %q accepted", raw)
		}
	}
}

func TestMarketplaceCoordinatesRejectUnsafeFieldsAndKinds(t *testing.T) {
	digest := Digest(sha256a)
	valid := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{{
		URL: "https://market.example", InstanceID: "market",
		Collections: []MarketplaceCollection{{SourceID: "sets", Name: "base", Revision: digest, Direct: true, Members: []CollectionEdge{{SourceID: "source", Kind: "skill", Name: "review", Revision: digest}}}},
		Resources:   []MarketplaceResource{{SourceID: "source", Kind: "skill", Name: "review", Revision: digest, Acquisition: AcquisitionMirror, ArchiveDigest: digest, SelectedBy: []CollectionEdge{{SourceID: "sets", Name: "base", Revision: digest}}}},
	}}}
	cases := map[string]func(*Document){
		"instance whitespace":         func(doc *Document) { doc.Marketplaces[0].InstanceID = "market id" },
		"collection source traversal": func(doc *Document) { doc.Marketplaces[0].Collections[0].SourceID = "../sets" },
		"collection name control":     func(doc *Document) { doc.Marketplaces[0].Collections[0].Name = "base\n" },
		"member source whitespace":    func(doc *Document) { doc.Marketplaces[0].Collections[0].Members[0].SourceID = "source id" },
		"member kind unsupported":     func(doc *Document) { doc.Marketplaces[0].Collections[0].Members[0].Kind = "rule" },
		"resource kind unsupported":   func(doc *Document) { doc.Marketplaces[0].Resources[0].Kind = "rule" },
		"resource name separator":     func(doc *Document) { doc.Marketplaces[0].Resources[0].Name = "review\\copy" },
		"selected by name whitespace": func(doc *Document) { doc.Marketplaces[0].Resources[0].SelectedBy[0].Name = "base id" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := cloneDocument(valid)
			mutate(&doc)
			err := Validate(doc)
			var lockErr *Error
			if !errors.As(err, &lockErr) || lockErr.Code != CodeInvalidDocument {
				t.Fatalf("Validate error = %v, want %s", err, CodeInvalidDocument)
			}
		})
	}
}

func TestMarketplaceCollectionGraphRejectsExcessiveDepth(t *testing.T) {
	const depth = maxCollectionGraphDepth + 1
	digest := Digest(sha256a)
	collections := make([]MarketplaceCollection, depth)
	for i := range collections {
		collections[i] = MarketplaceCollection{SourceID: "sets", Name: "c" + strings.Repeat("x", i+1), Revision: digest, Direct: i == 0}
		if i+1 < len(collections) {
			collections[i].Members = []CollectionEdge{{SourceID: "sets", Kind: "collection", Name: "c" + strings.Repeat("x", i+2), Revision: digest}}
		}
	}
	err := Validate(Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Marketplaces: []Marketplace{{
		URL: "https://market.example", InstanceID: "market", Collections: collections,
	}}})
	var lockErr *Error
	if !errors.As(err, &lockErr) || lockErr.Code != CodeInvalidDocument || !strings.Contains(err.Error(), "depth limit") {
		t.Fatalf("depth validation error = %v", err)
	}
}

func TestPlanAutodetectedGitHarnessMatchesExistingRoot(t *testing.T) {
	current := Document{APIVersion: APIVersionV1, Scope: ScopeRepo, Registries: []GitRegistry{{
		URL: "https://example.com/repo", Ref: "main", Commit: mustOID(t, sha1a),
		Roots: []GitRoot{{Path: "skills", SourceHarness: "factory", RevisionSchema: "v1", RevisionDigest: Digest(sha256a)}},
	}}}
	requirements := Requirements{Scope: ScopeRepo, Git: []GitRequirement{{
		URL: "https://example.com/repo.git", Ref: "main", Roots: []GitRootRequirement{{Path: "skills"}},
	}}}
	plan, err := Plan(requirements, &current, OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed() || len(plan.Retained) != 1 || len(plan.Acquisitions) != 0 {
		t.Fatalf("autodetected plan = %#v", plan)
	}
	if len(plan.Desired.Registries) != 1 || plan.Desired.Registries[0].Roots[0].SourceHarness != "factory" {
		t.Fatalf("autodetected root was not retained: %#v", plan.Desired.Registries)
	}

	requirements.Git[0].Roots[0].SourceHarness = "claude"
	plan, err = Plan(requirements, &current, OperationSync)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Invalidated) != 1 || len(plan.Acquisitions) != 1 {
		t.Fatalf("explicit harness mismatch plan = %#v", plan)
	}
}

func marketplaceTestResource(source, kind, name string, revision Digest, direct bool) MarketplaceResource {
	return MarketplaceResource{SourceID: source, Kind: kind, Name: name, Revision: revision, Acquisition: AcquisitionMirror, ArchiveDigest: revision, Direct: direct}
}

func findMarketplaceTestResource(resources []MarketplaceResource, name string) (MarketplaceResource, bool) {
	for _, resource := range resources {
		if resource.Name == name {
			return resource, true
		}
	}
	return MarketplaceResource{}, false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mustOID(t *testing.T, value string) GitOID {
	t.Helper()
	oid, err := ParseGitOID(value)
	if err != nil {
		t.Fatal(err)
	}
	return oid
}
