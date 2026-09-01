package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	hccartifact "github.com/Adyen/agent-marketplace/pkg/harness-compilation/artifact"
	mpapi "github.com/Adyen/agent-marketplace/pkg/marketplace/api"
	mpconfig "github.com/Adyen/agent-marketplace/pkg/marketplace/config"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/store"
	"github.com/Adyen/agent-marketplace/pkg/marketplace/worker"
)

func TestMarketplacePublicGate(t *testing.T) {
	env := newEnv(t)
	env.InitGitRepo()

	files := RegistryFilesEx(
		[]Skill{{Name: "skill-one", Description: "A hostile-safe skill"}},
		[]Command{{Name: "command-one", Description: "A command", Command: "echo command-one"}},
		[]Subagent{{Name: "agent-one", Description: "An agent", Tools: []string{"Read"}}},
		[]MCPServer{{Name: "server-one", Command: "echo", Args: []string{"server-one"}}},
	)
	files["hooks/hooks.json"] = `{"hooks":{"PreToolUse":[{"type":"command","command":"echo hook","matcher":"Read"}]}}`
	files["collections/public-bundle/COLLECTION.yaml"] = `name: public-bundle
version: "1.0"
description: Public gate collection
resources:
  - name: skill-one
    kind: skill
  - name: command-one
    kind: command
`
	rooted := make(map[string]string, len(files))
	for path, content := range files {
		rooted[path] = content
	}
	env.CreateBareRepo("public-gate-registry", rooted)
	gitServer := newGitHTTPServer(t, env.repos)
	defer gitServer.Close()
	registryURL := gitServer.URL + "/public-gate-registry.git"

	dbPath := filepath.Join(env.workspace, "marketplace.db")
	blobsPath := filepath.Join(env.workspace, "blobs")
	listenAddr := publicGateEphemeralAddress(t)
	operationsAddr := publicGateEphemeralAddress(t)
	config := publicGateConfig(registryURL, "", dbPath, blobsPath, listenAddr, operationsAddr)
	env.WriteFile("/workspace/marketplace.yaml", config)
	result := env.RunAMWithEnv(map[string]string{"MARKETPLACE_PRIVATE_TOKEN": "private-origin-secret"}, "serve", "reindex", "--config", "marketplace.yaml")
	if result.ExitCode != 0 {
		t.Fatalf("serve reindex failed (exit %d):\n%s", result.ExitCode, result.Combined)
	}
	if strings.Contains(result.Combined, "private-origin-secret") {
		t.Fatalf("serve reindex output leaked the configured token: %s", result.Combined)
	}

	cfgPath := env.mustResolvePath("/workspace/marketplace.yaml")
	assertPublicGateProcess(t, env, cfgPath, listenAddr, operationsAddr)
	cfg, meta, blobs := openPublicGateStores(t, cfgPath)
	server := httptest.NewServer(mpapi.NewServer(cfg, meta, blobs).PublicHandler())
	defer server.Close()

	skillDigest := publicGateResourceDigest(t, server.URL, "skill", "skill-one")
	collectionDigest := publicGateCollection(t, server.URL)
	assertPublicGateRoutes(t, server.URL, []string{"skill", "command", "subagent", "hook", "mcpserver", "collection"}, skillDigest)
	assertPublicGateArtifactIntegrity(t, server.URL, "skill", "skill-one")
	assertPublicGateNoSecrets(t, server.URL, []string{"MARKETPLACE_PRIVATE_TOKEN", "private-origin-secret", "refresh-token"})
	assertMarketplaceConsumerRoundTrip(t, server.URL)

	server.Close()
	if err := meta.Close(); err != nil {
		t.Fatal(err)
	}
	env.WriteFile("/workspace/marketplace.yaml", publicGateConfig(registryURL, "e2e-public-gate/registry-public/skill/skill-one@"+skillDigest+"\n    - e2e-public-gate/registry-public/collection/public-bundle@"+collectionDigest, dbPath, blobsPath, listenAddr, operationsAddr))
	cfg, meta, blobs = openPublicGateStores(t, cfgPath)
	if err := worker.EnsureRevocations(t.Context(), worker.Deps{Config: cfg, MetadataStore: meta, BlobStore: blobs}); err != nil {
		t.Fatalf("revocation reconciliation failed: %v", err)
	}
	defer meta.Close()
	revokedServer := httptest.NewServer(mpapi.NewServer(cfg, meta, blobs).PublicHandler())
	defer revokedServer.Close()
	body := getPublicGate(t, revokedServer.URL+"/api/v1/resources/public-registry/skill/skill-one")
	if !strings.Contains(body, `"revocation"`) || !strings.Contains(body, "operator configured") {
		t.Fatalf("revoked metadata did not remain visible: %s", body)
	}
	artifact := getPublicGateResponse(t, revokedServer.URL+"/api/v1/resources/public-registry/skill/skill-one")
	defer artifact.Body.Close()
	var revision struct {
		ArchiveDigest string `json:"archive_digest"`
	}
	if err := json.NewDecoder(artifact.Body).Decode(&revision); err != nil {
		t.Fatal(err)
	}
	if revision.ArchiveDigest == "" {
		t.Fatal("published skill has no archive digest")
	}
	artifactURL := strings.Replace(revision.ArchiveDigest, ":", "/", 1)
	response := getPublicGateResponse(t, revokedServer.URL+"/api/v1/artifacts/"+artifactURL)
	defer response.Body.Close()
	if response.StatusCode != http.StatusGone {
		t.Fatalf("revoked artifact status = %d, want %d", response.StatusCode, http.StatusGone)
	}
	inspection := getPublicGateResponse(t, revokedServer.URL+"/api/v1/resources/public-registry/skill/skill-one/revisions/"+skillDigest+"/inspection")
	defer inspection.Body.Close()
	if inspection.StatusCode != http.StatusOK {
		t.Fatalf("revoked inspection status = %d", inspection.StatusCode)
	}
	collectionManifest := getPublicGateResponse(t, revokedServer.URL+"/api/v1/manifests/"+collectionDigest)
	collectionManifest.Body.Close()
	if collectionManifest.StatusCode != http.StatusGone {
		t.Fatalf("revoked collection manifest status = %d, want %d", collectionManifest.StatusCode, http.StatusGone)
	}
}

func publicGateConfig(origin, revision, dbPath, blobsPath, listen, operationsListen string) string {
	revocations := "  revisions: []"
	if revision != "" {
		revocations = "  revisions:\n    - " + revision
	}
	return fmt.Sprintf(`apiVersion: agent-marketplace.io/marketplace/v1
instance:
  id: e2e-public-gate
  name: E2E Public Gate
  canonical_url: https://marketplace.example.invalid
  browser_base: https://marketplace.example.invalid
listen: %s
operations_listen: %s
storage:
  metadata:
    driver: sqlite
    dsn: %s
  blobs:
    driver: filesystem
    path: %s
acquisition_default: mirror
refresh:
  interval: 1h
policy:
  hidden_content: reject
  license_allowlist: [MIT]
  limits:
    repository_bytes: 104857600
    file_count: 1024
    file_bytes: 2097152
    artifact_bytes: 16777216
    clone_duration: 300s
    ingestion_duration: 120s
registries:
  - id: registry-public
    slug: public-registry
    origin: %s
    ref: main
    acquisition: mirror
    publisher:
      name: E2E Platform
    license: MIT
    credentials:
      token:
        env: MARKETPLACE_PRIVATE_TOKEN
    roots:
      - path: .
        source_harness: claude
revocations:
  registry_ids: []
  coordinates: []
%s
`, listen, operationsListen, dbPath, blobsPath, origin, revocations)
}

func openPublicGateStores(t *testing.T, cfgPath string) (mpconfig.Config, store.MetadataStore, store.BlobStore) {
	t.Helper()
	cfg, err := mpconfig.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	meta, blobs, err := mpconfig.OpenStores(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, meta, blobs
}

func publicGateEphemeralAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func assertPublicGateProcess(t *testing.T, env *Env, cfgPath, publicAddr, operationsAddr string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, env.binary, "serve", "--config", cfgPath, "--role", "api")
	cmd.Dir = env.workspace
	cmd.Env = env.subprocessEnv()
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve --role api: %v", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	waited := false
	var waitErr error
	wait := func() error {
		if !waited {
			waitErr = <-waitCh
			waited = true
		}
		return waitErr
	}
	t.Cleanup(func() {
		cancel()
		_ = wait()
	})

	client := &http.Client{Timeout: 300 * time.Millisecond}
	waitFor := func(url string, want int) {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case err := <-waitCh:
				waited, waitErr = true, err
				t.Fatalf("serve --role api exited before %s was ready: %v\n%s", url, err, output.String())
			default:
			}
			response, err := client.Get(url)
			if err == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode == want {
					return
				}
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("serve --role api did not make %s return %d\n%s", url, want, output.String())
	}
	waitFor("http://"+publicAddr+"/healthz", http.StatusOK)
	waitFor("http://"+operationsAddr+"/readyz", http.StatusOK)
	waitFor("http://"+publicAddr+"/readyz", http.StatusNotFound)
	waitFor("http://"+operationsAddr+"/healthz", http.StatusNotFound)
}

func publicGateResourceDigest(t *testing.T, base, kind, name string) string {
	t.Helper()
	response := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("resource %s/%s status = %d", kind, name, response.StatusCode)
	}
	var revision struct {
		ContentDigest string `json:"content_digest"`
	}
	if err := json.NewDecoder(response.Body).Decode(&revision); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(revision.ContentDigest, "sha256:") {
		t.Fatalf("resource %s/%s returned invalid digest %q", kind, name, revision.ContentDigest)
	}
	return revision.ContentDigest
}

func publicGateCollection(t *testing.T, base string) string {
	t.Helper()
	response := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/collection/public-bundle")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("collection status = %d", response.StatusCode)
	}
	var revision struct {
		ContentDigest string `json:"content_digest"`
		ArchiveDigest string `json:"archive_digest"`
	}
	if err := json.NewDecoder(response.Body).Decode(&revision); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(revision.ContentDigest, "sha256:") {
		t.Fatalf("collection returned invalid content digest %q", revision.ContentDigest)
	}
	if revision.ArchiveDigest != "" {
		t.Fatalf("virtual collection returned archive digest %q", revision.ArchiveDigest)
	}

	manifestResponse := getPublicGateResponse(t, base+"/api/v1/manifests/"+revision.ContentDigest)
	payload, err := io.ReadAll(manifestResponse.Body)
	manifestResponse.Body.Close()
	if err != nil || manifestResponse.StatusCode != http.StatusOK {
		t.Fatalf("collection manifest status/body = %d/%s (%v)", manifestResponse.StatusCode, payload, err)
	}
	manifest, canonicalPayload, err := hccartifact.DecodeCollectionManifest(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode collection manifest: %v", err)
	}
	if err := hccartifact.VerifyCollectionManifest(manifest, canonicalPayload); err != nil {
		t.Fatalf("verify collection manifest: %v", err)
	}
	if err := hccartifact.ValidateFlatMirrorManifest(manifest); err != nil {
		t.Fatalf("validate flat collection manifest: %v", err)
	}
	if manifest.Name != "public-bundle" || manifest.ContentDigest != revision.ContentDigest || len(manifest.Members) != 2 {
		t.Fatalf("collection manifest identity/members = %#v", manifest)
	}
	archiveResponse := getPublicGateResponse(t, base+"/api/v1/artifacts/"+strings.Replace(revision.ContentDigest, ":", "/", 1))
	archiveResponse.Body.Close()
	if archiveResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("virtual collection artifact status = %d, want %d", archiveResponse.StatusCode, http.StatusNotFound)
	}
	return revision.ContentDigest
}

func assertPublicGateArtifactIntegrity(t *testing.T, base, kind, name string) {
	t.Helper()
	response := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name)
	var revision struct {
		ArchiveDigest string `json:"archive_digest"`
	}
	if err := json.NewDecoder(response.Body).Decode(&revision); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if !strings.HasPrefix(revision.ArchiveDigest, "sha256:") {
		t.Fatalf("%s/%s returned invalid archive digest %q", kind, name, revision.ArchiveDigest)
	}
	artifactResponse := getPublicGateResponse(t, base+"/api/v1/artifacts/"+strings.Replace(revision.ArchiveDigest, ":", "/", 1))
	payload, err := io.ReadAll(artifactResponse.Body)
	artifactResponse.Body.Close()
	if err != nil || artifactResponse.StatusCode != http.StatusOK {
		t.Fatalf("%s/%s artifact status/body = %d/%s (%v)", kind, name, artifactResponse.StatusCode, payload, err)
	}
	artifact, _, err := hccartifact.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode %s/%s artifact: %v", kind, name, err)
	}
	if err := artifact.VerifyArchiveDigest(payload); err != nil {
		t.Fatalf("verify %s/%s artifact integrity: %v", kind, name, err)
	}
}

func assertMarketplaceConsumerRoundTrip(t *testing.T, endpoint string) {
	t.Helper()
	consumer := newEnv(t)
	consumer.InitGitRepo()
	consumer.WriteGlobalConfig("apiVersion: agent-marketplace/v2\nharnesses: [factory]\nregistries: []")
	consumer.WriteRepoConfig(fmt.Sprintf(`apiVersion: agent-marketplace/v2
marketplaces:
  - url: %s
    instance_id: e2e-public-gate
    alias: public
`, endpoint))
	for _, locator := range []string{
		"public:public-registry/skill/skill-one",
		"public:public-registry/command/command-one",
		"public:public-registry/subagent/agent-one",
		"public:public-registry/hook/PreToolUse-command",
		"public:public-registry/mcpserver/server-one",
	} {
		result := consumer.RunAM("install", locator, "--yes")
		if result.ExitCode != 0 {
			t.Fatalf("Marketplace mirror install %s failed (exit %d):\n%s", locator, result.ExitCode, result.Combined)
		}
	}
	for _, path := range []string{
		"/workspace/.factory/skills/skill-one/SKILL.md",
		"/workspace/.factory/commands/command-one.md",
		"/workspace/.factory/droids/agent-one.md",
		"/workspace/.factory/hooks/hooks.json",
		"/workspace/.factory/mcp.json",
	} {
		consumer.AssertFileExists(path)
	}
	if !strings.Contains(consumer.ReadFile("/workspace/.factory/skills/skill-one/SKILL.md"), "hostile-safe skill") {
		t.Fatal("Marketplace mirror install produced the wrong skill content")
	}
	config := consumer.ReadFile("/workspace/.agents/config.yaml")
	for _, value := range []string{"kind: skill", "kind: command", "kind: subagent", "kind: hook", "kind: mcpserver", "direct: true", "acquisition: mirror"} {
		if !strings.Contains(config, value) {
			t.Fatalf("Marketplace pin config missing %q:\n%s", value, config)
		}
	}
	for _, args := range [][]string{{"sync"}, {"sync", "--offline"}, {"sync", "--check"}} {
		result := consumer.RunAM(args...)
		if result.ExitCode != 0 {
			t.Fatalf("Marketplace mirror %s failed (exit %d):\n%s", strings.Join(args, " "), result.ExitCode, result.Combined)
		}
	}
}

func assertPublicGateRoutes(t *testing.T, base string, kinds []string, skillDigest string) {
	t.Helper()
	if got := getPublicGate(t, base+"/.well-known/agent-marketplace.json"); !strings.Contains(got, `"instance_id":"e2e-public-gate"`) {
		t.Fatalf("well-known response missing instance identity: %s", got)
	}
	if got := getPublicGate(t, base+"/api/v1/search?limit=100"); !strings.Contains(got, `"total":6`) {
		t.Fatalf("search did not publish all five resource kinds and collection: %s", got)
	}
	for _, path := range []string{
		"/r/public-registry/skill/skill-one",
		"/r/public-registry/skill/skill-one/revisions/" + skillDigest,
		"/readyz",
		"/metrics",
	} {
		response := getPublicGateResponse(t, base+path)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("API-only route %s status = %d, want %d", path, response.StatusCode, http.StatusNotFound)
		}
	}

	for _, kind := range kinds {
		name := map[string]string{
			"skill":      "skill-one",
			"command":    "command-one",
			"subagent":   "agent-one",
			"hook":       "PreToolUse-command",
			"mcpserver":  "server-one",
			"collection": "public-bundle",
		}[kind]
		resource := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name)
		payload, err := io.ReadAll(resource.Body)
		resource.Body.Close()
		if err != nil || resource.StatusCode != http.StatusOK {
			t.Fatalf("%s latest status/body = %d/%s (%v)", kind, resource.StatusCode, payload, err)
		}
		var revision struct {
			ContentDigest string `json:"content_digest"`
		}
		if err := json.Unmarshal(payload, &revision); err != nil {
			t.Fatalf("decode %s revision: %v", kind, err)
		}
		if !strings.HasPrefix(revision.ContentDigest, "sha256:") {
			t.Fatalf("%s latest did not return an exact digest: %q", kind, revision.ContentDigest)
		}
		exact := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name+"/revisions/"+revision.ContentDigest)
		if exact.StatusCode != http.StatusOK {
			t.Fatalf("%s exact status = %d", kind, exact.StatusCode)
		}
		exact.Body.Close()
		inspection := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name+"/revisions/"+revision.ContentDigest+"/inspection")
		inspectionBody, _ := io.ReadAll(inspection.Body)
		inspection.Body.Close()
		if inspection.StatusCode != http.StatusOK || !strings.Contains(string(inspectionBody), `"schema_version":"v1"`) {
			t.Fatalf("%s inspection status/body = %d/%s", kind, inspection.StatusCode, inspectionBody)
		}
		content := getPublicGateResponse(t, base+"/api/v1/resources/public-registry/"+kind+"/"+name+"/revisions/"+revision.ContentDigest+"/inspection/content")
		contentBody, _ := io.ReadAll(content.Body)
		content.Body.Close()
		if content.StatusCode != http.StatusOK || strings.Contains(string(contentBody), "<script>") || content.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s inspection content unsafe status/body/headers = %d/%s/%s", kind, content.StatusCode, contentBody, content.Header.Get("X-Content-Type-Options"))
		}
	}
}

func assertPublicGateNoSecrets(t *testing.T, base string, secrets []string) {
	t.Helper()
	for _, path := range []string{"/.well-known/agent-marketplace.json", "/api/v1/instance", "/api/v1/registries/public-registry", "/api/v1/search"} {
		body := getPublicGate(t, base+path)
		for _, secret := range secrets {
			if strings.Contains(body, secret) {
				t.Fatalf("%s leaked private value %q: %s", path, secret, body)
			}
		}
	}
}

func getPublicGate(t *testing.T, url string) string {
	t.Helper()
	response := getPublicGateResponse(t, url)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status/body = %d/%s", url, response.StatusCode, body)
	}
	return string(body)
}

func getPublicGateResponse(t *testing.T, url string) *http.Response {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return response
}

func newGitHTTPServer(t *testing.T, projectRoot string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		env := append(os.Environ(),
			"GIT_PROJECT_ROOT="+projectRoot,
			"GIT_HTTP_EXPORT_ALL=1",
			"PATH_INFO="+r.URL.Path,
			"QUERY_STRING="+r.URL.RawQuery,
			"REQUEST_METHOD="+r.Method,
			"CONTENT_TYPE="+r.Header.Get("Content-Type"),
			"CONTENT_LENGTH="+strconv.Itoa(len(body)),
			"REMOTE_ADDR=127.0.0.1",
		)
		cmd := exec.Command("git", "http-backend")
		cmd.Env = env
		cmd.Stdin = bytes.NewReader(body)
		output, err := cmd.Output()
		if err != nil {
			http.Error(w, "git http backend failed", http.StatusBadGateway)
			return
		}
		headerEnd := bytes.Index(output, []byte("\r\n\r\n"))
		separator := 4
		if headerEnd < 0 {
			headerEnd = bytes.Index(output, []byte("\n\n"))
			separator = 2
		}
		if headerEnd < 0 {
			http.Error(w, "git http backend returned malformed headers", http.StatusBadGateway)
			return
		}
		status := http.StatusOK
		for _, line := range strings.Split(string(output[:headerEnd]), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if strings.HasPrefix(line, "Status: ") {
				fields := strings.Fields(strings.TrimPrefix(line, "Status: "))
				if len(fields) > 0 {
					if parsed, parseErr := strconv.Atoi(fields[0]); parseErr == nil {
						status = parsed
					}
				}
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if ok {
				w.Header().Add(strings.TrimSpace(key), strings.TrimSpace(value))
			}
		}
		w.WriteHeader(status)
		if len(output) > headerEnd+separator {
			_, _ = w.Write(output[headerEnd+separator:])
		}
	}))
}

func TestMarketplaceProxyRoutingContract(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"deploy/compose/sqlite-filesystem/nginx.conf",
		"deploy/compose/postgres-s3/nginx.conf",
	} {
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		config := string(data)
		for _, fragment := range []string{
			"location = /.well-known/agent-marketplace.json {\n        proxy_pass http://marketplace_api;",
			"location /api/ {\n        proxy_pass http://marketplace_api;",
			"location /r/ {\n        proxy_pass http://marketplace_web;",
			"location / {\n        proxy_pass http://marketplace_web;",
			"location = /readyz { return 404; }",
			"location = /metrics { return 404; }",
		} {
			if !strings.Contains(config, fragment) {
				t.Errorf("%s missing routing contract fragment %q", relative, fragment)
			}
		}
		if strings.Contains(config, "location /r/ {\n        proxy_pass http://marketplace_api;") {
			t.Errorf("%s routes browser pages to the API", relative)
		}
	}
}
