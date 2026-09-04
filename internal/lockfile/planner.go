package lockfile

import (
	"fmt"
	"sort"
)

type Operation uint8

const (
	OperationSync Operation = iota
	OperationUpgrade
)

type Requirements struct {
	Scope   Scope
	Git     []GitRequirement
	Upgrade UpgradeSelection
}

type GitRequirement struct {
	URL, Ref string
	Roots    []GitRootRequirement
}

type GitRootRequirement struct{ Path, SourceHarness string }

type UpgradeSelection struct {
	All bool
	Git []GitUpgradeSelector
}

type GitUpgradeSelector struct{ URL, Ref, Path string }

func (s UpgradeSelection) SelectsGit(r GitRequirement) bool {
	if s.All || len(s.Git) == 0 {
		return true
	}
	for _, target := range s.Git {
		url, err := CanonicalGitURL(target.URL)
		if err != nil || url != r.URL || target.Ref != "" && target.Ref != r.Ref {
			continue
		}
		if target.Path == "" {
			return true
		}
		for _, root := range r.Roots {
			if root.Path == target.Path {
				return true
			}
		}
	}
	return false
}

type RecordKind string

const RecordGit RecordKind = "git"

type RecordRef struct {
	Kind     RecordKind
	Identity string
}

type AcquisitionRequest struct {
	Record RecordRef
	Git    *GitRequirement
}

type ResolutionPlan struct {
	Desired      Document
	Retained     []RecordRef
	Added        []RecordRef
	Invalidated  []RecordRef
	Upgraded     []RecordRef
	Removed      []RecordRef
	Acquisitions []AcquisitionRequest
}

func (p ResolutionPlan) Changed() bool {
	return len(p.Added)+len(p.Invalidated)+len(p.Upgraded)+len(p.Removed) > 0
}

func Plan(requirements Requirements, current *Document, operation Operation) (ResolutionPlan, error) {
	if !requirements.Scope.Valid() {
		return ResolutionPlan{}, coded(CodeInvalidScope, "invalid requirement scope %q", requirements.Scope)
	}
	if operation != OperationSync && operation != OperationUpgrade {
		return ResolutionPlan{}, coded(CodeInvalidDocument, "invalid lock operation")
	}
	plan := ResolutionPlan{Desired: Document{APIVersion: APIVersionV1, Scope: requirements.Scope}}
	old := Document{APIVersion: APIVersionV1, Scope: requirements.Scope}
	if current != nil {
		normalized, err := Normalize(*current)
		if err != nil {
			return ResolutionPlan{}, err
		}
		if err := ValidateScope(normalized, requirements.Scope); err != nil {
			return ResolutionPlan{}, err
		}
		old = normalized
	}

	gitRequirements, err := coalesceGitRequirements(requirements.Git)
	if err != nil {
		return ResolutionPlan{}, err
	}
	oldGit := make(map[string]GitRegistry, len(old.Registries))
	for _, registry := range old.Registries {
		oldGit[gitIdentity(registry.URL, registry.Ref)] = registry
	}
	wanted := make(map[string]bool, len(gitRequirements))
	for _, requirement := range gitRequirements {
		identity := gitIdentity(requirement.URL, requirement.Ref)
		wanted[identity] = true
		record := RecordRef{Kind: RecordGit, Identity: identity}
		locked, exists := oldGit[identity]
		compatible := exists && rootsCompatible(requirement.Roots, locked.Roots)
		upgrade := operation == OperationUpgrade && requirements.Upgrade.SelectsGit(requirement)
		switch {
		case upgrade && compatible:
			plan.Upgraded = append(plan.Upgraded, record)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: record, Git: &requirement})
		case compatible:
			plan.Retained = append(plan.Retained, record)
			for _, root := range locked.Roots {
				if !requiresRoot(requirement.Roots, root) {
					plan.Removed = append(plan.Removed, RecordRef{Kind: RecordGit, Identity: identity + "\x00root\x00" + root.Path + "\x00" + root.SourceHarness})
				}
			}
			locked.Roots = projectRoots(requirement.Roots, locked.Roots)
			plan.Desired.Registries = append(plan.Desired.Registries, locked)
		case exists:
			plan.Invalidated = append(plan.Invalidated, record)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: record, Git: &requirement})
		default:
			plan.Added = append(plan.Added, record)
			plan.Acquisitions = append(plan.Acquisitions, AcquisitionRequest{Record: record, Git: &requirement})
		}
	}
	for _, registry := range old.Registries {
		identity := gitIdentity(registry.URL, registry.Ref)
		if !wanted[identity] {
			plan.Removed = append(plan.Removed, RecordRef{Kind: RecordGit, Identity: identity})
		}
	}
	sortPlan(&plan)
	return plan, nil
}

func coalesceGitRequirements(input []GitRequirement) ([]GitRequirement, error) {
	byIdentity := make(map[string]GitRequirement, len(input))
	for _, raw := range input {
		requirement, err := normalizeGitRequirement(raw)
		if err != nil {
			return nil, err
		}
		identity := gitIdentity(requirement.URL, requirement.Ref)
		combined := byIdentity[identity]
		if combined.URL == "" {
			combined.URL, combined.Ref = requirement.URL, requirement.Ref
		}
		for _, root := range requirement.Roots {
			found := false
			for _, existing := range combined.Roots {
				if existing == root {
					found = true
					break
				}
			}
			if !found {
				combined.Roots = append(combined.Roots, root)
			}
		}
		byIdentity[identity] = combined
	}
	out := make([]GitRequirement, 0, len(byIdentity))
	for _, requirement := range byIdentity {
		sort.Slice(requirement.Roots, func(i, j int) bool {
			if requirement.Roots[i].Path != requirement.Roots[j].Path {
				return requirement.Roots[i].Path < requirement.Roots[j].Path
			}
			return requirement.Roots[i].SourceHarness < requirement.Roots[j].SourceHarness
		})
		out = append(out, requirement)
	}
	sort.Slice(out, func(i, j int) bool { return gitIdentity(out[i].URL, out[i].Ref) < gitIdentity(out[j].URL, out[j].Ref) })
	return out, nil
}

func normalizeGitRequirement(requirement GitRequirement) (GitRequirement, error) {
	var err error
	requirement.URL, err = CanonicalGitURL(requirement.URL)
	if err != nil {
		return requirement, err
	}
	if requirement.Ref == "" {
		return requirement, coded(CodeInvalidDocument, "Git requirement ref is required")
	}
	for i := range requirement.Roots {
		requirement.Roots[i].Path, err = normalizeArtifactPath(requirement.Roots[i].Path, true)
		if err != nil {
			return requirement, err
		}
	}
	sort.Slice(requirement.Roots, func(i, j int) bool {
		if requirement.Roots[i].Path != requirement.Roots[j].Path {
			return requirement.Roots[i].Path < requirement.Roots[j].Path
		}
		return requirement.Roots[i].SourceHarness < requirement.Roots[j].SourceHarness
	})
	return requirement, nil
}

func gitIdentity(url, ref string) string { return url + "\x00" + ref }

func gitRootMatches(want GitRootRequirement, got GitRoot) bool {
	return want.Path == got.Path && (want.SourceHarness == "" || want.SourceHarness == got.SourceHarness)
}

func rootsCompatible(required []GitRootRequirement, locked []GitRoot) bool {
	for _, want := range required {
		found := false
		for _, got := range locked {
			if gitRootMatches(want, got) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func requiresRoot(required []GitRootRequirement, locked GitRoot) bool {
	for _, want := range required {
		if gitRootMatches(want, locked) {
			return true
		}
	}
	return false
}

func projectRoots(required []GitRootRequirement, locked []GitRoot) []GitRoot {
	out := make([]GitRoot, 0, len(required))
	seen := make(map[string]bool, len(required))
	for _, want := range required {
		for _, got := range locked {
			if !gitRootMatches(want, got) {
				continue
			}
			key := got.Path + "\x00" + got.SourceHarness
			if !seen[key] {
				out = append(out, got)
				seen[key] = true
			}
			if want.SourceHarness != "" {
				break
			}
		}
	}
	return out
}

func sortPlan(plan *ResolutionPlan) {
	sortRecords := func(records []RecordRef) {
		sort.Slice(records, func(i, j int) bool {
			if records[i].Kind != records[j].Kind {
				return records[i].Kind < records[j].Kind
			}
			return records[i].Identity < records[j].Identity
		})
	}
	sortRecords(plan.Retained)
	sortRecords(plan.Added)
	sortRecords(plan.Invalidated)
	sortRecords(plan.Upgraded)
	sortRecords(plan.Removed)
	sort.Slice(plan.Acquisitions, func(i, j int) bool {
		return fmt.Sprint(plan.Acquisitions[i].Record) < fmt.Sprint(plan.Acquisitions[j].Record)
	})
	sort.Slice(plan.Desired.Registries, func(i, j int) bool {
		return gitIdentity(plan.Desired.Registries[i].URL, plan.Desired.Registries[i].Ref) < gitIdentity(plan.Desired.Registries[j].URL, plan.Desired.Registries[j].Ref)
	})
}
