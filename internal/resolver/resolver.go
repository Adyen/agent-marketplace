package resolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/redact"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

// ResolvedArtifact represents a resource that has been selected by the resolver
// after applying merge resolution, tag matching, excludes, and deduplication.
// It embeds catalog.Entry (the canonical discovery type) which promotes
// Kind, Name, Description, Tags, Resource, and Path for direct access.
type ResolvedArtifact struct {
	catalog.Entry
	RegistryURL    string
	RegistryPath   string // path from the registry entry; empty for root
	Ref            string
	V4Kind         config.V4SourceKind // explicit v4 source kind, when authored by v4
	Source         config.Source
	FilePrecedence int
	Required       bool
	CatalogKey     string
	// Via is set when the artifact was contributed by a collection rather than
	// directly configured. Format: "collection:<name>@<version>".
	// Empty string for directly-configured artifacts.
	Via string

	// Harnesses lists the target harnesses this artifact should be compiled
	// into. Inherited from the top-level config by default; overridden by
	// per-entry harnesses if set. Nil means "no harnesses configured"
	// (v1-style config); empty slice means "explicitly opted out".
	Harnesses []hcc.Target

	// Marketplace-specific fields.
	IsMarketplace         bool
	MarketplaceURL        string
	MarketplaceInstanceID string
	MarketplaceAlias      string
	MarketplaceRegistryID string
	InstalledName         string // overrides Name for materialization while preserving canonical digest
	Unresolved            bool   // true when only metadata was fetched (status inspect mode)
	Revoked               bool   // true when the resolved revision is revoked
	UpdateAvailable       bool   // true when a newer revision exists (status/update)
	Stale                 bool   // true when the registry refresh is stale/unavailable
}

func HasAdditiveSources(kind ir.ResourceKind) bool {
	return kind == ir.KindHook
}

type UnmetRequirementWarning struct {
	ResourceKind ir.ResourceKind
	ResourceName string
	Requirement  catalog.Requirement
}

func (w *UnmetRequirementWarning) Error() string {
	return fmt.Sprintf("%s %q requires %s %q, but it is not selected",
		skill.KindLabel(w.ResourceKind),
		w.ResourceName,
		skill.KindLabel(w.Requirement.Kind),
		w.Requirement.Name,
	)
}

func (w *UnmetRequirementWarning) Remediation() string {
	command := fmt.Sprintf("agent-marketplace install %q --kind %s", w.Requirement.Name, skill.KindLabel(w.Requirement.Kind))
	if w.Requirement.Registry != "" {
		command += fmt.Sprintf(" --from %q", redact.URL(w.Requirement.Registry))
	}
	if w.Requirement.Path != "" {
		command += fmt.Sprintf(" --path %q", redact.URL(w.Requirement.Path))
	}
	return command
}

// resolveOptions holds optional parameters for Resolve.
type resolveOptions struct {
	context                     context.Context
	resources                   map[string]map[string]ir.Resource
	malformed                   map[string][]skill.MalformedSkill
	targetHarnesses             []hcc.Target
	marketplaceResolver         MarketplaceResolver
	marketplaceMode             MarketplaceResolveMode
	marketplaceRevocationPolicy MarketplaceRevocationPolicy
}

// ResolveOption configures optional behavior for Resolve.
type ResolveOption func(*resolveOptions)

// MarketplaceResolveMode controls how much network work the Marketplace resolver
// performs.
type MarketplaceResolveMode int

const (
	// MarketplaceResolveModeRefresh fetches artifacts when they are missing from cache.
	MarketplaceResolveModeRefresh MarketplaceResolveMode = iota
	// MarketplaceResolveModeCached uses only cached artifacts and metadata.
	MarketplaceResolveModeCached
	// MarketplaceResolveModeInspect fetches metadata only and leaves Resource nil.
	MarketplaceResolveModeInspect
	// MarketplaceResolveModeCachedNoFetch uses only cached metadata and verified
	// artifacts. It does not construct a client or perform any HTTP request.
	MarketplaceResolveModeCachedNoFetch
)

// MarketplaceRevocationIdentity is the exact identity a temporary revoked
// override is allowed to match. ContentDigest is part of the identity so an
// override cannot authorize a different revision of the same resource.
type MarketplaceRevocationIdentity struct {
	Marketplace   string
	Registry      string
	Kind          string
	Name          string
	ContentDigest string
}

// MarketplaceRevocationPolicy authorizes only explicitly approved revoked
// identities for the current in-memory resolution.
type MarketplaceRevocationPolicy interface {
	AllowRevoked(MarketplaceRevocationIdentity) bool
}

// MarketplaceResolver resolves configured Marketplace pins into ResolvedArtifact
// values that participate in the same deduplication/collision pipeline as
// registry resources.
type MarketplaceResolver interface {
	Resolve(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode MarketplaceResolveMode) ([]ResolvedArtifact, []error)
}

// MarketplaceResolverWithPolicy is an optional extension implemented by
// Marketplace resolvers that support temporary revocation overrides. Existing
// MarketplaceResolver implementations remain source-compatible.
type MarketplaceResolverWithPolicy interface {
	ResolveWithPolicy(ctx context.Context, cfg *config.Config, targetSet []hcc.Target, mode MarketplaceResolveMode, policy MarketplaceRevocationPolicy) ([]ResolvedArtifact, []error)
}

// WithContext supplies the caller context to Marketplace resolution. It keeps
// the historical Resolve signature compatible for non-Marketplace callers.
func WithContext(ctx context.Context) ResolveOption {
	return func(opts *resolveOptions) {
		opts.context = ctx
	}
}

// WithMarketplaceResolver injects a Marketplace resolver.
func WithMarketplaceResolver(r MarketplaceResolver) ResolveOption {
	return func(opts *resolveOptions) {
		opts.marketplaceResolver = r
	}
}

// WithMarketplaceResolveMode sets the Marketplace resolve mode.
func WithMarketplaceResolveMode(m MarketplaceResolveMode) ResolveOption {
	return func(opts *resolveOptions) {
		opts.marketplaceMode = m
	}
}

// WithMarketplaceRevocationPolicy supplies an optional in-memory policy for
// exact revoked Marketplace overrides.
func WithMarketplaceRevocationPolicy(policy MarketplaceRevocationPolicy) ResolveOption {
	return func(opts *resolveOptions) {
		opts.marketplaceRevocationPolicy = policy
	}
}

// WithResources provides IR resources keyed by catalog key and
// resource-keyed name. When a catalog entry has a nil Resource (legacy
// discovery path), this map is used to attach the resource so
// materialize can compile without re-parsing.
func WithResources(resources map[string]map[string]ir.Resource) ResolveOption {
	return func(opts *resolveOptions) {
		opts.resources = resources
	}
}

// WithMalformed provides the resources that failed ingest validation, keyed by
// catalog key. Those resources are absent from the catalog entries — an
// ingester that cannot load a resource records a diagnostic and drops the
// resource — so without this set a selection that names one is indistinguishable
// from a selection that names a deleted resource, and Resolve reports the
// deletion. Supplying it lets Resolve blame the parse error it already has
// instead of guessing at an upstream removal.
func WithMalformed(malformed map[string][]skill.MalformedSkill) ResolveOption {
	return func(opts *resolveOptions) {
		opts.malformed = malformed
	}
}

// WithTargetHarnesses sets the machine-wide target harness set. When nil or
// empty, the resolver preserves existing nil-vs-empty harness semantics. When
// non-empty, every registry entry's effective harnesses are intersected with
// this set.
func WithTargetHarnesses(targets []hcc.Target) ResolveOption {
	return func(opts *resolveOptions) {
		if len(targets) > 0 {
			opts.targetHarnesses = make([]hcc.Target, len(targets))
			copy(opts.targetHarnesses, targets)
		}
	}
}

// Remediable is an error that provides a user-facing remediation hint.
type Remediable interface {
	Remediation() string
}

type skillOccurrence struct {
	resolved       ResolvedArtifact
	source         string
	url            string
	order          int
	partitionOrder int
}

type IneffectiveExcludeWarning struct {
	Scope  string
	Name   string
	Source config.Source
}

func (w *IneffectiveExcludeWarning) Error() string {
	return fmt.Sprintf("exclude %q in %s config is ineffective: skill is installed at %s level and materializes to a different directory", w.Name, w.Scope, w.Source)
}

// Remediation returns a user-facing hint about how to resolve the ineffective exclude.
func (w *IneffectiveExcludeWarning) Remediation() string {
	return fmt.Sprintf("exclude at %s level instead, or run 'agent-marketplace remove --scope %s %q'", w.Source, sourceToCLIScope(w.Source), w.Name)
}

// sourceToCLIScope maps a config.Source to a valid --scope flag value for the CLI.
func sourceToCLIScope(s config.Source) string {
	switch s {
	case config.SourceUnset:
		return "repo"
	case config.Organization, config.UserPersonal:
		return "user"
	case config.RepoCommitted, config.RepoPersonal:
		return "repo"
	default:
		return "user"
	}
}

type NameCollisionError struct {
	Kind    ir.ResourceKind
	Name    string
	Sources []string
}

func (e *NameCollisionError) Error() string {
	return fmt.Sprintf("%s is provided by multiple registries: %s", e.Resource(), e.Source())
}

func (e *NameCollisionError) Resource() string {
	return fmt.Sprintf("%s %q", skill.KindLabel(e.Kind), e.Name)
}
func (e *NameCollisionError) Source() string { return strings.Join(redact.Strings(e.Sources), ", ") }

// Remediation returns a user-facing hint about how to resolve the name collision.
func (e *NameCollisionError) Remediation() string {
	if len(e.Sources) > 0 {
		return fmt.Sprintf("exclude from one registry: 'agent-marketplace remove %q --from %s'", e.Name, redact.URL(e.Sources[0]))
	}
	return fmt.Sprintf("exclude from one registry: 'agent-marketplace remove %q --from <url>'", e.Name)
}

// NameCollisionWarning is emitted when the same resource name is provided by
// multiple registries. Unlike NameCollisionError (which skips the resource),
// the resolver picks a deterministic winner and continues — the resource is
// installed from the winning source. This is a soft warning: the user is
// informed but no manual action is required.
type NameCollisionWarning struct {
	Kind      ir.ResourceKind
	Name      string
	Winner    string // display label of the winning occurrence
	RunnersUp []string

	// WinnerURL, WinnerPath, and WinnerRef retain the structured registry
	// location used to produce Winner. Remediation must use these fields rather
	// than parsing display labels, whose delimiters can occur in valid values.
	WinnerURL  string
	WinnerPath string
	WinnerRef  string
}

func (e *NameCollisionWarning) Error() string {
	return fmt.Sprintf("%s is provided by multiple registries; %s", e.Resource(), e.Source())
}

func (e *NameCollisionWarning) Resource() string {
	return fmt.Sprintf("%s %q", skill.KindLabel(e.Kind), e.Name)
}
func (e *NameCollisionWarning) Source() string {
	return fmt.Sprintf("using %s (others: %s)", redact.URL(e.Winner), strings.Join(redact.Strings(e.RunnersUp), ", "))
}

// Remediation returns a user-facing hint about how to resolve the name collision
// if the user disagrees with the chosen winner.
func (e *NameCollisionWarning) Remediation() string {
	if len(e.RunnersUp) > 0 {
		from := e.WinnerURL
		if from == "" {
			from = e.Winner
		}
		args := []string{"agent-marketplace", "remove", shellQuote(e.Name), "--from", shellQuote(redact.URL(from))}
		if e.WinnerPath != "" {
			args = append(args, "--path", shellQuote(e.WinnerPath))
		}
		if e.WinnerRef != "" {
			args = append(args, "--ref", shellQuote(e.WinnerRef))
		}
		return "to prefer a different source, exclude the winner: " + strings.Join(args, " ")
	}
	return fmt.Sprintf("to prefer a different source, exclude the winner: agent-marketplace remove %s --from <url>", shellQuote(e.Name))
}

// shellQuote quotes one argument for a POSIX shell while leaving ordinary
// registry URLs and resource names readable. Values containing shell syntax,
// whitespace, or delimiters are single-quoted with embedded quotes escaped.
func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case strings.ContainsRune("._/:@%+,-=", r):
			return false
		default:
			return true
		}
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// AmbiguousCollectionMemberWarning reports a member with multiple registry locations.
type AmbiguousCollectionMemberWarning struct {
	Kind ir.ResourceKind
	Name string
	// URL is the registry URL as declared by the collection member, not the
	// spelling of any particular config entry.
	URL string
	Ref string
	// Via is the collection provenance label, e.g. "collection:<name>@<v>".
	Via string
	// Chosen is the label of the location that was bound, in the same
	// spelling as the matching element of Candidates.
	Chosen string
	Path   string
	// Candidates lists every configured location that contains the member, in
	// config order. It always holds one label per distinct location, so
	// len(Candidates) is the number of locations the warning is about.
	Candidates []string
}

func (w *AmbiguousCollectionMemberWarning) Error() string {
	reference := "references it without a path"
	if w.Path != "" {
		reference = fmt.Sprintf("references it at path %q without a ref", w.Path)
	}
	return fmt.Sprintf("%s %q is provided by %d configured locations of %s@%s (%s); %s, so %s was used",
		skill.KindLabel(w.Kind), w.Name, len(w.Candidates), w.URL, w.Ref,
		strings.Join(w.Candidates, ", "), w.Via+" "+reference, w.Chosen)
}

// Remediation returns a user-facing hint about how to make the binding explicit.
func (w *AmbiguousCollectionMemberWarning) Remediation() string {
	if w.Path != "" {
		return fmt.Sprintf("pin the member to %s by adding an explicit 'ref:' next to the path %q for %q in the collection definition, or drop the duplicate registry entry from your config", w.Chosen, w.Path, w.Name)
	}
	return fmt.Sprintf("pin the member to %s by adding an explicit 'path:' (and a 'ref:' when the locations differ only by ref) next to %q in the collection definition, or drop the duplicate registry entry from your config", w.Chosen, w.Name)
}

// IsCollectionMemberAmbiguity reports whether err is an
// AmbiguousCollectionMemberWarning.
func IsCollectionMemberAmbiguity(err error) bool {
	var ambiguous *AmbiguousCollectionMemberWarning
	return errors.As(err, &ambiguous)
}

func IsUnmetRequirement(err error) bool {
	var requirement *UnmetRequirementWarning
	return errors.As(err, &requirement)
}

func IsNameCollision(err error) bool {
	var collisionErr *NameCollisionError
	var collisionWarn *NameCollisionWarning
	return errors.As(err, &collisionErr) || errors.As(err, &collisionWarn)
}

// IncompatibleOrganizationRequirement is a hard error emitted when an
// Organization registry entry marked required:true has no harnesses remaining
// after intersecting with the machine-wide target harness set.
type IncompatibleOrganizationRequirement struct {
	Kind    ir.ResourceKind
	Name    string
	Entry   config.RegistryEntry
	Targets []hcc.Target
}

func (e *IncompatibleOrganizationRequirement) Error() string {
	return fmt.Sprintf("organization requirement %s %q from %s is incompatible with the target harness set (%s)", skill.KindLabel(e.Kind), e.Name, e.Entry.Source, formatTargets(e.Targets))
}

// Remediation returns a user-facing hint about how to resolve the incompatible
// organization requirement.
func (e *IncompatibleOrganizationRequirement) Remediation() string {
	return "remove the requirement or add a compatible harness to the user/global target harness set"
}

// IncompatibleOrganizationDefault is a warning emitted when an Organization
// registry entry marked default:true has no harnesses remaining after
// intersecting with the machine-wide target harness set.
type IncompatibleOrganizationDefault struct {
	Kind    ir.ResourceKind
	Name    string
	Entry   config.RegistryEntry
	Targets []hcc.Target
}

func (e *IncompatibleOrganizationDefault) Error() string {
	return fmt.Sprintf("organization default %s %q from %s is incompatible with the target harness set (%s)", skill.KindLabel(e.Kind), e.Name, e.Entry.Source, formatTargets(e.Targets))
}

// Remediation returns a user-facing hint about how to resolve the incompatible
// organization default.
func (e *IncompatibleOrganizationDefault) Remediation() string {
	return "remove the default entry or add a compatible harness to the user/global target harness set"
}

func IsWarning(err error) bool {
	var warning *IneffectiveExcludeWarning
	var collision *NameCollisionWarning
	var requirement *UnmetRequirementWarning
	var incompatibleDefault *IncompatibleOrganizationDefault
	return errors.As(err, &warning) ||
		errors.As(err, &collision) ||
		errors.As(err, &requirement) ||
		errors.As(err, &incompatibleDefault) ||
		IsCollectionMemberAmbiguity(err)
}

// SelectedNotFoundError indicates that a resource was explicitly selected in
// the config but could not be found in the referenced registry. This is a
// soft failure — the sync continues and the error is surfaced as a warning
// rather than a hard error, since it typically results from upstream skill
// removal and the user cannot fix it by retrying. However, if the registry
// was marked Required, the error remains hard (exit 1) to preserve policy
// enforcement.
type SelectedNotFoundError struct {
	Kind     ir.ResourceKind
	Name     string
	URL      string
	Path     string
	Ref      string
	Hint     string
	Required bool
	// Via is set when the selection originated from a collection expansion
	// rather than a direct config entry. Format: "collection:<name>@<version>".
	Via string
	// CandidatePaths is set when the selection could not be attributed to a
	// single subdirectory: a collection member named a registry URL without a
	// path, so every configured location of that repository was searched. It
	// holds those locations in config order. When it is set, Path is left empty
	// (no single path may be blamed) and the remediation stops claiming an
	// upstream removal, because the resource may well exist in the repository
	// at a path that is simply not configured.
	CandidatePaths []string
	// RequestedPath is set when a collection member pinned an explicit `path:`
	// that no configured entry for that repository declares. The pin is an
	// authoring detail of a collection the consumer usually cannot edit, so it
	// must be reported exactly (the requested path is named) without being able
	// to fail their sync on its own; CandidatePaths then holds the paths that
	// are configured.
	RequestedPath string
}

func (e *SelectedNotFoundError) Error() string {
	msg := fmt.Sprintf("%s was selected but not found in %s", e.Resource(), e.Source())
	if e.Hint != "" {
		msg += "; " + e.Hint
	}
	return msg
}

func (e *SelectedNotFoundError) Resource() string {
	return fmt.Sprintf("%s %q", skill.KindLabel(e.Kind), e.Name)
}
func (e *SelectedNotFoundError) Source() string { return e.location() }

// location renders the registry location the resource was looked for in. For a
// path-less collection member it names every searched location rather than
// inventing the one that happened to be tried first; for a member pinned to a
// path nobody configured, it names the requested path alongside the configured
// ones.
func (e *SelectedNotFoundError) location() string {
	if e.RequestedPath != "" {
		return fmt.Sprintf("%s@%s at the requested path %q, which no configured entry uses (configured paths: %s)",
			redact.URL(e.URL), e.Ref, redact.URL(e.RequestedPath), strings.Join(redact.Strings(e.CandidatePaths), ", "))
	}
	if len(e.CandidatePaths) > 0 {
		return fmt.Sprintf("%s@%s under any configured path (searched: %s)", redact.URL(e.URL), e.Ref, strings.Join(redact.Strings(e.CandidatePaths), ", "))
	}
	loc := redact.URL(e.URL)
	if e.Path != "" {
		loc += " (" + redact.URL(e.Path) + ")"
	}
	return loc + "@" + e.Ref
}

// Remediation returns a user-facing hint about how to resolve the stale selection.
func (e *SelectedNotFoundError) Remediation() string {
	if e.RequestedPath != "" {
		// The requested and configured paths are already enumerated by Error().
		parts := []string{
			fmt.Sprintf("%s pins this %s to path %q of %s, which no configured registry entry provides", e.Via, skill.KindLabel(e.Kind), redact.URL(e.RequestedPath), redact.URL(e.URL)),
		}
		if e.Hint != "" {
			parts = append(parts, e.Hint)
		}
		parts = append(parts, fmt.Sprintf("configure a registry entry for that path, or update the %q member of the collection definition to a path you do configure", e.Name))
		return strings.Join(parts, "; ")
	}
	if len(e.CandidatePaths) > 0 {
		// The searched locations are already enumerated by Error(); repeating
		// them here only lengthens the message.
		parts := []string{
			fmt.Sprintf("%s references this %s by registry only, with no path, so every configured location of %s was searched and none contains it", e.Via, skill.KindLabel(e.Kind), redact.URL(e.URL)),
		}
		if e.Hint != "" {
			parts = append(parts, e.Hint)
		}
		parts = append(parts, fmt.Sprintf("add an explicit 'path:' to the %q member of the collection definition, or configure a registry entry for the subdirectory that provides it", e.Name))
		return strings.Join(parts, "; ")
	}
	if e.Via != "" {
		parts := []string{
			fmt.Sprintf("this skill is referenced by %s; the skill was likely removed or renamed upstream", e.Via),
		}
		if e.Hint != "" {
			parts = append(parts, e.Hint)
		}
		parts = append(parts, fmt.Sprintf("update the collection definition to remove or fix the reference to %q, or remove the collection from your config: 'agent-marketplace remove %s'", e.Name, collectionRemoveTarget(e.Via)))
		return strings.Join(parts, "; ")
	}
	parts := []string{
		"this may be caused by an upstream removal, a stale ref pin, a rename, or a cache issue",
	}
	if e.Hint != "" {
		parts = append(parts, e.Hint)
	}
	parts = append(parts, fmt.Sprintf("run 'agent-marketplace status' to diagnose, or 'agent-marketplace remove %q' to drop the selection", e.Name))
	return strings.Join(parts, "; ")
}

// collectionRemoveTarget derives the bare collection name that
// 'agent-marketplace remove' expects from a Via label of the form
// "collection:<name>@<version>". The "collection:" prefix and any "@<version>"
// suffix are stripped because removal matches collections by name only.
func collectionRemoveTarget(via string) string {
	name := strings.TrimPrefix(via, "collection:")
	if at := strings.IndexByte(name, '@'); at >= 0 {
		name = name[:at]
	}
	return name
}

func IsSelectedNotFound(err error) bool {
	var snf *SelectedNotFoundError
	return errors.As(err, &snf)
}

// SelectedMalformedError indicates that a resource was explicitly selected in
// the config, exists in the referenced registry, and was dropped from the
// catalog because it failed ingest validation.
//
// It is deliberately NOT a SelectedNotFoundError and must never be reported as
// one: the two have opposite remediations. A not-found selection is stale and
// the fix is to drop it (which is why `sync --prune-stale` acts on that type
// alone). A malformed selection is correct and the fix is to repair the file
// named here — dropping it would delete a working selection over a syntax error
// the tool has already read and already has an exact message for.
type SelectedMalformedError struct {
	Kind     ir.ResourceKind
	Name     string
	URL      string
	Path     string
	Ref      string
	Required bool
	// Via is set when the selection originated from a collection expansion
	// rather than a direct config entry. Format: "collection:<name>@<version>".
	Via string
	// DirName is the directory (or file) the ingester failed to load. It differs
	// from Name when the selection matched the authored frontmatter name.
	DirName string
	// File is the source file the user has to fix.
	File string
	// Err is the underlying validation failure.
	Err error
}

func (e *SelectedMalformedError) Error() string {
	loc := redact.URL(e.URL)
	if e.Path != "" {
		loc += " (" + e.Path + ")"
	}
	loc += "@" + e.Ref
	msg := fmt.Sprintf("%s %q was selected and found in %s but failed to load", skill.KindLabel(e.Kind), e.Name, loc)
	if e.DirName != "" && e.DirName != e.Name {
		// Not "authored as": Name is the authored frontmatter name in exactly
		// this branch, and DirName is what the ingester saw on disk.
		msg += fmt.Sprintf(" (reported by the ingester as %q)", e.DirName)
	}
	msg += ": " + skill.ParseFailureForFile(e.File)
	if e.Err != nil {
		msg += fmt.Sprintf(": %v", e.Err)
	}
	return msg
}

func (e *SelectedMalformedError) Unwrap() error { return e.Err }

// Remediation points at the file and the validation failure. It never suggests
// dropping the selection: the selection is valid and the resource is present.
func (e *SelectedMalformedError) Remediation() string {
	target := e.File
	if target == "" {
		target = e.Name
	}
	parts := []string{
		fmt.Sprintf("the %s is present in the registry but did not pass ingest validation, so it was dropped from the catalog", skill.KindLabel(e.Kind)),
		fmt.Sprintf("fix the frontmatter in %s (or ask the registry owner to) and re-run 'agent-marketplace sync'", target),
	}
	if e.Via != "" {
		parts = append(parts, fmt.Sprintf("this %s is contributed by %s, so every consumer of that collection is affected", skill.KindLabel(e.Kind), e.Via))
	}
	parts = append(parts, "the selection itself is correct and should be kept; run 'agent-marketplace sync --verbose' to list every resource that failed validation")
	return strings.Join(parts, "; ")
}

// IsSelectedMalformed reports whether err is a SelectedMalformedError.
func IsSelectedMalformed(err error) bool {
	var sme *SelectedMalformedError
	return errors.As(err, &sme)
}

// IsSoftSelectedMalformed reports whether err is a SelectedMalformedError that
// is safe to downgrade to a warning, mirroring IsSoftSelectedNotFound: required
// resources and collections stay hard so policy enforcement and user config
// errors keep failing the run.
func IsSoftSelectedMalformed(err error) bool {
	var sme *SelectedMalformedError
	if !errors.As(err, &sme) {
		return false
	}
	if sme.Kind == ir.KindCollection {
		return false
	}
	return !sme.Required
}

// malformedIndex answers "is this missing selection actually a malformed
// resource?" in constant time per query.
//
// Resolve is a documented hot path and correlation runs once per (unresolved
// selection × malformed entry): a linear scan is O(misses × malformed) per
// registry, which on a large stale-heavy config against a badly broken registry
// (30 entries × 50 misses × 200 dropped resources) cost ~30ms per Resolve —
// see BenchmarkResolveMalformedCorrelationWorstCase. Indexing makes it
// O(malformed) once per registry instead.
//
// The per-registry index is built lazily on the first miss for that registry,
// so a healthy resolve (no misses) pays nothing at all.
type malformedIndex struct {
	byCatalog map[string][]skill.MalformedSkill
	byName    map[string]map[string][]skill.MalformedSkill
}

func newMalformedIndex(malformed map[string][]skill.MalformedSkill) *malformedIndex {
	return &malformedIndex{
		byCatalog: malformed,
		byName:    make(map[string]map[string][]skill.MalformedSkill, len(malformed)),
	}
}

// lookup returns the malformed resource of the given kind that name refers to
// within one registry's catalog. A malformed entry that does not declare its
// kind matches any kind; two known and different kinds never correlate.
func (ix *malformedIndex) lookup(catalogKey string, kind ir.ResourceKind, name string) (skill.MalformedSkill, bool) {
	if ix == nil || len(ix.byCatalog) == 0 {
		return skill.MalformedSkill{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return skill.MalformedSkill{}, false
	}
	candidates, built := ix.byName[catalogKey]
	if !built {
		candidates = buildMalformedNameIndex(ix.byCatalog[catalogKey])
		ix.byName[catalogKey] = candidates
	}
	wantKind := kind.String()
	for _, m := range candidates[name] {
		if skill.KindsCompatible(m.Kind, wantKind) {
			return m, true
		}
	}
	return skill.MalformedSkill{}, false
}

// buildMalformedNameIndex maps every name a selection could legitimately use
// for a malformed resource back to that resource. Entries are appended in
// malformed-set order, so the earliest matching entry still wins.
func buildMalformedNameIndex(entries []skill.MalformedSkill) map[string][]skill.MalformedSkill {
	if len(entries) == 0 {
		return nil
	}
	index := make(map[string][]skill.MalformedSkill, len(entries))
	for _, m := range entries {
		for _, candidate := range skill.MalformedSelectionNames(m) {
			index[candidate] = append(index[candidate], m)
		}
	}
	return index
}

// malformedSelectionError correlates a selection that is absent from a catalog
// with that catalog's malformed set, and is the single place both trigger sites
// (direct selections in buildOccurrences, collection members in
// expandMembersRecursive) consult before constructing a SelectedNotFoundError.
// It returns nil when the absence is a genuine absence, which keeps the existing
// not-found warning and remediation for resources that really are gone.
func malformedSelectionError(
	malformed *malformedIndex,
	catalogKey string,
	kind ir.ResourceKind,
	name string,
	url, path, ref string,
	required bool,
	via string,
) *SelectedMalformedError {
	match, found := malformed.lookup(catalogKey, kind, name)
	if !found {
		return nil
	}
	return &SelectedMalformedError{
		Kind:     kind,
		Name:     name,
		URL:      url,
		Path:     path,
		Ref:      ref,
		Required: required,
		Via:      via,
		DirName:  strings.TrimSpace(match.DirName),
		File:     skill.MalformedSourceFile(match),
		Err:      match.Error,
	}
}

// IsSoftSelectedNotFound returns true if the error is a SelectedNotFoundError
// for a non-required resource (i.e., safe to downgrade to a warning).
// Required resources that go missing remain hard errors for policy enforcement.
// Collection selected-not-found is always hard because a config referencing a
// non-existent collection is a user error that must be surfaced.
func IsSoftSelectedNotFound(err error) bool {
	var snf *SelectedNotFoundError
	if !errors.As(err, &snf) {
		return false
	}
	if snf.Kind == ir.KindCollection {
		return false
	}
	return !snf.Required
}

// Resolve selects artifacts from catalogs according to the merged config,
// applying tag matching, excludes, deduplication, and collision detection.
// Optional ResolveOption parameters extend resolution with IR resource
// attachment (WithResources) and other future capabilities.
func Resolve(cfg *config.Config, catalogs map[string][]catalog.Entry, localCfg *config.LocalConfig, opts ...ResolveOption) ([]ResolvedArtifact, []error) {
	start := time.Now()
	var ro resolveOptions
	for _, opt := range opts {
		opt(&ro)
	}
	if ro.context == nil {
		ro.context = context.Background()
	}

	if cfg == nil || (len(cfg.Registries) == 0 && (ro.marketplaceResolver == nil || len(cfg.Marketplaces) == 0)) {
		slog.Debug("resolver skipped", "reason", "no_registries_or_marketplaces")
		return []ResolvedArtifact{}, nil
	}

	if catalogs == nil {
		catalogs = map[string][]catalog.Entry{}
	}

	// Collection propagation is an internal normalization step. Work on a
	// deep-enough copy so appending selectors never changes the caller's
	// configuration (or races another concurrent Resolve using it).
	cfg = cloneConfigForResolve(cfg)

	var localSelections resourceSelections
	var localExcludes resourceSelections
	var excludeAllKinds map[ir.ResourceKind]bool
	if localCfg != nil {
		localSelections = localResourceSelections(localCfg)
		localExcludes = localResourceExcludes(localCfg)
		excludeAllKinds = localExcludeAllKinds(localCfg)
	}

	// Propagate top-level Collections.Include to all registry entries so
	// collections are selected from whichever registry provides them.
	// Without this, a collection named in the global config would only be
	// looked for in registries from the global layer, missing collections
	// from repo-level registries.
	propagateTopLevelCollections(cfg)

	targetSet := ro.targetHarnesses

	// Correlation index over the resources that failed ingest validation,
	// shared by both trigger sites and built lazily per registry.
	malformedIdx := newMalformedIndex(ro.malformed)

	// Phase 1: Build occurrence map from registry entries.
	occurrencesByName, errs := buildOccurrences(cfg, catalogs, ro.resources, malformedIdx, targetSet)
	filterShadowedOccurrences(occurrencesByName, cfg.Registries)
	slog.Debug("resolver occurrences built", "registries", len(cfg.Registries), "resources", len(occurrencesByName), "errors", len(errs))

	// Phase 1b: Add Marketplace pins to the occurrence map.
	if ro.marketplaceResolver != nil && len(cfg.Marketplaces) > 0 {
		nextOrder := maxOccurrenceOrder(occurrencesByName)
		mpOccurrences, mpErrs := buildMarketplaceOccurrences(ro.context, ro.marketplaceResolver, cfg, targetSet, ro.marketplaceMode, ro.marketplaceRevocationPolicy, nextOrder)
		errs = append(errs, mpErrs...)
		for sel, occs := range mpOccurrences {
			occurrencesByName[sel] = append(occurrencesByName[sel], occs...)
		}
		slog.Debug("resolver marketplace occurrences built", "marketplaces", len(cfg.Marketplaces), "errors", len(mpErrs))
	}

	// Qualify winner selection by target before deduplicating. The same resource
	// may legitimately have different winners, requirements, or collection
	// members in otherwise disjoint output partitions.
	occurrencesByName = partitionOccurrencesByTarget(occurrencesByName)

	// Phase 2: Pick winners (deduplicate, detect collisions).
	winners, pickErrs := pickWinners(occurrencesByName)
	errs = append(errs, pickErrs...)
	slog.Debug("resolver winners picked", "winners", len(winners), "collision_warnings", len(pickErrs))

	resolved := make([]ResolvedArtifact, 0, len(winners))
	for _, winner := range winners {
		resolved = append(resolved, winner.resolved)
	}

	// Phase 3: Apply cross-layer excludes and local selections.
	resolved = applyCrossLayerExcludes(resolved, cfg.Registries)

	var localErrs []error
	resolved, localErrs = applyLocalResources(resolved, localSelections, localExcludes, excludeAllKinds, cfg, catalogs, ro.resources, targetSet)
	errs = append(errs, localErrs...)
	preExcludeResolved := append([]ResolvedArtifact(nil), resolved...)

	resolved = applyPersonalExcludes(resolved, localExcludes, excludeAllKinds)
	errs = append(errs, validateCrossLayerExcludes(preExcludeResolved, cfg.Registries, localCfg)...)

	// Collections are virtual resources — they do not materialize to disk.
	// Expand each resolved KindCollection into its member resources, carrying
	// a Via annotation, and remove the collection artifacts themselves.
	// This replaces the pre-resolver collection.Expand call, giving
	// collections the same collision detection and disambiguation as
	// skills/commands/etc.
	var expandErrs []error
	resolved, expandErrs = expandCollections(resolved, cfg, catalogs, ro.resources, malformedIdx, localExcludes, cfg.Collections.ExcludeAll, excludeAllKinds, targetSet)
	errs = append(errs, expandErrs...)
	errs = append(errs, validateRequirements(resolved)...)
	resolved = mergeTargetQualifiedArtifacts(resolved)
	slog.Debug("resolver end", "duration_ms", time.Since(start).Milliseconds(), "resolved", len(resolved), "errors", len(errs))

	return resolved, errs
}

type CatalogArtifact struct {
	ResolvedArtifact
	Selected bool
}

func ResolveCatalogView(cfg *config.Config, catalogs map[string][]catalog.Entry, resolved []ResolvedArtifact) []CatalogArtifact {
	if cfg == nil {
		return nil
	}

	displayIdentity := func(a ResolvedArtifact) string {
		if a.IsLocal() {
			return a.CatalogKey
		}
		return normalizedRegistryURL(a.RegistryURL) + "\x00" + strings.TrimSpace(a.Ref) + "\x00" + normalizedEntryPath(a.RegistryPath)
	}

	type displayKey struct {
		kind     ir.ResourceKind
		name     string
		identity string
	}
	selectedResources := make(map[resourceSelection]struct{}, len(resolved))
	seen := make(map[displayKey]struct{}, len(resolved))
	view := make([]CatalogArtifact, 0, len(resolved))
	for _, artifact := range resolved {
		selection := resourceSelection{kind: artifact.Kind, name: strings.TrimSpace(artifact.Name)}
		key := displayKey{kind: selection.kind, name: selection.name, identity: displayIdentity(artifact)}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		selectedResources[selection] = struct{}{}
		seen[key] = struct{}{}
		view = append(view, CatalogArtifact{ResolvedArtifact: artifact, Selected: true})
	}

	occurrences := make(map[resourceSelection][]skillOccurrence)
	order := 0
	for entryIndex, entry := range cfg.Registries {
		entries, ok := catalogs[entry.CatalogKey]
		if !ok {
			continue
		}
		seenEntries := make(map[resourceSelection]struct{}, len(entries))
		for _, candidate := range entries {
			name := strings.TrimSpace(candidate.Name)
			key := resourceSelection{kind: candidate.Kind, name: name}
			if name == "" {
				continue
			}
			if _, duplicate := seenEntries[key]; duplicate {
				continue
			}
			seenEntries[key] = struct{}{}
			occurrences[key] = append(occurrences[key], newSkillOccurrence(entry, entryIndex, candidate, nil, harnessTargetsFromEntry(entry), order))
			order++
		}
	}

	winners, _ := pickWinnersReporting(occurrences, false)
	for _, winner := range winners {
		artifact := winner.resolved
		selection := resourceSelection{kind: artifact.Kind, name: strings.TrimSpace(artifact.Name)}
		if _, selected := selectedResources[selection]; selected && !HasAdditiveSources(selection.kind) {
			continue
		}
		key := displayKey{kind: selection.kind, name: selection.name, identity: displayIdentity(artifact)}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		view = append(view, CatalogArtifact{ResolvedArtifact: artifact})
	}
	return view
}

func maxOccurrenceOrder(occurrencesByName map[resourceSelection][]skillOccurrence) int {
	max := 0
	for _, occs := range occurrencesByName {
		for _, occ := range occs {
			if occ.order >= max {
				max = occ.order + 1
			}
		}
	}
	return max
}

func buildMarketplaceOccurrences(ctx context.Context, mpResolver MarketplaceResolver, cfg *config.Config, targetSet []hcc.Target, mode MarketplaceResolveMode, policy MarketplaceRevocationPolicy, startOrder int) (map[resourceSelection][]skillOccurrence, []error) {
	var artifacts []ResolvedArtifact
	var errs []error
	if resolverWithPolicy, ok := mpResolver.(MarketplaceResolverWithPolicy); ok {
		artifacts, errs = resolverWithPolicy.ResolveWithPolicy(ctx, cfg, targetSet, mode, policy)
	} else {
		artifacts, errs = mpResolver.Resolve(ctx, cfg, targetSet, mode)
	}
	occurrencesByName := make(map[resourceSelection][]skillOccurrence, len(artifacts))
	for i, a := range artifacts {
		sel := resourceSelection{kind: a.Kind, name: a.Name}
		source := a.MarketplaceURL
		if a.MarketplaceRegistryID != "" {
			source = fmt.Sprintf("marketplace:%s:%s", a.MarketplaceURL, a.MarketplaceRegistryID)
		}
		occ := skillOccurrence{
			resolved: a,
			source:   source,
			url:      a.MarketplaceURL,
			order:    startOrder + i,
		}
		occurrencesByName[sel] = append(occurrencesByName[sel], occ)
	}
	return occurrencesByName, errs
}

func partitionOccurrencesByTarget(input map[resourceSelection][]skillOccurrence) map[resourceSelection][]skillOccurrence {
	partitioned := make(map[resourceSelection][]skillOccurrence, len(input))
	for selection, occurrences := range input {
		for _, occurrence := range occurrences {
			if occurrence.resolved.Harnesses == nil {
				partitioned[selection] = append(partitioned[selection], occurrence)
				continue
			}
			if len(occurrence.resolved.Harnesses) == 0 {
				key := selection
				key.empty = true
				partitioned[key] = append(partitioned[key], occurrence)
				continue
			}
			for targetIndex, target := range occurrence.resolved.Harnesses {
				clone := occurrence
				clone.partitionOrder = targetIndex
				clone.resolved.Harnesses = []hcc.Target{target}
				key := resourceSelection{kind: selection.kind, name: selection.name, target: target}
				partitioned[key] = append(partitioned[key], clone)
			}
		}
	}
	return partitioned
}

func validateRequirements(resolved []ResolvedArtifact) []error {
	type resourceKey struct {
		kind         ir.ResourceKind
		name         string
		target       hcc.Target
		machineScope bool
	}

	selected := make(map[resourceKey]struct{}, len(resolved))
	for _, artifact := range resolved {
		if artifact.Harnesses != nil && len(artifact.Harnesses) == 0 {
			continue
		}
		for _, target := range artifactTargets(artifact) {
			selected[resourceKey{
				kind:         artifact.Kind,
				name:         artifact.Name,
				target:       target,
				machineScope: artifact.Source.IsMachineScope(),
			}] = struct{}{}
		}
	}

	var warnings []error
	for _, artifact := range resolved {
		if artifact.Harnesses != nil && len(artifact.Harnesses) == 0 {
			continue
		}
		for _, requirement := range catalog.RequirementsFromResource(artifact.Resource) {
			for _, target := range artifactTargets(artifact) {
				key := resourceKey{
					kind:         requirement.Kind,
					name:         requirement.Name,
					target:       target,
					machineScope: artifact.Source.IsMachineScope(),
				}
				if _, ok := selected[key]; ok {
					continue
				}
				slog.Debug("unmet resource requirement",
					"resource_kind", skill.KindLabel(artifact.Kind),
					"resource_name", artifact.Name,
					"requirement_kind", skill.KindLabel(requirement.Kind),
					"requirement_name", requirement.Name,
					"target", key.target,
					"machine_scope", key.machineScope,
				)
				warnings = append(warnings, &UnmetRequirementWarning{
					ResourceKind: artifact.Kind,
					ResourceName: artifact.Name,
					Requirement:  requirement,
				})
			}
		}
	}
	slog.Debug("requirement validation end", "unmet", len(warnings))
	return warnings
}

func artifactTarget(artifact ResolvedArtifact) hcc.Target {
	if len(artifact.Harnesses) == 1 {
		return artifact.Harnesses[0]
	}
	return ""
}

// artifactTargets returns every output partition represented by an artifact.
// Legacy nil-target artifacts retain the empty target identity for diagnostics;
// explicit target lists are checked independently.
func artifactTargets(artifact ResolvedArtifact) []hcc.Target {
	if len(artifact.Harnesses) > 0 {
		return artifact.Harnesses
	}
	return []hcc.Target{artifactTarget(artifact)}
}

type resolvedArtifactIdentity struct {
	kind                  ir.ResourceKind
	name                  string
	path                  string
	registryURL           string
	registryPath          string
	ref                   string
	source                config.Source
	filePrecedence        int
	required              bool
	catalogKey            string
	via                   string
	isMarketplace         bool
	marketplaceURL        string
	marketplaceInstanceID string
	marketplaceAlias      string
	marketplaceRegistryID string
	installedName         string
	unresolved            bool
	revoked               bool
	updateAvailable       bool
	stale                 bool
}

func resolvedArtifactIdentityOf(artifact ResolvedArtifact) resolvedArtifactIdentity {
	return resolvedArtifactIdentity{
		kind:                  artifact.Kind,
		name:                  artifact.Name,
		path:                  artifact.Path,
		registryURL:           artifact.RegistryURL,
		registryPath:          artifact.RegistryPath,
		ref:                   artifact.Ref,
		source:                artifact.Source,
		filePrecedence:        artifact.FilePrecedence,
		required:              artifact.Required,
		catalogKey:            artifact.CatalogKey,
		via:                   artifact.Via,
		isMarketplace:         artifact.IsMarketplace,
		marketplaceURL:        artifact.MarketplaceURL,
		marketplaceInstanceID: artifact.MarketplaceInstanceID,
		marketplaceAlias:      artifact.MarketplaceAlias,
		marketplaceRegistryID: artifact.MarketplaceRegistryID,
		installedName:         artifact.InstalledName,
		unresolved:            artifact.Unresolved,
		revoked:               artifact.Revoked,
		updateAvailable:       artifact.UpdateAvailable,
		stale:                 artifact.Stale,
	}
}

func mergeTargetQualifiedArtifacts(resolved []ResolvedArtifact) []ResolvedArtifact {
	merged := make([]ResolvedArtifact, 0, len(resolved))
	indices := make(map[resolvedArtifactIdentity]int, len(resolved))
	for _, artifact := range resolved {
		identity := resolvedArtifactIdentityOf(artifact)
		if index, ok := indices[identity]; ok {
			for _, target := range artifact.Harnesses {
				if !slices.Contains(merged[index].Harnesses, target) {
					merged[index].Harnesses = append(merged[index].Harnesses, target)
				}
			}
			continue
		}
		indices[identity] = len(merged)
		merged = append(merged, artifact)
	}
	return merged
}

// cloneStringsForResolve preserves nil-vs-empty selector semantics while
// isolating Resolve's private config copy from the caller.
func cloneStringsForResolve(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneSelectorForResolve(selector config.KindSelector) config.KindSelector {
	selector.Include = cloneStringsForResolve(selector.Include)
	selector.Exclude = cloneStringsForResolve(selector.Exclude)
	return selector
}

func cloneConfigForResolve(cfg *config.Config) *config.Config {
	cloned := *cfg
	cloned.Collections = cloneSelectorForResolve(cfg.Collections)
	cloned.Skills = cloneSelectorForResolve(cfg.Skills)
	cloned.Commands = cloneSelectorForResolve(cfg.Commands)
	cloned.Subagents = cloneSelectorForResolve(cfg.Subagents)
	cloned.Hooks = cloneSelectorForResolve(cfg.Hooks)
	cloned.MCP = cloneSelectorForResolve(cfg.MCP)
	cloned.Tags = cloneStringsForResolve(cfg.Tags)
	cloned.Harnesses = cloneStringsForResolve(cfg.Harnesses)
	cloned.Registries = make([]config.RegistryEntry, len(cfg.Registries))
	for i, entry := range cfg.Registries {
		entry.Skills = cloneSelectorForResolve(entry.Skills)
		entry.Commands = cloneSelectorForResolve(entry.Commands)
		entry.Subagents = cloneSelectorForResolve(entry.Subagents)
		entry.Hooks = cloneSelectorForResolve(entry.Hooks)
		entry.MCP = cloneSelectorForResolve(entry.MCP)
		entry.Collections = cloneSelectorForResolve(entry.Collections)
		entry.Tags = cloneStringsForResolve(entry.Tags)
		entry.Harnesses = cloneStringsForResolve(entry.Harnesses)
		cloned.Registries[i] = entry
	}
	return &cloned
}

// propagateTopLevelCollections adds the top-level Collections.Include names
// to every registry entry's Collections selector (deduplicating). This ensures
// that a collection listed in cfg.Collections.Include is selected from
// whichever registry provides it, not just from entries that inherited the
// collection from their source layer.
func propagateTopLevelCollections(cfg *config.Config) {
	for i := range cfg.Registries {
		sel := cfg.Registries[i].SelectorForKindPtr(ir.KindCollection)
		if sel == nil {
			continue
		}

		existingInclude := make(map[string]struct{}, len(sel.Include))
		for _, name := range sel.Include {
			existingInclude[strings.TrimSpace(name)] = struct{}{}
		}
		for _, name := range cfg.Collections.Include {
			if _, ok := existingInclude[strings.TrimSpace(name)]; !ok {
				sel.Include = append(sel.Include, name)
			}
		}

		existingExclude := make(map[string]struct{}, len(sel.Exclude))
		for _, name := range sel.Exclude {
			existingExclude[strings.TrimSpace(name)] = struct{}{}
		}
		for _, name := range cfg.Collections.Exclude {
			if _, ok := existingExclude[strings.TrimSpace(name)]; !ok {
				sel.Exclude = append(sel.Exclude, name)
			}
		}
	}
}

// expandCollections replaces each KindCollection artifact with its expanded
// member resources. Collections are virtual — they don't materialize. When
// a collection is resolved, we look up its ir.Collection resource, walk its
// Members, and produce one ResolvedArtifact per member with a Via annotation.
// Members that reference a different registry than the collection's source
// are looked up in the catalog for that registry.
func expandCollections(resolved []ResolvedArtifact, cfg *config.Config, catalogs map[string][]catalog.Entry, resources map[string]map[string]ir.Resource, malformed *malformedIndex, localExcludes resourceSelections, collectionExcludeAll bool, localExcludeAllKinds map[ir.ResourceKind]bool, targetSet []hcc.Target) ([]ResolvedArtifact, []error) {
	var out []ResolvedArtifact
	var errs []error
	expanded := false

	// Build a set of directly-resolved resource names (kind:name) from
	// non-collection artifacts. Collection members that are already directly
	// resolved should NOT be duplicated — the direct resolution takes precedence.
	directResolved := make(map[string]struct{})
	for _, artifact := range resolved {
		if artifact.Kind != ir.KindCollection {
			memberKey := ViaMapKey(artifact.Kind, artifact.Name)
			directResolved[memberKey] = struct{}{} // diagnostic suppression compatibility
			for _, partitionKey := range artifactPartitionKeys(memberKey, artifact.Harnesses, artifact.Source) {
				directResolved[partitionKey] = struct{}{}
			}
		}
	}

	// Evaluate local exclusions after binding members to their owning registry;
	// flattening them here would suppress optional machine-scope members.
	excludedResources := make(map[string]struct{})
	for _, name := range cfg.Collections.Exclude {
		excludedResources[ViaMapKey(ir.KindCollection, strings.TrimSpace(name))] = struct{}{}
	}

	// Index catalog entries by (kind, name) for each catalog key so we can
	// look up member resources efficiently.
	catalogIndex := make(map[string]map[resourceSelection]catalog.Entry)
	for key, entries := range catalogs {
		catalogIndex[key] = indexCatalog(entries)
	}

	// Build an index of all discovered KindCollection entries by name so we
	// can resolve includes (nested collections) even when the included
	// collection isn't independently selected. Keep every configured owner so
	// config authority, not catalog-key order, selects nested definitions.
	collectionCandidates := make(map[string][]collectionCandidate)
	for key, entries := range catalogs {
		owners := registryEntriesForCatalogKey(cfg, key)
		for _, entry := range entries {
			if entry.Kind != ir.KindCollection {
				continue
			}
			name := strings.TrimSpace(entry.Name)
			if name == "" {
				continue
			}
			if len(owners) == 0 {
				collectionCandidates[name] = append(collectionCandidates[name], collectionCandidate{
					entry:      entry,
					catalogKey: key,
				})
				continue
			}
			for _, owner := range owners {
				collectionCandidates[name] = append(collectionCandidates[name], collectionCandidate{
					entry:      entry,
					catalogKey: key,
					owner:      owner,
					ownerFound: true,
				})
			}
		}
	}
	allCollectionsByName := make(map[string][]collectionCandidate, len(collectionCandidates))
	for name, candidates := range collectionCandidates {
		slices.SortFunc(candidates, func(a, b collectionCandidate) int {
			if a.catalogKey != b.catalogKey {
				return strings.Compare(a.catalogKey, b.catalogKey)
			}
			if a.owner.URL != b.owner.URL {
				return strings.Compare(a.owner.URL, b.owner.URL)
			}
			return strings.Compare(a.owner.Path, b.owner.Path)
		})
		allCollectionsByName[name] = candidates
	}

	// Expand each resolved collection artifact into member resources.
	for _, artifact := range resolved {
		if artifact.Kind != ir.KindCollection {
			out = append(out, artifact)
			continue
		}

		// This is a resolved collection. Extract the ir.Collection resource.
		irCol, ok := artifact.Resource.(*ir.Collection)
		if !ok || irCol == nil {
			// No IR resource — skip silently (malformed collection).
			continue
		}
		expanded = true

		viaLabel := "collection:" + irCol.Label()

		// Expand members recursively (handles Includes via depth-first walk).
		visited := map[string]struct{}{irCol.Label(): {}}
		expandMembersRecursive(irCol, viaLabel, artifact, cfg, catalogIndex, resources, malformed, allCollectionsByName, directResolved, excludedResources, localExcludes, collectionExcludeAll, localExcludeAllKinds, visited, &out, &errs, targetSet)
	}

	if !expanded {
		return out, errs
	}
	reconciled, reconcileErrs := reconcileExpandedHooks(out)
	errs = append(errs, reconcileErrs...)
	return reconciled, suppressResolvedMemberMisses(errs, directResolved)
}

func reconcileExpandedHooks(artifacts []ResolvedArtifact) ([]ResolvedArtifact, []error) {
	occurrences := make(map[resourceSelection][]skillOccurrence)
	orderToIndex := make(map[int]int, len(artifacts))
	for i, a := range artifacts {
		if !HasAdditiveSources(a.Kind) {
			continue
		}
		order := i
		if a.Via != "" {
			order += len(artifacts)
		}
		orderToIndex[order] = i
		url := a.RegistryURL
		if a.IsMarketplace {
			url = a.MarketplaceURL
		}
		key := resourceSelection{kind: a.Kind, name: a.Name}
		occurrences[key] = append(occurrences[key], skillOccurrence{resolved: a, url: url, order: order})
	}
	if len(occurrences) == 0 {
		return artifacts, nil
	}
	winners, errs := pickWinners(occurrences)
	keep := make(map[int]bool, len(winners))
	for _, winner := range winners {
		if index, ok := orderToIndex[winner.order]; ok {
			keep[index] = true
		}
	}
	filtered := make([]ResolvedArtifact, 0, len(artifacts))
	for i, a := range artifacts {
		if !HasAdditiveSources(a.Kind) || keep[i] {
			filtered = append(filtered, a)
		}
	}
	return filtered, errs
}

// suppressResolvedMemberMisses drops "selected but not found" errors raised
// while expanding a collection member when that exact (kind, name) was in fact
// resolved — directly from a config entry, or from another registry through
// another collection. Reporting the miss then is a false positive, and with an
// all-required candidate set it would fail a sync for a resource that is
// present. This mirrors the cross-registry suppression already applied to
// collection lookups themselves in buildOccurrences.
func suppressResolvedMemberMisses(errs []error, resolvedNames map[string]struct{}) []error {
	if len(errs) == 0 || len(resolvedNames) == 0 {
		return errs
	}
	filtered := errs[:0]
	for _, err := range errs {
		var snf *SelectedNotFoundError
		if errors.As(err, &snf) && snf.Via != "" && !HasAdditiveSources(snf.Kind) {
			if _, resolved := resolvedNames[ViaMapKey(snf.Kind, snf.Name)]; resolved {
				slog.Debug("collection member miss suppressed: resolved elsewhere",
					"kind", snf.Kind.String(), "name", snf.Name, "via", snf.Via)
				continue
			}
		}
		filtered = append(filtered, err)
	}
	return filtered
}

// collectionCandidate pairs a discovered collection definition with the
// configured registry entry that owns its catalog. A collection can be defined
// more than once, so ownership metadata must travel with the definition until
// the recursive include chooses one.
type collectionCandidate struct {
	entry      catalog.Entry
	catalogKey string
	owner      config.RegistryEntry
	ownerFound bool
}

// registryEntriesForCatalogKey returns every configured owner of a catalog.
// Duplicate owners are retained because their source and file precedence may
// differ after include composition.
func registryEntriesForCatalogKey(cfg *config.Config, catalogKey string) []config.RegistryEntry {
	var owners []config.RegistryEntry
	for _, entry := range cfg.Registries {
		if entry.CatalogKey == catalogKey {
			owners = append(owners, entry)
		}
	}
	return owners
}

// nestedCollectionOwnerMatch reports whether a candidate is owned by the
// collection currently being expanded. Matching the parent location first
// keeps an include bound to its defining registry when duplicate definitions
// exist; required and precedence still outrank that locality preference.
func nestedCollectionOwnerMatch(candidate collectionCandidate, parent ResolvedArtifact) bool {
	if !candidate.ownerFound {
		return false
	}
	return normalizedRegistryURL(candidate.owner.URL) == normalizedRegistryURL(parent.RegistryURL) &&
		strings.TrimSpace(candidate.owner.Ref) == strings.TrimSpace(parent.Ref) &&
		normalizedEntryPath(candidate.owner.Path) == normalizedEntryPath(parent.RegistryPath)
}

// chooseNestedCollection selects a definition using policy authority and
// stable ownership tie-breakers.
func chooseNestedCollection(candidates []collectionCandidate, parent ResolvedArtifact) (collectionCandidate, bool) {
	if len(candidates) == 0 {
		return collectionCandidate{}, false
	}
	chosen := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.owner.Required != chosen.owner.Required {
			if candidate.owner.Required {
				chosen = candidate
			}
			continue
		}
		if candidate.ownerFound && chosen.ownerFound {
			precedence := config.CompareEntryPrecedence(candidate.owner.Source, candidate.owner.FilePrecedence, chosen.owner.Source, chosen.owner.FilePrecedence)
			if precedence != 0 {
				if precedence > 0 {
					chosen = candidate
				}
				continue
			}
		} else if candidate.ownerFound != chosen.ownerFound {
			if candidate.ownerFound {
				chosen = candidate
			}
			continue
		}
		candidateMatches := nestedCollectionOwnerMatch(candidate, parent)
		chosenMatches := nestedCollectionOwnerMatch(chosen, parent)
		if candidateMatches != chosenMatches {
			if candidateMatches {
				chosen = candidate
			}
			continue
		}
		candidateURL, chosenURL := normalizedRegistryURL(candidate.owner.URL), normalizedRegistryURL(chosen.owner.URL)
		if candidateURL != chosenURL {
			if candidateURL < chosenURL {
				chosen = candidate
			}
			continue
		}
		candidatePath, chosenPath := normalizedEntryPath(candidate.owner.Path), normalizedEntryPath(chosen.owner.Path)
		if candidatePath != chosenPath {
			if candidatePath < chosenPath {
				chosen = candidate
			}
			continue
		}
		if candidate.catalogKey < chosen.catalogKey {
			chosen = candidate
		}
	}
	return chosen, true
}

// nestedCollectionArtifact carries a candidate definition's owner metadata into
// exclusion checks and recursive expansion. Without this step, an excluded
// higher-authority duplicate can win before its exclusion is noticed.
func nestedCollectionArtifact(candidate collectionCandidate, parent ResolvedArtifact) ResolvedArtifact {
	artifact := ResolvedArtifact{
		Entry:          candidate.entry,
		RegistryURL:    parent.RegistryURL,
		RegistryPath:   parent.RegistryPath,
		Ref:            parent.Ref,
		V4Kind:         parent.V4Kind,
		Source:         parent.Source,
		FilePrecedence: parent.FilePrecedence,
		Required:       parent.Required,
		CatalogKey:     candidate.catalogKey,
		Harnesses:      parent.Harnesses,
	}
	if candidate.ownerFound {
		artifact.RegistryURL = candidate.owner.URL
		artifact.RegistryPath = normalizedEntryPath(candidate.owner.Path)
		artifact.Ref = candidate.owner.Ref
		artifact.V4Kind = candidate.owner.V4Kind
		artifact.Source = candidate.owner.Source
		artifact.FilePrecedence = candidate.owner.FilePrecedence
		artifact.Required = candidate.owner.Required
	}
	return artifact
}

func nestedCollectionCompatible(candidate collectionCandidate, parent ResolvedArtifact, targetSet []hcc.Target) bool {
	if parent.Harnesses != nil && len(parent.Harnesses) == 0 {
		return false
	}
	if !candidate.ownerFound {
		return true
	}
	if len(targetSet) == 0 && candidate.owner.Harnesses == nil && parent.Harnesses == nil {
		return true
	}
	return len(effectiveMemberHarnesses(parent.Harnesses, candidate.owner, true, targetSet)) > 0
}

// chooseNestedCollectionEligible filters definitions before applying authority
// ordering. Required definitions remain immune because
// collectionDefinitionExcluded preserves required artifacts.
func chooseNestedCollectionEligible(candidates []collectionCandidate, parent ResolvedArtifact, excludedResources map[string]struct{}, localExcludes resourceSelections, collectionExcludeAll bool, localExcludeAllKinds map[ir.ResourceKind]bool, entries []config.RegistryEntry, targetSet []hcc.Target) (collectionCandidate, bool) {
	eligible := make([]collectionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !nestedCollectionCompatible(candidate, parent, targetSet) {
			continue
		}
		if collectionDefinitionExcluded(nestedCollectionArtifact(candidate, parent), excludedResources, localExcludes, collectionExcludeAll, localExcludeAllKinds, entries) {
			continue
		}
		eligible = append(eligible, candidate)
	}
	return chooseNestedCollection(eligible, parent)
}

// cloneVisitedWith extends a collection's include ancestry for one branch.
// Keeping a separate map per branch allows a shared DAG node to expand once
// under each sibling while still detecting cycles along either path.
func cloneVisitedWith(visited map[string]struct{}, name string) map[string]struct{} {
	branch := make(map[string]struct{}, len(visited)+1)
	for seen := range visited {
		branch[seen] = struct{}{}
	}
	branch[name] = struct{}{}
	return branch
}

// expandMembersRecursive walks the members of a collection and appends resolved
// artifacts for each. When a collection includes other collections (via
// ir.Collection.Includes), those are expanded recursively with cycle detection.
func expandMembersRecursive(
	col *ir.Collection,
	viaLabel string,
	artifact ResolvedArtifact,
	cfg *config.Config,
	catalogIndex map[string]map[resourceSelection]catalog.Entry,
	resources map[string]map[string]ir.Resource,
	malformed *malformedIndex,
	allCollectionsByName map[string][]collectionCandidate,
	directResolved map[string]struct{},
	excludedResources map[string]struct{},
	localExcludes resourceSelections,
	collectionExcludeAll bool,
	localExcludeAllKinds map[ir.ResourceKind]bool,
	visited map[string]struct{},
	out *[]ResolvedArtifact,
	errs *[]error,
	targetSet []hcc.Target,
) {
	// Expand direct members.
	for _, member := range col.Members {
		memberName := strings.TrimSpace(member.Name)
		if memberName == "" {
			continue
		}

		// Determine the registry and catalog key for this member, and the
		// registry entry that governs its harnesses.
		memberCatalogKey := artifact.CatalogKey
		memberRegistryURL := artifact.RegistryURL
		memberRegistryPath := artifact.RegistryPath
		memberRef := artifact.Ref
		memberV4Kind := artifact.V4Kind
		memberSource := artifact.Source
		memberFilePrecedence := artifact.FilePrecedence
		memberRequired := artifact.Required
		memberEntry := config.RegistryEntry{}
		memberEntryFound := false

		sel := resourceSelection{kind: member.Kind, name: memberName}

		if memberRegistry := strings.TrimSpace(member.Registry); memberRegistry != "" {
			forURL := findRegistryCandidates(cfg, memberRegistry)
			if len(forURL) == 0 {
				*errs = append(*errs, fmt.Errorf("collection %q: %s %q references registry %q which is not present in any config layer", col.Label(), member.Kind, memberName, memberRegistry))
				continue
			}

			// A member pinned to a ref must never bind to another ref's
			// content behind the user's back: only entries at that ref (or
			// left unpinned, as local registries may be) are candidates.
			requestedRef := strings.TrimSpace(member.Ref)
			candidates := candidatesAtRef(forURL, requestedRef)
			if len(candidates) == 0 {
				*errs = append(*errs, &SelectedNotFoundError{
					Kind:           member.Kind,
					Name:           memberName,
					URL:            memberRegistry,
					Ref:            requestedRef,
					Required:       anyLocationRequired(forURL),
					Via:            viaLabel,
					Hint:           fmt.Sprintf("no configured entry for this repository uses ref %q", requestedRef),
					CandidatePaths: candidateLocationLabels(forURL),
				})
				continue
			}

			// Locations the user deliberately scoped away (`exclude_all`,
			// written by `agent-marketplace remove --from`) are not eligible
			// bindings, so a collection can never re-introduce a resource
			// through the back door of a directory the user removed. Required
			// entries stay immune to excludes, as everywhere else.
			eligible := candidatesNotScopedAway(candidates, member.Kind, cfg.Registries)
			if len(eligible) == 0 {
				slog.Debug("collection member skipped: every configured location is excluded",
					"collection", col.Label(), "kind", member.Kind.String(), "name", memberName, "registry", memberRegistry)
				continue
			}
			candidates = eligible

			requestedPath := strings.TrimSpace(member.Path)
			memberPath := normalizedEntryPath(requestedPath)
			var chosen memberEntryCandidate
			if requestedPath != "" {
				atPath := candidatesAtPath(candidates, memberPath)
				allAtPath := atPath
				// A path pin identifies a set of candidate owners, not just a
				// catalog path. Apply the same local exclusion and effective
				// harness policy as path-less binding before choosing a winner.
				atPath = filterCandidates(atPath, func(candidate memberEntryCandidate) bool {
					return !localResourceExcluded(localExcludes, localExcludeAllKinds, member.Kind, memberName, candidate.entry.Required, candidate.entry.Source)
				})
				if len(atPath) > 0 && len(targetSet) > 0 {
					atPath = filterCandidates(atPath, func(candidate memberEntryCandidate) bool {
						if len(effectiveMemberHarnesses(artifact.Harnesses, candidate.entry, true, targetSet)) == 0 {
							appendEmptyHarnessError(candidate.entry, member.Kind, memberName, targetSet, errs)
							return false
						}
						return true
					})
				}
				if len(atPath) == 0 {
					// A local exclusion or incompatible target deliberately
					// removes this optional member; it is not a missing resource.
					if len(allAtPath) > 0 {
						continue
					}
					// The pin is an authoring detail of the collection, not of
					// the consumer's config, so it is reported with the same
					// severity as a path-less miss: soft unless any
					// configured location for the repository is required,
					// which keeps `required: true` policy unbypassable.
					*errs = append(*errs, &SelectedNotFoundError{
						Kind:           member.Kind,
						Name:           memberName,
						URL:            memberRegistry,
						Ref:            strings.TrimSpace(candidates[0].entry.Ref),
						Required:       anyLocationRequired(candidates),
						Via:            viaLabel,
						RequestedPath:  requestedPath,
						CandidatePaths: candidateLocationLabels(candidates),
					})
					continue
				}
				chosen = preferRequired(atPath)
				if strings.TrimSpace(member.Ref) == "" && distinctLocations(atPath) > 1 {
					*errs = append(*errs, &AmbiguousCollectionMemberWarning{
						Kind:       member.Kind,
						Name:       memberName,
						URL:        memberRegistry,
						Ref:        strings.TrimSpace(chosen.entry.Ref),
						Via:        viaLabel,
						Path:       requestedPath,
						Chosen:     locationLabel(chosen.entry, spansMultipleRefs(atPath)),
						Candidates: candidateLocationLabels(atPath),
					})
				}
			} else {
				// A path-less member means "this repository, wherever the
				// resource actually is": select by containment, not by config
				// order, so an unrelated entry for the same monorepo can never
				// absorb the reference.
				containing := candidatesContaining(candidates, catalogIndex, sel)
				// Local excludes are evaluated against the candidate owner before
				// selection. Otherwise an excluded optional repo owner can win
				// over a compatible machine owner and make the member disappear.
				allContaining := containing
				containing = filterCandidates(containing, func(candidate memberEntryCandidate) bool {
					return !localResourceExcluded(localExcludes, localExcludeAllKinds, member.Kind, memberName, candidate.entry.Required, candidate.entry.Source)
				})
				if len(containing) == 0 {
					// A local exclusion deliberately removes this optional member;
					// it is not a missing resource and must not produce a diagnostic.
					if len(allContaining) > 0 {
						continue
					}
					*errs = append(*errs, &SelectedNotFoundError{
						Kind:           member.Kind,
						Name:           memberName,
						URL:            memberRegistry,
						Ref:            strings.TrimSpace(candidates[0].entry.Ref),
						Required:       anyLocationRequired(candidates),
						Via:            viaLabel,
						CandidatePaths: candidateLocationLabels(candidates),
					})
					continue
				}
				if len(targetSet) > 0 {
					compatible := make([]memberEntryCandidate, 0, len(containing))
					for _, candidate := range containing {
						if len(effectiveMemberHarnesses(artifact.Harnesses, candidate.entry, true, targetSet)) == 0 {
							appendEmptyHarnessError(candidate.entry, member.Kind, memberName, targetSet, errs)
							continue
						}
						compatible = append(compatible, candidate)
					}
					if len(compatible) == 0 {
						continue
					}
					containing = compatible
				}
				chosen = preferRequired(containing)
				if distinctLocations(containing) > 1 {
					*errs = append(*errs, &AmbiguousCollectionMemberWarning{
						Kind:       member.Kind,
						Name:       memberName,
						URL:        memberRegistry,
						Ref:        strings.TrimSpace(chosen.entry.Ref),
						Via:        viaLabel,
						Chosen:     locationLabel(chosen.entry, spansMultipleRefs(containing)),
						Candidates: candidateLocationLabels(containing),
					})
				}
			}

			entry := chosen.entry
			memberCatalogKey = chosen.catalogKey
			memberRegistryURL = entry.URL
			memberRegistryPath = memberPath
			if memberRegistryPath == "" {
				memberRegistryPath = normalizedEntryPath(entry.Path)
			}
			memberRef = entry.Ref
			memberV4Kind = entry.V4Kind
			memberSource = entry.Source
			memberFilePrecedence = entry.FilePrecedence
			memberRequired = entry.Required
			memberEntry = entry
			memberEntryFound = true
		} else {
			if entry, _, found := findRegistryEntryForArtifact(cfg, artifact); found {
				memberEntry = entry
				memberEntryFound = true
				// The member inherits the collection's registry location, but
				// its effective identity must still carry the owning entry's
				// ref/path so entry exclusions cannot bleed across refs.
				memberRegistryURL = entry.URL
				memberRegistryPath = normalizedEntryPath(entry.Path)
				memberRef = entry.Ref
				memberV4Kind = entry.V4Kind
				memberSource = entry.Source
				memberFilePrecedence = entry.FilePrecedence
				memberRequired = entry.Required
			}
		}

		memberArtifact := ResolvedArtifact{
			Entry:          catalog.Entry{Kind: member.Kind, Name: memberName},
			RegistryURL:    memberRegistryURL,
			RegistryPath:   memberRegistryPath,
			Ref:            memberRef,
			V4Kind:         memberV4Kind,
			Source:         memberSource,
			FilePrecedence: memberFilePrecedence,
			Required:       memberRequired,
		}
		if collectionMemberShadowedByEntries(memberArtifact, cfg.Registries) {
			continue
		}

		// Compute effective harnesses for the member. Inherit from the parent
		// collection when the member's entry has no per-entry harnesses, and
		// intersect with the machine-wide target set when one is configured.
		memberHarnesses := effectiveMemberHarnesses(artifact.Harnesses, memberEntry, memberEntryFound, targetSet)
		if len(targetSet) > 0 && len(memberHarnesses) == 0 {
			if memberEntryFound {
				appendEmptyHarnessError(memberEntry, member.Kind, memberName, targetSet, errs)
			}
			continue
		}

		// Look up the member in the catalog.
		idx, ok := catalogIndex[memberCatalogKey]
		if !ok {
			*errs = append(*errs, fmt.Errorf("collection %q: no catalog for registry key %q", col.Label(), memberCatalogKey))
			continue
		}

		catEntry, found := idx[sel]
		if !found {
			// Correlate before blaming: a member that failed ingest validation
			// is present upstream, so reporting it as removed would send the
			// user to edit a collection definition that is correct.
			if sme := malformedSelectionError(malformed, memberCatalogKey, member.Kind, memberName,
				memberRegistryURL, memberRegistryPath, memberRef, memberRequired, viaLabel); sme != nil {
				*errs = append(*errs, sme)
				continue
			}
			*errs = append(*errs, &SelectedNotFoundError{
				Kind:     member.Kind,
				Name:     memberName,
				URL:      memberRegistryURL,
				Path:     memberRegistryPath,
				Ref:      memberRef,
				Required: memberRequired,
				Via:      viaLabel,
			})
			continue
		}

		// Skip if already directly resolved (direct resolution takes
		// precedence over collection expansion; no duplicates).

		memberNameKey := ViaMapKey(member.Kind, memberName)

		memberKeys := artifactPartitionKeys(memberNameKey, memberHarnesses, memberSource)
		direct := false
		for _, memberKey := range memberKeys {
			if _, direct = directResolved[memberKey]; direct {
				break
			}
		}
		if direct && !HasAdditiveSources(member.Kind) {

			continue
		}

		if _, excluded := excludedResources[memberNameKey]; excluded && !memberRequired {
			continue
		}
		if localResourceExcluded(localExcludes, localExcludeAllKinds, member.Kind, memberName, memberRequired, memberSource) {
			continue
		}

		// Mark as resolved to prevent duplicates from other collections.
		directResolved[memberNameKey] = struct{}{} // diagnostic suppression compatibility
		for _, memberKey := range memberKeys {
			directResolved[memberKey] = struct{}{}
		}

		*out = append(*out, ResolvedArtifact{
			Entry:          attachResource(catEntry, resources, memberCatalogKey),
			RegistryURL:    memberRegistryURL,
			RegistryPath:   memberRegistryPath,
			Ref:            memberRef,
			V4Kind:         memberV4Kind,
			Source:         memberSource,
			FilePrecedence: memberFilePrecedence,
			Required:       memberRequired,
			CatalogKey:     memberCatalogKey,
			Via:            viaLabel,
			Harnesses:      memberHarnesses,
		})
	}

	// Expand includes (nested collections) recursively.
	for _, includedName := range col.Includes {
		name := strings.TrimSpace(includedName)
		if name == "" {
			continue
		}

		// Cycle detection is path-local: a collection shared by sibling branches
		// is valid, while an include repeated along one branch is a cycle.
		if _, seen := visited[name]; seen {
			*errs = append(*errs, fmt.Errorf("collection %q: cycle detected including %q", col.Label(), name))
			continue
		}
		branchVisited := cloneVisitedWith(visited, name)

		includedEntry, found := chooseNestedCollectionEligible(allCollectionsByName[name], artifact, excludedResources, localExcludes, collectionExcludeAll, localExcludeAllKinds, cfg.Registries, targetSet)
		if !found {
			// A known definition may have been excluded in every eligible
			// location. That is an intentional no-op, not a missing include.
			if len(allCollectionsByName[name]) > 0 {
				slog.Debug("nested collection skipped: all definitions excluded", "collection", name, "via", viaLabel)
				continue
			}
			*errs = append(*errs, fmt.Errorf("collection %q: includes %q which was not found in configured registries", col.Label(), name))
			continue
		}

		includedEntry.entry = attachResource(includedEntry.entry, resources, includedEntry.catalogKey)
		irCol, ok := includedEntry.entry.Resource.(*ir.Collection)
		if !ok || irCol == nil {
			continue
		}

		includedViaLabel := viaLabel + " > " + "collection:" + irCol.Label()
		includedArtifact := nestedCollectionArtifact(includedEntry, artifact)
		if includedEntry.ownerFound {
			ownerHarnesses := resolveEntryHarnesses(includedEntry.owner, targetSet)
			if artifact.Harnesses != nil {
				// A nested owner can narrow, never widen, the effective harnesses
				// selected by its containing collection. A nil owner list means
				// inherit; an explicitly empty list remains an opt-out.
				if ownerHarnesses == nil {
					includedArtifact.Harnesses = artifact.Harnesses
				} else {
					includedArtifact.Harnesses = intersectTargets(ownerHarnesses, artifact.Harnesses)
				}
			} else {
				includedArtifact.Harnesses = ownerHarnesses
			}
		}
		expandMembersRecursive(irCol, includedViaLabel, includedArtifact, cfg, catalogIndex, resources, malformed, allCollectionsByName, directResolved, excludedResources, localExcludes, collectionExcludeAll, localExcludeAllKinds, branchVisited, out, errs, targetSet)
	}
}

// memberEntryCandidate is a configured registry entry whose URL matches the
// `registry:` value of a collection member, paired with its catalog key.
type memberEntryCandidate struct {
	entry      config.RegistryEntry
	catalogKey string
}

// location identifies the physical place a candidate points at. Two entries
// that differ only in ".git" spelling, or that repeat across config layers,
// describe one location and must not look like two.
func (c memberEntryCandidate) location() string {
	return normalizedRegistryURL(c.entry.URL) + "\x00" + normalizedEntryPath(c.entry.Path) + "\x00" + strings.TrimSpace(c.entry.Ref)
}

// normalizedRegistryURL normalizes a registry URL for comparison. It trims
// surrounding whitespace before delegating to config.NormalizeGitURL, which
// does not: a stray space in a hand-edited config must not make a configured
// repository invisible to collection-member binding.
func normalizedRegistryURL(url string) string {
	return config.NormalizeGitURL(strings.TrimSpace(url))
}

// pathLabel renders a registry entry subdirectory for user-facing messages. The
// repository root has an empty path, which would otherwise print as nothing.
func pathLabel(path string) string {
	if p := normalizedEntryPath(path); p != "" {
		return p
	}
	return "<repository root>"
}

// locationLabel renders one candidate location. The ref is appended only when
// the candidate set spans more than one, so the common single-ref case stays
// terse while two refs of the same subdirectory stay distinguishable.
func locationLabel(entry config.RegistryEntry, withRef bool) string {
	label := pathLabel(entry.Path)
	if withRef {
		label += "@" + strings.TrimSpace(entry.Ref)
	}
	return label
}

// spansMultipleRefs reports whether the candidates are pinned to more than one
// ref, in which case a path alone no longer identifies a location.
func spansMultipleRefs(candidates []memberEntryCandidate) bool {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		seen[strings.TrimSpace(candidate.entry.Ref)] = struct{}{}
		if len(seen) > 1 {
			return true
		}
	}
	return false
}

// candidateLocationLabels renders the distinct locations of the given
// candidates, in config order, one label per location as counted by
// distinctLocations.
func candidateLocationLabels(candidates []memberEntryCandidate) []string {
	withRef := spansMultipleRefs(candidates)
	labels := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		label := locationLabel(candidate.entry, withRef)
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels
}

// findRegistryCandidates returns every configured registry entry whose URL
// matches registryURL, in config order. Matching is normalized, so neither a
// ".git" suffix difference nor stray whitespace hides an entry.
func findRegistryCandidates(cfg *config.Config, registryURL string) []memberEntryCandidate {
	norm := normalizedRegistryURL(registryURL)
	var all []memberEntryCandidate
	for _, entry := range cfg.Registries {
		if normalizedRegistryURL(entry.URL) != norm {
			continue
		}
		all = append(all, memberEntryCandidate{entry: entry, catalogKey: entry.CatalogKey})
	}
	return all
}

// candidatesAtRef narrows candidates to those a member pinned to ref may bind
// to: entries at exactly that ref, plus entries that leave the ref unset (local
// registries, where a ref is meaningless). An empty ref means the member did
// not care, so every candidate stays eligible. Binding across a ref the member
// did not ask for would silently deliver different content.
func candidatesAtRef(candidates []memberEntryCandidate, ref string) []memberEntryCandidate {
	if ref == "" {
		return candidates
	}
	return filterCandidates(candidates, func(candidate memberEntryCandidate) bool {
		entryRef := strings.TrimSpace(candidate.entry.Ref)
		return entryRef == ref || entryRef == ""
	})
}

// filterCandidates keeps the candidates that satisfy keep, preserving config
// order. Resolve is a hot path and the common case drops nothing, so the input
// slice is returned unchanged rather than copied when every candidate is kept.
func filterCandidates(candidates []memberEntryCandidate, keep func(memberEntryCandidate) bool) []memberEntryCandidate {
	first := -1
	for i, candidate := range candidates {
		if !keep(candidate) {
			first = i
			break
		}
	}
	if first < 0 {
		return candidates
	}
	filtered := make([]memberEntryCandidate, first, len(candidates)-1)
	copy(filtered, candidates[:first])
	for _, candidate := range candidates[first+1:] {
		if keep(candidate) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// candidatesNotScopedAway drops candidates excluded by their own entry or a
// higher-authority shadow. Required entries remain immune to exclusions.
func candidatesNotScopedAway(candidates []memberEntryCandidate, kind ir.ResourceKind, entries []config.RegistryEntry) []memberEntryCandidate {
	return filterCandidates(candidates, func(candidate memberEntryCandidate) bool {
		if candidate.entry.Required || candidate.entry.SelectorForKind(kind).ExcludeAll {
			return candidate.entry.Required
		}
		for _, shadow := range entries {
			if entryCanShadow(shadow, candidate.entry.Source, candidate.entry.FilePrecedence) &&
				shadow.SelectorForKind(kind).ExcludeAll &&
				registryIdentityMatches(shadow.URL, shadow.Path, shadow.Ref, candidate.entry.URL, candidate.entry.Path, candidate.entry.Ref) {
				return false
			}
		}
		return true
	})
}

// preferRequired chooses among valid bindings. Required entries outrank
// optional entries; otherwise higher config authority wins. Ties retain config
// order.
func preferRequired(candidates []memberEntryCandidate) memberEntryCandidate {
	winner := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.entry.Required && !winner.entry.Required {
			winner = candidate
			continue
		}
		if candidate.entry.Required == winner.entry.Required &&
			config.CompareEntryPrecedence(candidate.entry.Source, candidate.entry.FilePrecedence, winner.entry.Source, winner.entry.FilePrecedence) > 0 {
			winner = candidate
		}
	}
	return winner
}

// normalizedEntryPath returns one comparison form for registry subdirectories.
func normalizedEntryPath(p string) string {
	return config.NormalizeRegistryPath(p)
}

// candidatesAtPath narrows candidates to those configured at exactly path.
func candidatesAtPath(candidates []memberEntryCandidate, path string) []memberEntryCandidate {
	want := normalizedEntryPath(path)
	filtered := make([]memberEntryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if normalizedEntryPath(candidate.entry.Path) == want {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// candidatesContaining narrows candidates to those whose catalog actually
// contains sel. This is what makes a path-less collection member mean "this
// repository, wherever the resource actually is" instead of "whichever entry
// for this repository happens to come first in the merged config".
func candidatesContaining(candidates []memberEntryCandidate, catalogIndex map[string]map[resourceSelection]catalog.Entry, sel resourceSelection) []memberEntryCandidate {
	containing := make([]memberEntryCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		idx, ok := catalogIndex[candidate.catalogKey]
		if !ok {
			continue
		}
		if _, found := idx[sel]; found {
			containing = append(containing, candidate)
		}
	}
	return containing
}

// distinctLocations counts how many physically distinct locations the given
// candidates describe.
func distinctLocations(candidates []memberEntryCandidate) int {
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		seen[candidate.location()] = struct{}{}
	}
	return len(seen)
}

// anyLocationRequired reports whether ANY configured *location* of the
// repository is `required: true`, which is what makes an unbindable member a
// hard (sync-failing) miss rather than a warning.
//
// This used to demand that EVERY location be required. That rule was
// bypassable: `required: true` exists to enforce org-wide security standards,
// and a user could add a non-required entry for the same repository at a
// different path in `.agents/config.local.yaml` — gitignored and personal —
// to turn an org-mandated hard miss into a soft warning. A severity a personal
// config layer can soften is not enforcement, and required registries are
// already immune to every exclude (see AGENTS.md), so the quantifier is `any`.
//
// The accepted cost is escalation: one `required: true` entry anywhere in a
// monorepo makes a miss for a path-less member of that repository hard, even
// when the member has nothing to do with the mandated subdirectory. That is
// bounded — it only affects members the resolver could not bind at all, and a
// member that binds anywhere is unaffected — and it is the safe direction: a
// false hard failure is visible and fixable (pin a `path:`, or configure the
// providing subdirectory), a silently softened policy is neither.
//
// Required-ness is evaluated per distinct location, not per entry: two config
// entries that describe the same location (a ".git" spelling difference, or
// the same entry repeated in another layer) are one location, and it is
// required when any of them says so. The fold is kept even though an
// any-quantifier does not strictly need it, so the rule stays stated in terms
// of locations — the unit every other binding decision here is made in.
//
// The empty set is NOT vacuously anything: no candidates means no required
// location, so the miss stays soft.
func anyLocationRequired(candidates []memberEntryCandidate) bool {
	requiredByLocation := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		loc := candidate.location()
		requiredByLocation[loc] = requiredByLocation[loc] || candidate.entry.Required
	}
	for _, required := range requiredByLocation {
		if required {
			return true
		}
	}
	return false
}

func findRegistryEntryForArtifact(cfg *config.Config, artifact ResolvedArtifact) (config.RegistryEntry, string, bool) {
	wantURL := normalizedRegistryURL(artifact.RegistryURL)
	wantPath := normalizedEntryPath(artifact.RegistryPath)
	wantRef := strings.TrimSpace(artifact.Ref)
	for _, entry := range cfg.Registries {
		if normalizedRegistryURL(entry.URL) == wantURL &&
			normalizedEntryPath(entry.Path) == wantPath &&
			strings.TrimSpace(entry.Ref) == wantRef &&
			entry.Source == artifact.Source &&
			entry.FilePrecedence == artifact.FilePrecedence {
			return entry, entry.CatalogKey, true
		}
	}
	return findRegistryEntry(cfg, artifact.RegistryURL, artifact.Ref, artifact.RegistryPath)
}

// findRegistryEntry finds a config registry entry matching the given URL
// (and optionally ref and path). Returns the entry, its catalog key, and
// whether it was found.
func findRegistryEntry(cfg *config.Config, registryURL, ref, path string) (config.RegistryEntry, string, bool) {
	norm := normalizedRegistryURL(registryURL)
	wantPath := normalizedEntryPath(path)
	for _, entry := range cfg.Registries {
		if normalizedRegistryURL(entry.URL) != norm {
			continue
		}
		if ref != "" && strings.TrimSpace(entry.Ref) != ref {
			continue
		}
		if wantPath != "" && normalizedEntryPath(entry.Path) != wantPath {
			continue
		}
		return entry, entry.CatalogKey, true
	}
	// Fallback: try matching by URL only (for local registries where ref is optional).
	for _, entry := range cfg.Registries {
		if normalizedRegistryURL(entry.URL) != norm {
			continue
		}
		if wantPath != "" && normalizedEntryPath(entry.Path) != wantPath {
			continue
		}
		return entry, entry.CatalogKey, true
	}
	return config.RegistryEntry{}, "", false
}

// ViaMapKey returns a kind-aware key for Via annotations, formatted as
// "<kind>:<name>". This prevents cross-kind collisions.
func ViaMapKey(kind ir.ResourceKind, name string) string {
	return kind.String() + ":" + strings.TrimSpace(name)
}

func artifactPartitionKey(resourceKey string, harnesses []hcc.Target, source config.Source) string {
	return resourceKey + "\x00" + string(artifactTarget(ResolvedArtifact{Harnesses: harnesses})) + "\x00" + fmt.Sprint(source.IsMachineScope())
}

func artifactPartitionKeys(resourceKey string, harnesses []hcc.Target, source config.Source) []string {
	if len(harnesses) == 0 {
		return []string{artifactPartitionKey(resourceKey, harnesses, source)}
	}
	keys := make([]string, 0, len(harnesses))
	for _, target := range harnesses {
		keys = append(keys, artifactPartitionKey(resourceKey, []hcc.Target{target}, source))
	}
	return keys
}

// deferredCollectionError is a per-registry collection resolution failure held
// back until every registry has been walked, because a top-level
// collections.include is propagated to all of them and only fails for real
// when no registry provides it.
type deferredCollectionError struct {
	name string
	err  error
}

func unresolvedSelectionArtifact(entry config.RegistryEntry, selected resourceSelection) ResolvedArtifact {
	return ResolvedArtifact{
		Entry:          catalog.Entry{Kind: selected.kind, Name: selected.name},
		RegistryURL:    entry.URL,
		RegistryPath:   normalizedEntryPath(entry.Path),
		Ref:            entry.Ref,
		V4Kind:         entry.V4Kind,
		Source:         entry.Source,
		FilePrecedence: entry.FilePrecedence,
		Required:       entry.Required,
	}
}

func newSkillOccurrence(entry config.RegistryEntry, entryIndex int, candidate catalog.Entry, resources map[string]map[string]ir.Resource, harnessTargets []hcc.Target, order int) skillOccurrence {
	return skillOccurrence{
		resolved: ResolvedArtifact{
			Entry:          attachResource(candidate, resources, entry.CatalogKey),
			RegistryURL:    entry.URL,
			RegistryPath:   normalizedEntryPath(entry.Path),
			Ref:            entry.Ref,
			V4Kind:         entry.V4Kind,
			Source:         entry.Source,
			FilePrecedence: entry.FilePrecedence,
			Required:       entry.Required,
			CatalogKey:     entry.CatalogKey,
			Harnesses:      harnessTargets,
		},
		source: config.RegistrySelectionKey(entry, entryIndex),
		url:    entry.URL,
		order:  order,
	}
}

// buildOccurrences iterates over registry entries and catalogs to build a map
// of skill occurrences keyed by (kind, name).
func buildOccurrences(cfg *config.Config, catalogs map[string][]catalog.Entry, resources map[string]map[string]ir.Resource, malformed *malformedIndex, targetSet []hcc.Target) (map[resourceSelection][]skillOccurrence, []error) {
	var errs []error
	occurrencesByName := make(map[resourceSelection][]skillOccurrence)
	reportedNotFound := make(map[missingResource]struct{})
	// Track per-registry collection not-found errors separately so they
	// can be suppressed when the collection is found in another registry.
	// propagateTopLevelCollections adds collection includes to every entry,
	// so "not found in registry X" is only a real error if the collection
	// isn't found in ANY registry. Malformed correlation does not change that:
	// a per-registry "failed to load" for a collection resolved elsewhere is
	// the same false positive, and it is a HARD error, so it must defer too.
	var collectionNotFoundErrs []deferredCollectionError
	order := 0

	for entryIndex, entry := range cfg.Registries {
		catalogKey := entry.CatalogKey
		catalog, ok := catalogs[catalogKey]
		if !ok {
			errs = append(errs, fmt.Errorf("missing skill catalog for %s@%s", redact.URL(entry.URL), entry.Ref))
			continue
		}

		harnessTargets := resolveEntryHarnesses(entry, targetSet)
		byName := indexCatalog(catalog)
		selectedResources := resolveSelectedResources(entry, catalog)

		for _, selected := range selectedResources {
			if artifactShadowedByEntries(unresolvedSelectionArtifact(entry, selected), cfg.Registries) {
				continue
			}
			if len(targetSet) > 0 && len(harnessTargets) == 0 {
				appendEmptyHarnessError(entry, selected.kind, selected.name, targetSet, &errs)
				continue
			}

			selectedSkill, found := byName[selected]
			if !found {
				notFoundKey := missingResource{resourceSelection: selected, url: entry.URL, path: entry.Path, ref: entry.Ref}
				if _, reported := reportedNotFound[notFoundKey]; !reported {
					reportedNotFound[notFoundKey] = struct{}{}
					// Correlate before blaming. The resource may be absent from
					// the catalog because the ingester could not load it, in
					// which case the exact validation failure is already known
					// and "removed upstream" is a misdiagnosis.
					var selErr error
					if sme := malformedSelectionError(malformed, catalogKey, selected.kind, selected.name,
						entry.URL, entry.Path, entry.Ref, entry.Required, ""); sme != nil {
						selErr = sme
					} else {
						snf := &SelectedNotFoundError{
							Kind:     selected.kind,
							Name:     selected.name,
							URL:      entry.URL,
							Path:     entry.Path,
							Ref:      entry.Ref,
							Required: entry.Required,
						}
						// Check if the name exists under a different kind and suggest the correct selector.
						if hint := kindMismatchHint(byName, selected); hint != "" {
							snf.Hint = hint
						}
						selErr = snf
					}
					// Defer collection errors: they may be false positives if
					// the collection exists in another registry.
					if selected.kind == ir.KindCollection {
						collectionNotFoundErrs = append(collectionNotFoundErrs, deferredCollectionError{name: selected.name, err: selErr})
					} else {
						errs = append(errs, selErr)
					}
				}
				continue
			}

			occurrencesByName[selected] = append(occurrencesByName[selected], newSkillOccurrence(entry, entryIndex, selectedSkill, resources, harnessTargets, order))
			order++
		}
	}

	// Reconcile collection not-found errors: if the collection was found in
	// at least one registry, suppress per-registry "not found" errors from
	// other registries. A collection referenced in top-level
	// collections.include is virtual — it doesn't belong to any specific
	// registry, so "not found in registry X" is only a real error if the
	// collection doesn't exist anywhere.
	for _, deferred := range collectionNotFoundErrs {
		key := resourceSelection{kind: ir.KindCollection, name: deferred.name}
		if _, found := occurrencesByName[key]; found {
			// Collection exists in another registry — suppress.
			continue
		}
		errs = append(errs, deferred.err)
	}

	return occurrencesByName, errs
}

func newNameCollisionWarning(winner skillOccurrence, occurrences []skillOccurrence) *NameCollisionWarning {
	return &NameCollisionWarning{
		Kind:       winner.resolved.Kind,
		Name:       winner.resolved.Name,
		Winner:     collisionSourceLabel(winner, collisionLabelsNeedRef(occurrences)),
		RunnersUp:  collisionRunnersUpByPath(occurrences, winner),
		WinnerURL:  strings.TrimSpace(winner.url),
		WinnerPath: strings.TrimSpace(winner.resolved.RegistryPath),
		WinnerRef:  strings.TrimSpace(winner.resolved.Ref),
	}
}

func groupHookSourceVersions(occurrences []skillOccurrence) [][]skillOccurrence {
	type sourceKey struct {
		url, path, instance, registry string
		machine                       bool
	}
	indices := make(map[sourceKey]int, len(occurrences))
	groups := make([][]skillOccurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		a := occurrence.resolved
		key := sourceKey{url: normalizedRegistryURL(occurrence.url), path: normalizedEntryPath(a.RegistryPath), machine: a.Source.IsMachineScope()}
		if a.IsLocal() {
			key.url = occurrence.url
		}
		if a.IsMarketplace {
			key.instance = strings.TrimSpace(a.MarketplaceInstanceID)
			key.registry = strings.TrimSpace(a.MarketplaceRegistryID)
		}
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], occurrence)
	}
	return groups
}

// pickWinners deduplicates occurrences and detects multi-registry collisions.
// Returns the winning occurrences in insertion order.
func pickWinners(occurrencesByName map[resourceSelection][]skillOccurrence) ([]skillOccurrence, []error) {
	return pickWinnersReporting(occurrencesByName, true)
}

func pickWinnersReporting(occurrencesByName map[resourceSelection][]skillOccurrence, reportCollisions bool) ([]skillOccurrence, []error) {
	var errs []error
	winners := make([]skillOccurrence, 0, len(occurrencesByName))

	// Sort keys for deterministic error ordering.
	keys := make([]resourceSelection, 0, len(occurrencesByName))
	for key := range occurrencesByName {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b resourceSelection) int {
		if a.kind != b.kind {
			return int(a.kind) - int(b.kind)
		}
		if a.name != b.name {
			return strings.Compare(a.name, b.name)
		}
		if a.empty != b.empty {
			if a.empty {
				return -1
			}
			return 1
		}
		return strings.Compare(string(a.target), string(b.target))
	})

	for _, key := range keys {
		occurrences := occurrencesByName[key]
		deduped := deduplicateOccurrences(occurrences)

		if HasAdditiveSources(key.kind) {
			for _, group := range groupHookSourceVersions(deduped) {
				winner := pickCollisionWinner(group)
				winners = append(winners, winner)
				if len(group) > 1 && reportCollisions {
					warning := newNameCollisionWarning(winner, group)
					slog.Warn("resource collision resolved", "kind", warning.Kind.String(), "name", warning.Name, "winner", warning.Winner, "runners_up", warning.RunnersUp)
					errs = append(errs, warning)
				}
			}
			continue
		}

		if hasMultiRegistryCollision(deduped) {
			// Pick a deterministic winner instead of skipping the resource.
			// Precedence: required > higher source precedence > alphabetically-first URL.
			winner := pickCollisionWinner(deduped)
			if reportCollisions {
				warning := newNameCollisionWarning(winner, occurrences)
				slog.Warn("resource collision resolved", "kind", warning.Kind.String(), "name", warning.Name, "winner", warning.Winner, "runners_up", warning.RunnersUp)
				errs = append(errs, warning)
			}
			winners = append(winners, winner)
			continue
		}

		// When the same skill appears in both a machine-scoped registry
		// (UserPersonal) and a repo-scoped registry (RepoCommitted) via
		// different paths of the same remote, both entries are valid and
		// complementary: the machine-scoped one keeps the skill available
		// globally while the repo-scoped one ensures it is present in the
		// project's harness directory. Emit all scope-separated winners so
		// the materializer can place each in its target directory.
		// However, if entries within the same scope also have different
		// paths, they collide for the shared target directory — emit a
		// warning per scope group so the user knows.
		if len(deduped) > 1 && scopesSeparated(deduped) {
			winners = append(winners, deduped...)
			// Classify all occurrences, including paths deduplicated for output.
			if reportCollisions {
				for _, group := range groupByScope(occurrences) {
					if len(group) > 1 && pathsDiffer(group) {
						winner := pickCollisionWinner(group)
						warning := newNameCollisionWarning(winner, group)
						slog.Warn("resource collision resolved", "kind", warning.Kind.String(), "name", warning.Name, "winner", warning.Winner, "runners_up", warning.RunnersUp)
						errs = append(errs, warning)
					}
				}
			}
		} else if len(deduped) > 1 && pathsDiffer(deduped) {
			// Same URL, different registry paths, same scope. These are
			// distinct entries (e.g. Organization path:teams/dev and
			// UserPersonal path:teams/pm) that happen to provide a
			// same-named skill. Keep all so the user sees both in
			// status/TUI, but emit a warning since the materializer can
			// only install one to the shared target directory.
			winners = append(winners, deduped...)
			if reportCollisions {
				winner := pickCollisionWinner(deduped)
				warning := newNameCollisionWarning(winner, occurrences)
				slog.Warn("resource collision resolved", "kind", warning.Kind.String(), "name", warning.Name, "winner", warning.Winner, "runners_up", warning.RunnersUp)
				errs = append(errs, warning)
			}
		} else {
			winners = append(winners, deduped[0])
		}
	}

	slices.SortFunc(winners, func(a, b skillOccurrence) int {
		if a.order != b.order {
			return a.order - b.order
		}
		if a.partitionOrder != b.partitionOrder {
			return a.partitionOrder - b.partitionOrder
		}
		return strings.Compare(string(artifactTarget(a.resolved)), string(artifactTarget(b.resolved)))
	})

	return winners, errs
}

// harnessTargetsFromEntry converts the entry's Harnesses field (inherited or
// overridden during merge) to []hcc.Target. Returns nil when neither top-level
// nor per-entry harnesses are configured (v1-style), preserving the
// nil-vs-empty distinction.
func harnessTargetsFromEntry(entry config.RegistryEntry) []hcc.Target {
	if entry.Harnesses == nil {
		return nil
	}
	targets := make([]hcc.Target, len(entry.Harnesses))
	for i, h := range entry.Harnesses {
		targets[i] = hcc.Target(h)
	}
	return targets
}

// resolveEntryHarnesses intersects the entry's configured harnesses with the
// machine-wide target set. When targetSet is nil or empty, it returns the
// entry's harnesses unchanged (preserving nil-vs-empty semantics). When the
// entry has no per-entry harnesses and targetSet is non-empty, the target set
// is returned. Otherwise the intersection is returned, preserving the entry's
// harness order.
func resolveEntryHarnesses(entry config.RegistryEntry, targetSet []hcc.Target) []hcc.Target {
	if len(targetSet) == 0 {
		return harnessTargetsFromEntry(entry)
	}
	entryTargets := harnessTargetsFromEntry(entry)
	if entryTargets == nil {
		out := make([]hcc.Target, len(targetSet))
		copy(out, targetSet)
		return out
	}
	return intersectTargets(entryTargets, targetSet)
}

// intersectTargets returns the intersection of a and b, preserving the order
// of a. The result is a new slice; it may be empty but never nil when a is
// non-nil.
func intersectTargets(a, b []hcc.Target) []hcc.Target {
	if len(a) == 0 || len(b) == 0 {
		return []hcc.Target{}
	}
	keep := make(map[hcc.Target]struct{}, len(b))
	for _, t := range b {
		keep[t] = struct{}{}
	}
	out := make([]hcc.Target, 0, len(a))
	for _, t := range a {
		if _, ok := keep[t]; ok {
			out = append(out, t)
		}
	}
	return out
}

// effectiveMemberHarnesses applies the same inheritance and target intersection
// used for a resolved collection member. Keeping this calculation reusable lets
// path-less binding reject owners that cannot materialize before owner selection.
func effectiveMemberHarnesses(parentHarnesses []hcc.Target, entry config.RegistryEntry, entryFound bool, targetSet []hcc.Target) []hcc.Target {
	entryHarnesses := resolveEntryHarnesses(entry, targetSet)
	if len(targetSet) == 0 && (!entryFound || entry.Harnesses == nil) {
		return parentHarnesses
	}
	if parentHarnesses != nil {
		return intersectTargets(entryHarnesses, parentHarnesses)
	}
	return entryHarnesses
}

// formatTargets returns a comma-separated string representation of targets.
func formatTargets(targets []hcc.Target) string {
	if len(targets) == 0 {
		return ""
	}
	s := make([]string, len(targets))
	for i, t := range targets {
		s[i] = string(t)
	}
	return strings.Join(s, ", ")
}

// appendEmptyHarnessError appends an error or warning when an entry has no
// effective harnesses after intersecting with a non-empty target set. It
// reports organization-level required/default resources as hard errors or
// warnings respectively, and silently skips all other sources.
func appendEmptyHarnessError(entry config.RegistryEntry, kind ir.ResourceKind, name string, targetSet []hcc.Target, errs *[]error) {
	slog.Debug("skipping resource with no compatible target harnesses",
		"kind", skill.KindLabel(kind),
		"name", name,
		"source", entry.Source.String(),
		"required", entry.Required,
		"default", entry.Default,
		"entry_harnesses", entry.Harnesses,
		"target_harnesses", formatTargets(targetSet),
	)
	if entry.Source == config.Organization {
		if entry.Required {
			*errs = append(*errs, &IncompatibleOrganizationRequirement{Kind: kind, Name: name, Entry: entry, Targets: targetSet})
			return
		}
		if entry.Default {
			*errs = append(*errs, &IncompatibleOrganizationDefault{Kind: kind, Name: name, Entry: entry, Targets: targetSet})
			return
		}
	}
}

func lookupResource(resources map[string]map[string]ir.Resource, catalogKey string, kind ir.ResourceKind, name string) ir.Resource {
	if resources == nil {
		return nil
	}
	byName, ok := resources[catalogKey]
	if !ok {
		return nil
	}
	return byName[skill.ResourceKey(kind, name)]
}

// attachResource returns the entry with its Resource field populated from
// the fallback map if the entry has a nil Resource. This handles the legacy
// discovery path where entries don't carry IR resources.
func attachResource(e catalog.Entry, resources map[string]map[string]ir.Resource, catalogKey string) catalog.Entry {
	if e.Resource != nil {
		return e
	}
	if r := lookupResource(resources, catalogKey, e.Kind, e.Name); r != nil {
		e.Resource = r
	}
	return e
}

type resourceSelection struct {
	kind   ir.ResourceKind
	name   string
	target hcc.Target // non-empty when this selection belongs to one output partition
	empty  bool       // true for an explicit empty target intent
}

type missingResource struct {
	resourceSelection
	url  string
	path string
	ref  string
}

type resourceSelections []resourceSelection

type SelectedResource struct {
	Kind ir.ResourceKind
	Name string
}

func localResourceSelections(localCfg *config.LocalConfig) resourceSelections {
	if localCfg == nil {
		return nil
	}
	var selections resourceSelections
	for _, kind := range config.AllKinds {
		selections = appendSelections(selections, kind, localCfg.SelectorForKind(kind).Include)
	}
	return selections
}

func localResourceExcludes(localCfg *config.LocalConfig) resourceSelections {
	if localCfg == nil {
		return nil
	}
	var excludes resourceSelections
	for _, kind := range config.AllKinds {
		excludes = appendSelections(excludes, kind, localCfg.SelectorForKind(kind).Exclude)
	}
	return excludes
}

// localResourceExcluded applies local exclusions after owner binding, while
// preserving machine-scope and required resources.
func localResourceExcluded(excludes resourceSelections, excludeAllKinds map[ir.ResourceKind]bool, kind ir.ResourceKind, name string, required bool, source config.Source) bool {
	if required || source.IsMachineScope() {
		return false
	}
	if excludeAllKinds[kind] {
		return true
	}
	selected := resourceSelection{kind: kind, name: strings.TrimSpace(name)}
	for _, excluded := range excludes {
		if excluded == selected {
			return true
		}
	}
	return false
}

// localExcludeAllKinds returns the set of kinds for which the local config
// has ExcludeAll=true at the top level (e.g. skills.exclude_all: true).
func localExcludeAllKinds(localCfg *config.LocalConfig) map[ir.ResourceKind]bool {
	if localCfg == nil {
		return nil
	}
	kinds := make(map[ir.ResourceKind]bool)
	for _, kind := range config.AllKinds {
		if localCfg.SelectorForKind(kind).ExcludeAll {
			kinds[kind] = true
		}
	}
	return kinds
}

func entryResourceExcludes(entry config.RegistryEntry) resourceSelections {
	var excludes resourceSelections
	for _, kind := range config.AllKinds {
		excludes = appendSelections(excludes, kind, entry.SelectorForKind(kind).Exclude)
	}
	return excludes
}

// entryExcludeAllKinds returns the set of kinds for which the entry has
// ExcludeAll=true on its per-kind selector.
func entryExcludeAllKinds(entry config.RegistryEntry) map[ir.ResourceKind]bool {
	kinds := make(map[ir.ResourceKind]bool)
	for _, kind := range config.AllKinds {
		if entry.SelectorForKind(kind).ExcludeAll {
			kinds[kind] = true
		}
	}
	return kinds
}

type localOwnerCandidate struct {
	entry      config.RegistryEntry
	catalogKey string
	resource   catalog.Entry
	harnesses  []hcc.Target
}

func localCandidateArtifact(candidate localOwnerCandidate) ResolvedArtifact {
	return ResolvedArtifact{
		Entry:          candidate.resource,
		RegistryURL:    candidate.entry.URL,
		RegistryPath:   normalizedEntryPath(candidate.entry.Path),
		Ref:            candidate.entry.Ref,
		V4Kind:         candidate.entry.V4Kind,
		Source:         candidate.entry.Source,
		FilePrecedence: candidate.entry.FilePrecedence,
		Required:       candidate.entry.Required,
		CatalogKey:     candidate.catalogKey,
		Harnesses:      candidate.harnesses,
	}
}

func localOwnerExcluded(candidate localOwnerCandidate, kind ir.ResourceKind, name string, cfg *config.Config, localExcludes resourceSelections, localExcludeAllKinds map[ir.ResourceKind]bool) bool {
	if artifactShadowedByEntries(localCandidateArtifact(candidate), cfg.Registries) {
		return true
	}
	selector := candidate.entry.SelectorForKind(kind)
	if !candidate.entry.Required && (selector.ExcludeAll || slices.ContainsFunc(selector.Exclude, func(excluded string) bool {
		return strings.TrimSpace(excluded) == name
	})) {
		return true
	}
	return localResourceExcluded(localExcludes, localExcludeAllKinds, kind, name, candidate.entry.Required, candidate.entry.Source)
}

func localCandidateSource(candidate localOwnerCandidate) config.Source {
	// Local enablement creates a repo-personal selection only for optional
	// repo-scoped owners. Required and machine-scoped policy must not be
	// downgraded into the project scope.
	if candidate.entry.Required || candidate.entry.Source.IsMachineScope() {
		return candidate.entry.Source
	}
	return config.RepoPersonal
}

func applyLocalResources(resolved []ResolvedArtifact, localSelections, localExcludes resourceSelections, localExcludeAllKinds map[ir.ResourceKind]bool, cfg *config.Config, catalogs map[string][]catalog.Entry, resources map[string]map[string]ir.Resource, targetSet []hcc.Target) ([]ResolvedArtifact, []error) {
	if len(localSelections) == 0 {
		return resolved, nil
	}

	resolvedIndexByName := make(map[resourceSelection]int, len(resolved))
	for i, r := range resolved {
		resolvedIndexByName[resourceSelection{kind: r.Kind, name: r.Name}] = i
	}

	var errs []error
	seen := make(map[resourceSelection]struct{}, len(localSelections))
	for _, selected := range localSelections {
		selected.name = strings.TrimSpace(selected.name)
		if selected.name == "" {
			continue
		}
		if _, dup := seen[selected]; dup {
			continue
		}
		seen[selected] = struct{}{}

		if idx, already := resolvedIndexByName[selected]; already {
			// Explicit local enablement should keep optional repo resources
			// repo-scoped, but must never downgrade machine or required policy.
			if !resolved[idx].Required && !resolved[idx].Source.IsMachineScope() {
				resolved[idx].Source = config.RepoPersonal
			}
			continue
		}

		var candidates []localOwnerCandidate
		matched := false
		for i := range cfg.Registries {
			entry := cfg.Registries[i]
			cat, ok := catalogs[entry.CatalogKey]
			if !ok {
				continue
			}
			for _, resource := range cat {
				if resource.Kind != selected.kind || strings.TrimSpace(resource.Name) != selected.name {
					continue
				}
				matched = true
				candidate := localOwnerCandidate{
					entry:      entry,
					catalogKey: entry.CatalogKey,
					resource:   resource,
					harnesses:  resolveEntryHarnesses(entry, targetSet),
				}
				if localOwnerExcluded(candidate, selected.kind, selected.name, cfg, localExcludes, localExcludeAllKinds) {
					continue
				}
				if len(targetSet) > 0 && len(candidate.harnesses) == 0 {
					appendEmptyHarnessError(entry, selected.kind, selected.name, targetSet, &errs)
					continue
				}
				candidates = append(candidates, candidate)
			}
		}

		if len(candidates) == 0 {
			if !matched {
				errs = append(errs, fmt.Errorf("%s %q enabled in local config is not present in any configured registry", skill.KindLabel(selected.kind), selected.name))
			}
			continue
		}

		// Required policy wins first, then source/file authority, then the
		// stable config order. Do not let catalog map iteration affect binding.
		chosen := candidates[0]
		for _, candidate := range candidates[1:] {
			if candidate.entry.Required != chosen.entry.Required {
				if candidate.entry.Required {
					chosen = candidate
				}
				continue
			}
			if precedence := config.CompareEntryPrecedence(candidate.entry.Source, candidate.entry.FilePrecedence, chosen.entry.Source, chosen.entry.FilePrecedence); precedence > 0 {
				chosen = candidate
			}
		}

		chosen.resource = attachResource(chosen.resource, resources, chosen.catalogKey)
		artifact := localCandidateArtifact(chosen)
		artifact.Source = localCandidateSource(chosen)
		resolved = append(resolved, artifact)
		resolvedIndexByName[selected] = len(resolved) - 1
	}

	return resolved, errs
}

// applyPersonalExcludes applies the user's per-kind personal exclude list
// (from config.local.yaml). Required and machine-scope resources are immune;
// local excludes only apply to repo-scoped resources materialized in the repo.
// excludeAllKinds is a set of kinds where ExcludeAll=true was set at the local
// config's top level (skills.exclude_all: true), which suppresses all
// non-required, non-machine-scope resources of that kind.
func applyPersonalExcludes(resolved []ResolvedArtifact, excludes resourceSelections, excludeAllKinds map[ir.ResourceKind]bool) []ResolvedArtifact {
	hasExcludes := len(excludes) > 0
	hasExcludeAll := len(excludeAllKinds) > 0
	if !hasExcludes && !hasExcludeAll {
		return resolved
	}

	excludeSet := make(map[resourceSelection]struct{}, len(excludes))
	for _, ex := range excludes {
		excludeSet[ex] = struct{}{}
	}

	filtered := make([]ResolvedArtifact, 0, len(resolved))
	for _, s := range resolved {
		if s.Required || s.Source.IsMachineScope() {
			reason := "machine_scope"
			if s.Required {
				reason = "required"
			}
			slog.Debug("resolver skipped personal exclude", "kind", skill.KindLabel(s.Kind), "name", s.Name, "source", s.Source.String(), "required", s.Required, "reason", reason)
			filtered = append(filtered, s)
			continue
		}
		if hasExcludeAll && excludeAllKinds[s.Kind] {
			slog.Debug("resolver skipped resource due to personal exclude_all", "kind", skill.KindLabel(s.Kind), "name", s.Name, "source", s.Source.String(), "required", s.Required)
			continue
		}
		if _, excluded := excludeSet[resourceSelection{kind: s.Kind, name: s.Name}]; excluded {
			slog.Debug("resolver skipped resource due to personal exclude", "kind", skill.KindLabel(s.Kind), "name", s.Name, "source", s.Source.String(), "required", s.Required)
			continue
		}
		filtered = append(filtered, s)
	}

	return filtered
}

func validateCrossLayerExcludes(resolved []ResolvedArtifact, entries []config.RegistryEntry, localCfg *config.LocalConfig) []error {
	if len(resolved) == 0 {
		return nil
	}

	// Build (kind, name) → Source map for machine-scope resources.
	machineSourceByKey := make(map[resourceSelection]config.Source)
	for _, resource := range resolved {
		name := strings.TrimSpace(resource.Name)
		if name == "" || !resource.Source.IsMachineScope() {
			continue
		}
		key := resourceSelection{kind: resource.Kind, name: name}
		if existing, ok := machineSourceByKey[key]; !ok || resource.Source.Precedence() > existing.Precedence() {
			machineSourceByKey[key] = resource.Source
		}
	}
	if len(machineSourceByKey) == 0 {
		return nil
	}

	var warns []error
	warnedLocal := make(map[resourceSelection]struct{})
	if localCfg != nil {
		for _, excluded := range localResourceExcludes(localCfg) {
			source, machineScoped := machineSourceByKey[excluded]
			if !machineScoped {
				continue
			}
			if _, alreadyWarned := warnedLocal[excluded]; alreadyWarned {
				continue
			}
			warns = append(warns, &IneffectiveExcludeWarning{
				Scope:  "local",
				Name:   excluded.name,
				Source: source,
			})
			warnedLocal[excluded] = struct{}{}
		}
	}

	warnedRepo := make(map[resourceSelection]struct{})
	for _, entry := range entries {
		if entry.Source != config.RepoCommitted && entry.Source != config.RepoPersonal {
			continue
		}
		for _, excluded := range entryResourceExcludes(entry) {
			source, machineScoped := machineSourceByKey[excluded]
			if !machineScoped {
				continue
			}
			if _, alreadyWarned := warnedRepo[excluded]; alreadyWarned {
				continue
			}
			warns = append(warns, &IneffectiveExcludeWarning{
				Scope:  "repo",
				Name:   excluded.name,
				Source: source,
			})
			warnedRepo[excluded] = struct{}{}
		}
	}

	return warns
}

// entryCanShadow reports whether an entry has authority to suppress a target.
// Repository layers cannot suppress machine-scoped targets, and only user
// config can suppress organization targets.
func entryCanShadow(entry config.RegistryEntry, source config.Source, filePrecedence int) bool {
	if config.CompareEntryPrecedence(entry.Source, entry.FilePrecedence, source, filePrecedence) <= 0 {
		return false
	}
	if entry.Source != source && source.IsMachineScope() && (entry.Source == config.RepoCommitted || entry.Source == config.RepoPersonal) {
		return false
	}
	if entry.Source != source && source == config.Organization && entry.Source != config.UserPersonal {
		return false
	}
	return true
}

func artifactShadowedByEntries(s ResolvedArtifact, entries []config.RegistryEntry) bool {
	if s.Required {
		return false
	}
	for _, entry := range entries {
		if !entryCanShadow(entry, s.Source, s.FilePrecedence) {
			continue
		}
		for _, ex := range entryResourceExcludes(entry) {
			if ex.kind != s.Kind || strings.TrimSpace(ex.name) != s.Name {
				continue
			}
			if entry.V4Decisions == nil && entry.Source != s.Source && normalizedRegistryURL(entry.URL) != normalizedRegistryURL(s.RegistryURL) {
				return true
			}
			if registryIdentityMatches(entry.URL, entry.Path, entry.Ref, s.RegistryURL, s.RegistryPath, s.Ref) {
				return true
			}
		}
		if entry.SelectorForKind(s.Kind).ExcludeAll &&
			registryIdentityMatches(entry.URL, entry.Path, entry.Ref, s.RegistryURL, s.RegistryPath, s.Ref) {
			return true
		}
	}
	return false
}

// collectionMemberShadowedByEntries also honors exclusions in the entry that
// binds the member; other entries must have higher authority.
func collectionMemberShadowedByEntries(member ResolvedArtifact, entries []config.RegistryEntry) bool {
	if member.Required {
		return false
	}
	for _, entry := range entries {
		sameFile := entry.Source == member.Source && entry.FilePrecedence == member.FilePrecedence
		if (!sameFile && !entryCanShadow(entry, member.Source, member.FilePrecedence)) ||
			!registryIdentityMatches(entry.URL, entry.Path, entry.Ref, member.RegistryURL, member.RegistryPath, member.Ref) {
			continue
		}
		selector := entry.SelectorForKind(member.Kind)
		if selector.ExcludeAll {
			return true
		}
		for _, name := range selector.Exclude {
			if strings.TrimSpace(name) == member.Name {
				return true
			}
		}
	}
	return false
}

// collectionDefinitionExcluded prevents an included definition from bypassing
// collection exclusions before its members are expanded.
func collectionDefinitionExcluded(collection ResolvedArtifact, excludedResources map[string]struct{}, localExcludes resourceSelections, collectionExcludeAll bool, localExcludeAllKinds map[ir.ResourceKind]bool, entries []config.RegistryEntry) bool {
	if collection.Required {
		return false
	}
	if _, excluded := excludedResources[ViaMapKey(ir.KindCollection, collection.Name)]; excluded {
		return true
	}
	if localResourceExcluded(localExcludes, localExcludeAllKinds, ir.KindCollection, collection.Name, collection.Required, collection.Source) {
		return true
	}
	if collectionExcludeAll {
		return true
	}
	return collectionMemberShadowedByEntries(collection, entries)
}

func filterShadowedOccurrences(byName map[resourceSelection][]skillOccurrence, entries []config.RegistryEntry) {
	for key, occurrences := range byName {
		filtered := occurrences[:0]
		for _, occurrence := range occurrences {
			if !artifactShadowedByEntries(occurrence.resolved, entries) {
				filtered = append(filtered, occurrence)
			}
		}
		if len(filtered) == 0 {
			delete(byName, key)
		} else {
			byName[key] = filtered
		}
	}
}

// applyCrossLayerExcludes removes artifacts suppressed by higher-authority
// entries while preserving required artifacts.
func applyCrossLayerExcludes(resolved []ResolvedArtifact, entries []config.RegistryEntry) []ResolvedArtifact {
	if len(resolved) == 0 || len(entries) == 0 {
		return resolved
	}

	filtered := make([]ResolvedArtifact, 0, len(resolved))
	for _, artifact := range resolved {
		if !artifactShadowedByEntries(artifact, entries) {
			filtered = append(filtered, artifact)
		}
	}

	return filtered
}

func indexCatalog(entries []catalog.Entry) map[resourceSelection]catalog.Entry {
	byName := make(map[resourceSelection]catalog.Entry, len(entries))

	for _, candidate := range entries {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			continue
		}

		key := resourceSelection{kind: candidate.Kind, name: name}
		if _, exists := byName[key]; exists {
			continue
		}

		byName[key] = candidate
	}

	return byName
}

// kindSelectorKey maps a resource kind to the canonical YAML config key used in
// per-registry KindSelector fields (e.g. skills, commands, subagents, hooks, mcp).
var kindSelectorKey = map[ir.ResourceKind]string{
	ir.KindSkill:      "skills",
	ir.KindCommand:    "commands",
	ir.KindSubagent:   "subagents",
	ir.KindHook:       "hooks",
	ir.KindMcpServer:  "mcp",
	ir.KindCollection: "collections",
}

// kindMismatchHint checks whether the given name exists under a different kind
// in the catalog index. If so, it returns a hint string suggesting the correct
// selector field (e.g. "resource exists as command; use commands: include: [X]").
func kindMismatchHint(byName map[resourceSelection]catalog.Entry, selected resourceSelection) string {
	for _, kind := range config.AllKinds {
		if kind == selected.kind {
			continue
		}
		if _, ok := byName[resourceSelection{kind: kind, name: selected.name}]; ok {
			label := skill.KindLabel(kind)
			selectorKey := kindSelectorKey[kind]
			return fmt.Sprintf("resource exists as %s; use %s: include: [%s]", label, selectorKey, selected.name)
		}
	}
	return ""
}

// NamesSelected returns the set of skill names that the given registry entry
// selects from its catalog. Uses the same selection logic as Resolve but
// without cross-registry deduplication, collision detection, or error
// reporting. Callers use this to compute per-scope enablement (e.g., which
// skills are enabled via a specific registry vs. another).
func NamesSelected(entry config.RegistryEntry, catalog []catalog.Entry) []string {
	selected := resolveSelectedResources(entry, catalog)
	names := make([]string, 0, len(selected))
	for _, s := range selected {
		names = append(names, s.name)
	}
	return names
}

func ResourcesSelected(entry config.RegistryEntry, catalog []catalog.Entry) []SelectedResource {
	selected := resolveSelectedResources(entry, catalog)
	resources := make([]SelectedResource, 0, len(selected))
	for _, s := range selected {
		resources = append(resources, SelectedResource{Kind: s.kind, Name: s.name})
	}
	return resources
}

func resolveSelectedResources(entry config.RegistryEntry, catalog []catalog.Entry) resourceSelections {
	selected := make(map[resourceSelection]struct{})

	// v4 all:true is additive, including when the group also has explicit
	// includes or tags. Legacy required/default selection retains its historical
	// explicit-selection guard.
	autoSelectAll := entry.V4All || ((entry.Required || entry.Default) && !hasExplicitSelection(entry))
	if autoSelectAll {
		for _, candidate := range catalog {
			name := strings.TrimSpace(candidate.Name)
			if name == "" {
				continue
			}
			selected[resourceSelection{kind: candidate.Kind, name: name}] = struct{}{}
		}
	}

	// Tag selection participates in the ordered v4 stream: exclusions must be
	// able to suppress tagged resources just as they suppress explicit includes.
	for _, candidate := range catalog {
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			continue
		}
		if config.HasMatchingTag(candidate.Tags, entry.Tags) {
			selected[resourceSelection{kind: candidate.Kind, name: name}] = struct{}{}
		}
	}

	// V4 carries an ordered decision stream. Apply it after dynamic tag/all
	// selection so an exclusion can suppress inherited selections while a later
	// include can explicitly re-enable the same resource.
	if entry.V4Decisions != nil {
		for _, decision := range entry.V4Decisions {
			switch decision.Action {
			case config.V4SelectionExplicit:
				if decision.Request == nil {
					continue
				}
				selected[resourceSelection{kind: v4DecisionKind(decision.Request.Kind), name: decision.Request.Name}] = struct{}{}
			case config.V4SelectionExclude:
				if entry.Required {
					continue
				}
				for candidate := range selected {
					if v4DecisionExclusionMatches(decision.Exclusion, candidate) {
						delete(selected, candidate)
					}
				}
			}
		}
	} else {
		selections := resourceSelections{}
		for _, kind := range config.AllKinds {
			selections = appendSelections(selections, kind, entry.SelectorForKind(kind).Include)
		}
		for _, s := range selections {
			selected[s] = struct{}{}
		}
	}

	// required:true is immune to entry-level excludes; default:true and explicit
	// selections respect them. V4's ordered stream already applied its excludes.
	if !entry.Required && entry.V4Decisions == nil {
		excludeAllKinds := entryExcludeAllKinds(entry)
		for _, excluded := range entryResourceExcludes(entry) {
			for candidate := range selected {
				if candidate.kind == excluded.kind && candidate.name == strings.TrimSpace(excluded.name) {
					delete(selected, candidate)
				}
			}
		}
		for kind := range excludeAllKinds {
			for candidate := range selected {
				if candidate.kind == kind {
					delete(selected, candidate)
				}
			}
		}
	}

	resources := make(resourceSelections, 0, len(selected))
	for selected := range selected {
		resources = append(resources, selected)
	}
	slices.SortFunc(resources, func(a, b resourceSelection) int {
		if a.kind != b.kind {
			return int(a.kind) - int(b.kind)
		}
		return strings.Compare(a.name, b.name)
	})
	orderWindsurfHookSelections(resources, catalog)
	return resources
}

func orderWindsurfHookSelections(resources resourceSelections, entries []catalog.Entry) {
	var ranks map[resourceSelection]int
	for i, candidate := range entries {
		if candidate.Kind != ir.KindHook || candidate.SourceHarness != string(hcc.TargetWindsurf) {
			continue
		}
		if ranks == nil {
			ranks = make(map[resourceSelection]int)
		}
		key := resourceSelection{kind: candidate.Kind, name: strings.TrimSpace(candidate.Name)}
		if _, exists := ranks[key]; !exists {
			ranks[key] = i
		}
	}
	if len(ranks) < 2 {
		return
	}
	var positions []int
	var hooks resourceSelections
	for i, resource := range resources {
		if _, ordered := ranks[resource]; ordered {
			positions = append(positions, i)
			hooks = append(hooks, resource)
		}
	}
	slices.SortFunc(hooks, func(a, b resourceSelection) int { return ranks[a] - ranks[b] })
	for i, position := range positions {
		resources[position] = hooks[i]
	}
}

func appendSelections(out resourceSelections, kind ir.ResourceKind, names []string) resourceSelections {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, resourceSelection{kind: kind, name: name})
	}
	return out
}

func v4DecisionKind(kind string) ir.ResourceKind {
	switch kind {
	case "skill":
		return ir.KindSkill
	case "command":
		return ir.KindCommand
	case "subagent":
		return ir.KindSubagent
	case "hook":
		return ir.KindHook
	case "mcp":
		return ir.KindMcpServer
	case "collection":
		return ir.KindCollection
	default:
		return ir.ResourceKind(-1)
	}
}

func v4DecisionExclusionMatches(exclusion string, selected resourceSelection) bool {
	if exclusion == "*" {
		return true
	}
	parts := strings.Split(strings.TrimSuffix(exclusion, "/*"), "/")
	if strings.HasSuffix(exclusion, "/*") {
		if len(parts) == 1 {
			return v4DecisionKind(parts[0]) == selected.kind
		}
		if len(parts) == 2 {
			return v4DecisionKind(parts[1]) == selected.kind
		}
		return false
	}
	if len(parts) == 2 {
		return v4DecisionKind(parts[0]) == selected.kind && parts[1] == selected.name
	}
	if len(parts) == 3 {
		return parts[2] == selected.name && v4DecisionKind(parts[1]) == selected.kind
	}
	return false
}

func hasExplicitSelection(entry config.RegistryEntry) bool {
	// A v4 decision stream is explicit only when it contains an include; an
	// exclude-only group must not disable required/default dynamic selection.
	for _, decision := range entry.V4Decisions {
		if decision.Action == config.V4SelectionExplicit && decision.Request != nil {
			return true
		}
	}
	// A selector is explicit only when it has a non-empty Include list,
	// not merely an Exclude. Exclude-only selectors should not disable autoSelectAll.
	for _, kind := range config.AllKinds {
		if len(entry.SelectorForKind(kind).Include) > 0 {
			return true
		}
	}
	return entry.Tags != nil
}

// deduplicateOccurrences merges occurrences from the same logical registry
// source within one installation scope. Same-URL/ref entries with overlapping
// paths are merged; unrelated paths and machine/repo scopes remain distinct.
// Marketplace instance and registry identity are also part of the logical
// source. Within a merged group, required policy, path specificity, and config
// precedence choose the surviving occurrence.
func deduplicateOccurrences(occurrences []skillOccurrence) []skillOccurrence {
	if len(occurrences) < 2 {
		return occurrences
	}

	type logicalKey struct {
		catalogKey            string
		normalizedURL         string
		ref                   string
		path                  string // registry sub-path, keeps different-path entries separate
		machineScope          bool   // machine and repo occurrences materialize separately
		marketplaceInstanceID string
		marketplaceRegistryID string
	}

	byKey := make(map[logicalKey][]skillOccurrence, len(occurrences))
	for _, occ := range occurrences {
		// Use CatalogKey as primary identity, but also group by normalized
		// URL+ref+path (plus Marketplace instance/registry identity) for
		// equivalent remote entries that differ only by cache-key spelling.
		// Scope is part of identity: identical physical locations in machine and
		// repo scopes are complementary, not duplicates.
		lk := logicalKey{catalogKey: occ.resolved.CatalogKey, machineScope: occ.resolved.Source.IsMachineScope()}
		if !occ.resolved.IsLocal() {
			lk.normalizedURL = normalizedRegistryURL(occ.url)
			lk.ref = strings.TrimSpace(occ.resolved.Ref)
			lk.path = normalizedEntryPath(occ.resolved.RegistryPath)
			if occ.resolved.IsMarketplace {
				lk.marketplaceInstanceID = strings.TrimSpace(occ.resolved.MarketplaceInstanceID)
				lk.marketplaceRegistryID = strings.TrimSpace(occ.resolved.MarketplaceRegistryID)
			}

		} else {
			// For local registries, use the URL (file path) as the registry
			// identity so overlapping-path detection can work.
			lk.normalizedURL = occ.url
			lk.path = normalizedEntryPath(occ.resolved.RegistryPath)
		}
		byKey[lk] = append(byKey[lk], occ)
	}

	// Merge groups that share normalizedURL+ref+path and Marketplace identity
	// but have different catalogKeys (e.g. "repo" and "repo.git" producing
	// different cache hashes). Path and Marketplace identity are included so
	// different sub-directories, sources, or instances remain distinct.
	type normKey struct {
		normalizedURL         string
		ref                   string
		path                  string
		machineScope          bool
		marketplaceInstanceID string
		marketplaceRegistryID string
	}
	compareLogicalKeys := func(a, b logicalKey) int {
		if a.catalogKey != b.catalogKey {
			return strings.Compare(a.catalogKey, b.catalogKey)
		}
		if a.normalizedURL != b.normalizedURL {
			return strings.Compare(a.normalizedURL, b.normalizedURL)
		}
		if a.ref != b.ref {
			return strings.Compare(a.ref, b.ref)
		}
		if a.path != b.path {
			return strings.Compare(a.path, b.path)
		}
		if a.machineScope != b.machineScope {
			if a.machineScope {
				return 1
			}
			return -1
		}
		if a.marketplaceInstanceID != b.marketplaceInstanceID {
			return strings.Compare(a.marketplaceInstanceID, b.marketplaceInstanceID)
		}
		return strings.Compare(a.marketplaceRegistryID, b.marketplaceRegistryID)
	}
	compareNormKeys := func(a, b normKey) int {
		if a.normalizedURL != b.normalizedURL {
			return strings.Compare(a.normalizedURL, b.normalizedURL)
		}
		if a.ref != b.ref {
			return strings.Compare(a.ref, b.ref)
		}
		if a.path != b.path {
			return strings.Compare(a.path, b.path)
		}
		if a.machineScope != b.machineScope {
			if a.machineScope {
				return 1
			}
			return -1
		}
		if a.marketplaceInstanceID != b.marketplaceInstanceID {
			return strings.Compare(a.marketplaceInstanceID, b.marketplaceInstanceID)
		}
		return strings.Compare(a.marketplaceRegistryID, b.marketplaceRegistryID)
	}
	normGroups := make(map[normKey][]logicalKey)
	for lk := range byKey {
		if lk.normalizedURL == "" {
			continue
		}
		nk := normKey{
			normalizedURL:         lk.normalizedURL,
			ref:                   lk.ref,
			path:                  lk.path,
			machineScope:          lk.machineScope,
			marketplaceInstanceID: lk.marketplaceInstanceID,
			marketplaceRegistryID: lk.marketplaceRegistryID,
		}
		normGroups[nk] = append(normGroups[nk], lk)
	}

	// Sort map-derived groups before merging so the surviving logical key and
	// occurrence order do not vary between Resolve calls.
	normKeys := make([]normKey, 0, len(normGroups))
	for nk := range normGroups {
		normKeys = append(normKeys, nk)
	}
	slices.SortFunc(normKeys, compareNormKeys)
	for _, nk := range normKeys {
		keys := normGroups[nk]
		if len(keys) < 2 {
			continue
		}
		slices.SortFunc(keys, compareLogicalKeys)
		normGroups[nk] = keys
		// Merge all occurrences from these keys into the first key's group.
		target := keys[0]
		for _, src := range keys[1:] {
			byKey[target] = append(byKey[target], byKey[src]...)
			delete(byKey, src)
		}
	}

	// Overlapping-path merge: when two entries share the same
	// normalizedURL+ref but one path is a component-wise prefix of the
	// other, they are the same physical registry — the parent path
	// discovers everything the child does (and more). These are NOT
	// genuine collisions and must be merged. The more-specific (child)
	// path wins for any skill that appears in both, because the child
	// entry explicitly targets the sub-directory.
	//
	// However, overlapping-path occurrences that span different scopes
	// (machine-scope vs repo-scope) are complementary, not duplicates:
	// the machine-scoped one installs globally while the repo-scoped one
	// installs into the project. These must NOT be merged — the
	// materializer needs both artifacts.
	type urlRefKey struct {
		normalizedURL         string
		ref                   string
		marketplaceInstanceID string
		marketplaceRegistryID string
	}
	// Collect all logical keys per URL+ref, then find overlapping groups.
	urlRefGroups := make(map[urlRefKey][]logicalKey)
	for lk := range byKey {
		if lk.normalizedURL == "" {
			continue
		}
		urk := urlRefKey{
			normalizedURL:         lk.normalizedURL,
			ref:                   lk.ref,
			marketplaceInstanceID: lk.marketplaceInstanceID,
			marketplaceRegistryID: lk.marketplaceRegistryID,
		}
		urlRefGroups[urk] = append(urlRefGroups[urk], lk)
	}
	urlRefKeys := make([]urlRefKey, 0, len(urlRefGroups))
	for key := range urlRefGroups {
		urlRefKeys = append(urlRefKeys, key)
	}
	slices.SortFunc(urlRefKeys, func(a, b urlRefKey) int {
		if a.normalizedURL != b.normalizedURL {
			return strings.Compare(a.normalizedURL, b.normalizedURL)
		}
		return strings.Compare(a.ref, b.ref)
	})
	for _, urlRef := range urlRefKeys {
		lks := urlRefGroups[urlRef]
		if len(lks) < 2 {
			continue
		}
		slices.SortFunc(lks, compareLogicalKeys)
		// For each pair, check if one path is a component-wise prefix of the other.
		// Only merge same-scope overlapping paths; cross-scope pairs are kept separate
		// (they are complementary installation targets, not duplicates).
		var mergePairs [][2]logicalKey // [0]=parent (absorbed), [1]=child (survives)
		for i := 0; i < len(lks); i++ {
			for j := i + 1; j < len(lks); j++ {
				a, b := lks[i], lks[j]
				// Check if paths overlap AND all occurrences are same-scope.
				var isPrefix bool
				var parent, child logicalKey
				if isPathPrefix(a.path, b.path) {
					isPrefix = true
					parent, child = a, b
				} else if isPathPrefix(b.path, a.path) {
					isPrefix = true
					parent, child = b, a
				}
				if !isPrefix {
					continue
				}
				// Only merge if ALL occurrences in both groups are same-scope.
				// If any occurrence in one group has a different scope than
				// any in the other, they are complementary targets, not duplicates.
				sameScope := true
				for _, occA := range byKey[a] {
					for _, occB := range byKey[b] {
						if occA.resolved.Source.IsMachineScope() != occB.resolved.Source.IsMachineScope() {
							sameScope = false
							break
						}
					}
					if !sameScope {
						break
					}
				}
				if !sameScope {
					continue
				}
				mergePairs = append(mergePairs, [2]logicalKey{parent, child})
			}
		}
		// Sort pair application as well as candidate discovery. This matters for
		// transitive overlaps (root, root/a, root/a/b): map iteration must not
		// choose which intermediate group survives.
		slices.SortFunc(mergePairs, func(a, b [2]logicalKey) int {
			if cmp := compareLogicalKeys(a[0], b[0]); cmp != 0 {
				return cmp
			}
			return compareLogicalKeys(a[1], b[1])
		})
		// Apply merges: parent occurrences are absorbed into the child group.
		// Within each merged group, pick the child occurrence as winner
		// (more-specific path takes precedence).
		for _, pair := range mergePairs {
			parent, child := pair[0], pair[1]
			parentOccs, parentOk := byKey[parent]
			childOccs, childOk := byKey[child]
			if !parentOk || !childOk {
				continue
			}
			// Merge parent occurrences into child group.
			byKey[child] = append(childOccs, parentOccs...)
			delete(byKey, parent)
		}
	}

	// Collect and sort logical keys for deterministic iteration over byKey.
	sortedKeys := make([]logicalKey, 0, len(byKey))
	for lk := range byKey {
		sortedKeys = append(sortedKeys, lk)
	}
	slices.SortFunc(sortedKeys, compareLogicalKeys)

	deduped := make([]skillOccurrence, 0, len(byKey))
	for _, lk := range sortedKeys {
		group := byKey[lk]
		winner := group[0]
		for _, occ := range group[1:] {
			if occ.resolved.Required && !winner.resolved.Required {
				winner = occ
				continue
			}
			if occ.resolved.Required == winner.resolved.Required {
				// For a same-scope physical overlap, specificity is policy's
				// second tie-breaker: a child path wins over its parent even
				// when the parent came from a higher-authority config layer.
				// This preserves the documented deeper-path-wins behavior.
				specificity := compareOverlappingPathSpecificity(occ, winner)
				if specificity > 0 {
					winner = occ
					continue
				}
				if specificity < 0 {
					continue
				}
				precedence := config.CompareEntryPrecedence(occ.resolved.Source, occ.resolved.FilePrecedence, winner.resolved.Source, winner.resolved.FilePrecedence)
				if precedence > 0 {
					winner = occ
					continue
				}
				if precedence == 0 && HasAdditiveSources(occ.resolved.Kind) && occ.resolved.Via == "" && winner.resolved.Via != "" {
					winner = occ
				}
			}
		}
		deduped = append(deduped, winner)
	}

	slices.SortFunc(deduped, func(a, b skillOccurrence) int {
		return a.order - b.order
	})

	return deduped
}

// IsLocal reports whether the resolved artifact came from a local (non-git) registry.
func (r ResolvedArtifact) IsLocal() bool {
	return r.RegistryURL != "" && config.RegistryEntry{URL: r.RegistryURL, Ref: r.Ref, V4Kind: r.V4Kind}.IsLocal()
}

// IsGlobal reports whether the artifact is installed into the machine-wide
// skills directory. This is a convenience alias for Source.IsMachineScope()
// kept for callers that have not yet migrated to the Source field.
//
// Deprecated: use Source.IsMachineScope() directly.
func (r ResolvedArtifact) IsGlobal() bool { return r.Source.IsMachineScope() }

// scopesSeparated reports whether occurrences span both machine-scope and
// repo-scope. When true the occurrences are complementary, not conflicting:
// the machine-scoped one serves global availability while the repo-scoped one
// ensures the skill is present in the project's harness directory.
func scopesSeparated(occurrences []skillOccurrence) bool {
	hasMachine, hasRepo := false, false
	for _, occ := range occurrences {
		if occ.resolved.Source.IsMachineScope() {
			hasMachine = true
		} else {
			hasRepo = true
		}
	}
	return hasMachine && hasRepo
}

// groupByScope splits occurrences into machine-scoped and repo-scoped groups.
// Each group contains entries that contend for the same install target
// directory, so within-group path collisions need separate warnings.
func groupByScope(occurrences []skillOccurrence) [][]skillOccurrence {
	var machine, repo []skillOccurrence
	for _, occ := range occurrences {
		if occ.resolved.Source.IsMachineScope() {
			machine = append(machine, occ)
		} else {
			repo = append(repo, occ)
		}
	}
	var groups [][]skillOccurrence
	if len(machine) > 0 {
		groups = append(groups, machine)
	}
	if len(repo) > 0 {
		groups = append(groups, repo)
	}
	return groups
}

// isPathPrefix reports whether parent is a component-wise path prefix of child.
// For example, isPathPrefix("teams/dev", "teams/dev/eng") is true,
// but isPathPrefix("teams/dev", "teams/dev-tools") is false.
// An empty parent is a prefix of any child (root path discovers everything).
func isPathPrefix(parent, child string) bool {
	if parent == child {
		return false // same path is not a "prefix" relationship
	}
	if parent == "" {
		return true // root path is prefix of everything
	}
	if child == "" {
		return false // non-root cannot be prefix of root
	}
	if !strings.HasPrefix(child, parent) {
		return false
	}
	// Ensure the prefix ends at a component boundary.
	// "teams/dev" is a prefix of "teams/dev/eng" (next char is '/'),
	// but "teams/dev" is NOT a prefix of "teams/dev-tools" (next char is '-').
	return len(child) > len(parent) && child[len(parent)] == '/'
}

// compareOverlappingPathSpecificity reports whether a is the deeper member of
// a same-URL/ref overlapping pair. It returns 1 when a is the child, -1 when
// b is the child, and 0 for unrelated or equal paths.
func compareOverlappingPathSpecificity(a, b skillOccurrence) int {
	if a.resolved.Source.IsMachineScope() != b.resolved.Source.IsMachineScope() ||
		normalizedRegistryURL(a.url) != normalizedRegistryURL(b.url) ||
		strings.TrimSpace(a.resolved.Ref) != strings.TrimSpace(b.resolved.Ref) {
		return 0
	}
	aPath := normalizedEntryPath(a.resolved.RegistryPath)
	bPath := normalizedEntryPath(b.resolved.RegistryPath)
	if isPathPrefix(bPath, aPath) {
		return 1
	}
	if isPathPrefix(aPath, bPath) {
		return -1
	}
	return 0
}

// pathScopeCovers reports whether a registry entry rooted at scopePath governs
// a resource discovered at resourcePath (both are entry-relative registry
// paths, already component-clean). It is true when the paths are identical or
// scopePath is a component-wise prefix of resourcePath. An empty scopePath
// (whole-repo root) covers every resource of the URL. This is used to scope
// exclude_all so it only suppresses resources within the excluding entry's own
// path subtree, not sibling paths of the same registry URL.
func pathScopeCovers(scopePath, resourcePath string) bool {
	scopePath = normalizedEntryPath(scopePath)
	resourcePath = normalizedEntryPath(resourcePath)
	return scopePath == resourcePath || isPathPrefix(scopePath, resourcePath)
}

// registryIdentityMatches keeps registry-entry exclusions scoped to the
// requested URL/path/ref. An omitted ref remains a deliberate wildcard for
// hand-authored legacy exclusions; shadow entries created for --ref always
// carry a concrete ref and therefore cannot suppress sibling refs.
func registryIdentityMatches(entryURL, entryPath, entryRef, targetURL, targetPath, targetRef string) bool {
	return normalizedRegistryURL(entryURL) == normalizedRegistryURL(targetURL) &&
		pathScopeCovers(entryPath, targetPath) &&
		(strings.TrimSpace(entryRef) == "" || strings.TrimSpace(targetRef) == "" || strings.TrimSpace(entryRef) == strings.TrimSpace(targetRef))
}

// pathsDiffer reports whether any two occurrences in the set have different
// normalized RegistryPath values.
func pathsDiffer(occurrences []skillOccurrence) bool {
	if len(occurrences) < 2 {
		return false
	}
	first := normalizedEntryPath(occurrences[0].resolved.RegistryPath)
	for _, occ := range occurrences[1:] {
		if normalizedEntryPath(occ.resolved.RegistryPath) != first {
			return true
		}
	}
	return false
}

func hasMultiRegistryCollision(occurrences []skillOccurrence) bool {
	if len(occurrences) < 2 {
		return false
	}

	first := occurrences[0]
	firstKey := logicalRegistryKey(first)
	for _, occurrence := range occurrences[1:] {
		if logicalRegistryKey(occurrence) != firstKey {
			return true
		}
	}

	return false
}

func logicalRegistryKey(occ skillOccurrence) string {
	if occ.resolved.IsLocal() {
		return occ.resolved.CatalogKey
	}
	if occ.resolved.IsMarketplace {
		return normalizedRegistryURL(occ.resolved.MarketplaceURL) + "\x00" + strings.TrimSpace(occ.resolved.MarketplaceInstanceID) + "\x00" + strings.TrimSpace(occ.resolved.MarketplaceRegistryID) + "\x00" + strings.TrimSpace(occ.resolved.Ref)
	}
	return normalizedRegistryURL(occ.url) + "\x00" + strings.TrimSpace(occ.resolved.Ref)
}

// pickCollisionWinner selects a deterministic winner from colliding
// occurrences. Precedence: required > higher source precedence >
// alphabetically-first normalized URL as tiebreaker.
func pickCollisionWinner(occurrences []skillOccurrence) skillOccurrence {
	winner := occurrences[0]
	for _, occ := range occurrences[1:] {
		if occ.resolved.Required && !winner.resolved.Required {
			winner = occ
			continue
		}
		if occ.resolved.Required == winner.resolved.Required &&
			config.CompareEntryPrecedence(occ.resolved.Source, occ.resolved.FilePrecedence, winner.resolved.Source, winner.resolved.FilePrecedence) > 0 {
			winner = occ
			continue
		}
		if occ.resolved.Required == winner.resolved.Required &&
			config.CompareEntryPrecedence(occ.resolved.Source, occ.resolved.FilePrecedence, winner.resolved.Source, winner.resolved.FilePrecedence) == 0 &&
			normalizedRegistryURL(occ.url) < normalizedRegistryURL(winner.url) {
			winner = occ
		}
	}
	return winner
}

// collisionLabelsNeedRef reports whether URL, path, and Marketplace identity
// alone would collapse two collision participants. Refs are intentionally
// omitted for the common case, but become part of every label when one
// location is configured at multiple refs.
func collisionLabelsNeedRef(occurrences []skillOccurrence) bool {
	refsByLocation := make(map[string]string, len(occurrences))
	for _, occ := range occurrences {
		location := normalizedRegistryURL(occ.url) + "\x00" + normalizedEntryPath(occ.resolved.RegistryPath) + "\x00" + strings.TrimSpace(occ.resolved.MarketplaceInstanceID) + "\x00" + strings.TrimSpace(occ.resolved.MarketplaceRegistryID)
		ref := strings.TrimSpace(occ.resolved.Ref)
		if previous, ok := refsByLocation[location]; ok && previous != ref {
			return true
		}
		refsByLocation[location] = ref
	}
	return false
}

// collisionSourceLabel returns a human-readable label for a collision
// participant that includes Marketplace identity and the registry path when
// set, so same-URL-different-registry/path entries are distinguishable in the
// warning. Include refs when the same location is configured at multiple refs.
func collisionSourceLabel(occ skillOccurrence, includeRef bool) string {
	label := normalizedRegistryURL(occ.url)
	if occ.resolved.IsMarketplace {
		label = fmt.Sprintf("%s [instance_id=%s, registry_id=%s]",
			normalizedRegistryURL(occ.resolved.MarketplaceURL),
			occ.resolved.MarketplaceInstanceID,
			occ.resolved.MarketplaceRegistryID,
		)
	}
	if p := normalizedEntryPath(occ.resolved.RegistryPath); p != "" {
		label += " (" + p + ")"
	}
	if includeRef && strings.TrimSpace(occ.resolved.Ref) != "" {
		label += " @" + strings.TrimSpace(occ.resolved.Ref)
	}
	return label
}

// collisionRunnersUpByPath returns source labels (URL + path + ref when
// needed) of all occurrences except the winner, for collisions where the URL
// alone is insufficient to tell entries apart.
func collisionRunnersUpByPath(occurrences []skillOccurrence, winner skillOccurrence) []string {
	includeRef := collisionLabelsNeedRef(occurrences)
	winnerLabel := collisionSourceLabel(winner, includeRef)
	var runnersUp []string
	seen := make(map[string]struct{})
	for _, occ := range occurrences {
		label := collisionSourceLabel(occ, includeRef)
		if label == winnerLabel {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		runnersUp = append(runnersUp, label)
	}
	return runnersUp
}
