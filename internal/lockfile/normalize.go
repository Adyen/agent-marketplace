package lockfile

import (
	"reflect"
	"sort"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

func cloneDocument(in Document) Document {
	out := in
	out.Registries = append([]GitRegistry(nil), in.Registries...)
	for i := range out.Registries {
		out.Registries[i].Roots = append([]GitRoot(nil), in.Registries[i].Roots...)
		for j := range out.Registries[i].Roots {
			out.Registries[i].Roots[j].Resources = append([]GitResource(nil), in.Registries[i].Roots[j].Resources...)
			for k := range out.Registries[i].Roots[j].Resources {
				source := in.Registries[i].Roots[j].Resources[k].SupportFiles
				out.Registries[i].Roots[j].Resources[k].SupportFiles = make(map[string]Digest, len(source))
				for name, digest := range source {
					out.Registries[i].Roots[j].Resources[k].SupportFiles[name] = digest
				}
			}
		}
	}
	out.Marketplaces = append([]Marketplace(nil), in.Marketplaces...)
	for i := range out.Marketplaces {
		out.Marketplaces[i].Collections = append([]MarketplaceCollection(nil), in.Marketplaces[i].Collections...)
		for j := range out.Marketplaces[i].Collections {
			out.Marketplaces[i].Collections[j].Members = append([]CollectionEdge(nil), in.Marketplaces[i].Collections[j].Members...)
		}
		out.Marketplaces[i].Resources = append([]MarketplaceResource(nil), in.Marketplaces[i].Resources...)
		for j := range out.Marketplaces[i].Resources {
			out.Marketplaces[i].Resources[j].SelectedBy = append([]CollectionEdge(nil), in.Marketplaces[i].Resources[j].SelectedBy...)
		}
	}
	return out
}

func dedupeGitResources(items []GitResource) ([]GitResource, error) {
	return dedupeComparable(items, func(v GitResource) string { return v.Kind + "\x00" + v.Name }, "Git resource")
}
func dedupeRoots(items []GitRoot) ([]GitRoot, error) {
	return dedupeComparable(items, func(v GitRoot) string { return v.Path + "\x00" + v.SourceHarness }, "Git root")
}
func dedupeRegistries(items []GitRegistry) ([]GitRegistry, error) {
	return dedupeComparable(items, func(v GitRegistry) string { return v.URL + "\x00" + v.Ref }, "Git registry")
}
func dedupeMarketplaces(items []Marketplace) ([]Marketplace, error) {
	return dedupeComparable(items, func(v Marketplace) string { return v.InstanceID + "\x00" + v.URL }, "Marketplace")
}

func dedupeComparable[T any](items []T, identity func(T) string, noun string) ([]T, error) {
	if len(items) < 2 {
		return items, nil
	}
	out := items[:0]
	for _, item := range items {
		if len(out) > 0 && identity(out[len(out)-1]) == identity(item) {
			if !reflect.DeepEqual(out[len(out)-1], item) {
				return nil, coded(CodeIdentityConflict, "%s identity %q has conflicting records", noun, identity(item))
			}
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func normalizeMarketplace(m *Marketplace) error {
	var err error
	m.URL, err = canonicalMarketplaceURL(m.URL)
	if err != nil {
		return err
	}
	if err = validateMarketplaceSegment("instance_id", m.InstanceID); err != nil {
		return err
	}
	for i := range m.Collections {
		c := &m.Collections[i]
		if err = requiredCoordinate(c.SourceID, "collection", c.Name); err != nil {
			return err
		}
		if err = validateDigest(c.Revision, "collection revision"); err != nil {
			return err
		}
		for j := range c.Members {
			e := &c.Members[j]
			if err = requiredCoordinate(e.SourceID, e.Kind, e.Name); err != nil {
				return err
			}
			if err = validateDigest(e.Revision, "member revision"); err != nil {
				return err
			}
		}
		sortEdges(c.Members)
		c.Members = dedupeEdges(c.Members)
	}
	sort.Slice(m.Collections, func(i, j int) bool { return collectionKey(m.Collections[i]) < collectionKey(m.Collections[j]) })
	m.Collections, err = dedupeComparable(m.Collections, func(c MarketplaceCollection) string { return c.SourceID + "\x00" + c.Name }, "Marketplace collection")
	if err != nil {
		return err
	}
	for i := range m.Resources {
		r := &m.Resources[i]
		if err = requiredCoordinate(r.SourceID, r.Kind, r.Name); err != nil {
			return err
		}
		if r.Kind == "collection" {
			return coded(CodeInvalidDocument, "collection must be in collections")
		}
		if err = validateDigest(r.Revision, "resource revision"); err != nil {
			return err
		}
		switch r.Acquisition {
		case AcquisitionMirror:
			if err = validateDigest(r.ArchiveDigest, "archive_digest"); err != nil {
				return err
			}
			if !r.SourceCommit.IsZero() || r.SourceManifestDigest != "" {
				return coded(CodeInvalidDocument, "mirror resource has source-occurrence fields")
			}
		case AcquisitionIndexOnly:
			if r.SourceCommit.Algorithm() != "sha1" {
				return coded(CodeUnsupportedGitOID, "index-only source_commit must use sha1")
			}
			if _, err = ParseGitOID(r.SourceCommit.String()); err != nil {
				return err
			}
			if err = validateDigest(r.SourceManifestDigest, "source_manifest_digest"); err != nil {
				return err
			}
			if r.ArchiveDigest != "" {
				return coded(CodeInvalidDocument, "index-only resource has archive_digest")
			}
		default:
			return coded(CodeInvalidDocument, "invalid acquisition %q", r.Acquisition)
		}
		if !r.Direct && len(r.SelectedBy) == 0 {
			return coded(CodeInvalidDocument, "resource %s is neither direct nor selected by a collection", r.Name)
		}
		for j := range r.SelectedBy {
			e := &r.SelectedBy[j]
			if e.Kind != "" && e.Kind != "collection" {
				return coded(CodeInvalidDocument, "selected_by edge kind must be collection")
			}
			e.Kind = ""
			if err = requiredCoordinate(e.SourceID, "collection", e.Name); err != nil {
				return err
			}
			if err = validateDigest(e.Revision, "selected_by revision"); err != nil {
				return err
			}
		}
		sortEdges(r.SelectedBy)
		r.SelectedBy = dedupeEdges(r.SelectedBy)
	}
	sort.Slice(m.Resources, func(i, j int) bool { return resourceKey(m.Resources[i]) < resourceKey(m.Resources[j]) })
	m.Resources, err = dedupeComparable(m.Resources, func(r MarketplaceResource) string { return r.SourceID + "\x00" + r.Kind + "\x00" + r.Name }, "Marketplace resource")
	if err != nil {
		return err
	}
	return validateCollectionGraph(*m)
}

func validateMarketplaceSegment(label, value string) error {
	if err := api.ValidateMarketplaceSegment(label, value); err != nil {
		return coded(CodeInvalidDocument, "%s", err)
	}
	return nil
}

func requiredCoordinate(source, kind, name string) error {
	if err := validateMarketplaceSegment("source_id", source); err != nil {
		return err
	}
	if !api.IsMarketplaceKind(kind) {
		return coded(CodeInvalidDocument, "unsupported Marketplace kind %q", kind)
	}
	if err := validateMarketplaceSegment("name", name); err != nil {
		return err
	}
	return nil
}
func edgeKey(e CollectionEdge) string {
	return e.SourceID + "\x00" + e.Kind + "\x00" + e.Name + "\x00" + e.Revision.String()
}
func collectionKey(c MarketplaceCollection) string {
	return c.SourceID + "\x00" + c.Name + "\x00" + c.Revision.String()
}
func resourceKey(r MarketplaceResource) string {
	return r.SourceID + "\x00" + r.Kind + "\x00" + r.Name + "\x00" + r.Revision.String()
}
func sortEdges(v []CollectionEdge) {
	sort.Slice(v, func(i, j int) bool { return edgeKey(v[i]) < edgeKey(v[j]) })
}
func dedupeEdges(v []CollectionEdge) []CollectionEdge {
	if len(v) < 2 {
		return v
	}
	out := v[:1]
	for _, e := range v[1:] {
		if edgeKey(e) != edgeKey(out[len(out)-1]) {
			out = append(out, e)
		}
	}
	return out
}

const (
	maxCollectionGraphEdges     = 100000
	maxCollectionExpansionSteps = 250000
	maxCollectionGraphDepth     = 100
)

type collectionClosure struct {
	leaves map[string]struct{}
}

func validateCollectionGraph(m Marketplace) error {
	collections := make(map[string]MarketplaceCollection, len(m.Collections))
	coordinateRevision := make(map[string]Digest, len(m.Collections)+len(m.Resources))
	graphEdges := 0
	for _, c := range m.Collections {
		key := collectionKey(c)
		collections[key] = c
		graphEdges += len(c.Members)
		if graphEdges > maxCollectionGraphEdges {
			return coded(CodeInvalidDocument, "Marketplace collection graph exceeds edge limit")
		}
		coord := c.SourceID + "\x00collection\x00" + c.Name
		if old, ok := coordinateRevision[coord]; ok && old != c.Revision {
			return coded(CodeIdentityConflict, "collection coordinate has conflicting revisions")
		}
		coordinateRevision[coord] = c.Revision
	}
	resources := make(map[string]MarketplaceResource, len(m.Resources))
	for _, r := range m.Resources {
		coord := r.SourceID + "\x00" + r.Kind + "\x00" + r.Name
		if old, ok := coordinateRevision[coord]; ok && old != r.Revision {
			return coded(CodeIdentityConflict, "resource coordinate has conflicting revisions")
		}
		coordinateRevision[coord] = r.Revision
		resources[resourceKey(r)] = r
	}
	closures, err := buildCollectionClosures(collections, resources)
	if err != nil {
		return err
	}
	collectionKeys := make([]string, 0, len(collections))
	for key := range collections {
		collectionKeys = append(collectionKeys, key)
	}
	sort.Strings(collectionKeys)
	reachable := make(map[string]bool, len(collections))
	actualSelected := make(map[string]map[string]struct{})
	for _, rootKey := range collectionKeys {
		root := collections[rootKey]
		if !root.Direct {
			continue
		}
		rootKey = collectionKey(root)
		queue := []string{rootKey}
		reachable[rootKey] = true
		for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
			key := queue[queueIndex]
			for _, edge := range collections[key].Members {
				if edge.Kind != "collection" {
					continue
				}
				childKey := edge.SourceID + "\x00" + edge.Name + "\x00" + edge.Revision.String()
				if !reachable[childKey] {
					reachable[childKey] = true
					queue = append(queue, childKey)
				}
			}
		}
		for leaf := range closures[rootKey].leaves {
			if actualSelected[leaf] == nil {
				actualSelected[leaf] = make(map[string]struct{})
			}
			actualSelected[leaf][rootKey] = struct{}{}
		}
	}
	for _, key := range collectionKeys {
		c := collections[key]
		if !c.Direct && !reachable[key] {
			return coded(CodeInvalidDocument, "non-direct collection %s is unreachable", c.Name)
		}
	}
	resourceKeys := make([]string, 0, len(resources))
	for key := range resources {
		resourceKeys = append(resourceKeys, key)
	}
	sort.Strings(resourceKeys)
	for _, key := range resourceKeys {
		r := resources[key]
		expected := make(map[string]struct{}, len(r.SelectedBy))
		for _, edge := range r.SelectedBy {
			collectionKey := edge.SourceID + "\x00" + edge.Name + "\x00" + edge.Revision.String()
			collection, ok := collections[collectionKey]
			if !ok {
				return coded(CodeInvalidDocument, "selected_by references missing collection")
			}
			if !collection.Direct {
				return coded(CodeInvalidDocument, "selected_by must reference a direct collection root")
			}
			expected[collectionKey] = struct{}{}
		}
		actual := actualSelected[key]
		if len(actual) != len(expected) {
			return coded(CodeInvalidDocument, "resource %s selected_by closure mismatch", r.Name)
		}
		for root := range actual {
			if _, ok := expected[root]; !ok {
				return coded(CodeInvalidDocument, "resource %s selected_by closure mismatch", r.Name)
			}
		}
	}
	return nil
}

func buildCollectionClosures(collections map[string]MarketplaceCollection, resources map[string]MarketplaceResource) (map[string]collectionClosure, error) {
	closures := make(map[string]collectionClosure, len(collections))
	state := make(map[string]uint8, len(collections))
	type frame struct {
		key  string
		next int
	}
	steps := 0
	keys := make([]string, 0, len(collections))
	for key := range collections {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, rootKey := range keys {
		if state[rootKey] == 2 {
			continue
		}
		state[rootKey] = 1
		closures[rootKey] = collectionClosure{leaves: make(map[string]struct{})}
		stack := []frame{{key: rootKey}}
		for len(stack) > 0 {
			if len(stack) > maxCollectionGraphDepth {
				return nil, coded(CodeInvalidDocument, "Marketplace collection graph exceeds depth limit")
			}
			index := len(stack) - 1
			current := &stack[index]
			collection := collections[current.key]
			if current.next == len(collection.Members) {
				finishedKey := current.key
				state[finishedKey] = 2
				stack = stack[:index]
				if index > 0 {
					parentKey := stack[index-1].key
					steps += mergeCollectionLeaves(closures[parentKey].leaves, closures[finishedKey].leaves)
					if steps > maxCollectionExpansionSteps {
						return nil, coded(CodeInvalidDocument, "Marketplace collection graph exceeds expansion limit")
					}
				}
				continue
			}
			edge := collection.Members[current.next]
			current.next++
			if edge.Kind == "collection" {
				childKey := edge.SourceID + "\x00" + edge.Name + "\x00" + edge.Revision.String()
				if _, ok := collections[childKey]; !ok {
					return nil, coded(CodeInvalidDocument, "collection member references missing collection")
				}
				switch state[childKey] {
				case 1:
					return nil, coded(CodeInvalidDocument, "collection cycle at %s", edge.Name)
				case 0:
					state[childKey] = 1
					closures[childKey] = collectionClosure{leaves: make(map[string]struct{})}
					stack = append(stack, frame{key: childKey})
				default:
					steps += mergeCollectionLeaves(closures[current.key].leaves, closures[childKey].leaves)
					if steps > maxCollectionExpansionSteps {
						return nil, coded(CodeInvalidDocument, "Marketplace collection graph exceeds expansion limit")
					}
				}
				continue
			}
			resourceKey := edge.SourceID + "\x00" + edge.Kind + "\x00" + edge.Name + "\x00" + edge.Revision.String()
			if _, ok := resources[resourceKey]; !ok {
				return nil, coded(CodeInvalidDocument, "collection member references missing resource")
			}
			if _, ok := closures[current.key].leaves[resourceKey]; !ok {
				closures[current.key].leaves[resourceKey] = struct{}{}
				steps++
				if steps > maxCollectionExpansionSteps {
					return nil, coded(CodeInvalidDocument, "Marketplace collection graph exceeds expansion limit")
				}
			}
		}
	}
	return closures, nil
}

func mergeCollectionLeaves(dst, src map[string]struct{}) int {
	added := 0
	for key := range src {
		if _, ok := dst[key]; ok {
			continue
		}
		dst[key] = struct{}{}
		added++
	}
	return added
}
