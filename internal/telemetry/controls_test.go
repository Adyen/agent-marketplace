package telemetry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	amconfig "github.com/Adyen/agent-marketplace/internal/config"
	tmcfg "github.com/Adyen/agent-marketplace/internal/telemetry/config"
)

func TestReadInstallationID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installation-id")

	id, err := ReadInstallationID(path)
	if err != nil {
		t.Fatalf("ReadInstallationID missing: %v", err)
	}
	if id != "" {
		t.Fatalf("expected empty id for missing file, got %q", id)
	}

	if err := os.WriteFile(path, []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err = ReadInstallationID(path)
	if err != nil {
		t.Fatalf("ReadInstallationID: %v", err)
	}
	if id != "abc123" {
		t.Fatalf("expected abc123, got %q", id)
	}
}

func TestLoadInstallationIDConcurrentFirstUse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installation-id")

	const callers = 32
	start := make(chan struct{})
	ids := make(chan string, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			<-start
			id, err := LoadInstallationID(path)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent load: %v", err)
	}
	var want string
	for id := range ids {
		if want == "" {
			want = id
			continue
		}
		if id != want {
			t.Fatalf("concurrent loads returned different IDs: %q and %q", want, id)
		}
	}
	if want == "" {
		t.Fatal("concurrent loads returned no ID")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read installation id directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".installation-id.tmp-") {
			t.Errorf("temporary installation id file was not cleaned up: %s", entry.Name())
		}
	}
}

func TestQueueOldestAge(t *testing.T) {
	dir := t.TempDir()
	q := NewQueue(dir)

	if _, ok := q.OldestAge(); ok {
		t.Fatal("expected no oldest age for empty queue")
	}

	oldPath := filepath.Join(dir, "metrics", "1000000000-000000000-aaaa.json")
	newPath := filepath.Join(dir, "metrics", "2000000000-000000000-bbbb.json")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure at least a small gap between modification times.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(newPath, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	age, ok := q.OldestAge()
	if !ok {
		t.Fatal("expected oldest age for non-empty queue")
	}
	if age <= 0 {
		t.Fatalf("expected positive oldest age, got %v", age)
	}
}

func TestGetStatusInfoNoConfig(t *testing.T) {
	setupTelemetryTest(t)

	info, err := GetStatusInfo(context.Background())
	if err != nil {
		t.Fatalf("GetStatusInfo: %v", err)
	}

	if info.State.EffectiveLevel != tmcfg.LevelOff {
		t.Fatalf("expected effective level off, got %q", info.State.EffectiveLevel)
	}
	if info.InstallationID != "" {
		t.Fatalf("expected no installation id, got %q", info.InstallationID)
	}
	if info.QueueCount != 0 {
		t.Fatalf("expected empty queue, got %d", info.QueueCount)
	}
	if info.LastFailure.Error != "" {
		t.Fatalf("expected no failure, got %q", info.LastFailure.Error)
	}
}

func TestGetStatusInfoWithQueueAndFailure(t *testing.T) {
	dir := setupTelemetryTest(t)
	stateRoot := filepath.Join(dir, "agent-marketplace", "telemetry")
	queueDir := filepath.Join(stateRoot, "queue", "metrics")
	if err := os.MkdirAll(queueDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queueDir, "queued.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	diagDir := filepath.Join(stateRoot, "diagnostic")
	if err := os.MkdirAll(diagDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diagDir, "diag.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	failurePath := filepath.Join(stateRoot, "last-delivery-failure")
	if err := os.WriteFile(failurePath, []byte(`{"time":"2026-08-19T12:00:00Z","error":"network timeout"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := GetStatusInfo(context.Background())
	if err != nil {
		t.Fatalf("GetStatusInfo: %v", err)
	}
	if info.QueueCount != 1 {
		t.Fatalf("expected queue count 1, got %d", info.QueueCount)
	}
	if info.DiagnosticCount != 1 {
		t.Fatalf("expected diagnostic count 1, got %d", info.DiagnosticCount)
	}
	if info.LastFailure.Error != "network timeout" {
		t.Fatalf("expected network timeout, got %q", info.LastFailure.Error)
	}
}

func TestPreview(t *testing.T) {
	dir := setupTelemetryTest(t)
	state, err := ResolveTelemetryConfig(amconfig.Settings{}, amconfig.Settings{}, "", "")
	if err != nil {
		t.Fatalf("ResolveTelemetryConfig: %v", err)
	}

	path, err := Preview(context.Background(), tmcfg.LevelBasic, state, "3.0.4")
	if err != nil {
		t.Fatalf("Preview basic: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, "agent-marketplace", "telemetry", "preview")) {
		t.Fatalf("unexpected preview path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "command.invocations") {
		t.Fatalf("expected command.invocations in preview, got:\n%s", content)
	}
	if strings.Contains(content, "spans") {
		t.Fatalf("basic preview should not contain spans, got:\n%s", content)
	}
	if strings.Contains(content, string(tmcfg.InventorySnapshotName)) {
		t.Fatalf("basic preview should not contain inventory, got:\n%s", content)
	}

	path, err = Preview(context.Background(), tmcfg.LevelDetailed, state, "3.0.4")
	if err != nil {
		t.Fatalf("Preview detailed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "agent_marketplace.command") {
		t.Fatalf("expected detailed spans in preview, got:\n%s", string(data))
	}
	for _, want := range []string{string(tmcfg.InventorySnapshotName), string(tmcfg.InventoryRegistryName), string(tmcfg.InventoryResourceName), string(tmcfg.InventoryOmissionsName), "inventory_too_large", "config.fingerprint", "resource.name", "agent_marketplace.tui.config_registry", "registry.location", "agent_marketplace.tui.config_resources", "resource.names", "code-review", "smith"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected detailed config value %q in preview, got:\n%s", want, string(data))
		}
	}

	path, err = Preview(context.Background(), tmcfg.LevelDiagnostic, state, "3.0.4")
	if err != nil {
		t.Fatalf("Preview diagnostic: %v", err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "sensitive_data_warning") {
		t.Fatalf("expected diagnostic warning in preview, got:\n%s", string(data))
	}
}

func TestPurge(t *testing.T) {
	dir := setupTelemetryTest(t)
	stateRoot := filepath.Join(dir, "agent-marketplace", "telemetry")
	idPath := filepath.Join(dir, "agent-marketplace", DefaultInstallationIDSubpath)

	queueDir := filepath.Join(stateRoot, "queue", "metrics")
	if err := os.MkdirAll(queueDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queueDir, "queued.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	diagDir := filepath.Join(stateRoot, "diagnostic")
	if err := os.MkdirAll(diagDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diagDir, "diag.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(idPath), 0o700); err != nil {
		t.Fatal(err)
	}

	qCount, dCount, err := Purge(idPath)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if qCount != 1 || dCount != 1 {
		t.Fatalf("expected counts 1,1 got %d,%d", qCount, dCount)
	}

	if _, err := os.Stat(queueDir); !os.IsNotExist(err) {
		t.Fatal("queue dir should have been removed")
	}
	if _, err := os.Stat(diagDir); !os.IsNotExist(err) {
		t.Fatal("diagnostic dir should have been removed")
	}
}

func setupTelemetryTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("AGENT_MARKETPLACE_TELEMETRY_LEVEL", "")
	t.Setenv("AGENT_MARKETPLACE_TELEMETRY_ENDPOINT", "")
	t.Setenv("AGENT_MARKETPLACE_TELEMETRY_COMMUNITY_REPORTING", "")

	orgPath := filepath.Join(dir, "nonexistent-org-config.yaml")
	amconfig.SetTestV3OrganizationConfigPath(t, orgPath)
	return dir
}
