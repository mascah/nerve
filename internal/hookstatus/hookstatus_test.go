package hookstatus

import (
	"testing"
	"time"
)

func TestWriteReadRoundTrip(t *testing.T) {
	repo := t.TempDir()
	want := Status{
		State:         StateFailed,
		PID:           4242,
		StartedAt:     time.Now().Truncate(time.Second),
		FinishedAt:    time.Now().Truncate(time.Second).Add(5 * time.Second),
		ExitCode:      3,
		FailedCommand: "pnpm i",
	}
	if err := Write(repo, "feat_x", want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, found, err := Read(repo, "feat_x")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatalf("Read: found = false; want true")
	}
	if got.State != want.State || got.PID != want.PID || got.ExitCode != want.ExitCode || got.FailedCommand != want.FailedCommand {
		t.Fatalf("Read returned %+v; want %+v", got, want)
	}
	if !got.StartedAt.Equal(want.StartedAt) || !got.FinishedAt.Equal(want.FinishedAt) {
		t.Fatalf("timestamps mismatch: got start=%v finish=%v", got.StartedAt, got.FinishedAt)
	}
}

func TestReadMissingIsNotFound(t *testing.T) {
	repo := t.TempDir()
	got, found, err := Read(repo, "nope")
	if err != nil {
		t.Fatalf("Read missing: unexpected err %v", err)
	}
	if found {
		t.Fatalf("Read missing: found = true; want false")
	}
	if got != (Status{}) {
		t.Fatalf("Read missing: status = %+v; want zero value", got)
	}
}

func TestClearRemovesStatus(t *testing.T) {
	repo := t.TempDir()
	if err := Write(repo, "feat_x", Status{State: StateRunning, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Clear(repo, "feat_x"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, found, _ := Read(repo, "feat_x"); found {
		t.Fatalf("Read after Clear: found = true; want false")
	}
	// Clearing a slug with no status is not an error.
	if err := Clear(repo, "feat_x"); err != nil {
		t.Fatalf("Clear (missing): %v", err)
	}
}

func TestStatusDone(t *testing.T) {
	cases := []struct {
		state State
		done  bool
	}{
		{StateRunning, false},
		{StateOK, true},
		{StateFailed, true},
	}
	for _, c := range cases {
		if got := (Status{State: c.state}).Done(); got != c.done {
			t.Errorf("Status{%s}.Done() = %v; want %v", c.state, got, c.done)
		}
	}
}
