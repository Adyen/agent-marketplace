package monitoring

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

type dashboard struct {
	UID        string  `json:"uid"`
	Panels     []panel `json:"panels"`
	Templating struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	} `json:"templating"`
}

type panel struct {
	ID      int `json:"id"`
	Targets []struct {
		Expr  string `json:"expr"`
		RefID string `json:"refId"`
	} `json:"targets"`
}

func loadDashboard(t *testing.T) dashboard {
	t.Helper()
	data, err := os.ReadFile("agent-marketplace-telemetry-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var value dashboard
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDashboardContract(t *testing.T) {
	value := loadDashboard(t)
	if value.UID != "agent-marketplace-telemetry" {
		t.Fatalf("uid = %q", value.UID)
	}

	metricPattern := regexp.MustCompile(`agent_marketplace_receiver_[a-z_]+`)
	metrics := map[string]struct{}{}
	targets := map[string]struct{}{}
	for _, panel := range value.Panels {
		for _, target := range panel.Targets {
			key := fmt.Sprintf("%d/%s", panel.ID, target.RefID)
			if _, duplicate := targets[key]; duplicate {
				t.Fatalf("duplicate target %s", key)
			}
			targets[key] = struct{}{}
			for _, name := range metricPattern.FindAllString(target.Expr, -1) {
				metrics[name] = struct{}{}
			}
		}
	}
	if len(targets) != 21 {
		t.Fatalf("target count = %d, want 21", len(targets))
	}

	wantMetrics := []string{
		"agent_marketplace_receiver_active_installations",
		"agent_marketplace_receiver_command_duration_milliseconds_bucket",
		"agent_marketplace_receiver_datapoints_total",
		"agent_marketplace_receiver_harness_invocations_total",
		"agent_marketplace_receiver_installation_budget_limit",
		"agent_marketplace_receiver_installation_budget_used",
		"agent_marketplace_receiver_invocations_total",
		"agent_marketplace_receiver_last_receive_timestamp_seconds",
		"agent_marketplace_receiver_requests_total",
		"agent_marketplace_receiver_series_budget_limit",
		"agent_marketplace_receiver_series_budget_used",
		"agent_marketplace_receiver_storage_bytes",
	}
	var gotMetrics []string
	for name := range metrics {
		gotMetrics = append(gotMetrics, name)
	}
	sort.Strings(gotMetrics)
	if strings.Join(gotMetrics, "\n") != strings.Join(wantMetrics, "\n") {
		t.Fatalf("metric inventory:\n%s", strings.Join(gotMetrics, "\n"))
	}

	var variables []string
	for _, variable := range value.Templating.List {
		variables = append(variables, variable.Name)
	}
	if strings.Join(variables, ",") != "datasource,job,command,harness,cli_version" {
		t.Fatalf("variables = %v", variables)
	}

	for _, panel := range value.Panels {
		if panel.ID != 4 && panel.ID != 5 && panel.ID != 6 && (panel.ID < 10 || panel.ID > 14) {
			continue
		}
		for _, target := range panel.Targets {
			if strings.Contains(target.Expr, "increase(") || strings.Contains(target.Expr, "rate(") {
				t.Fatalf("retained-total panel %d hides first events: %s", panel.ID, target.Expr)
			}
		}
	}
}

func TestDashboardPromQL(t *testing.T) {
	endpoint := os.Getenv("PROMETHEUS_URL")
	if endpoint == "" {
		t.Skip("PROMETHEUS_URL is not set")
	}
	value := loadDashboard(t)
	client := &http.Client{Timeout: 5 * time.Second}
	replacer := strings.NewReplacer(
		"$__rate_interval", "1m",
		"$__range", "5m",
		"$job", ".*",
		"$command", ".*",
		"$harness", ".*",
		"$cli_version", ".*",
	)
	for _, panel := range value.Panels {
		for _, target := range panel.Targets {
			query := replacer.Replace(target.Expr)
			resp, err := client.PostForm(strings.TrimRight(endpoint, "/")+"/api/v1/query", url.Values{"query": {query}})
			if err != nil {
				t.Fatalf("panel %d/%s: %v", panel.ID, target.RefID, err)
			}
			var result struct {
				Status string `json:"status"`
				Error  string `json:"error"`
			}
			err = json.NewDecoder(resp.Body).Decode(&result)
			_ = resp.Body.Close()
			if err != nil || resp.StatusCode/100 != 2 || result.Status != "success" {
				t.Fatalf("panel %d/%s query %q: status=%d result=%+v err=%v", panel.ID, target.RefID, query, resp.StatusCode, result, err)
			}
		}
	}
}
