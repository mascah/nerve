package tui

// asyncList is a small passive holder for a tab whose rows are loaded off the UI loop
// via a tea.Cmd. It collapses the duplicated "rows + loading + loaded" triple that the
// Worktrees and Ports tabs each carried. It is deliberately passive — no Update/View —
// because the tabs have divergent key/render logic; the owning projectView still holds
// the load tea.Cmds and removal-confirm state, so no I/O moves here and the non-blocking
// invariant is preserved.
type asyncList[T any] struct {
	rows []T
	// loading is true while a load command is in flight; drives the "loading…"
	// placeholder and the pending-count tab label.
	loading bool
	// loaded is true once a load has completed (success or error).
	loaded bool
}

// clamp keeps a cursor within bounds after the row count changes. Exact rule: if the
// cursor is past the end and not already at zero, snap it to the last row; otherwise
// leave it (so an empty list keeps cursor 0).
func (l *asyncList[T]) clamp(cursor int) int {
	if cursor >= len(l.rows) && cursor > 0 {
		return len(l.rows) - 1
	}
	return cursor
}

// len reports the number of loaded rows.
func (l *asyncList[T]) len() int { return len(l.rows) }

// set records a successful load: stores rows and marks loaded (not loading).
func (l *asyncList[T]) set(rows []T) {
	l.rows = rows
	l.loading = false
	l.loaded = true
}

// fail records a failed load: clears rows (nil, not empty) and marks loaded (not loading)
// so the tab shows its empty state rather than a stale list.
func (l *asyncList[T]) fail() {
	l.rows = nil
	l.loading = false
	l.loaded = true
}

// begin marks a load as in flight.
func (l *asyncList[T]) begin() { l.loading = true }
