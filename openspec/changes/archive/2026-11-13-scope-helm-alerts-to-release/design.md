# Design

The shared PrometheusRule alert-label helper adds `namespace` and `release` from the Helm release context. It omits those keys from `prometheusRule.ruleLabels`, while retaining every other custom label and the existing severity label.

A real Helm render test checks all ten alerts in `all` and `split` modes across releases that differ by namespace and name. Existing promtool firing expectations gain the two identity labels. The chart README documents the reserved labels.

Alert expressions, thresholds, durations, and rule selection remain unchanged.
