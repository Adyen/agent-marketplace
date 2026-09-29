package lockfile

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

var (
	propScopes       = []Scope{ScopeOrganization, ScopeUser, ScopeRepo, ScopeLocal}
	propGitHosts     = []string{"git-a.example", "git-b.example", "git-c.example"}
	propRepoPaths    = []string{"team/tools", "org/libs", "deep/nested/repo"}
	propRefs         = []string{"main", "develop", "v1.2.3"}
	propHarnesses    = []string{"claude", "devin", "codex"}
	propSchemas      = []string{"v1", "v2", "v3", "v4"}
	propRootPaths    = []string{"skills", "commands", "bundles/deep"}
	propGitKinds     = []string{"skill", "command", "subagent", "hook", "mcpserver"}
	propNames        = []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	propSupportFiles = []string{"docs/a.md", "examples/b.yaml", "notes.txt", "deep/dir/c.md"}
	propMarketHosts  = []string{"market-a.example", "market-b.example", "market-c.example"}
	propInstanceIDs  = []string{"market-id", "zeta-id", "omega-id"}
	propRevisions    = []string{"rev-a", "rev-b", "rev-c"}
	propBindings     = [][2]string{{"tools", "tools-id"}, {"collections", "collections-id"}, {"zeta", "zeta-id"}, {"extra", "extra-id"}}
	propMarketKinds  = []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}
	propAcquisitions = []string{MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndexOnly}
)

func propDigest(t *rapid.T, label string) Digest {
	return Digest(fmt.Sprintf("sha256:%064x", rapid.IntRange(0, 1<<20).Draw(t, label)))
}

func propOID(t *rapid.T, label string) GitOID {
	var raw string
	if rapid.Bool().Draw(t, label+"-alg") {
		raw = fmt.Sprintf("sha256:%064x", rapid.IntRange(0, 1<<20).Draw(t, label))
	} else {
		raw = fmt.Sprintf("sha1:%040x", rapid.IntRange(0, 1<<20).Draw(t, label))
	}
	oid, err := ParseGitOID(raw)
	if err != nil {
		t.Fatalf("generator produced invalid OID %q: %v", raw, err)
	}
	return oid
}

func propDrawGitResource(t *rapid.T, withSupport bool) GitResource {
	r := GitResource{
		Kind:          rapid.SampledFrom(propGitKinds).Draw(t, "kind"),
		Name:          rapid.SampledFrom(propNames).Draw(t, "name"),
		ContentDigest: propDigest(t, "content-digest"),
		ArchiveDigest: propDigest(t, "archive-digest"),
	}
	n := 0
	if withSupport {
		n = rapid.IntRange(1, len(propSupportFiles)).Draw(t, "support-count")
	} else if rapid.Bool().Draw(t, "has-support") {
		n = rapid.IntRange(1, 2).Draw(t, "support-count")
	}
	if n > 0 {
		files := append([]string(nil), propSupportFiles...)
		propShuffle(t, "support-order", files)
		r.SupportFiles = make(map[string]Digest, n)
		for _, f := range files[:n] {
			r.SupportFiles[f] = propDigest(t, "support-digest")
		}
	}
	return r
}

func propDrawGitRoot(t *rapid.T, withSupport bool) GitRoot {
	root := GitRoot{
		Path:           rapid.SampledFrom(propRootPaths).Draw(t, "path"),
		SourceHarness:  rapid.SampledFrom(propHarnesses).Draw(t, "harness"),
		RevisionSchema: rapid.SampledFrom(propSchemas).Draw(t, "schema"),
		RevisionDigest: propDigest(t, "revision-digest"),
	}
	n := rapid.IntRange(1, 4).Draw(t, "resources")
	seen := make(map[string]bool, n)
	for len(root.Resources) < n {
		resource := propDrawGitResource(t, withSupport && len(root.Resources) == 0)
		key := resource.Kind + "\x00" + resource.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		root.Resources = append(root.Resources, resource)
	}
	return root
}

func propDrawGitRegistry(t *rapid.T, minRoots int, withSupport bool) GitRegistry {
	raw := "https://" + rapid.SampledFrom(propGitHosts).Draw(t, "host") + "/" + rapid.SampledFrom(propRepoPaths).Draw(t, "repo")
	canonical, err := CanonicalGitURL(raw)
	if err != nil {
		t.Fatalf("generator produced invalid Git URL %q: %v", raw, err)
	}
	registry := GitRegistry{URL: canonical, Ref: rapid.SampledFrom(propRefs).Draw(t, "ref"), Commit: propOID(t, "commit")}
	n := rapid.IntRange(minRoots, 3).Draw(t, "roots")
	seen := make(map[string]bool, n)
	for len(registry.Roots) < n {
		root := propDrawGitRoot(t, withSupport && len(registry.Roots) == 0)
		key := root.Path + "\x00" + root.SourceHarness
		if seen[key] {
			continue
		}
		seen[key] = true
		registry.Roots = append(registry.Roots, root)
	}
	return registry
}

func propDrawMarketplace(t *rapid.T) MarketplaceLock {
	raw := "https://" + rapid.SampledFrom(propMarketHosts).Draw(t, "mp-host")
	canonical, err := urlutil.NormalizeEndpointURL(raw)
	if err != nil {
		t.Fatalf("generator produced invalid endpoint %q: %v", raw, err)
	}
	mp := MarketplaceLock{URL: canonical, InstanceID: rapid.SampledFrom(propInstanceIDs).Draw(t, "instance")}

	pairs := append([][2]string(nil), propBindings...)
	propShuffle(t, "binding-order", pairs)
	nBindings := rapid.IntRange(2, len(pairs)).Draw(t, "bindings")
	names := pairs[:nBindings]
	for _, pair := range names {
		mp.RegistryBindings = append(mp.RegistryBindings, MarketplaceRegistryBinding{Name: pair[0], RegistryID: pair[1]})
	}

	nRes := rapid.IntRange(1, 4).Draw(t, "mp-resources")
	seen := make(map[string]bool, nRes)
	var collections []MarketplaceResourceLock
	for len(mp.Resources) < nRes {
		binding := names[rapid.IntRange(0, len(names)-1).Draw(t, "binding")]
		r := MarketplaceResourceLock{
			RegistryName: binding[0], RegistryID: binding[1],
			Kind:     rapid.SampledFrom(propMarketKinds).Draw(t, "mp-kind"),
			Name:     rapid.SampledFrom(propNames).Draw(t, "mp-name"),
			Revision: rapid.SampledFrom(propRevisions).Draw(t, "mp-revision"),
		}
		key := r.RegistryID + "\x00" + r.Kind + "\x00" + r.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		if r.Kind == "collection" {
			r.ContentDigest = propDigest(t, "mp-content")
			r.CollectionManifestDigest = r.ContentDigest
			r.Acquisition = MarketplaceAcquisitionMirror
			collections = append(collections, r)
		} else {
			r.ContentDigest = propDigest(t, "mp-content")
			r.ArchiveDigest = propDigest(t, "mp-archive")
			switch rapid.SampledFrom(propAcquisitions).Draw(t, "acquisition") {
			case MarketplaceAcquisitionIndexOnly:
				r.Acquisition = MarketplaceAcquisitionIndexOnly
				r.RegistryCommit = fmt.Sprintf("%040x", rapid.IntRange(0, 1<<20).Draw(t, "mp-commit"))
				r.RegistryManifestDigest = propDigest(t, "mp-manifest")
				r.SourcePath = rapid.SampledFrom(propSupportFiles).Draw(t, "mp-source")
			default:
				r.Acquisition = MarketplaceAcquisitionMirror
			}
		}
		mp.Resources = append(mp.Resources, r)
	}
	for i := range mp.Resources {
		r := &mp.Resources[i]
		if r.Kind == "collection" || len(collections) == 0 || !rapid.Bool().Draw(t, "has-selection") {
			continue
		}
		collection := collections[rapid.IntRange(0, len(collections)-1).Draw(t, "selection-target")]
		r.SelectedBy = []MarketplaceSelectionLock{
			{RegistryID: collection.RegistryID, Name: collection.Name, Revision: collection.ContentDigest},
		}
	}
	return mp
}

func propDrawDocument(t *rapid.T, apiVersion string, minRoots int, needSupport, needMarketplace bool) Document {
	doc := Document{APIVersion: apiVersion, Scope: rapid.SampledFrom(propScopes).Draw(t, "scope")}
	minRegistries, maxRegistries := 0, 2
	if apiVersion == APIVersionV1 || minRoots > 0 {
		minRegistries = 1
		maxRegistries = 3
	}
	seen := make(map[string]bool)
	for n := rapid.IntRange(minRegistries, maxRegistries).Draw(t, "registries"); len(doc.Registries) < n; {
		registry := propDrawGitRegistry(t, minRoots, needSupport && len(doc.Registries) == 0)
		key := registry.URL + "\x00" + registry.Ref
		if seen[key] {
			continue
		}
		seen[key] = true
		doc.Registries = append(doc.Registries, registry)
	}
	if apiVersion == APIVersionV2 && (needMarketplace || rapid.Bool().Draw(t, "has-marketplaces")) {
		mpSeen := make(map[string]bool)
		for n := rapid.IntRange(1, 2).Draw(t, "marketplaces"); len(doc.Marketplaces) < n; {
			mp := propDrawMarketplace(t)
			if mpSeen[mp.URL] {
				continue
			}
			mpSeen[mp.URL] = true
			doc.Marketplaces = append(doc.Marketplaces, mp)
		}
	}
	return doc
}

func propCanon(d Document) Document {
	out := cloneDocument(d)
	for i := range out.Registries {
		registry := &out.Registries[i]
		for j := range registry.Roots {
			slices.SortFunc(registry.Roots[j].Resources, func(a, b GitResource) int {
				return strings.Compare(a.Kind+"\x00"+a.Name, b.Kind+"\x00"+b.Name)
			})
		}
		slices.SortFunc(registry.Roots, func(a, b GitRoot) int {
			return strings.Compare(a.Path+"\x00"+a.SourceHarness, b.Path+"\x00"+b.SourceHarness)
		})
	}
	slices.SortFunc(out.Registries, func(a, b GitRegistry) int {
		return strings.Compare(a.URL+"\x00"+a.Ref, b.URL+"\x00"+b.Ref)
	})
	for i := range out.Marketplaces {
		mp := &out.Marketplaces[i]
		slices.SortFunc(mp.RegistryBindings, func(a, b MarketplaceRegistryBinding) int {
			return strings.Compare(a.Name+"\x00"+a.RegistryID, b.Name+"\x00"+b.RegistryID)
		})
		slices.SortFunc(mp.Resources, func(a, b MarketplaceResourceLock) int {
			return strings.Compare(propMpResourceKey(a), propMpResourceKey(b))
		})
		for j := range mp.Resources {
			slices.SortFunc(mp.Resources[j].SelectedBy, func(a, b MarketplaceSelectionLock) int {
				return strings.Compare(propSelectionKey(a), propSelectionKey(b))
			})
		}
	}
	slices.SortFunc(out.Marketplaces, func(a, b MarketplaceLock) int {
		return strings.Compare(a.URL+"\x00"+a.InstanceID, b.URL+"\x00"+b.InstanceID)
	})
	return out
}

func propMpResourceKey(r MarketplaceResourceLock) string {
	return r.RegistryID + "\x00" + r.Kind + "\x00" + r.Name
}

func propSelectionKey(s MarketplaceSelectionLock) string {
	return s.RegistryID + "\x00" + s.Name + "\x00" + s.Revision.String()
}

func propShuffle[T any](t *rapid.T, label string, items []T) {
	for i := len(items) - 1; i > 0; i-- {
		j := rapid.IntRange(0, i).Draw(t, label)
		items[i], items[j] = items[j], items[i]
	}
}

func propPermute(t *rapid.T, d Document) Document {
	out := cloneDocument(d)
	for i := range out.Registries {
		registry := &out.Registries[i]
		for j := range registry.Roots {
			root := &registry.Roots[j]
			propShuffle(t, "perm-res", root.Resources)
			for k := range root.Resources {
				files := root.Resources[k].SupportFiles
				if len(files) == 0 {
					continue
				}
				keys := make([]string, 0, len(files))
				for name := range files {
					keys = append(keys, name)
				}
				slices.Sort(keys)
				propShuffle(t, "perm-support", keys)
				rebuilt := make(map[string]Digest, len(files))
				for _, name := range keys {
					rebuilt[name] = files[name]
				}
				root.Resources[k].SupportFiles = rebuilt
			}
		}
		propShuffle(t, "perm-roots", registry.Roots)
	}
	propShuffle(t, "perm-registries", out.Registries)
	for i := range out.Marketplaces {
		mp := &out.Marketplaces[i]
		propShuffle(t, "perm-bindings", mp.RegistryBindings)
		propShuffle(t, "perm-mp-resources", mp.Resources)
		for j := range mp.Resources {
			propShuffle(t, "perm-selections", mp.Resources[j].SelectedBy)
		}
	}
	propShuffle(t, "perm-marketplaces", out.Marketplaces)
	return out
}

func propAssertOrder(t *rapid.T, doc Document) {
	t.Helper()
	if !slices.IsSortedFunc(doc.Registries, func(a, b GitRegistry) int {
		if a.URL != b.URL {
			return strings.Compare(a.URL, b.URL)
		}
		return strings.Compare(a.Ref, b.Ref)
	}) {
		t.Fatalf("registries not ordered by (url, ref)")
	}
	for _, registry := range doc.Registries {
		if !slices.IsSortedFunc(registry.Roots, func(a, b GitRoot) int {
			if a.Path != b.Path {
				return strings.Compare(a.Path, b.Path)
			}
			return strings.Compare(a.SourceHarness, b.SourceHarness)
		}) {
			t.Fatalf("roots not ordered by (path, source_harness) in %q", registry.URL)
		}
		for _, root := range registry.Roots {
			if !slices.IsSortedFunc(root.Resources, func(a, b GitResource) int {
				if a.Kind != b.Kind {
					return strings.Compare(a.Kind, b.Kind)
				}
				return strings.Compare(a.Name, b.Name)
			}) {
				t.Fatalf("resources not ordered by (kind, name) in root %q", root.Path)
			}
		}
	}
	if !slices.IsSortedFunc(doc.Marketplaces, func(a, b MarketplaceLock) int {
		if a.URL != b.URL {
			return strings.Compare(a.URL, b.URL)
		}
		return strings.Compare(a.InstanceID, b.InstanceID)
	}) {
		t.Fatalf("marketplaces not ordered by (url, instance_id)")
	}
	for _, mp := range doc.Marketplaces {
		if !slices.IsSortedFunc(mp.RegistryBindings, func(a, b MarketplaceRegistryBinding) int {
			if a.Name != b.Name {
				return strings.Compare(a.Name, b.Name)
			}
			return strings.Compare(a.RegistryID, b.RegistryID)
		}) {
			t.Fatalf("bindings not ordered in %q", mp.URL)
		}
		if !slices.IsSortedFunc(mp.Resources, func(a, b MarketplaceResourceLock) int {
			return strings.Compare(propMpResourceKey(a), propMpResourceKey(b))
		}) {
			t.Fatalf("marketplace resources not ordered in %q", mp.URL)
		}
		for _, r := range mp.Resources {
			if !slices.IsSortedFunc(r.SelectedBy, func(a, b MarketplaceSelectionLock) int {
				return strings.Compare(propSelectionKey(a), propSelectionKey(b))
			}) {
				t.Fatalf("selections not ordered in %q resource %q", mp.URL, r.Name)
			}
		}
	}
}

func propCheckDocument(t *rapid.T, doc Document) {
	t.Helper()
	data, err := Encode(doc)
	if err != nil {
		t.Fatalf("Encode rejected generated document: %v\n%#v", err, doc)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode rejected encoded document: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(propCanon(decoded), propCanon(doc)) {
		t.Fatalf("round trip changed semantic content:\n%#v\nwant:\n%#v", decoded, doc)
	}
	propAssertOrder(t, decoded)
	again, err := Encode(decoded)
	if err != nil {
		t.Fatalf("Encode rejected decoded document: %v", err)
	}
	if !bytes.Equal(data, again) {
		t.Fatalf("fixed point broken:\n%s\nwant:\n%s", again, data)
	}
	permuted, err := Encode(propPermute(t, doc))
	if err != nil {
		t.Fatalf("Encode rejected permuted document: %v", err)
	}
	if !bytes.Equal(data, permuted) {
		t.Fatalf("permutation changed bytes:\n%s\nwant:\n%s", permuted, data)
	}
}

func TestLockfileCodecProperties(t *testing.T) {
	cases := map[string]func(*rapid.T) Document{
		"v1-git": func(t *rapid.T) Document {
			return propDrawDocument(t, APIVersionV1, 1, false, false)
		},
		"v2-marketplace": func(t *rapid.T) Document {
			return propDrawDocument(t, APIVersionV2, 0, false, true)
		},
		"multi-root": func(t *rapid.T) Document {
			return propDrawDocument(t, APIVersionV1, 2, false, false)
		},
		"support-files": func(t *rapid.T) Document {
			return propDrawDocument(t, APIVersionV1, 1, true, false)
		},
		"mixed": func(t *rapid.T) Document {
			version := APIVersionV1
			if rapid.Bool().Draw(t, "v2") {
				version = APIVersionV2
			}
			return propDrawDocument(t, version, 0, false, false)
		},
	}
	for name, draw := range cases {
		t.Run(name, rapid.MakeCheck(func(t *rapid.T) {
			propCheckDocument(t, draw(t))
		}))
	}
}

var propDupSites = []string{"registry", "root", "resource", "marketplace", "binding", "mp-resource"}

func propDuplicateSite(t *rapid.T, doc *Document, site string, conflicting bool) {
	switch site {
	case "registry":
		dup := doc.Registries[0]
		if conflicting {
			dup.Commit = propOID(t, "conflict-commit")
			for dup.Commit == doc.Registries[0].Commit {
				dup.Commit = propOID(t, "conflict-commit")
			}
		}
		doc.Registries = append(doc.Registries, dup)
	case "root":
		root := &doc.Registries[0]
		dup := root.Roots[0]
		if conflicting {
			dup.RevisionDigest = propDigest(t, "conflict-digest")
			for dup.RevisionDigest == root.Roots[0].RevisionDigest {
				dup.RevisionDigest = propDigest(t, "conflict-digest")
			}
		}
		root.Roots = append(root.Roots, dup)
	case "resource":
		root := &doc.Registries[0].Roots[0]
		dup := root.Resources[0]
		if conflicting {
			dup.ContentDigest = propDigest(t, "conflict-digest")
			for dup.ContentDigest == root.Resources[0].ContentDigest {
				dup.ContentDigest = propDigest(t, "conflict-digest")
			}
		}
		root.Resources = append(root.Resources, dup)
	case "marketplace":
		dup := doc.Marketplaces[0]
		if conflicting {
			dup.InstanceID = rapid.SampledFrom(propInstanceIDs).Draw(t, "conflict-instance")
			for dup.InstanceID == doc.Marketplaces[0].InstanceID {
				dup.InstanceID = rapid.SampledFrom(propInstanceIDs).Draw(t, "conflict-instance")
			}
		}
		doc.Marketplaces = append(doc.Marketplaces, dup)
	case "binding":
		mp := &doc.Marketplaces[0]
		dup := mp.RegistryBindings[0]
		if conflicting {
			dup.RegistryID = rapid.SampledFrom(propInstanceIDs).Draw(t, "conflict-binding-id")
			for dup.RegistryID == mp.RegistryBindings[0].RegistryID {
				dup.RegistryID = rapid.SampledFrom(propInstanceIDs).Draw(t, "conflict-binding-id")
			}
		}
		mp.RegistryBindings = append(mp.RegistryBindings, dup)
	default:
		mp := &doc.Marketplaces[0]
		dup := mp.Resources[0]
		if conflicting {
			dup.Revision = rapid.SampledFrom(propRevisions).Draw(t, "conflict-revision")
			for dup.Revision == mp.Resources[0].Revision {
				dup.Revision = rapid.SampledFrom(propRevisions).Draw(t, "conflict-revision")
			}
		}
		mp.Resources = append(mp.Resources, dup)
	}
}

func TestLockfileDuplicateProperties(t *testing.T) {
	t.Run("exact-duplicate", rapid.MakeCheck(func(t *rapid.T) {
		doc := propDrawDocument(t, APIVersionV2, 1, true, true)
		want, err := Encode(doc)
		if err != nil {
			t.Fatalf("Encode rejected generated document: %v", err)
		}
		for _, site := range propDupSites {
			dup := cloneDocument(doc)
			propDuplicateSite(t, &dup, site, false)
			got, err := Encode(dup)
			if err != nil {
				t.Fatalf("Encode rejected exact %s duplicate: %v", site, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("exact %s duplicate changed bytes:\n%s\nwant:\n%s", site, got, want)
			}
		}
	}))
	t.Run("conflicting-duplicate", rapid.MakeCheck(func(t *rapid.T) {
		doc := propDrawDocument(t, APIVersionV2, 1, false, true)
		for _, site := range propDupSites {
			dup := cloneDocument(doc)
			propDuplicateSite(t, &dup, site, true)
			_, err := Encode(dup)
			if err == nil {
				t.Fatalf("Encode accepted a conflicting %s duplicate", site)
			}
			var lockErr *Error
			if !errors.As(err, &lockErr) || lockErr.Code != CodeIdentityConflict {
				t.Fatalf("%s duplicate error = %v, want *lockfile.Error code %q", site, err, CodeIdentityConflict)
			}
		}
	}))
}
