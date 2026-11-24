package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/marketplace"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/packagegen"
	"github.com/Adyen/agent-marketplace/internal/place"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/urlutil"
)

type placeUpsert struct {
	owner       ownership.PlacePackage
	input       packagegen.PlaceInput
	generation  packagegen.Generation
	sourceFiles map[string]targetprofile.PackageFile
}

type preparedPlaceUpserts struct {
	profile  targetprofile.Profile
	stateDir string
	upserts  map[string]placeUpsert
	ceded    []string
	plan     dependencytxn.PlacePlan
	changed  bool
}

// CommitPlaceUpserts publishes approved locked place packages without pruning
// packages or files that are absent from the input set.
func CommitPlaceUpserts(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profileBytes []byte,
	environment targetprofile.ApprovalEnvironment,
	packages []marketplace.ResolvedPlacePackage,
) (result dependencytxn.PlaceResult, retErr error) {
	result, _, retErr = commitPlaceChanges(ctx, binding, profileBytes, environment, packages, false)
	return result, retErr
}

func commitPlaceChanges(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profileBytes []byte,
	environment targetprofile.ApprovalEnvironment,
	packages []marketplace.ResolvedPlacePackage,
	pruneDeselected bool,
	assertions ...dependencytxn.ReadAssertion,
) (result dependencytxn.PlaceResult, ceded []string, retErr error) {
	slog.Debug("place upsert transaction started", "target_id", binding.Profile.TargetID, "packages", len(packages))
	defer func() {
		if retErr != nil {
			slog.Warn("place upsert transaction failed", "target_id", binding.Profile.TargetID, "error", retErr)
		}
	}()
	prepared, err := preparePlaceChanges(ctx, binding, profileBytes, environment, packages, pruneDeselected)
	if err != nil {
		return dependencytxn.PlaceResult{}, nil, err
	}
	if !prepared.changed {
		return dependencytxn.PlaceResult{}, append([]string(nil), prepared.ceded...), nil
	}
	if len(assertions) > 0 {
		if !pruneDeselected {
			return dependencytxn.PlaceResult{}, nil, fmt.Errorf("place selection assertions require explicit deselection mode")
		}
		if err := dependencytxn.ValidateReadAssertions(assertions); err != nil {
			return dependencytxn.PlaceResult{}, nil, err
		}
	}
	err = packagegen.WithPackagesLock(ctx, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := checkCurrentPlaceApproval(binding, prepared.profile, environment); err != nil {
			return err
		}
		if err := dependencytxn.CheckPending(prepared.stateDir); err != nil {
			return err
		}
		ids := make([]string, 0, len(prepared.upserts))
		for id := range prepared.upserts {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			upsert := prepared.upserts[id]
			generation, err := packagegen.PublishPlaceUnderPackagesLock(upsert.input)
			if err != nil {
				return fmt.Errorf("publish place package generation %s/%s: %w", upsert.owner.Kind, upsert.owner.Name, err)
			}
			if generation != upsert.generation {
				return fmt.Errorf("place package generation changed during publication")
			}
		}
		var err error
		var commitOptions []dependencytxn.Option
		if len(assertions) > 0 {
			commitOptions = append(commitOptions, dependencytxn.WithReadAssertions(assertions))
		}
		result, err = dependencytxn.CommitPlace(ctx, prepared.stateDir, prepared.plan, commitOptions...)
		return err
	})
	if err != nil {
		return dependencytxn.PlaceResult{}, nil, err
	}
	cededSet := make(map[string]struct{}, len(prepared.ceded)+len(result.Retained))
	for _, path := range prepared.ceded {
		cededSet[path] = struct{}{}
	}
	for _, path := range result.Retained {
		cededSet[path] = struct{}{}
	}
	ceded = make([]string, 0, len(cededSet))
	for path := range cededSet {
		ceded = append(ceded, path)
	}
	sort.Strings(ceded)
	slog.Info("place upsert transaction committed", "target_id", binding.Profile.TargetID, "packages", len(prepared.upserts), "changed_paths", len(result.Changed))
	return result, ceded, nil
}

func preparePlaceUpserts(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profileBytes []byte,
	environment targetprofile.ApprovalEnvironment,
	packages []marketplace.ResolvedPlacePackage,
) (prepared preparedPlaceUpserts, retErr error) {
	return preparePlaceChanges(ctx, binding, profileBytes, environment, packages, false)
}

func preparePlaceChanges(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profileBytes []byte,
	environment targetprofile.ApprovalEnvironment,
	packages []marketplace.ResolvedPlacePackage,
	pruneDeselected bool,
) (prepared preparedPlaceUpserts, retErr error) {
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	profileBytes = append([]byte(nil), profileBytes...)
	profile, err := targetprofile.Parse(profileBytes, binding.Profile)
	if err != nil {
		return prepared, fmt.Errorf("target %s profile validation: %w", binding.Profile.TargetID, err)
	}
	if err := checkCurrentPlaceApproval(binding, profile, environment); err != nil {
		return prepared, err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return prepared, fmt.Errorf("resolve place transaction state: %w", err)
	}
	stateRoot := filepath.Dir(stateDir)
	if stateRoot != binding.CanonicalStateRoot {
		return prepared, fmt.Errorf("target.approval.state-root-mismatch: transaction state root %q differs from approved state root %q", stateRoot, binding.CanonicalStateRoot)
	}
	prepared.profile = profile
	prepared.stateDir = stateDir
	if len(packages) == 0 && !pruneDeselected {
		return prepared, nil
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return prepared, err
	}
	packages = cloneResolvedPlacePackages(packages)

	root, err := fsboundary.OpenDir(binding.CanonicalRoot)
	if err != nil {
		return prepared, fmt.Errorf("open approved place root: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	if root.Path() != binding.CanonicalRoot {
		return prepared, fmt.Errorf("target.approval.root-mismatch: approved root is no longer canonical")
	}
	ownerStore, err := ownership.Open(binding.CanonicalRoot, ownership.WithStateRoot(binding.CanonicalStateRoot))
	if err != nil {
		return prepared, fmt.Errorf("open place ownership state: %w", err)
	}
	if ownerStore.CanonicalRoot() != binding.CanonicalRoot {
		return prepared, fmt.Errorf("target.approval.root-mismatch: ownership root differs from approval")
	}
	snapshot, err := ownerStore.SnapshotState()
	if err != nil {
		return prepared, fmt.Errorf("read place ownership state: %w", err)
	}
	current, err := ownerStore.PlaceFromSnapshot(snapshot)
	if err != nil {
		return prepared, fmt.Errorf("read place ownership projection: %w", err)
	}
	current = normalizePlaceProjection(current)

	priorFiles := make(map[string]ownership.PlaceFile)
	for _, pkg := range current.Packages {
		for _, file := range pkg.Files {
			priorFiles[file.Path] = file
		}
	}
	upserts := make(map[string]placeUpsert, len(packages))
	for _, resolved := range packages {
		upsert, err := planPlaceUpsert(binding, profile, stateRoot, resolved)
		if err != nil {
			return prepared, err
		}
		if _, duplicate := upserts[upsert.owner.ID]; duplicate {
			return prepared, fmt.Errorf("duplicate place package upsert %q", upsert.owner.ID)
		}
		for i := range upsert.owner.Files {
			if prior, exists := priorFiles[upsert.owner.Files[i].Path]; exists && prior.Parent != nil {
				parent := *prior.Parent
				upsert.owner.Files[i].Parent = &parent
			}
		}
		upserts[upsert.owner.ID] = upsert
	}

	selectedIDs := make(map[string]struct{}, len(upserts))
	for id := range upserts {
		selectedIDs[id] = struct{}{}
	}
	deselection := placeRetirementPlan{}
	retained := current
	if pruneDeselected {
		deselection, err = planPlaceDeselection(ctx, root, current, binding.Profile, selectedIDs)
		if err != nil {
			return prepared, err
		}
		if len(deselection.Unproven) != 0 {
			return prepared, fmt.Errorf("target.place.retirement-unproven: cannot deselect files without parent proof: %v", deselection.Unproven)
		}
		retained = deselection.Projection
		currentByID := make(map[string]ownership.PlacePackage, len(current.Packages))
		for _, pkg := range current.Packages {
			currentByID[pkg.ID] = pkg
		}
		for id, upsert := range upserts {
			prior, exists := currentByID[id]
			if !exists {
				continue
			}
			desiredPaths := make(map[string]struct{}, len(upsert.owner.Files))
			for _, file := range upsert.owner.Files {
				desiredPaths[file.Path] = struct{}{}
			}
			for _, file := range prior.Files {
				if _, remains := desiredPaths[file.Path]; !remains {
					return prepared, fmt.Errorf("selected place package path removals are unsupported: %s", file.Path)
				}
			}
		}
	}

	ownerByID := make(map[string]ownership.PlacePackage, len(retained.Packages)+len(upserts))
	placeByID := make(map[string]place.Package, len(retained.Packages)+len(upserts))
	for _, pkg := range retained.Packages {
		ownerByID[pkg.ID] = pkg
		placeByID[pkg.ID] = projectionPlacePackage(pkg)
	}
	for id, upsert := range upserts {
		if prior, exists := ownerByID[id]; exists &&
			prior.Binding == upsert.owner.Binding && prior.GenerationKey == upsert.owner.GenerationKey &&
			prior.Verification != nil {
			verification := *prior.Verification
			upsert.owner.Verification = &verification
			upserts[id] = upsert
		}
		ownerByID[id] = upsert.owner
		placeByID[id] = projectionPlacePackage(upsert.owner)
	}
	desiredPackages := make([]place.Package, 0, len(placeByID))
	for _, pkg := range placeByID {
		desiredPackages = append(desiredPackages, pkg)
	}
	sort.Slice(desiredPackages, func(i, j int) bool { return desiredPackages[i].ID < desiredPackages[j].ID })

	var observed map[string]place.Observation
	if pruneDeselected {
		observed, err = observePlaceChanges(root, current, upserts, selectedIDs, binding.Profile, deselection)
	} else {
		observed, err = observePlaceProjection(root, current, desiredPackages)
	}
	if err != nil {
		return prepared, err
	}
	plan, err := place.BuildPlan(placeProjectionPackages(current), desiredPackages, observed)
	if err != nil {
		return prepared, fmt.Errorf("plan place upserts: %w", err)
	}
	if len(plan.Refused) != 0 {
		refusal := plan.Refused[0]
		return prepared, fmt.Errorf("place upsert refused at %s: %s", refusal.Path, refusal.Reason)
	}
	var prunes []dependencytxn.PlacePrune
	var ceded []string
	if pruneDeselected {
		pruneByPath := make(map[string]dependencytxn.PlacePrune, len(deselection.Prunes))
		for _, prune := range deselection.Prunes {
			pruneByPath[prune.Path] = prune
		}
		cededByPath := make(map[string]struct{}, len(deselection.Ceded))
		for _, filePath := range deselection.Ceded {
			cededByPath[filePath] = struct{}{}
		}
		for _, removal := range plan.Removes {
			prune, approved := pruneByPath[removal.File.Path]
			if !approved {
				return prepared, fmt.Errorf("selected place update attempted unsupported path removal %s", removal.File.Path)
			}
			prunes = append(prunes, prune)
			delete(pruneByPath, removal.File.Path)
		}
		for _, removal := range plan.Ceded {
			if _, approved := cededByPath[removal.File.Path]; !approved {
				return prepared, fmt.Errorf("place plan ceded an unapproved path %s", removal.File.Path)
			}
			ceded = append(ceded, removal.File.Path)
			delete(cededByPath, removal.File.Path)
		}
		if len(pruneByPath) != 0 || len(cededByPath) != 0 {
			return prepared, fmt.Errorf("combined place plan differs from proven deselection plan")
		}
		sort.Strings(ceded)
	} else if len(plan.Removes) != 0 || len(plan.Ceded) != 0 {
		return prepared, fmt.Errorf("place upsert plan contains unsupported removals")
	}
	writes, err := dependencyPlaceWrites(plan.Writes, upserts)
	if err != nil {
		return prepared, err
	}
	if err := preparePlaceWriteParentProofs(root, current, priorFiles, plan.Writes, upserts, writes); err != nil {
		return prepared, err
	}
	for id, upsert := range upserts {
		ownerByID[id] = upsert.owner
	}

	nextProjection := ownership.PlaceProjection{Directories: append([]dependencytxn.PlaceDirectory(nil), current.Directories...)}
	for _, pkg := range plan.Next {
		ownerPkg, ok := ownerByID[pkg.ID]
		if !ok {
			return prepared, fmt.Errorf("place upsert plan lost package ownership %q", pkg.ID)
		}
		nextProjection.Packages = append(nextProjection.Packages, ownerPkg)
	}
	nextProjection = normalizePlaceProjection(nextProjection)
	if len(plan.Writes) == 0 && reflect.DeepEqual(current, nextProjection) {
		slog.Debug("place upsert transaction is already current", "target_id", binding.Profile.TargetID, "packages", len(upserts))
		return prepared, nil
	}
	ownershipFile, err := ownerStore.PreparePlace(snapshot, nextProjection)
	if err != nil {
		return prepared, fmt.Errorf("prepare place ownership state: %w", err)
	}
	placePlan := dependencytxn.PlacePlan{
		Root: binding.CanonicalRoot, RootIdentity: root.Identity(),
		Directories: placeWriteDirectories(writes), Writes: writes, Prunes: prunes, Ownership: ownershipFile,
	}
	prepared.upserts = upserts
	prepared.ceded = ceded
	prepared.plan = placePlan
	prepared.changed = true
	return prepared, nil
}

func preparePlaceWriteParentProofs(
	root *fsboundary.Dir,
	current ownership.PlaceProjection,
	priorFiles map[string]ownership.PlaceFile,
	plannedWrites []place.Write,
	upserts map[string]placeUpsert,
	writes []dependencytxn.PlaceWrite,
) error {
	if len(plannedWrites) != len(writes) {
		return fmt.Errorf("place write parent proofs do not match planned writes")
	}
	newOwnerFiles := make(map[string]*ownership.PlaceFile)
	for id := range upserts {
		upsert := upserts[id]
		for i := range upsert.owner.Files {
			file := &upsert.owner.Files[i]
			if _, existed := priorFiles[file.Path]; existed {
				continue
			}
			if _, duplicate := newOwnerFiles[file.Path]; duplicate {
				return fmt.Errorf("multiple new place owners claim %s", file.Path)
			}
			newOwnerFiles[file.Path] = file
		}
		upserts[id] = upsert
	}
	directories := make(map[string]fsboundary.Identity, len(current.Directories))
	for _, directory := range current.Directories {
		directories[directory.Path] = directory.Identity
	}
	writeDirectories := placeWriteDirectories(writes)
	for i, planned := range plannedWrites {
		parentPath := path.Dir(planned.File.Path)
		prior, existed := priorFiles[planned.File.Path]
		var expected *fsboundary.Identity
		if existed && prior.Parent != nil {
			parent := *prior.Parent
			expected = &parent
		} else if identity, recorded := directories[parentPath]; existed && recorded {
			parent := identity
			expected = &parent
		}
		parent, err := root.OpenPath(filepath.FromSlash(parentPath))
		if errors.Is(err, fs.ErrNotExist) {
			if expected != nil {
				return fmt.Errorf("target.place.parent-mismatch: recorded place write parent is missing at %s", parentPath)
			}
			if !slices.Contains(writeDirectories, parentPath) ||
				writes[i].Expected.Exists || writes[i].ExpectedMode != 0 || writes[i].Link != nil {
				return fmt.Errorf("place write parent %s is missing without a valid creation plan", parentPath)
			}
			writes[i].ParentCreated = true
			continue
		}
		if err != nil {
			return fmt.Errorf("open place write parent %s: %w", parentPath, err)
		}
		actual := parent.Identity()
		closeErr := parent.Close()
		if closeErr != nil {
			return fmt.Errorf("close place write parent %s: %w", parentPath, closeErr)
		}
		if expected != nil && !actual.SameObject(*expected) {
			return fmt.Errorf("target.place.parent-mismatch: place write parent changed at %s", parentPath)
		}
		if existed {
			if expected != nil {
				parent := *expected
				writes[i].Parent = &parent
			}
			continue
		}
		ownerFile := newOwnerFiles[planned.File.Path]
		if ownerFile == nil {
			return fmt.Errorf("place write %s has no new ownership file", planned.File.Path)
		}
		parentProof := actual
		ownerFile.Parent = &parentProof
		writeProof := actual
		writes[i].Parent = &writeProof
	}
	return nil
}

func checkCurrentPlaceApproval(binding targetprofile.ApprovalBinding, profile targetprofile.Profile, environment targetprofile.ApprovalEnvironment) error {
	current, err := resolvePlaceApprovalBinding(profile, binding.Profile, environment)
	if err != nil {
		return fmt.Errorf("target %s approval binding: %w", binding.Profile.TargetID, err)
	}
	if err := binding.Match(current); err != nil {
		return err
	}
	if err := targetprofile.RequireApproval(current); err != nil {
		return fmt.Errorf("target %s approval: %w", binding.Profile.TargetID, err)
	}
	return nil
}

func cloneResolvedPlacePackages(packages []marketplace.ResolvedPlacePackage) []marketplace.ResolvedPlacePackage {
	cloned := make([]marketplace.ResolvedPlacePackage, len(packages))
	for i, pkg := range packages {
		pkg.Files = append([]targetprofile.PackageFile(nil), pkg.Files...)
		for j := range pkg.Files {
			pkg.Files[j].Bytes = append([]byte(nil), pkg.Files[j].Bytes...)
		}
		if pkg.Revision.Assessment.HostPackageIdentity != nil {
			identity := *pkg.Revision.Assessment.HostPackageIdentity
			pkg.Revision.Assessment.HostPackageIdentity = &identity
		}
		cloned[i] = pkg
	}
	return cloned
}

func planPlaceUpsert(
	binding targetprofile.ApprovalBinding,
	profile targetprofile.Profile,
	stateRoot string,
	resolved marketplace.ResolvedPlacePackage,
) (placeUpsert, error) {
	entry, pin, revision := resolved.Entry, resolved.Pin, resolved.Revision
	endpoint, err := urlutil.NormalizeEndpointURL(entry.URL)
	if err != nil || endpoint != entry.URL {
		return placeUpsert{}, fmt.Errorf("place package %s has a noncanonical Marketplace endpoint", pin.Name)
	}
	if err := nameutil.ValidateSegment(entry.InstanceID); err != nil {
		return placeUpsert{}, fmt.Errorf("place package has invalid Marketplace instance ID: %w", err)
	}
	if err := nameutil.ValidateSegment(pin.RegistryID); err != nil {
		return placeUpsert{}, fmt.Errorf("place package has invalid registry ID: %w", err)
	}
	targetID, _, scoped := nameutil.SplitPlaceKind(pin.Kind)
	if !scoped || targetID != binding.Profile.TargetID || !nameutil.IsPlacePackageName(pin.Name) {
		return placeUpsert{}, fmt.Errorf("place package pin has invalid scoped coordinate")
	}
	expected := store.ResourceCoordinate{
		Marketplace: store.MarketplaceID(entry.InstanceID), Registry: store.RegistryID(pin.RegistryID),
		Kind: pin.Kind, Name: pin.Name,
	}
	if revision.Coordinate != expected || revision.Assessment.Coordinate != expected {
		return placeUpsert{}, fmt.Errorf("place package %s coordinate differs from its pin", pin.Name)
	}
	if string(revision.ContentDigest) != pin.Digest || string(revision.ContentDigest) != pin.Revision ||
		string(revision.Assessment.ContentDigest) != pin.Digest {
		return placeUpsert{}, fmt.Errorf("place package %s content digest differs from its pin", pin.Name)
	}
	if !nameutil.IsCanonicalSHA256Digest(pin.LockArchiveDigest) ||
		string(revision.ArchiveDigest) != pin.LockArchiveDigest ||
		string(revision.Assessment.AssessmentDigest) != pin.LockAssessmentDigest {
		return placeUpsert{}, fmt.Errorf("place package %s archive or assessment digest differs from its lock", pin.Name)
	}
	if err := revision.Assessment.Validate(); err != nil {
		return placeUpsert{}, fmt.Errorf("place package %s assessment is invalid: %w", pin.Name, err)
	}
	if revision.Revocation != nil {
		return placeUpsert{}, fmt.Errorf("place package %s revision is revoked", pin.Name)
	}
	if revision.Assessment.Decision == store.AssessmentDecisionReject {
		return placeUpsert{}, fmt.Errorf("place package %s assessment is rejected", pin.Name)
	}
	if revision.Requires != "" && revision.Requires != "[]" {
		return placeUpsert{}, fmt.Errorf("place package %s has unsupported indexed prerequisites", pin.Name)
	}
	if revision.Assessment.HostPackageIdentity == nil ||
		*revision.Assessment.HostPackageIdentity != pin.LockHostPackageIdentity {
		return placeUpsert{}, fmt.Errorf("place package %s host package binding differs from its pin", pin.Name)
	}
	if err := pin.LockHostPackageIdentity.MatchProfile(binding.Profile); err != nil {
		return placeUpsert{}, fmt.Errorf("place package %s profile binding mismatch: %w", pin.Name, err)
	}
	if err := targetprofile.ValidatePackageDestinations(profile, pin.Kind, resolved.Files); err != nil {
		return placeUpsert{}, fmt.Errorf("place package %s destinations: %w", pin.Name, err)
	}
	for _, file := range resolved.Files {
		if int64(len(file.Bytes)) > dependencytxn.PlaceFileLimit {
			return placeUpsert{}, fmt.Errorf("place package %s exceeds the placement file limit at %s", pin.Name, file.DestinationPath)
		}
	}
	input := packagegen.PlaceInput{Binding: pin.LockHostPackageIdentity, Files: resolved.Files}
	generation, err := packagegen.PlanPlace(input)
	if err != nil {
		return placeUpsert{}, fmt.Errorf("plan place package generation %s/%s: %w", pin.Kind, pin.Name, err)
	}
	if filepath.Dir(generation.Path) != filepath.Join(stateRoot, "packages") {
		return placeUpsert{}, fmt.Errorf("place package generation is outside the approved state root")
	}
	id := dependencytxn.Digest([]byte(strings.Join([]string{
		"agent-marketplace/place-package/v1", endpoint, string(expected.Marketplace),
		string(expected.Registry), expected.Kind, expected.Name,
	}, "\x00")))
	owner := ownership.PlacePackage{
		ID: id, Kind: pin.Kind, Name: pin.Name, Binding: pin.LockHostPackageIdentity,
		GenerationKey: generation.Key,
	}
	sourceFiles := make(map[string]targetprofile.PackageFile, len(resolved.Files))
	for _, file := range resolved.Files {
		digest := ownership.Digest(file.Bytes)
		owner.Files = append(owner.Files, ownership.PlaceFile{Path: file.DestinationPath, Digest: digest, Mode: file.Mode})
		sourceFiles[file.DestinationPath] = file
	}
	sort.Slice(owner.Files, func(i, j int) bool { return owner.Files[i].Path < owner.Files[j].Path })
	return placeUpsert{
		owner: owner, input: input,
		generation: generation, sourceFiles: sourceFiles,
	}, nil
}

func projectionPlacePackage(pkg ownership.PlacePackage) place.Package {
	files := make([]place.File, 0, len(pkg.Files))
	for _, file := range pkg.Files {
		files = append(files, place.File{Path: file.Path, Digest: file.Digest, Mode: file.Mode})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return place.Package{
		ID: pkg.ID, Revision: pkg.Binding.PackageContentDigest,
		Files: files, Requires: append([]string(nil), pkg.Requires...),
	}
}

func placeProjectionPackages(projection ownership.PlaceProjection) []place.Package {
	packages := make([]place.Package, 0, len(projection.Packages))
	for _, pkg := range projection.Packages {
		packages = append(packages, projectionPlacePackage(pkg))
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ID < packages[j].ID })
	return packages
}

func normalizePlaceProjection(projection ownership.PlaceProjection) ownership.PlaceProjection {
	projection.Packages = append([]ownership.PlacePackage(nil), projection.Packages...)
	for i := range projection.Packages {
		projection.Packages[i].Files = append([]ownership.PlaceFile(nil), projection.Packages[i].Files...)
		projection.Packages[i].Requires = append([]string(nil), projection.Packages[i].Requires...)
		for j := range projection.Packages[i].Files {
			if parent := projection.Packages[i].Files[j].Parent; parent != nil {
				copy := *parent
				projection.Packages[i].Files[j].Parent = &copy
			}
		}
		sort.Slice(projection.Packages[i].Files, func(a, b int) bool {
			return projection.Packages[i].Files[a].Path < projection.Packages[i].Files[b].Path
		})
	}
	sort.Slice(projection.Packages, func(i, j int) bool { return projection.Packages[i].ID < projection.Packages[j].ID })
	projection.Directories = append([]dependencytxn.PlaceDirectory(nil), projection.Directories...)
	return projection
}

func observePlaceProjection(root *fsboundary.Dir, installed ownership.PlaceProjection, desired []place.Package) (map[string]place.Observation, error) {
	paths := make(map[string]struct{})
	for _, pkg := range placeProjectionPackages(installed) {
		for _, file := range pkg.Files {
			paths[file.Path] = struct{}{}
		}
	}
	for _, pkg := range desired {
		for _, file := range pkg.Files {
			paths[file.Path] = struct{}{}
		}
	}
	return observePlacePaths(root, paths)
}

func observePlaceChanges(
	root *fsboundary.Dir,
	installed ownership.PlaceProjection,
	upserts map[string]placeUpsert,
	selectedIDs map[string]struct{},
	profile targetprofile.Identity,
	deselection placeRetirementPlan,
) (map[string]place.Observation, error) {
	paths := make(map[string]struct{})
	frozen := make(map[string]ownership.PlaceFile)
	for _, pkg := range installed.Packages {
		_, selected := selectedIDs[pkg.ID]
		if selected {
			for _, file := range pkg.Files {
				paths[file.Path] = struct{}{}
			}
			continue
		}
		if pkg.Binding.ProfileIdentity == profile {
			continue
		}
		for _, file := range pkg.Files {
			frozen[file.Path] = file
		}
	}
	for _, upsert := range upserts {
		for _, file := range upsert.owner.Files {
			if _, reserved := frozen[file.Path]; !reserved {
				paths[file.Path] = struct{}{}
			}
		}
	}
	observed, err := observePlacePaths(root, paths)
	if err != nil {
		return nil, err
	}
	for filePath, file := range frozen {
		observed[filePath] = place.Observation{Mode: file.Mode, Digest: file.Digest}
	}
	for _, prune := range deselection.Prunes {
		observed[prune.Path] = place.Observation{
			Mode: prune.Mode, Digest: "sha256:" + prune.Expected.Digest,
		}
	}
	return observed, nil
}

func observePlacePaths(root *fsboundary.Dir, paths map[string]struct{}) (map[string]place.Observation, error) {
	ordered := make([]string, 0, len(paths))
	for filePath := range paths {
		ordered = append(ordered, filePath)
	}
	sort.Strings(ordered)
	observed := make(map[string]place.Observation, len(paths))
	for _, filePath := range ordered {
		data, identity, err := root.ReadFile(filepath.FromSlash(filePath), dependencytxn.PlaceFileLimit)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("observe place path %s: %w", filePath, err)
		}
		observed[filePath] = place.Observation{Mode: identity.Mode, Digest: ownership.Digest(data)}
	}
	return observed, nil
}

func dependencyPlaceWrites(writes []place.Write, upserts map[string]placeUpsert) ([]dependencytxn.PlaceWrite, error) {
	out := make([]dependencytxn.PlaceWrite, 0, len(writes))
	for _, write := range writes {
		upsert, ok := upserts[write.PackageID]
		if !ok {
			return nil, fmt.Errorf("place plan writes package without an upsert source: %s", write.PackageID)
		}
		source, ok := upsert.sourceFiles[write.File.Path]
		if !ok {
			return nil, fmt.Errorf("place plan writes path without package source: %s", write.File.Path)
		}
		expected := dependencytxn.Generation{}
		var expectedMode fs.FileMode
		if write.Prior != nil {
			expected = dependencytxn.Generation{
				Exists: true, Digest: strings.TrimPrefix(write.Prior.Digest, "sha256:"),
			}
			expectedMode = write.Prior.Mode
		}
		out = append(out, dependencytxn.PlaceWrite{
			Path: write.File.Path, GenerationKey: upsert.owner.GenerationKey,
			SourcePath: source.SourcePath, Expected: expected, ExpectedMode: expectedMode,
			Digest: strings.TrimPrefix(write.File.Digest, "sha256:"),
			Mode:   write.File.Mode, Size: int64(len(source.Bytes)),
		})
	}
	return out, nil
}

func placeWriteDirectories(writes []dependencytxn.PlaceWrite) []string {
	directories := make(map[string]struct{})
	for _, write := range writes {
		parent := path.Dir(write.Path)
		for parent != "." {
			directories[parent] = struct{}{}
			parent = path.Dir(parent)
		}
	}
	ordered := make([]string, 0, len(directories))
	for directory := range directories {
		ordered = append(ordered, directory)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := strings.Count(ordered[i], "/"), strings.Count(ordered[j], "/")
		if left != right {
			return left < right
		}
		return ordered[i] < ordered[j]
	})
	return ordered
}
