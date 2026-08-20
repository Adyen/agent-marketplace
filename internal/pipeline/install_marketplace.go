package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/config"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

// MarketplaceConsentError carries the verified preview even when consent
// prevents mutation, so automation can log the exact artifact it declined.
type MarketplaceConsentError struct {
	Preview string
	Err     error
}

func (e *MarketplaceConsentError) Error() string { return e.Err.Error() }
func (e *MarketplaceConsentError) Unwrap() error { return e.Err }

func isMarketplaceInstall(opts InstallOptions) bool {
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

func installMarketplace(ctx context.Context, opts InstallOptions, configPath string, ec *config.EffectiveConfig) (*InstallResult, error) {
	if ec == nil || len(ec.TargetHarnesses) == 0 {
		return nil, &TargetHarnessSetMissingError{}
	}
	if len(opts.Args) != 1 {
		return nil, fmt.Errorf("marketplace install requires exactly one resource locator")
	}
	locator, err := marketplaceLocator(opts.Args[0], opts.Marketplace)
	if err != nil {
		return nil, err
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

	resolverMP := marketplace.NewResolver()
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
	if err := evaluateMarketplaceConsent(plan, opts.AcceptRisk); err != nil {
		return nil, &MarketplaceConsentError{Preview: preview, Err: err}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requirementWarnings, err := ensureMarketplacePinAndSync(configPath, plan, func() ([]error, error) {
		return syncAfterInstall(ctx, opts.RepoRoot, []resourceIdentity{{kind: plan.Resolved.Kind, name: plan.Pin.Name}}, silentSyncOptsForInstall(opts)...)
	})
	if err != nil {
		return nil, err
	}
	return &InstallResult{Installed: []string{plan.Pin.Name}, MarketplacePreview: preview, RequirementWarnings: requirementWarnings}, nil
}

// ensureMarketplacePinAndSync makes the pin mutation transactional with
// respect to the target config file. Materialization may have partially
// changed unrelated generated files before returning an error. A cleanup sync
// is intentionally not attempted: rerunning broad reconciliation here could
// recurse through install behavior or destructively prune user-visible output,
// and could also obscure the original sync error. The config rollback remains
// byte-precise and transactional.
func ensureMarketplacePinAndSync(configPath string, plan *marketplace.PreparedInstall, sync func() ([]error, error)) (warnings []error, retErr error) {
	snapshot, commit, err := config.EnsureMarketplacePinInConfigWithSnapshotAndCommit(configPath, plan.Entry.URL, plan.Entry.InstanceID, plan.Entry.Alias, plan.Pin)
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
	resolved := Resolve(ctx, ResolveOptions{RepoRoot: opts.RepoRoot, LocalConfigDir: opts.LocalConfigDir})
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
		pin.SourceID == plan.Pin.SourceID && pin.Kind == plan.Pin.Kind && pin.Name == plan.Pin.Name
}

func sameResolvedMarketplaceIdentity(existing resolver.ResolvedArtifact, plan *marketplace.PreparedInstall) bool {
	existingURL := canonicalMarketplaceURL(existing.MarketplaceURL)
	planURL := canonicalMarketplaceURL(plan.Entry.URL)
	return existing.IsMarketplace && existingURL != "" && existingURL == planURL &&
		existing.MarketplaceInstanceID == plan.Entry.InstanceID && existing.MarketplaceSourceID == plan.Pin.SourceID &&
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
	for _, finding := range plan.Artifact.Manifest.Findings {
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
	if (len(plan.Artifact.Manifest.Findings) > 0 || len(plan.Artifact.Manifest.Capabilities) > 0) && !accept {
		return fmt.Errorf("marketplace install requires risk acceptance: %d finding(s), %d active capability(ies); re-run with --accept-risk (or --yes)", len(plan.Artifact.Manifest.Findings), len(plan.Artifact.Manifest.Capabilities))
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

func marketplaceInstallPreview(plan *marketplace.PreparedInstall) (string, error) {
	m := plan.Artifact.Manifest
	findings := make([]marketplacePreviewFinding, 0, len(m.Findings))
	for _, finding := range m.Findings {
		findings = append(findings, marketplacePreviewFinding{
			Code: marketplace.TerminalString(finding.Code), Severity: marketplace.TerminalString(finding.Severity), Message: marketplace.TerminalString(finding.Message),
			File: marketplace.TerminalString(finding.File), Line: finding.Line, Analyzer: marketplace.TerminalString(finding.Analyzer),
			PolicyOutcome: marketplace.TerminalString(finding.PolicyOutcome),
		})
	}
	slices.SortFunc(findings, func(a, b marketplacePreviewFinding) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return strings.Compare(string(left), string(right))
	})
	caps := slices.Clone(m.Capabilities)
	sort.Strings(caps)
	findingsJSON := "none"
	if len(findings) > 0 {
		encoded, err := json.Marshal(findings)
		if err != nil {
			return "", fmt.Errorf("encode Marketplace finding preview: %w", err)
		}
		findingsJSON = string(encoded)
	}
	capabilitiesJSON, err := json.Marshal(marketplace.TerminalStrings(caps))
	if err != nil {
		return "", fmt.Errorf("encode Marketplace capability preview: %w", err)
	}
	capabilities := string(capabilitiesJSON)
	if len(caps) == 0 {
		capabilities = marketplace.QuoteTerminal("none")
	}
	provenance := fmt.Sprintf("registry_url=%s ref=%s path=%s generator=%s", marketplace.QuoteTerminal(m.Provenance.RegistryURL), marketplace.QuoteTerminal(m.Provenance.Ref), marketplace.QuoteTerminal(m.Provenance.Path), marketplace.QuoteTerminal(m.Provenance.Generator))
	return fmt.Sprintf("Marketplace install preview:\nmarketplace: %s (%s)\ninstance_id: %s\nsource: %s (%s)\nkind: %s\nname: %s\ninstalled_name: %s\nrevision: %s\ncontent_digest: %s\narchive_digest: %s\nacquisition: %s\nlicense: %s\nfindings: %s\ncapabilities: %s\nprovenance: %s", marketplace.QuoteTerminal(displayMarketplace(plan)), marketplace.QuoteTerminal(plan.Entry.URL), marketplace.QuoteTerminal(plan.Entry.InstanceID), marketplace.QuoteTerminal(plan.Pin.SourceSlug), marketplace.QuoteTerminal(plan.Pin.SourceID), marketplace.QuoteTerminal(plan.Pin.Kind), marketplace.QuoteTerminal(plan.Pin.Name), marketplace.QuoteTerminal(nonEmpty(plan.Pin.InstalledName, plan.Pin.Name)), marketplace.QuoteTerminal(plan.Pin.Revision), marketplace.QuoteTerminal(plan.Pin.Digest), marketplace.QuoteTerminal(plan.ArchiveDigest), marketplace.QuoteTerminal("mirror"), marketplace.QuoteTerminal(nonEmpty(m.License, "none")), findingsJSON, capabilities, provenance), nil
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
