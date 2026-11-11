package main

import (
	"context"
	"testing"

	"github.com/Adyen/agent-marketplace/internal/lockfile"
)

func TestPluginPlanSourceObservesDocsAfterInvalidation(t *testing.T) {
	t.Parallel()
	calls := 0
	planner := &pluginPlanSource{
		docs: func() []lockfile.Document {
			calls++
			return nil
		},
	}
	if _, _, err := planner.get(context.Background()); err != nil {
		t.Fatalf("first get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("docs invoked %d times after first get, want 1", calls)
	}
	if _, _, err := planner.get(context.Background()); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("docs invoked %d times on cached get, want 1", calls)
	}
	planner.built = false
	if _, _, err := planner.get(context.Background()); err != nil {
		t.Fatalf("get after invalidation: %v", err)
	}
	if calls != 2 {
		t.Fatalf("docs invoked %d times after invalidation, want 2", calls)
	}
}
