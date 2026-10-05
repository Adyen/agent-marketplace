package tui

import (
	"charm.land/lipgloss/v2"
)

// compositeModal places the rendered modal centered over the base screen
// using Lip Gloss layer composition. Lines of the modal that would fall
// outside the base bounds are clipped by the compositor.
func compositeModal(base, modal string, width, height int) string {
	if modal == "" {
		return base
	}
	col := max(0, (width-lipgloss.Width(modal))/2)
	row := max(0, (height-lipgloss.Height(modal))/2)
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	return canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(modal).X(col).Y(row).Z(1),
	)).Render()
}

// toastStackLayer renders the visible toast stack for top-right layer
// composition, one sanitized single-line toast per row.
func toastStackLayer(toasts []queuedToast) string {
	if len(toasts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(toasts))
	for _, t := range toasts {
		lines = append(lines, styleToast.Render(sanitizeTerminalData(t.text, terminalSingleLine)))
	}
	return lipgloss.JoinVertical(lipgloss.Right, lines...)
}

// compositeToasts overlays the toast stack at the top-right of the base
// screen — below the header so controls and the footer stay unobscured —
// using the same Lip Gloss compositor as modal overlays.
func compositeToasts(base string, toasts []queuedToast, width, height int) string {
	stack := toastStackLayer(toasts)
	if stack == "" {
		return base
	}
	canvas := lipgloss.NewCanvas(max(1, width), max(1, height))
	return canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(stack).X(max(0, width-lipgloss.Width(stack))).Y(1).Z(2),
	)).Render()
}
