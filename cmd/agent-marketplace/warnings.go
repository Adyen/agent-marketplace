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
	presentation string
	resources    []string
	details      []warningDetail
	seenResource map[string]struct{}
	seenDetail   map[string]struct{}
}

func writeGroupedWarnings(out io.Writer, warnings []error, verbose bool) error {
	for _, group := range groupWarnings(warnings) {
		if _, err := fmt.Fprintf(out, "warning: %v\n", group.warning); err != nil {
			return err
		}
		if len(group.resources) > 1 {
			resources, suffix := warningResources(group, verbose)
			if _, err := fmt.Fprintf(out, "  → %d resources: %s%s\n", len(group.resources), strings.Join(resources, ", "), suffix); err != nil {
				return err
			}
		}

		if remediable := warningRemediation(group.warning); remediable != nil {
			if _, err := fmt.Fprintf(out, "  → %s\n", remediable.Remediation()); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeSyncWarnings(out io.Writer, warnings []error, verbose bool, prefix string) error {
	return writeSyncIssues(out, warnings, verbose, prefix)
}

func writeSyncIssues(out io.Writer, issues []error, verbose bool, prefix string) error {
	for _, group := range groupWarnings(issues) {
		text := group.warning.Error()
		if group.presentation != "" {
			text = group.presentation
		}
		text += warningGroupSuffix(group)
		if _, err := fmt.Fprintf(out, "%s %s\n", prefix, text); err != nil {
			return err
		}
		if len(group.resources) > 0 && (group.presentation != "" || len(group.resources) > 1) {
			resources, suffix := warningResources(group, verbose)
			if _, err := fmt.Fprintf(out, "  %s%s\n", strings.Join(resources, ", "), suffix); err != nil {
				return err
			}
		}
		if remediable := warningRemediation(group.warning); remediable != nil {
			if _, err := fmt.Fprintf(out, "  → %s\n", remediable.Remediation()); err != nil {
				return err
			}
		}
	}
	return nil
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
				resource = fmt.Sprintf("%s (%s)", resource, detail.source)
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
