package lockfile

import (
	"reflect"
	"sort"
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
	if err = require(m.InstanceID, "instance_id"); err != nil {
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

func requiredCoordinate(source, kind, name string) error {
	if source == "" || kind == "" || name == "" {
		return coded(CodeInvalidDocument, "incomplete Marketplace coordinate")
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

func validateCollectionGraph(m Marketplace) error {
	collections := make(map[string]MarketplaceCollection, len(m.Collections))
	coordinateRevision := map[string]Digest{}
	for _, c := range m.Collections {
		k := collectionKey(c)
		collections[k] = c
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
	cycleState := map[string]uint8{}
	for _, collection := range m.Collections {
		if err := detectCollectionCycle(collection, collections, cycleState); err != nil {
			return err
		}
	}
	reachable := map[string]bool{}
	for _, root := range m.Collections {
		if !root.Direct {
			continue
		}
		leaves := map[string]bool{}
		visiting := map[string]bool{}
		if err := expandCollection(root, collections, resources, visiting, reachable, leaves); err != nil {
			return err
		}
		rootEdge := CollectionEdge{SourceID: root.SourceID, Name: root.Name, Revision: root.Revision}
		for key, r := range resources {
			selected := false
			for _, edge := range r.SelectedBy {
				if edge.SourceID == rootEdge.SourceID && edge.Name == rootEdge.Name && edge.Revision == rootEdge.Revision {
					selected = true
					break
				}
			}
			if selected != leaves[key] {
				return coded(CodeInvalidDocument, "collection %s selected_by closure mismatch for %s", root.Name, r.Name)
			}
		}
	}
	for _, c := range m.Collections {
		if !c.Direct && !reachable[collectionKey(c)] {
			return coded(CodeInvalidDocument, "non-direct collection %s is unreachable", c.Name)
		}
	}
	for _, r := range m.Resources {
		for _, edge := range r.SelectedBy {
			collection, ok := collections[edge.SourceID+"\x00"+edge.Name+"\x00"+edge.Revision.String()]
			if !ok {
				return coded(CodeInvalidDocument, "selected_by references missing collection")
			}
			if !collection.Direct {
				return coded(CodeInvalidDocument, "selected_by must reference a direct collection root")
			}
		}
	}
	return nil
}

func detectCollectionCycle(collection MarketplaceCollection, collections map[string]MarketplaceCollection, state map[string]uint8) error {
	key := collectionKey(collection)
	if state[key] == 1 {
		return coded(CodeInvalidDocument, "collection cycle at %s", collection.Name)
	}
	if state[key] == 2 {
		return nil
	}
	state[key] = 1
	for _, edge := range collection.Members {
		if edge.Kind != "collection" {
			continue
		}
		childKey := edge.SourceID + "\x00" + edge.Name + "\x00" + edge.Revision.String()
		child, ok := collections[childKey]
		if !ok {
			return coded(CodeInvalidDocument, "collection member references missing collection")
		}
		if err := detectCollectionCycle(child, collections, state); err != nil {
			return err
		}
	}
	state[key] = 2
	return nil
}

func expandCollection(c MarketplaceCollection, collections map[string]MarketplaceCollection, resources map[string]MarketplaceResource, visiting, reachable, leaves map[string]bool) error {
	key := collectionKey(c)
	if visiting[key] {
		return coded(CodeInvalidDocument, "collection cycle at %s", c.Name)
	}
	visiting[key] = true
	reachable[key] = true
	defer delete(visiting, key)
	for _, e := range c.Members {
		if e.Kind == "collection" {
			childKey := e.SourceID + "\x00" + e.Name + "\x00" + e.Revision.String()
			child, ok := collections[childKey]
			if !ok {
				return coded(CodeInvalidDocument, "collection member references missing collection")
			}
			if err := expandCollection(child, collections, resources, visiting, reachable, leaves); err != nil {
				return err
			}
			continue
		}
		rkey := e.SourceID + "\x00" + e.Kind + "\x00" + e.Name + "\x00" + e.Revision.String()
		if _, ok := resources[rkey]; !ok {
			return coded(CodeInvalidDocument, "collection member references missing resource")
		}
		leaves[rkey] = true
	}
	return nil
}
