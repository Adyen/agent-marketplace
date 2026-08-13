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

type warningGroup struct {
	warning   error
	resources []string
	seen      map[string]struct{}
}

func writeGroupedWarnings(out io.Writer, warnings []error, verbose bool) error {
	groups := make([]*warningGroup, 0, len(warnings))
	byMessage := make(map[string]*warningGroup, len(warnings))
	byDedupKey := make(map[string]*warningGroup, len(warnings))
	for _, warning := range warnings {
		message := warning.Error()
		group := byMessage[message]
		var dedup warningDedupKey
		dedupKey := ""
		if group == nil && errors.As(warning, &dedup) {
			dedupKey = dedup.DedupKey()
			if dedupKey != "" {
				group = byDedupKey[dedupKey]
			}
		}
		if group == nil {
			group = &warningGroup{warning: warning}
			groups = append(groups, group)
		}
		byMessage[message] = group
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
		if group.seen == nil {
			group.seen = make(map[string]struct{})
		}
		if _, exists := group.seen[resource]; exists {
			continue
		}
		group.seen[resource] = struct{}{}
		group.resources = append(group.resources, resource)
	}

	for _, group := range groups {
		if _, err := fmt.Fprintf(out, "warning: %v\n", group.warning); err != nil {
			return err
		}
		if len(group.resources) > 1 {
			resources := slices.Clone(group.resources)
			slices.Sort(resources)
			if !verbose && len(resources) > warningResourcePreview {
				resources = resources[:warningResourcePreview]
			}
			suffix := ""
			if remaining := len(group.resources) - len(resources); remaining > 0 {
				suffix = fmt.Sprintf(" (and %d more)", remaining)
			}
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
