package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// spec: marketplace-worker-telemetry/helm-alerts-distinguish-releases
func TestAlertReleaseIdentity(t *testing.T) {
	if os.Getenv("AM_HELM_CHECK") != "1" {
		t.Skip("the Helm chart gate enables real chart rendering")
	}

	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Fatal("helm is required to run TestAlertReleaseIdentity; install Helm 3 and make it available on PATH")
	}
	chartDir, err := filepath.Abs(filepath.Join("..", "..", "deploy", "helm", "agent-marketplace"))
	if err != nil {
		t.Fatalf("resolve chart directory: %v", err)
	}

	releases := []struct {
		name      string
		namespace string
	}{
		{name: "marketplace", namespace: "marketplace"},
		{name: "marketplace", namespace: "other-marketplace"},
		{name: "another-marketplace", namespace: "marketplace"},
	}

	for _, mode := range []string{"all", "split"} {
		t.Run(mode, func(t *testing.T) {
			rendered := make([]map[string]map[string]string, len(releases))
			for i, release := range releases {
				rendered[i] = renderPrometheusRule(t, helm, chartDir, release.name, release.namespace, mode)
				if len(rendered[i]) != 10 {
					t.Fatalf("release %s/%s rendered %d alerts, want 10", release.namespace, release.name, len(rendered[i]))
				}
				for alert, labels := range rendered[i] {
					if labels["namespace"] != release.namespace {
						t.Errorf("%s labels.namespace = %q, want %q", alert, labels["namespace"], release.namespace)
					}
					if labels["release"] != release.name {
						t.Errorf("%s labels.release = %q, want %q", alert, labels["release"], release.name)
					}
					if labels["team"] != "operations" {
						t.Errorf("%s labels.team = %q, want operations", alert, labels["team"])
					}
				}
			}

			for alert := range rendered[0] {
				for i := range rendered {
					for j := i + 1; j < len(rendered); j++ {
						if reflect.DeepEqual(rendered[i][alert], rendered[j][alert]) {
							t.Errorf("%s labels are identical for releases %s/%s and %s/%s", alert,
								releases[i].namespace, releases[i].name, releases[j].namespace, releases[j].name)
						}
					}
				}
			}
		})
	}
}

func renderPrometheusRule(t *testing.T, helm, chartDir, releaseName, namespace, mode string) map[string]map[string]string {
	t.Helper()

	cmd := exec.Command(helm, "template", releaseName, chartDir,
		"--namespace", namespace,
		"--show-only", "templates/prometheusrule.yaml",
		"--set-string", "image.tag=ci",
		"--set", "prometheusRule.enabled=true",
		"--set", "fullnameOverride=agent-marketplace",
		"--set", "mode="+mode,
		"--set-string", "prometheusRule.ruleLabels.team=operations",
		"--set-string", "prometheusRule.ruleLabels.namespace=forged-namespace",
		"--set-string", "prometheusRule.ruleLabels.release=forged-release",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm template for release %s/%s: %v\n%s", namespace, releaseName, err, output)
	}

	var document struct {
		Spec struct {
			Groups []struct {
				Rules []struct {
					Alert  string            `yaml:"alert"`
					Labels map[string]string `yaml:"labels"`
				} `yaml:"rules"`
			} `yaml:"groups"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(output, &document); err != nil {
		t.Fatalf("parse rendered PrometheusRule for release %s/%s: %v", namespace, releaseName, err)
	}

	alerts := make(map[string]map[string]string)
	for _, group := range document.Spec.Groups {
		for _, rule := range group.Rules {
			if _, exists := alerts[rule.Alert]; exists {
				t.Fatalf("rendered duplicate alert %q", rule.Alert)
			}
			alerts[rule.Alert] = rule.Labels
		}
	}
	return alerts
}
