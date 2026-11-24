package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/dependencytxn"
	"github.com/Adyen/agent-marketplace/internal/fsboundary"
	"github.com/Adyen/agent-marketplace/internal/ownership"
	"github.com/Adyen/agent-marketplace/internal/targetprofile"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/nameutil"
)

type preparedPlaceVerification struct {
	pkg             ownership.PlacePackage
	checkName       string
	profile         targetprofile.Profile
	scope           string
	argv            []string
	binding         targetprofile.ApprovalBinding
	snapshot        ownership.StateSnapshot
	owner           *ownership.Store
	rootIdentity    fsboundary.Identity
	subjectIdentity fsboundary.Identity
	stateDir        string
}

func preparePlaceVerification(
	ctx context.Context,
	binding targetprofile.ApprovalBinding,
	profileBytes []byte,
	env targetprofile.ApprovalEnvironment,
	kind, name string,
) (prepared preparedPlaceVerification, retErr error) {
	slog.Debug("place verification preflight started", "target_id", binding.Profile.TargetID, "kind", kind, "name", name)
	defer func() {
		slog.Debug("place verification preflight completed",
			"target_id", binding.Profile.TargetID, "kind", kind, "name", name, "error", retErr != nil,
		)
	}()
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	targetID, localKind, scoped := nameutil.SplitPlaceKind(kind)
	if !scoped || targetID != binding.Profile.TargetID || !nameutil.IsPlacePackageName(name) {
		return prepared, fmt.Errorf("target.place.verification-invalid: kind or package name is not in the approved target scope")
	}
	profile, err := targetprofile.Parse(profileBytes, binding.Profile)
	if err != nil {
		return prepared, fmt.Errorf("target %s profile validation: %w", binding.Profile.TargetID, err)
	}
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	if err := checkCurrentPlaceApproval(binding, profile, env); err != nil {
		return prepared, err
	}
	if err := binding.VerifyExecutable(); err != nil {
		return prepared, err
	}
	stateDir, err := dependencytxn.StateDir()
	if err != nil {
		return prepared, fmt.Errorf("resolve place verification state: %w", err)
	}
	stateRoot := filepath.Dir(stateDir)
	if stateRoot != binding.CanonicalStateRoot || stateRoot != env.CanonicalStateRoot {
		return prepared, fmt.Errorf("target.approval.state-root-mismatch: place verification state root differs from approved state root")
	}
	if err := dependencytxn.CheckPending(stateDir); err != nil {
		return prepared, err
	}
	root, err := openCanonicalPlaceSyncRoot(binding.CanonicalRoot)
	if err != nil {
		return prepared, fmt.Errorf("open approved place root: %w", err)
	}
	rootIdentity := root.Identity()
	defer func() { retErr = errors.Join(retErr, root.Close()) }()

	owner, err := ownership.Open(root.Path(), ownership.WithStateRoot(stateRoot))
	if err != nil {
		return prepared, fmt.Errorf("open place ownership state: %w", err)
	}
	snapshot, err := owner.SnapshotState()
	if err != nil {
		return prepared, fmt.Errorf("read place ownership state: %w", err)
	}
	projection, err := owner.PlaceFromSnapshot(snapshot)
	if err != nil {
		return prepared, fmt.Errorf("read place ownership projection: %w", err)
	}
	matches := make([]ownership.PlacePackage, 0, 1)
	for _, pkg := range projection.Packages {
		if pkg.Kind == kind && pkg.Name == name {
			matches = append(matches, pkg)
		}
	}
	if len(matches) == 0 {
		return prepared, fmt.Errorf("target.place.package-not-installed: target %s package %s/%s is not installed", targetID, kind, name)
	}
	if len(matches) != 1 {
		return prepared, fmt.Errorf("target.place.package-ambiguous: target %s package %s/%s has multiple installed records", targetID, kind, name)
	}
	pkg := matches[0]
	if pkg.Binding.ProfileIdentity != binding.Profile {
		return prepared, fmt.Errorf("target.place.profile-identity-mismatch: installed package does not use the selected profile identity")
	}
	kindProfile, ok := profile.Kinds[localKind]
	if !ok || kindProfile.Verify == "" {
		return prepared, fmt.Errorf("target.place.check-unavailable: target %s kind %s has no declared verification check", targetID, kind)
	}
	check, ok := profile.Checks[kindProfile.Verify]
	if !ok {
		return prepared, fmt.Errorf("target.place.check-unavailable: target %s kind %s references no verification check", targetID, kind)
	}
	subject := path.Join(check.Subject.Under, pkg.Name)
	if subject == "." || path.IsAbs(subject) || path.Clean(subject) != subject {
		return prepared, fmt.Errorf("target.place.check-subject-invalid: target %s package subject is not a clean relative path", targetID)
	}
	subjectOwned := false
	for _, file := range pkg.Files {
		if strings.HasPrefix(file.Path, subject+"/") {
			subjectOwned = true
			break
		}
	}
	if !subjectOwned {
		return prepared, fmt.Errorf("target.place.subject-unowned: target %s package has no owned files below its verification subject", targetID)
	}
	subjectDir, subjectOpenErr := root.OpenPath(filepath.FromSlash(subject))
	var subjectIdentity fsboundary.Identity
	if subjectOpenErr == nil {
		subjectIdentity = subjectDir.Identity()
		defer func() { retErr = errors.Join(retErr, subjectDir.Close()) }()
	}
	subjectPath := filepath.Clean(filepath.Join(root.Path(), filepath.FromSlash(subject)))
	subjectRelative, err := filepath.Rel(root.Path(), subjectPath)
	if err != nil || !filepath.IsAbs(subjectPath) || subjectRelative == "." || subjectRelative == ".." ||
		strings.HasPrefix(subjectRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(subjectRelative) {
		return prepared, fmt.Errorf("target.place.check-subject-invalid: target %s package subject is outside its approved root", targetID)
	}

	drift, err := placeOwnedFileDrift(ctx, root, pkg.Files)
	if err != nil {
		return prepared, err
	}
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	if len(drift) > 0 {
		return prepared, fmt.Errorf("target.place.drift: installed package files differ: %s", strings.Join(drift, ", "))
	}
	if subjectOpenErr != nil {
		return prepared, fmt.Errorf("target.place.drift: verification subject %s is missing or unsafe: %w", subject, subjectOpenErr)
	}

	argv := append([]string(nil), check.Argv...)
	subjectArguments := 0
	for i, arg := range argv {
		if arg == "{subject}" {
			argv[i] = subjectPath
			subjectArguments++
		}
	}
	if subjectArguments != 1 {
		return prepared, fmt.Errorf("target.place.check-invalid: target %s check %s has no exact subject argument", targetID, kindProfile.Verify)
	}
	if err := ctx.Err(); err != nil {
		return prepared, err
	}
	currentRoot, err := openCanonicalPlaceSyncRoot(binding.CanonicalRoot)
	if err != nil {
		return prepared, fmt.Errorf("reopen approved place root after verification preflight: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, currentRoot.Close()) }()
	if !rootIdentity.SameObject(currentRoot.Identity()) {
		return prepared, fmt.Errorf("target.approval.root-mismatch: approved target root changed during verification preflight")
	}
	currentSubject, err := currentRoot.OpenPath(filepath.FromSlash(subject))
	if err != nil {
		return prepared, fmt.Errorf("target.place.drift: verification subject %s changed or is unsafe", subject)
	}
	sameSubject := subjectIdentity.SameObject(currentSubject.Identity())
	if err := currentSubject.Close(); err != nil {
		return prepared, fmt.Errorf("close revalidated verification subject %s: %w", subject, err)
	}
	if !sameSubject {
		return prepared, fmt.Errorf("target.place.drift: verification subject %s changed during preflight", subject)
	}
	prepared = preparedPlaceVerification{
		pkg: pkg, checkName: kindProfile.Verify, profile: profile, scope: check.Scope,
		argv: argv, binding: binding, snapshot: snapshot, owner: owner, rootIdentity: rootIdentity,
		subjectIdentity: subjectIdentity, stateDir: stateDir,
	}
	return prepared, nil
}

func placeOwnedFileDrift(
	ctx context.Context,
	root *fsboundary.Dir,
	files []ownership.PlaceFile,
) ([]string, error) {
	files = append([]ownership.PlaceFile(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	var drift []string
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, identity, err := root.ReadFile(filepath.FromSlash(file.Path), dependencytxn.PlaceFileLimit)
		if err != nil || !identity.Mode.IsRegular() || identity.Mode.Perm() != file.Mode.Perm() ||
			ownership.Digest(data) != file.Digest {
			drift = append(drift, file.Path)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return drift, nil
}
