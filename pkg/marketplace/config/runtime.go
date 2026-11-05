package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

type indexingRootProjection struct {
	Path          string `json:"path"`
	SourceHarness string `json:"source_harness"`
}

type indexingCredentialProjection struct {
	SSHKeyFile     string `json:"ssh_key_file"`
	SSHKeyValueSet bool   `json:"ssh_key_value_set"`
	TokenFile      string `json:"token_file"`
	TokenEnv       string `json:"token_env"`
	TokenValueSet  bool   `json:"token_value_set"`
}

type indexingLimitsProjection struct {
	RepositoryBytes   int64    `json:"repository_bytes"`
	FileCount         int      `json:"file_count"`
	FileBytes         int64    `json:"file_bytes"`
	ArtifactBytes     int64    `json:"artifact_bytes"`
	CloneDuration     Duration `json:"clone_duration"`
	IngestionDuration Duration `json:"ingestion_duration"`
}

type indexingPolicyProjection struct {
	HiddenContent     string                   `json:"hidden_content"`
	LicenseAllowlist  []string                 `json:"license_allowlist"`
	LicenseUndeclared string                   `json:"license_undeclared"`
	RejectedResources string                   `json:"rejected_resources"`
	Limits            indexingLimitsProjection `json:"limits"`
}

type indexingGenerationProjection struct {
	Version             int                          `json:"version"`
	Origin              string                       `json:"origin"`
	Ref                 string                       `json:"ref"`
	Roots               []indexingRootProjection     `json:"roots"`
	ResolvedAcquisition string                       `json:"resolved_acquisition"`
	License             string                       `json:"license"`
	CAFile              string                       `json:"ca_file"`
	Credentials         indexingCredentialProjection `json:"credentials"`
	PolicyOverrides     map[string]any               `json:"policy_overrides"`
	Policy              indexingPolicyProjection     `json:"policy"`
}

// IndexingGeneration identifies the canonical input that can change one
// registry's indexed result.
func IndexingGeneration(cfg Config, registry Registry) (string, error) {
	roots := make([]indexingRootProjection, len(registry.Roots))
	for i, root := range registry.Roots {
		roots[i] = indexingRootProjection(root)
	}
	acquisition := registry.Acquisition
	if acquisition == "" {
		acquisition = cfg.AcquisitionDefault
	}
	credentials := indexingCredentialProjection{}
	if registry.Credentials.SSHKey != nil {
		credentials.SSHKeyFile = registry.Credentials.SSHKey.File
		credentials.SSHKeyValueSet = registry.Credentials.SSHKey.Value != ""
	}
	if registry.Credentials.Token != nil {
		credentials.TokenFile = registry.Credentials.Token.File
		credentials.TokenEnv = registry.Credentials.Token.Env
		credentials.TokenValueSet = registry.Credentials.Token.Value != ""
	}
	overrides := make(map[string]any, len(registry.PolicyOverrides))
	for key, value := range registry.PolicyOverrides {
		overrides[key] = value
	}
	policy := indexingPolicyProjection{
		HiddenContent:     cfg.Policy.HiddenContent,
		LicenseAllowlist:  append([]string(nil), cfg.Policy.LicenseAllowlist...),
		LicenseUndeclared: cfg.Policy.LicenseUndeclared,
		RejectedResources: cfg.Policy.RejectedResources,
		Limits: indexingLimitsProjection{
			RepositoryBytes:   cfg.Policy.Limits.RepositoryBytes,
			FileCount:         cfg.Policy.Limits.FileCount,
			FileBytes:         cfg.Policy.Limits.FileBytes,
			ArtifactBytes:     cfg.Policy.Limits.ArtifactBytes,
			CloneDuration:     cfg.Policy.Limits.CloneDuration,
			IngestionDuration: cfg.Policy.Limits.IngestionDuration,
		},
	}
	data, err := json.Marshal(indexingGenerationProjection{
		Version:             1,
		Origin:              registry.Origin,
		Ref:                 registry.Ref,
		Roots:               roots,
		ResolvedAcquisition: acquisition,
		License:             registry.License,
		CAFile:              registry.CAFile,
		Credentials:         credentials,
		PolicyOverrides:     overrides,
		Policy:              policy,
	})
	if err != nil {
		return "", fmt.Errorf("marshal indexing generation: %w", err)
	}
	digest := sha256.Sum256(data)
	return "semantic-v1:" + hex.EncodeToString(digest[:]), nil
}

// Classification describes the worker-safe effects of a valid configuration
// candidate relative to the currently running configuration.
type Classification struct {
	Added                  []string
	Removed                []string
	IndexingChanged        []string
	MetadataChanged        []string
	RefreshIntervalChanged bool
	RestartRequired        []string
}

// Classify compares a parsed candidate with startup and running configurations.
func Classify(startup, running, candidate Config) (Classification, error) {
	classification := Classification{
		RefreshIntervalChanged: running.Refresh.Interval != candidate.Refresh.Interval,
	}
	startupOwned := []struct {
		name string
		diff bool
	}{
		{"listen", startup.Listen != candidate.Listen},
		{"operations_listen", startup.OperationsListen != candidate.OperationsListen},
		{"storage", !reflect.DeepEqual(startup.Storage, candidate.Storage)},
		{"instance", !reflect.DeepEqual(startup.Instance, candidate.Instance)},
		{"revocations", !reflect.DeepEqual(startup.Revocations, candidate.Revocations)},
		{"management", !reflect.DeepEqual(startup.Management, candidate.Management)},
		{"admin", !reflect.DeepEqual(startup.Admin, candidate.Admin)},
		{"catalog_projection", !reflect.DeepEqual(startup.CatalogProjection, candidate.CatalogProjection)},
		{"installation_metrics", !reflect.DeepEqual(startup.InstallationMetrics, candidate.InstallationMetrics)},
		{"refresh_concurrency", startup.RefreshConcurrency != candidate.RefreshConcurrency},
	}
	for _, field := range startupOwned {
		if field.diff {
			classification.RestartRequired = append(classification.RestartRequired, field.name)
		}
	}

	runningByID := make(map[string]Registry, len(running.Registries))
	for _, registry := range running.Registries {
		runningByID[registry.ID] = registry
	}
	candidateByID := make(map[string]Registry, len(candidate.Registries))
	for _, registry := range candidate.Registries {
		candidateByID[registry.ID] = registry
		if _, exists := runningByID[registry.ID]; !exists {
			classification.Added = append(classification.Added, registry.ID)
			continue
		}
		previous := runningByID[registry.ID]
		previousGeneration, err := IndexingGeneration(running, previous)
		if err != nil {
			return Classification{}, fmt.Errorf("index running registry %q: %w", registry.ID, err)
		}
		nextGeneration, err := IndexingGeneration(candidate, registry)
		if err != nil {
			return Classification{}, fmt.Errorf("index candidate registry %q: %w", registry.ID, err)
		}
		if previousGeneration != nextGeneration {
			classification.IndexingChanged = append(classification.IndexingChanged, registry.ID)
		} else if previous.Slug != registry.Slug ||
			!reflect.DeepEqual(previous.Aliases, registry.Aliases) ||
			!reflect.DeepEqual(previous.Publisher, registry.Publisher) {
			classification.MetadataChanged = append(classification.MetadataChanged, registry.ID)
		}
	}
	for _, registry := range running.Registries {
		if _, exists := candidateByID[registry.ID]; !exists {
			classification.Removed = append(classification.Removed, registry.ID)
		}
	}
	return classification, nil
}
