package tui

import (
	"strings"
	"testing"
)

// TestProjectsView_LoadingPlaceholder verifies the projects list shows a placeholder
// until the async per-project status load lands (bug #2 fix: the git fork-fest moved off
// the UI loop). Skeleton rows carry name/path but the status table is deferred.
func TestProjectsView_LoadingPlaceholder(t *testing.T) {
	v := &projectsView{
		rows: []projectRow{
			{name: "alpha", path: "/repos/alpha"},
			{name: "beta", path: "/repos/beta"},
		},
	}
	out := v.View()
	if !strings.Contains(out, "loading projects…") {
		t.Errorf("expected loading placeholder before load completes; got:\n%s", out)
	}
	if !strings.Contains(out, "2 registered") {
		t.Errorf("expected registered count from skeleton rows; got:\n%s", out)
	}
	// The status table header must not render until loaded.
	if strings.Contains(out, "WORKTREES") {
		t.Errorf("status table should not render before load; got:\n%s", out)
	}
}

// TestProjectsView_LoadedRendersTable verifies the status table renders once the async
// projectsLoadedMsg lands, replacing the placeholder.
func TestProjectsView_LoadedRendersTable(t *testing.T) {
	v := &projectsView{
		rows: []projectRow{{name: "alpha", path: "/repos/alpha"}},
	}
	cmd := v.Update(projectsLoadedMsg{rows: []projectRow{
		{name: "alpha", path: "/repos/alpha", configured: true, worktrees: 3, allocations: 2},
	}})
	if cmd != nil {
		t.Fatalf("expected nil cmd after projectsLoadedMsg, got non-nil")
	}
	if !v.loaded {
		t.Fatal("loaded should be true after projectsLoadedMsg")
	}
	out := v.View()
	if !strings.Contains(out, "WORKTREES") {
		t.Errorf("expected status table header after load; got:\n%s", out)
	}
	if !strings.Contains(out, "configured") {
		t.Errorf("expected configured mode after load; got:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected project name after load; got:\n%s", out)
	}
}

// TestProjectsView_LoadedClampsCursor verifies the cursor is clamped if a reload returns
// fewer rows than before (e.g. a project was removed out-of-band).
func TestProjectsView_LoadedClampsCursor(t *testing.T) {
	v := &projectsView{
		rows: []projectRow{
			{name: "a", path: "/a"},
			{name: "b", path: "/b"},
			{name: "c", path: "/c"},
		},
		cursor: 2,
	}
	v.Update(projectsLoadedMsg{rows: []projectRow{{name: "a", path: "/a"}}})
	if v.cursor != 0 {
		t.Errorf("cursor should be clamped to last row (0), got %d", v.cursor)
	}
}

// TestProjectsView_LoadedErrorKeepsSkeleton verifies a load error marks the view loaded
// (so it stops showing the placeholder) without clobbering the skeleton rows that still
// allow navigation.
func TestProjectsView_LoadedErrorKeepsSkeleton(t *testing.T) {
	v := &projectsView{
		rows: []projectRow{{name: "alpha", path: "/repos/alpha"}},
	}
	v.Update(projectsLoadedMsg{err: errTest})
	if !v.loaded {
		t.Error("loaded should be true even on error so the placeholder clears")
	}
	if len(v.rows) != 1 || v.rows[0].name != "alpha" {
		t.Errorf("skeleton rows should survive a load error, got %+v", v.rows)
	}
}

// errTest is a sentinel error for the load-error case.
var errTest = errTestType("load failed")

type errTestType string

func (e errTestType) Error() string { return string(e) }
