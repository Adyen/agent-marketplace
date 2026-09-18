package lockfile

import "reflect"

func cloneDocument(in Document) Document {
	out := in
	out.Registries = append([]GitRegistry(nil), in.Registries...)
	for i := range out.Registries {
		out.Registries[i].Roots = append([]GitRoot(nil), in.Registries[i].Roots...)
		for j := range out.Registries[i].Roots {
			out.Registries[i].Roots[j].Resources = append([]GitResource(nil), in.Registries[i].Roots[j].Resources...)
			for k := range out.Registries[i].Roots[j].Resources {
				source := in.Registries[i].Roots[j].Resources[k].SupportFiles
				if len(source) == 0 {
					out.Registries[i].Roots[j].Resources[k].SupportFiles = nil
					continue
				}
				out.Registries[i].Roots[j].Resources[k].SupportFiles = make(map[string]Digest, len(source))
				for name, digest := range source {
					out.Registries[i].Roots[j].Resources[k].SupportFiles[name] = digest
				}
			}
		}
	}
	out.Marketplaces = append([]MarketplaceLock(nil), in.Marketplaces...)
	for i := range out.Marketplaces {
		out.Marketplaces[i].RegistryBindings = append([]MarketplaceRegistryBinding(nil), in.Marketplaces[i].RegistryBindings...)
		out.Marketplaces[i].Resources = append([]MarketplaceResourceLock(nil), in.Marketplaces[i].Resources...)
		for j := range out.Marketplaces[i].Resources {
			out.Marketplaces[i].Resources[j].SelectedBy = append([]MarketplaceSelectionLock(nil), in.Marketplaces[i].Resources[j].SelectedBy...)
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
