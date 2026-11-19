package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Adyen/agent-marketplace/pkg/marketplace/claudeplugins"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
)

func TestFinishClaudeDeliveryHonorsRetryAfter(t *testing.T) {
	ctx := context.Background()
	deps, _ := managedFixture(t)
	payload, err := json.Marshal(store.ClaudeDeliveryJobPayload{Marketplace: "test-mp"})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.MetadataStore.EnqueueJob(ctx, store.Job{
		ID: "rate-limited-delivery", Kind: store.ClaudeDeliveryJobKind,
		Payload: payload, MaxAttempts: 1,
	}); err != nil {
		t.Fatalf("enqueue delivery job: %v", err)
	}
	leased, err := deps.MetadataStore.(store.ClaudeDeliveryJobStore).LeaseClaudeDeliveryJob(
		ctx, "test-mp", "rate-limited-delivery", "delivery-worker", time.Minute,
	)
	if err != nil {
		t.Fatalf("lease delivery job: %v", err)
	}
	const cooldown = 2 * time.Minute
	throttle := &claudeplugins.ThrottledError{
		APIError: &claudeplugins.APIError{RetryAfter: cooldown}, RetryAfter: cooldown,
	}
	before := time.Now().UTC()
	if err := finishClaudeDelivery(ctx, deps, leased, "delivery-worker", throttle); !errors.Is(err, throttle) {
		t.Fatalf("finishClaudeDelivery error = %v, want the throttle cause", err)
	}
	failed, err := deps.MetadataStore.GetJob(ctx, leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != store.JobStatusFailed || failed.Backoff != cooldown ||
		failed.LeaseUntil.Before(before.Add(cooldown-time.Millisecond)) ||
		failed.LeaseUntil.After(time.Now().Add(cooldown+time.Second)) {
		t.Fatalf("stored retry cooldown = %+v, want %s", failed, cooldown)
	}
}

func TestClaudeDeliveryThrottleFailureCode(t *testing.T) {
	throttle := &claudeplugins.ThrottledError{
		APIError:   &claudeplugins.APIError{Code: "rate_limited", RetryAfter: time.Minute},
		RetryAfter: time.Minute,
	}
	if got := deliveryFailureCode(throttle); got != "claude-api.rate-limited" {
		t.Fatalf("deliveryFailureCode = %q, want claude-api.rate-limited", got)
	}
}
