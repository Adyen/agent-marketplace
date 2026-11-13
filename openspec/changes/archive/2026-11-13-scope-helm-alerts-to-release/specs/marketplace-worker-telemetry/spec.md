## ADDED Requirements

### Requirement: Helm worker alerts carry release identity

When prometheusRule.enabled is true, every Helm worker alert SHALL carry namespace and release labels set to the Helm release namespace and name. Custom prometheusRule.ruleLabels SHALL NOT replace either identity label. Other custom rule labels SHALL be retained.

#### Scenario: Helm alerts distinguish releases

- **WHEN** the chart renders enabled worker alerts in all and split modes for releases with the same name in different namespaces or different names in the same namespace, including custom rule labels that attempt to replace namespace and release
- **THEN** every alert carries its actual Helm release namespace and name, alert label sets distinguish the releases, and other custom rule labels remain present
