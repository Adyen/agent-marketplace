package config

import (
	"context"
	"crypto/sha256"
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
	Telemetry           *telemetryconfig.TelemetryConfig
	Materialization     *MaterializationConfig
}

type SourceKind = V4SourceKind

type SourceDecl struct {
	Kind         SourceKind
	CanonicalURL string
}

type Evidence struct {
	Locks             [5]*lockfile.Document
	Reads             []ConfigRead
	errors            [5][]error
	reads             [5][]ConfigRead
	runtimeSetupError [5]bool
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

func (l LayerIntent) Settings() Settings {
	if l.v4 != nil {
		settings := Settings{
			Harnesses:       cloneV4StringList(l.v4.Harnesses),
			Backup:          l.v4.Backup,
			Logging:         l.v4.Logging,
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
			if src.Kind != V4SourceDirectory {
				decl.CanonicalURL = src.CanonicalURL
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
			decls = append(decls, SourceDecl{Kind: V4SourceGit, CanonicalURL: NormalizeGitURL(entry.URL)})
		}
	}
	for _, entry := range l.legacy.Marketplaces {
		decls = append(decls, SourceDecl{Kind: V4SourceMarketplace, CanonicalURL: entry.URL})
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
	doc, _, err := readDeployedLock(l.Path, overlays)
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

func readLayerIntent(path, canonical string, data []byte, kind layerKind, overlays map[string][]byte) (LayerIntent, error) {
	li := LayerIntent{Source: sourceForLayer(kind), Path: path}
	if PeekAPIVersion(data) == APIVersionV4 {
		organizationPath := ""
		if kind == organizationLayer {
			organizationPath = path
		}
		v4, err := loadV4TreeWithOverlays(path, true, map[string]bool{}, overlays, data, organizationPath, kind)
		if err != nil {
			return li, err
		}
		reads := make([]ConfigRead, 0, len(v4.Files))
		for _, file := range v4.Files {
			contents, overlaid, readErr := v4ConfigData(file, file, false, nil, overlays)
			if readErr != nil {
				return li, fmt.Errorf("read v4 config %q: %w", file, readErr)
			}
			reads = append(reads, ConfigRead{Path: file, RequestedPath: file, Digest: fmt.Sprintf("%x", sha256.Sum256(contents)), Overlay: overlaid})
		}
		li.v4 = v4
		li.Reads = reads
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

func readDeployedLock(path string, overlays map[string][]byte) (*lockfile.Document, *ConfigRead, error) {
	canonical, err := canonicalConfigPath(path)
	if err != nil {
		return nil, nil, err
	}
	paths, err := configLockPaths(path, canonical)
	if err != nil {
		return nil, nil, err
	}
	data, overlaid := overlays[paths.CanonicalLock]
	if !overlaid {
		data, overlaid = overlays[paths.RequestedLock]
	}
	if !overlaid {
		data, err = lockfile.ReadBytes(paths.CanonicalLock)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, nil
			}
			return nil, nil, fmt.Errorf("read lock %q for config %q: %w", paths.CanonicalLock, path, err)
		}
	}
	doc, err := lockfile.Decode(data)
	if err != nil {
		return nil, nil, fmt.Errorf("decode lock %q for config %q: %w", paths.CanonicalLock, path, err)
	}
	read := &ConfigRead{
		Path:            paths.CanonicalLock,
		RequestedPath:   paths.RequestedLock,
		Digest:          fmt.Sprintf("%x", sha256.Sum256(data)),
		Overlay:         overlaid,
		AcquisitionLock: true,
	}
	return &doc, read, nil
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

func deployedLayerEvidence(li LayerIntent, source Source, opts LoadEffectiveOptions) (*lockfile.Document, []ConfigRead, []error, bool) {
	overlays := opts.FileOverlays
	runtimeLock := false
	if source == Organization {
		overlays = nil
		if li.rootRead && len(opts.OrganizationRuntimeLock) > 0 {
			canonical, err := canonicalConfigPath(li.Path)
			if err != nil {
				return nil, nil, []error{err}, true
			}
			paths, err := configLockPaths(li.Path, canonical)
			if err != nil {
				return nil, nil, []error{err}, true
			}
			lockOverlays, err := organizationRuntimeLockOverlay(paths.CanonicalLock, opts.OrganizationRuntimeLock)
			if err != nil {
				return nil, nil, []error{err}, true
			}
			overlays = lockOverlays
			runtimeLock = true
		}
	}

	if !li.Present() || len(li.errors) > 0 {
		return nil, nil, nil, false
	}

	var doc *lockfile.Document
	var read *ConfigRead
	if li.NeedsMarketplaceLock() {
		var err error
		doc, read, err = readDeployedLock(li.Path, overlays)
		if err != nil {
			return nil, nil, []error{err}, false
		}
	}
	var reads []ConfigRead
	if read != nil {
		read.RuntimeLock = runtimeLock
		reads = []ConfigRead{*read}
	}
	return doc, reads, nil, false
}

func deployedLocks(intent Intent, opts LoadEffectiveOptions) Evidence {
	ev := Evidence{}
	for _, source := range intentLayerOrder {
		doc, reads, errs, setupErr := deployedLayerEvidence(intent.Layers[source], source, opts)
		ev.Locks[source] = doc
		ev.errors[source] = errs
		ev.reads[source] = reads
		ev.runtimeSetupError[source] = setupErr
		ev.Reads = append(ev.Reads, reads...)
	}
	return ev
}

func ReadIntent(ctx context.Context, opts LoadEffectiveOptions) Intent {
	if err := ctx.Err(); err != nil {
		return Intent{Errors: []error{err}, aborted: true}
	}

	slog.Info("loading effective config")

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = FindRepoRoot()
	}
	localDir := opts.LocalConfigDir
	if localDir == "" {
		localDir = FindLocalConfigDir()
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
	lr := LayerResult{Source: li.Source, Path: li.Path, Reads: li.Reads}
	if ev.runtimeSetupError[source] {
		lr.Errors = append(lr.Errors, ev.errors[source]...)
		lr.Reads = append(lr.Reads, ev.reads[source]...)
		return lr
	}
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

	if ev.runtimeSetupError[Organization] {
		ec.Diagnostics.State = ManagedInstallationUnmanaged
		ec.Diagnostics.Validation = "unreadable"
	} else if len(intent.Layers[Organization].errors) == 0 && len(ec.Organization.Errors) > 0 {
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

func bindDeployed(intent Intent, ev Evidence) EffectiveConfig {
	ec := bindLayers(intent, ev)
	if ec.Organization.Config != nil && len(ec.Organization.Errors) == 0 && organizationHasRemoteRequirements(ec.Organization.Config) {
		if trustErr := ValidateOrganizationTrustUnit(ec.Organization.Path, true, true); trustErr != nil {
			ev.errors[Organization] = append(ev.errors[Organization], trustErr)
			ec.Organization.Config = nil
			ec.Organization.Errors = append(ec.Organization.Errors, trustErr)
		}
	}
	return finishBind(intent, ev, ec)
}
