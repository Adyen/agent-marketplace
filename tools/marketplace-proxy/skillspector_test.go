package marketplaceproxy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type skillspectorResource struct {
	ContentDigest string `json:"content_digest"`
	Assessment    struct {
		Decision        string `json:"decision"`
		AdvisoryReports []struct {
			Tool     string `json:"tool"`
			Version  string `json:"version"`
			Status   string `json:"status"`
			Reason   string `json:"reason"`
			Findings []struct {
				Code     string `json:"code"`
				Severity string `json:"severity"`
				Summary  string `json:"summary"`
				Path     string `json:"path"`
			} `json:"findings"`
		} `json:"advisory_reports"`
	} `json:"assessment"`
}

// spec: marketplace-delivery-security/default-deployment-scans-with-an-isolated-runner
func TestMarketplaceSkillSpectorComposeWitness(t *testing.T) {
	if os.Getenv("AM_SKILLSPECTOR_WITNESS") != "1" {
		t.Skip("set AM_SKILLSPECTOR_WITNESS=1 with local candidate app and runner images")
	}
	appImage := os.Getenv("AM_SKILLSPECTOR_APP_IMAGE")
	runnerImage := os.Getenv("AM_SKILLSPECTOR_RUNNER_IMAGE")
	if appImage == "" || runnerImage == "" {
		t.Fatal("AM_SKILLSPECTOR_APP_IMAGE and AM_SKILLSPECTOR_RUNNER_IMAGE must name local candidate images")
	}
	for _, image := range []string{appImage, runnerImage} {
		if output, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image).CombinedOutput(); err != nil {
			t.Fatalf("inspect local image %q (images are never pulled): %v\n%s", image, err, output)
		}
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	temp, err := os.MkdirTemp("", "marketplace-skillspector-witness-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	project := fmt.Sprintf("am-skillspector-%d", time.Now().UnixNano())
	port := freeLoopbackPort(t)
	registry := filepath.Join(temp, "registry")
	skill := filepath.Join(registry, ".agents", "skills", "offline-dependency")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWitnessFile(t, filepath.Join(skill, "SKILL.md"), "---\nname: offline-dependency\ndescription: Inert offline scanner fixture\nlicense: MIT\n---\nRead files and summarize their contents.\n")
	writeWitnessFile(t, filepath.Join(skill, "requirements.txt"), "requests==2.30.0\n")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "skillspector@example.invalid"},
		{"config", "user.name", "SkillSpector witness"},
		{"add", "."},
		{"commit", "-q", "-m", "inert SkillSpector fixture"},
		{"update-server-info"},
	} {
		command := exec.Command("git", args...)
		command.Dir = registry
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	config := `apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: local-skillspector-witness
  name: Local SkillSpector witness
  canonical_url: https://marketplace.example.invalid
  browser_base: https://marketplace.example.invalid
listen: 0.0.0.0:8080
operations_listen: 127.0.0.1:9090
storage:
  metadata:
    driver: sqlite
    dsn: /var/lib/agent-marketplace/marketplace.db
  blobs:
    driver: filesystem
    path: /var/lib/agent-marketplace/blobs
acquisition_default: mirror
refresh:
  interval: 30m
refresh_concurrency: 1
policy:
  hidden_content: warn
  license_allowlist: [MIT]
  license_undeclared: reject
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: offline-fixture
    slug: offline-fixture
    origin: http://fixture:8082/
    ref: main
    acquisition: mirror
    publisher:
      name: Local fixture
    license: MIT
    roots:
      - path: .
`
	configPath := filepath.Join(temp, "marketplace.yaml")
	writeWitnessFile(t, configPath, config)
	override := fmt.Sprintf(`services:
  app:
    image: %s
    ports:
      - target: 8080
        published: "%d"
        host_ip: 127.0.0.1
        protocol: tcp
    volumes:
      - type: bind
        source: %s
        target: /app/marketplace.yaml
        read_only: true
  skillspector:
    image: %s
  fixture:
    image: %s
    entrypoint: ["/opt/scanner/bin/python", "-m", "http.server", "8082", "--directory", "/fixtures"]
    command: []
    read_only: true
    volumes:
      - type: bind
        source: %s
        target: /fixtures
        read_only: true
`, strconv.Quote(appImage), port, strconv.Quote(configPath), strconv.Quote(runnerImage), strconv.Quote(runnerImage), strconv.Quote(filepath.Join(registry, ".git")))
	overridePath := filepath.Join(temp, "compose.skillspector.yaml")
	writeWitnessFile(t, overridePath, override)
	t.Cleanup(func() {
		if t.Failed() {
			command := skillspectorComposeCommand(root, project, overridePath, port, "logs", "--tail", "30", "app", "skillspector", "fixture")
			if output, err := command.CombinedOutput(); err != nil {
				t.Logf("bounded local Compose logs unavailable: %v\n%s", err, output)
			} else {
				t.Logf("bounded local Compose logs:\n%s", output)
			}
		}
		command := skillspectorComposeCommand(root, project, overridePath, port, "down", "--volumes", "--remove-orphans")
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("remove disposable Compose project: %v\n%s", err, output)
		}
	})
	for _, service := range []string{"fixture", "skillspector", "app"} {
		command := skillspectorComposeCommand(root, project, overridePath, port, "up", "-d", "--no-build", "--pull", "never", service)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("start local %s service without building or pulling: %v\n%s", service, err, output)
		}
	}
	containerID := strings.TrimSpace(runSkillspectorCompose(t, root, project, overridePath, port, "ps", "-q", "skillspector"))
	assertSkillspectorContainerIsolated(t, containerID)
	revision := waitForSkillspectorRevision(t, fmt.Sprintf("http://127.0.0.1:%d/api/v1/resources/offline-fixture/skill/offline-dependency", port))
	if revision.ContentDigest == "" || revision.Assessment.Decision != "allow" {
		t.Fatalf("built-in policy outcome changed: content=%q decision=%q", revision.ContentDigest, revision.Assessment.Decision)
	}
	if len(revision.Assessment.AdvisoryReports) != 1 {
		t.Fatalf("advisory reports = %#v", revision.Assessment.AdvisoryReports)
	}
	report := revision.Assessment.AdvisoryReports[0]
	if report.Tool != "skillspector" || report.Version != "2.13.6" || report.Status != "partial" ||
		len(report.Findings) == 0 || report.Findings[0].Code != "SC4" ||
		report.Findings[0].Summary != "Dependency risk detected." ||
		report.Findings[0].Path != "support/requirements.txt" {
		t.Fatalf("real SkillSpector report = %#v", report)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port
}

func writeWitnessFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write disposable fixture: %v", err)
	}
}

func skillspectorComposeCommand(root, project, override string, port int, args ...string) *exec.Cmd {
	composeArgs := []string{
		"compose", "--env-file", "/dev/null", "-p", project,
		"-f", filepath.Join(root, "compose.yaml"), "-f", override,
	}
	commandArgs := append(composeArgs, args...)
	command := exec.Command("docker", commandArgs...)
	command.Dir = root
	command.Env = append(os.Environ(), "MARKETPLACE_HTTP_PORT="+strconv.Itoa(port))
	return command
}

func runSkillspectorCompose(t *testing.T, root, project, override string, port int, args ...string) string {
	t.Helper()
	command := skillspectorComposeCommand(root, project, override, port, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func waitForSkillspectorRevision(t *testing.T, endpoint string) skillspectorResource {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(90 * time.Second)
	var lastStatus int
	var lastBody string
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			lastStatus = response.StatusCode
			body, readErr := responseBody(response)
			lastBody = body
			if readErr == nil && response.StatusCode == http.StatusOK {
				var revision skillspectorResource
				if err := json.Unmarshal([]byte(body), &revision); err != nil {
					t.Fatalf("decode published revision: %v\n%s", err, body)
				}
				if len(revision.Assessment.AdvisoryReports) != 0 {
					return revision
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("published SkillSpector report did not arrive; last HTTP status=%d response=%s", lastStatus, lastBody)
	return skillspectorResource{}
}

func responseBody(response *http.Response) (string, error) {
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return string(body), err
}

func assertSkillspectorContainerIsolated(t *testing.T, id string) {
	t.Helper()
	if id == "" {
		t.Fatal("Compose did not start the runner container")
	}
	output, err := exec.Command("docker", "inspect", id).Output()
	if err != nil {
		t.Fatalf("inspect runner container: %v", err)
	}
	var containers []struct {
		Config struct {
			User         string         `json:"User"`
			Env          []string       `json:"Env"`
			ExposedPorts map[string]any `json:"ExposedPorts"`
		} `json:"Config"`
		HostConfig struct {
			NetworkMode    string           `json:"NetworkMode"`
			ReadonlyRootfs bool             `json:"ReadonlyRootfs"`
			PortBindings   map[string][]any `json:"PortBindings"`
			CapDrop        []string         `json:"CapDrop"`
			SecurityOpt    []string         `json:"SecurityOpt"`
			Memory         int64            `json:"Memory"`
			NanoCpus       int64            `json:"NanoCpus"`
			PidsLimit      int64            `json:"PidsLimit"`
		} `json:"HostConfig"`
		Mounts []struct {
			Type        string `json:"Type"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(output, &containers); err != nil || len(containers) != 1 {
		t.Fatalf("decode runner inspection: %v\n%s", err, output)
	}
	container := containers[0]
	if container.HostConfig.NetworkMode != "none" || container.Config.User != "1000:1000" ||
		!container.HostConfig.ReadonlyRootfs || container.HostConfig.Memory == 0 ||
		container.HostConfig.NanoCpus == 0 || container.HostConfig.PidsLimit == 0 ||
		len(container.HostConfig.PortBindings) != 0 || len(container.Config.ExposedPorts) != 0 {
		t.Fatalf("runner is not isolated and bounded: %#v", container.HostConfig)
	}
	if !strings.Contains(strings.Join(container.HostConfig.CapDrop, ","), "ALL") ||
		!strings.Contains(strings.Join(container.HostConfig.SecurityOpt, ","), "no-new-privileges:true") {
		t.Fatalf("runner privileges = %#v", container.HostConfig)
	}
	socketVolume := false
	for _, mount := range container.Mounts {
		if mount.Type == "bind" || mount.Destination == "/var/run/docker.sock" {
			t.Fatalf("runner has a host or runtime mount: %#v", mount)
		}
		if mount.Type == "volume" && mount.Destination == "/run/skillspector" {
			socketVolume = true
		}
		if mount.Destination != "/run/skillspector" && mount.Destination != "/tmp" {
			t.Fatalf("unexpected runner mount: %#v", mount)
		}
	}
	if !socketVolume {
		t.Fatalf("runner has no scoped socket volume: %#v", container.Mounts)
	}
	for _, value := range container.Config.Env {
		if strings.HasPrefix(value, "AWS_") || strings.HasPrefix(value, "MARKETPLACE_") ||
			strings.Contains(strings.ToLower(value), "token") || strings.Contains(strings.ToLower(value), "secret") {
			t.Fatalf("runner received a credential or Marketplace setting: %q", value)
		}
	}
}
