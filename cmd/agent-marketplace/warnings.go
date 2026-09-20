package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/resolver"
)

const warningResourcePreview = 5

type warningResource interface {
	Resource() string
}

type warningDedupKey interface {
	DedupKey() string
}

type warningPresentation interface {
	PresentationKey() string
	PresentationText() string
	DiagnosticCode() string
	DiagnosticMessage() string
}

type warningSource interface {
	Source() string
}

type warningDetail struct {
	resource string
	source   string
}

type warningGroup struct {
	warning      error
	code         string
	message      string
	presentation string
	resources    []string
	details      []warningDetail
	seenResource map[string]struct{}
	seenDetail   map[string]struct{}
}

type syncIssueGroup struct {
	code     string
	problems []*warningGroup
}

type syncResolverIssue struct {
	err         error
	key         string
	text        string
	resource    string
	source      string
	remediation string
}

func (i *syncResolverIssue) Error() string             { return i.err.Error() }
func (i *syncResolverIssue) Unwrap() error             { return i.err }
func (i *syncResolverIssue) Resource() string          { return i.resource }
func (i *syncResolverIssue) Source() string            { return i.source }
func (i *syncResolverIssue) PresentationKey() string   { return i.key }
func (i *syncResolverIssue) PresentationText() string  { return i.text }
func (i *syncResolverIssue) DiagnosticCode() string    { return "" }
func (i *syncResolverIssue) DiagnosticMessage() string { return "" }
func (i *syncResolverIssue) Remediation() string       { return i.remediation }

func writeGroupedWarnings(out io.Writer, warnings []error, verbose bool) error {
	for _, group := range groupWarnings(warnings) {
		if _, err := fmt.Fprintf(out, "warning: %s\n", terminalSafeText(group.warning.Error())); err != nil {
			return err
		}
		if len(group.resources) > 1 {
			resources, suffix := warningResources(group, verbose)
			if _, err := fmt.Fprintf(out, "  → %d resources: %s%s\n", len(group.resources), terminalSafeText(strings.Join(resources, ", ")), suffix); err != nil {
				return err
			}
		}

		if remediable := warningRemediation(group.warning); remediable != nil {
			if _, err := fmt.Fprintf(out, "  → %s\n", terminalSafeText(remediable.Remediation())); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeSyncWarnings(out io.Writer, warnings []error, verbose bool, prefix string) error {
	return writeSyncIssues(out, warnings, verbose, prefix)
}

func presentSyncResolverIssues(issues []error) []error {
	selectedNotFound, nameCollisions := 0, 0
	for _, issue := range issues {
		switch typed := issue.(type) {
		case *resolver.SelectedNotFoundError:
			if typed.Via == "" && typed.Hint == "" && typed.RequestedPath == "" && len(typed.CandidatePaths) == 0 {
				selectedNotFound++
			}
		case *resolver.NameCollisionWarning, *resolver.NameCollisionError:
			nameCollisions++
		}
	}
	if selectedNotFound < 2 && nameCollisions < 2 {
		return issues
	}

	var presented []error
	for index, issue := range issues {
		replacement, changed := presentSyncResolverIssue(issue, selectedNotFound > 1, nameCollisions > 1)
		if changed && presented == nil {
			presented = append(make([]error, 0, len(issues)), issues[:index]...)
		}
		if presented != nil {
			if changed {
				presented = append(presented, replacement)
			} else {
				presented = append(presented, issue)
			}
		}
	}
	if presented == nil {
		return issues
	}
	return presented
}

func presentSyncResolverIssue(issue error, groupSelectedNotFound, groupNameCollisions bool) (error, bool) {
	switch typed := issue.(type) {
	case *resolver.SelectedNotFoundError:
		if !groupSelectedNotFound || typed.Via != "" || typed.Hint != "" || typed.RequestedPath != "" || len(typed.CandidatePaths) > 0 {
			return issue, false
		}
		return &syncResolverIssue{
			err: issue, key: "resolver.selected-not-found", text: "selected resources were not found", resource: typed.Resource(), source: typed.Source(),
			remediation: "run 'agent-marketplace status' to diagnose; remove stale selections with 'agent-marketplace remove <name> [--kind <kind>]'",
		}, true
	case *resolver.NameCollisionWarning:
		if !groupNameCollisions {
			return issue, false
		}
		return &syncResolverIssue{
			err: issue, key: "resolver.name-collision", text: "resources are provided by multiple registries", resource: typed.Resource(), source: typed.Source(),
			remediation: "to prefer a different source, exclude its current winner with 'agent-marketplace remove <name> --from <winning-registry>'",
		}, true
	case *resolver.NameCollisionError:
		if !groupNameCollisions {
			return issue, false
		}
		return &syncResolverIssue{
			err: issue, key: "resolver.name-collision", text: "resources are provided by multiple registries", resource: typed.Resource(), source: typed.Source(),
			remediation: "exclude each conflicting resource from one registry with 'agent-marketplace remove <name> --from <registry>'",
		}, true
	default:
		return issue, false
	}
}

func writeSyncIssues(out io.Writer, issues []error, verbose bool, prefix string) error {
	problems := groupWarnings(presentSyncResolverIssues(issues))
	if len(problems) == 1 {
		group := problems[0]
		text := group.warning.Error()
		if group.presentation != "" {
			text = group.presentation
		}
		text += warningGroupSuffix(group)
		if _, err := fmt.Fprintf(out, "%s %s\n", prefix, terminalSafeText(text)); err != nil {
			return err
		}
		if len(group.resources) > 0 && (group.presentation != "" || len(group.resources) > 1) {
			resources, suffix := warningResources(group, verbose)
			if _, err := fmt.Fprintf(out, "  %s%s\n", terminalSafeText(strings.Join(resources, ", ")), suffix); err != nil {
				return err
			}
		}
		if remediable := warningRemediation(group.warning); remediable != nil {
			_, err := fmt.Fprintf(out, "  → %s\n", terminalSafeText(remediable.Remediation()))
			return err
		}
		return nil
	}
	for _, group := range groupSyncIssues(problems) {
		if len(group.problems) == 1 {
			if err := writeSyncIssue(out, group.problems[0], verbose, prefix, "", true); err != nil {
				return err
			}
			continue
		}
		resources := syncIssueResourceCount(group)
		suffix := fmt.Sprintf(" (%d problems)", len(group.problems))
		if resources > 0 {
			suffix = fmt.Sprintf(" (%d problems, %d resources)", len(group.problems), resources)
		}
		if _, err := fmt.Fprintf(out, "%s %s%s\n", prefix, terminalSafeText(group.code), suffix); err != nil {
			return err
		}
		for _, problem := range group.problems {
			if err := writeSyncIssue(out, problem, verbose, "", "  ", false); err != nil {
				return err
			}
		}
		if err := writeSyncIssueRemediations(out, group); err != nil {
			return err
		}
	}
	return nil
}

func writeSyncIssue(out io.Writer, group *warningGroup, verbose bool, prefix, indent string, remediation bool) error {
	text := group.warning.Error()
	if group.presentation != "" {
		text = group.presentation
	}
	if group.message != "" && indent != "" {
		text = group.message
	}
	text += warningGroupSuffix(group)
	if indent == "" {
		if _, err := fmt.Fprintf(out, "%s %s\n", prefix, terminalSafeText(text)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(out, "%s%s\n", indent, terminalSafeText(text)); err != nil {
		return err
	}
	if len(group.resources) > 0 && (group.presentation != "" || len(group.resources) > 1) {
		resources, suffix := warningResources(group, verbose)
		if _, err := fmt.Fprintf(out, "%s  %s%s\n", indent, terminalSafeText(strings.Join(resources, ", ")), suffix); err != nil {
			return err
		}
	}
	if remediation {
		if remediable := warningRemediation(group.warning); remediable != nil {
			if _, err := fmt.Fprintf(out, "%s  → %s\n", indent, terminalSafeText(remediable.Remediation())); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeSyncIssueRemediations(out io.Writer, group *syncIssueGroup) error {
	seen := make(map[string]struct{}, len(group.problems))
	for _, problem := range group.problems {
		remediable := warningRemediation(problem.warning)
		if remediable == nil {
			continue
		}
		hint := remediable.Remediation()
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		if _, err := fmt.Fprintf(out, "  → %s\n", terminalSafeText(hint)); err != nil {
			return err
		}
	}
	return nil
}

func groupSyncIssues(problems []*warningGroup) []*syncIssueGroup {
	groups := make([]*syncIssueGroup, 0, len(problems))
	byCode := make(map[string]*syncIssueGroup, len(problems))
	for _, problem := range problems {
		if problem.code == "" {
			groups = append(groups, &syncIssueGroup{problems: []*warningGroup{problem}})
			continue
		}
		group := byCode[problem.code]
		if group == nil {
			group = &syncIssueGroup{code: problem.code}
			byCode[problem.code] = group
			groups = append(groups, group)
		}
		group.problems = append(group.problems, problem)
	}
	return groups
}

func syncIssueResourceCount(group *syncIssueGroup) int {
	resources := make(map[string]struct{})
	for _, problem := range group.problems {
		for _, resource := range problem.resources {
			resources[resource] = struct{}{}
		}
	}
	return len(resources)
}

type syncCommandError struct {
	issues []error
	text   string
}

func newSyncCommandError(issues []error, verbose bool, prefix string) *syncCommandError {
	var text strings.Builder
	_ = writeSyncIssues(&text, issues, verbose, prefix)
	return &syncCommandError{issues: slices.Clone(issues), text: strings.TrimSuffix(text.String(), "\n")}
}

func (e *syncCommandError) Error() string   { return e.text }
func (e *syncCommandError) Unwrap() []error { return e.issues }

func groupWarnings(warnings []error) []*warningGroup {
	groups := make([]*warningGroup, 0, len(warnings))
	byMessage := make(map[string]*warningGroup, len(warnings))
	byDedupKey := make(map[string]*warningGroup, len(warnings))
	byPresentationKey := make(map[string]*warningGroup, len(warnings))
	for _, warning := range warnings {
		if warning == nil {
			continue
		}
		message := warning.Error()
		group := byMessage[message]
		var presentation warningPresentation
		presentationKey := ""
		if errors.As(warning, &presentation) {
			presentationKey = presentation.PresentationKey()
			if presentationKey != "" {
				group = byPresentationKey[presentationKey]
			}
		}
		var dedup warningDedupKey
		dedupKey := ""
		if errors.As(warning, &dedup) {
			dedupKey = dedup.DedupKey()
			if group == nil && dedupKey != "" {
				group = byDedupKey[dedupKey]
			}
		}
		if group == nil {
			group = &warningGroup{warning: warning}
			if presentationKey != "" {
				group.presentation = presentation.PresentationText()
			}
			if presentation != nil {
				group.code = presentation.DiagnosticCode()
				group.message = presentation.DiagnosticMessage()
			}
			groups = append(groups, group)
		}
		if presentationKey == "" {
			byMessage[message] = group
		} else {
			byPresentationKey[presentationKey] = group
		}
		if dedup != nil && dedupKey != "" {
			byDedupKey[dedupKey] = group
		}
		var resourceWarning warningResource
		if !errors.As(warning, &resourceWarning) {
			continue
		}
		resource := strings.TrimSpace(resourceWarning.Resource())
		if resource == "" {
			continue
		}
		source := ""
		var sourceWarning warningSource
		if errors.As(warning, &sourceWarning) {
			source = sourceWarning.Source()
		}
		detailKey := resource + "\x00" + source
		if dedupKey != "" {
			detailKey = dedupKey
		}
		if group.seenDetail == nil {
			group.seenDetail = make(map[string]struct{})
		}
		if _, exists := group.seenDetail[detailKey]; !exists {
			group.seenDetail[detailKey] = struct{}{}
			group.details = append(group.details, warningDetail{resource: resource, source: source})
		}
		if group.seenResource == nil {
			group.seenResource = make(map[string]struct{})
		}
		if _, exists := group.seenResource[resource]; !exists {
			group.seenResource[resource] = struct{}{}
			group.resources = append(group.resources, resource)
		}
	}
	return groups
}

func warningGroupSuffix(group *warningGroup) string {
	if len(group.details) > len(group.resources) {
		if len(group.resources) == 1 {
			return fmt.Sprintf(" (%d occurrences)", len(group.details))
		}
		return fmt.Sprintf(" (%d occurrences across %d resources)", len(group.details), len(group.resources))
	}
	if len(group.resources) > 1 {
		return fmt.Sprintf(" (%d resources)", len(group.resources))
	}
	return ""
}

func warningResources(group *warningGroup, verbose bool) ([]string, string) {
	var resources []string
	if verbose || len(group.details) > len(group.resources) {
		details := slices.Clone(group.details)
		slices.SortFunc(details, func(a, b warningDetail) int {
			if byResource := strings.Compare(a.resource, b.resource); byResource != 0 {
				return byResource
			}
			return strings.Compare(a.source, b.source)
		})
		resources = make([]string, 0, len(details))
		for _, detail := range details {
			resource := detail.resource
			if detail.source != "" {
				resource = fmt.Sprintf("%s (%s)", resource, displayDiagnosticSource(detail.source))
			}
			resources = append(resources, resource)
		}
	} else {
		resources = slices.Clone(group.resources)
		slices.Sort(resources)
	}
	total := len(resources)
	if !verbose && len(resources) > warningResourcePreview {
		resources = resources[:warningResourcePreview]
	}
	suffix := ""
	if remaining := total - len(resources); remaining > 0 {
		suffix = fmt.Sprintf(" (and %d more)", remaining)
	}
	return resources, suffix
}

func displayDiagnosticSource(source string) string {
	const marker = "/exact-git/snapshots/"
	_, suffix, ok := strings.Cut(source, marker)
	if !ok {
		return source
	}
	_, relative, ok := strings.Cut(suffix, "/")
	if !ok || relative == "" {
		return source
	}
	return relative
}

func warningRemediation(warning error) resolver.Remediable {
	if remediable, ok := warning.(resolver.Remediable); ok {
		return remediable
	}
	if _, ok := warning.(interface{ Unwrap() error }); !ok {
		if _, ok := warning.(interface{ Unwrap() []error }); !ok {
			if _, ok := warning.(interface{ As(any) bool }); !ok {
				return nil
			}
		}
	}
	var remediable resolver.Remediable
	if errors.As(warning, &remediable) {
		return remediable
	}
	return nil
}
