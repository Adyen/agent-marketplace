package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/catalog"
	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// MarketplaceConsentError carries the verified preview even when consent
// prevents mutation, so automation can log the exact artifact it declined.
type MarketplaceConsentError struct {
	Preview  string
	Prepared *PreparedMarketplaceInstall
	Err      error
}

type PreparedMarketplaceInstall struct {
	options             InstallOptions
	configPath          string
	plan                *marketplace.PreparedInstall
	preview             string
	targetHarnesses     []string
	allowedMarketplaces *[]string
	allowedRegistries   *[]string
	endpointConfigured  bool
}

func (e *MarketplaceConsentError) Error() string { return e.Err.Error() }
func (e *MarketplaceConsentError) Unwrap() error { return e.Err }

func isMarketplaceInstall(opts InstallOptions) bool {
	if opts.ExactMarketplaceLocator != nil {
		return true
	}
	if strings.TrimSpace(opts.Marketplace) != "" {
		return true
	}
	if len(opts.Args) != 1 {
		return false
	}
	raw := strings.TrimSpace(opts.Args[0])
	if strings.Contains(raw, ":") || strings.HasPrefix(strings.ToLower(raw), "http://") || strings.HasPrefix(strings.ToLower(raw), "https://") {
		return true
	}
	_, err := marketplace.ParseMarketplaceLocator(raw)
	return err == nil
}

func isMarketplaceEndpointRegistration(opts InstallOptions) bool {
	if opts.ExactMarketplaceLocator != nil || len(opts.Args) != 1 || strings.TrimSpace(opts.Marketplace) != "" || strings.TrimSpace(opts.FromURL) != "" {
		return false
	}
	raw := strings.TrimSpace(opts.Args[0])
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return false
	}
	if _, err := marketplace.ParseMarketplaceLocator(raw); err == nil {
		return false
	}
	_, err := marketplace.NormalizeMarketplaceURL(raw)
	return err == nil
}

func registerMarketplaceEndpoint(ctx context.Context, opts InstallOptions, configPath string, ec *config.EffectiveConfig) (*InstallResult, error) {
	if opts.HasKind {
		return nil, fmt.Errorf("--kind is only supported when installing a resource")
	}
	endpoint, err := marketplace.NormalizeMarketplaceURL(opts.Args[0])
	if err != nil {
		return nil, err
	}
	allowed := []string(nil)
	if ec != nil && ec.Merged != nil && ec.Merged.AllowedMarketplaces != nil {
		allowed = *ec.Merged.AllowedMarketplaces
	}
	if err := config.ValidateMarketplaceURLs([]config.MarketplaceEntry{{URL: endpoint}}, allowed); err != nil {
		return nil, fmt.Errorf("marketplace endpoint rejected before network access: %w", err)
	}

	slog.Info("registering Marketplace endpoint", "url", endpoint, "config_path", configPath, "scope", opts.Scope)
	client, err := marketplace.NewClient(endpoint)
	if err != nil {
		return nil, err
	}
	wellKnown, err := client.WellKnown(ctx)
	if err != nil {
		if parsed := ParseGitWebURL(endpoint); parsed.Ok {
			return nil, fmt.Errorf("marketplace %s well-known document: %w; if this is a Git registry, use --from %q instead", endpoint, err, endpoint)
		}
		return nil, fmt.Errorf("marketplace %s well-known document: %w", endpoint, err)
	}
	canonical, err := marketplace.NormalizeMarketplaceURL(wellKnown.CanonicalURL)
	if err != nil {
		return nil, fmt.Errorf("marketplace %s reported an invalid canonical URL: %w", endpoint, err)
	}
	if canonical != endpoint {
		return nil, fmt.Errorf("marketplace endpoint %q reports canonical URL %q; install the canonical URL explicitly", endpoint, canonical)
	}
	entry := config.MarketplaceEntry{URL: canonical, InstanceID: wellKnown.InstanceID}
	if errs := config.ValidateMarketplaceEntries([]config.MarketplaceEntry{entry}); len(errs) > 0 {
		return nil, fmt.Errorf("invalid Marketplace identity: %w", errors.Join(errs...))
	}

	targetSource := installScopeSource(opts.Scope)
	var existingPath string
	if ec != nil && ec.Merged != nil {
		for _, configured := range ec.Merged.Marketplaces {
			configuredURL, normalizeErr := marketplace.NormalizeMarketplaceURL(configured.URL)
			if normalizeErr != nil {
				continue
			}
			sameURL := configuredURL == canonical
			sameInstance := configured.InstanceID == entry.InstanceID
			switch {
			case sameURL && !sameInstance:
				return nil, fmt.Errorf("marketplace endpoint %s is configured with instance ID %q but now reports %q", canonical, configured.InstanceID, entry.InstanceID)
			case sameInstance && !sameURL:
				return nil, fmt.Errorf("marketplace instance %q is already configured at %s; endpoint migration must be explicit", entry.InstanceID, configuredURL)
			case sameURL && sameInstance && configured.Source.Precedence() <= targetSource.Precedence() && existingPath == "":
				existingPath = marketplaceRegistrationConfigPath(ec, configured.Source, configPath)
			}
		}
	}
	if existingPath != "" {
		slog.Info("Marketplace endpoint already configured", "url", canonical, "instance_id", entry.InstanceID, "config_path", existingPath)
		return &InstallResult{MarketplaceEndpoint: canonical, MarketplaceConfigPath: existingPath, MarketplaceAlreadyConfigured: true}, nil
	}
	if err := config.EnsureMarketplaceEntryInConfig(configPath, entry); err != nil {
		return nil, fmt.Errorf("write Marketplace endpoint: %w", err)
	}
	slog.Info("Marketplace endpoint registered", "url", canonical, "instance_id", entry.InstanceID, "config_path", configPath)
	return &InstallResult{MarketplaceEndpoint: canonical, MarketplaceConfigPath: configPath}, nil
}

func installScopeSource(scope Scope) config.Source {
	switch scope {
	case ScopeUser:
		return config.UserPersonal
	case ScopeLocal:
		return config.RepoPersonal
	default:
		return config.RepoCommitted
	}
}

func marketplaceRegistrationConfigPath(ec *config.EffectiveConfig, source config.Source, targetPath string) string {
	switch source {
	case config.Organization:
		return ec.Organization.Path
	case config.UserPersonal:
		return ec.User.Path
	case config.RepoCommitted:
		return ec.Repo.Path
	case config.RepoPersonal:
		return targetPath
	default:
		return targetPath
	}
}

func installMarketplace(ctx context.Context, opts InstallOptions, configPath string, ec *config.EffectiveConfig) (*InstallResult, error) {
	prepared, err := prepareMarketplaceInstall(ctx, opts, configPath, ec)
	if err != nil {
		return nil, err
	}
	if err := evaluateMarketplaceConsent(prepared.plan, opts.AcceptRisk); err != nil {
		return nil, &MarketplaceConsentError{Preview: prepared.preview, Prepared: prepared, Err: err}
	}
	return CommitPreparedMarketplaceInstall(ctx, prepared)
}

func prepareMarketplaceInstall(ctx context.Context, opts InstallOptions, configPath string, ec *config.EffectiveConfig) (*PreparedMarketplaceInstall, error) {
	if ec == nil || len(ec.TargetHarnesses) == 0 {
		return nil, &TargetHarnessSetMissingError{}
	}
	var locator marketplace.MarketplaceLocator
	var err error
	if opts.ExactMarketplaceLocator != nil {
		locator = *opts.ExactMarketplaceLocator
	} else {
		if len(opts.Args) != 1 {
			return nil, fmt.Errorf("marketplace install requires exactly one resource locator")
		}
		locator, err = marketplaceLocator(opts.Args[0], opts.Marketplace)
		if err != nil {
			return nil, err
		}
	}
	if opts.HasKind && !marketplaceKindMatches(opts.Kind, locator.Kind) {
		return nil, fmt.Errorf("--kind %s does not match Marketplace locator kind %s", opts.Kind, locator.Kind)
	}
	if opts.InstalledName != "" {
		if err := marketplace.ValidateInstalledName(opts.InstalledName); err != nil {
			return nil, err
		}
	}
	entry, err := resolveMarketplaceEntry(locator, opts.Marketplace, ec.Merged)
	if err != nil {
		return nil, err
	}
	allowed := []string(nil)
	if ec.Merged != nil && ec.Merged.AllowedMarketplaces != nil {
		allowed = *ec.Merged.AllowedMarketplaces
	}
	if err := config.ValidateMarketplaceURLs([]config.MarketplaceEntry{entry}, allowed); err != nil {
		return nil, fmt.Errorf("marketplace endpoint rejected before network access: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allowedRegistries := []string(nil)
	if ec.Merged != nil && ec.Merged.AllowedRegistries != nil {
		allowedRegistries = *ec.Merged.AllowedRegistries
	}
	resolverMP := marketplace.NewResolver()
	resolverMP.AllowedRegistries = allowedRegistries
	plan, err := resolverMP.PrepareLocator(ctx, entry, locator, hccTargetsFromStrings(ec.TargetHarnesses))
	if err != nil {
		return nil, err
	}
	if opts.InstalledName != "" {
		plan.Pin.InstalledName = opts.InstalledName
	}
	if err := checkMarketplaceInstallCollision(ctx, opts, ec, plan); err != nil {
		return nil, err
	}
	preview, err := marketplaceInstallPreview(plan)
	if err != nil {
		return nil, err
	}
	var preparedAllowedMarketplaces, preparedAllowedRegistries *[]string
	if ec.Merged != nil {
		preparedAllowedMarketplaces = cloneStringPolicy(ec.Merged.AllowedMarketplaces)
		preparedAllowedRegistries = cloneStringPolicy(ec.Merged.AllowedRegistries)
	}
	return &PreparedMarketplaceInstall{
		options:             opts,
		configPath:          configPath,
		plan:                plan,
		preview:             preview,
		targetHarnesses:     slices.Clone(ec.TargetHarnesses),
		allowedMarketplaces: preparedAllowedMarketplaces,
		allowedRegistries:   preparedAllowedRegistries,
		endpointConfigured:  hasMarketplaceIdentity(ec.Merged, plan.Entry),
	}, nil
}

func CommitPreparedMarketplaceInstall(ctx context.Context, prepared *PreparedMarketplaceInstall) (*InstallResult, error) {
	if prepared == nil || prepared.plan == nil {
		return nil, errors.New("marketplace install was not prepared")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := evaluateMarketplaceConsent(prepared.plan, true); err != nil {
		return nil, err
	}
	ec := config.LoadEffective(ctx, config.LoadEffectiveOptions{RepoRoot: prepared.options.RepoRoot, LocalConfigDir: prepared.options.LocalConfigDir})
	var allowedMarketplaces, allowedRegistries *[]string
	if ec.Merged != nil {
		allowedMarketplaces = ec.Merged.AllowedMarketplaces
		allowedRegistries = ec.Merged.AllowedRegistries
	}
	if !equalStringPolicy(prepared.allowedMarketplaces, allowedMarketplaces) {
		return nil, errors.New("allowed_marketplaces changed after Marketplace install preview; preview again")
	}
	if !equalStringPolicy(prepared.allowedRegistries, allowedRegistries) {
		return nil, errors.New("allowed_registries changed after Marketplace install preview; preview again")
	}
	if len(ec.Errors) > 0 {
		return nil, errors.Join(ec.Errors...)
	}
	if !slices.Equal(ec.TargetHarnesses, prepared.targetHarnesses) {
		return nil, errors.New("target harnesses changed after Marketplace install preview; preview again")
	}
	commitEntry := prepared.plan.Entry
	if current, ok := marketplaceIdentity(ec.Merged, prepared.plan.Entry); ok {
		commitEntry.Alias = current.Alias
	} else if prepared.endpointConfigured {
		return nil, errors.New("marketplace endpoint changed after install preview; preview again")
	}
	if err := checkMarketplaceInstallCollision(ctx, prepared.options, &ec, prepared.plan); err != nil {
		return nil, err
	}
	pins := marketplaceInstallPins(prepared.plan)
	identities := marketplaceInstallIdentities(prepared.plan)
	requirementWarnings, err := ensureMarketplacePinsAndSync(prepared.configPath, commitEntry, pins, func() ([]error, error) {
		return syncAfterInstall(ctx, prepared.options.RepoRoot, identities, silentSyncOptsForInstall(prepared.options)...)
	})
	if err != nil {
		return nil, err
	}
	installed := make([]string, 0, len(identities))
	for _, identity := range identities {
		installed = append(installed, identity.name)
	}
	return &InstallResult{Installed: installed, IsCollection: len(prepared.plan.CollectionMembers) > 0, CollectionLabel: prepared.plan.Pin.Name, MarketplacePreview: prepared.preview, RequirementWarnings: requirementWarnings}, nil
}

func cloneStringPolicy(policy *[]string) *[]string {
	if policy == nil {
		return nil
	}
	cloned := slices.Clone(*policy)
	return &cloned
}

func equalStringPolicy(a, b *[]string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return slices.Equal(*a, *b)
}

func hasMarketplaceIdentity(cfg *config.Config, want config.MarketplaceEntry) bool {
	_, ok := marketplaceIdentity(cfg, want)
	return ok
}

func marketplaceIdentity(cfg *config.Config, want config.MarketplaceEntry) (config.MarketplaceEntry, bool) {
	if cfg != nil {
		for _, entry := range cfg.Marketplaces {
			if canonicalMarketplaceURL(entry.URL) == canonicalMarketplaceURL(want.URL) && entry.InstanceID == want.InstanceID {
				return entry, true
			}
		}
	}
	return config.MarketplaceEntry{}, false
}

func marketplaceInstallPins(plan *marketplace.PreparedInstall) []config.MarketplaceResourcePin {
	if len(plan.CollectionMembers) == 0 {
		return []config.MarketplaceResourcePin{config.EnsureMarketplaceDirectPin(plan.Pin)}
	}
	pins := make([]config.MarketplaceResourcePin, 0, len(plan.CollectionMembers)+1)
	pins = append(pins, plan.Pin)
	for _, member := range plan.CollectionMembers {
		pins = append(pins, member.Pin)
	}
	return pins
}

func marketplaceInstallIdentities(plan *marketplace.PreparedInstall) []resourceIdentity {
	if len(plan.CollectionMembers) == 0 {
		return []resourceIdentity{{kind: plan.Resolved.Kind, name: plan.Pin.Name}}
	}
	identities := make([]resourceIdentity, 0, len(plan.CollectionMembers))
	for _, member := range plan.CollectionMembers {
		identities = append(identities, resourceIdentity{kind: member.Resolved.Kind, name: member.Pin.Name})
	}
	return identities
}

// ensureMarketplacePinAndSync makes the pin mutation transactional with
// respect to the target config file. Materialization may have partially
// changed unrelated generated files before returning an error. A cleanup sync
// is intentionally not attempted: rerunning broad reconciliation here could
// recurse through install behavior or destructively prune user-visible output,
// and could also obscure the original sync error. The config rollback remains
// byte-precise and transactional.
func ensureMarketplacePinAndSync(configPath string, plan *marketplace.PreparedInstall, sync func() ([]error, error)) (warnings []error, retErr error) {
	return ensureMarketplacePinsAndSync(configPath, plan.Entry, []config.MarketplaceResourcePin{plan.Pin}, sync)
}

func ensureMarketplacePinsAndSync(configPath string, entry config.MarketplaceEntry, pins []config.MarketplaceResourcePin, sync func() ([]error, error)) (warnings []error, retErr error) {
	snapshot, commit, err := config.EnsureMarketplacePinsInConfigWithSnapshotAndCommit(configPath, entry.URL, entry.InstanceID, entry.Alias, pins)
	if err != nil {
		return nil, fmt.Errorf("write Marketplace pin: %w", err)
	}
	defer func() {
		if retErr == nil {
			return
		}
		if rollbackErr := snapshot.RestoreIfCurrent(commit); rollbackErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("rollback Marketplace config: %w", rollbackErr))
		}
	}()

	warnings, err = sync()
	if err != nil {
		return nil, err
	}
	current, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read Marketplace config after sync: %w", err)
	}
	if !bytes.Equal(current, commit.Bytes) {
		return nil, fmt.Errorf("marketplace config changed concurrently during install")
	}
	return warnings, nil
}

func marketplaceProtocolKind(kind ir.ResourceKind) string {
	if kind == ir.KindMcpServer {
		return "mcpserver"
	}
	return kind.String()
}

func marketplaceKindMatches(kind ir.ResourceKind, locatorKind string) bool {
	return marketplaceProtocolKind(kind) == locatorKind
}

func marketplaceLocator(raw, selector string) (marketplace.MarketplaceLocator, error) {
	if strings.TrimSpace(selector) != "" && !strings.Contains(raw, ":") && !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		if strings.HasPrefix(strings.ToLower(selector), "http://") || strings.HasPrefix(strings.ToLower(selector), "https://") {
			endpoint, err := marketplace.NormalizeMarketplaceURL(selector)
			if err != nil {
				return marketplace.MarketplaceLocator{}, err
			}
			raw = endpoint + "/s/" + raw
		} else {
			raw = selector + ":" + raw
		}
	}
	loc, err := marketplace.ParseMarketplaceLocator(raw)
	if err != nil {
		return loc, fmt.Errorf("invalid Marketplace locator: %w", err)
	}
	if selector != "" && loc.Alias != "" && !strings.Contains(selector, "://") {
		loc.Alias, loc.Marketplace = selector, selector
	}
	return loc, nil
}

func resolveMarketplaceEntry(locator marketplace.MarketplaceLocator, selector string, cfg *config.Config) (config.MarketplaceEntry, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	want := locator.URL
	alias := locator.Alias
	if selector != "" {
		if strings.Contains(selector, "://") {
			var err error
			want, err = marketplace.NormalizeMarketplaceURL(selector)
			if err != nil {
				return config.MarketplaceEntry{}, err
			}
			alias = ""
		} else {
			alias = selector
		}
	}
	for _, entry := range cfg.Marketplaces {
		canonical, err := marketplace.NormalizeMarketplaceURL(entry.URL)
		if err != nil {
			continue
		}
		if (alias != "" && entry.Alias == alias) || (want != "" && canonical == want) {
			entry.URL = canonical
			return entry, nil
		}
	}
	if alias != "" {
		return config.MarketplaceEntry{}, fmt.Errorf("marketplace alias %q is not configured; configure it or use a browser URL locator", alias)
	}
	if want == "" {
		return config.MarketplaceEntry{}, fmt.Errorf("marketplace endpoint is required")
	}
	return config.MarketplaceEntry{URL: want}, nil
}

func checkMarketplaceInstallCollision(ctx context.Context, opts InstallOptions, ec *config.EffectiveConfig, plan *marketplace.PreparedInstall) error {
	if len(plan.CollectionMembers) > 0 {
		seen := make(map[string]*marketplace.PreparedInstall, len(plan.CollectionMembers))
		for _, member := range plan.CollectionMembers {
			kind := marketplaceProtocolKind(member.Resolved.Kind)
			key := kind + "\x00" + marketplaceInstalledName(member.Pin)
			if previous, ok := seen[key]; ok {
				if !sameMarketplacePlanIdentity(previous, member) {
					return fmt.Errorf("collection leaves collide on %s installed name %q", kind, marketplaceInstalledName(member.Pin))
				}
				continue
			}
			seen[key] = member
			if err := checkMarketplaceInstallCollision(ctx, opts, ec, member); err != nil {
				return err
			}
		}
		return nil
	}
	installed := marketplaceInstalledName(plan.Pin)
	if ec.Merged != nil {
		for _, entry := range ec.Merged.Marketplaces {
			for _, pin := range entry.Resources {
				if pin.Kind != plan.Pin.Kind || marketplaceInstalledName(pin) != installed {
					continue
				}
				if sameMarketplacePinIdentity(entry.URL, entry.InstanceID, pin, plan) {
					continue
				}
				return fmt.Errorf("marketplace %s %q collides with installed name %q", pin.Kind, pin.Name, installed)
			}
		}
	}
	// Resolve the pre-existing selection without mutating or materializing it.
	// This includes registry and local resources, and remains valid when there
	// are no Git registries at all.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	marketplaceMode := resolver.MarketplaceResolveModeRefresh
	resolved := Resolve(ctx, ResolveOptions{RepoRoot: opts.RepoRoot, LocalConfigDir: opts.LocalConfigDir, CatalogMode: catalog.LoadModeCached, CatalogOption: opts.CatalogOption, MarketplaceMode: &marketplaceMode})
	if len(resolved.Errors) > 0 {
		return errors.Join(resolved.Errors...)
	}
	return checkResolvedMarketplaceInstallCollision(resolved.Resolved, plan)
}

func checkResolvedMarketplaceInstallCollision(resolved []resolver.ResolvedArtifact, plan *marketplace.PreparedInstall) error {
	installed := marketplaceInstalledName(plan.Pin)
	for _, existing := range resolved {
		if existing.Kind != plan.Resolved.Kind || marketplaceResolvedInstalledName(existing) != installed {
			continue
		}
		if sameResolvedMarketplaceIdentity(existing, plan) {
			continue
		}
		return fmt.Errorf("%s %q collides with installed name %q", existing.Kind, existing.Name, installed)
	}
	return nil
}

func sameMarketplacePinIdentity(url, instanceID string, pin config.MarketplaceResourcePin, plan *marketplace.PreparedInstall) bool {
	existingURL := canonicalMarketplaceURL(url)
	planURL := canonicalMarketplaceURL(plan.Entry.URL)
	return existingURL != "" && existingURL == planURL && instanceID == plan.Entry.InstanceID &&
		pin.RegistryID == plan.Pin.RegistryID && pin.Kind == plan.Pin.Kind && pin.Name == plan.Pin.Name
}

func sameMarketplacePlanIdentity(a, b *marketplace.PreparedInstall) bool {
	return canonicalMarketplaceURL(a.Entry.URL) != "" &&
		canonicalMarketplaceURL(a.Entry.URL) == canonicalMarketplaceURL(b.Entry.URL) &&
		a.Entry.InstanceID == b.Entry.InstanceID && a.Pin.RegistryID == b.Pin.RegistryID &&
		a.Pin.Kind == b.Pin.Kind && a.Pin.Name == b.Pin.Name
}

func sameResolvedMarketplaceIdentity(existing resolver.ResolvedArtifact, plan *marketplace.PreparedInstall) bool {
	existingURL := canonicalMarketplaceURL(existing.MarketplaceURL)
	planURL := canonicalMarketplaceURL(plan.Entry.URL)
	return existing.IsMarketplace && existingURL != "" && existingURL == planURL &&
		existing.MarketplaceInstanceID == plan.Entry.InstanceID && existing.MarketplaceRegistryID == plan.Pin.RegistryID &&
		existing.Kind == plan.Resolved.Kind && existing.Name == plan.Pin.Name
}

func marketplaceResolvedInstalledName(existing resolver.ResolvedArtifact) string {
	if existing.InstalledName != "" {
		return existing.InstalledName
	}
	return existing.Name
}

func canonicalMarketplaceURL(raw string) string {
	canonical, err := marketplace.NormalizeMarketplaceURL(raw)
	if err != nil {
		return ""
	}
	return canonical
}

func marketplaceInstalledName(pin config.MarketplaceResourcePin) string {
	if pin.InstalledName != "" {
		return pin.InstalledName
	}
	return pin.Name
}

func evaluateMarketplaceConsent(plan *marketplace.PreparedInstall, accept bool) error {
	// Evaluate the virtual collection revision before descending into leaves. A
	// collection finding is part of the trust decision even though the root has
	// no materialized artifact.
	return evaluateMarketplacePreviewConsentRecursive(plan.Preview(), accept)
}

func evaluateMarketplacePreviewConsentRecursive(preview marketplace.InstallPreview, accept bool) error {
	if err := evaluateMarketplacePreviewConsent(preview.Findings, preview.Capabilities, accept); err != nil {
		return err
	}
	for _, member := range preview.CollectionMembers {
		if err := evaluateMarketplacePreviewConsentRecursive(member, accept); err != nil {
			return err
		}
	}
	return nil
}

func evaluateMarketplacePreviewConsent(findings []hccartifact.CanonicalFinding, capabilities []string, accept bool) error {
	for _, finding := range findings {
		outcome := strings.ToLower(strings.TrimSpace(finding.PolicyOutcome))
		switch outcome {
		case "allow", "warn":
			continue
		case "reject":
			return fmt.Errorf("marketplace revision is rejected by policy (%s: %s); --accept-risk cannot bypass policy rejection", marketplace.QuoteTerminal(finding.Code), marketplace.QuoteTerminal(finding.Message))
		default:
			return fmt.Errorf("marketplace finding %s has unknown policy outcome %q; refusing to continue", marketplace.QuoteTerminal(finding.Code), marketplace.QuoteTerminal(finding.PolicyOutcome))
		}
	}
	if (len(findings) > 0 || len(capabilities) > 0) && !accept {
		return fmt.Errorf("marketplace install requires risk acceptance: %d finding(s), %d active capability(ies); re-run with --accept-risk (or --yes)", len(findings), len(capabilities))
	}
	return nil
}

type marketplacePreviewFinding struct {
	Code          marketplace.TerminalString `json:"code"`
	Severity      marketplace.TerminalString `json:"severity"`
	Message       marketplace.TerminalString `json:"message"`
	File          marketplace.TerminalString `json:"file"`
	Line          int                        `json:"line"`
	Analyzer      marketplace.TerminalString `json:"analyzer"`
	PolicyOutcome marketplace.TerminalString `json:"policy_outcome"`
}

type marketplacePreviewLeaf struct {
	RegistryID             marketplace.TerminalString   `json:"registry_id"`
	RegistrySlug           marketplace.TerminalString   `json:"registry_slug"`
	Kind                   marketplace.TerminalString   `json:"kind"`
	Name                   marketplace.TerminalString   `json:"name"`
	InstalledName          marketplace.TerminalString   `json:"installed_name"`
	Revision               marketplace.TerminalString   `json:"revision"`
	ContentDigest          marketplace.TerminalString   `json:"content_digest"`
	ArchiveDigest          marketplace.TerminalString   `json:"archive_digest"`
	Acquisition            marketplace.TerminalString   `json:"acquisition"`
	RegistryCommit         marketplace.TerminalString   `json:"registry_commit,omitempty"`
	RegistryManifestDigest marketplace.TerminalString   `json:"registry_manifest_digest,omitempty"`
	License                marketplace.TerminalString   `json:"license,omitempty"`
	Findings               []marketplacePreviewFinding  `json:"findings"`
	Capabilities           []marketplace.TerminalString `json:"capabilities"`
}

func marketplacePreviewFindings(values []hccartifact.CanonicalFinding) []marketplacePreviewFinding {
	findings := make([]marketplacePreviewFinding, 0, len(values))
	for _, finding := range values {
		analyzer := finding.Analyzer
		if analyzer == "" {
			analyzer = finding.AnalyzerVersion
		}
		findings = append(findings, marketplacePreviewFinding{
			Code: marketplace.TerminalString(finding.Code), Severity: marketplace.TerminalString(finding.Severity), Message: marketplace.TerminalString(finding.Message),
			File: marketplace.TerminalString(finding.File), Line: finding.Line, Analyzer: marketplace.TerminalString(analyzer),
			PolicyOutcome: marketplace.TerminalString(finding.PolicyOutcome),
		})
	}
	slices.SortFunc(findings, func(a, b marketplacePreviewFinding) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return strings.Compare(string(left), string(right))
	})
	return findings
}

func marketplacePreviewLeafFromPreview(preview marketplace.InstallPreview) marketplacePreviewLeaf {
	caps := slices.Clone(preview.Capabilities)
	sort.Strings(caps)
	return marketplacePreviewLeaf{
		RegistryID: marketplace.TerminalString(preview.RegistryID), RegistrySlug: marketplace.TerminalString(preview.RegistrySlug),
		Kind: marketplace.TerminalString(preview.Kind), Name: marketplace.TerminalString(preview.Name),
		InstalledName: marketplace.TerminalString(preview.InstalledName), Revision: marketplace.TerminalString(preview.Revision),
		ContentDigest: marketplace.TerminalString(preview.ContentDigest), ArchiveDigest: marketplace.TerminalString(preview.ArchiveDigest),
		Acquisition:    marketplace.TerminalString(nonEmpty(preview.Acquisition, config.MarketplaceAcquisitionMirror)),
		RegistryCommit: marketplace.TerminalString(preview.RegistryCommit), RegistryManifestDigest: marketplace.TerminalString(preview.RegistryManifestDigest),
		License: marketplace.TerminalString(preview.License), Findings: marketplacePreviewFindings(preview.Findings),
		Capabilities: marketplace.TerminalStrings(caps),
	}
}

func marketplacePreviewLeaves(members []marketplace.InstallPreview) []marketplacePreviewLeaf {
	leaves := make([]marketplacePreviewLeaf, 0, len(members))
	var appendLeaves func([]marketplace.InstallPreview)
	appendLeaves = func(values []marketplace.InstallPreview) {
		for _, member := range values {
			if len(member.CollectionMembers) > 0 {
				appendLeaves(member.CollectionMembers)
				continue
			}
			leaves = append(leaves, marketplacePreviewLeafFromPreview(member))
		}
	}
	appendLeaves(members)
	slices.SortFunc(leaves, func(a, b marketplacePreviewLeaf) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return strings.Compare(string(left), string(right))
	})
	return leaves
}

func marketplacePreviewFindingsText(findings []marketplacePreviewFinding) string {
	if len(findings) == 0 {
		return "findings: none"
	}
	var result strings.Builder
	fmt.Fprintf(&result, "findings (%d):", len(findings))
	for i, finding := range findings {
		fmt.Fprintf(&result, "\n  %d. severity: %s", i+1, marketplace.QuoteTerminal(string(finding.Severity)))
		if finding.Message != "" {
			fmt.Fprintf(&result, "\n     message: %s", marketplace.QuoteTerminal(string(finding.Message)))
		}
		if finding.File != "" {
			fmt.Fprintf(&result, "\n     location: %s", marketplace.QuoteTerminal(string(finding.File)))
			if finding.Line > 0 {
				fmt.Fprintf(&result, ":%d", finding.Line)
			}
		}
		fmt.Fprintf(&result, "\n     code: %s", marketplace.QuoteTerminal(string(finding.Code)))
		if finding.PolicyOutcome != "" {
			fmt.Fprintf(&result, "\n     policy: %s", marketplace.QuoteTerminal(string(finding.PolicyOutcome)))
		}
		if finding.Analyzer != "" {
			fmt.Fprintf(&result, "\n     analyzer: %s", marketplace.QuoteTerminal(string(finding.Analyzer)))
		}
	}
	return result.String()
}

func marketplaceInstallPreview(plan *marketplace.PreparedInstall) (string, error) {
	preview := plan.Preview()
	findings := marketplacePreviewFindingsText(marketplacePreviewFindings(preview.Findings))
	caps := slices.Clone(preview.Capabilities)
	sort.Strings(caps)
	capabilitiesJSON, err := json.Marshal(marketplace.TerminalStrings(caps))
	if err != nil {
		return "", fmt.Errorf("encode Marketplace capability preview: %w", err)
	}
	capabilities := string(capabilitiesJSON)
	if len(caps) == 0 {
		capabilities = marketplace.QuoteTerminal("none")
	}
	provenance := fmt.Sprintf("registry_url=%s ref=%s path=%s generator=%s", marketplace.QuoteTerminal(preview.Provenance.RegistryURL), marketplace.QuoteTerminal(preview.Provenance.Ref), marketplace.QuoteTerminal(preview.Provenance.Path), marketplace.QuoteTerminal(preview.Provenance.Generator))
	result := fmt.Sprintf("Marketplace install preview:\nmarketplace: %s (%s)\ninstance_id: %s\nregistry: %s (%s)\nkind: %s\nname: %s\ninstalled_name: %s\nrevision: %s\ncontent_digest: %s\narchive_digest: %s\nacquisition: %s\nregistry_commit: %s\nregistry_manifest_digest: %s\nlicense: %s\n%s\ncapabilities: %s\nprovenance: %s", marketplace.QuoteTerminal(displayMarketplace(plan)), marketplace.QuoteTerminal(preview.MarketplaceURL), marketplace.QuoteTerminal(preview.InstanceID), marketplace.QuoteTerminal(preview.RegistrySlug), marketplace.QuoteTerminal(preview.RegistryID), marketplace.QuoteTerminal(preview.Kind), marketplace.QuoteTerminal(preview.Name), marketplace.QuoteTerminal(preview.InstalledName), marketplace.QuoteTerminal(preview.Revision), marketplace.QuoteTerminal(preview.ContentDigest), marketplace.QuoteTerminal(preview.ArchiveDigest), marketplace.QuoteTerminal(nonEmpty(preview.Acquisition, config.MarketplaceAcquisitionMirror)), marketplace.QuoteTerminal(nonEmpty(preview.RegistryCommit, "none")), marketplace.QuoteTerminal(nonEmpty(preview.RegistryManifestDigest, "none")), marketplace.QuoteTerminal(nonEmpty(preview.License, "none")), findings, capabilities, provenance)
	if len(preview.CollectionMembers) == 0 {
		return result, nil
	}
	leaves, err := json.Marshal(marketplacePreviewLeaves(preview.CollectionMembers))
	if err != nil {
		return "", fmt.Errorf("encode Marketplace collection leaf preview: %w", err)
	}
	return result + "\ncollection_leaves: " + string(leaves), nil
}

func displayMarketplace(plan *marketplace.PreparedInstall) string {
	if plan.Entry.Alias != "" {
		return plan.Entry.Alias
	}
	return plan.Entry.URL
}

func nonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
