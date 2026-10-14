package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
	telemetryconfig "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
)

type Intent struct {
	Layers      [5]LayerIntent
	Errors      []error
	Warnings    []error
	Diagnostics ManagedInstallationDiagnostics
	aborted     bool
}

type LayerIntent struct {
	Source   Source
	Path     string
	Reads    []ConfigRead
	legacy   *Config
	v4       *V4Config
	errors   []error
	rootRead bool
}

type Settings struct {
	Harnesses           []string
	AllowedRegistries   *[]string
	AllowedMarketplaces *[]string
	Backup              *BackupConfig
	Logging             *LoggingConfig
	UpdateCheck         *UpdateCheckConfig
	Telemetry           *telemetryconfig.TelemetryConfig
	Materialization     *MaterializationConfig
}

type SourceKind = V4SourceKind

type SourceDecl struct {
	Kind          SourceKind
	URL           string
	Ref           string
	Path          string
	SourceHarness string
}

type MarketplaceSource struct {
	Name     string
	URL      string
	Requests []V4ResourceRequest
}

type Evidence struct {
	Locks  [5]*lockfile.Document
	Reads  []ConfigRead
	errors [5][]error
	reads  [5][]ConfigRead
}

func (e *Evidence) RecordDeployedLock(source Source, doc *lockfile.Document, read ConfigRead) {
	e.Locks[source] = doc
	e.reads[source] = append(e.reads[source], read)
	e.Reads = append(e.Reads, read)
}

var intentLayerOrder = []Source{Organization, UserPersonal, RepoCommitted, RepoPersonal}

func layerForIntentSource(source Source) layerKind {
	switch source {
	case Organization:
		return organizationLayer
	case UserPersonal:
		return userLayer
	case RepoPersonal:
		return localLayer
	default:
		return repoLayer
	}
}

func (l LayerIntent) Present() bool {
	return l.legacy != nil || l.v4 != nil
}

func (l LayerIntent) Err() error {
	return errors.Join(l.errors...)
}

func (l LayerIntent) Settings() Settings {
	if l.v4 != nil {
		settings := Settings{
			Harnesses:       cloneV4StringList(l.v4.Harnesses),
			Backup:          l.v4.Backup,
			Logging:         l.v4.Logging,
			UpdateCheck:     l.v4.UpdateCheck,
			Telemetry:       l.v4.Telemetry,
			Materialization: l.v4.Materialization,
		}
		if l.v4.AllowedRegistries != nil {
			values := cloneV4StringList(*l.v4.AllowedRegistries)
			settings.AllowedRegistries = &values
		}
		if l.v4.AllowedMarketplaces != nil {
			values := cloneV4StringList(*l.v4.AllowedMarketplaces)
			settings.AllowedMarketplaces = &values
		}
		return settings
	}
	if l.legacy == nil {
		return Settings{}
	}
	settings := Settings{
		Harnesses:       cloneV4StringList(l.legacy.Harnesses),
		Backup:          l.legacy.Backup,
		Logging:         l.legacy.Logging,
		Telemetry:       l.legacy.Telemetry,
		Materialization: l.legacy.Materialization,
	}
	if l.legacy.AllowedRegistries != nil {
		values := cloneV4StringList(*l.legacy.AllowedRegistries)
		settings.AllowedRegistries = &values
	}
	if l.legacy.AllowedMarketplaces != nil {
		values := cloneV4StringList(*l.legacy.AllowedMarketplaces)
		settings.AllowedMarketplaces = &values
	}
	return settings
}

func (s Settings) config() *Config {
	return &Config{
		APIVersion:          APIVersionV4,
		Harnesses:           s.Harnesses,
		AllowedRegistries:   s.AllowedRegistries,
		AllowedMarketplaces: s.AllowedMarketplaces,
		Backup:              s.Backup,
		Logging:             s.Logging,
		Telemetry:           s.Telemetry,
		Materialization:     s.Materialization,
	}
}

func (l LayerIntent) Sources() []SourceDecl {
	if l.v4 != nil {
		names := make([]string, 0, len(l.v4.Sources))
		for name := range l.v4.Sources {
			names = append(names, name)
		}
		sort.Strings(names)
		decls := make([]SourceDecl, 0, len(names))
		for _, name := range names {
			src := l.v4.Sources[name]
			decl := SourceDecl{Kind: src.Kind}
			if src.Kind == V4SourceGit {
				decl.URL = src.URL
				decl.Ref = src.Ref
				decl.Path = src.Path
				decl.SourceHarness = src.SourceHarness
			}
			if src.Kind == V4SourceMarketplace {
				decl.URL = src.CanonicalURL
			}
			decls = append(decls, decl)
		}
		return decls
	}
	if l.legacy == nil {
		return nil
	}
	decls := make([]SourceDecl, 0, len(l.legacy.Registries)+len(l.legacy.Marketplaces))
	for _, entry := range l.legacy.Registries {
		if entry.IsLocal() {
			decls = append(decls, SourceDecl{Kind: V4SourceDirectory})
		} else {
			decls = append(decls, SourceDecl{Kind: V4SourceGit, URL: entry.URL, Ref: entry.Ref, Path: entry.Path, SourceHarness: entry.SourceHarness})
		}
	}
	for _, entry := range l.legacy.Marketplaces {
		decls = append(decls, SourceDecl{Kind: V4SourceMarketplace, URL: entry.URL})
	}
	return decls
}

func (l LayerIntent) NeedsLock() bool {
	if l.v4 != nil {
		for name, src := range l.v4.Sources {
			if src.Kind == V4SourceGit || l.v4.AcquiresMarketplace(name) {
				return true
			}
		}
		return false
	}
	if l.legacy == nil {
		return false
	}
	for _, entry := range l.legacy.Registries {
		if !entry.IsLocal() {
			return true
		}
	}
	return false
}

func (l LayerIntent) NeedsMarketplaceLock() bool {
	if l.v4 == nil {
		return false
	}
	for name := range l.v4.Sources {
		if l.v4.AcquiresMarketplace(name) {
			return true
		}
	}
	return false
}

// ValidateDeployedMarketplaceLock reports whether a present acquisition lock
// document is congruent with this layer's v4 Marketplace sources; a nil lock
// or a layer without acquiring Marketplace sources is always congruent.
func (l LayerIntent) ValidateDeployedMarketplaceLock(doc *lockfile.Document) error {
	if !l.NeedsMarketplaceLock() || doc == nil {
		return nil
	}
	_, err := l.bind(doc)
	return err
}

// LegacyMarketplaces returns a legacy (v2/v3) layer's Marketplace entries, or
// nil for v4 and absent layers.
func (l LayerIntent) LegacyMarketplaces() []MarketplaceEntry {
	if l.legacy == nil {
		return nil
	}
	return l.legacy.Marketplaces
}

func (l LayerIntent) MarketplaceEndpoints() []string {
	if l.v4 != nil {
		var urls []string
		for _, source := range l.MarketplaceSources() {
			urls = append(urls, source.URL)
		}
		return urls
	}
	if l.legacy == nil {
		return nil
	}
	urls := make([]string, 0, len(l.legacy.Marketplaces))
	for _, entry := range l.legacy.Marketplaces {
		urls = append(urls, entry.URL)
	}
	return urls
}

func (l LayerIntent) MarketplaceSources() []MarketplaceSource {
	if l.v4 == nil {
		return nil
	}
	names := make([]string, 0, len(l.v4.Sources))
	for name := range l.v4.Sources {
		if l.v4.AcquiresMarketplace(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	sources := make([]MarketplaceSource, 0, len(names))
	for _, name := range names {
		source := MarketplaceSource{Name: name, URL: l.v4.Sources[name].CanonicalURL}
		for _, request := range l.v4.Requests {
			if request.SourceName == name {
				source.Requests = append(source.Requests, request)
			}
		}
		sources = append(sources, source)
	}
	return sources
}

func (l LayerIntent) probeConfig() *Config {
	if l.legacy != nil {
		return l.legacy
	}
	if l.v4 != nil {
		return &Config{APIVersion: APIVersionV4}
	}
	return nil
}

func (l LayerIntent) bind(doc *lockfile.Document) (*Config, error) {
	if l.legacy != nil {
		return l.legacy, nil
	}
	if l.v4 == nil {
		return nil, nil
	}
	return l.v4.toLegacyConfig(layerForIntentSource(l.Source), doc)
}

func (l LayerIntent) deployedLock(overlays map[string][]byte) (*lockfile.Document, error) {
	if !l.NeedsMarketplaceLock() {
		return nil, nil
	}
	doc, _, err := readDeployedLock(l.Path, lockfile.Scope(l.Source.LockScope()), overlays)
	return doc, err
}

func (i Intent) TargetHarnesses() ([]string, Source) {
	if harnesses := i.Layers[RepoCommitted].Settings().Harnesses; len(harnesses) > 0 {
		return append([]string(nil), harnesses...), RepoCommitted
	}
	if harnesses := i.Layers[UserPersonal].Settings().Harnesses; len(harnesses) > 0 {
		return append([]string(nil), harnesses...), UserPersonal
	}
	if harnesses := i.Layers[Organization].Settings().Harnesses; len(harnesses) > 0 {
		return append([]string(nil), harnesses...), i.Layers[Organization].Source
	}
	return nil, SourceUnset
}

func (i Intent) AllowedRegistries() []string {
	for _, source := range intentLayerOrder {
		allowed := i.Layers[source].Settings().AllowedRegistries
		if allowed != nil {
			return *allowed
		}
	}
	return nil
}

type ExplicitSelection struct {
	Kind ir.ResourceKind
	Name string
}

func (l LayerIntent) ExplicitSelections() []ExplicitSelection {
	if l.legacy != nil {
		var out []ExplicitSelection
		for _, entry := range l.legacy.Registries {
			for _, kind := range AllKinds {
				for _, name := range entry.SelectorForKind(kind).Include {
					out = append(out, ExplicitSelection{Kind: kind, Name: name})
				}
			}
		}
		return out
	}
	if l.v4 == nil {
		return nil
	}
	names := make([]string, 0, len(l.v4.Sources))
	for name := range l.v4.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []ExplicitSelection
	for _, name := range names {
		if l.v4.Sources[name].Kind == V4SourceMarketplace {
			continue
		}
		for _, request := range effectiveV4Requests(l.v4.Groups[name]) {
			out = append(out, ExplicitSelection{Kind: v4KindToIR(request.Kind), Name: request.Name})
		}
	}
	return out
}

func (i Intent) AllowedMarketplaces() []string {
	for _, source := range intentLayerOrder {
		allowed := i.Layers[source].Settings().AllowedMarketplaces
		if allowed != nil {
			return *allowed
		}
	}
	return nil
}

func readLayerIntent(path, canonical string, data []byte, kind layerKind, overlays map[string][]byte) (LayerIntent, error) {
	li := LayerIntent{Source: sourceForLayer(kind), Path: path}
	if PeekAPIVersion(data) == APIVersionV4 {
		organizationPath := ""
		if kind == organizationLayer {
			organizationPath = path
		}
		v4, err := loadV4TreeWithResolvedRoot(path, canonical, true, map[string]bool{}, overlays, data, organizationPath, kind)
		if err != nil {
			return li, err
		}
		li.v4 = v4
		li.Reads = append([]ConfigRead(nil), v4.reads...)
		return li, nil
	}
	loader := newConfigIncludeLoader(kind)
	loader.rootPath = canonical
	loader.rootData = data
	loader.overlays = overlays
	loader.readByPath = make(map[string]ConfigRead)
	cfg, err := loadCollectedConfigTree(path, loader)
	li.legacy = cfg
	li.Reads = loader.reads
	return li, err
}

func readRootLayerIntent(path string, data []byte, kind layerKind) (LayerIntent, error) {
	li := LayerIntent{Source: sourceForLayer(kind), Path: path}
	if PeekAPIVersion(data) == APIVersionV4 {
		v4, err := ParseV4Config(data, path, true)
		if err != nil {
			return li, err
		}
		li.v4 = v4
		return li, nil
	}
	cfg, errs := loadV3ConfigWithAPIVersion(data, kind, false, kind != localLayer)
	if len(errs) > 0 {
		return li, fmt.Errorf("validate config %q: %w", path, errors.Join(errs...))
	}
	if logErrs := validateLogging(cfg.Logging); len(logErrs) > 0 {
		return li, fmt.Errorf("validate logging config %q: %w", path, errors.Join(logErrs...))
	}
	li.legacy = cfg
	return li, nil
}

func loadLayerIntent(source Source, path string, kind layerKind, overlays map[string][]byte) LayerIntent {
	li := LayerIntent{Source: source, Path: path}
	canonicalPath, data, readErr := readEffectiveLayer(path, overlays)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return li
		}
		if kind == localLayer {
			li.errors = []error{fmt.Errorf("read local config %q: %w", path, readErr)}
		} else {
			li.errors = []error{readErr}
		}
		return li
	}
	parsed, err := readLayerIntent(path, canonicalPath, data, kind, overlays)
	parsed.rootRead = true
	if err != nil {
		parsed.errors = []error{err}
	}
	return parsed
}

func settingsForLayer(li LayerIntent, kind layerKind) (Settings, error) {
	if err := errors.Join(li.errors...); err != nil {
		return Settings{}, err
	}
	if li.v4 != nil {
		if err := li.v4.validateLayerFields(kind); err != nil {
			return Settings{}, err
		}
	}
	return li.Settings(), nil
}

func loadSettings(path string, kind layerKind) (Settings, error) {
	li := loadLayerIntent(sourceForLayer(kind), path, kind, nil)
	return settingsForLayer(li, kind)
}

func LoadOrganizationSettings() (Settings, error) {
	li, _ := readOrganizationIntent(organizationConfigPath(""))
	return settingsForLayer(li, organizationLayer)
}

func LoadUserSettings() (Settings, error) {
	path, err := globalConfigPath()
	if err != nil {
		return Settings{}, err
	}
	return loadSettings(path, userLayer)
}

func readOrganizationIntent(path string) (LayerIntent, ManagedInstallationDiagnostics) {
	if runtime.GOOS == "windows" {
		li := LayerIntent{Source: SourceUnset, Path: path}
		return li, inspectManagedConfig(path, fs.ErrNotExist, nil, SourceUnset, nil)
	}

	canonicalPath, canonicalErr := canonicalConfigPath(path)
	if canonicalErr != nil {
		if errors.Is(canonicalErr, os.ErrNotExist) {
			li := LayerIntent{Source: SourceUnset, Path: path}
			return li, inspectManagedConfig(path, os.ErrNotExist, nil, SourceUnset, nil)
		}
		li := LayerIntent{Source: Organization, Path: path, errors: []error{canonicalErr}}
		return li, inspectManagedConfig(path, canonicalErr, nil, Organization, li.errors)
	}
	if trustErr := ValidateOrganizationConfigAuthority(path); trustErr != nil {
		li := LayerIntent{Source: Organization, Path: path, errors: []error{trustErr}}
		d := inspectManagedConfig(path, nil, nil, Organization, li.errors)
		d.Validation = "untrusted"
		d.Warnings = append(d.Warnings, trustErr)
		return li, d
	}

	data, readErr := readConfigFile(canonicalPath)
	if readErr != nil {
		li := LayerIntent{Source: Organization, Path: path, errors: []error{fmt.Errorf("read organization config %q: %w", path, readErr)}}
		return li, inspectManagedConfig(path, readErr, nil, Organization, li.errors)
	}
	li, err := readLayerIntent(path, canonicalPath, data, organizationLayer, nil)
	li.rootRead = true
	if err != nil {
		li.errors = []error{err}
	}
	if len(li.Reads) > 0 {
		li.Reads[0].RequestedPath, _ = filepath.Abs(path)
	}
	probe := li.probeConfig()
	if len(li.errors) > 0 {
		probe = nil
	}
	return li, inspectManagedConfig(path, nil, probe, li.Source, li.errors)
}

func deployedLayerEvidence(li LayerIntent, source Source, opts LoadEffectiveOptions) (*lockfile.Document, []ConfigRead, []error) {
	overlays := opts.FileOverlays
	if source == Organization {
		overlays = nil
	}
	if !li.Present() || len(li.errors) > 0 {
		return nil, nil, nil
	}

	var doc *lockfile.Document
	var read *ConfigRead
	if li.NeedsMarketplaceLock() {
		var err error
		doc, read, err = readDeployedLock(li.Path, lockfile.Scope(li.Source.LockScope()), overlays)
		if err != nil {
			return nil, nil, []error{err}
		}
	}
	var reads []ConfigRead
	if read != nil {
		reads = []ConfigRead{*read}
	}
	return doc, reads, nil
}

func ReadIntent(ctx context.Context, opts LoadEffectiveOptions) Intent {
	if err := ctx.Err(); err != nil {
		return Intent{Errors: []error{err}, aborted: true}
	}

	slog.Info("loading effective config")

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		return Intent{Errors: []error{errors.New("repository root is required: pass a validated workspace repository root")}, aborted: true}
	}
	localDir := opts.LocalConfigDir
	if localDir == "" {
		localDir = repoRoot
	}

	intent := Intent{}

	orgPath := organizationConfigPath(opts.OrganizationPath)
	intent.Layers[Organization], intent.Diagnostics = readOrganizationIntent(orgPath)
	intent.Warnings = append(intent.Warnings, intent.Diagnostics.Warnings...)
	LogManagedDiagnostics(intent.Diagnostics)

	userPath := opts.GlobalPath
	if userPath == "" {
		var err error
		userPath, err = globalConfigPath()
		if err != nil {
			intent.Layers[UserPersonal] = LayerIntent{Source: UserPersonal, errors: []error{err}}
			userPath = ""
		}
	}
	if userPath != "" {
		intent.Layers[UserPersonal] = loadLayerIntent(UserPersonal, userPath, userLayer, opts.FileOverlays)
	}

	repoPath := opts.RepoPath
	if repoPath == "" {
		repoPath = ProjectConfigPath(repoRoot)
	}
	if IsHomeDir(repoRoot) {
		intent.Layers[RepoCommitted] = LayerIntent{Source: RepoCommitted, Path: repoPath}
	} else {
		intent.Layers[RepoCommitted] = loadLayerIntent(RepoCommitted, repoPath, repoLayer, opts.FileOverlays)
	}

	localPath := opts.LocalPath
	if localPath == "" {
		localPath = LocalConfigPath(localDir)
	}
	if IsHomeDir(localDir) {
		intent.Layers[RepoPersonal] = LayerIntent{Source: RepoPersonal, Path: localPath}
	} else {
		intent.Layers[RepoPersonal] = loadLayerIntent(RepoPersonal, localPath, localLayer, opts.FileOverlays)
	}

	for _, source := range intentLayerOrder {
		intent.Errors = append(intent.Errors, intent.Layers[source].errors...)
	}
	return intent
}

func bindLayerResult(li LayerIntent, ev Evidence, source Source) LayerResult {
	lr := LayerResult{Source: li.Source, Path: li.Path, Reads: li.Reads, Sources: li.Sources()}
	lr.Errors = append(lr.Errors, li.errors...)
	lr.Errors = append(lr.Errors, ev.errors[source]...)
	lr.Reads = append(lr.Reads, ev.reads[source]...)
	if li.Present() && len(li.errors) == 0 && len(ev.errors[source]) == 0 {
		cfg, err := li.bind(ev.Locks[source])
		if err != nil {
			lr.Errors = append(lr.Errors, err)
		} else if source == RepoPersonal {
			lr.Local = configAsLocal(cfg)
		} else {
			lr.Config = cfg
		}
	}
	return lr
}

func bindLayers(intent Intent, ev Evidence) EffectiveConfig {
	ec := EffectiveConfig{Diagnostics: intent.Diagnostics}
	ec.Errors = intent.Errors[:len(intent.Errors):len(intent.Errors)]
	ec.Warnings = intent.Warnings[:len(intent.Warnings):len(intent.Warnings)]

	ec.Organization = bindLayerResult(intent.Layers[Organization], ev, Organization)
	ec.User = bindLayerResult(intent.Layers[UserPersonal], ev, UserPersonal)
	ec.Repo = bindLayerResult(intent.Layers[RepoCommitted], ev, RepoCommitted)
	ec.LocalLayer = bindLayerResult(intent.Layers[RepoPersonal], ev, RepoPersonal)
	ec.Local = ec.LocalLayer.Local
	return ec
}

func finishBind(intent Intent, ev Evidence, ec EffectiveConfig) EffectiveConfig {
	if intent.aborted {
		return ec
	}
	ec.Errors = nil
	for _, layer := range ec.LayerResults() {
		ec.Errors = append(ec.Errors, layer.Errors...)
	}

	if len(intent.Layers[Organization].errors) == 0 && len(ec.Organization.Errors) > 0 {
		var trustErr *OrganizationTrustError
		if errors.As(errors.Join(ev.errors[Organization]...), &trustErr) {
			ec.Diagnostics.State = ManagedInstallationUnmanaged
			ec.Diagnostics.Validation = "blocked"
			ec.Diagnostics.Warnings = append(ec.Diagnostics.Warnings, trustErr)
			ec.Warnings = append(ec.Warnings, trustErr)
		} else {
			ec.Diagnostics.State = ManagedInstallationUnmanaged
			ec.Diagnostics.Validation = "invalid"
		}
	}
	ec.Managed = ec.Diagnostics.Managed()

	for _, err := range ec.Errors {
		slog.Error("effective config layer error", "error", err)
	}

	// Resolve the target harness set. Project-level harnesses take precedence
	// for project operations; user-personal harnesses apply when the project
	// does not specify its own, and organization harnesses remain the fallback.
	if ec.Repo.Config != nil && len(ec.Repo.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.Repo.Config.Harnesses...)
		ec.TargetHarnessSource = RepoCommitted
	} else if ec.User.Config != nil && len(ec.User.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.User.Config.Harnesses...)
		ec.TargetHarnessSource = UserPersonal
	} else if ec.Organization.Config != nil && len(ec.Organization.Config.Harnesses) > 0 {
		ec.TargetHarnesses = append([]string(nil), ec.Organization.Config.Harnesses...)
		ec.TargetHarnessSource = ec.Organization.Source
	}

	slog.Info("resolved target harness set",
		"harnesses", ec.TargetHarnesses,
		"source", ec.TargetHarnessSource,
	)

	layers := make([]Layer, 0, 4)
	if ec.Organization.Config != nil {
		layers = append(layers, Layer{Source: Organization, Config: ec.Organization.Config})
	}
	if ec.User.Config != nil {
		layers = append(layers, Layer{Source: UserPersonal, Config: ec.User.Config})
	}
	if ec.Repo.Config != nil {
		layers = append(layers, Layer{Source: RepoCommitted, Config: ec.Repo.Config})
	}
	if l := LocalConfigAsLayer(ec.Local); l != nil {
		layers = append(layers, *l)
	}

	merged := MergeLayers(layers)
	ec.Merged = merged

	// Resolve the allowlist from the merged file layers. A nil pointer means
	// no layer declared an allowlist, which permits all remote origins. A
	// non-nil empty slice means an explicit empty allowlist, which denies all
	// remote origins.
	var allowed []string
	if merged.AllowedRegistries != nil {
		allowed = *merged.AllowedRegistries
	}
	ec.Allowed = allowed

	if err := ValidateRegistryURLs(merged.Registries, allowed); err != nil {
		slog.Error("registry allowlist rejected one or more URLs", "error", err)
		ec.Errors = append(ec.Errors, err)
	}

	var allowedMarketplaces []string
	if merged.AllowedMarketplaces != nil {
		allowedMarketplaces = *merged.AllowedMarketplaces
	}
	ec.AllowedMarketplaces = allowedMarketplaces

	if err := ValidateMarketplaceURLs(merged.Marketplaces, allowedMarketplaces); err != nil {
		slog.Error("marketplace allowlist rejected one or more URLs", "error", err)
		ec.Errors = append(ec.Errors, err)
	}
	if errs := ValidateMarketplaceEntries(merged.Marketplaces); len(errs) > 0 {
		err := errors.Join(errs...)
		slog.Error("effective Marketplace entries rejected", "error", err)
		ec.Errors = append(ec.Errors, err)
	}

	if len(ec.Errors) > 0 {
		slog.Error("effective config loaded with errors",
			"error_count", len(ec.Errors),
			"warning_count", len(ec.Warnings),
		)
	} else {
		slog.Info("effective config loaded successfully",
			"registry_count", len(merged.Registries),
			"warning_count", len(ec.Warnings),
		)
	}

	return ec
}

func Bind(intent Intent, ev Evidence) EffectiveConfig {
	return finishBind(intent, ev, bindLayers(intent, ev))
}
