// Package session holds the surface-neutral interactive-session core:
// staged snapshots, rows and search indexing, generation isolation,
// mutation gating, progress observation, diagnostics, guarded actions, and
// first-configuration detection. It must never depend on Bubble Tea,
// Lipgloss, Huh, Bubbles, or any other charm.land/charmbracelet package.
package session
