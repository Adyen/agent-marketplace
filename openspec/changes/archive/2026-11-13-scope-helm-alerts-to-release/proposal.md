# Scope Helm worker alerts to their release

## Why

Alerts from separate Helm releases can otherwise have identical label sets. Custom alert labels must not replace the release identity.

## What Changes

- Add fixed Helm release namespace and name labels to every worker alert.
- Retain custom alert labels other than `namespace` and `release`.
- Verify rendered rules in both chart modes without changing alert expressions, thresholds, or durations.

## Release note

Helm worker alerts now carry the release namespace and name so alerts from separate releases remain distinct.
