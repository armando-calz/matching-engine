package bitso

import (
	"slices"
	"testing"

	"github.com/armando-calz/matching-engine/orderbook"
)

func open(id string, side orderbook.Side, price orderbook.Price, amount orderbook.Quantity) Update {
	return Update{ID: id, Side: side, Price: price, Amount: amount, Status: Open}
}

func gone(id string, side orderbook.Side, s Status) Update {
	return Update{ID: id, Side: side, Status: s}
}

func diff(seq uint64, u ...Update) Diff { return Diff{Seq: seq, Updates: u} }

// snapshot at sequence 10: bids 100 ×(5, 2), 99 ×1; asks 101 ×3.
func snapshot() Snapshot {
	return Snapshot{Seq: 10, Orders: []Update{
		open("b1", orderbook.Buy, 100, 5), open("b2", orderbook.Buy, 100, 2), open("b3", orderbook.Buy, 99, 1),
		open("a1", orderbook.Sell, 101, 3),
	}}
}

func TestSnapshotAndQueuedDiffs(t *testing.T) {
	b := NewLocalBook()
	// Subscribed first: these arrive while the snapshot is being fetched.
	for _, d := range []Diff{
		diff(9, open("old", orderbook.Buy, 98, 1)),      // older than the snapshot: discarded
		diff(10, open("old2", orderbook.Buy, 98, 1)),    // already in the snapshot: discarded
		diff(11, open("b1", orderbook.Buy, 100, 4)),     // b1 partially filled
		diff(12, gone("a1", orderbook.Sell, Completed)), // a1 filled
		diff(13, open("a2", orderbook.Sell, 102, 7), open("a3", orderbook.Sell, 102, 1)),
	} {
		if b.ApplyDiff(d) {
			t.Fatal("asked for a snapshot while waiting for the first one")
		}
	}
	if b.Synced() {
		t.Fatal("synced before any snapshot")
	}

	if b.LoadSnapshot(snapshot()) {
		t.Fatal("queued diffs continue the snapshot; no new one needed")
	}
	if !b.Synced() || b.Seq() != 13 {
		t.Fatalf("synced %t at %d, want true at 13", b.Synced(), b.Seq())
	}
	wantBids := []orderbook.Level{lvl(100, 6, 2), lvl(99, 1, 1)}
	wantAsks := []orderbook.Level{lvl(102, 8, 2)}
	if got := b.Depth(orderbook.Buy, 10); !slices.Equal(got, wantBids) {
		t.Errorf("bids = %v, want %v", got, wantBids)
	}
	if got := b.Depth(orderbook.Sell, 10); !slices.Equal(got, wantAsks) {
		t.Errorf("asks = %v, want %v", got, wantAsks)
	}
	if b.Len() != 5 {
		t.Errorf("Len = %d, want 5", b.Len())
	}
}

func TestLiveDiffs(t *testing.T) {
	b := NewLocalBook()
	b.LoadSnapshot(snapshot())

	steps := []struct {
		diff     Diff
		wantBids []orderbook.Level
	}{
		{diff(11, open("b4", orderbook.Buy, 100, 1)), []orderbook.Level{lvl(100, 8, 3), lvl(99, 1, 1)}},
		{diff(12, gone("b3", orderbook.Buy, Cancelled)), []orderbook.Level{lvl(100, 8, 3)}},
		{diff(12, open("x", orderbook.Buy, 50, 9)), []orderbook.Level{lvl(100, 8, 3)}},        // repeated sequence: ignored
		{diff(13, gone("nope", orderbook.Buy, Cancelled)), []orderbook.Level{lvl(100, 8, 3)}}, // unknown order: harmless
		{diff(14, open("b2", orderbook.Buy, 100, 2)), []orderbook.Level{lvl(100, 8, 3)}},      // same state again: idempotent
		{diff(15, gone("b1", orderbook.Buy, Completed), gone("b2", orderbook.Buy, Cancelled), gone("b4", orderbook.Buy, Cancelled)), []orderbook.Level{}},
	}
	for _, s := range steps {
		if b.ApplyDiff(s.diff) {
			t.Fatalf("diff %d: unexpected gap", s.diff.Seq)
		}
		if got := b.Depth(orderbook.Buy, 10); !slices.Equal(got, s.wantBids) {
			t.Fatalf("after diff %d: bids = %v, want %v", s.diff.Seq, got, s.wantBids)
		}
	}
	if b.Seq() != 15 || b.Gaps() != 0 {
		t.Errorf("seq %d gaps %d, want 15 and 0", b.Seq(), b.Gaps())
	}
}

func TestGapForcesResync(t *testing.T) {
	b := NewLocalBook()
	b.LoadSnapshot(snapshot())

	if !b.ApplyDiff(diff(12, open("lost-11-is-missing", orderbook.Buy, 100, 1))) {
		t.Fatal("sequence 11 is missing but no snapshot was requested")
	}
	if b.Synced() || b.Gaps() != 1 {
		t.Fatalf("synced %t gaps %d, want false and 1", b.Synced(), b.Gaps())
	}
	b.ApplyDiff(diff(13, gone("b1", orderbook.Buy, Cancelled))) // queued until the new snapshot

	// The new snapshot already includes 11 and 12 (b1 is still there as of 12).
	s := snapshot()
	s.Seq = 12
	if b.LoadSnapshot(s) {
		t.Fatal("new snapshot plus queued diff 13 should be in sync")
	}
	if want := []orderbook.Level{lvl(100, 2, 1), lvl(99, 1, 1)}; !slices.Equal(b.Depth(orderbook.Buy, 10), want) {
		t.Errorf("bids = %v, want %v", b.Depth(orderbook.Buy, 10), want)
	}
	if !b.Synced() || b.Seq() != 13 {
		t.Errorf("synced %t at %d, want true at 13", b.Synced(), b.Seq())
	}
}

func TestSnapshotOlderThanTheQueue(t *testing.T) {
	b := NewLocalBook()
	b.ApplyDiff(diff(15, open("a9", orderbook.Sell, 105, 1))) // 11 to 14 were never received
	b.ApplyDiff(diff(16, open("a8", orderbook.Sell, 106, 1)))

	if !b.LoadSnapshot(snapshot()) { // as of 10
		t.Fatal("snapshot at 10 cannot be continued by diff 15")
	}
	if b.Synced() {
		t.Fatal("synced with a hole between snapshot and queue")
	}

	s := snapshot()
	s.Seq = 15 // a newer snapshot that already includes a9
	s.Orders = append(s.Orders, open("a9", orderbook.Sell, 105, 1))
	if b.LoadSnapshot(s) || !b.Synced() || b.Seq() != 16 {
		t.Fatalf("synced %t at %d after the newer snapshot, want true at 16", b.Synced(), b.Seq())
	}
	if got := len(b.Depth(orderbook.Sell, 10)); got != 3 {
		t.Errorf("ask levels = %d, want 3 (101, 105, 106)", got)
	}
}

func lvl(p orderbook.Price, q orderbook.Quantity, n int) orderbook.Level {
	return orderbook.Level{Price: p, Quantity: q, Orders: n}
}
