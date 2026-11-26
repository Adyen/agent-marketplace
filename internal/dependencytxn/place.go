package dependencytxn

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/rootkey"
)

type PlacePlan struct {
	Root         string
	RootIdentity fsboundary.Identity
	Directories  []string
	Writes       []PlaceWrite
	Ownership    DesiredFile
	Prunes       []PlacePrune
}

type PlaceWrite struct {
	Path          string
	GenerationKey string
	SourcePath    string
	Expected      Generation
	ExpectedMode  fs.FileMode
	Digest        string
	Mode          fs.FileMode
	Size          int64
	// Parent pins an already-existing destination parent.
	Parent *fsboundary.Identity
	// ParentCreated requires the transaction to create the immediate parent.
	ParentCreated bool
	Link          *fsboundary.CapturedLink
}

type PlacePrune struct {
	Path      string
	Parent    fsboundary.Identity
	Expected  Generation
	Mode      fs.FileMode
	Directory *fsboundary.Identity
	Link      *fsboundary.CapturedLink
}

// PlaceDirectory records a directory created and owned by place delivery.
type PlaceDirectory struct {
	Path     string              `json:"path"`
	Identity fsboundary.Identity `json:"identity"`
}

type PlaceResult struct {
	Changed  []string
	Retained []string
}

type placeIntent struct {
	Root                  string                 `json:"root"`
	RootIdentity          fsboundary.Identity    `json:"root_identity"`
	Stage                 string                 `json:"stage"`
	StageIdentity         *fsboundary.Identity   `json:"stage_identity,omitempty"`
	OwnershipPath         string                 `json:"ownership_path"`
	OwnershipExpected     Generation             `json:"ownership_expected"`
	OwnershipExpectedMode fs.FileMode            `json:"ownership_expected_mode"`
	OwnershipMode         fs.FileMode            `json:"ownership_mode"`
	OwnershipInputDigest  string                 `json:"ownership_input_digest"`
	OwnershipInputSize    int64                  `json:"ownership_input_size"`
	GenerationKeys        []string               `json:"generation_keys,omitempty"`
	OwnershipNextDigest   string                 `json:"ownership_next_digest,omitempty"`
	OwnershipNextSize     int64                  `json:"ownership_next_size,omitempty"`
	Writes                []placeWriteIntent     `json:"writes"`
	Directories           []placeDirectoryIntent `json:"directories,omitempty"`
	Prunes                []PlacePrune           `json:"prunes,omitempty"`
	Operations            []placeOperation       `json:"operations"`
	Completed             int                    `json:"completed"`
	Pending               *placeOperation        `json:"pending,omitempty"`
	Retained              []string               `json:"retained,omitempty"`
}

type placeWriteIntent struct {
	Path          string                   `json:"path"`
	GenerationKey string                   `json:"generation_key"`
	SourcePath    string                   `json:"source_path"`
	Expected      Generation               `json:"expected"`
	ExpectedMode  fs.FileMode              `json:"expected_mode"`
	Digest        string                   `json:"digest"`
	Mode          fs.FileMode              `json:"mode"`
	Size          int64                    `json:"size"`
	Parent        *fsboundary.Identity     `json:"parent,omitempty"`
	ParentCreated bool                     `json:"parent_created,omitempty"`
	Link          *fsboundary.CapturedLink `json:"link,omitempty"`
	StageName     string                   `json:"stage_name"`
}

type placeDirectoryIntent struct {
	Path     string               `json:"path"`
	Identity *fsboundary.Identity `json:"identity,omitempty"`
}

type placeOperation struct {
	Kind  string `json:"kind"`
	Index int    `json:"index"`
	Path  string `json:"path"`
}

type placeStageFile struct {
	Name          string      `json:"name"`
	Digest        string      `json:"digest"`
	Size          int64       `json:"size"`
	Mode          fs.FileMode `json:"mode"`
	GenerationKey string      `json:"generation_key,omitempty"`
	SourcePath    string      `json:"source_path,omitempty"`
}

const (
	placeOwnershipInputName = "ownership-input"
	placeOwnershipNextName  = "ownership-next"
	placeStageMode          = 0o700
	placeFileMode           = 0o600
)

func clonePlaceIdentity(identity *fsboundary.Identity) *fsboundary.Identity {
	if identity == nil {
		return nil
	}
	copy := *identity
	return &copy
}

// PlaceFileLimit is the maximum size of an observed or committed place file.
const PlaceFileLimit int64 = maxDependencyFileSize

func CommitPlace(ctx context.Context, stateDir string, plan PlacePlan, opts ...Option) (PlaceResult, error) {
	cfg := applyOptions(opts)
	if !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		return PlaceResult{}, fmt.Errorf("%w: state directory must be a clean absolute path", ErrUnsafePath)
	}
	canonicalStateDir, err := canonicalizeStatePath(stateDir)
	if err != nil || canonicalStateDir != stateDir {
		return PlaceResult{}, fmt.Errorf("%w: state directory is not its canonical spelling: %s", ErrUnsafePath, stateDir)
	}
	stateRoot := filepath.Dir(stateDir)
	if err := validatePlacePlan(plan, stateRoot); err != nil {
		return PlaceResult{}, err
	}
	if err := CheckPending(stateDir); err != nil {
		return PlaceResult{}, err
	}
	preflightRoot, err := openPlaceRoot(plan.Root, plan.RootIdentity)
	if err != nil {
		return PlaceResult{}, err
	}
	if err := preflightRoot.Close(); err != nil {
		return PlaceResult{}, err
	}
	if _, err := prepareStateDir(stateDir); err != nil {
		return PlaceResult{}, err
	}
	id, err := randomID()
	if err != nil {
		return PlaceResult{}, err
	}
	sizingDirectories := make([]placeDirectoryIntent, len(plan.Directories))
	for i, directory := range plan.Directories {
		sizingDirectories[i].Path = directory
	}
	sizingIntent, err := newPlaceIntent(id, stateRoot, plan, sizingDirectories)
	if err != nil {
		return PlaceResult{}, err
	}
	if err := checkPlaceJournalSize(&journal{Version: journalVersion, ID: id, Place: sizingIntent}); err != nil {
		return PlaceResult{}, err
	}
	pathSet := make(map[string]struct{}, len(plan.Writes)+len(plan.Prunes)+1+len(cfg.reads))
	for _, path := range placeLockPaths(plan) {
		pathSet[path] = struct{}{}
	}
	for _, read := range cfg.reads {
		if err := validateReadAssertion(read); err != nil {
			return PlaceResult{}, err
		}
		pathSet[read.Path] = struct{}{}
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	locks, err := acquireLocks(ctx, paths)
	if err != nil {
		return PlaceResult{}, err
	}
	defer releaseLocks(locks)
	if err := inject(cfg, FaultPoint{Stage: FaultAfterLocks, Index: -1}); err != nil {
		return PlaceResult{}, err
	}
	if err := checkPendingDestinations(stateDir, paths); err != nil {
		return PlaceResult{}, err
	}

	root, err := openPlaceRoot(plan.Root, plan.RootIdentity)
	if err != nil {
		return PlaceResult{}, err
	}
	defer func() { _ = root.Close() }()
	if err := validatePlaceOwnership(stateRoot, plan); err != nil {
		return PlaceResult{}, err
	}
	for _, write := range plan.Writes {
		if _, err := readPlaceGeneration(stateRoot, write); err != nil {
			return PlaceResult{}, err
		}
	}
	directories, err := missingPlaceDirectories(root, plan)
	if err != nil {
		return PlaceResult{}, err
	}
	for i := range plan.Writes {
		if err := validatePlaceWriteDestination(root, plan.Writes[i], directories); err != nil {
			return PlaceResult{}, err
		}
	}
	for i := range plan.Prunes {
		if err := validatePlacePruneDestination(root, plan.Prunes[i]); err != nil {
			return PlaceResult{}, err
		}
	}
	if err := validatePlaceDevices(root, stateRoot, plan.Writes, sizingIntent.OwnershipPath); err != nil {
		return PlaceResult{}, err
	}

	intent, err := newPlaceIntent(id, stateRoot, plan, directories)
	if err != nil {
		return PlaceResult{}, err
	}
	j := journal{Version: journalVersion, ID: id, Place: intent}
	if err := checkPlaceJournalSize(&j); err != nil {
		return PlaceResult{}, err
	}
	if err := ValidateReadAssertions(cfg.reads); err != nil {
		return PlaceResult{}, err
	}
	journalPath, err := persistJournal(stateDir, &j, cfg.syncDir)
	if err != nil {
		return PlaceResult{}, err
	}

	stage, stageParent, err := createPlaceStage(stateRoot, intent)
	if err != nil {
		return PlaceResult{}, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	stageIdentity := stage.Identity()
	intent.StageIdentity = &stageIdentity
	if err := persistPlaceJournal(stateDir, journalPath, &j, cfg.syncDir); err != nil {
		_ = stage.Close()
		_ = stageParent.Close()
		return PlaceResult{}, err
	}
	for i, write := range intent.Writes {
		if err := ctx.Err(); err != nil {
			_ = stage.Close()
			_ = stageParent.Close()
			return PlaceResult{}, err
		}
		source, err := readPlaceGeneration(stateRoot, plan.Writes[i])
		if err != nil {
			_ = stage.Close()
			_ = stageParent.Close()
			return PlaceResult{}, placeStop(ErrRecoveryBlocked, journalPath, write.Path, err)
		}
		if err := writePlaceStageFile(stage, write.StageName, source, write.Mode); err != nil {
			_ = stage.Close()
			_ = stageParent.Close()
			return PlaceResult{}, placeStop(ErrRecoveryBlocked, journalPath, write.Path, err)
		}
		if err := stage.Sync(); err != nil {
			_ = stage.Close()
			_ = stageParent.Close()
			return PlaceResult{}, err
		}
		if err := inject(cfg, FaultPoint{Stage: FaultPlaceAfterStage, Index: i, Path: write.Path}); err != nil {
			_ = stage.Close()
			_ = stageParent.Close()
			return PlaceResult{}, err
		}
	}
	if err := writePlaceStageFile(stage, placeOwnershipInputName, plan.Ownership.Bytes, placeFileMode); err != nil {
		_ = stage.Close()
		_ = stageParent.Close()
		return PlaceResult{}, placeStop(ErrRecoveryBlocked, journalPath, intent.OwnershipPath, err)
	}
	if err := stage.Sync(); err != nil {
		_ = stage.Close()
		_ = stageParent.Close()
		return PlaceResult{}, err
	}
	_ = stage.Close()
	_ = stageParent.Close()

	result, err := executePlaceOperations(ctx, stateDir, stateRoot, journalPath, &j, cfg, false)
	if err != nil {
		return result, err
	}
	return result, nil
}

func ReadPendingPlaceGenerationKeys(stateDir string) ([]string, error) {
	stateDir, err := normalizeDestinationPath(stateDir)
	if err != nil {
		return nil, err
	}
	if err := validateDestinationAncestors(stateDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(stateDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{})
	for _, entry := range entries {
		if !isJournalEntry(entry) {
			continue
		}
		journalPath := filepath.Join(stateDir, entry.Name())
		hasPlace, err := journalHasPlace(journalPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect dependency journal %s: %w", journalPath, err)
		}
		if !hasPlace {
			continue
		}
		j, err := readJournal(journalPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if j.Place == nil {
			continue
		}
		for _, key := range j.Place.GenerationKeys {
			keys[key] = struct{}{}
		}
		for _, write := range j.Place.Writes {
			keys[write.GenerationKey] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func validatePlacePlan(plan PlacePlan, stateRoot string) error {
	if !filepath.IsAbs(plan.Root) || filepath.Clean(plan.Root) != plan.Root {
		return fmt.Errorf("%w: place root must be a clean absolute path", ErrUnsafePath)
	}
	if err := validatePlaceRootStateOverlap(plan.Root, stateRoot); err != nil {
		return err
	}
	if plan.RootIdentity.Inode == 0 || !plan.RootIdentity.Mode.IsDir() {
		return fmt.Errorf("%w: missing captured place root identity", ErrUnsafePath)
	}
	if err := validateGeneration(plan.Ownership.Expected); err != nil {
		return fmt.Errorf("%w: invalid ownership generation: %v", ErrUnsafePath, err)
	}
	if plan.Ownership.Delete || len(plan.Ownership.Bytes) == 0 || int64(len(plan.Ownership.Bytes)) > maxDependencyFileSize ||
		plan.Ownership.Mode != plan.Ownership.Mode.Perm() || plan.Ownership.Mode == 0 {
		return fmt.Errorf("%w: invalid ownership participant", ErrUnsafePath)
	}
	rootKey := rootkey.ForCanonicalRoot(plan.Root)
	wantOwnership := filepath.Join(stateRoot, "ownership", rootKey, "state.json")
	if plan.Ownership.Path != wantOwnership {
		return fmt.Errorf("%w: ownership participant must be %s", ErrUnsafePath, wantOwnership)
	}
	if plan.Ownership.ExpectedMode != plan.Ownership.ExpectedMode.Perm() ||
		(!plan.Ownership.Expected.Exists && plan.Ownership.ExpectedMode != 0) {
		return fmt.Errorf("%w: invalid ownership mode", ErrUnsafePath)
	}
	if err := validatePlaceOwnershipBytes(plan.Ownership.Bytes, plan.Root, rootKey); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(plan.Writes)+len(plan.Prunes))
	for _, write := range plan.Writes {
		if err := validatePlaceRelative(write.Path); err != nil {
			return err
		}
		if _, ok := seen[write.Path]; ok {
			return fmt.Errorf("%w: duplicate place path %s", ErrUnsafePath, write.Path)
		}
		seen[write.Path] = struct{}{}
		if !validPlaceKey(write.GenerationKey) || !validPlaceKey(write.Digest) || write.Size < 0 || write.Size > maxDependencyFileSize ||
			write.Mode != write.Mode.Perm() || write.Mode == 0 || write.ExpectedMode != write.ExpectedMode.Perm() {
			return fmt.Errorf("%w: invalid place write for %s", ErrUnsafePath, write.Path)
		}
		if write.Parent != nil && (write.Parent.Inode == 0 || !write.Parent.Mode.IsDir() || write.Link != nil) {
			return fmt.Errorf("%w: invalid or redirected parent proof for %s", ErrUnsafePath, write.Path)
		}
		if write.ParentCreated && (write.Parent != nil || write.Link != nil ||
			write.Expected.Exists || write.ExpectedMode != 0 ||
			!slices.Contains(plan.Directories, placeParentPath(write.Path))) {
			return fmt.Errorf("%w: invalid created-parent expectation for %s", ErrUnsafePath, write.Path)
		}
		if err := validateGeneration(write.Expected); err != nil ||
			(!write.Expected.Exists && write.ExpectedMode != 0) {
			return fmt.Errorf("%w: invalid expected generation for %s", ErrUnsafePath, write.Path)
		}
		if err := validatePlaceRelative(write.SourcePath); err != nil {
			return err
		}
		if write.Link != nil && (write.Link.ParentPath == "" || write.Link.Name == "" || write.Link.TargetText == "" || write.Link.Target == "") {
			return fmt.Errorf("%w: invalid captured link for %s", ErrUnsafePath, write.Path)
		}
	}
	for _, prune := range plan.Prunes {
		if err := validatePlaceRelative(prune.Path); err != nil {
			return err
		}
		if _, ok := seen[prune.Path]; ok {
			return fmt.Errorf("%w: duplicate place path %s", ErrUnsafePath, prune.Path)
		}
		seen[prune.Path] = struct{}{}
		if prune.Parent.Inode == 0 || !prune.Parent.Mode.IsDir() || prune.Mode != prune.Mode.Perm() ||
			validateGeneration(prune.Expected) != nil {
			return fmt.Errorf("%w: invalid place prune for %s", ErrUnsafePath, prune.Path)
		}
		if prune.Directory != nil {
			if !prune.Directory.Mode.IsDir() || prune.Directory.Inode == 0 || prune.Expected.Exists {
				return fmt.Errorf("%w: invalid directory prune for %s", ErrUnsafePath, prune.Path)
			}
		} else if !prune.Expected.Exists || prune.Mode == 0 {
			return fmt.Errorf("%w: invalid file prune for %s", ErrUnsafePath, prune.Path)
		}
		if prune.Link != nil && (prune.Link.ParentPath == "" || prune.Link.Name == "" || prune.Link.TargetText == "" || prune.Link.Target == "") {
			return fmt.Errorf("%w: invalid captured prune link for %s", ErrUnsafePath, prune.Path)
		}
	}
	seenDirectories := make(map[string]struct{}, len(plan.Directories))
	for i, directory := range plan.Directories {
		if err := validatePlaceRelative(directory); err != nil {
			return err
		}
		if _, ok := seenDirectories[directory]; ok {
			return fmt.Errorf("%w: duplicate place directory %s", ErrUnsafePath, directory)
		}
		seenDirectories[directory] = struct{}{}
		if i > 0 && placeDepth(plan.Directories[i-1]) > placeDepth(directory) {
			return fmt.Errorf("%w: place directories are not ordered parent-first", ErrUnsafePath)
		}
		used := false
		for _, write := range plan.Writes {
			parent := path.Dir(write.Path)
			if parent != "." && placePathWithin(parent, directory) {
				used = true
				break
			}
		}
		if !used {
			return fmt.Errorf("%w: unreferenced place directory %s", ErrUnsafePath, directory)
		}
	}
	return nil
}

func validatePlaceRootStateOverlap(root, stateRoot string) error {
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot {
		return fmt.Errorf("%w: invalid place state root", ErrUnsafePath)
	}
	if absolutePathWithin(root, stateRoot) ||
		absolutePathWithin(filepath.Join(stateRoot, "place-stage"), root) ||
		absolutePathWithin(filepath.Join(stateRoot, "packages"), root) {
		return fmt.Errorf("%w: place target root overlaps generation or staging state", ErrUnsafePath)
	}
	return nil
}

func absolutePathWithin(parent, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil && (relative == "." ||
		relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative))
}

func validatePlaceRelative(value string) error {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl) ||
		path.Clean(value) != value || !fs.ValidPath(value) || value == "." || filepath.IsAbs(value) {
		return fmt.Errorf("%w: invalid place-relative path %q", ErrUnsafePath, value)
	}
	return nil
}

func validPlaceKey(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validatePlaceOwnershipBytes(raw []byte, root, rootKey string) error {
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil || record == nil {
		return fmt.Errorf("%w: invalid ownership participant bytes", ErrUnsafePath)
	}
	var canonical, key string
	if err := json.Unmarshal(record["canonical_root"], &canonical); err != nil || canonical != root {
		return fmt.Errorf("%w: ownership canonical_root does not match place root", ErrUnsafePath)
	}
	if err := json.Unmarshal(record["root_key"], &key); err != nil || key != rootKey {
		return fmt.Errorf("%w: ownership root_key does not match place root", ErrUnsafePath)
	}
	if dirs, ok := record["place_directories"]; ok {
		var values []PlaceDirectory
		if err := json.Unmarshal(dirs, &values); err != nil {
			return fmt.Errorf("%w: invalid place_directories ownership field", ErrUnsafePath)
		}
		seen := make(map[string]struct{}, len(values))
		for _, directory := range values {
			if err := validatePlaceRelative(directory.Path); err != nil || directory.Identity.Inode == 0 || !directory.Identity.Mode.IsDir() {
				return fmt.Errorf("%w: invalid recorded place directory", ErrUnsafePath)
			}
			if _, ok := seen[directory.Path]; ok {
				return fmt.Errorf("%w: duplicate recorded place directory %s", ErrUnsafePath, directory.Path)
			}
			seen[directory.Path] = struct{}{}
		}
	}
	return nil
}

func placeLockPaths(plan PlacePlan) []string {
	paths := make([]string, 0, len(plan.Writes)+len(plan.Prunes)+1)
	for _, write := range plan.Writes {
		paths = append(paths, filepath.Join(plan.Root, filepath.FromSlash(write.Path)))
	}
	for _, prune := range plan.Prunes {
		paths = append(paths, filepath.Join(plan.Root, filepath.FromSlash(prune.Path)))
	}
	paths = append(paths, plan.Ownership.Path)
	sort.Strings(paths)
	return paths
}

func openPlaceRoot(rootPath string, expected fsboundary.Identity) (*fsboundary.Dir, error) {
	root, err := fsboundary.OpenCanonicalDir(rootPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open place root %s: %w", ErrGenerationConflict, rootPath, err)
	}
	if !root.Identity().SameObject(expected) {
		_ = root.Close()
		return nil, fmt.Errorf("%w: place root identity changed: %s", ErrGenerationConflict, rootPath)
	}
	return root, nil
}

func validatePlaceOwnership(stateRoot string, plan PlacePlan) error {
	rootKey := rootkey.ForCanonicalRoot(plan.Root)
	raw, current, mode, err := inspectPlaceOwnership(stateRoot, filepath.ToSlash(filepath.Join("ownership", rootKey, "state.json")))
	if err != nil {
		return err
	}
	if current != plan.Ownership.Expected || current.Exists && mode != plan.Ownership.ExpectedMode {
		return fmt.Errorf("%w: ownership participant %s", ErrGenerationConflict, plan.Ownership.Path)
	}
	if current.Exists {
		if err := validatePlaceOwnershipBytes(raw, plan.Root, rootKey); err != nil {
			return fmt.Errorf("%w: prior ownership participant %s: %v", ErrGenerationConflict, plan.Ownership.Path, err)
		}
	}
	return nil
}

func missingPlaceDirectories(root *fsboundary.Dir, plan PlacePlan) ([]placeDirectoryIntent, error) {
	requested := make(map[string]bool, len(plan.Directories))
	for _, dir := range plan.Directories {
		requested[dir] = true
	}
	missing := make([]placeDirectoryIntent, 0, len(plan.Directories))
	for _, dir := range plan.Directories {
		opened, err := root.OpenPath(dir)
		if err == nil {
			if closeErr := opened.Close(); closeErr != nil {
				return nil, closeErr
			}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: place directory %s: %v", ErrUnsafePath, dir, err)
		}
		parent := path.Dir(dir)
		for parent != "." {
			if requested[parent] {
				break
			}
			opened, openErr := root.OpenPath(parent)
			if openErr == nil {
				_ = opened.Close()
				break
			}
			if !errors.Is(openErr, fs.ErrNotExist) {
				return nil, fmt.Errorf("%w: place directory parent %s: %v", ErrUnsafePath, parent, openErr)
			}
			parent = path.Dir(parent)
		}
		missing = append(missing, placeDirectoryIntent{Path: dir})
	}
	for _, directory := range missing {
		used := false
		for _, write := range plan.Writes {
			if placePathWithin(path.Dir(write.Path), directory.Path) {
				used = true
				break
			}
		}
		if !used {
			return nil, fmt.Errorf("%w: unreferenced place directory %s", ErrUnsafePath, directory.Path)
		}
	}
	return missing, nil
}

func placeDepth(value string) int { return strings.Count(value, "/") + 1 }

func placePathWithin(parent, directory string) bool {
	return parent == directory || strings.HasPrefix(parent, directory+"/")
}

func placeParentPath(rel string) string {
	parent := path.Dir(rel)
	if parent == "." {
		return "."
	}
	return parent
}

func openPlaceParent(root *fsboundary.Dir, rel string) (*fsboundary.Dir, error) {
	return root.OpenPath(placeParentPath(rel))
}

func validatePlaceWriteDestination(root *fsboundary.Dir, write PlaceWrite, missing []placeDirectoryIntent) error {
	destination, err := fsboundary.CaptureDestination(root, write.Path, fsboundary.LinkPolicyRepository)
	if errors.Is(err, fs.ErrNotExist) {
		if write.ParentCreated {
			for _, directory := range missing {
				if directory.Path == placeParentPath(write.Path) {
					return nil
				}
			}
			return fmt.Errorf("%w: created parent was not missing at preflight for %s", ErrGenerationConflict, write.Path)
		}
		if write.Parent != nil {
			return fmt.Errorf("%w: parent proof missing for destination %s", ErrGenerationConflict, write.Path)
		}
		if write.Expected.Exists || write.ExpectedMode != 0 || write.Link != nil {
			return fmt.Errorf("%w: missing parent for expected destination %s", ErrGenerationConflict, write.Path)
		}
		for _, dir := range missing {
			if placePathWithin(placeParentPath(write.Path), dir.Path) {
				return nil
			}
		}
		return fmt.Errorf("%w: unjournaled missing parent for %s", ErrUnsafePath, write.Path)
	}
	if err != nil {
		return fmt.Errorf("%w: inspect destination for %s: %v", ErrUnsafePath, write.Path, err)
	}
	defer func() { _ = destination.Close() }()
	if write.ParentCreated {
		return fmt.Errorf("%w: created parent already exists for %s", ErrGenerationConflict, write.Path)
	}
	if err := verifyPlaceWriteParent(destination, write.Parent); err != nil {
		return err
	}
	if !samePlaceLink(destination.Link(), write.Link) {
		return fmt.Errorf("%w: captured link changed for %s", ErrGenerationConflict, write.Path)
	}
	generation, mode, err := readPlaceDestination(destination)
	if err != nil {
		return err
	}
	if generation != write.Expected || generation.Exists && mode != write.ExpectedMode {
		return fmt.Errorf("%w: place destination %s", ErrGenerationConflict, write.Path)
	}
	return nil
}

func verifyPlaceWriteParent(destination *fsboundary.Destination, expected *fsboundary.Identity) error {
	if expected == nil {
		return nil
	}
	parent, err := destination.StagingDir()
	if err != nil {
		return fmt.Errorf("%w: inspect proven write parent: %w", ErrGenerationConflict, err)
	}
	actual := parent.Identity()
	closeErr := parent.Close()
	if closeErr != nil {
		return fmt.Errorf("close proven write parent: %w", closeErr)
	}
	if !actual.SameObject(*expected) {
		return fmt.Errorf("%w: place write parent changed", ErrGenerationConflict)
	}
	return nil
}

func inspectPlaceWriteLeaf(
	root *fsboundary.Dir,
	write placeWriteIntent,
	directories []placeDirectoryIntent,
) (Generation, fs.FileMode, error) {
	destination, err := fsboundary.CaptureDestination(root, write.Path, fsboundary.LinkPolicyRepository)
	if err != nil {
		return Generation{}, 0, fmt.Errorf("%w: capture place write %s: %w", ErrUnsafePath, write.Path, err)
	}
	defer func() { _ = destination.Close() }()
	expected, err := placeWriteParentProof(write, directories)
	if err != nil {
		return Generation{}, 0, err
	}
	if err := verifyPlaceWriteParent(destination, expected); err != nil {
		return Generation{}, 0, fmt.Errorf("inspect place write %s: %w", write.Path, err)
	}
	if !samePlaceLink(destination.Link(), write.Link) {
		return Generation{}, 0, fmt.Errorf("%w: captured link changed for %s", ErrGenerationConflict, write.Path)
	}
	return readPlaceDestination(destination)
}

func placeWriteParentProof(
	write placeWriteIntent,
	directories []placeDirectoryIntent,
) (*fsboundary.Identity, error) {
	if write.Parent != nil {
		return write.Parent, nil
	}
	parentPath := path.Dir(write.Path)
	for _, directory := range directories {
		if directory.Path != parentPath {
			continue
		}
		if directory.Identity == nil && write.ParentCreated {
			return nil, fmt.Errorf("%w: created parent identity is not recorded for %s", ErrRecoveryBlocked, write.Path)
		}
		return directory.Identity, nil
	}
	if write.ParentCreated {
		return nil, fmt.Errorf("%w: created parent is not journaled for %s", ErrRecoveryBlocked, write.Path)
	}
	return nil, nil
}

func placeIntentHasDirectory(directories []placeDirectoryIntent, value string) bool {
	for _, directory := range directories {
		if directory.Path == value {
			return true
		}
	}
	return false
}

func validatePlacePruneDestination(root *fsboundary.Dir, prune PlacePrune) error {
	parent, err := openPlaceParent(root, prune.Path)
	if err != nil {
		return fmt.Errorf("%w: inspect prune parent for %s: %v", ErrGenerationConflict, prune.Path, err)
	}
	if !parent.Identity().SameObject(prune.Parent) {
		_ = parent.Close()
		return fmt.Errorf("%w: prune parent changed for %s", ErrGenerationConflict, prune.Path)
	}
	defer func() { _ = parent.Close() }()
	if prune.Directory != nil {
		dir, err := root.OpenPath(prune.Path)
		if err != nil {
			return fmt.Errorf("%w: inspect prune directory %s: %v", ErrGenerationConflict, prune.Path, err)
		}
		defer func() { _ = dir.Close() }()
		if !dir.Identity().SameObject(*prune.Directory) {
			return fmt.Errorf("%w: prune directory changed for %s", ErrGenerationConflict, prune.Path)
		}
		return nil
	}
	generation, mode, err := inspectPlaceLeaf(root, prune.Path, prune.Link)
	if err != nil {
		return err
	}
	if generation != prune.Expected || mode != prune.Mode {
		return fmt.Errorf("%w: stale place file %s", ErrGenerationConflict, prune.Path)
	}
	return nil
}

func inspectPlaceLeaf(root *fsboundary.Dir, rel string, link *fsboundary.CapturedLink) (Generation, fs.FileMode, error) {
	destination, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return Generation{}, 0, fmt.Errorf("%w: capture place destination %s: %w", ErrUnsafePath, rel, err)
	}
	defer func() { _ = destination.Close() }()
	if !samePlaceLink(destination.Link(), link) {
		return Generation{}, 0, fmt.Errorf("%w: captured link changed for %s", ErrGenerationConflict, rel)
	}
	return readPlaceDestination(destination)
}

func readPlaceDestination(destination *fsboundary.Destination) (Generation, fs.FileMode, error) {
	if !destination.Exists() {
		return Generation{}, 0, destination.Verify()
	}
	data, identity, err := destination.ReadTarget(maxDependencyFileSize)
	if err != nil {
		return Generation{}, 0, err
	}
	return Generation{Exists: true, Digest: Digest(data)}, identity.Mode.Perm(), nil
}

func samePlaceLink(a, b *fsboundary.CapturedLink) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ParentPath == b.ParentPath && a.Name == b.Name &&
		a.TargetText == b.TargetText && a.Target == b.Target
}

func readPlaceGeneration(stateRoot string, write PlaceWrite) ([]byte, error) {
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	rel := path.Join("packages", write.GenerationKey, write.SourcePath)
	file, identity, err := root.OpenFile(rel)
	if err != nil {
		return nil, fmt.Errorf("%w: read place generation %s/%s: %w", ErrRecoveryBlocked, write.GenerationKey, write.SourcePath, err)
	}
	defer func() { _ = file.Close() }()
	data, after, err := fsboundary.ReadFile(file, maxDependencyFileSize)
	if err != nil {
		return nil, err
	}
	if !identity.SameState(after) || int64(len(data)) != write.Size || Digest(data) != write.Digest || after.Mode.Perm() != write.Mode {
		return nil, fmt.Errorf("%w: immutable place generation changed: %s/%s", ErrGenerationConflict, write.GenerationKey, write.SourcePath)
	}
	return data, nil
}

func validatePlaceDevices(root *fsboundary.Dir, stateRoot string, writes []PlaceWrite, ownershipRel string) error {
	state, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()
	stageDevice := state.Identity().Device
	if stageParent, err := state.OpenPath("place-stage"); err == nil {
		stageDevice = stageParent.Identity().Device
		_ = stageParent.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, write := range writes {
		if err := validatePlacePublishDevice(root, write.Path, write.Link, stageDevice); err != nil {
			return err
		}
	}
	// Ownership is published by rename too, but its leaf may never be a link.
	return validatePlacePublishDevice(state, ownershipRel, nil, stageDevice)
}

func validatePlacePublishDevice(root *fsboundary.Dir, rel string, link *fsboundary.CapturedLink, stageDevice uint64) error {
	device := root.Identity().Device
	if link != nil {
		destination, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
		if err != nil {
			return err
		}
		defer func() { _ = destination.Close() }()
		effective, err := destination.StagingDir()
		if err != nil {
			return err
		}
		device = effective.Identity().Device
		_ = effective.Close()
	} else {
		for ancestor := path.Dir(rel); ancestor != "."; ancestor = path.Dir(ancestor) {
			dir, err := root.OpenPath(ancestor)
			if err == nil {
				device = dir.Identity().Device
				_ = dir.Close()
				break
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	if device != stageDevice {
		return fmt.Errorf("%w: place stage device %d differs from target ancestor device %d at %s", ErrUnsafePath, stageDevice, device, rel)
	}
	return nil
}

func newPlaceIntent(id, stateRoot string, plan PlacePlan, directories []placeDirectoryIntent) (*placeIntent, error) {
	ownershipRel, err := filepath.Rel(stateRoot, plan.Ownership.Path)
	if err != nil || filepath.IsAbs(ownershipRel) || strings.HasPrefix(ownershipRel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: ownership path escapes state root", ErrUnsafePath)
	}
	generationKeys, err := placeGenerationKeys(plan.Ownership.Bytes)
	if err != nil {
		return nil, err
	}
	intent := &placeIntent{
		Root: plan.Root, RootIdentity: plan.RootIdentity,
		Stage:                 filepath.ToSlash(filepath.Join("place-stage", id)),
		OwnershipPath:         filepath.ToSlash(ownershipRel),
		OwnershipExpected:     plan.Ownership.Expected,
		OwnershipExpectedMode: plan.Ownership.ExpectedMode,
		OwnershipMode:         plan.Ownership.Mode,
		OwnershipInputDigest:  Digest(plan.Ownership.Bytes),
		OwnershipInputSize:    int64(len(plan.Ownership.Bytes)),
		GenerationKeys:        generationKeys,
		Directories:           directories,
		Prunes:                append([]PlacePrune(nil), plan.Prunes...),
	}
	for i, write := range plan.Writes {
		intent.Writes = append(intent.Writes, placeWriteIntent{
			Path: write.Path, GenerationKey: write.GenerationKey, SourcePath: write.SourcePath,
			Expected: write.Expected, ExpectedMode: write.ExpectedMode, Digest: write.Digest,
			Mode: write.Mode, Size: write.Size, Parent: clonePlaceIdentity(write.Parent),
			ParentCreated: write.ParentCreated, Link: write.Link,
			StageName: fmt.Sprintf("write-%06d", i),
		})
	}
	created := make(map[string]bool, len(directories))
	for i, write := range intent.Writes {
		parent := path.Dir(write.Path)
		if parent != "." {
			for _, directory := range directories {
				if placePathWithin(parent, directory.Path) && !created[directory.Path] {
					intent.Operations = append(intent.Operations, placeOperation{Kind: "directory", Index: indexPlaceDirectory(directories, directory.Path), Path: directory.Path})
					created[directory.Path] = true
				}
			}
		}
		intent.Operations = append(intent.Operations, placeOperation{Kind: "write", Index: i, Path: write.Path})
	}
	intent.Operations = append(intent.Operations, placeOperation{Kind: "ownership", Index: 0, Path: intent.OwnershipPath})
	for i, prune := range intent.Prunes {
		kind := "prune"
		intent.Operations = append(intent.Operations, placeOperation{Kind: kind, Index: i, Path: prune.Path})
	}
	return intent, nil
}

func placeGenerationKeys(data []byte) ([]string, error) {
	var ownership struct {
		PlacePackages []struct {
			GenerationKey string `json:"generation_key"`
		} `json:"place_packages"`
	}
	if err := json.Unmarshal(data, &ownership); err != nil {
		return nil, fmt.Errorf("%w: invalid place ownership projection: %v", ErrUnsafePath, err)
	}
	keys := make(map[string]struct{}, len(ownership.PlacePackages))
	for _, pkg := range ownership.PlacePackages {
		if !validPlaceKey(pkg.GenerationKey) {
			return nil, fmt.Errorf("%w: invalid place generation reference", ErrUnsafePath)
		}
		keys[pkg.GenerationKey] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func indexPlaceDirectory(directories []placeDirectoryIntent, value string) int {
	for i := range directories {
		if directories[i].Path == value {
			return i
		}
	}
	return -1
}

func checkPlaceJournalSize(j *journal) error {
	intent := *j.Place
	intent.Directories = append([]placeDirectoryIntent(nil), j.Place.Directories...)
	intent.Retained = make([]string, 0, len(intent.Prunes))
	for _, prune := range intent.Prunes {
		intent.Retained = append(intent.Retained, prune.Path)
	}
	intent.StageIdentity = placeSizingIdentity()
	for directory := range intent.Directories {
		intent.Directories[directory].Identity = placeSizingIdentity()
	}
	intent.OwnershipNextDigest = strings.Repeat("0", 64)
	intent.OwnershipNextSize = maxDependencyFileSize
	intent.Completed = len(intent.Operations)
	intent.Pending = nil
	// An omitted pending field is smaller than any marked one. Compare just
	// the encoded field values, then marshal the fully marked journal once.
	largest := 0
	for i := range intent.Operations {
		op := &intent.Operations[i]
		raw, err := json.Marshal(op)
		if err != nil {
			return err
		}
		if len(raw) > largest {
			largest = len(raw)
			intent.Pending = op
		}
	}
	copy := *j
	copy.Place = &intent
	_, err := marshalBoundedJournal(&copy, "fully marked place journal")
	return err
}

func placeSizingIdentity() *fsboundary.Identity {
	maxInt := int64(^uint64(0) >> 1)
	return &fsboundary.Identity{
		Device: ^uint64(0), Inode: ^uint64(0), Size: maxInt, ModTimeNS: maxInt,
		ChangeTimeNS: maxInt, Links: ^uint64(0), Mode: fs.FileMode(^uint32(0)), UID: ^uint32(0),
	}
}

func persistPlaceJournal(stateDir, path string, j *journal, syncDir func(string) error) error {
	if err := validateJournalAt(j, filepath.Dir(stateDir)); err != nil {
		return err
	}
	if err := checkPlaceJournalSize(j); err != nil {
		return err
	}
	persisted, err := persistJournal(stateDir, j, syncDir)
	if err != nil {
		return err
	}
	if persisted != path {
		return fmt.Errorf("%w: place journal path changed", ErrRecoveryBlocked)
	}
	return nil
}

func createPlaceStage(stateRoot string, intent *placeIntent) (*fsboundary.Dir, *fsboundary.Dir, error) {
	state, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return nil, nil, err
	}
	parent, err := state.EnsureRoot("place-stage")
	_ = state.Close()
	if err != nil {
		return nil, nil, err
	}
	if err := parent.Mkdir(path.Base(intent.Stage), placeStageMode); err != nil {
		_ = parent.Close()
		return nil, nil, err
	}
	if err := parent.Sync(); err != nil {
		_ = parent.Close()
		return nil, nil, err
	}
	stage, err := parent.OpenPath(path.Base(intent.Stage))
	if err != nil {
		_ = parent.Close()
		return nil, nil, err
	}
	return stage, parent, nil
}

func writePlaceStageFile(stage *fsboundary.Dir, name string, data []byte, mode fs.FileMode) error {
	file, err := stage.OpenPersistentFile(name, mode)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if name == placeOwnershipNextName {
		// Recheck the prefix on the descriptor we will write, not just at
		// recovery preflight. ReadFile leaves the offset after the prefix.
		prefix, identity, err := fsboundary.ReadFile(file, int64(len(data)))
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, prefix) || len(prefix) > 0 && identity.Mode.Perm() != mode {
			return fmt.Errorf("stage prefix changed at %s", name)
		}
		data = data[len(prefix):]
	} else {
		if err := file.Truncate(0); err != nil {
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func validatePlaceJournal(j *journal, stateRoot string) error {
	intent := j.Place
	if intent == nil || !filepath.IsAbs(intent.Root) || filepath.Clean(intent.Root) != intent.Root ||
		intent.RootIdentity.Inode == 0 || !intent.RootIdentity.Mode.IsDir() {
		return fmt.Errorf("%w: invalid place root binding", ErrRecoveryBlocked)
	}
	if err := validatePlaceRootStateOverlap(intent.Root, stateRoot); err != nil {
		return fmt.Errorf("%w: invalid place root and state overlap: %v", ErrRecoveryBlocked, err)
	}
	if intent.Stage != filepath.ToSlash(filepath.Join("place-stage", j.ID)) {
		return fmt.Errorf("%w: invalid place stage path %s", ErrRecoveryBlocked, intent.Stage)
	}
	if intent.StageIdentity != nil && (intent.StageIdentity.Inode == 0 || !intent.StageIdentity.Mode.IsDir()) {
		return fmt.Errorf("%w: invalid place stage identity", ErrRecoveryBlocked)
	}
	if err := validateGeneration(intent.OwnershipExpected); err != nil ||
		(!intent.OwnershipExpected.Exists && intent.OwnershipExpectedMode != 0) ||
		intent.OwnershipExpectedMode != intent.OwnershipExpectedMode.Perm() ||
		intent.OwnershipMode == 0 || intent.OwnershipMode != intent.OwnershipMode.Perm() ||
		!validPlaceKey(intent.OwnershipInputDigest) || intent.OwnershipInputSize < 1 ||
		intent.OwnershipInputSize > maxDependencyFileSize {
		return fmt.Errorf("%w: invalid place ownership precondition", ErrRecoveryBlocked)
	}
	wantOwnership := filepath.ToSlash(filepath.Join("ownership", rootkey.ForCanonicalRoot(intent.Root), "state.json"))
	if intent.OwnershipPath != wantOwnership {
		return fmt.Errorf("%w: unsafe place ownership path %s", ErrRecoveryBlocked, intent.OwnershipPath)
	}
	previousKey := ""
	for _, key := range intent.GenerationKeys {
		if !validPlaceKey(key) || key <= previousKey {
			return fmt.Errorf("%w: invalid or unordered place generation keys", ErrRecoveryBlocked)
		}
		previousKey = key
	}
	for i, write := range intent.Writes {
		if validatePlaceRelative(write.Path) != nil || !validPlaceKey(write.GenerationKey) ||
			validatePlaceRelative(write.SourcePath) != nil || validateGeneration(write.Expected) != nil ||
			(!write.Expected.Exists && write.ExpectedMode != 0) ||
			write.ExpectedMode != write.ExpectedMode.Perm() || !validPlaceKey(write.Digest) ||
			write.Size < 0 || write.Size > maxDependencyFileSize ||
			write.Mode == 0 || write.Mode != write.Mode.Perm() ||
			write.Parent != nil && (write.Parent.Inode == 0 || !write.Parent.Mode.IsDir() || write.Link != nil) ||
			write.ParentCreated && (write.Parent != nil || write.Link != nil ||
				write.Expected.Exists || write.ExpectedMode != 0 ||
				!placeIntentHasDirectory(intent.Directories, placeParentPath(write.Path))) ||
			write.StageName != fmt.Sprintf("write-%06d", i) {
			return fmt.Errorf("%w: invalid or unordered place write %s", ErrRecoveryBlocked, write.Path)
		}
	}
	seenDirectories := make(map[string]struct{}, len(intent.Directories))
	previousDepth := 0
	for _, directory := range intent.Directories {
		if validatePlaceRelative(directory.Path) != nil || placeDepth(directory.Path) < previousDepth {
			return fmt.Errorf("%w: invalid or unordered place directory %s", ErrRecoveryBlocked, directory.Path)
		}
		if _, ok := seenDirectories[directory.Path]; ok {
			return fmt.Errorf("%w: duplicate place directory %s", ErrRecoveryBlocked, directory.Path)
		}
		seenDirectories[directory.Path] = struct{}{}
		previousDepth = placeDepth(directory.Path)
		used := false
		for _, write := range intent.Writes {
			if placePathWithin(path.Dir(write.Path), directory.Path) {
				used = true
				break
			}
		}
		if !used {
			return fmt.Errorf("%w: unreferenced place directory %s", ErrRecoveryBlocked, directory.Path)
		}
		if directory.Identity != nil && (directory.Identity.Inode == 0 || !directory.Identity.Mode.IsDir()) {
			return fmt.Errorf("%w: invalid recorded place directory %s", ErrRecoveryBlocked, directory.Path)
		}
	}
	for _, prune := range intent.Prunes {
		if validatePlaceRelative(prune.Path) != nil || prune.Parent.Inode == 0 || !prune.Parent.Mode.IsDir() {
			return fmt.Errorf("%w: invalid place prune %s", ErrRecoveryBlocked, prune.Path)
		}
		if prune.Directory == nil {
			if !prune.Expected.Exists || validateGeneration(prune.Expected) != nil ||
				prune.Mode == 0 || prune.Mode != prune.Mode.Perm() {
				return fmt.Errorf("%w: invalid place file prune %s", ErrRecoveryBlocked, prune.Path)
			}
		} else if !prune.Directory.Mode.IsDir() || prune.Directory.Inode == 0 || prune.Expected.Exists {
			return fmt.Errorf("%w: invalid place directory prune %s", ErrRecoveryBlocked, prune.Path)
		}
	}
	wantOps := expectedPlaceOperations(intent)
	if len(wantOps) != len(intent.Operations) {
		return fmt.Errorf("%w: invalid place operation list", ErrRecoveryBlocked)
	}
	for i := range wantOps {
		if wantOps[i] != intent.Operations[i] {
			return fmt.Errorf("%w: unordered place operation at %d", ErrRecoveryBlocked, i)
		}
	}
	if intent.Completed < 0 || intent.Completed > len(intent.Operations) {
		return fmt.Errorf("%w: invalid place operation prefix", ErrRecoveryBlocked)
	}
	if intent.Pending != nil {
		if intent.Completed == len(intent.Operations) || *intent.Pending != intent.Operations[intent.Completed] {
			return fmt.Errorf("%w: invalid marked place operation", ErrRecoveryBlocked)
		}
	}
	if intent.OwnershipNextDigest == "" && intent.OwnershipNextSize != 0 ||
		intent.OwnershipNextDigest != "" && (!validPlaceKey(intent.OwnershipNextDigest) ||
			intent.OwnershipNextSize < 1 || intent.OwnershipNextSize > maxDependencyFileSize) {
		return fmt.Errorf("%w: invalid next ownership digest", ErrRecoveryBlocked)
	}
	for _, retained := range intent.Retained {
		if validatePlaceRelative(retained) != nil {
			return fmt.Errorf("%w: invalid retained place path %s", ErrRecoveryBlocked, retained)
		}
	}
	return nil
}

func expectedPlaceOperations(intent *placeIntent) []placeOperation {
	operations := make([]placeOperation, 0, len(intent.Writes)+len(intent.Directories)+len(intent.Prunes)+1)
	created := make(map[string]bool, len(intent.Directories))
	for i, write := range intent.Writes {
		parent := path.Dir(write.Path)
		if parent != "." {
			for directoryIndex, directory := range intent.Directories {
				if placePathWithin(parent, directory.Path) && !created[directory.Path] {
					operations = append(operations, placeOperation{Kind: "directory", Index: directoryIndex, Path: directory.Path})
					created[directory.Path] = true
				}
			}
		}
		operations = append(operations, placeOperation{Kind: "write", Index: i, Path: write.Path})
	}
	operations = append(operations, placeOperation{Kind: "ownership", Path: intent.OwnershipPath})
	for i, prune := range intent.Prunes {
		operations = append(operations, placeOperation{Kind: "prune", Index: i, Path: prune.Path})
	}
	return operations
}

func placePendingPath(intent *placeIntent) string {
	if intent.Pending != nil {
		return intent.Pending.Path
	}
	if intent.Completed >= 0 && intent.Completed < len(intent.Operations) {
		return intent.Operations[intent.Completed].Path
	}
	return intent.Stage
}

func placeStop(kind error, journalPath, targetPath string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: journal %s path %s", kind, journalPath, targetPath)
	}
	return fmt.Errorf("%w: journal %s path %s: %w", kind, journalPath, targetPath, cause)
}

func recoverPlaceJournal(ctx context.Context, journalPath, stateDir string, j journal, cfg options) error {
	intent := j.Place
	stateRoot := filepath.Dir(stateDir)
	paths := make([]string, 0, len(intent.Writes)+len(intent.Prunes)+1)
	for _, write := range intent.Writes {
		paths = append(paths, filepath.Join(intent.Root, filepath.FromSlash(write.Path)))
	}
	for _, prune := range intent.Prunes {
		paths = append(paths, filepath.Join(intent.Root, filepath.FromSlash(prune.Path)))
	}
	paths = append(paths, filepath.Join(stateRoot, filepath.FromSlash(intent.OwnershipPath)))
	sort.Strings(paths)
	locks, err := acquireLocks(ctx, paths)
	if err != nil {
		return err
	}
	defer releaseLocks(locks)

	j, err = readJournal(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	intent = j.Place
	root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
	if err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Root, err)
	}
	_ = root.Close()
	if err := validatePlaceStageForRecovery(stateRoot, journalPath, intent); err != nil {
		return err
	}
	if intent.StageIdentity == nil {
		if intent.Completed == 0 && intent.Pending == nil {
			return finalizeJournal(journalPath, stateDir, cfg.syncDir)
		}
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, errors.New("stage identity was not recorded"))
	}
	stage, err := openPlaceStage(stateRoot, intent)
	if errors.Is(err, fs.ErrNotExist) && intent.Completed == len(intent.Operations) && intent.Pending == nil {
		if err := verifyPlaceCompletedPrefix(stateRoot, journalPath, intent); err != nil {
			return err
		}
		return finalizeJournal(journalPath, stateDir, cfg.syncDir)
	}
	if errors.Is(err, fs.ErrNotExist) && intent.Completed == 0 {
		if intent.Pending == nil {
			return finalizeJournal(journalPath, stateDir, cfg.syncDir)
		}
		unapplied, checkErr := initialPlaceOperationUnapplied(intent, stateRoot, journalPath)
		if checkErr != nil {
			return checkErr
		}
		if unapplied {
			return finalizeJournal(journalPath, stateDir, cfg.syncDir)
		}
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
		_ = stage.Close()
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	_ = stage.Close()

	if intent.Completed == 0 && intent.Pending == nil {
		return clearPlaceBeforeWrites(stateDir, journalPath, stateRoot, &j, cfg)
	}
	if intent.Completed == 0 && intent.Pending != nil {
		unapplied, err := initialPlaceOperationUnapplied(intent, stateRoot, journalPath)
		if err != nil {
			return err
		}
		if unapplied {
			return clearPlaceBeforeWrites(stateDir, journalPath, stateRoot, &j, cfg)
		}
	}
	if err := verifyPlaceCompletedPrefix(stateRoot, journalPath, intent); err != nil {
		return err
	}
	_, err = executePlaceOperations(ctx, stateDir, stateRoot, journalPath, &j, cfg, true)
	return err
}

func verifyPlaceCompletedPrefix(stateRoot, journalPath string, intent *placeIntent) error {
	root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
	if err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Root, err)
	}
	defer func() { _ = root.Close() }()
	prunedDirectories := make(map[string]struct{})
	markPrunedDirectory := func(op placeOperation) {
		if op.Kind != "prune" || op.Index < 0 || op.Index >= len(intent.Prunes) {
			return
		}
		prune := intent.Prunes[op.Index]
		if prune.Directory != nil && !placePathRetained(intent.Retained, prune.Path) {
			prunedDirectories[prune.Path] = struct{}{}
		}
	}
	for i := 0; i < intent.Completed && i < len(intent.Operations); i++ {
		markPrunedDirectory(intent.Operations[i])
	}
	if intent.Pending != nil {
		markPrunedDirectory(*intent.Pending)
	}
	for i := 0; i < intent.Completed; i++ {
		op := intent.Operations[i]
		switch op.Kind {
		case "directory":
			record := intent.Directories[op.Index]
			directory, err := root.OpenPath(op.Path)
			if err != nil {
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
			}
			same := record.Identity != nil && directory.Identity().SameObject(*record.Identity)
			_ = directory.Close()
			if !same {
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("recorded directory identity changed"))
			}
		case "write":
			write := intent.Writes[op.Index]
			generation, mode, err := inspectPlaceWriteLeaf(root, write, intent.Directories)
			if err != nil || !generation.Exists || generation.Digest != write.Digest || mode != write.Mode {
				if err == nil {
					err = errors.New("published generation changed")
				}
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
			}
		case "ownership":
			if intent.OwnershipNextDigest == "" {
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("ownership generation was not recorded"))
			}
			generation, mode, err := readPlaceOwnership(stateRoot, intent.OwnershipPath)
			if err != nil || !generation.Exists || generation.Digest != intent.OwnershipNextDigest || mode != intent.OwnershipMode {
				if err == nil {
					err = errors.New("committed ownership generation changed")
				}
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
			}
		case "prune":
			prune := intent.Prunes[op.Index]
			if placePathRetained(intent.Retained, prune.Path) {
				continue
			}
			absent, err := placePruneAbsent(intent.Root, intent.RootIdentity, prune)
			// A later marked directory prune can remove the completed file prune's parent.
			if errors.Is(err, fs.ErrNotExist) && prune.Directory == nil {
				for parent := path.Dir(prune.Path); parent != "."; parent = path.Dir(parent) {
					if _, marked := prunedDirectories[parent]; !marked {
						continue
					}
					parentDir, parentErr := root.OpenPath(filepath.FromSlash(parent))
					if errors.Is(parentErr, fs.ErrNotExist) {
						absent, err = true, nil
						break
					}
					if parentErr != nil {
						err = parentErr
						break
					}
					if closeErr := parentDir.Close(); closeErr != nil {
						err = closeErr
						break
					}
				}
			}
			if err != nil || !absent {
				if err == nil {
					err = errors.New("completed prune is present again")
				}
				return placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
			}
		default:
			return placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unknown completed place operation"))
		}
	}
	return nil
}

func placePathRetained(paths []string, value string) bool {
	for _, path := range paths {
		if path == value {
			return true
		}
	}
	return false
}

func placeJournalProblem(stateRoot, journalPath string, intent *placeIntent) (string, error) {
	if intent.StageIdentity == nil {
		stagePath := filepath.Join(stateRoot, filepath.FromSlash(intent.Stage))
		if _, err := os.Lstat(stagePath); err == nil {
			return intent.Stage, errors.New("unrecorded stage directory")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return intent.Stage, err
		}
	} else {
		stage, err := openPlaceStage(stateRoot, intent)
		if err != nil {
			return intent.Stage, err
		}
		if !stage.Identity().SameObject(*intent.StageIdentity) {
			_ = stage.Close()
			return intent.Stage, errors.New("stage identity changed")
		}
		if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
			_ = stage.Close()
			return intent.Stage, err
		}
		_ = stage.Close()
	}
	if err := verifyPlaceCompletedPrefix(stateRoot, journalPath, intent); err != nil {
		for i := 0; i < intent.Completed; i++ {
			if strings.Contains(err.Error(), "path "+intent.Operations[i].Path) {
				return intent.Operations[i].Path, err
			}
		}
		return placePendingPath(intent), err
	}
	if intent.Pending == nil {
		return placePendingPath(intent), nil
	}
	op := *intent.Pending
	switch op.Kind {
	case "directory":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return intent.Root, err
		}
		defer func() { _ = root.Close() }()
		directory, err := root.OpenPath(op.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return op.Path, nil
		}
		if err != nil {
			return op.Path, err
		}
		defer func() { _ = directory.Close() }()
		identity := intent.Directories[op.Index].Identity
		if identity == nil || !directory.Identity().SameObject(*identity) {
			return op.Path, errors.New("directory is present without its recorded identity")
		}
	case "write":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return intent.Root, err
		}
		defer func() { _ = root.Close() }()
		write := intent.Writes[op.Index]
		generation, mode, err := inspectPlaceWriteLeaf(root, write, intent.Directories)
		if err != nil {
			return op.Path, err
		}
		if generation == write.Expected && (!generation.Exists || mode == write.ExpectedMode) ||
			generation.Exists && generation.Digest == write.Digest && mode == write.Mode {
			return op.Path, nil
		}
		return op.Path, errors.New("destination is neither old nor intended generation")
	case "ownership":
		generation, mode, err := readPlaceOwnership(stateRoot, intent.OwnershipPath)
		if err != nil {
			return op.Path, err
		}
		if generation == intent.OwnershipExpected && (!generation.Exists || mode == intent.OwnershipExpectedMode) ||
			generation.Exists && intent.OwnershipNextDigest != "" && generation.Digest == intent.OwnershipNextDigest && mode == intent.OwnershipMode {
			return op.Path, nil
		}
		return op.Path, errors.New("ownership is neither old nor intended generation")
	case "prune":
		prune := intent.Prunes[op.Index]
		absent, err := placePruneAbsent(intent.Root, intent.RootIdentity, prune)
		if err != nil {
			return op.Path, err
		}
		if absent {
			return op.Path, nil
		}
	}
	return op.Path, nil
}

func validatePlaceStageForRecovery(stateRoot, journalPath string, intent *placeIntent) error {
	stagePath := filepath.Join(stateRoot, filepath.FromSlash(intent.Stage))
	if intent.StageIdentity == nil {
		if _, err := os.Lstat(stagePath); err == nil {
			return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, errors.New("unrecorded stage directory"))
		} else if !errors.Is(err, fs.ErrNotExist) {
			return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
		}
		return nil
	}
	stage, err := openPlaceStage(stateRoot, intent)
	if errors.Is(err, fs.ErrNotExist) &&
		(intent.Completed == 0 || intent.Completed == len(intent.Operations) && intent.Pending == nil) {
		return nil
	}
	if err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	defer func() { _ = stage.Close() }()
	if !stage.Identity().SameObject(*intent.StageIdentity) {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, errors.New("stage identity changed"))
	}
	if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	return nil
}

func openPlaceStage(stateRoot string, intent *placeIntent) (*fsboundary.Dir, error) {
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.OpenPath(filepath.FromSlash(intent.Stage))
}

func validatePlaceStageContents(stage *fsboundary.Dir, stateRoot string, intent *placeIntent) error {
	known := make(map[string]placeStageFile, len(intent.Writes)+2)
	for _, write := range intent.Writes {
		known[write.StageName] = placeStageFile{
			Name: write.StageName, Digest: write.Digest, Size: write.Size, Mode: write.Mode,
			GenerationKey: write.GenerationKey, SourcePath: write.SourcePath,
		}
	}
	known[placeOwnershipInputName] = placeStageFile{
		Name: placeOwnershipInputName, Digest: intent.OwnershipInputDigest,
		Size: intent.OwnershipInputSize, Mode: placeFileMode,
	}
	if intent.OwnershipNextDigest != "" {
		known[placeOwnershipNextName] = placeStageFile{
			Name: placeOwnershipNextName, Digest: intent.OwnershipNextDigest,
			Size: intent.OwnershipNextSize, Mode: intent.OwnershipMode,
		}
	}
	entries, err := os.ReadDir(stage.Path())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		expected, ok := known[entry.Name()]
		if !ok || entry.IsDir() {
			return fmt.Errorf("unexpected stage entry %s", entry.Name())
		}
		data, identity, err := stage.ReadFile(entry.Name(), maxDependencyFileSize)
		if err != nil {
			return err
		}
		if identity.Mode.Perm() != expected.Mode {
			return fmt.Errorf("stage mode changed at %s", entry.Name())
		}
		switch {
		case int64(len(data)) == expected.Size && Digest(data) == expected.Digest:
		case int64(len(data)) < expected.Size && entry.Name() == placeOwnershipNextName &&
			intent.Pending != nil && intent.Pending.Kind == "ownership":
			source, err := expectedPlaceOwnership(stage, intent)
			if err != nil {
				return fmt.Errorf("verify %s: %w", entry.Name(), err)
			}
			if int64(len(source)) != expected.Size || Digest(source) != expected.Digest || !bytes.HasPrefix(source, data) {
				return fmt.Errorf("stage prefix changed at %s", entry.Name())
			}
		case int64(len(data)) < expected.Size && expected.GenerationKey != "":
			source, err := readPlaceGeneration(stateRoot, PlaceWrite{
				GenerationKey: expected.GenerationKey, SourcePath: expected.SourcePath,
				Digest: expected.Digest, Size: expected.Size, Mode: expected.Mode,
			})
			if err != nil || !bytes.HasPrefix(source, data) {
				return fmt.Errorf("stage prefix changed at %s", entry.Name())
			}
		default:
			return fmt.Errorf("stage content changed at %s", entry.Name())
		}
	}
	return nil
}

func initialPlaceOperationUnapplied(intent *placeIntent, stateRoot, journalPath string) (bool, error) {
	op := *intent.Pending
	switch op.Kind {
	case "directory":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = root.Close() }()
		dir, err := root.OpenPath(op.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = dir.Close() }()
		identity := intent.Directories[op.Index].Identity
		if identity == nil || !dir.Identity().SameObject(*identity) {
			return false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("directory is present without its recorded identity"))
		}
		return false, nil
	case "write":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = root.Close() }()
		write := intent.Writes[op.Index]
		generation, mode, err := inspectPlaceWriteLeaf(root, write, intent.Directories)
		if err != nil {
			return false, placeStop(ErrRecoveryBlocked, journalPath, write.Path, err)
		}
		if generation.Exists && generation.Digest == write.Digest && mode == write.Mode {
			return false, nil
		}
		if generation == write.Expected && (!generation.Exists || mode == write.ExpectedMode) {
			return true, nil
		}
		return false, placeStop(ErrRecoveryBlocked, journalPath, write.Path, errors.New("destination is neither old nor intended generation"))
	case "ownership":
		current, mode, err := readPlaceOwnership(stateRoot, intent.OwnershipPath)
		if err != nil {
			return false, placeStop(ErrRecoveryBlocked, journalPath, intent.OwnershipPath, err)
		}
		if current.Exists && intent.OwnershipNextDigest != "" && current.Digest == intent.OwnershipNextDigest && mode == intent.OwnershipMode {
			return false, nil
		}
		if current == intent.OwnershipExpected && (!current.Exists || mode == intent.OwnershipExpectedMode) {
			return true, nil
		}
		return false, placeStop(ErrRecoveryBlocked, journalPath, intent.OwnershipPath, errors.New("ownership is neither old nor intended generation"))
	default:
		return false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unexpected initial operation"))
	}
}

func clearPlaceBeforeWrites(stateDir, journalPath, stateRoot string, j *journal, cfg options) error {
	intent := j.Place
	if intent.StageIdentity == nil {
		return finalizeJournal(journalPath, stateDir, cfg.syncDir)
	}
	stage, err := openPlaceStage(stateRoot, intent)
	if errors.Is(err, fs.ErrNotExist) {
		return finalizeJournal(journalPath, stateDir, cfg.syncDir)
	}
	if err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if !stage.Identity().SameObject(*intent.StageIdentity) {
		_ = stage.Close()
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, errors.New("stage identity changed"))
	}
	if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
		_ = stage.Close()
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err := removePlaceStageFiles(stage, stateRoot, intent); err != nil {
		_ = stage.Close()
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	_ = stage.Close()
	if err := removePlaceStageDir(stateRoot, intent); err != nil {
		return placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	return finalizeJournal(journalPath, stateDir, cfg.syncDir)
}

func executePlaceOperations(ctx context.Context, stateDir, stateRoot, journalPath string, j *journal, cfg options, recovering bool) (PlaceResult, error) {
	result := PlaceResult{}
	intent := j.Place
	stage, err := openPlaceStage(stateRoot, intent)
	if err != nil {
		return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	defer func() { _ = stage.Close() }()
	if intent.StageIdentity == nil || !stage.Identity().SameObject(*intent.StageIdentity) {
		return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, errors.New("stage identity changed"))
	}
	for intent.Completed < len(intent.Operations) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		op := intent.Operations[intent.Completed]
		resumeMarked := recovering && intent.Pending != nil
		if intent.Pending != nil && *intent.Pending != op {
			return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Pending.Path, errors.New("journal operation prefix changed"))
		}

		changed, retained, err := applyPlaceOperation(stateDir, stateRoot, journalPath, j, stage, op, cfg, recovering, resumeMarked)
		if err != nil {
			return result, err
		}
		if changed && (op.Kind == "write" || op.Kind == "prune") {
			result.Changed = append(result.Changed, op.Path)
		}
		if retained {
			intent.Retained = appendUniquePath(intent.Retained, op.Path)
			result.Retained = appendUniquePath(result.Retained, op.Path)
		}
		if intent.Pending == nil {
			intent.Completed++
			if err := persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir); err != nil {
				return result, err
			}
		}
		if op.Kind == "directory" {
			if err := inject(cfg, FaultPoint{Stage: FaultPlaceAfterDirectory, Index: op.Index, Path: op.Path}); err != nil {
				return result, err
			}
		}
		if op.Kind == "write" {
			if err := inject(cfg, FaultPoint{Stage: FaultPlaceAfterPublish, Index: op.Index, Path: op.Path}); err != nil {
				return result, err
			}
		}
		if op.Kind == "ownership" {
			if err := inject(cfg, FaultPoint{Stage: FaultPlaceAfterOwnership, Index: op.Index, Path: op.Path}); err != nil {
				return result, err
			}
		}
		if op.Kind == "prune" {
			if err := inject(cfg, FaultPoint{Stage: FaultPlaceAfterPrune, Index: op.Index, Path: op.Path}); err != nil {
				return result, err
			}
		}
		if intent.Pending != nil {
			intent.Completed++
			intent.Pending = nil
			if err := persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir); err != nil {
				return result, err
			}
		}
	}
	if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
		return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err := removePlaceStageFiles(stage, stateRoot, intent); err != nil {
		return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err := stage.Close(); err != nil {
		return result, err
	}
	if err := removePlaceStageDir(stateRoot, intent); err != nil {
		return result, placeStop(ErrRecoveryBlocked, journalPath, intent.Stage, err)
	}
	if err := finalizeJournal(journalPath, stateDir, cfg.syncDir); err != nil {
		return result, err
	}
	if len(intent.Retained) != 0 {
		slog.Info("place transaction retained edited paths", "paths", intent.Retained)
	}
	return result, nil
}

func appendUniquePath(paths []string, value string) []string {
	for _, existing := range paths {
		if existing == value {
			return paths
		}
	}
	return append(paths, value)
}

func applyPlaceOperation(stateDir, stateRoot, journalPath string, j *journal, stage *fsboundary.Dir, op placeOperation, cfg options, recovering, resumeMarked bool) (bool, bool, error) {
	intent := j.Place
	stopAfterParentSync := func(err error) error {
		if err == nil {
			return nil
		}
		return placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
	}
	mark := func() error {
		if intent.Pending != nil {
			return nil
		}
		intent.Pending = &op
		return persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir)
	}
	switch op.Kind {
	case "directory":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		defer func() { _ = root.Close() }()
		record := &intent.Directories[op.Index]
		dir, err := root.OpenPath(op.Path)
		if err == nil {
			defer func() { _ = dir.Close() }()
			if record.Identity == nil || !dir.Identity().SameObject(*record.Identity) {
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("directory is present without its recorded identity"))
			}
			return false, false, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if record.Identity != nil {
			if !resumeMarked || intent.Completed == 0 {
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("recorded directory is missing"))
			}
			record.Identity = nil
			if err := persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir); err != nil {
				return false, false, err
			}
		}
		parent, err := openPlaceParent(root, op.Path)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		checkRoot, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			_ = parent.Close()
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		_ = checkRoot.Close()
		if err := mark(); err != nil {
			_ = parent.Close()
			return false, false, err
		}
		if err := parent.Mkdir(path.Base(op.Path), 0o755); err != nil {
			_ = parent.Close()
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if err := parent.Sync(); err != nil {
			_ = parent.Close()
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		_ = parent.Close()
		created, err := root.OpenPath(op.Path)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		identity := created.Identity()
		_ = created.Close()
		record.Identity = &identity
		if err := persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir); err != nil {
			return false, false, err
		}
		return true, false, nil

	case "write":
		write := intent.Writes[op.Index]
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		defer func() { _ = root.Close() }()
		destination, err := fsboundary.CaptureDestination(root, write.Path, fsboundary.LinkPolicyRepository)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = destination.Close() }()
		expectedParent, err := placeWriteParentProof(write, intent.Directories)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if err := verifyPlaceWriteParent(destination, expectedParent); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if !samePlaceLink(destination.Link(), write.Link) {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("captured link binding changed"))
		}
		current, mode, err := readPlaceDestination(destination)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		oldGeneration := current == write.Expected && (!current.Exists || mode == write.ExpectedMode)
		intendedGeneration := current.Exists && current.Digest == write.Digest && mode == write.Mode
		if oldGeneration && intendedGeneration {
			return false, false, nil
		}
		if intendedGeneration {
			if resumeMarked {
				absent, err := placeStageFileAbsent(stage, write.StageName)
				if err != nil {
					return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
				}
				if absent {
					return false, false, stopAfterParentSync(syncPlaceDestinationParent(destination, cfg.syncPruneParent))
				}
			}
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("destination is neither old nor intended generation"))
		}
		if !oldGeneration {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("destination is neither old nor intended generation"))
		}
		if err := ensurePlaceStageWrite(stage, stateRoot, write); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		targetParent, err := destination.StagingDir()
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if targetParent.Identity().Device != stage.Identity().Device {
			_ = targetParent.Close()
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("cross-filesystem place publication refused"))
		}
		_ = targetParent.Close()
		if err := mark(); err != nil {
			return false, false, err
		}
		if err := inject(cfg, FaultPoint{Stage: FaultPlaceBeforePublish, Index: op.Index, Path: op.Path}); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		checkRoot, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		_ = checkRoot.Close()
		if err := destination.Publish(stage, write.StageName); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		return true, false, nil

	case "ownership":
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		_ = root.Close()
		if err := verifyPlaceCompletedPrefix(stateRoot, journalPath, intent); err != nil {
			return false, false, err
		}
		nextBytes, err := expectedPlaceOwnership(stage, intent)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		nextDigest := Digest(nextBytes)
		nextSize := int64(len(nextBytes))
		if intent.OwnershipNextDigest != "" &&
			(intent.OwnershipNextDigest != nextDigest || intent.OwnershipNextSize != nextSize) {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("ownership next generation changed"))
		}
		state, err := fsboundary.OpenDir(stateRoot)
		if err != nil {
			return false, false, err
		}
		parent, err := state.EnsurePath(filepath.ToSlash(path.Dir(intent.OwnershipPath)))
		if err != nil {
			_ = state.Close()
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		_ = parent.Close()
		destination, err := fsboundary.CaptureDestination(state, intent.OwnershipPath, fsboundary.LinkPolicyRepository)
		_ = state.Close()
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = destination.Close() }()
		if destination.Link() != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("ownership participant is a symbolic link"))
		}
		current, mode, err := readPlaceDestination(destination)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		oldGeneration := current == intent.OwnershipExpected &&
			(!current.Exists || mode == intent.OwnershipExpectedMode)
		intendedGeneration := current.Exists && current.Digest == nextDigest && mode == intent.OwnershipMode
		if oldGeneration && intendedGeneration {
			intent.OwnershipNextDigest = nextDigest
			intent.OwnershipNextSize = nextSize
			return false, false, nil
		}
		if intendedGeneration {
			if resumeMarked {
				absent, err := placeStageFileAbsent(stage, placeOwnershipNextName)
				if err != nil {
					return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
				}
				if absent {
					return false, false, stopAfterParentSync(syncPlaceDestinationParent(destination, cfg.syncPruneParent))
				}
			}
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("ownership is neither old nor intended generation"))
		}
		if !oldGeneration {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("ownership is neither old nor intended generation"))
		}
		if intent.OwnershipNextDigest == "" {
			intent.OwnershipNextDigest = nextDigest
			intent.OwnershipNextSize = nextSize
			if intent.Pending != nil {
				if err := persistPlaceJournal(stateDir, journalPath, j, cfg.syncDir); err != nil {
					return false, false, err
				}
			}
		}
		if err := mark(); err != nil {
			return false, false, err
		}
		if err := writePlaceStageFile(stage, placeOwnershipNextName, nextBytes, intent.OwnershipMode); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if err := stage.Sync(); err != nil {
			return false, false, err
		}
		checkRoot, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		_ = checkRoot.Close()
		if err := destination.Publish(stage, placeOwnershipNextName); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		return true, false, nil

	case "prune":
		prune := intent.Prunes[op.Index]
		root, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		defer func() { _ = root.Close() }()
		parent, err := openPlaceParent(root, prune.Path)
		if errors.Is(err, fs.ErrNotExist) {
			if resumeMarked {
				return false, false, nil
			}
			if recovering {
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unmarked prune parent is missing"))
			}
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("prune parent is missing"))
		}
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = parent.Close() }()
		if !parent.Identity().SameObject(prune.Parent) {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("prune parent identity changed"))
		}
		if prune.Directory != nil {
			directory, err := parent.OpenPath(path.Base(prune.Path))
			if errors.Is(err, fs.ErrNotExist) && resumeMarked {
				// Rmdir may have succeeded before its parent sync became durable.
				return false, false, cfg.syncPruneParent(parent)
			}
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && recovering {
					return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unmarked prune directory is absent"))
				}
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
			}
			defer func() { _ = directory.Close() }()
			if !directory.Identity().SameObject(*prune.Directory) {
				return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("prune directory identity changed"))
			}
			checkRoot, err := openPlaceRoot(intent.Root, intent.RootIdentity)
			if err != nil {
				return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
			}
			_ = checkRoot.Close()
			if err := mark(); err != nil {
				return false, false, err
			}
			removeErr := parent.Rmdir(path.Base(prune.Path))
			if errors.Is(removeErr, syscall.ENOTEMPTY) || errors.Is(removeErr, syscall.EEXIST) {
				return false, true, nil
			}
			if removeErr != nil {
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, removeErr)
			}
			if err := cfg.syncPruneParent(parent); err != nil {
				return false, false, err
			}
			return true, false, nil
		}

		destination, err := fsboundary.CaptureDestination(root, prune.Path, fsboundary.LinkPolicyRepository)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		defer func() { _ = destination.Close() }()
		if !samePlaceLink(destination.Link(), prune.Link) {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("prune link binding changed"))
		}
		generation, mode, err := readPlaceDestination(destination)
		if err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		if !generation.Exists {
			if resumeMarked {
				return false, false, stopAfterParentSync(cfg.syncPruneParent(parent))
			}
			if recovering {
				return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unmarked stale destination is absent"))
			}
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("stale file disappeared before prune mark"))
		}
		if generation != prune.Expected || mode != prune.Mode {
			return false, true, nil
		}
		targetParent, err := destination.StagingDir()
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		if destination.Link() == nil && !targetParent.Identity().SameObject(prune.Parent) {
			_ = targetParent.Close()
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, errors.New("prune destination parent identity changed"))
		}
		_ = targetParent.Close()
		checkRoot, err := openPlaceRoot(intent.Root, intent.RootIdentity)
		if err != nil {
			return false, false, placeStop(ErrGenerationConflict, journalPath, op.Path, err)
		}
		_ = checkRoot.Close()
		if err := mark(); err != nil {
			return false, false, err
		}
		if err := destination.RemoveLeaf(); err != nil {
			return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, err)
		}
		return true, false, nil
	}
	return false, false, placeStop(ErrRecoveryBlocked, journalPath, op.Path, errors.New("unknown place operation"))
}

func syncPlaceDestinationParent(destination *fsboundary.Destination, syncParent func(*fsboundary.Dir) error) error {
	parent, err := destination.StagingDir()
	if err != nil {
		return err
	}
	return errors.Join(syncParent(parent), parent.Close())
}

func placeStageFileAbsent(stage *fsboundary.Dir, name string) (bool, error) {
	_, _, err := stage.ReadFile(name, maxDependencyFileSize)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func ensurePlaceStageWrite(stage *fsboundary.Dir, stateRoot string, write placeWriteIntent) error {
	data, identity, err := stage.ReadFile(write.StageName, maxDependencyFileSize)
	if err == nil {
		if identity.Mode.Perm() != write.Mode {
			return errors.New("stage file mode does not match intended generation")
		}
		if int64(len(data)) == write.Size && Digest(data) == write.Digest {
			return nil
		}
		source, sourceErr := readPlaceGeneration(stateRoot, PlaceWrite{
			GenerationKey: write.GenerationKey, SourcePath: write.SourcePath, Digest: write.Digest,
			Size: write.Size, Mode: write.Mode,
		})
		if sourceErr != nil || int64(len(data)) >= write.Size || !bytes.HasPrefix(source, data) {
			return errors.New("stage file is not an exact generation prefix")
		}
		return writePlaceStageFile(stage, write.StageName, source, write.Mode)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	source, err := readPlaceGeneration(stateRoot, PlaceWrite{
		GenerationKey: write.GenerationKey, SourcePath: write.SourcePath, Digest: write.Digest,
		Size: write.Size, Mode: write.Mode,
	})
	if err != nil {
		return err
	}
	return writePlaceStageFile(stage, write.StageName, source, write.Mode)
}

func expectedPlaceOwnership(stage *fsboundary.Dir, intent *placeIntent) ([]byte, error) {
	raw, _, err := stage.ReadFile(placeOwnershipInputName, maxDependencyFileSize)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != intent.OwnershipInputSize || Digest(raw) != intent.OwnershipInputDigest {
		return nil, errors.New("ownership input stage changed")
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(raw, &record); err != nil || record == nil {
		return nil, errors.New("invalid staged ownership input")
	}
	var directories []PlaceDirectory
	if existing, ok := record["place_directories"]; ok {
		if err := json.Unmarshal(existing, &directories); err != nil {
			return nil, err
		}
	}
	byPath := make(map[string]int, len(directories)+len(intent.Directories))
	for i, directory := range directories {
		byPath[directory.Path] = i
	}
	for _, directory := range intent.Directories {
		if directory.Identity == nil {
			continue
		}
		next := PlaceDirectory{Path: directory.Path, Identity: *directory.Identity}
		if index, ok := byPath[directory.Path]; ok {
			directories[index] = next
		} else {
			byPath[directory.Path] = len(directories)
			directories = append(directories, next)
		}
	}
	encoded, err := json.Marshal(directories)
	if err != nil {
		return nil, err
	}
	record["place_directories"] = encoded
	next, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if int64(len(next)) > maxDependencyFileSize {
		return nil, fmt.Errorf("%w: ownership generation exceeds %d bytes", ErrRecoveryBlocked, maxDependencyFileSize)
	}
	return next, nil
}

func readPlaceOwnership(stateRoot, rel string) (Generation, fs.FileMode, error) {
	_, generation, mode, err := inspectPlaceOwnership(stateRoot, rel)
	return generation, mode, err
}

func inspectPlaceOwnership(stateRoot, rel string) ([]byte, Generation, fs.FileMode, error) {
	if err := validatePlaceRelative(rel); err != nil {
		return nil, Generation{}, 0, err
	}
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return nil, Generation{}, 0, err
	}
	defer func() { _ = root.Close() }()
	parent, err := root.OpenPath(path.Dir(rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Generation{}, 0, nil
	}
	if err != nil {
		return nil, Generation{}, 0, err
	}
	_ = parent.Close()
	destination, err := fsboundary.CaptureDestination(root, rel, fsboundary.LinkPolicyRepository)
	if err != nil {
		return nil, Generation{}, 0, err
	}
	defer func() { _ = destination.Close() }()
	if destination.Link() != nil {
		return nil, Generation{}, 0, fmt.Errorf("%w: ownership participant is a symbolic link", ErrUnsafePath)
	}
	if !destination.Exists() {
		return nil, Generation{}, 0, nil
	}
	data, identity, err := destination.ReadTarget(maxDependencyFileSize)
	if err != nil {
		return nil, Generation{}, 0, err
	}
	return data, Generation{Exists: true, Digest: Digest(data)}, identity.Mode.Perm(), nil
}

func placePruneAbsent(rootPath string, rootIdentity fsboundary.Identity, prune PlacePrune) (bool, error) {
	root, err := openPlaceRoot(rootPath, rootIdentity)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	if prune.Directory != nil {
		dir, err := root.OpenPath(prune.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		_ = dir.Close()
		return false, nil
	}
	parent, err := openPlaceParent(root, prune.Path)
	if err != nil {
		return false, err
	}
	defer func() { _ = parent.Close() }()
	dest, err := fsboundary.CaptureDestination(root, prune.Path, fsboundary.LinkPolicyRepository)
	if err != nil {
		return false, err
	}
	defer func() { _ = dest.Close() }()
	return !dest.Exists(), nil
}

func removePlaceStageFiles(stage *fsboundary.Dir, stateRoot string, intent *placeIntent) error {
	if err := validatePlaceStageContents(stage, stateRoot, intent); err != nil {
		return err
	}
	known := map[string]bool{placeOwnershipInputName: true}
	for _, write := range intent.Writes {
		known[write.StageName] = true
	}
	if intent.OwnershipNextDigest != "" {
		known[placeOwnershipNextName] = true
	}
	entries, err := os.ReadDir(stage.Path())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !known[entry.Name()] {
			return fmt.Errorf("unexpected stage directory %s", entry.Name())
		}
		if err := stage.Unlink(entry.Name()); err != nil {
			return err
		}
	}
	return stage.Sync()
}

func removePlaceStageDir(stateRoot string, intent *placeIntent) error {
	root, err := fsboundary.OpenDir(stateRoot)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	parent, err := root.OpenPath("place-stage")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	stage, err := parent.OpenPath(path.Base(intent.Stage))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if intent.StageIdentity == nil || !stage.Identity().SameObject(*intent.StageIdentity) {
		_ = stage.Close()
		return errors.New("stage identity changed before removal")
	}
	_ = stage.Close()
	if err := parent.Rmdir(path.Base(intent.Stage)); err != nil {
		return err
	}
	return parent.Sync()
}
