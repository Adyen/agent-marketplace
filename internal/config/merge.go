package config

import (
	"fmt"
	"strings"
)

// MergeLayers merges an ordered slice of Layers into a single Config.
// Layers are processed in the order given; each layer's registries are
// appended after the previous layer's, with the layer's Source stamped onto
// each resulting RegistryEntry.
func MergeLayers(layers []Layer) *Config {
	merged := &Config{}

	for _, layer := range layers {
		if layer.Config == nil {
			continue
		}

		for _, entry := range layer.Config.Registries {
			resolved := resolveRegistryEntry(layer.Config, entry)
			resolved.Source = layer.Source
			merged.Registries = append(merged.Registries, resolved)
		}

		// Collections are cumulative across layers (include from all layers
		// is unioned; exclude from higher-precedence layers overrides, but
		// only when the layer explicitly sets excludes — nil means "no opinion"
		// and must not clear earlier excludes).
		merged.Collections.Include = appendUniqueStrings(merged.Collections.Include, layer.Config.Collections.Include)
		if layer.Config.Collections.Exclude != nil {
			merged.Collections.Exclude = appendUniqueStrings(merged.Collections.Exclude, layer.Config.Collections.Exclude)
		}

		// Inherit scalar/override fields from higher-precedence layers.
		// Later layers (higher precedence) override earlier ones.
		// Note: top-level selectors/tags are not merged cumulatively; they act
		// as defaults for each registry entry in the same layer.
		if layer.Config.APIVersion != "" {
			merged.APIVersion = layer.Config.APIVersion
		}
		if layer.Config.Harnesses != nil {
			merged.Harnesses = layer.Config.Harnesses
		}
		// Preserve the highest-precedence project-facing setting for callers
		// that display the merged config. Execution uses
		// ResolveMaterializationModes so this value never affects machine scope.
		if layer.Config.Materialization != nil && (layer.Source == UserPersonal || layer.Source == RepoCommitted || layer.Source == RepoPersonal) {
			merged.Materialization = &MaterializationConfig{Mode: layer.Config.Materialization.Mode}
		}
		// AllowedRegistries uses first-non-nil-wins semantics so that
		// Organization policy cannot be relaxed by higher-precedence layers.
		// An explicit empty slice (allowed_registries: []) is non-nil and
		// therefore "claims" the field — but only if no earlier layer did.
		if merged.AllowedRegistries == nil && layer.Config.AllowedRegistries != nil {
			merged.AllowedRegistries = layer.Config.AllowedRegistries
		}
		// Backup config controls machine-scope user state, so repo-scoped
		// config must not change it.
		if layer.Config.Backup != nil && layer.Source.CanConfigureBackup() {
			if merged.Backup == nil {
				merged.Backup = &BackupConfig{}
			}
			if layer.Config.Backup.Enabled != nil {
				merged.Backup.Enabled = layer.Config.Backup.Enabled
			}
			if layer.Config.Backup.MaxDays != nil {
				merged.Backup.MaxDays = layer.Config.Backup.MaxDays
			}
			if layer.Config.Backup.MaxPerDay != nil {
				merged.Backup.MaxPerDay = layer.Config.Backup.MaxPerDay
			}
		}
		// Logging config is user-level only, so repo-scoped config must not
		// change it.
		if layer.Config.Logging != nil && layer.Source.CanConfigureLogging() {
			if merged.Logging == nil {
				merged.Logging = &LoggingConfig{}
			}
			if layer.Config.Logging.Enabled != nil {
				merged.Logging.Enabled = layer.Config.Logging.Enabled
			}
			if layer.Config.Logging.Level != nil {
				merged.Logging.Level = layer.Config.Logging.Level
			}
			if layer.Config.Logging.File != nil {
				merged.Logging.File = layer.Config.Logging.File
			}
			if layer.Config.Logging.MaxSizeMB != nil {
				merged.Logging.MaxSizeMB = layer.Config.Logging.MaxSizeMB
			}
			if layer.Config.Logging.MaxBackups != nil {
				merged.Logging.MaxBackups = layer.Config.Logging.MaxBackups
			}
			if layer.Config.Logging.MaxAgeDays != nil {
				merged.Logging.MaxAgeDays = layer.Config.Logging.MaxAgeDays
			}
			if layer.Config.Logging.Compress != nil {
				merged.Logging.Compress = layer.Config.Logging.Compress
			}
		}
	}

	// Backfill Harnesses on entries that don't have their own, using the
	// merged top-level Harnesses. This legacy merge behavior is retained for
	// callers that do not provide a separate machine-wide target set. The v3
	// effective loader strips machine target fields before invoking MergeLayers.
	if len(merged.Harnesses) > 0 {
		for i := range merged.Registries {
			if merged.Registries[i].Harnesses == nil {
				merged.Registries[i].Harnesses = append([]string(nil), merged.Harnesses...)
			}
		}
	}

	return merged
}

// LocalConfigAsLayer converts a LocalConfig into a merge-ready Layer with
// RepoPersonal source. When the LocalConfig is nil or has no registries,
// it returns nil so callers can pass the result to MergeLayers without
// guards. Registries and the project-only Materialization setting are carried
// into the layer; top-level kind selectors are NOT copied because they are already applied globally
// by the resolver's localCfg path (applyLocalResources / applyPersonalExcludes).
// Copying them here would cause double-application: once as per-entry
// inheritance defaults (surprising) and once as global post-hoc filters
// (expected). For per-registry selectors inside a registries entry, the
// RegistryEntry.SelectorForKind fields are already populated from YAML
// and flow through merge/resolve normally.
func LocalConfigAsLayer(localCfg *LocalConfig) *Layer {
	if localCfg == nil || (len(localCfg.Registries) == 0 && localCfg.Materialization == nil) {
		return nil
	}

	cfg := Config{
		APIVersion:      APIVersionV2,
		Registries:      localCfg.Registries,
		Materialization: localCfg.Materialization,
	}

	return &Layer{Source: RepoPersonal, Config: &cfg}
}

// Merge is a backwards-compatible wrapper around MergeLayers.
// global is treated as UserPersonal scope; repo as RepoCommitted scope.
func Merge(global, repo *Config) *Config {
	return MergeLayers([]Layer{
		{Source: UserPersonal, Config: global},
		{Source: RepoCommitted, Config: repo},
	})
}

func (s Source) CanConfigureBackup() bool {
	return s == Organization || s == UserPersonal
}

func (s Source) CanConfigureLogging() bool {
	return s == Organization || s == UserPersonal
}

func resolveRegistryEntry(cfg *Config, entry RegistryEntry) RegistryEntry {
	resolved := RegistryEntry{
		URL:           entry.URL,
		Ref:           entry.Ref,
		Path:          entry.Path,
		Required:      entry.Required,
		Default:       entry.Default,
		SourceHarness: entry.SourceHarness,
		Tags:          resolveInheritedList(cfg.Tags, entry.Tags),
		Harnesses:     resolveInheritedList(cfg.Harnesses, entry.Harnesses),
	}
	// Resolve per-kind selectors with top-level inheritance (entry-specific wins).
	for _, kind := range AllKinds {
		if ptr := resolved.SelectorForKindPtr(kind); ptr != nil {
			*ptr = resolveInheritedSelector(cfg.SelectorForKind(kind), entry.SelectorForKind(kind))
		}
	}
	// Propagate entry-level ExcludeAll sugar to all per-kind selectors.
	if entry.ExcludeAll {
		for _, kind := range AllKinds {
			if ptr := resolved.SelectorForKindPtr(kind); ptr != nil {
				ptr.ExcludeAll = true
			}
		}
	}
	return resolved
}

func resolveInheritedSelector(defaults, overrides KindSelector) KindSelector {
	return KindSelector{
		Include:    resolveInheritedList(defaults.Include, overrides.Include),
		Exclude:    resolveInheritedList(defaults.Exclude, overrides.Exclude),
		ExcludeAll: defaults.ExcludeAll || overrides.ExcludeAll,
	}
}

func resolveInheritedList(defaults, overrides []string) []string {
	if overrides != nil {
		if len(overrides) == 0 {
			// Preserve explicit empty overrides (non-nil) so callers can
			// distinguish them from "unset" (nil) and avoid backfilling.
			return []string{}
		}
		return append([]string(nil), overrides...)
	}

	if defaults == nil {
		return nil
	}
	if len(defaults) == 0 {
		// Preserve explicit empty defaults (non-nil).
		return []string{}
	}

	return append([]string(nil), defaults...)
}

// HasMatchingTag checks whether any tag in wantedTags matches a tag in skillTags.
// Exported for use by the resolver package.
func HasMatchingTag(skillTags, wantedTags []string) bool {
	if len(skillTags) == 0 || len(wantedTags) == 0 {
		return false
	}

	for _, wanted := range wantedTags {
		wanted = strings.TrimSpace(wanted)
		if wanted == "" {
			continue
		}

		for _, tag := range skillTags {
			if strings.TrimSpace(tag) == wanted {
				return true
			}
		}
	}

	return false
}

// RegistrySelectionKey returns a unique key for a registry entry.
// Exported for use by the resolver package.
func RegistrySelectionKey(entry RegistryEntry, index int) string {
	if entry.IsLocal() {
		return fmt.Sprintf("%s#%d", entry.URL, index)
	}
	return fmt.Sprintf("%s@%s#%d", entry.URL, entry.Ref, index)
}

// NormalizeGitURL normalizes a git remote URL for comparison.
// Strips trailing ".git" suffix and trailing slashes so that equivalent URLs
// (e.g. "git@host:repo" and "git@host:repo.git") are treated as the same.
func NormalizeGitURL(url string) string {
	u := strings.TrimRight(url, "/")
	u = strings.TrimSuffix(u, ".git")
	return u
}

// appendUniqueStrings appends strings from src to dst, skipping duplicates.
func appendUniqueStrings(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, v := range dst {
		seen[v] = struct{}{}
	}
	for _, v := range src {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}
