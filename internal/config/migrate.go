package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/lockfile"
	telemetryconfig "github.com/Adyen/agent-marketplace/internal/telemetry/config"
	"github.com/Adyen/agent-marketplace/pkg/harness-compilation/ir"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
	"gopkg.in/yaml.v3"
)

var (
	// ErrLegacyIdentity is returned when a config contains legacy identity or
	// team_profiles fields. Migration refuses to silently discard these.
	ErrLegacyIdentity = errors.New("config contains legacy identity or team_profiles fields")

	// ErrHarnessesRequired is returned when an existing user config needs a
	// target harness set and the caller must supply one or prompt.
	ErrHarnessesRequired = errors.New("target harness set is required")
)

// MigratePaths holds the filesystem paths for the layers that are migrated in
// a single transactional pass.
type MigratePaths struct {
	User  string
	Repo  string
	Local string
}

// MigrateResult reports which config files were written during migration.
type MigrateResult struct {
	UserMigrated  bool
	RepoMigrated  bool
	LocalMigrated bool
	UserPath      string
	RepoPath      string
	LocalPath     string
}

// migrationFile holds the parsed YAML state for one config file during the
// migration pass.
type migrationFile struct {
	path       string
	exists     bool
	mode       os.FileMode
	doc        *yaml.Node
	root       *yaml.Node
	kind       layerKind
	apiVersion string
	harnesses  []string
	modified   bool
}

// MigrateConfigFiles performs structural remediation on user personal,
// repository committed, and repository personal config files in one
// transactional pass. It does not rewrite a valid v2 or v3 config to another
// schema version; when changes are required, output is canonicalized to v2.
// All inputs are validated before any file is written.
//
// When initializing a missing user personal config, the target harness set is
// chosen from existing user harnesses first, then the explicit harnesses
// argument. Repository harnesses remain project-scoped and are never moved to
// the user config.
func MigrateConfigFiles(paths MigratePaths, explicitHarnesses []string) (*MigrateResult, error) {
	slog.Info("migrating config files",
		"user", paths.User,
		"repo", paths.Repo,
		"local", paths.Local,
		"explicit_harnesses", explicitHarnesses,
	)

	// Deduplicate paths so the same file is not processed twice when a caller
	// passes identical user/repo/local paths.
	workPaths := paths
	if workPaths.User != "" && workPaths.User == workPaths.Repo {
		workPaths.Repo = ""
	}
	if workPaths.User != "" && workPaths.User == workPaths.Local {
		workPaths.Local = ""
	}
	if workPaths.Repo != "" && workPaths.Repo == workPaths.Local {
		workPaths.Local = ""
	}

	files := make(map[string]*migrationFile, 3)

	if workPaths.User != "" {
		f, err := loadMigrationFile(workPaths.User, userLayer)
		if err != nil {
			return nil, err
		}
		files["user"] = f
	}
	if workPaths.Repo != "" {
		f, err := loadMigrationFile(workPaths.Repo, repoLayer)
		if err != nil {
			return nil, err
		}
		files["repo"] = f
	}
	if workPaths.Local != "" {
		f, err := loadMigrationFile(workPaths.Local, localLayer)
		if err != nil {
			return nil, err
		}
		files["local"] = f
	}

	// Validate every existing document before any mutation.
	layerOrder := []string{"user", "repo", "local"}
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists {
			continue
		}
		if f.apiVersion == "" {
			return nil, fmt.Errorf("%s config at %q has no apiVersion (v1 input is not supported)", name, f.path)
		}
		if f.apiVersion != APIVersionV2 && f.apiVersion != APIVersionV3 {
			return nil, fmt.Errorf("%s config at %q has unsupported apiVersion %q", name, f.path, f.apiVersion)
		}
		for _, key := range []string{"identity", "team_profiles"} {
			if findMappingValue(f.root, key) != nil {
				return nil, fmt.Errorf("%w: %s config at %q contains %q", ErrLegacyIdentity, name, f.path, key)
			}
		}
	}

	// Determine the target harness set.
	targetHarnesses, err := resolveMigrationTargetHarnesses(files, explicitHarnesses)
	if err != nil {
		slog.Error("failed to resolve migration target harnesses", "error", err)
		return nil, err
	}
	slog.Info("resolved migration target harnesses", "harnesses", targetHarnesses)

	// Build the remediated document for each existing file.
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists {
			continue
		}
		switch name {
		case "user":
			if err := migrateFileNode(f, targetHarnesses); err != nil {
				return nil, err
			}
		case "repo":
			if err := migrateFileNode(f, nil); err != nil {
				return nil, err
			}
			for _, key := range []string{"backup", "logging"} {
				if removeMappingKeyByName(f.root, key) {
					f.modified = true
				}
			}
		case "local":
			if err := migrateFileNode(f, nil); err != nil {
				return nil, err
			}
			for _, key := range []string{"harnesses", "backup", "logging", "allowed_registries", "allowed_marketplaces"} {
				if removeMappingKeyByName(f.root, key) {
					f.modified = true
				}
			}
		}
		canonicalizeMigrationAPIVersion(f)
	}

	// If a user personal config is absent, only create one when an explicit
	// target harness set was supplied. Project-only configurations do not need
	// to be rewritten or promoted to the user scope.
	if (files["user"] == nil || !files["user"].exists) && len(targetHarnesses) > 0 {
		if workPaths.User == "" {
			return nil, fmt.Errorf("cannot create user personal config without a path")
		}
		userFile := &migrationFile{
			path:   workPaths.User,
			exists: true,
			mode:   defaultConfigMode,
			kind:   userLayer,
		}
		userFile.doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		userFile.root = userFile.doc.Content[0]
		files["user"] = userFile
		if err := migrateFileNode(userFile, targetHarnesses); err != nil {
			return nil, err
		}
		canonicalizeMigrationAPIVersion(userFile)
	}

	// Validate the migrated documents before writing.
	if err := validateMigratedFiles(files); err != nil {
		return nil, err
	}

	result := &MigrateResult{
		UserPath:  paths.User,
		RepoPath:  paths.Repo,
		LocalPath: paths.Local,
	}

	// Write migrated documents atomically: create all temp files first, then
	// rename them into place. If any write fails, remove the temps and do not
	// perform any partial renames.
	if err := writeMigratedFilesAtomically(files, result); err != nil {
		slog.Error("failed to write migrated configs", "error", err)
		return nil, err
	}

	slog.Info("config migration complete", "result", result)
	return result, nil
}

// loadMigrationFile reads and parses a single config file for the migration
// pass. Missing files are represented with exists=false.
func loadMigrationFile(path string, kind layerKind) (*migrationFile, error) {
	f := &migrationFile{path: path, kind: kind}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, fmt.Errorf("read %s config %q: %w", kind.name(), path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s config %q: %w", kind.name(), path, err)
	}
	f.mode = info.Mode().Perm()

	if len(bytes.TrimSpace(data)) == 0 {
		f.exists = true
		f.doc = &yaml.Node{Kind: yaml.DocumentNode}
		f.root, _ = ensureDocumentMapping(f.doc)
		return f, nil
	}

	f.exists = true
	f.doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, f.doc); err != nil {
		return nil, fmt.Errorf("parse %s config %q: %w", kind.name(), path, err)
	}

	root, err := ensureDocumentMapping(f.doc)
	if err != nil {
		return nil, fmt.Errorf("%s config %q: %w", kind.name(), path, err)
	}
	f.root = root

	if av := findMappingValue(f.root, "apiVersion"); av != nil && av.Kind == yaml.ScalarNode {
		f.apiVersion = strings.TrimSpace(av.Value)
	}
	if h := findMappingValue(f.root, "harnesses"); h != nil && h.Kind == yaml.SequenceNode {
		f.harnesses = sequenceValues(h)
	}

	return f, nil
}

// resolveMigrationTargetHarnesses chooses harnesses for an existing user
// config or an explicitly requested user initialization. A project config's
// harnesses are intentionally not promoted to user scope.
func resolveMigrationTargetHarnesses(files map[string]*migrationFile, explicitHarnesses []string) ([]string, error) {
	if userFile := files["user"]; userFile != nil && userFile.exists && len(userFile.harnesses) > 0 {
		return normalizedUniqueValues(userFile.harnesses), nil
	}
	if len(explicitHarnesses) > 0 {
		return normalizedUniqueValues(explicitHarnesses), nil
	}
	if userFile := files["user"]; userFile != nil && userFile.exists {
		return nil, ErrHarnessesRequired
	}
	if repoFile := files["repo"]; repoFile != nil && repoFile.exists && len(repoFile.harnesses) > 0 {
		return nil, nil
	}
	return nil, ErrHarnessesRequired
}

// migrateFileNode sets harnesses when provided and converts legacy
// collections. It tracks whether any change was made. The caller is
// responsible for canonicalizing apiVersion after structural edits.
func migrateFileNode(f *migrationFile, harnesses []string) error {
	if f == nil || !f.exists {
		return nil
	}

	if len(harnesses) > 0 {
		harnesses = normalizedUniqueValues(harnesses)
		h := findMappingValue(f.root, "harnesses")
		if h == nil {
			insertHarnesses(f.root, harnesses)
			f.modified = true
		} else {
			if h.Kind != yaml.SequenceNode {
				return fmt.Errorf("%s config %q: harnesses must be a sequence", f.kind.name(), f.path)
			}
			old := sequenceValues(h)
			newH := normalizedUniqueValues(harnesses)
			if !stringSlicesEqual(old, newH) {
				setSequenceValues(h, newH)
				f.modified = true
			}
		}
	}

	if migrateCollections(f.root) {
		f.modified = true
	}

	return nil
}

// canonicalizeMigrationAPIVersion ensures the file declares a valid
// apiVersion. Valid v2 and v3 files are left untouched when no structural
// remediation is needed. New or structurally remediated files use canonical
// v2; this keeps migration from forcing a schema-version rewrite on users.
func canonicalizeMigrationAPIVersion(f *migrationFile) {
	if f == nil || !f.exists {
		return
	}

	av := findMappingValue(f.root, "apiVersion")
	if av == nil {
		f.root.Content = append([]*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apiVersion"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV2},
		}, f.root.Content...)
		f.modified = true
		return
	}
	if av.Kind != yaml.ScalarNode {
		return
	}

	v := strings.TrimSpace(av.Value)
	if !f.modified {
		if v == APIVersionV2 || v == APIVersionV3 {
			return
		}
		av.Value = APIVersionV2
		f.modified = true
		return
	}

	if v != APIVersionV2 {
		av.Value = APIVersionV2
		if v != APIVersionV3 {
			f.modified = true
		}
	}
}

// migrateCollections rewrites the legacy [{name: X}] and [X] collections
// formats to the {include: [X]} KindSelector map form. It operates on the YAML
// AST so comments are retained rather than being lost through a struct round
// trip. It returns true when the document was modified.
func migrateCollections(root *yaml.Node) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind != yaml.ScalarNode || root.Content[i].Value != "collections" {
			continue
		}
		collectionsNode := root.Content[i+1]
		if collectionsNode.Kind != yaml.SequenceNode || len(collectionsNode.Content) == 0 {
			return false
		}

		includeSeq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range collectionsNode.Content {
			nameNode, ok := legacyCollectionNameNode(item)
			if !ok {
				// Do not replace a partially understood legacy sequence. Leaving
				// it intact is safer than silently dropping an entry.
				return false
			}
			includeSeq.Content = append(includeSeq.Content, nameNode)
		}

		newMap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		newMap.HeadComment = collectionsNode.HeadComment
		newMap.LineComment = collectionsNode.LineComment
		newMap.FootComment = collectionsNode.FootComment
		newMap.Content = []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "include"},
			includeSeq,
		}
		root.Content[i+1] = newMap
		return true
	}
	return false
}

func legacyCollectionNameNode(item *yaml.Node) (*yaml.Node, bool) {
	if item == nil {
		return nil, false
	}
	if item.Kind == yaml.ScalarNode {
		name := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: item.Value}
		name.HeadComment = item.HeadComment
		name.LineComment = item.LineComment
		name.FootComment = item.FootComment
		return name, true
	}
	if item.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(item.Content); i += 2 {
		key, value := item.Content[i], item.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value != "name" || value.Kind != yaml.ScalarNode {
			continue
		}
		name := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value.Value}
		name.HeadComment = joinCommentText(item.HeadComment, value.HeadComment)
		name.LineComment = value.LineComment
		name.FootComment = joinCommentText(item.FootComment, value.FootComment)
		return name, true
	}
	return nil, false
}

func joinCommentText(existing, addition string) string {
	if existing == "" {
		return addition
	}
	if addition == "" {
		return existing
	}
	return existing + "\n" + addition
}

// validateMigratedFiles serializes each remediated document and validates it
// against the strict layer rules.
func validateMigratedFiles(files map[string]*migrationFile) error {
	layerOrder := []string{"user", "repo", "local"}
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists {
			continue
		}
		data, err := encodeMigrationDocument(f.doc)
		if err != nil {
			return fmt.Errorf("encode migrated %s config: %w", name, err)
		}

		switch f.kind {
		case userLayer:
			if _, errs := loadV3Config(data, userLayer, true); len(errs) > 0 {
				return fmt.Errorf("validate migrated %s config: %w", name, errors.Join(errs...))
			}
		case repoLayer:
			if _, errs := loadV3Config(data, repoLayer, false); len(errs) > 0 {
				return fmt.Errorf("validate migrated %s config: %w", name, errors.Join(errs...))
			}
		case localLayer:
			if _, errs := loadV3LocalConfig(data); len(errs) > 0 {
				return fmt.Errorf("validate migrated %s config: %w", name, errors.Join(errs...))
			}
		}
	}
	return nil
}

// insertHarnesses adds a harnesses key after the apiVersion key in the root
// mapping. If apiVersion is absent, it appends at the end.
func insertHarnesses(root *yaml.Node, harnesses []string) {
	if len(harnesses) == 0 {
		return
	}

	harnessesValue := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, h := range harnesses {
		harnessesValue.Content = append(harnessesValue.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: h},
		)
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Kind == yaml.ScalarNode && root.Content[i].Value == "apiVersion" {
			insertAt := i + 2
			prefix := root.Content[:insertAt]
			suffix := root.Content[insertAt:]
			root.Content = make([]*yaml.Node, 0, len(prefix)+2+len(suffix))
			root.Content = append(root.Content, prefix...)
			root.Content = append(root.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "harnesses"},
				harnessesValue,
			)
			root.Content = append(root.Content, suffix...)
			return
		}
	}

	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "harnesses"},
		harnessesValue,
	)
}

func encodeMigrationDocument(doc *yaml.Node) ([]byte, error) {
	if isEmptyConfigDocument(doc) {
		return nil, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type stagedMigratedFile struct {
	name string
	tmp  string
	dst  string
	mode os.FileMode
}

// writeMigratedFilesAtomically writes all modified migrated documents to temp
// files next to their targets and then renames them into place. If any temp
// write fails, all temps are removed and no renames occur.
func writeMigratedFilesAtomically(files map[string]*migrationFile, result *MigrateResult) error {
	layerOrder := []string{"user", "repo", "local"}
	var staged []stagedMigratedFile

	// First pass: write every temp file.
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists || !f.modified {
			continue
		}

		data, err := encodeMigrationDocument(f.doc)
		if err != nil {
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("encode migrated %s config: %w", name, err)
		}

		dir := filepath.Dir(f.path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("create config directory for %q: %w", f.path, err)
		}

		tmp, err := os.CreateTemp(dir, filepath.Base(f.path)+".tmp-*")
		if err != nil {
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("create temp config for %q: %w", f.path, err)
		}

		if err := tmp.Chmod(f.mode); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("chmod temp config for %q: %w", f.path, err)
		}
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("write temp config for %q: %w", f.path, err)
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmp.Name())
			cleanupStagedMigratedFiles(staged)
			return fmt.Errorf("close temp config for %q: %w", f.path, err)
		}

		staged = append(staged, stagedMigratedFile{name: name, tmp: tmp.Name(), dst: f.path, mode: f.mode})
	}

	// Second pass: rename all temps to their final paths.
	for _, s := range staged {
		if err := os.Rename(s.tmp, s.dst); err != nil {
			return fmt.Errorf("replace config %q: %w", s.dst, err)
		}
		switch s.name {
		case "user":
			result.UserMigrated = true
		case "repo":
			result.RepoMigrated = true
		case "local":
			result.LocalMigrated = true
		}
	}

	return nil
}

func cleanupStagedMigratedFiles(staged []stagedMigratedFile) {
	for _, s := range staged {
		_ = os.Remove(s.tmp)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// V4MigrationScope limits an explicit format migration to one writable layer.
// The default command scope is V4MigrationAll, which validates every writable
// layer before publishing any of them.
type V4MigrationScope string

const (
	V4MigrationAll   V4MigrationScope = "all"
	V4MigrationUser  V4MigrationScope = "user"
	V4MigrationRepo  V4MigrationScope = "repo"
	V4MigrationLocal V4MigrationScope = "local"
)

type V4MigrationCandidate struct {
	Scope V4MigrationScope
	Path  string
}

// FindV4MigrationCandidates returns writable legacy roots that can be offered
// for consented v4 migration. Missing files and already-v4 roots are omitted;
// unversioned roots are considered legacy only for the local layer.
func FindV4MigrationCandidates(paths MigratePaths) ([]V4MigrationCandidate, error) {
	selected := []V4MigrationCandidate{
		{Scope: V4MigrationUser, Path: paths.User},
		{Scope: V4MigrationRepo, Path: paths.Repo},
		{Scope: V4MigrationLocal, Path: paths.Local},
	}
	candidates := make([]V4MigrationCandidate, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, candidate := range selected {
		if candidate.Path == "" {
			continue
		}
		path := filepath.Clean(candidate.Path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		data, err := readConfigFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s config %q: %w", candidate.Scope, path, err)
		}
		version := PeekAPIVersion(data)
		if version == APIVersionV2 || version == APIVersionV3 || (version == "" && candidate.Scope == V4MigrationLocal) {
			candidate.Path = path
			candidates = append(candidates, candidate)
		}
	}
	return candidates, nil
}

type V4MigrationResult struct {
	Scope         V4MigrationScope
	DryRun        bool
	UserMigrated  bool
	RepoMigrated  bool
	LocalMigrated bool
	UserPath      string
	RepoPath      string
	LocalPath     string
}

type v4MigrationSource struct {
	Git           string `yaml:"git,omitempty"`
	Directory     string `yaml:"directory,omitempty"`
	Marketplace   string `yaml:"marketplace,omitempty"`
	Ref           string `yaml:"ref,omitempty"`
	Path          string `yaml:"path,omitempty"`
	SourceHarness string `yaml:"source_harness,omitempty"`
}

// v4MigrationResourceOptions carries explicit per-request options on a v4
// include locator mapping.
type v4MigrationResourceOptions struct {
	As       string `yaml:"as,omitempty"`
	Revision string `yaml:"revision,omitempty"`
}

// v4MigrationEncoding pairs the generated v4 config bytes with the desired
// Marketplace lock endpoints derived from verified cache evidence.
type v4MigrationEncoding struct {
	ConfigBytes  []byte
	Marketplaces []lockfile.MarketplaceLock
}

// MarketplaceMigrationEvidence reconstructs a fully populated Marketplace lock
// record for one legacy pin from verified offline cache evidence.
type MarketplaceMigrationEvidence interface {
	VerifiedMarketplaceLockResource(MarketplaceEntry, MarketplaceResourcePin) (lockfile.MarketplaceResourceLock, error)
}

// commitV4Migration publishes config and lock pairs atomically; tests swap it
// to inject transaction failures.
var commitV4Migration = dependencytxn.Commit

type v4MigrationGroup struct {
	Include   []any     `yaml:"include,omitempty"`
	Exclude   []string  `yaml:"exclude,omitempty"`
	Tags      *[]string `yaml:"tags,omitempty"`
	All       *bool     `yaml:"all,omitempty"`
	Required  *bool     `yaml:"required,omitempty"`
	Harnesses *[]string `yaml:"harnesses,omitempty"`
}

type v4MigrationDocument struct {
	APIVersion          string                           `yaml:"apiVersion"`
	Sources             map[string]v4MigrationSource     `yaml:"sources,omitempty"`
	Resources           map[string]v4MigrationGroup      `yaml:"resources,omitempty"`
	Harnesses           []string                         `yaml:"harnesses,omitempty"`
	AllowedRegistries   *[]string                        `yaml:"allowed_registries,omitempty"`
	AllowedMarketplaces *[]string                        `yaml:"allowed_marketplaces,omitempty"`
	Backup              *BackupConfig                    `yaml:"backup,omitempty"`
	Logging             *LoggingConfig                   `yaml:"logging,omitempty"`
	Telemetry           *telemetryconfig.TelemetryConfig `yaml:"telemetry,omitempty"`
	Materialization     *MaterializationConfig           `yaml:"materialization,omitempty"`
}

type v4MigrationInput struct {
	name          string
	path          string
	logicalPath   string
	kind          layerKind
	mode          os.FileMode
	cfg           *Config
	originalData  []byte
	canonicalPath string
	generation    dependencytxn.Generation
	expectedMode  fs.FileMode
}

// MigrateConfigFilesToV4 performs the explicitly requested semantic and
// selection-preserving legacy conversion for representable inputs. It
// regenerates canonical v4 YAML and does not promise authored comments or
// formatting. It never reads or writes organization configuration. Inputs
// carrying Marketplace entries are refused without verified evidence.
func MigrateConfigFilesToV4(paths MigratePaths, scope V4MigrationScope, dryRun bool) (*V4MigrationResult, error) {
	return MigrateConfigFilesToV4WithEvidence(context.Background(), paths, scope, dryRun, nil)
}

// MigrateConfigFilesToV4WithEvidence is MigrateConfigFilesToV4 with an
// explicit context and offline Marketplace evidence provider. For layers that
// carry Marketplace entries it derives the exact lock records through
// evidence, merges them into the owning layer lock, and publishes every
// config+lock pair in one dependency transaction. Every selected file is
// parsed, converted, and pair-validated before anything is published;
// unsupported constructs therefore leave all inputs intact.
func MigrateConfigFilesToV4WithEvidence(ctx context.Context, paths MigratePaths, scope V4MigrationScope, dryRun bool, evidence MarketplaceMigrationEvidence) (*V4MigrationResult, error) {
	if scope != V4MigrationAll && scope != V4MigrationUser && scope != V4MigrationRepo && scope != V4MigrationLocal {
		return nil, v4err("v4.migration-scope", "scope", 0, "unsupported migration scope %q", scope)
	}
	slog.Info("v4 migration start", "scope", scope, "dry_run", dryRun)
	selected := []struct {
		name string
		path string
		kind layerKind
	}{
		{"user", paths.User, userLayer},
		{"repo", paths.Repo, repoLayer},
		{"local", paths.Local, localLayer},
	}
	type inputPath struct{ name, path string }
	inputs := make([]v4MigrationInput, 0, len(selected))
	canonicalInputs := make(map[string]inputPath, len(selected))
	for _, item := range selected {
		if scope != V4MigrationAll && string(scope) != item.name {
			continue
		}
		if item.path == "" {
			continue
		}
		logicalPath, err := filepath.Abs(item.path)
		if err != nil {
			return nil, fmt.Errorf("resolve %s config path %q: %w", item.name, item.path, err)
		}
		logicalPath = filepath.Clean(logicalPath)
		canonical, err := canonicalConfigPath(logicalPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("resolve %s config %q: %w", item.name, item.path, err)
		}
		data, generation, mode, err := dependencytxn.ReadSnapshot(canonical)
		if err != nil {
			return nil, fmt.Errorf("read %s config %q: %w", item.name, canonical, err)
		}
		if !generation.Exists {
			continue
		}
		if PeekAPIVersion(data) == APIVersionV4 {
			v4, err := LoadV4ConfigFromPath(item.path)
			if err != nil {
				return nil, err
			}
			if _, _, err := loadV4RuntimeConfig(v4, item.path, item.kind, nil); err != nil {
				return nil, err
			}
			// An already-v4 file is valid input and needs no rewrite.
			continue
		}
		if previous, exists := canonicalInputs[canonical]; exists {
			return nil, v4err("v4.migration-input", item.path, 0, "%s config %q and %s config %q resolve to the same legacy file", previous.name, previous.path, item.name, item.path)
		}
		canonicalInputs[canonical] = inputPath{name: item.name, path: item.path}
		cfg, errs := loadV3ConfigWithAPIVersion(data, item.kind, false, item.kind != localLayer)
		if len(errs) > 0 {
			return nil, v4err("v4.migration-input", item.path, 0, "%v", errors.Join(errs...))
		}
		if len(cfg.Include) > 0 {
			return nil, v4err("v4.migration-unsupported", item.path, 0, "include trees cannot be converted without editing included files")
		}
		if len(cfg.Marketplaces) > 0 && evidence == nil {
			return nil, v4err("v4.migration-marketplace-evidence", item.path, 0, "Marketplace entries require verified cache evidence for exact lock migration")
		}
		inputs = append(inputs, v4MigrationInput{name: item.name, path: item.path, logicalPath: logicalPath, kind: item.kind, mode: mode, cfg: cfg, originalData: data, canonicalPath: canonical, generation: generation, expectedMode: mode})
	}

	result := &V4MigrationResult{Scope: scope, DryRun: dryRun, UserPath: paths.User, RepoPath: paths.Repo, LocalPath: paths.Local}
	backupTimestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	plans := make([]v4MigrationPlan, 0, len(inputs))
	for _, input := range inputs {
		encoding, err := encodeV4MigrationWithEvidence(input.cfg, input.path, input.kind, evidence)
		if err != nil {
			return nil, err
		}
		plan := v4MigrationPlan{
			input:      input,
			data:       encoding.ConfigBytes,
			backupPath: input.logicalPath + ".pre-v4." + backupTimestamp + ".bak",
		}
		if len(encoding.Marketplaces) > 0 {
			lockPath, lockDoc, lockData, lockGeneration, lockMode, err := mergeV4MigrationLock(input, encoding.Marketplaces)
			if err != nil {
				return nil, err
			}
			plan.lockPath = lockPath
			plan.lockDoc = &lockDoc
			plan.lockData = lockData
			plan.lockGeneration = lockGeneration
			plan.lockExpectedMode = lockMode
		}
		// Parse the generated bytes again so publication is gated by the same
		// strict v4 validator normal readers use.
		parsed, err := ParseV4Config(plan.data, input.path, true)
		if err != nil {
			return nil, fmt.Errorf("validate v4 migration for %q: %w", input.path, err)
		}
		if plan.lockDoc != nil {
			if _, err := parsed.ToLegacyConfigWithLock(input.kind, plan.lockDoc); err != nil {
				return nil, fmt.Errorf("validate v4 migration lock pair for %q: %w", input.path, err)
			}
		} else if _, err := parsed.ToLegacyConfig(input.kind); err != nil {
			return nil, fmt.Errorf("validate v4 runtime configuration for %q: %w", input.path, err)
		}
		plans = append(plans, plan)
	}
	if dryRun {
		for _, plan := range plans {
			markV4MigrationResult(result, plan.input.name)
		}
		slog.Info("v4 migration preview complete", "files", len(inputs))
		return result, nil
	}
	if err := publishV4MigrationTransaction(ctx, plans); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		markV4MigrationResult(result, plan.input.name)
	}
	slog.Info("v4 migration complete", "user", result.UserMigrated, "repo", result.RepoMigrated, "local", result.LocalMigrated)
	return result, nil
}

func markV4MigrationResult(result *V4MigrationResult, name string) {
	switch name {
	case "user":
		result.UserMigrated = true
	case "repo":
		result.RepoMigrated = true
	case "local":
		result.LocalMigrated = true
	}
}

// v4MigrationPlan is one validated config migration plus, when the layer
// carries Marketplace entries, its merged lock document.
type v4MigrationPlan struct {
	input            v4MigrationInput
	data             []byte
	backupPath       string
	lockPath         string
	lockDoc          *lockfile.Document
	lockData         []byte
	lockGeneration   dependencytxn.Generation
	lockExpectedMode fs.FileMode
}

// mergeV4MigrationLock merges desired Marketplace endpoint records into the
// layer's existing lock: absent locks start as v2, v1 Git records are
// preserved through the version upgrade, and existing v2 Marketplace records
// must be identical. Anything malformed, wrong-scope, or conflicting is
// refused before any write.
func mergeV4MigrationLock(input v4MigrationInput, desired []lockfile.MarketplaceLock) (string, lockfile.Document, []byte, dependencytxn.Generation, fs.FileMode, error) {
	fail := func(err error) (string, lockfile.Document, []byte, dependencytxn.Generation, fs.FileMode, error) {
		return "", lockfile.Document{}, nil, dependencytxn.Generation{}, 0, err
	}
	lockPath, err := lockfile.PathForConfig(input.canonicalPath)
	if err != nil {
		return fail(v4err("v4.migration-marketplace-lock", input.path, 0, "%v", err))
	}
	layerScope := lockfile.Scope(sourceForLayer(input.kind).LockScope())
	doc := lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: layerScope}
	raw, generation, mode, readErr := dependencytxn.ReadSnapshot(lockPath)
	if readErr != nil {
		return fail(fmt.Errorf("read existing lock %q: %w", lockPath, readErr))
	}
	if generation.Exists {
		existing, err := lockfile.Decode(raw)
		if err != nil {
			return fail(v4err("v4.migration-marketplace-lock", input.path, 0, "existing lock %q: %v", lockPath, err))
		}
		if existing.Scope != layerScope {
			return fail(v4err("v4.migration-marketplace-lock", input.path, 0, "existing lock %q has scope %q, want %q", lockPath, existing.Scope, layerScope))
		}
		switch existing.APIVersion {
		case lockfile.APIVersionV1:
			doc.Registries = existing.Registries
		case lockfile.APIVersionV2, lockfile.APIVersionV3:
			if !reflect.DeepEqual(existing.Marketplaces, desired) {
				return fail(v4err("v4.migration-marketplace-lock-conflict", input.path, 0, "existing lock %q records different Marketplace identity", lockPath))
			}
			doc.Registries = existing.Registries
		default:
			return fail(v4err("v4.migration-marketplace-lock", input.path, 0, "existing lock %q has unsupported apiVersion %q", lockPath, existing.APIVersion))
		}
	}
	doc.Marketplaces = desired
	normalized, err := lockfile.NormalizeArtifactSelections(doc)
	if err != nil {
		return fail(v4err("v4.migration-marketplace-lock", input.path, 0, "normalize merged lock: %v", err))
	}
	data, err := lockfile.Encode(normalized)
	if err != nil {
		return fail(fmt.Errorf("encode merged lock %q: %w", lockPath, err))
	}
	return lockPath, normalized, data, generation, mode, nil
}

// publishV4MigrationTransaction publishes every planned config and lock file
// in one dependency transaction so a failure leaves all originals intact.
func publishV4MigrationTransaction(ctx context.Context, plans []v4MigrationPlan) error {
	started := time.Now()
	desired := make([]dependencytxn.DesiredFile, 0, len(plans)*3)
	for _, plan := range plans {
		// Generations and modes were captured with the original bytes during
		// planning, so a concurrent writer between then and commit trips the
		// compare-and-swap precondition instead of being overwritten.
		desired = append(desired,
			dependencytxn.DesiredFile{Path: plan.backupPath, Bytes: plan.input.originalData, Mode: 0o600},
			dependencytxn.DesiredFile{
				Path: plan.input.canonicalPath, Expected: plan.input.generation, ExpectedMode: plan.input.expectedMode,
				Bytes: plan.data, Mode: plan.input.mode,
			},
		)
		if plan.lockDoc == nil {
			continue
		}
		lockMode := plan.lockExpectedMode
		if !plan.lockGeneration.Exists {
			lockMode = 0o644
		}
		desired = append(desired, dependencytxn.DesiredFile{
			Path: plan.lockPath, Expected: plan.lockGeneration, ExpectedMode: plan.lockExpectedMode,
			Bytes: plan.lockData, Mode: lockMode,
		})
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return fmt.Errorf("resolve dependency state dir: %w", err)
	}
	slog.Info("v4 migration transaction started", "files", len(desired))
	result, err := commitV4Migration(ctx, stateDir, desired)
	if err != nil {
		slog.Warn("v4 migration transaction failed", "files", len(desired), "duration_ms", time.Since(started).Milliseconds())
		return fmt.Errorf("commit v4 migration transaction: %w", err)
	}
	slog.Info("v4 migration transaction committed", "files", len(desired), "changed", len(result.Changed), "duration_ms", time.Since(started).Milliseconds())
	return nil
}

func encodeV4MigrationWithEvidence(cfg *Config, configPath string, kind layerKind, evidence MarketplaceMigrationEvidence) (v4MigrationEncoding, error) {
	encoding := v4MigrationEncoding{}
	if kind == localLayer {
		for _, resourceKind := range AllKinds {
			if !cfg.SelectorForKind(resourceKind).IsZero() {
				return encoding, v4err("v4.migration-unsupported", configPath, 0, "source-free personal-local %s selector cannot be represented in v4", v4ResourceKindName(resourceKind))
			}
		}
	}
	doc := v4MigrationDocument{APIVersion: APIVersionV4, Sources: map[string]v4MigrationSource{}, Resources: map[string]v4MigrationGroup{}, Harnesses: append([]string(nil), cfg.Harnesses...), AllowedRegistries: cfg.AllowedRegistries, AllowedMarketplaces: cfg.AllowedMarketplaces, Backup: cfg.Backup, Logging: cfg.Logging, Telemetry: cfg.Telemetry, Materialization: cfg.Materialization}
	for _, original := range cfg.Registries {
		entry := resolveRegistryEntry(cfg, original)
		name := v4MigrationRegistrySourceName(doc.Sources, entry.URL)
		source := v4MigrationSource{Ref: entry.Ref, Path: NormalizeRegistryPath(entry.Path), SourceHarness: entry.SourceHarness}
		if entry.IsLocal() {
			if kind == userLayer && entry.IsRepoRelativeLocal() {
				return encoding, v4err("v4.migration-unsupported", configPath, 0, "repository-relative local source %q cannot be migrated into user scope", entry.URL)
			}
			base := legacyLocalBase(configPath, kind)
			localPath, err := entry.ResolveLocalPath(base)
			if err != nil {
				return encoding, v4err("v4.migration-unsupported", configPath, 0, "local source %q cannot be represented: %v", entry.URL, err)
			}
			source.Directory = entry.BundlePath(localPath)
			if entry.IsRepoRelativeLocal() {
				directoryBase := v4DirectoryBase(configPath, true, kind)
				if v4PathWithin(directoryBase, source.Directory) {
					relative, err := filepath.Rel(directoryBase, source.Directory)
					if err != nil {
						return encoding, fmt.Errorf("relativize migrated directory source: %w", err)
					}
					source.Directory = filepath.ToSlash(relative)
					if source.Directory != "." {
						source.Directory = "./" + source.Directory
					}
				}
			}
			source.Ref, source.Path = "", ""
		} else {
			source.Git = entry.URL
		}
		doc.Sources[name] = source
		group := v4MigrationGroup{}
		if original.Harnesses != nil {
			values := cloneV4StringList(original.Harnesses)
			group.Harnesses = &values
		}
		hasExplicit := false
		exclusions := make([]string, 0)
		exclusionSeen := map[string]bool{}
		addExclusion := func(value string) {
			if exclusionSeen[value] {
				return
			}
			exclusionSeen[value] = true
			exclusions = append(exclusions, value)
		}
		for _, resourceKind := range AllKinds {
			selector := entry.SelectorForKind(resourceKind)
			for _, resourceName := range selector.Include {
				hasExplicit = true
				if strings.TrimSpace(resourceName) == "" || strings.Contains(resourceName, "/") {
					return encoding, v4err("v4.migration-unsupported", configPath, 0, "resource name %q is not a v4 locator segment", resourceName)
				}
				group.Include = append(group.Include, fmt.Sprintf("%s/%s", v4ResourceKindName(resourceKind), resourceName))
			}
			for _, resourceName := range selector.Exclude {
				if strings.TrimSpace(resourceName) == "" || strings.Contains(resourceName, "/") {
					return encoding, v4err("v4.migration-unsupported", configPath, 0, "resource exclusion %q is not a v4 locator segment", resourceName)
				}
				addExclusion(fmt.Sprintf("%s/%s", v4ResourceKindName(resourceKind), resourceName))
			}
			if selector.ExcludeAll || entry.ExcludeAll {
				addExclusion(v4ResourceKindName(resourceKind) + "/*")
			}
		}
		group.Exclude = append(group.Exclude, exclusions...)
		if entry.Tags != nil {
			values := cloneV4StringList(entry.Tags)
			group.Tags = &values
		}
		if entry.Required {
			value := true
			group.Required = &value
		}
		if (entry.Required || entry.Default) && !hasExplicit && entry.Tags == nil {
			value := true
			group.All = &value
		}
		if len(group.Include) > 0 || len(group.Exclude) > 0 || group.Tags != nil || group.Required != nil || group.All != nil || group.Harnesses != nil {
			doc.Resources[name] = group
		}
	}
	layerScope := lockfile.Scope(sourceForLayer(kind).LockScope())
	for index, entry := range cfg.Marketplaces {
		canonical, err := urlutil.NormalizeEndpointURL(entry.URL)
		if err != nil {
			return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d] url: %v", index, err)
		}
		name := v4MigrationMarketplaceSourceName(doc.Sources, entry.Alias)
		doc.Sources[name] = v4MigrationSource{Marketplace: canonical}
		lock := lockfile.MarketplaceLock{URL: canonical, InstanceID: entry.InstanceID}
		bindings := map[string]string{}
		group := v4MigrationGroup{}
		for pinIndex, pin := range entry.Resources {
			record, err := evidence.VerifiedMarketplaceLockResource(entry, pin)
			if err != nil {
				return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d].resources[%d] (%s/%s/%s): %v", index, pinIndex, pin.RegistryID, pin.Kind, pin.Name, err)
			}
			if err := verifyMigrationEvidenceRecord(pin, record); err != nil {
				return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d].resources[%d] (%s/%s/%s): %v", index, pinIndex, pin.RegistryID, pin.Kind, pin.Name, err)
			}
			if bound, ok := bindings[record.RegistryName]; ok && bound != record.RegistryID {
				return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d]: registry name %q binds both %q and %q", index, record.RegistryName, bound, record.RegistryID)
			}
			bindings[record.RegistryName] = record.RegistryID
			lock.Resources = append(lock.Resources, record)
			direct := pin.Direct || len(pin.SelectedBy.List()) == 0
			if !direct {
				if pin.InstalledName != "" {
					return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d].resources[%d]: installed_name %q on a collection-derived pin cannot be represented without creating direct intent", index, pinIndex, pin.InstalledName)
				}
				continue
			}
			locator := record.RegistryName + "/" + v4MigrationMarketplaceKind(pin.Kind) + "/" + pin.Name
			options := v4MigrationResourceOptions{As: pin.InstalledName}
			if pin.Revision != pin.Digest {
				if !v4RevisionRE.MatchString(pin.Revision) {
					return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d].resources[%d]: revision %q is not a canonical sha256 digest and cannot be preserved", index, pinIndex, pin.Revision)
				}
				options.Revision = pin.Revision
			}
			if options.As == "" && options.Revision == "" {
				group.Include = append(group.Include, locator)
			} else {
				group.Include = append(group.Include, map[string]v4MigrationResourceOptions{locator: options})
			}
		}
		bindingNames := make([]string, 0, len(bindings))
		for binding := range bindings {
			bindingNames = append(bindingNames, binding)
		}
		sort.Strings(bindingNames)
		for _, binding := range bindingNames {
			lock.RegistryBindings = append(lock.RegistryBindings, lockfile.MarketplaceRegistryBinding{Name: binding, RegistryID: bindings[binding]})
		}
		normalized, err := lockfile.NormalizeArtifactSelections(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: layerScope, Marketplaces: []lockfile.MarketplaceLock{lock}})
		if err != nil {
			return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "marketplaces[%d]: normalize desired lock endpoint: %v", index, err)
		}
		if len(group.Include) > 0 {
			doc.Resources[name] = group
		}
		encoding.Marketplaces = append(encoding.Marketplaces, normalized.Marketplaces[0])
	}
	if len(encoding.Marketplaces) > 0 {
		// Normalize the complete endpoint list so the merge comparison sees the
		// same canonical order a decoded existing lock uses (sorted by URL).
		normalized, err := lockfile.NormalizeArtifactSelections(lockfile.Document{APIVersion: lockfile.APIVersionV2, Scope: layerScope, Marketplaces: encoding.Marketplaces})
		if err != nil {
			return encoding, v4err("v4.migration-marketplace-evidence", configPath, 0, "normalize desired marketplace locks: %v", err)
		}
		encoding.Marketplaces = normalized.Marketplaces
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return encoding, fmt.Errorf("encode v4 config %q: %w", configPath, err)
	}
	encoding.ConfigBytes = data
	return encoding, nil
}

// v4MigrationMarketplaceKind converts a legacy Marketplace kind into the v4
// locator kind segment.
func v4MigrationMarketplaceKind(kind string) string {
	if kind == "mcpserver" {
		return "mcp"
	}
	return kind
}

// v4MigrationMarketplaceSourceName picks a unique v4 source name for a legacy
// Marketplace entry: a valid authored alias wins, otherwise a deterministic
// marketplace-N name is allocated without colliding with registry sources.
func v4MigrationMarketplaceSourceName(sources map[string]v4MigrationSource, alias string) string {
	if alias != "" && v4SourceNameRE.MatchString(alias) {
		if _, taken := sources[alias]; !taken {
			return alias
		}
	}
	return v4MigrationUniqueSourceName(sources, "marketplace")
}

// verifyMigrationEvidenceRecord checks that a verified lock record represents
// the same immutable identity the legacy pin declares; archive, collection
// manifest, and source-path fields are cache-enriched and exempt.
func verifyMigrationEvidenceRecord(pin MarketplaceResourcePin, record lockfile.MarketplaceResourceLock) error {
	registryName := pin.RegistrySlug
	if registryName == "" {
		registryName = pin.RegistryID
	}
	if record.RegistryName != registryName {
		return fmt.Errorf("evidence registry name %q does not match pin %q", record.RegistryName, registryName)
	}
	if record.RegistryID != pin.RegistryID || record.Kind != pin.Kind || record.Name != pin.Name || record.Revision != pin.Revision {
		return fmt.Errorf("evidence identity (%s/%s/%s@%s) does not match pin (%s/%s/%s@%s)", record.RegistryID, record.Kind, record.Name, record.Revision, pin.RegistryID, pin.Kind, pin.Name, pin.Revision)
	}
	if string(record.ContentDigest) != pin.Digest {
		return fmt.Errorf("evidence content digest %q does not match pin digest %q", record.ContentDigest, pin.Digest)
	}
	acquisition := pin.Acquisition
	if acquisition == "" {
		acquisition = MarketplaceAcquisitionMirror
	}
	if record.Acquisition != acquisition {
		return fmt.Errorf("evidence acquisition %q does not match pin %q", record.Acquisition, acquisition)
	}
	if record.RegistryCommit != pin.RegistryCommit || record.RegistryManifestDigest.String() != pin.RegistryManifestDigest {
		return fmt.Errorf("evidence occurrence (%s/%s) does not match pin (%s/%s)", record.RegistryCommit, record.RegistryManifestDigest, pin.RegistryCommit, pin.RegistryManifestDigest)
	}
	selected := pin.SelectedBy.List()
	if len(record.SelectedBy) != len(selected) {
		return fmt.Errorf("evidence selected_by length %d does not match pin %d", len(record.SelectedBy), len(selected))
	}
	for i, edge := range selected {
		got := record.SelectedBy[i]
		if got.RegistryID != edge.RegistryID || got.Name != edge.Name || got.Revision.String() != edge.Revision {
			return fmt.Errorf("evidence selected_by[%d] %v does not match pin %v", i, got, edge)
		}
	}
	return nil
}

func legacyLocalBase(configPath string, kind layerKind) string {
	if kind == userLayer {
		return filepath.Dir(configPath)
	}
	return filepath.Dir(filepath.Dir(configPath))
}

// v4MigrationRegistrySourceName derives a v4 source name from the basename of
// the registry's normalized Git URL or local directory, falling back to
// "registry" when the basename is not a valid v4 source name.
func v4MigrationRegistrySourceName(sources map[string]v4MigrationSource, registryURL string) string {
	name := NormalizeGitURL(registryURL)
	if separator := strings.LastIndexAny(name, "/:"); separator >= 0 {
		name = name[separator+1:]
	}
	if !v4SourceNameRE.MatchString(name) {
		name = "registry"
	}
	return v4MigrationUniqueSourceName(sources, name)
}

// v4MigrationUniqueSourceName allocates base, base-2, base-3, ... until an
// unused source name is found.
func v4MigrationUniqueSourceName(sources map[string]v4MigrationSource, base string) string {
	for index := 1; ; index++ {
		name := base
		if index > 1 {
			name = fmt.Sprintf("%s-%d", base, index)
		}
		if _, taken := sources[name]; !taken {
			return name
		}
	}
}

func v4ResourceKindName(kind ir.ResourceKind) string {
	switch kind {
	case ir.KindSkill:
		return "skill"
	case ir.KindCommand:
		return "command"
	case ir.KindSubagent:
		return "subagent"
	case ir.KindHook:
		return "hook"
	case ir.KindMcpServer:
		return "mcp"
	case ir.KindCollection:
		return "collection"
	default:
		return "unknown"
	}
}
