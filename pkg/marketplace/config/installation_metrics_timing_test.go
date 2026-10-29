package config

import (
	"strings"
	"testing"
	"time"
)

// spec: marketplace-api/existing-configuration-uses-defaults-independently-of-reindexing
func TestInstallationMetricsTimingDefaultsIndependentOfReindex(t *testing.T) {
	yaml := strings.Replace(validConfigYAML(), "  interval: 30m\n", "  interval: 7m\n", 1)
	cfg, err := LoadConfig(writeConfig(t, yaml+"installation_metrics:\n  url: https://metrics.internal\n"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	timing := ResolveInstallationMetricsTiming(cfg.InstallationMetrics)
	want := InstallationMetricsTiming{
		Interval: 4 * time.Minute, RetryInterval: 30 * time.Second,
		Jitter: 15 * time.Second, MaxAge: 5 * time.Minute, MaxStale: 24 * time.Hour, Timeout: 3 * time.Second,
	}
	if timing != want {
		t.Fatalf("resolved timing = %+v, want defaults %+v", timing, want)
	}

	absent, err := LoadConfig(writeConfig(t, validConfigYAML()))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if ResolveInstallationMetricsTiming(absent.InstallationMetrics) != want {
		t.Fatal("absent installation_metrics did not resolve to defaults")
	}

	if ResolveInstallationMetricsTiming(InstallationMetrics{}) != want {
		t.Fatal("zero-value InstallationMetrics did not resolve to defaults")
	}
}

// spec: marketplace-api/timing-overrides-and-explicit-zero-jitter-are-honored
func TestInstallationMetricsTimingOverrides(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfigYAML()+`installation_metrics:
  url: https://metrics.internal
  max_age: 30m
  max_stale: 2h
  timeout: 5s
  refresh:
    interval: 10m
    retry_interval: 2m
    jitter: 0s
`))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	timing := ResolveInstallationMetricsTiming(cfg.InstallationMetrics)
	want := InstallationMetricsTiming{
		Interval: 10 * time.Minute, RetryInterval: 2 * time.Minute,
		Jitter: 0, MaxAge: 30 * time.Minute, MaxStale: 2 * time.Hour, Timeout: 5 * time.Second,
	}
	if timing != want {
		t.Fatalf("resolved timing = %+v, want overrides %+v", timing, want)
	}
	if cfg.InstallationMetrics.Refresh.Jitter == nil {
		t.Fatal("explicit zero jitter was not preserved as an explicit value")
	}
}

// spec: marketplace-api/omitted-max-stale-follows-a-longer-max-age
func TestInstallationMetricsTimingOmittedMaxStaleFollowsMaxAge(t *testing.T) {
	cfg, err := LoadConfig(writeConfig(t, validConfigYAML()+"installation_metrics:\n  url: https://metrics.internal\n  max_age: 25h\n"))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	timing := ResolveInstallationMetricsTiming(cfg.InstallationMetrics)
	if timing.MaxStale != 25*time.Hour {
		t.Fatalf("MaxStale = %v, want max_age 25h", timing.MaxStale)
	}
}

// spec: marketplace-api/invalid-timing-configuration-is-rejected
func TestInstallationMetricsTimingRejectsInvalidValues(t *testing.T) {
	block := func(inner string) string {
		return validConfigYAML() + "installation_metrics:\n  url: https://metrics.internal\n" + inner
	}
	tests := []struct {
		name string
		yaml string
	}{
		{name: "zero interval", yaml: block("  refresh:\n    interval: 0s\n")},
		{name: "negative interval", yaml: block("  refresh:\n    interval: -1m\n")},
		{name: "zero retry interval", yaml: block("  refresh:\n    retry_interval: 0s\n")},
		{name: "negative retry interval", yaml: block("  refresh:\n    retry_interval: -30s\n")},
		{name: "zero max age", yaml: block("  max_age: 0s\n")},
		{name: "negative max age", yaml: block("  max_age: -5m\n")},
		{name: "zero max stale", yaml: block("  max_stale: 0s\n")},
		{name: "negative max stale", yaml: block("  max_stale: -1h\n")},
		{name: "max stale shorter than max age", yaml: block("  max_age: 10m\n  max_stale: 5m\n")},
		{name: "zero timeout", yaml: block("  timeout: 0s\n")},
		{name: "negative timeout", yaml: block("  timeout: -2s\n")},
		{name: "negative jitter", yaml: block("  refresh:\n    jitter: -1s\n")},
		{name: "no freshness headroom", yaml: block("  max_age: 5m\n  refresh:\n    interval: 4m\n    jitter: 30s\n  timeout: 45s\n")},
		{name: "interval equal to max age", yaml: block("  max_age: 4m\n  refresh:\n    interval: 4m\n")},
		{name: "exact headroom equality", yaml: block("  max_age: 6m\n  timeout: 90s\n  refresh:\n    interval: 4m\n    jitter: 30s\n")},
		{name: "int64 overflow headroom", yaml: block("  max_age: 2562047h47m16.854775807s\n  timeout: 17s\n  refresh:\n    interval: 2562047h\n    jitter: 47m\n")},
		{name: "unknown refresh key", yaml: block("  refresh:\n    bogus: 1m\n")},
		{name: "unknown metrics key", yaml: block("  refresh_interval: 1m\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, test.yaml)); err == nil {
				t.Fatal("LoadConfig() accepted invalid installation_metrics timing")
			}
		})
	}

	metrics := InstallationMetrics{URL: "https://metrics.internal"}
	zero := Duration(0)
	metrics.Refresh.Interval = &zero
	if err := metrics.Validate(); len(err) == 0 {
		t.Fatal("Validate() accepted a zero refresh interval")
	}
}
