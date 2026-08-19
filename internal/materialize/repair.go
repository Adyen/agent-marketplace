package materialize

import (
	"context"

	"github.com/Adyen/agent-marketplace/internal/resolver"
	hcc "github.com/Adyen/agent-marketplace/pkg/harness-compilation"
)

// TargetRepairPreview pairs one ownership-reconciled target with its read-only
// repair preview.
type TargetRepairPreview struct {
	Target  hcc.Target
	Preview *RepairPreview
}

// RepairPreviews enumerates the ownership-reconciled targets in stable order.
func RepairPreviews(ctx context.Context, repoRoot string, resolved []resolver.ResolvedArtifact) ([]TargetRepairPreview, error) {
	providers := []struct {
		target  hcc.Target
		preview func(context.Context, string, []resolver.ResolvedArtifact) (*RepairPreview, error)
	}{
		{target: hcc.TargetDevin, preview: DevinRepairPreview},
		{target: hcc.TargetCodex, preview: CodexRepairPreview},
	}
	out := make([]TargetRepairPreview, 0, len(providers))
	for _, provider := range providers {
		preview, err := provider.preview(ctx, repoRoot, resolved)
		if err != nil {
			return nil, err
		}
		out = append(out, TargetRepairPreview{Target: provider.target, Preview: preview})
	}
	return out, nil
}
