package telemetry

import (
	"context"
	"strconv"
	"time"
	"unicode/utf8"

	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"

	otelmetric "go.opentelemetry.io/otel/metric"
)

// InventorySnapshot is one complete, telemetry-safe effective configuration.
// Fingerprint and all child values must already be canonicalized and sanitized
// by the inventory builder; recording is best effort.
type InventorySnapshot struct {
	Fingerprint  string
	Harnesses    string
	HarnessScope string
	Registries   []InventoryRegistry
	Resources    []InventoryResource
}

// InventoryRegistry is the telemetry-safe identity and configuration metadata
// for one registry in an InventorySnapshot.
type InventoryRegistry struct {
	ID          string
	Location    string
	Path        string
	Ref         string
	ConfigScope string
	Required    bool
	Default     bool
}

// InventoryResource is the telemetry-safe identity and effective source
// metadata for one enabled resource in an InventorySnapshot.
type InventoryResource struct {
	RegistryID       string
	RegistryLocation string
	RegistryPath     string
	RegistryRef      string
	Kind             string
	Name             string
	ConfigScope      string
	Required         bool
}

// InventoryOmissionReason is one of the bounded reasons an inventory
// observation was not exported.
type InventoryOmissionReason string

const (
	MaxInventoryRegistries     = 1_000
	MaxInventoryResources      = 10_000
	MaxInventoryAttributeBytes = 512

	// InventoryCardinalityLimit leaves room for the SDK overflow point while
	// preserving every resource in one maximum complete generation.
	InventoryCardinalityLimit = MaxInventoryResources + 1

	InventoryOmissionTooLarge  InventoryOmissionReason = "inventory_too_large"
	InventoryOmissionInvalid   InventoryOmissionReason = "invalid_inventory"
	InventoryOmissionQueueFull InventoryOmissionReason = "queue_full"
)

type inventoryObservation struct {
	snapshot   InventorySnapshot
	observedAt int64
}

// InventorySnapshotOmissionReason validates the public inventory cardinality
// and UTF-8 attribute contract.
func InventorySnapshotOmissionReason(snapshot InventorySnapshot) InventoryOmissionReason {
	if len(snapshot.Registries) > MaxInventoryRegistries || len(snapshot.Resources) > MaxInventoryResources {
		return InventoryOmissionTooLarge
	}
	if reason := inventoryAttributeOmissionReason(snapshot.Fingerprint, snapshot.Harnesses, snapshot.HarnessScope); reason != "" {
		return reason
	}
	for _, registry := range snapshot.Registries {
		if reason := inventoryAttributeOmissionReason(registry.ID, registry.Location, registry.Path, registry.Ref, registry.ConfigScope); reason != "" {
			return reason
		}
	}
	for _, resource := range snapshot.Resources {
		if reason := inventoryAttributeOmissionReason(resource.RegistryID, resource.RegistryLocation, resource.RegistryPath, resource.RegistryRef, resource.Kind, resource.Name, resource.ConfigScope); reason != "" {
			return reason
		}
	}
	return ""
}

func inventoryAttributeOmissionReason(values ...string) InventoryOmissionReason {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return InventoryOmissionInvalid
		}
		if len(value) > MaxInventoryAttributeBytes {
			return InventoryOmissionTooLarge
		}
	}
	return ""
}

// InventorySnapshotWithinBounds reports whether a complete snapshot fits the
// public inventory cardinality and UTF-8 byte limits.
func InventorySnapshotWithinBounds(snapshot InventorySnapshot) bool {
	return InventorySnapshotOmissionReason(snapshot) == ""
}

// RecordInventory records a complete inventory snapshot on the telemetry
// runtime carried by ctx. It is a no-op when telemetry is absent or inventory
// metrics are unavailable.
func RecordInventory(ctx context.Context, snapshot InventorySnapshot) {
	if tel := TelemetryFromContext(ctx); tel != nil {
		tel.RecordInventory(ctx, snapshot)
	}
}

func InventoryEnabled(ctx context.Context) bool {
	tel := TelemetryFromContext(ctx)
	return tel != nil && tel.inventorySnapshotGauge != nil
}

// RecordInventoryOmission increments a bounded detailed-primary diagnostic
// counter when an inventory snapshot cannot be represented or queued.
func RecordInventoryOmission(ctx context.Context, reason InventoryOmissionReason) {
	if tel := TelemetryFromContext(ctx); tel != nil {
		tel.RecordInventoryOmission(ctx, reason)
	}
}

// RecordInventory enqueues a complete inventory snapshot for prompt background
// export. It never performs network I/O on the caller's goroutine.
func (t *Telemetry) RecordInventory(ctx context.Context, snapshot InventorySnapshot) {
	if t == nil || t.inventorySnapshotGauge == nil {
		return
	}
	if reason := InventorySnapshotOmissionReason(snapshot); reason != "" {
		t.RecordInventoryOmission(ctx, reason)
		return
	}
	snapshot.Registries = append([]InventoryRegistry(nil), snapshot.Registries...)
	snapshot.Resources = append([]InventoryResource(nil), snapshot.Resources...)

	t.inventoryMu.Lock()
	// Current Unix microseconds remain exactly representable in Prometheus float64
	// samples; advance logically when this runtime observes a wall-clock tie.
	now := time.Now().UnixMicro()
	if now <= t.lastInventoryObservedAt {
		now = t.lastInventoryObservedAt + 1
	}
	t.lastInventoryObservedAt = now
	if t.diagnostic {
		t.inventoryMu.Unlock()
		t.recordInventoryObservation(ctx, inventoryObservation{snapshot: snapshot, observedAt: now})
		return
	}
	if t.inventoryStopped || t.inventoryQueue == nil {
		t.inventoryMu.Unlock()
		return
	}
	select {
	case t.inventoryQueue <- inventoryObservation{snapshot: snapshot, observedAt: now}:
		t.inventoryMu.Unlock()
	default:
		t.inventoryMu.Unlock()
		t.RecordInventoryOmission(ctx, InventoryOmissionQueueFull)
	}
}

// RecordInventoryOmission records only predefined reasons, keeping the counter
// cardinality bounded.
func (t *Telemetry) RecordInventoryOmission(ctx context.Context, reason InventoryOmissionReason) {
	if t == nil || t.inventoryOmissionCounter == nil {
		return
	}
	switch reason {
	case InventoryOmissionTooLarge, InventoryOmissionInvalid, InventoryOmissionQueueFull:
	default:
		reason = InventoryOmissionInvalid
	}
	attrs := filteredAttrsToKeyValues(map[string]string{
		string(tmcfg.MetricAttrOmissionReason): string(reason),
	}, inventoryOmissionAllowed)
	t.inventoryOmissionCounter.Add(contextOrBackground(ctx), 1, otelmetric.WithAttributes(attrs...))
}

func (t *Telemetry) startInventoryWorker() {
	workerCtx, cancel := context.WithCancel(context.Background())
	t.inventoryQueue = make(chan inventoryObservation, inventoryQueueCapacity)
	t.inventoryDone = make(chan struct{})
	t.inventoryCancel = cancel
	go func() {
		defer close(t.inventoryDone)
		for {
			select {
			case <-workerCtx.Done():
				return
			default:
			}
			select {
			case <-workerCtx.Done():
				return
			case observation, ok := <-t.inventoryQueue:
				if !ok {
					return
				}
				t.recordInventoryObservation(workerCtx, observation)
				if t.inventoryIsStopped() {
					for observation := range t.inventoryQueue {
						t.recordInventoryObservation(workerCtx, observation)
					}
					t.flushInventory(workerCtx, inventoryShutdownExportTimeout)
					return
				}
				t.flushInventory(workerCtx, inventoryExportTimeout)
			}
		}
	}()
}

func (t *Telemetry) inventoryIsStopped() bool {
	t.inventoryMu.Lock()
	defer t.inventoryMu.Unlock()
	return t.inventoryStopped
}

func (t *Telemetry) flushInventory(ctx context.Context, timeout time.Duration) {
	flushCtx, cancel := context.WithTimeout(ctx, timeout)
	err := t.inventoryMP.ForceFlush(flushCtx)
	cancel()
	if err != nil && ctx.Err() == nil {
		t.recordFailure(err)
	}
}

func (t *Telemetry) stopInventoryWorker(ctx context.Context) bool {
	if t == nil || t.inventoryDone == nil {
		return true
	}
	t.inventoryMu.Lock()
	if !t.inventoryStopped {
		t.inventoryStopped = true
		close(t.inventoryQueue)
	}
	done := t.inventoryDone
	cancel := t.inventoryCancel
	t.inventoryMu.Unlock()

	select {
	case <-done:
		cancel()
		return true
	case <-ctx.Done():
		cancel()
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
}

func (t *Telemetry) shutdownInventoryProvider(ctx context.Context) error {
	if t.stopInventoryWorker(ctx) {
		return t.inventoryMP.Shutdown(ctx)
	}

	// Never race provider shutdown against a ForceFlush that ignored
	// cancellation. If it eventually returns, finish cleanup independently.
	done := t.inventoryDone
	go func() {
		<-done
		cleanupCtx, cancel := context.WithTimeout(context.Background(), maxShutdownFlush)
		defer cancel()
		if err := t.inventoryMP.Shutdown(cleanupCtx); err != nil {
			t.recordFailure(err)
		}
	}()
	return ctx.Err()
}

func (t *Telemetry) recordInventoryObservation(ctx context.Context, observation inventoryObservation) {
	snapshot := observation.snapshot
	ctx = contextOrBackground(ctx)
	recordGauge(ctx, t.inventorySnapshotGauge, observation.observedAt, map[string]string{
		string(tmcfg.MetricAttrConfigFingerprint): snapshot.Fingerprint,
		string(tmcfg.MetricAttrHarnesses):         snapshot.Harnesses,
		string(tmcfg.MetricAttrHarnessScope):      snapshot.HarnessScope,
		string(tmcfg.MetricAttrRegistryCount):     strconv.Itoa(len(snapshot.Registries)),
		string(tmcfg.MetricAttrResourceCount):     strconv.Itoa(len(snapshot.Resources)),
	}, inventorySnapshotAllowed)

	for _, registry := range snapshot.Registries {
		recordGauge(ctx, t.inventoryRegistryGauge, 1, map[string]string{
			string(tmcfg.MetricAttrConfigFingerprint): snapshot.Fingerprint,
			string(tmcfg.MetricAttrRegistryID):        registry.ID,
			string(tmcfg.MetricAttrRegistryLocation):  registry.Location,
			string(tmcfg.MetricAttrRegistryPath):      registry.Path,
			string(tmcfg.MetricAttrRegistryRef):       registry.Ref,
			string(tmcfg.MetricAttrConfigScope):       registry.ConfigScope,
			string(tmcfg.MetricAttrRegistryRequired):  strconv.FormatBool(registry.Required),
			string(tmcfg.MetricAttrRegistryDefault):   strconv.FormatBool(registry.Default),
		}, inventoryRegistryAllowed)
	}
	for _, resource := range snapshot.Resources {
		recordGauge(ctx, t.inventoryResourceGauge, 1, map[string]string{
			string(tmcfg.MetricAttrConfigFingerprint): snapshot.Fingerprint,
			string(tmcfg.MetricAttrRegistryID):        resource.RegistryID,
			string(tmcfg.MetricAttrRegistryLocation):  resource.RegistryLocation,
			string(tmcfg.MetricAttrRegistryPath):      resource.RegistryPath,
			string(tmcfg.MetricAttrRegistryRef):       resource.RegistryRef,
			string(tmcfg.MetricAttrResourceKind):      resource.Kind,
			string(tmcfg.MetricAttrResourceName):      resource.Name,
			string(tmcfg.MetricAttrConfigScope):       resource.ConfigScope,
			string(tmcfg.MetricAttrResourceRequired):  strconv.FormatBool(resource.Required),
		}, inventoryResourceAllowed)
	}
}

var (
	inventorySnapshotAllowed = metricAttributeSet(tmcfg.InventorySnapshotAttributes)
	inventoryRegistryAllowed = metricAttributeSet(tmcfg.InventoryRegistryAttributes)
	inventoryResourceAllowed = metricAttributeSet(tmcfg.InventoryResourceAttributes)
	inventoryOmissionAllowed = metricAttributeSet(tmcfg.InventoryOmissionAttributes)
)

func metricAttributeSet(attrs []tmcfg.MetricAttribute) map[string]struct{} {
	out := make(map[string]struct{}, len(attrs))
	for _, attr := range attrs {
		out[string(attr)] = struct{}{}
	}
	return out
}

func recordGauge(ctx context.Context, gauge otelmetric.Int64Gauge, value int64, attrs map[string]string, allowed map[string]struct{}) {
	if gauge == nil {
		return
	}
	filtered := filteredAttrsToKeyValues(attrs, allowed)
	if len(filtered) == 0 {
		gauge.Record(ctx, value)
		return
	}
	gauge.Record(ctx, value, otelmetric.WithAttributes(filtered...))
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
