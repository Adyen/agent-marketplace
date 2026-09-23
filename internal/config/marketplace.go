package config

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
	"gopkg.in/yaml.v3"
)

type MarketplaceExclusion struct {
	URL     string `yaml:"-"`
	Locator string `yaml:"-"`
	Source  Source `yaml:"-"`
}

// Matches reports whether the exclusion suppresses pin. A user exclusion
// overrides an optional organization pin; any exclusion also removes a
// collection-selected member of its own layer's selection, because an exclude
// takes precedence over the collection include. Required entries are immune.
func (x MarketplaceExclusion) Matches(entry MarketplaceEntry, pin MarketplaceResourcePin) bool {
	if entry.Required {
		return false
	}
	organizationOverride := x.Source == UserPersonal && entry.Source == Organization
	ownCollectionMember := x.Source == entry.Source && !pin.Direct
	if !organizationOverride && !ownCollectionMember {
		return false
	}
	if x.URL != entry.URL {
		canonical, err := NormalizeMarketplaceURL(entry.URL)
		if err != nil || x.URL != canonical {
			return false
		}
	}
	kind := pin.Kind
	if kind == api.KindMcpServer {
		kind = "mcp"
	}
	return x.Locator == pin.RegistrySlug+"/"+kind+"/"+pin.Name
}

// MarketplacePinExcluded reports whether any configured exclusion suppresses
// pin of entry.
func (c *Config) MarketplacePinExcluded(entry MarketplaceEntry, pin MarketplaceResourcePin) bool {
	if c == nil {
		return false
	}
	for _, exclusion := range c.MarketplaceExclusions {
		if exclusion.Matches(entry, pin) {
			return true
		}
	}
	return false
}

// MarketplaceEntry is one configured marketplace endpoint and optional exact pins.
type MarketplaceEntry struct {
	URL        string                   `yaml:"url"`
	InstanceID string                   `yaml:"instance_id"`
	Alias      string                   `yaml:"alias,omitempty"`
	Resources  []MarketplaceResourcePin `yaml:"resources,omitempty"`
	Source     Source                   `yaml:"-"`
	Required   bool                     `yaml:"-"`
}

// MarketplaceResourcePin is an exact Marketplace resource coordinate and digest.
// MarketplaceSelection records an exact collection revision that keeps a leaf
// selected. Its absence on a leaf represents a direct user selection.
type MarketplaceSelection struct {
	RegistryID string `yaml:"registry_id"`
	Name       string `yaml:"name"`
	Revision   string `yaml:"revision"`
}

// MarketplaceSelections is a comparable, YAML-sequence-backed value. Keeping
// its storage scalar preserves compatibility with callers comparing pins while
// still emitting the readable selected_by sequence on disk.
type MarketplaceSelections string

func MarketplaceSelectionsFrom(values []MarketplaceSelection) MarketplaceSelections {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.RegistryID+"\x00"+value.Name+"\x00"+value.Revision)
	}
	return MarketplaceSelections(strings.Join(parts, "\n"))
}

func (s MarketplaceSelections) List() []MarketplaceSelection {
	if s == "" {
		return nil
	}
	var values []MarketplaceSelection
	for _, part := range strings.Split(string(s), "\n") {
		fields := strings.Split(part, "\x00")
		if len(fields) == 3 {
			values = append(values, MarketplaceSelection{RegistryID: fields[0], Name: fields[1], Revision: fields[2]})
		}
	}
	return values
}

func validateMarketplaceSelections(selections MarketplaceSelections) error {
	if selections == "" {
		return nil
	}
	for index, part := range strings.Split(string(selections), "\n") {
		fields := strings.Split(part, "\x00")
		if len(fields) != 3 {
			return fmt.Errorf("selected_by[%d] is malformed", index)
		}
		if err := validateMarketplaceSelection(MarketplaceSelection{RegistryID: fields[0], Name: fields[1], Revision: fields[2]}); err != nil {
			return fmt.Errorf("selected_by[%d]: %w", index, err)
		}
	}
	return nil
}

func (s MarketplaceSelections) MarshalYAML() (any, error) { return s.List(), nil }

func (s *MarketplaceSelections) UnmarshalYAML(node *yaml.Node) error {
	if err := validateMarketplaceSelectedByNode(node, "selected_by"); err != nil {
		return err
	}
	var values []MarketplaceSelection
	if err := node.Decode(&values); err != nil {
		return err
	}
	*s = MarketplaceSelectionsFrom(values)
	return nil
}

type MarketplaceResourcePin struct {
	RegistryID             string                `yaml:"registry_id"`
	RegistrySlug           string                `yaml:"registry_slug,omitempty"`
	Kind                   string                `yaml:"kind"`
	Name                   string                `yaml:"name"`
	Revision               string                `yaml:"revision"`
	Digest                 string                `yaml:"digest"`                // "sha256:<64 lowercase hex>"
	Acquisition            string                `yaml:"acquisition,omitempty"` // "mirror" or "index-only"
	RegistryCommit         string                `yaml:"registry_commit,omitempty"`
	RegistryManifestDigest string                `yaml:"registry_manifest_digest,omitempty"`
	InstalledName          string                `yaml:"installed_name,omitempty"`
	Direct                 bool                  `yaml:"direct,omitempty"`
	SelectedBy             MarketplaceSelections `yaml:"selected_by,omitempty"`

	// Runtime-only integrity fields hydrated from the owning acquisition lock.
	// They are never serialized to user-facing configuration.
	LockArchiveDigest            string `yaml:"-"`
	LockCollectionManifestDigest string `yaml:"-"`
	LockSourcePath               string `yaml:"-"`
}

const (
	MarketplaceAcquisitionMirror    = "mirror"
	MarketplaceAcquisitionIndexOnly = "index-only"
)

// validateAllowedMarketplacePatterns validates that every allowlist entry is a
// valid regular expression.
func validateAllowedMarketplacePatterns(patterns *[]string) []error {
	if patterns == nil {
		return nil
	}
	var errs []error
	for i, p := range *patterns {
		if _, err := regexp.Compile(p); err != nil {
			errs = append(errs, fmt.Errorf("allowed_marketplaces[%d] is not a valid regex: %w", i, err))
		}
	}
	return errs
}

func MarketplaceCatalogKey(entry MarketplaceEntry, pin MarketplaceResourcePin) string {
	endpoint := entry.URL
	if normalized, err := NormalizeMarketplaceURL(endpoint); err == nil {
		endpoint = normalized
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = MarketplaceAcquisitionMirror
	}
	return "marketplace:" + strings.Join([]string{endpoint, entry.InstanceID, pin.RegistryID, pin.Kind, pin.Name, pin.Digest, pin.Revision, acquisition, pin.RegistryCommit, pin.RegistryManifestDigest}, "\x00")
}

// ValidateMarketplaceEntries validates a slice of marketplace entries.
func ValidateMarketplaceEntries(entries []MarketplaceEntry) []error {
	type aliasUse struct {
		index      int
		url        string
		instanceID string
		source     Source
		valid      bool
	}
	var errs []error
	aliases := make(map[string]aliasUse)
	for i, e := range entries {
		errs = append(errs, validateMarketplaceEntry(i, e)...)
		if e.Alias == "" {
			continue
		}
		normalizedURL, normalizeErr := urlutil.NormalizeEndpointURL(e.URL)
		current := aliasUse{index: i, url: normalizedURL, instanceID: e.InstanceID, source: e.Source, valid: normalizeErr == nil}
		if previous, ok := aliases[e.Alias]; ok {
			if previous.valid && current.valid && previous.source != SourceUnset && current.source != SourceUnset && previous.source != current.source && previous.url == current.url && previous.instanceID == current.instanceID {
				continue
			}
			errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q duplicates marketplaces[%d].alias", i, e.Alias, previous.index))
			continue
		}
		aliases[e.Alias] = current
	}
	return errs
}

func validateMarketplaceEntry(i int, e MarketplaceEntry) []error {
	var errs []error
	if strings.TrimSpace(e.URL) == "" {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url is required", i))
	} else if _, err := urlutil.NormalizeEndpointURL(e.URL); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].url: %w", i, err))
	}
	if err := validateMarketplaceSegment("instance_id", e.InstanceID); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].%w", i, err))
	}
	if e.Alias != "" {
		if err := validateMarketplaceSegment("alias", e.Alias); err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d].%w", i, err))
		} else if e.Alias == "all" {
			errs = append(errs, fmt.Errorf("marketplaces[%d].alias %q is reserved for federated search", i, e.Alias))
		}
	}
	for j, p := range e.Resources {
		errs = append(errs, validateMarketplacePin(i, j, p)...)
	}
	return errs
}

func validateMarketplacePin(entryIdx, pinIdx int, p MarketplaceResourcePin) []error {
	var errs []error
	if err := validateMarketplaceSegment("registry_id", p.RegistryID); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if p.RegistrySlug != "" {
		if err := validateMarketplaceSegment("registry_slug", p.RegistrySlug); err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
		}
	}
	if !api.IsMarketplaceKind(p.Kind) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].kind %q is not a valid marketplace kind", entryIdx, pinIdx, p.Kind))
	}
	if err := validateMarketplaceSegment("name", p.Name); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if err := validateMarketplaceSegment("revision", p.Revision); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if !api.IsCanonicalSHA256Digest(p.Digest) {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].digest must be sha256:<64-hex>", entryIdx, pinIdx))
	}
	if p.Acquisition != "" && p.Acquisition != MarketplaceAcquisitionMirror && p.Acquisition != MarketplaceAcquisitionIndexOnly {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].acquisition must be %q or %q", entryIdx, pinIdx, MarketplaceAcquisitionMirror, MarketplaceAcquisitionIndexOnly))
	}
	if err := validateMarketplaceOccurrenceFields(p.Acquisition, p.RegistryCommit, p.RegistryManifestDigest); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	if err := ValidateMarketplaceInstalledName(p.InstalledName); err != nil {
		errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].%w", entryIdx, pinIdx, err))
	}
	for selectorIndex, selector := range p.SelectedBy.List() {
		if err := validateMarketplaceSelection(selector); err != nil {
			errs = append(errs, fmt.Errorf("marketplaces[%d].resources[%d].selected_by[%d].%w", entryIdx, pinIdx, selectorIndex, err))
		}
	}
	return errs
}

func validateMarketplaceSelection(selection MarketplaceSelection) error {
	if err := validateMarketplaceSegment("selected_by registry_id", selection.RegistryID); err != nil {
		return err
	}
	if err := validateMarketplaceSegment("selected_by name", selection.Name); err != nil {
		return err
	}
	if err := validateMarketplaceSegment("selected_by revision", selection.Revision); err != nil {
		return err
	}
	if !api.IsCanonicalSHA256Digest(selection.Revision) {
		return fmt.Errorf("selected_by revision must be sha256:<64-hex>")
	}
	return nil
}

func validateMarketplaceOccurrenceFields(acquisition, registryCommit, registryManifestDigest string) error {
	if acquisition == MarketplaceAcquisitionIndexOnly {
		if !api.IsCanonicalGitCommit(registryCommit) {
			return fmt.Errorf("registry_commit must be a full lowercase Git commit")
		}
		if !api.IsCanonicalSHA256Digest(registryManifestDigest) {
			return fmt.Errorf("registry_manifest_digest must be sha256:<64-hex>")
		}
		return nil
	}
	if registryCommit != "" || registryManifestDigest != "" {
		// These fields are the immutable occurrence pin for index-only
		// acquisition. Do not accept them on a mirror pin, where they would be
		// silently ignored and could mislead an editor or later sync.
		return fmt.Errorf("registry_commit and registry_manifest_digest require acquisition %q", MarketplaceAcquisitionIndexOnly)
	}
	return nil
}

// IsMarketplaceKind reports whether kind is a selectable Marketplace resource kind.
// Kept in config as a thin wrapper so consumers do not need to import api.
func IsMarketplaceKind(kind string) bool {
	return api.IsMarketplaceKind(kind)
}
