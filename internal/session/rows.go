package session

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/materialize"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
)

// DriftAssessment reports whether the authoritative materialization
// assessment for the newest snapshot succeeded.
type DriftAssessment uint8

const (
	DriftUnknown DriftAssessment = iota
	DriftReady
	DriftFailed
)

// MarketplaceSelection is one pinned resource inside a configured
// Marketplace entry.
type MarketplaceSelection struct {
	Entry config.MarketplaceEntry
	Pin   config.MarketplaceResourcePin
}

// MarketplaceDiscovery is one Marketplace discovery item bound to its
// endpoint and cache state.
type MarketplaceDiscovery struct {
	Endpoint   config.MarketplaceEntry
	Item       mpapi.ResourceListItem
	CacheState marketplace.DiscoveryCacheState
	ObservedAt time.Time
}

// Row is the surface-neutral state for one resource line: its artifact,
// selection scope flags, drift, and search index. Rendering concerns
// (label memos, cell widths) are owned by the surface.
type Row struct {
	Artifact          resolver.ResolvedArtifact
	DetailSource      *DetailSourceSnapshot // immutable bounded copy captured while the exact snapshot lease is held
	IsAvailable       bool                  // not in resolved (available but not enabled)
	IsExcluded        bool                  // legacy: skill is in localCfg.Exclude (mirrors ExcludedLocally)
	IsMalformed       bool
	MalformedMsg      string
	MalformedKind     string
	UnavailableReason string
	UnavailableSoft   bool                   // absent from a loaded registry; a warning that does not block drift
	DriftItem         *materialize.DriftItem // nil if in sync or available-only

	// Per-scope enablement: which config file(s) enable this skill.
	// A skill may be enabled in multiple scopes simultaneously.
	EnabledGlobal       bool
	EnabledOrganization bool
	EnabledUser         bool
	EnabledProject      bool
	EnabledLocal        bool
	ExcludedLocally     bool
	ExcludedByUser      bool

	// Source registry of the skill. For available (non-enabled) skills, set
	// to the first registry whose catalog contained the skill, in cfg order.
	RegistryURL   string
	RegistryName  string // display-friendly form: last path segment, .git stripped
	RegistryPath  string // path subdirectory from the registry entry (for config writes)
	RegistryRef   string // ref from the registry entry (for config writes)
	Harnesses     []string
	HarnessSource string

	MarketplaceSelections     map[config.Source]MarketplaceSelection
	MarketplaceRegistrySlug   string
	MarketplaceCollectionOnly bool
	MarketplaceDiscovery      *MarketplaceDiscovery

	PendingVerification bool

	PluginSelectedGlobal  bool
	PluginSelectedProject bool
	PluginSelectedLocal   bool
	PluginMemberExcluded  bool

	// SearchWords is the cached, sorted and deduplicated set of lowercased
	// words from the name and metadata that query terms prefix-match.
	// Populated lazily by EnsureSearchable so filtering does not rebuild
	// it on every keystroke while the user is typing in the search input.
	SearchWords     []string
	SearchReady     bool
	SearchName      string
	SearchRanked    bool
	SearchNameMatch bool
	SearchScore     int
	// SearchMatchRunes holds rune indexes into the sanitized name that the
	// query genuinely matched; it is empty for metadata-only matches.
	SearchMatchRunes []int

	// LabelMemo is an opaque slot owned by the rendering surface (the TUI
	// stores its width-dependent label memo here). The session never reads
	// or interprets it.
	LabelMemo any
}

// CollectionNames returns each collection parent once.
func (r Row) CollectionNames() []string {
	var names []string
	addPath := func(path string) {
		for _, parent := range strings.Split(path, " > ") {
			parent = strings.TrimSpace(parent)
			if strings.HasPrefix(parent, "collection:") {
				names = append(names, collectionNameFromVia(parent))
			}
		}
	}
	addPath(r.Artifact.Via)
	for _, parent := range r.Artifact.CollectionParents {
		addPath(parent.Path)
	}
	for _, source := range []config.Source{config.Organization, config.UserPersonal, config.RepoCommitted, config.RepoPersonal} {
		selection, ok := r.MarketplaceSelections[source]
		if !ok {
			continue
		}
		for _, parent := range selection.Pin.SelectedBy.List() {
			names = append(names, parent.Name)
		}
	}
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	return unique
}

// SourceLabel is the display label for a source section: the registry display
// name (or URL-derived repository name) with the full normalized subpath, or
// "(local)" without registry metadata.
func SourceLabel(r Row) string {
	base := strings.TrimSpace(r.RegistryName)
	if base == "" {
		base = RegistryDisplayName(r.RegistryURL)
	}
	if base == "" {
		base = "(local)"
	}
	if p := config.NormalizeRegistryPath(r.RegistryPath); p != "" {
		base = base + " / " + p
	}
	return base
}

// EnsureSearchable populates the search index on first use and
// short-circuits on subsequent calls. Safe to call from any code path that
// is about to filter the row; BuildRows and test helpers can skip the call
// because the first filter pass will fill it in.
func (r *Row) EnsureSearchable() {
	if r.SearchReady {
		return
	}
	size := len(r.Artifact.Name) + len(r.Artifact.RegistryURL) + 2
	for _, t := range r.Artifact.Tags {
		size += len(t) + 1
	}
	var b strings.Builder
	b.Grow(size)
	b.WriteString(r.Artifact.Name)
	b.WriteByte(' ')
	for i, t := range r.Artifact.Tags {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
	}
	b.WriteByte(' ')
	b.WriteString(r.Artifact.RegistryURL)
	b.WriteByte(' ')
	b.WriteString(r.Artifact.Kind.String())
	b.WriteByte(' ')
	b.WriteString(skill.KindLabel(r.Artifact.Kind))
	b.WriteByte(' ')
	b.WriteString(SourceLabel(*r))
	b.WriteByte(' ')
	b.WriteString(r.Artifact.Description)
	if r.MarketplaceDiscovery != nil {
		b.WriteByte(' ')
		b.WriteString(MarketplaceEndpointDisplay(r.MarketplaceDiscovery.Endpoint))
		b.WriteByte(' ')
		b.WriteString(r.MarketplaceDiscovery.Item.RegistrySlug)
		b.WriteByte(' ')
		b.WriteString(string(r.MarketplaceDiscovery.Item.Availability))
		metadataKeys := make([]string, 0, len(r.MarketplaceDiscovery.Item.Metadata))
		for key := range r.MarketplaceDiscovery.Item.Metadata {
			metadataKeys = append(metadataKeys, key)
		}
		sort.Strings(metadataKeys)
		for _, key := range metadataKeys {
			b.WriteByte(' ')
			b.WriteString(key)
			b.WriteByte(' ')
			b.WriteString(r.MarketplaceDiscovery.Item.Metadata[key])
		}
	}
	r.SearchName = strings.ToLower(SanitizeTerminalData(r.Artifact.Name, TerminalSingleLine))
	words := SearchTerms(b.String())
	slices.Sort(words)
	r.SearchWords = slices.Compact(words)
	r.SearchReady = true
}

// SearchTerms splits text into lowercased words at every character that is
// neither a letter nor a digit. Queries and searchable metadata share it so
// "platform.git" matches the words of a registry URL.
func SearchTerms(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	})
}

// HasWordWithPrefix reports whether any of the row's search words begins
// with term.
func (r *Row) HasWordWithPrefix(term string) bool {
	i, _ := slices.BinarySearch(r.SearchWords, term)
	return i < len(r.SearchWords) && strings.HasPrefix(r.SearchWords[i], term)
}

// KindLabel is the row's kind label; a malformed row reports the kind its
// failed load was for.
func (r Row) KindLabel() string {
	if r.IsMalformed && r.MalformedKind != "" {
		return r.MalformedKind
	}
	return skill.KindLabel(r.Artifact.Kind)
}

func (r Row) CanOverrideMarketplace() bool {
	return r.Artifact.IsMarketplace && r.Artifact.Source == config.Organization && !r.Artifact.Required
}

// rowStatus is the authoritative result of a row's status precedence: one
// decision derives the ASCII glyph, the color-profile symbol, and the status
// label so the three can never diverge.
type rowStatus struct {
	Glyph  string
	Symbol string
	Label  string
}

func (r Row) Status() rowStatus {
	if r.PendingVerification {
		label := "enabled in config, waiting for verification"
		if r.IsExcluded {
			label = "excluded, waiting for verification"
		} else if r.IsAvailable {
			label = "available, waiting for verification"
		}
		if r.IsAvailable {
			return rowStatus{"[ ]", "○", label}
		}
		return rowStatus{"[x]", "●", label}
	}
	if r.IsMalformed {
		return rowStatus{"[!]", "✗", "malformed: " + r.MalformedMsg}
	}
	if r.UnavailableReason != "" {
		return rowStatus{"[!]", "✗", "unavailable: " + r.UnavailableReason}
	}
	if r.IsAvailable {
		if r.IsExcluded {
			return rowStatus{"[ ]", "○", "excluded"}
		}
		if r.MarketplaceDiscovery != nil {
			label := string(r.MarketplaceDiscovery.Item.Availability)
			if label == "" {
				label = "unknown"
			}
			if r.MarketplaceDiscovery.CacheState == marketplace.DiscoveryCacheStale {
				label += ", stale"
			}
			return rowStatus{"[ ]", "○", label + " via Marketplace"}
		}
		return rowStatus{"[ ]", "○", "available"}
	}
	if r.Artifact.Required {
		if r.Artifact.Source == config.Organization {
			return rowStatus{"[R]", "■", "required by org"}
		}
		return rowStatus{"[R]", "■", "required"}
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing, materialize.DriftOrphaned:
			return rowStatus{"[*]", "◐", "enabled, waiting for sync"}
		case materialize.DriftBroken:
			return rowStatus{"[!]", "✗", "broken"}
		}
	}
	return rowStatus{"[x]", "●", "enabled"}
}

func (r Row) Glyph() string { return r.Status().Glyph }

func (r Row) StatusText() string { return r.Status().Label }

// MarketplaceEndpointDisplay returns the endpoint's alias or normalized URL
// for display.
func MarketplaceEndpointDisplay(endpoint config.MarketplaceEntry) string {
	if endpoint.Alias != "" {
		return endpoint.Alias
	}
	if normalized, err := marketplace.NormalizeMarketplaceURL(endpoint.URL); err == nil {
		return normalized
	}
	return endpoint.URL
}

// MarketplaceRegistryLabel returns the endpoint display followed by the
// hosted registry slug when one is known.
func MarketplaceRegistryLabel(endpoint config.MarketplaceEntry, slug string) string {
	label := MarketplaceEndpointDisplay(endpoint)
	if slug != "" {
		label += "/" + slug
	}
	return label
}

// MarketplaceRegistry returns the shared registry label for a Marketplace
// row, whether installed, pending verification, or discovered.
func (r Row) MarketplaceRegistry() (string, bool) {
	if r.MarketplaceDiscovery != nil {
		return r.RegistryName, true
	}
	if !r.Artifact.IsMarketplace {
		return "", false
	}
	endpoint := config.MarketplaceEntry{Alias: r.Artifact.MarketplaceAlias, URL: r.Artifact.MarketplaceURL}
	if endpoint.Alias == "" && endpoint.URL == "" {
		return "", false
	}
	slug := cmp.Or(
		r.MarketplaceSelections[config.UserPersonal].Pin.RegistrySlug,
		r.MarketplaceSelections[config.Organization].Pin.RegistrySlug,
		r.MarketplaceRegistrySlug,
	)
	return MarketplaceRegistryLabel(endpoint, slug), true
}

// RowsHaveUnavailable reports whether any row carries an unavailable
// reason.
func RowsHaveUnavailable(rows []Row) bool {
	for _, r := range rows {
		if r.UnavailableReason != "" && !r.UnavailableSoft {
			return true
		}
	}
	return false
}
