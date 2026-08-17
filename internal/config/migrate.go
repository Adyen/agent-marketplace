package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
// schema version unless changes are required; when changes are required the
// output is canonicalized to agent-marketplace/v2. All inputs are validated
// before any file is written.
//
// The machine-wide target harness set is chosen as follows:
//  1. Harnesses already present in the user personal config.
//  2. The explicit harnesses argument.
//  3. Harnesses declared in the repository committed config (moved to user).
//  4. If none of the above are available, ErrHarnessesRequired is returned.
//
// The following structural fixes are applied:
//   - Top-level harnesses, backup, and logging are removed from repo configs.
//   - Top-level harnesses, backup, logging, and allowed_registries are removed
//     from local configs.
//   - Legacy collections sequences are converted to the {include: [...]} form.
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
			for _, key := range []string{"harnesses", "backup", "logging", "allowed_registries"} {
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
// collections. It tracks whether any change was made. The caller is responsible
// for canonicalizing the apiVersion after all structural edits.
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

// canonicalizeMigrationAPIVersion ensures the file declares a valid apiVersion.
// If the file was modified and declares v3, it is rewritten to the canonical v2.
// If apiVersion is missing, it is inserted as v2. Unsupported values are also
// replaced with v2. When no modifications were made and the apiVersion is
// already valid, the document is left untouched so valid v3 aliases remain
// readable and stable.
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
