package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

var ErrInvalidConfiguration = errors.New("marketplace/admin: invalid configuration")

type Deployment = config.Management

type Profile = config.CredentialProfile

type LicensePolicy struct {
	Allowlist  []string `json:"allowlist"`
	Undeclared string   `json:"undeclared"`
}

type DistributionPolicy struct {
	RejectedResources string `json:"rejected_resources"`
}

type Registry struct {
	Paused              bool                  `json:"paused"`
	Withdrawn           bool                  `json:"withdrawn"`
	Retired             bool                  `json:"retired"`
	ID                  store.RegistryID      `json:"id"`
	Slug                string                `json:"slug"`
	Aliases             []string              `json:"aliases"`
	Origin              string                `json:"origin"`
	HistoricalOrigins   []string              `json:"historical_origins"`
	Ref                 string                `json:"ref"`
	Roots               []store.RegistryRoot  `json:"roots"`
	Acquisition         store.AcquisitionMode `json:"acquisition"`
	Publisher           store.Publisher       `json:"publisher"`
	License             string                `json:"license"`
	ProfileID           string                `json:"profile_id,omitempty"`
	LicenseOverride     *LicensePolicy        `json:"license_override,omitempty"`
	RejectedResources   string                `json:"rejected_resources,omitempty"`
	NonLicenseOverrides map[string]any        `json:"non_license_overrides,omitempty"`
	Generation          int64                 `json:"generation"`
	Lifecycle           string                `json:"lifecycle"`
}

type Snapshot struct {
	Version            int                `json:"version"`
	PolicyRevision     int64              `json:"policy_revision"`
	LicensePolicy      LicensePolicy      `json:"license_policy"`
	DistributionPolicy DistributionPolicy `json:"distribution_policy"`
	Registries         []Registry         `json:"registries"`
	SourceFingerprint  string             `json:"source_fingerprint"`
}

type Service struct {
	Metadata   store.MetadataStore
	Deployment Deployment
}

type AdoptionRequest struct {
	Actor          store.ManagedActor
	Reason         string
	RequestID      string
	IdempotencyKey string
}

func (s Service) Adopt(ctx context.Context, cfg config.Config, req AdoptionRequest) (store.ManagedChange, error) {
	if s.Metadata == nil || cfg.Instance.ID == "" || len(cfg.Registries) > 10000 || !validUndeclared(cfg.Policy.LicenseUndeclared) || !validRejectedResources(cfg.Policy.RejectedResources, true) {
		return store.ManagedChange{}, ErrInvalidConfiguration
	}
	snapshot := Snapshot{Version: 1, PolicyRevision: 1, LicensePolicy: LicensePolicy{Allowlist: cfg.Policy.LicenseAllowlist, Undeclared: cfg.Policy.LicenseUndeclared}, DistributionPolicy: DistributionPolicy{RejectedResources: cfg.Policy.RejectedResourcesOutcome()}, Registries: make([]Registry, 0, len(cfg.Registries))}
	seen := make(map[store.RegistryID]bool)
	for _, source := range cfg.Registries {
		registry, err := s.adoptRegistry(source, cfg.Policy)
		if err != nil || seen[registry.ID] {
			return store.ManagedChange{}, ErrInvalidConfiguration
		}
		seen[registry.ID] = true
		snapshot.Registries = append(snapshot.Registries, registry)
	}
	slices.SortFunc(snapshot.Registries, func(a, b Registry) int { return strings.Compare(string(a.ID), string(b.ID)) })
	sourceBytes, err := json.Marshal(snapshot)
	if err != nil {
		return store.ManagedChange{}, ErrInvalidConfiguration
	}
	snapshot.SourceFingerprint = digest(sourceBytes)
	requestBytes, err := json.Marshal(struct {
		Source string
		Reason string
	}{snapshot.SourceFingerprint, req.Reason})
	if err != nil {
		return store.ManagedChange{}, ErrInvalidConfiguration
	}
	var accepted store.ManagedChange
	slog.InfoContext(ctx, "marketplace configuration adoption started", "registry_count", len(snapshot.Registries))
	err = s.Metadata.WithTx(ctx, func(ctx context.Context, tx store.MetadataStore) error {
		managed, ok := tx.(store.ManagedConfigurationStore)
		if !ok {
			return store.ErrTopology
		}
		prior, err := managed.FindManagedChange(ctx, store.MarketplaceID(cfg.Instance.ID), req.Actor, "configuration.adopt", req.IdempotencyKey)
		if err == nil {
			if prior.RequestDigest != digest(requestBytes) {
				return store.ErrManagedIdempotencyConflict
			}
			accepted = prior
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		existing, err := tx.ListRegistries(ctx)
		if err != nil {
			return fmt.Errorf("read adoption identities: %w", err)
		}
		byID := make(map[store.RegistryID]store.Registry, len(existing))
		names := make(map[string]store.RegistryID)
		for _, registry := range existing {
			if !seen[registry.ID] {
				return ErrInvalidConfiguration
			}
			byID[registry.ID] = registry
			for _, name := range append(slices.Clone(registry.Aliases), registry.Slug) {
				if owner, exists := names[name]; exists && owner != registry.ID {
					return ErrInvalidConfiguration
				}
				names[name] = registry.ID
			}
		}
		for i := range snapshot.Registries {
			r := &snapshot.Registries[i]
			prior := byID[r.ID]
			r.Aliases = uniqueNames(append(r.Aliases, append(prior.Aliases, prior.Slug)...), r.Slug)
			r.HistoricalOrigins = uniqueNames(prior.Origins, r.Origin)
			for _, name := range append(slices.Clone(r.Aliases), r.Slug) {
				if owner, exists := names[name]; exists && owner != r.ID {
					return ErrInvalidConfiguration
				}
				names[name] = r.ID
			}
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return ErrInvalidConfiguration
		}
		if _, err := tx.GetMarketplace(ctx, store.MarketplaceID(cfg.Instance.ID)); errors.Is(err, store.ErrNotFound) {
			if err := tx.PutMarketplace(ctx, store.Marketplace{ID: store.MarketplaceID(cfg.Instance.ID), Name: cfg.Instance.Name, CanonicalURL: cfg.Instance.CanonicalURL}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		jobIdentity, err := json.Marshal([]string{cfg.Instance.ID, req.Actor.Issuer, req.Actor.Subject, "adopt", req.IdempotencyKey})
		if err != nil {
			return ErrInvalidConfiguration
		}
		jobKey := "managed-adopt-" + digest(jobIdentity)
		payload, err := json.Marshal(struct {
			Marketplace string `json:"marketplace_id"`
			Revision    int64  `json:"revision"`
		}{cfg.Instance.ID, 1})
		if err != nil {
			return ErrInvalidConfiguration
		}
		accepted, err = managed.ApplyManagedConfiguration(ctx, store.ManagedChangeRequest{Change: store.ManagedChange{
			Marketplace: store.MarketplaceID(cfg.Instance.ID), Actor: req.Actor, Operation: "configuration.adopt", Reason: req.Reason,
			RequestID: req.RequestID, IdempotencyKey: req.IdempotencyKey, RequestDigest: digest(requestBytes), Snapshot: encoded,
		}, Jobs: []store.Job{{ID: store.JobID(jobKey), Kind: "managed-reconcile", Payload: payload, IdempotencyKey: jobKey, MaxAttempts: 5}}})
		return err
	})
	if err != nil {
		slog.WarnContext(ctx, "marketplace configuration adoption rejected")
		return store.ManagedChange{}, err
	}
	slog.InfoContext(ctx, "marketplace configuration adoption accepted", "revision", accepted.Revision)
	return accepted, nil
}

func (s Service) adoptRegistry(source config.Registry, policy config.Policy) (Registry, error) {
	if err := source.Validate(policy); err != nil || !originAllowed(source.Origin, s.Deployment.AllowedOrigins) {
		return Registry{}, ErrInvalidConfiguration
	}
	registry := Registry{ID: store.RegistryID(source.ID), Slug: source.Slug, Aliases: slices.Clone(source.Aliases), Origin: source.Origin,
		Ref: source.Ref, Acquisition: store.AcquisitionMode(source.Acquisition), Publisher: store.Publisher{Name: source.Publisher.Name, URL: source.Publisher.URL},
		License: source.License, Generation: 1, Lifecycle: "active", NonLicenseOverrides: make(map[string]any)}
	for _, root := range source.Roots {
		registry.Roots = append(registry.Roots, store.RegistryRoot{Path: root.Path, SourceHarness: root.SourceHarness})
	}
	if source.CAFile != "" || source.Credentials.SSHKey != nil || source.Credentials.Token != nil {
		for id, profile := range s.Deployment.Profiles {
			if profile.CAFile == source.CAFile && reflect.DeepEqual(profile.Credentials, source.Credentials) && originAllowed(source.Origin, profile.AllowedOrigins) {
				if registry.ProfileID != "" || id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\\r\n\x00 ") {
					return Registry{}, ErrInvalidConfiguration
				}
				registry.ProfileID = id
			}
		}
		if registry.ProfileID == "" {
			return Registry{}, ErrInvalidConfiguration
		}
	}
	for key, value := range source.PolicyOverrides {
		switch key {
		case "license_allowlist":
			encoded, err := json.Marshal(value)
			var licenses []string
			if err != nil || json.Unmarshal(encoded, &licenses) != nil || len(licenses) == 0 {
				return Registry{}, ErrInvalidConfiguration
			}
			if registry.LicenseOverride == nil {
				registry.LicenseOverride = &LicensePolicy{}
			}
			registry.LicenseOverride.Allowlist = licenses
		case "license_undeclared":
			outcome, ok := value.(string)
			if !ok || !validUndeclared(outcome) {
				return Registry{}, ErrInvalidConfiguration
			}
			if registry.LicenseOverride == nil {
				registry.LicenseOverride = &LicensePolicy{}
			}
			registry.LicenseOverride.Undeclared = outcome
		case "rejected_resources":
			mode, ok := value.(string)
			if !ok || !validRejectedResources(mode, false) {
				return Registry{}, ErrInvalidConfiguration
			}
			registry.RejectedResources = mode
		default:
			registry.NonLicenseOverrides[key] = value
		}
	}
	return registry, nil
}

func originAllowed(origin string, allowed []string) bool {
	if strings.HasPrefix(origin, "git@") && !strings.Contains(origin, "://") {
		host, path, ok := strings.Cut(strings.TrimPrefix(origin, "git@"), ":")
		if !ok || strings.ContainsAny(host, "/?#@") || path == "" {
			return false
		}
		origin = "ssh://git@" + host + "/" + path
	}
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || u.Path == "" || strings.ContainsAny(u.Path, "\\\x00\r\n") {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return false
	}
	if u.User != nil {
		if _, password := u.User.Password(); password || u.Scheme != "ssh" || u.User.Username() != "git" {
			return false
		}
	}
	authority := u.Scheme + "://" + strings.ToLower(u.Host)
	return slices.Contains(allowed, authority)
}

func validUndeclared(value string) bool {
	return value != "" && config.ResolveUndeclaredLicenseOutcome(value) == value
}

func uniqueNames(values []string, exclude string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && value != exclude {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
