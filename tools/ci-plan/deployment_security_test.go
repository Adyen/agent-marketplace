package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// spec: marketplace-delivery-security/secret-scanner-failure-fails-the-job
func TestSecretScanFailsClosed(t *testing.T) {
	jobs := getMap(t, loadWorkflow(t, "trufflehog.yml"), "jobs")
	job := getMap(t, jobs, "trufflehog")
	if job["continue-on-error"] != nil && job["continue-on-error"] != false {
		t.Fatal("secret scan job must not tolerate failure")
	}
	found := false
	for _, step := range getSlice(t, job, "steps") {
		m := stepMap(t, step)
		if condition, ok := m["if"].(string); ok && strings.Contains(condition, "steps.trufflehog.outcome") {
			t.Errorf("step %v must not repair the scanner's failure status", m["name"])
		}
		if m["name"] != "TruffleHog OSS" {
			continue
		}
		if m["continue-on-error"] != nil && m["continue-on-error"] != false {
			t.Error("the scanner itself must fail closed")
		}
		if m["if"] != nil {
			t.Error("the scanner must run unconditionally")
		}
		if !strings.HasPrefix(m["uses"].(string), "trufflesecurity/trufflehog@") {
			t.Error("secret scanner action is missing")
		}
		found = true
	}
	if !found {
		t.Fatal("TruffleHog step missing")
	}
}

// spec: marketplace-delivery-security/web-runtime-is-independent-of-the-builder
// spec: marketplace-delivery-security/web-healthcheck-has-an-explicit-request-deadline
func TestWebRuntimeIsolation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "web", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, want := range []string{
		"ARG NODE_IMAGE=node:24-trixie-slim",
		"ARG NODE_RUNTIME_IMAGE=gcr.io/distroless/nodejs24-debian13:nonroot",
		"FROM ${NODE_IMAGE} AS build",
		"FROM ${NODE_RUNTIME_IMAGE} AS runtime",
		"USER 1000:1000",
		`ENTRYPOINT ["/nodejs/bin/node"]`,
		`CMD [".output/server/index.mjs"]`,
		`CMD ["/nodejs/bin/node", "-e",`,
		"signal: AbortSignal.timeout(4000)",
		".catch(() => process.exit(1))",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("web Dockerfile missing %q", want)
		}
	}
}

// spec: marketplace-delivery-security/helm-refuses-an-unspecified-image-version
// spec: marketplace-delivery-security/helm-digests-take-precedence-for-each-enabled-workload
func TestHelmImageSelection(t *testing.T) {
	if os.Getenv("AM_HELM_CHECK") != "1" {
		t.Skip("the Helm chart gate enables real chart rendering")
	}
	chart := filepath.Join("..", "..", "deploy", "helm", "agent-marketplace")
	digest := "sha256:" + strings.Repeat("a", 64)
	webDigest := "sha256:" + strings.Repeat("b", 64)
	docsDigest := "sha256:" + strings.Repeat("c", 64)
	for _, tc := range []struct {
		name   string
		values string
		set    []string
		want   []string
		fail   bool
	}{
		{name: "missing version", fail: true},
		{name: "explicit tag", set: []string{"image.tag=dev"}, want: []string{"agent-marketplace:dev"}},
		{name: "digest wins", set: []string{"image.tag=ignored", "image.digest=" + digest}, want: []string{"agent-marketplace@" + digest}},
		{name: "web needs its own version", set: []string{"image.tag=dev", "web.enabled=true"}, fail: true},
		{name: "docs needs its own version", set: []string{"image.tag=dev", "docs.enabled=true"}, fail: true},
		{name: "null web digest cannot inherit the backend digest", set: []string{"image.digest=" + digest, "web.enabled=true", "web.image.digest=null"}, fail: true},
		{name: "null docs digest cannot inherit the backend digest", set: []string{"image.digest=" + digest, "docs.enabled=true", "docs.image.digest=null"}, fail: true},
		{
			name: "split digest inheritance and frontend isolation",
			set: []string{
				"mode=split", "image.tag=ignored", "image.digest=" + digest,
				"api.image.repository=example.invalid/api",
				"web.enabled=true", "web.image.digest=" + webDigest,
				"docs.enabled=true", "docs.image.digest=" + docsDigest,
			},
			want: []string{"example.invalid/api@" + digest, "agent-marketplace@" + digest, "agent-marketplace-web@" + webDigest, "agent-marketplace-docs@" + docsDigest},
		},
		{
			name: "split role overrides",
			set:  []string{"mode=split", "api.image.tag=api-dev", "worker.image.digest=" + digest},
			want: []string{"agent-marketplace:api-dev", "agent-marketplace@" + digest},
		},
		{name: "previous values require explicit versions", values: "helm-pre-hardening-values.yaml", fail: true},
		{name: "previous values need a web version", values: "helm-pre-hardening-values.yaml", set: []string{"image.digest=" + digest}, fail: true},
		{name: "previous values need a docs version", values: "helm-pre-hardening-values.yaml", set: []string{"image.digest=" + digest, "web.image.digest=" + webDigest}, fail: true},
		{
			name:   "previous values upgraded with explicit digests",
			values: "helm-pre-hardening-values.yaml",
			set:    []string{"image.digest=" + digest, "web.image.digest=" + webDigest, "docs.image.digest=" + docsDigest},
			want:   []string{"example.invalid/marketplace-worker@" + digest, "example.invalid/marketplace-web@" + webDigest, "example.invalid/marketplace-docs@" + docsDigest},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"template", "marketplace", chart}
			if tc.values != "" {
				args = append(args, "--values", filepath.Join("testdata", tc.values))
			}
			for _, value := range tc.set {
				args = append(args, "--set", value)
			}
			out, err := exec.Command("helm", args...).CombinedOutput()
			if tc.fail {
				if err == nil {
					t.Fatal("expected missing-version refusal, rendering succeeded")
				}
				if !strings.Contains(string(out), "set an explicit image.tag or image.digest") {
					t.Fatalf("expected missing-version refusal, got %v:\n%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("render chart: %v:\n%s", err, out)
			}
			decoder := yaml.NewDecoder(strings.NewReader(string(out)))
			var images []string
			for {
				var doc struct {
					Kind string
					Spec struct {
						Template struct {
							Spec struct {
								Containers []struct{ Image string }
							}
						}
					}
				}
				if err := decoder.Decode(&doc); err != nil {
					if !errors.Is(err, io.EOF) {
						t.Fatal(err)
					}
					break
				}
				if doc.Kind == "Deployment" {
					for _, container := range doc.Spec.Template.Spec.Containers {
						images = append(images, container.Image)
					}
				}
			}
			if len(images) != len(tc.want) {
				t.Fatalf("images = %v, want %v", images, tc.want)
			}
			for _, want := range tc.want {
				if !slices.Contains(images, want) {
					t.Errorf("images = %v, missing %q", images, want)
				}
			}
		})
	}
}

// TestChartPublisherUsesExplicitImageVersion keeps release validation independent
// of the intentionally empty deployment defaults.
func TestChartPublisherUsesExplicitImageVersion(t *testing.T) {
	jobs := getMap(t, loadWorkflow(t, "marketplace-chart.yml"), "jobs")
	for _, step := range getSlice(t, getMap(t, jobs, "publish"), "steps") {
		m := stepMap(t, step)
		if m["name"] != "Package release chart" {
			continue
		}
		run := m["run"].(string)
		for _, command := range []string{
			`helm lint --strict "$chart_dir" --set-string image.tag="$RELEASE_TAG"`,
			`helm template marketplace "$package" --namespace marketplace --set-string image.tag="$RELEASE_TAG"`,
		} {
			if !strings.Contains(run, command) {
				t.Errorf("publisher must validate with an explicit version: missing %q", command)
			}
		}
		return
	}
	t.Fatal("release chart packaging step missing")
}
