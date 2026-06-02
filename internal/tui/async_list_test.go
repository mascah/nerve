package tui

import "testing"

func TestAsyncList_SetAndFail(t *testing.T) {
	var l asyncList[int]
	l.begin()
	if !l.loading || l.loaded {
		t.Fatalf("after begin: loading=%v loaded=%v, want loading=true loaded=false", l.loading, l.loaded)
	}

	l.set([]int{1, 2, 3})
	if l.loading || !l.loaded {
		t.Fatalf("after set: loading=%v loaded=%v, want loading=false loaded=true", l.loading, l.loaded)
	}
	if l.len() != 3 {
		t.Fatalf("len = %d, want 3", l.len())
	}
	if l.rows == nil {
		t.Error("rows should be non-nil after a successful set")
	}

	l.fail()
	if l.loading || !l.loaded {
		t.Fatalf("after fail: loading=%v loaded=%v, want loading=false loaded=true", l.loading, l.loaded)
	}
	if l.rows != nil {
		t.Error("rows should be nil after fail")
	}
	if l.len() != 0 {
		t.Errorf("len = %d after fail, want 0", l.len())
	}
}

func TestAsyncList_Clamp(t *testing.T) {
	l := asyncList[int]{rows: []int{10, 20}}
	cases := []struct {
		cursor, want int
	}{
		{0, 0}, // in range
		{1, 1}, // last row
		{2, 1}, // past end → snap to last
		{9, 1}, // far past end → snap to last
	}
	for _, c := range cases {
		if got := l.clamp(c.cursor); got != c.want {
			t.Errorf("clamp(%d) with 2 rows = %d, want %d", c.cursor, got, c.want)
		}
	}

	// Empty list: cursor 0 stays 0 (the cursor>0 guard isn't met).
	var empty asyncList[int]
	if got := empty.clamp(0); got != 0 {
		t.Errorf("clamp(0) on empty = %d, want 0", got)
	}
	// Per the exact rule (cursor>=len && cursor>0 -> len-1), a positive cursor on an
	// empty list collapses to len-1 == -1. In practice callers never park a positive
	// cursor on an empty tab, so this is just documenting the rule.
	if got := empty.clamp(5); got != -1 {
		t.Errorf("clamp(5) on empty = %d, want -1 per the len-1 rule", got)
	}
}
