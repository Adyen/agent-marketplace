// Package config loads the Marketplace operator configuration file.
//
// Operator configuration is separate from the layered consumer configuration in
// internal/config. It defines the Marketplace instance, storage backends,
// indexing policy, approved sources, and revocations.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/publicurl"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"gopkg.in/yaml.v3"
)

const (
	apiVersionV1 = "agent-marketplace.io/marketplace/v1"

	defaultListen             = "127.0.0.1:8080"
	defaultOperationsListen   = "127.0.0.1:9090"
	defaultRefreshInterval    = 30 * time.Minute
	defaultHiddenContent      = "reject"
	defaultAcquisitionDefault = "mirror"
	defaultRepositoryBytes    = 100 * 1024 * 1024
	defaultFileCount          = 1024
	defaultFileBytes          = 2 * 1024 * 1024
	defaultArtifactBytes      = 16 * 1024 * 1024
	defaultCloneDuration      = 5 * time.Minute

	// DefaultArtifactBytes is the default maximum size for a published artifact
	// and its digest-addressed manifest payloads.
	DefaultArtifactBytes     = defaultArtifactBytes
	defaultIngestionDuration = 2 * time.Minute
	defaultMaxFindings       = 1000
)

var (
	validMetadataDrivers = map[string]bool{"sqlite": true, "postgres": true}
	validBlobDrivers     = map[string]bool{"filesystem": true, "s3": true}
	validAcquisition     = map[string]bool{"mirror": true, "index-only": true}
	validHiddenContent   = map[string]bool{"reject": true, "warn": true}
	validSourceHarnesses = map[string]bool{
		"":         true,
		"factory":  true,
		"claude":   true,
		"cursor":   true,
		"windsurf": true,
		"gemini":   true,
		"devin":    true,
	}
	defaultLicenseAllowlist = []string{"MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause"}
)

// Duration is a time.Duration that can be unmarshalled from a YAML string such
// as "30m" or from an integer number of nanoseconds.
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	switch node.Tag {
	case "!!int":
		var ns int64
		if err := node.Decode(&ns); err != nil {
			return err
		}
		*d = Duration(ns)
		return nil
	case "!!str", "":
		v, err := time.ParseDuration(node.Value)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", node.Value, err)
		}
		*d = Duration(v)
		return nil
	default:
		return fmt.Errorf("invalid duration type %q", node.Tag)
	}
}

func (d Duration) Duration() time.Duration { return time.Duration(d) }

// Config is the top-level Marketplace operator configuration.
type Config struct {
	APIVersion         string      `yaml:"apiVersion"`
	Instance           Instance    `yaml:"instance"`
	Listen             string      `yaml:"listen"`
	OperationsListen   string      `yaml:"operations_listen"`
	Storage            Storage     `yaml:"storage"`
	AcquisitionDefault string      `yaml:"acquisition_default"`
	Refresh            Refresh     `yaml:"refresh"`
	RefreshConcurrency int         `yaml:"refresh_concurrency,omitempty"`
	Policy             Policy      `yaml:"policy"`
	Sources            []Source    `yaml:"sources"`
	Revocations        Revocations `yaml:"revocations"`
}

// Instance describes the Marketplace deployment.
type Instance struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	CanonicalURL string `yaml:"canonical_url"`
	BrowserBase  string `yaml:"browser_base,omitempty"`
}

// Storage configures the metadata and blob stores.
type Storage struct {
	Metadata StorageMetadata `yaml:"metadata"`
	Blobs    StorageBlobs    `yaml:"blobs"`
}

// StorageMetadata configures the metadata backend.
type StorageMetadata struct {
	Driver  string `yaml:"driver"`
	DSN     string `yaml:"dsn"`
	DSNEnv  string `yaml:"dsn_env"`
	DSNFile string `yaml:"dsn_file"`
}

// ResolveDSN returns the configured metadata DSN without exposing its value in
// errors. Exactly one literal, environment, or file source must be selected.
func (s StorageMetadata) ResolveDSN() (string, error) {
	var selected []string
	if strings.TrimSpace(s.DSN) != "" {
		selected = append(selected, "dsn")
	}
	if strings.TrimSpace(s.DSNEnv) != "" {
		selected = append(selected, "dsn_env")
	}
	if strings.TrimSpace(s.DSNFile) != "" {
		selected = append(selected, "dsn_file")
	}
	if len(selected) != 1 {
		return "", fmt.Errorf("storage.metadata must select exactly one usable DSN source (dsn, dsn_env, or dsn_file); selected %v", selected)
	}

	switch selected[0] {
	case "dsn":
		return strings.TrimSpace(s.DSN), nil
	case "dsn_env":
		name := strings.TrimSpace(s.DSNEnv)
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("storage.metadata.dsn_env %q is unset or empty", name)
		}
		return strings.TrimSpace(value), nil
	case "dsn_file":
		path := strings.TrimSpace(s.DSNFile)
		value, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read storage.metadata.dsn_file %q: %w", path, err)
		}
		if strings.TrimSpace(string(value)) == "" {
			return "", fmt.Errorf("storage.metadata.dsn_file %q is empty", path)
		}
		return strings.TrimSpace(string(value)), nil
	default:
		return "", errors.New("storage.metadata DSN source is invalid")
	}
}

// StorageBlobs configures the blob backend.
type StorageBlobs struct {
	Driver   string `yaml:"driver"`
	Path     string `yaml:"path"`
	Bucket   string `yaml:"bucket"`
	Region   string `yaml:"region"`
	Endpoint string `yaml:"endpoint"`
}

// Refresh configures the background refresh scheduler.
type Refresh struct {
	Interval Duration `yaml:"interval"`
}

// Policy configures the global indexing policy.
type Policy struct {
	HiddenContent    string   `yaml:"hidden_content"`
	LicenseAllowlist []string `yaml:"license_allowlist"`
	Limits           Limits   `yaml:"limits"`
}

// Limits are resource and timing bounds enforced by the worker.
type Limits struct {
	RepositoryBytes   int64    `yaml:"repository_bytes"`
	FileCount         int      `yaml:"file_count"`
	FileBytes         int64    `yaml:"file_bytes"`
	ArtifactBytes     int64    `yaml:"artifact_bytes"`
	CloneDuration     Duration `yaml:"clone_duration"`
	IngestionDuration Duration `yaml:"ingestion_duration"`
}

// Source is an approved source repository.
type Source struct {
	ID              string         `yaml:"id"`
	Slug            string         `yaml:"slug"`
	Aliases         []string       `yaml:"aliases"`
	Origin          string         `yaml:"origin"`
	Ref             string         `yaml:"ref"`
	Acquisition     string         `yaml:"acquisition"`
	Publisher       Publisher      `yaml:"publisher"`
	License         string         `yaml:"license"`
	Roots           []SourceRoot   `yaml:"roots"`
	Credentials     Credentials    `yaml:"credentials"`
	PolicyOverrides map[string]any `yaml:"policy_overrides"`
}

// Publisher records publisher metadata for a source.
type Publisher struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// SourceRoot describes one ingest root within a source repository.
type SourceRoot struct {
	Path          string `yaml:"path"`
	SourceHarness string `yaml:"source_harness"`
}

// Credentials carries optional server-side credential references for a source.
// Secret values are never allowed; only file or environment references.
type Credentials struct {
	SSHKey *SSHKeyCredential `yaml:"ssh_key"`
	Token  *TokenCredential  `yaml:"token"`
}

// SSHKeyCredential references an SSH private key file.
type SSHKeyCredential struct {
	File  string `yaml:"file"`
	Value string `yaml:"value"`
}

// TokenCredential references a token via a file or environment variable.
type TokenCredential struct {
	File  string `yaml:"file"`
	Env   string `yaml:"env"`
	Value string `yaml:"value"`
}

// Revocations lists operator-configured revocations from broad to narrow:
// source IDs, complete coordinates, and exact coordinate-plus-digest targets.
type Revocations struct {
	SourceIDs   []string `yaml:"source_ids"`
	Coordinates []string `yaml:"coordinates"`
	Revisions   []string `yaml:"revisions"`
}

// LoadConfig reads and validates the operator configuration at path.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Listen == "" {
		cfg.Listen = defaultListen
	}
	if cfg.OperationsListen == "" {
		cfg.OperationsListen = defaultOperationsListen
	}
	if cfg.Refresh.Interval == 0 {
		cfg.Refresh.Interval = Duration(defaultRefreshInterval)
	}
	if cfg.AcquisitionDefault == "" {
		cfg.AcquisitionDefault = defaultAcquisitionDefault
	}
	if cfg.Policy.HiddenContent == "" {
		cfg.Policy.HiddenContent = defaultHiddenContent
	}
	if cfg.Policy.LicenseAllowlist == nil {
		cfg.Policy.LicenseAllowlist = defaultLicenseAllowlist
	}
	if cfg.Policy.Limits.RepositoryBytes == 0 {
		cfg.Policy.Limits.RepositoryBytes = defaultRepositoryBytes
	}
	if cfg.Policy.Limits.FileCount == 0 {
		cfg.Policy.Limits.FileCount = defaultFileCount
	}
	if cfg.Policy.Limits.FileBytes == 0 {
		cfg.Policy.Limits.FileBytes = defaultFileBytes
	}
	if cfg.Policy.Limits.ArtifactBytes == 0 {
		cfg.Policy.Limits.ArtifactBytes = defaultArtifactBytes
	}
	if cfg.Policy.Limits.CloneDuration == 0 {
		cfg.Policy.Limits.CloneDuration = Duration(defaultCloneDuration)
	}
	if cfg.Policy.Limits.IngestionDuration == 0 {
		cfg.Policy.Limits.IngestionDuration = Duration(defaultIngestionDuration)
	}
	for i := range cfg.Sources {
		if cfg.Sources[i].Acquisition == "" {
			cfg.Sources[i].Acquisition = cfg.AcquisitionDefault
		}
		for j := range cfg.Sources[i].Roots {
			if cfg.Sources[i].Roots[j].Path == "" {
				cfg.Sources[i].Roots[j].Path = "."
			}
		}
	}
}

// Validate returns a combined error for invalid configuration.
func (cfg Config) Validate() error {
	var errs []error
	if cfg.APIVersion != apiVersionV1 {
		errs = append(errs, fmt.Errorf("apiVersion must be %q", apiVersionV1))
	}
	err := validateInstance(cfg.Instance)
	if err != nil {
		errs = append(errs, err)
	}
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		errs = append(errs, fmt.Errorf("invalid listen address %q: %w", cfg.Listen, err))
	}
	if _, _, err := net.SplitHostPort(cfg.OperationsListen); err != nil {
		errs = append(errs, fmt.Errorf("invalid operations_listen address %q: %w", cfg.OperationsListen, err))
	}
	errs = append(errs, cfg.Storage.Validate()...)
	if !validAcquisition[cfg.AcquisitionDefault] {
		errs = append(errs, fmt.Errorf("invalid acquisition_default %q", cfg.AcquisitionDefault))
	}
	if cfg.Refresh.Interval.Duration() <= 0 {
		errs = append(errs, errors.New("refresh.interval must be a positive duration"))
	}
	if cfg.RefreshConcurrency < 0 {
		errs = append(errs, errors.New("refresh_concurrency must not be negative"))
	}
	errs = append(errs, cfg.Policy.Validate()...)
	seenIDs := make(map[string]bool)
	seenSlugs := make(map[string]bool)
	for i, src := range cfg.Sources {
		errs = append(errs, validateSource(i, src, cfg.Policy)...)
		if seenIDs[src.ID] {
			errs = append(errs, fmt.Errorf("sources[%d].id %q is duplicated", i, src.ID))
		}
		seenIDs[src.ID] = true
		if seenSlugs[src.Slug] {
			errs = append(errs, fmt.Errorf("sources[%d].slug %q is duplicated", i, src.Slug))
		}
		seenSlugs[src.Slug] = true
	}
	errs = append(errs, cfg.Revocations.ValidateForMarketplace(cfg.Instance.ID)...)
	return errors.Join(errs...)
}

func validateInstance(inst Instance) error {
	if strings.TrimSpace(inst.ID) == "" {
		return errors.New("instance.id is required")
	}
	if strings.TrimSpace(inst.Name) == "" {
		return errors.New("instance.name is required")
	}
	if strings.TrimSpace(inst.CanonicalURL) == "" {
		return errors.New("instance.canonical_url is required")
	}
	if err := publicurl.ErrorForField("instance.canonical_url", inst.CanonicalURL); err != nil {
		return err
	}
	if strings.Contains(inst.CanonicalURL, "..") {
		return errors.New("instance.canonical_url must not contain path traversal")
	}
	if strings.TrimSpace(inst.BrowserBase) != "" {
		if err := publicurl.ErrorForField("instance.browser_base", inst.BrowserBase); err != nil {
			return err
		}
		if strings.Contains(inst.BrowserBase, "..") {
			return errors.New("instance.browser_base must not contain path traversal")
		}
	}
	return nil
}

// validateMetadataDSN checks only the syntax and driver compatibility of a
// resolved metadata DSN. It intentionally never includes the DSN in an error.
func validateMetadataDSN(driver, dsn string) error {
	dsn = strings.TrimSpace(dsn)
	switch driver {
	case "sqlite":
		// In-memory, read-only, and immutable SQLite databases are useful in
		// unit tests but cannot provide durable, writable production metadata.
		if dsn == ":memory:" || strings.HasPrefix(strings.ToLower(dsn), "file::memory:") {
			return errors.New("storage.metadata sqlite DSN must be a writable filesystem database")
		}
		if strings.HasPrefix(dsn, "file:") {
			u, err := url.Parse(dsn)
			if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || (u.Path == "" && u.Opaque == "") {
				return errors.New("storage.metadata DSN is not valid for sqlite driver")
			}
			for key, values := range u.Query() {
				for _, value := range values {
					switch {
					case strings.EqualFold(key, "mode") && (strings.EqualFold(value, "memory") || strings.EqualFold(value, "ro")):
						return errors.New("storage.metadata sqlite DSN must not use memory or read-only mode")
					case strings.EqualFold(key, "immutable") && (value == "1" || strings.EqualFold(value, "true")):
						return errors.New("storage.metadata sqlite DSN must not use immutable mode")
					}
				}
			}
			return nil
		}
		if strings.Contains(dsn, "=") || strings.Contains(dsn, "://") || strings.IndexByte(dsn, 0) >= 0 {
			return errors.New("storage.metadata DSN is not valid for sqlite driver")
		}
		return nil
	case "postgres":
		if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
			u, err := url.Parse(dsn)
			if err != nil || u.Host == "" {
				return errors.New("storage.metadata DSN is not valid for postgres driver")
			}
			if port := u.Port(); port != "" {
				portNumber, portErr := strconv.Atoi(port)
				if portErr != nil || portNumber < 1 || portNumber > 65535 {
					return errors.New("storage.metadata DSN is not valid for postgres driver")
				}
			}
			return nil
		}
		if strings.HasPrefix(dsn, "file:") || dsn == ":memory:" || !validPostgresKeyValueDSN(dsn) {
			return errors.New("storage.metadata DSN is not valid for postgres driver")
		}
		return nil
	default:
		return nil
	}
}

func validPostgresKeyValueDSN(dsn string) bool {
	seen := false
	for i := 0; i < len(dsn); {
		for i < len(dsn) && (dsn[i] == ' ' || dsn[i] == '\t' || dsn[i] == '\n' || dsn[i] == '\r') {
			i++
		}
		if i == len(dsn) || !isDSNKeyStart(dsn[i]) {
			return false
		}
		start := i
		for i < len(dsn) && isDSNKeyChar(dsn[i]) {
			i++
		}
		if i == start || i >= len(dsn) || dsn[i] != '=' {
			return false
		}
		i++
		if i < len(dsn) && (dsn[i] == '\'' || dsn[i] == '"') {
			quote := dsn[i]
			i++
			closed := false
			for i < len(dsn) {
				if dsn[i] == '\\' && i+1 < len(dsn) {
					i += 2
					continue
				}
				if dsn[i] == quote {
					i++
					closed = true
					break
				}
				i++
			}
			if !closed || (i < len(dsn) && dsn[i] != ' ' && dsn[i] != '\t' && dsn[i] != '\n' && dsn[i] != '\r') {
				return false
			}
		} else {
			start = i
			for i < len(dsn) && dsn[i] != ' ' && dsn[i] != '\t' && dsn[i] != '\n' && dsn[i] != '\r' {
				i++
			}
			if i == start {
				return false
			}
		}
		seen = true
	}
	return seen
}

func isDSNKeyStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isDSNKeyChar(c byte) bool {
	return isDSNKeyStart(c) || (c >= '0' && c <= '9')
}

// Validate validates the storage configuration.
func (s Storage) Validate() []error {
	var errs []error
	if !validMetadataDrivers[s.Metadata.Driver] {
		errs = append(errs, fmt.Errorf("storage.metadata.driver must be one of %v", keys(validMetadataDrivers)))
	}
	dsn, dsnErr := s.Metadata.ResolveDSN()
	if dsnErr != nil {
		errs = append(errs, dsnErr)
	} else if validMetadataDrivers[s.Metadata.Driver] {
		if err := validateMetadataDSN(s.Metadata.Driver, dsn); err != nil {
			errs = append(errs, err)
		}
	}
	if !validBlobDrivers[s.Blobs.Driver] {
		errs = append(errs, fmt.Errorf("storage.blobs.driver must be one of %v", keys(validBlobDrivers)))
	}
	switch s.Blobs.Driver {
	case "filesystem":
		if strings.TrimSpace(s.Blobs.Path) == "" {
			errs = append(errs, errors.New("storage.blobs.path is required for filesystem driver"))
		}
	case "s3":
		if strings.TrimSpace(s.Blobs.Bucket) == "" {
			errs = append(errs, errors.New("storage.blobs.bucket is required for s3 driver"))
		}
		if strings.TrimSpace(s.Blobs.Region) == "" {
			errs = append(errs, errors.New("storage.blobs.region is required for s3 driver"))
		}
	}
	return errs
}

// Validate validates the policy and limits.
func (p Policy) Validate() []error {
	var errs []error
	if !validHiddenContent[p.HiddenContent] {
		errs = append(errs, fmt.Errorf("policy.hidden_content must be one of %v", keys(validHiddenContent)))
	}
	if p.Limits.RepositoryBytes <= 0 {
		errs = append(errs, errors.New("policy.limits.repository_bytes must be positive"))
	}
	if p.Limits.FileCount <= 0 {
		errs = append(errs, errors.New("policy.limits.file_count must be positive"))
	}
	if p.Limits.FileBytes <= 0 {
		errs = append(errs, errors.New("policy.limits.file_bytes must be positive"))
	}
	if p.Limits.ArtifactBytes <= 0 {
		errs = append(errs, errors.New("policy.limits.artifact_bytes must be positive"))
	}
	if p.Limits.CloneDuration.Duration() <= 0 {
		errs = append(errs, errors.New("policy.limits.clone_duration must be positive"))
	}
	if p.Limits.IngestionDuration.Duration() <= 0 {
		errs = append(errs, errors.New("policy.limits.ingestion_duration must be positive"))
	}
	return errs
}

func validateSourceOrigin(origin string) error {
	return publicurl.Validate(origin)
}

func validateSource(idx int, src Source, policy Policy) []error {
	var errs []error
	if strings.TrimSpace(src.ID) == "" {
		errs = append(errs, fmt.Errorf("sources[%d].id is required", idx))
	}
	if strings.TrimSpace(src.Slug) == "" {
		errs = append(errs, fmt.Errorf("sources[%d].slug is required", idx))
	} else if !slugRE.MatchString(src.Slug) {
		errs = append(errs, fmt.Errorf("sources[%d].slug %q contains invalid characters", idx, src.Slug))
	}
	if strings.TrimSpace(src.Origin) == "" {
		errs = append(errs, fmt.Errorf("sources[%d].origin is required", idx))
	} else if err := validateSourceOrigin(src.Origin); err != nil {
		errs = append(errs, fmt.Errorf("sources[%d].origin: %w", idx, err))
	}
	if strings.TrimSpace(src.Publisher.URL) != "" {
		if err := publicurl.ErrorForField("publisher.url", src.Publisher.URL); err != nil {
			errs = append(errs, fmt.Errorf("sources[%d].publisher.url: %w", idx, err))
		}
	}
	if strings.TrimSpace(src.Ref) == "" {
		errs = append(errs, fmt.Errorf("sources[%d].ref is required", idx))
	}
	if len(src.Roots) == 0 {
		errs = append(errs, fmt.Errorf("sources[%d].roots must contain at least one root", idx))
	}
	for i, root := range src.Roots {
		if strings.TrimSpace(root.Path) == "" {
			errs = append(errs, fmt.Errorf("sources[%d].roots[%d].path is required", idx, i))
		}
		if strings.Contains(root.Path, "..") || filepath.IsAbs(root.Path) {
			errs = append(errs, fmt.Errorf("sources[%d].roots[%d].path %q must be relative and not contain '..'", idx, i, root.Path))
		}
		if !validSourceHarnesses[root.SourceHarness] {
			errs = append(errs, fmt.Errorf("sources[%d].roots[%d].source_harness %q is invalid", idx, i, root.SourceHarness))
		}
	}
	if src.Acquisition != "" && !validAcquisition[src.Acquisition] {
		errs = append(errs, fmt.Errorf("sources[%d].acquisition must be 'mirror' or 'index-only'", idx))
	}
	if src.License != "" && len(policy.LicenseAllowlist) > 0 && !slices.Contains(policy.LicenseAllowlist, src.License) {
		errs = append(errs, fmt.Errorf("sources[%d].license %q is not in policy.license_allowlist", idx, src.License))
	}
	errs = append(errs, validateCredentials(idx, src.Credentials)...)
	return errs
}

func validateCredentials(idx int, cred Credentials) []error {
	var errs []error
	hasSSH := cred.SSHKey != nil
	hasToken := cred.Token != nil
	if hasSSH && hasToken {
		errs = append(errs, fmt.Errorf("sources[%d].credentials must specify ssh_key or token, not both", idx))
	}
	if hasSSH {
		if strings.TrimSpace(cred.SSHKey.File) == "" {
			errs = append(errs, fmt.Errorf("sources[%d].credentials.ssh_key.file is required", idx))
		}
		if strings.TrimSpace(cred.SSHKey.Value) != "" {
			errs = append(errs, fmt.Errorf("sources[%d].credentials.ssh_key contains an inline secret value", idx))
		}
	}
	if hasToken {
		hasRef := strings.TrimSpace(cred.Token.File) != "" || strings.TrimSpace(cred.Token.Env) != ""
		if !hasRef {
			errs = append(errs, fmt.Errorf("sources[%d].credentials.token must reference a file or env var", idx))
		}
		if strings.TrimSpace(cred.Token.Value) != "" {
			errs = append(errs, fmt.Errorf("sources[%d].credentials.token contains an inline secret value", idx))
		}
	}
	return errs
}

// Validate validates revocation entries without an expected Marketplace
// identity. Config.ValidateForMarketplace should be preferred for a complete
// operator config because exact revision declarations are instance-scoped.
func (r Revocations) Validate() []error {
	return r.validate("")
}

// ValidateForMarketplace validates revocation entries against the configured
// Marketplace instance. An exact revision declaration for another instance is
// never meaningful to this operator and is rejected rather than stored.
func (r Revocations) ValidateForMarketplace(instanceID string) []error {
	return r.validate(strings.TrimSpace(instanceID))
}

func (r Revocations) validate(instanceID string) []error {
	var errs []error
	for i, id := range r.SourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			errs = append(errs, fmt.Errorf("revocations.source_ids[%d] must not be empty", i))
		} else if strings.ContainsAny(id, "/@") {
			errs = append(errs, fmt.Errorf("revocations.source_ids[%d] %q must be a source ID, not a coordinate", i, id))
		}
	}
	for i, coord := range r.Coordinates {
		parsed, err := parseCoordinate(coord)
		if err != nil {
			errs = append(errs, fmt.Errorf("revocations.coordinates[%d] %q: %w", i, coord, err))
			continue
		}
		if instanceID != "" && parsed.Marketplace.String() != instanceID {
			errs = append(errs, fmt.Errorf("revocations.coordinates[%d] %q: marketplace %q does not match instance.id %q", i, coord, parsed.Marketplace, instanceID))
		}
	}
	for i, rev := range r.Revisions {
		target, err := store.ParseRevisionRevocationTarget(rev)
		if err != nil {
			errs = append(errs, fmt.Errorf("revocations.revisions[%d] %q: %w", i, rev, err))
			continue
		}
		if instanceID != "" && string(target.Coordinate.Marketplace) != instanceID {
			errs = append(errs, fmt.Errorf("revocations.revisions[%d] %q: marketplace %q does not match instance.id %q", i, rev, target.Coordinate.Marketplace, instanceID))
		}
	}
	return errs
}

var slugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func parseCoordinate(s string) (store.ResourceCoordinate, error) {
	value := strings.TrimSpace(s)
	coordinate, err := store.ParseResourceCoordinate(value)
	if err != nil {
		return store.ResourceCoordinate{}, fmt.Errorf("coordinate must have 4 components: marketplace/source/kind/name")
	}
	for label, part := range map[string]string{
		"marketplace": coordinate.Marketplace.String(), "source": coordinate.Source.String(),
		"kind": coordinate.Kind, "name": coordinate.Name,
	} {
		if part == "" || strings.ContainsAny(part, "/@") {
			return store.ResourceCoordinate{}, fmt.Errorf("coordinate %s must be a non-empty route token", label)
		}
	}
	if !store.IsMarketplaceResourceKind(coordinate.Kind) {
		return store.ResourceCoordinate{}, fmt.Errorf("unsupported resource kind %q", coordinate.Kind)
	}
	return coordinate, nil
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
