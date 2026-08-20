package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrLegacyIdentity is returned when a config contains legacy identity or
	// team_profiles fields. Migration refuses to silently discard these.
	ErrLegacyIdentity = errors.New("config contains legacy identity or team_profiles fields")

	// ErrHarnessesRequired is returned when an existing user config needs a
	// target harness set and the caller must supply one or prompt.
	ErrHarnessesRequired = errors.New("target harness set is required")

	// ErrUnmigratablePolicy is returned when a v2 field would be forbidden in
	// the destination layer and moving it would change its authority. Migration
	// refuses before writing rather than silently deleting policy.
	ErrUnmigratablePolicy = errors.New("config contains policy that cannot be migrated without changing authority")

	ErrMigrationSourceChanged = errors.New("config changed after migration was planned")
)

// MigratePaths holds the filesystem paths for the layers that are migrated in
// a single validated commit plan.
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
	Files         []MigrationFileResult
	RolledBack    bool
	Partial       bool
}

type MigrationFileStatus string

const (
	MigrationFilePending        MigrationFileStatus = "pending"
	MigrationFileMigrated       MigrationFileStatus = "migrated"
	MigrationFileUnchanged      MigrationFileStatus = "unchanged"
	MigrationFileRolledBack     MigrationFileStatus = "rolled-back"
	MigrationFileRollbackFailed MigrationFileStatus = "rollback-failed"
	MigrationFileDiverged       MigrationFileStatus = "diverged"
)

type MigrationFileResult struct {
	Layer        string
	Path         string
	ResolvedPath string
	Status       MigrationFileStatus
	Created      bool
}

type MigrationCommitError struct {
	Phase          string
	Layer          string
	Path           string
	Cause          error
	RolledBack     bool
	Partial        bool
	RollbackErrors []error
	Files          []MigrationFileResult
}

func (e *MigrationCommitError) Error() string {
	if e == nil {
		return "migration commit failed"
	}
	state := "no files were changed"
	if e.RolledBack {
		state = "all published files were rolled back"
	}
	if e.Partial {
		state = "migration is partially committed"
		if summary := migrationFileStateSummary(e.Files); summary != "" {
			state += ": " + summary
		}
		if rollbackErr := errors.Join(e.RollbackErrors...); rollbackErr != nil {
			state += "; rollback failure: " + rollbackErr.Error()
		}
	}
	if e.Layer == "" {
		return fmt.Sprintf("migration %s failed: %v (%s)", e.Phase, e.Cause, state)
	}
	return fmt.Sprintf("migration %s failed for %s config %q: %v (%s)", e.Phase, e.Layer, e.Path, e.Cause, state)
}

func (e *MigrationCommitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func migrationFileStateSummary(files []MigrationFileResult) string {
	parts := make([]string, 0, len(files))
	for _, file := range files {
		parts = append(parts, fmt.Sprintf("%s=%s", file.Layer, file.Status))
	}
	return strings.Join(parts, ", ")
}

type MigrationTargetSource string

const (
	MigrationTargetExplicit     MigrationTargetSource = "explicit"
	MigrationTargetUser         MigrationTargetSource = "user-personal"
	MigrationTargetRepo         MigrationTargetSource = "repository"
	MigrationTargetOrganization MigrationTargetSource = "organization-fallback"
)

type MigrationPlannedFile struct {
	Layer        string
	Path         string
	ResolvedPath string
	Create       bool
	SourceDigest string
	OutputDigest string
}

type MigrationSourceFile struct {
	Layer        string
	Path         string
	ResolvedPath string
	Exists       bool
	Digest       string
}

// MigrationPlan is a read-only, validated plan. The same parsed documents and
// original-byte preconditions shown to a caller are consumed by ExecuteMigrationPlan;
// execution never silently recomputes a different target source or file set.
type MigrationPlan struct {
	TargetHarnesses []string
	TargetSource    MigrationTargetSource
	Files           []MigrationPlannedFile
	Sources         []MigrationSourceFile
	CreatesUser     bool

	paths MigratePaths
	files map[string]*migrationFile
}

// migrationFile holds the parsed YAML state for one config file during the
// migration pass.
type migrationFile struct {
	path           string
	resolvedPath   string
	exists         bool
	originalExists bool
	created        bool
	original       []byte
	mode           os.FileMode
	doc            *yaml.Node
	root           *yaml.Node
	kind           layerKind
	apiVersion     string
	harnesses      []string
	modified       bool
	output         []byte
}

type migrationSourceChangedError struct {
	layer string
	path  string
	cause error
}

func (e *migrationSourceChangedError) Error() string {
	return e.cause.Error()
}

func (e *migrationSourceChangedError) Unwrap() error {
	return e.cause
}

// MigrateConfigFiles migrates user personal, repository committed, and
// repository personal v2 config files to strict v3 in one locked commit plan.
// All inputs and every staged output are validated before any replacement is
// published; source changes are compare-and-swap failures and ordinary publish
// failures are rolled back.
//
// The target harness set is chosen from an explicit selection first, then the
// existing user config, then a legacy repository declaration. Repository and
// local top-level target values are removed from migrated output.
func MigrateConfigFiles(paths MigratePaths, explicitHarnesses []string) (*MigrateResult, error) {
	plan, err := PlanConfigMigration(paths, explicitHarnesses, nil)
	if err != nil {
		return nil, err
	}
	return ExecuteMigrationPlan(plan)
}

// PlanConfigMigration validates every source and constructs the exact in-memory
// documents that execution will commit. organizationHarnesses is read-only
// fallback: when it is selected no user override is created.
func PlanConfigMigration(paths MigratePaths, explicitHarnesses, organizationHarnesses []string) (*MigrationPlan, error) {
	slog.Debug("planning config migration", "candidate_count", migrationPathCount(paths), "explicit_target_count", len(explicitHarnesses), "organization_target_count", len(organizationHarnesses))

	// Deduplicate paths so the same file is not processed twice when a caller
	// passes identical user/repo/local paths.
	workPaths := paths
	files := make(map[string]*migrationFile, 3)
	seenPaths := make(map[string]struct{}, 3)
	for _, candidate := range []struct {
		name string
		path string
		kind layerKind
	}{
		{name: "user", path: paths.User, kind: userLayer},
		{name: "repo", path: paths.Repo, kind: repoLayer},
		{name: "local", path: paths.Local, kind: localLayer},
	} {
		if candidate.path == "" {
			continue
		}
		f, err := loadMigrationFile(candidate.path, candidate.kind)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[f.resolvedPath]; duplicate {
			switch candidate.name {
			case "repo":
				workPaths.Repo = ""
			case "local":
				workPaths.Local = ""
			}
			continue
		}
		seenPaths[f.resolvedPath] = struct{}{}
		files[candidate.name] = f
	}

	// Validate every existing document before any mutation.
	layerOrder := []string{"user", "repo", "local"}
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists {
			continue
		}
		if f.apiVersion == "" {
			if f.kind != localLayer {
				return nil, fmt.Errorf("%s config at %q has no apiVersion (v1 input is not supported)", name, f.path)
			}
			if len(bytes.TrimSpace(f.original)) > 0 {
				if _, errs := loadV3LocalConfig(f.original); len(errs) > 0 {
					return nil, fmt.Errorf("validate unversioned local config at %q: %w", f.path, errors.Join(errs...))
				}
			}
		} else if f.apiVersion != APIVersionV2 && f.apiVersion != APIVersionV3 {
			return nil, fmt.Errorf("%s config at %q has unsupported apiVersion %q", name, f.path, f.apiVersion)
		}
		for _, key := range []string{"identity", "team_profiles"} {
			if findMappingValue(f.root, key) != nil {
				return nil, fmt.Errorf("%w: %s config at %q contains %q", ErrLegacyIdentity, name, f.path, key)
			}
		}
		if err := validateMigrationPolicyAuthority(name, f); err != nil {
			return nil, err
		}
		if err := validateMigrationInput(f); err != nil {
			return nil, fmt.Errorf("validate %s config at %q before migration: %w", name, f.path, err)
		}
	}

	// Determine the target harness set.
	targetHarnesses, targetSource, err := resolveMigrationTargetHarnesses(files, explicitHarnesses, organizationHarnesses)
	if err != nil {
		if errors.Is(err, ErrHarnessesRequired) {
			slog.Debug("migration requires target harness selection")
		} else {
			slog.Error("failed to resolve migration target harnesses", "error_type", fmt.Sprintf("%T", err))
		}
		return nil, err
	}
	slog.Debug("resolved migration target harnesses", "target_source", targetSource, "target_count", len(targetHarnesses))

	// Build the migrated document for each existing file.
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.exists {
			continue
		}
		switch name {
		case "user":
			userTargets := targetHarnesses
			if targetSource == MigrationTargetOrganization {
				userTargets = nil
			}
			if err := migrateFileNode(f, userTargets); err != nil {
				return nil, err
			}
		case "repo":
			if err := migrateFileNode(f, nil); err != nil {
				return nil, err
			}
			if removeMappingKeyByName(f.root, "harnesses") {
				f.modified = true
			}
		case "local":
			// An unversioned config.local.yaml already is the valid local-only
			// schema. It participates in pre-write validation but is not rewritten.
			if f.apiVersion == "" {
				continue
			}
			if err := migrateFileNode(f, nil); err != nil {
				return nil, err
			}
			if removeMappingKeyByName(f.root, "harnesses") {
				f.modified = true
			}
		}
	}

	// If there is no user personal config, create one for explicit or legacy
	// repository targets so the migrated machine target set remains durable.
	// Organization fallback is read-only and must not create a user override.
	if targetSource != MigrationTargetOrganization && (files["user"] == nil || !files["user"].exists) {
		if workPaths.User == "" {
			return nil, fmt.Errorf("cannot create user personal config without a path")
		}
		userFile := files["user"]
		if userFile == nil {
			resolvedPath, err := resolveMigrationPath(workPaths.User)
			if err != nil {
				return nil, err
			}
			userFile = &migrationFile{path: workPaths.User, resolvedPath: resolvedPath, kind: userLayer}
		}
		userFile.exists = true
		userFile.created = true
		userFile.mode = defaultConfigMode
		userFile.doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
		userFile.root = userFile.doc.Content[0]
		files["user"] = userFile
		if err := migrateFileNode(userFile, targetHarnesses); err != nil {
			return nil, err
		}
	}

	// Validate the migrated documents before writing.
	if err := validateMigratedFiles(files); err != nil {
		return nil, err
	}

	plan := &MigrationPlan{
		TargetHarnesses: append([]string(nil), targetHarnesses...),
		TargetSource:    targetSource,
		paths:           paths,
		files:           files,
	}
	for _, name := range layerOrder {
		f := files[name]
		if f == nil {
			continue
		}
		plan.Sources = append(plan.Sources, MigrationSourceFile{
			Layer:        name,
			Path:         f.path,
			ResolvedPath: f.resolvedPath,
			Exists:       f.originalExists,
			Digest:       migrationDigest(f.original, f.originalExists),
		})
		if !f.modified {
			continue
		}
		plan.Files = append(plan.Files, MigrationPlannedFile{
			Layer:        name,
			Path:         f.path,
			ResolvedPath: f.resolvedPath,
			Create:       f.created,
			SourceDigest: migrationDigest(f.original, f.originalExists),
			OutputDigest: migrationDigest(f.output, true),
		})
		if name == "user" && f.created {
			plan.CreatesUser = true
		}
	}
	slog.Debug("config migration planned", "target_source", targetSource, "target_count", len(targetHarnesses), "file_count", len(plan.Files), "source_count", len(plan.Sources))
	return plan, nil
}

// ExecuteMigrationPlan commits a previously validated plan. Original bytes are
// compare-and-swapped immediately before commit; a changed source is refused.
func ExecuteMigrationPlan(plan *MigrationPlan) (*MigrateResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("migration plan is nil")
	}
	result := &MigrateResult{
		UserPath:  plan.paths.User,
		RepoPath:  plan.paths.Repo,
		LocalPath: plan.paths.Local,
	}
	if err := commitMigratedFiles(plan.files, result); err != nil {
		slog.Error("failed to write migrated configs", "error_type", fmt.Sprintf("%T", err), "partial", result.Partial, "rolled_back", result.RolledBack)
		return result, err
	}
	slog.Info("config migration complete", "user_migrated", result.UserMigrated, "repo_migrated", result.RepoMigrated, "local_migrated", result.LocalMigrated)
	return result, nil
}

// loadMigrationFile reads and parses a single config file for the migration
// pass. Missing files are represented with exists=false.
func loadMigrationFile(path string, kind layerKind) (*migrationFile, error) {
	resolvedPath, err := resolveMigrationPath(path)
	if err != nil {
		return nil, fmt.Errorf("resolve %s config %q: %w", kind.name(), path, err)
	}
	f := &migrationFile{path: path, resolvedPath: resolvedPath, kind: kind, mode: defaultConfigMode}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return nil, fmt.Errorf("read %s config %q: %w", kind.name(), path, err)
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("stat %s config %q: %w", kind.name(), path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s config %q is not a regular file", kind.name(), path)
	}
	f.mode = info.Mode().Perm()
	f.original = append([]byte(nil), data...)
	f.originalExists = true

	if len(bytes.TrimSpace(data)) == 0 {
		f.exists = true
		f.doc = &yaml.Node{Kind: yaml.DocumentNode}
		f.root, _ = ensureDocumentMapping(f.doc)
		return f, nil
	}

	if extra, err := countExtraYAMLDocuments(data); err != nil {
		return nil, fmt.Errorf("parse %s config %q: %w", kind.name(), path, err)
	} else if extra > 0 {
		return nil, fmt.Errorf("parse %s config %q: multiple YAML documents are not supported (%d trailing document(s))", kind.name(), path, extra)
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

func migrationPathCount(paths MigratePaths) int {
	count := 0
	for _, path := range []string{paths.User, paths.Repo, paths.Local} {
		if path != "" {
			count++
		}
	}
	return count
}

func resolveMigrationPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	if _, err := os.Lstat(absolute); err == nil {
		return filepath.EvalSymlinks(absolute)
	} else if !os.IsNotExist(err) {
		return "", err
	}

	probe := absolute
	var suffix []string
	for {
		if _, err := os.Lstat(probe); err == nil {
			resolved, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return absolute, nil
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

func validateMigrationPolicyAuthority(layer string, f *migrationFile) error {
	if f == nil || f.apiVersion != APIVersionV2 {
		return nil
	}

	type topLevelPolicy struct {
		key         string
		remediation string
	}
	var policies []topLevelPolicy
	switch f.kind {
	case userLayer:
		policies = []topLevelPolicy{{
			key:         "materialization",
			remediation: "move it explicitly to repository committed or repository personal config before retrying",
		}}
	case repoLayer:
		policies = []topLevelPolicy{
			{key: "backup", remediation: "move it explicitly to user or organization config before retrying"},
			{key: "logging", remediation: "move it explicitly to user or organization config before retrying"},
		}
	case localLayer:
		policies = []topLevelPolicy{
			{key: "backup", remediation: "move it explicitly to user or organization config before retrying"},
			{key: "logging", remediation: "move it explicitly to user or organization config before retrying"},
			{key: "allowed_registries", remediation: "move it explicitly to user, repository committed, or organization config before retrying"},
			{key: "tags", remediation: "move or re-express it explicitly in user or repository committed config before retrying"},
		}
	}
	for _, policy := range policies {
		if findMappingValue(f.root, policy.key) != nil {
			return fmt.Errorf("%w: %s config at %q contains %q; %s", ErrUnmigratablePolicy, layer, f.path, policy.key, policy.remediation)
		}
	}

	if f.kind == localLayer {
		registries := findMappingValue(f.root, "registries")
		if registries != nil && registries.Kind == yaml.SequenceNode {
			for index, entry := range registries.Content {
				entry = resolveAlias(entry)
				if entry == nil || entry.Kind != yaml.MappingNode {
					continue
				}
				for _, key := range []string{"required", "default"} {
					if findMappingValue(entry, key) != nil {
						return fmt.Errorf("%w: %s config at %q contains registries[%d].%s; move that registry policy explicitly to user, repository committed, or organization config before retrying", ErrUnmigratablePolicy, layer, f.path, index, key)
					}
				}
			}
		}
	}
	return nil
}

func validateMigrationInput(f *migrationFile) error {
	if f == nil || !f.originalExists {
		return nil
	}
	if f.apiVersion == "" {
		if f.kind == localLayer && len(bytes.TrimSpace(f.original)) == 0 {
			return nil
		}
		if f.kind == localLayer {
			_, errs := loadV3LocalConfig(f.original)
			return errors.Join(errs...)
		}
		return fmt.Errorf("v1 input is not supported")
	}
	if f.apiVersion == APIVersionV3 {
		var errs []error
		switch f.kind {
		case userLayer:
			_, errs = loadStrictUserV3Config(f.original, false)
		case repoLayer:
			_, errs = loadV3Config(f.original, repoLayer, false)
		case localLayer:
			_, errs = loadV3LocalConfig(f.original)
		}
		return errors.Join(errs...)
	}
	if f.apiVersion != APIVersionV2 {
		return fmt.Errorf("unsupported apiVersion %q", f.apiVersion)
	}

	if harnessNode := findMappingValue(f.root, "harnesses"); harnessNode != nil {
		if harnessNode.Kind != yaml.SequenceNode {
			return fmt.Errorf("harnesses must be a sequence")
		}
		var harnesses []string
		if err := harnessNode.Decode(&harnesses); err != nil {
			return err
		}
		normalizeHarnesses(harnesses)
		if errs := validateHarnessTargets("harnesses", harnesses); len(errs) > 0 {
			return errors.Join(errs...)
		}
	}
	return nil
}

func migrationDigest(data []byte, exists bool) string {
	if !exists {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// resolveMigrationTargetHarnesses chooses the target set that strict-v3
// migration must preserve. An explicit selection is authoritative, followed by
// an existing user-personal set and then a legacy repository declaration that
// must be moved. Organization targets are a read-only fallback and never cause
// a user file to be created.
func resolveMigrationTargetHarnesses(files map[string]*migrationFile, explicitHarnesses, organizationHarnesses []string) ([]string, MigrationTargetSource, error) {
	if len(explicitHarnesses) > 0 {
		harnesses, err := canonicalCompilerHarnesses("explicit migration harnesses", explicitHarnesses)
		return harnesses, MigrationTargetExplicit, err
	}
	if userFile := files["user"]; userFile != nil && userFile.exists && len(userFile.harnesses) > 0 {
		harnesses, err := canonicalCompilerHarnesses("user personal harnesses", userFile.harnesses)
		return harnesses, MigrationTargetUser, err
	}
	if repoFile := files["repo"]; repoFile != nil && repoFile.exists && len(repoFile.harnesses) > 0 {
		harnesses, err := canonicalCompilerHarnesses("repository harnesses", repoFile.harnesses)
		return harnesses, MigrationTargetRepo, err
	}
	if len(organizationHarnesses) > 0 {
		harnesses, err := canonicalCompilerHarnesses("organization harnesses", organizationHarnesses)
		return harnesses, MigrationTargetOrganization, err
	}
	return nil, "", ErrHarnessesRequired
}

// migrateFileNode updates the document to apiVersion v3, sets harnesses when
// provided, and converts legacy collections. It tracks whether any change was
// made while preserving comments through YAML-node edits.
func migrateFileNode(f *migrationFile, harnesses []string) error {
	if f == nil || !f.exists {
		return nil
	}

	av := findMappingValue(f.root, "apiVersion")
	if av == nil {
		f.root.Content = append([]*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "apiVersion"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: APIVersionV3},
		}, f.root.Content...)
		f.modified = true
	} else if av.Kind == yaml.ScalarNode && strings.TrimSpace(av.Value) != APIVersionV3 {
		av.Value = APIVersionV3
		f.modified = true
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

// validateMigratedFiles serializes each migrated document and validates it
// against the strict rules for its layer.
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
			requireHarnesses := true
			if len(f.harnesses) == 0 {
				// An existing user file without targets may continue to inherit a
				// read-only organization fallback; migration must not fabricate one.
				requireHarnesses = false
			}
			if _, errs := loadStrictUserV3Config(data, requireHarnesses); len(errs) > 0 {
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
		if f.modified {
			f.output = append([]byte(nil), data...)
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
	name   string
	file   *migrationFile
	tmp    string
	backup string
}

type heldMigrationLock struct {
	path string
	file *os.File
}

var migrationRename = os.Rename
var migrationPublishBarrier = func(int, *migrationFile) {}

func migrationCommitFailure(result *MigrateResult, commitErr MigrationCommitError) *MigrationCommitError {
	if result != nil {
		commitErr.Files = append([]MigrationFileResult(nil), result.Files...)
	}
	return &commitErr
}

func migrationSourceChangeIdentity(err error) (string, string) {
	var changed *migrationSourceChangedError
	if errors.As(err, &changed) {
		return changed.layer, changed.path
	}
	return "", ""
}

// commitMigratedFiles stages all modified documents next to their targets, checks
// every planned source under advisory locks, and rolls back published files on failure.
func commitMigratedFiles(files map[string]*migrationFile, result *MigrateResult) error {
	layerOrder := []string{"user", "repo", "local"}
	for _, name := range layerOrder {
		f := files[name]
		if f == nil || !f.modified {
			continue
		}
		result.Files = append(result.Files, MigrationFileResult{Layer: name, Path: f.path, ResolvedPath: f.resolvedPath, Status: MigrationFilePending, Created: f.created})
	}
	if len(result.Files) == 0 {
		return nil
	}

	locks, err := acquireMigrationLocks(files)
	if err != nil {
		return migrationCommitFailure(result, MigrationCommitError{Phase: "lock", Cause: err})
	}
	defer releaseMigrationLocks(locks)

	if err := verifyMigrationPlanState(files, nil, 0); err != nil {
		layer, path := migrationSourceChangeIdentity(err)
		setMigrationResultStatus(result, layer, MigrationFileDiverged)
		return migrationCommitFailure(result, MigrationCommitError{Phase: "precondition", Layer: layer, Path: path, Cause: err})
	}

	staged, err := stageMigratedFiles(files)
	if err != nil {
		cleanupStagedMigratedFiles(staged)
		return migrationCommitFailure(result, MigrationCommitError{Phase: "stage", Cause: err})
	}
	defer cleanupStagedMigratedFiles(staged)

	for i, stagedFile := range staged {
		migrationPublishBarrier(i, stagedFile.file)
		if err := verifyMigrationPlanState(files, staged, i); err != nil {
			layer, path := migrationSourceChangeIdentity(err)
			setMigrationResultStatus(result, layer, MigrationFileDiverged)
			return rollbackMigrationFailure(staged, i, result, "precondition", layer, path, err)
		}
		if err := migrationRename(stagedFile.tmp, stagedFile.file.resolvedPath); err != nil {
			return rollbackMigrationFailure(staged, i+1, result, "publish", stagedFile.name, stagedFile.file.path, err)
		}
		setMigrationResultStatus(result, stagedFile.name, MigrationFileMigrated)
		setMigrationResultMigrated(result, stagedFile.name, true)
	}

	if err := syncMigrationDirectories(staged); err != nil {
		result.Partial = true
		return migrationCommitFailure(result, MigrationCommitError{Phase: "durability", Cause: err, Partial: true})
	}
	return nil
}

func stageMigratedFiles(files map[string]*migrationFile) ([]stagedMigratedFile, error) {
	var staged []stagedMigratedFile
	for _, name := range []string{"user", "repo", "local"} {
		f := files[name]
		if f == nil || !f.modified {
			continue
		}
		dir := filepath.Dir(f.resolvedPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return staged, fmt.Errorf("create config directory for %q: %w", f.path, err)
		}
		tmp, err := writeMigrationTemp(dir, "."+filepath.Base(f.resolvedPath)+".migration-new-*", f.output, f.mode)
		if err != nil {
			return staged, fmt.Errorf("stage config %q: %w", f.path, err)
		}
		stagedFile := stagedMigratedFile{name: name, file: f, tmp: tmp}
		if f.originalExists {
			backup, err := writeMigrationTemp(dir, "."+filepath.Base(f.resolvedPath)+".migration-old-*", f.original, f.mode)
			if err != nil {
				_ = os.Remove(tmp)
				return staged, fmt.Errorf("stage rollback copy for %q: %w", f.path, err)
			}
			stagedFile.backup = backup
		}
		staged = append(staged, stagedFile)
	}
	return staged, nil
}

func writeMigrationTemp(dir, pattern string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func verifyMigrationPlanState(files map[string]*migrationFile, staged []stagedMigratedFile, committed int) error {
	committedFiles := make(map[*migrationFile][]byte, committed)
	for i := 0; i < committed && i < len(staged); i++ {
		committedFiles[staged[i].file] = staged[i].file.output
	}
	for _, name := range []string{"user", "repo", "local"} {
		f := files[name]
		if f == nil {
			continue
		}
		expected, exists := f.original, f.originalExists
		if output, ok := committedFiles[f]; ok {
			expected, exists = output, true
		}
		if err := verifyMigrationFileState(f, expected, exists); err != nil {
			return &migrationSourceChangedError{layer: name, path: f.path, cause: err}
		}
	}
	return nil
}

func verifyMigrationFileState(f *migrationFile, expected []byte, expectedExists bool) error {
	resolved, err := resolveMigrationPath(f.path)
	if err != nil {
		return fmt.Errorf("%w: resolve %s config %q: %v", ErrMigrationSourceChanged, f.kind.name(), f.path, err)
	}
	if filepath.Clean(resolved) != filepath.Clean(f.resolvedPath) {
		return fmt.Errorf("%w: %s config %q resolves to a different file", ErrMigrationSourceChanged, f.kind.name(), f.path)
	}
	info, err := os.Lstat(f.resolvedPath)
	if err != nil {
		if os.IsNotExist(err) && !expectedExists {
			return nil
		}
		return fmt.Errorf("%w: stat %s config %q: %v", ErrMigrationSourceChanged, f.kind.name(), f.path, err)
	}
	if !expectedExists || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s config %q changed file type or existence", ErrMigrationSourceChanged, f.kind.name(), f.path)
	}
	data, err := os.ReadFile(f.resolvedPath)
	if err != nil {
		return fmt.Errorf("%w: read %s config %q: %v", ErrMigrationSourceChanged, f.kind.name(), f.path, err)
	}
	if !bytes.Equal(data, expected) {
		return fmt.Errorf("%w: %s config %q no longer matches source digest %s (current digest %s)", ErrMigrationSourceChanged, f.kind.name(), f.path, migrationDigest(expected, true), migrationDigest(data, true))
	}
	return nil
}

func rollbackMigrationFailure(staged []stagedMigratedFile, inspectCount int, result *MigrateResult, phase, failedLayer, failedPath string, cause error) error {
	published := result.UserMigrated || result.RepoMigrated || result.LocalMigrated
	rollbackErrs := rollbackMigratedFiles(staged, inspectCount, result)
	partial := len(rollbackErrs) > 0
	result.Partial = partial
	result.RolledBack = published && !partial
	if !partial {
		result.UserMigrated = false
		result.RepoMigrated = false
		result.LocalMigrated = false
	}
	return migrationCommitFailure(result, MigrationCommitError{
		Phase:          phase,
		Layer:          failedLayer,
		Path:           failedPath,
		Cause:          cause,
		RolledBack:     result.RolledBack,
		Partial:        partial,
		RollbackErrors: rollbackErrs,
	})
}

func rollbackMigratedFiles(staged []stagedMigratedFile, inspectCount int, result *MigrateResult) []error {
	if inspectCount > len(staged) {
		inspectCount = len(staged)
	}
	var errs []error
	for i := inspectCount - 1; i >= 0; i-- {
		s := staged[i]
		data, readErr := os.ReadFile(s.file.resolvedPath)
		if s.file.originalExists {
			if readErr == nil && bytes.Equal(data, s.file.original) {
				setMigrationResultStatus(result, s.name, MigrationFileUnchanged)
				setMigrationResultMigrated(result, s.name, false)
				continue
			}
			if readErr != nil || !bytes.Equal(data, s.file.output) {
				errs = append(errs, fmt.Errorf("rollback refused for %s config %q because its published bytes changed", s.name, s.file.path))
				setMigrationResultStatus(result, s.name, MigrationFileDiverged)
				continue
			}
			if err := migrationRename(s.backup, s.file.resolvedPath); err != nil {
				errs = append(errs, fmt.Errorf("restore %s config %q: %w", s.name, s.file.path, err))
				setMigrationResultStatus(result, s.name, MigrationFileRollbackFailed)
				continue
			}
		} else {
			if os.IsNotExist(readErr) {
				setMigrationResultStatus(result, s.name, MigrationFileUnchanged)
				continue
			}
			if readErr != nil || !bytes.Equal(data, s.file.output) {
				errs = append(errs, fmt.Errorf("rollback refused for new %s config %q because its published bytes changed", s.name, s.file.path))
				setMigrationResultStatus(result, s.name, MigrationFileDiverged)
				continue
			}
			if err := os.Remove(s.file.resolvedPath); err != nil {
				errs = append(errs, fmt.Errorf("remove new %s config %q during rollback: %w", s.name, s.file.path, err))
				setMigrationResultStatus(result, s.name, MigrationFileRollbackFailed)
				continue
			}
		}
		setMigrationResultStatus(result, s.name, MigrationFileRolledBack)
		setMigrationResultMigrated(result, s.name, false)
	}
	if err := syncMigrationDirectories(staged[:inspectCount]); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func setMigrationResultStatus(result *MigrateResult, layer string, status MigrationFileStatus) {
	for i := range result.Files {
		if result.Files[i].Layer == layer {
			result.Files[i].Status = status
			return
		}
	}
}

func setMigrationResultMigrated(result *MigrateResult, layer string, migrated bool) {
	switch layer {
	case "user":
		result.UserMigrated = migrated
	case "repo":
		result.RepoMigrated = migrated
	case "local":
		result.LocalMigrated = migrated
	}
}

func acquireMigrationLocks(files map[string]*migrationFile) ([]heldMigrationLock, error) {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		if f != nil {
			paths = append(paths, f.resolvedPath)
		}
	}
	sort.Strings(paths)
	locks := make([]heldMigrationLock, 0, len(paths))
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			releaseMigrationLocks(locks)
			return nil, err
		}
		lockPath := path + ".agent-marketplace.lock"
		file, err := os.OpenFile(lockPath, migrationLockOpenFlags, 0o600)
		if err != nil {
			releaseMigrationLocks(locks)
			return nil, err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			releaseMigrationLocks(locks)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("migration lock is not a regular file")
		}
		if err := lockMigrationFile(file); err != nil {
			_ = file.Close()
			releaseMigrationLocks(locks)
			return nil, err
		}
		locks = append(locks, heldMigrationLock{path: lockPath, file: file})
	}
	return locks, nil
}

func releaseMigrationLocks(locks []heldMigrationLock) {
	for i := len(locks) - 1; i >= 0; i-- {
		_ = unlockMigrationFile(locks[i].file)
		_ = locks[i].file.Close()
	}
}

func syncMigrationDirectories(staged []stagedMigratedFile) error {
	seen := make(map[string]struct{}, len(staged))
	for _, s := range staged {
		dir := filepath.Dir(s.file.resolvedPath)
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		file, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = file.Sync()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func cleanupStagedMigratedFiles(staged []stagedMigratedFile) {
	for _, s := range staged {
		_ = os.Remove(s.tmp)
		_ = os.Remove(s.backup)
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
