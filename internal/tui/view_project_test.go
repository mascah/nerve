package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

func TestProjectView_FocusPortsTabReturnsLoadCmd(t *testing.T) {
	cfg := config.Defaults()
	v := &projectView{
		name:       "demo",
		path:       "/tmp/demo",
		cfg:        &cfg,
		tab:        tabTemplates, // one shift+tab lands on tabPorts (the last tab)
		confirmIdx: -1,
	}

	// shift+tab from Templates wraps backward? No — Templates(2) → Worktrees(3) on tab.
	// Move forward twice: Templates → Worktrees → Ports.
	v.Update(keyMsg("tab")) // → Worktrees
	if v.tab != tabWorktrees {
		t.Fatalf("expected to land on Worktrees, got tab %d", v.tab)
	}
	cmd := v.Update(keyMsg("tab")) // → Ports, should trigger lazy load
	if v.tab != tabPorts {
		t.Fatalf("expected to land on Ports, got tab %d", v.tab)
	}
	if !v.loadingPorts {
		t.Error("loadingPorts should be set after focusing the Ports tab")
	}
	if cmd == nil {
		t.Fatal("expected a load cmd on first focus of the Ports tab")
	}
	if _, ok := cmd().(portsLoadedMsg); !ok {
		t.Errorf("expected the load cmd to produce a portsLoadedMsg, got %T", cmd())
	}

	// Re-focusing must not re-trigger a load once loaded.
	v.loadingPorts = false
	v.loadedPorts = true
	v.tab = tabWorktrees
	if again := v.Update(keyMsg("tab")); again != nil {
		t.Error("expected no load cmd when re-focusing an already-loaded Ports tab")
	}
}

func TestProjectView_PortsLoadedPopulatesRows(t *testing.T) {
	cfg := config.Defaults()
	cfg.Services = []config.Service{{ID: "web", BasePort: 3000, EnvKey: "WEB_PORT", Primary: true}}
	v := &projectView{
		name:         "demo",
		path:         "/tmp/demo",
		cfg:          &cfg,
		tab:          tabPorts,
		confirmIdx:   -1,
		loadingPorts: true,
	}

	rows := []portsRow{
		{Offset: 1, Branch: "", Ports: []portCell{{ServiceID: "web", Port: 3001, Listening: false}}},
		{Offset: 2, Branch: "feat", Ports: []portCell{{ServiceID: "web", Port: 3002, Listening: true}}},
	}
	// Park the cursor out of bounds to confirm clampPortsCursor runs without panicking
	// on the [5]int array.
	v.cursors[tabPorts] = 9

	cmd := v.Update(portsLoadedMsg{rows: rows})
	if cmd != nil {
		t.Fatalf("expected nil cmd after portsLoadedMsg, got non-nil")
	}
	if v.loadingPorts {
		t.Error("loadingPorts should be false after load completes")
	}
	if !v.loadedPorts {
		t.Error("loadedPorts should be true after load completes")
	}
	if len(v.ports) != 2 {
		t.Fatalf("expected 2 port rows, got %d", len(v.ports))
	}
	if v.cursors[tabPorts] != 1 {
		t.Errorf("cursor should be clamped to last row (1), got %d", v.cursors[tabPorts])
	}

	// Rendering the Ports tab must not panic and should show the grid.
	out := v.View()
	if !strings.Contains(out, "OFFSET") {
		t.Errorf("expected Ports grid header in render; got:\n%s", out)
	}
	if !strings.Contains(out, "feat") {
		t.Errorf("expected allocated branch in render; got:\n%s", out)
	}
}

func TestProjectView_PortsLightweightPlaceholder(t *testing.T) {
	// A config with no services is lightweight — the Ports tab shows a hint, never probes.
	v := &projectView{
		name:        "demo",
		path:        "/tmp/demo",
		cfg:         &config.ProjectConfig{},
		tab:         tabPorts,
		confirmIdx:  -1,
		loadedPorts: true,
	}
	out := v.View()
	if !strings.Contains(out, "no services configured") {
		t.Errorf("expected lightweight placeholder on Ports tab; got:\n%s", out)
	}
}

// keyMsg builds a tea.KeyMsg for the given key string ("tab", "r", etc.) so Update can
// be exercised without a real terminal.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
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
