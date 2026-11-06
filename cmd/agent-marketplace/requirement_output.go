package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Adyen/agent-marketplace/internal/pipeline"
	"github.com/Adyen/agent-marketplace/internal/resolver"
	"github.com/Adyen/agent-marketplace/internal/skill"
)

func requirementVia(via string, requiredBy []resolver.ResourceRef) string {
	if len(requiredBy) == 0 {
		return via
	}
	parts := make([]string, 0, len(requiredBy)+1)
	if via != "" {
		parts = append(parts, via)
	}
	for _, ref := range requiredBy {
		parts = append(parts, fmt.Sprintf("requires %s:%s", skill.KindLabel(ref.Kind), ref.Name))
	}
	return strings.Join(parts, ", ")
}

func requirementRequirers(requiredBy []resolver.ResourceRef) string {
	labels := make([]string, 0, len(requiredBy))
	for _, ref := range requiredBy {
		labels = append(labels, fmt.Sprintf("%s %q", skill.KindLabel(ref.Kind), terminalSafeText(ref.Name)))
	}
	return strings.Join(labels, ", ")
}

func writeInstallRequirementSummaries(out io.Writer, result *pipeline.InstallResult) error {
	for _, artifact := range result.AlsoSelected {
		if _, err := fmt.Fprintf(out, "also selected %s %q (required by %s)\n",
			skill.KindLabel(artifact.Kind), terminalSafeText(artifact.Name), requirementRequirers(artifact.RequiredBy)); err != nil {
			return err
		}
	}
	for _, artifact := range result.ImpliedPreview {
		if _, err := fmt.Fprintf(out, "would also select %s %q (required by %s)\n",
			skill.KindLabel(artifact.Kind), terminalSafeText(artifact.Name), requirementRequirers(artifact.RequiredBy)); err != nil {
			return err
		}
	}
	return nil
}
