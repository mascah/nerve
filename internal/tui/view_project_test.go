package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/hookstatus"
)

// newWorktreeTestView builds a projectView with a primary service so the Worktrees tab
// is active and rendering exercises the port/state/hook columns. No git work happens
// here — rows are injected directly via worktreesLoadedMsg.
func newWorktreeTestView() *projectView {
	cfg := config.Defaults()
	return &projectView{
		name:       "demo",
		path:       "/tmp/demo",
		cfg:        &cfg,
		tab:        tabWorktrees,
		confirmIdx: -1,
	}
}

func TestProjectView_WorktreesLoadedPopulatesRows(t *testing.T) {
	v := newWorktreeTestView()
	v.loadingWorktrees = true

	rows := []worktreeRow{
		{Branch: "feat-a", Path: "/wt/a", State: "clean"},
		{Branch: "feat-b", Path: "/wt/b", State: "dirty (2 files)", DirtyCount: 2},
	}
	cmd := v.Update(worktreesLoadedMsg{rows: rows})
	if cmd != nil {
		t.Fatalf("expected nil cmd after worktreesLoadedMsg, got non-nil")
	}
	if v.loadingWorktrees {
		t.Error("loadingWorktrees should be false after load completes")
	}
	if !v.loadedWorktrees {
		t.Error("loadedWorktrees should be true after load completes")
	}
	if len(v.worktrees) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(v.worktrees))
	}

	// The tab label count must now be visible without tabbing.
	out := v.View()
	if !strings.Contains(out, "Worktrees (2)") {
		t.Errorf("expected 'Worktrees (2)' tab label; got:\n%s", out)
	}
}

func TestProjectView_WorktreesLoadingPlaceholder(t *testing.T) {
	v := newWorktreeTestView()
	v.loadingWorktrees = true

	out := v.View()
	if !strings.Contains(out, "loading worktrees…") {
		t.Errorf("expected loading placeholder while loading; got:\n%s", out)
	}
	// Count not yet known → pending ellipsis on the tab label.
	if !strings.Contains(out, "Worktrees (…)") {
		t.Errorf("expected pending '(…)' count before load; got:\n%s", out)
	}
}

func TestProjectView_WorktreesLoadedError(t *testing.T) {
	v := newWorktreeTestView()
	v.loadingWorktrees = true

	cmd := v.Update(worktreesLoadedMsg{err: errors.New("boom")})
	if cmd != nil {
		t.Fatal("expected nil cmd on load error")
	}
	if v.worktrees != nil {
		t.Error("rows should be nil on load error")
	}
	if !strings.Contains(v.status, "boom") {
		t.Errorf("expected error in status banner, got %q", v.status)
	}
}

func TestProjectView_DirtyConfirmIncludesCount(t *testing.T) {
	v := newWorktreeTestView()
	v.loadedWorktrees = true
	v.worktrees = []worktreeRow{
		{Branch: "feat-x", Path: "/wt/x", State: "dirty (7 files)", DirtyCount: 7},
	}
	v.cursors[tabWorktrees] = 0

	// First 'd' arms the confirm with a dirty warning that names the count.
	if cmd := v.handleWorktreeDelete(); cmd != nil {
		t.Fatal("arming confirm should not return a cmd")
	}
	if v.confirmIdx != 0 {
		t.Errorf("expected confirmIdx=0 after arming, got %d", v.confirmIdx)
	}
	if !strings.Contains(v.status, "7 uncommitted changes") {
		t.Errorf("expected dirty count in confirm status, got %q", v.status)
	}
	if !strings.Contains(v.status, "feat-x") {
		t.Errorf("expected branch name in confirm status, got %q", v.status)
	}
}

func TestProjectView_CleanConfirmMessage(t *testing.T) {
	v := newWorktreeTestView()
	v.loadedWorktrees = true
	v.worktrees = []worktreeRow{
		{Branch: "feat-clean", Path: "/wt/clean", State: "clean"},
	}
	v.cursors[tabWorktrees] = 0

	if cmd := v.handleWorktreeDelete(); cmd != nil {
		t.Fatal("arming confirm should not return a cmd")
	}
	if v.status != "press d again to confirm removal, esc to cancel" {
		t.Errorf("unexpected clean confirm status: %q", v.status)
	}
}

func TestProjectView_SecondPressStartsAsyncRemove(t *testing.T) {
	v := newWorktreeTestView()
	v.loadedWorktrees = true
	v.worktrees = []worktreeRow{
		{Branch: "feat-x", Path: "/wt/x", State: "clean"},
	}
	v.cursors[tabWorktrees] = 0

	// Arm.
	v.handleWorktreeDelete()
	// Confirm — should return a removal command and not block.
	cmd := v.handleWorktreeDelete()
	if cmd == nil {
		t.Fatal("expected a removal command on confirming press")
	}
	if !v.removing {
		t.Error("removing guard should be set while removal is in flight")
	}
	if !strings.Contains(v.status, "removing feat-x") {
		t.Errorf("expected 'removing…' status, got %q", v.status)
	}
	// A further press while removing must be a no-op (guard).
	if again := v.handleWorktreeDelete(); again != nil {
		t.Error("expected no-op while removing is in flight")
	}
}

func TestProjectView_RemovedRefreshesOnSuccess(t *testing.T) {
	v := newWorktreeTestView()
	v.removing = true
	v.status = "removing feat-x…"

	cmd := v.Update(worktreeRemovedMsg{err: nil})
	if cmd == nil {
		t.Fatal("expected a refresh command after successful removal")
	}
	if v.removing {
		t.Error("removing guard should be cleared after removal completes")
	}
	if !v.loadingWorktrees {
		t.Error("loadingWorktrees should be set while the refresh is in flight")
	}
}

func TestProjectView_RemovedErrorSurfacesBanner(t *testing.T) {
	v := newWorktreeTestView()
	v.removing = true

	cmd := v.Update(worktreeRemovedMsg{err: errors.New("nope")})
	if cmd == nil {
		t.Fatal("expected an error cmd on removal failure")
	}
	if v.removing {
		t.Error("removing guard should be cleared on failure")
	}
	if msg, ok := cmd().(errMsg); !ok || !strings.Contains(msg.Error(), "nope") {
		t.Errorf("expected errMsg carrying the failure, got %#v", cmd())
	}
}

func TestHookStateLabel(t *testing.T) {
	cases := []struct {
		state hookstatus.State
		want  string // substring expected (styling stripped is impractical, match text)
	}{
		{hookstatus.StateRunning, "running"},
		{hookstatus.StateFailed, "failed"},
		{hookstatus.StateOK, "✓"},
		{hookstatus.State(""), ""},
	}
	for _, c := range cases {
		got := hookStateLabel(c.state)
		if c.want == "" {
			if got != "" {
				t.Errorf("state %q: expected empty label, got %q", c.state, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("state %q: expected label to contain %q, got %q", c.state, c.want, got)
		}
	}
}
