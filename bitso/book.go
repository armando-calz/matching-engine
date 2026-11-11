// Package bitso keeps a local copy of a Bitso order book from its public feed:
// a REST snapshot plus the diff-orders WebSocket channel, with gaps in the feed's
// sequence numbers detected and repaired by resynchronizing.
package bitso

import (
	"cmp"
	"maps"
	"slices"

	"github.com/armando-calz/matching-engine/orderbook"
)

// Status of an order in a diff-orders message.
type Status uint8

const (
	Open      Status = iota + 1 // in the book, possibly partially filled; Amount is what remains
	Cancelled                   // removed from the book
	Completed                   // fully filled and removed from the book
)

// Update is the new state of one order. Prices and amounts are already integers in
// the market's units (see Market).
type Update struct {
	ID     string
	Side   orderbook.Side
	Price  orderbook.Price
	Amount orderbook.Quantity
	Status Status
}

// Diff is one diff-orders message. Its sequence number is one more than the previous
// message's; anything else means messages were lost.
type Diff struct {
	Seq     uint64
	Updates []Update
}

// Snapshot is the full book from the REST API, as of sequence number Seq.
type Snapshot struct {
	Seq    uint64
	Orders []Update // all Open
}

type entry struct {
	side   orderbook.Side
	price  orderbook.Price
	amount orderbook.Quantity
}

type level struct {
	amount orderbook.Quantity
	orders int
}

// LocalBook mirrors an exchange's book order by order. Unlike orderbook.Book it never
// matches: the exchange already did, and the feed reports the outcome.
//
// It follows Bitso's procedure: diffs that arrive before a snapshot are queued, the
// snapshot is loaded, queued diffs at or below its sequence are discarded and the rest
// applied. A missing sequence number marks the book out of sync until a new snapshot.
// It is not safe for concurrent use.
type LocalBook struct {
	synced  bool
	seq     uint64
	pending []Diff
	orders  map[string]entry
	levels  map[orderbook.Side]map[orderbook.Price]*level
	gaps    int
}

// NewLocalBook returns an empty book waiting for its first snapshot.
func NewLocalBook() *LocalBook {
	b := &LocalBook{}
	b.reset()
	return b
}

// Synced reports whether the book is a faithful copy as of Seq.
func (b *LocalBook) Synced() bool { return b.synced }

// Seq returns the sequence number of the last diff (or snapshot) applied.
func (b *LocalBook) Seq() uint64 { return b.seq }

// Gaps returns how many times a missing sequence number was detected.
func (b *LocalBook) Gaps() int { return b.gaps }

// ApplyDiff applies a diff, or queues it while the book is out of sync. It returns
// true when it detected a gap: the caller must then fetch a new snapshot.
func (b *LocalBook) ApplyDiff(d Diff) (needSnapshot bool) {
	if !b.synced {
		b.pending = append(b.pending, d)
		return false
	}
	switch {
	case d.Seq <= b.seq: // repeated or older than the snapshot
		return false
	case d.Seq == b.seq+1:
		b.apply(d)
		return false
	default:
		b.gaps++
		b.synced = false
		b.pending = append(b.pending[:0], d)
		return true
	}
}

// LoadSnapshot replaces the book with a snapshot and replays the queued diffs newer
// than it. It returns true if the queued diffs do not continue the snapshot, in which
// case a newer snapshot is needed.
func (b *LocalBook) LoadSnapshot(s Snapshot) (needSnapshot bool) {
	b.reset()
	for _, u := range s.Orders {
		b.upsert(u)
	}
	b.seq = s.Seq
	b.synced = true

	pending := b.pending
	b.pending = nil
	for i, d := range pending {
		if b.ApplyDiff(d) {
			b.pending = append(b.pending, pending[i+1:]...)
			return true
		}
	}
	return false
}

// Depth returns up to n aggregated levels of one side, best price first.
func (b *LocalBook) Depth(side orderbook.Side, n int) []orderbook.Level {
	levels := b.levels[side]
	prices := slices.SortedFunc(maps.Keys(levels), func(x, y orderbook.Price) int {
		if side == orderbook.Buy {
			return cmp.Compare(y, x)
		}
		return cmp.Compare(x, y)
	})
	depth := make([]orderbook.Level, 0, min(n, len(prices)))
	for _, p := range prices[:min(n, len(prices))] {
		depth = append(depth, orderbook.Level{Price: p, Quantity: levels[p].amount, Orders: levels[p].orders})
	}
	return depth
}

// Len returns the number of orders in the book.
func (b *LocalBook) Len() int { return len(b.orders) }

func (b *LocalBook) reset() {
	b.orders = make(map[string]entry)
	b.levels = map[orderbook.Side]map[orderbook.Price]*level{
		orderbook.Buy:  {},
		orderbook.Sell: {},
	}
}

func (b *LocalBook) apply(d Diff) {
	for _, u := range d.Updates {
		if u.Status == Open {
			b.upsert(u)
		} else {
			b.remove(u.ID)
		}
	}
	b.seq = d.Seq
}

// upsert sets an order's state. Updates carry the remaining amount, not a change,
// so applying the same update twice is harmless.
func (b *LocalBook) upsert(u Update) {
	b.remove(u.ID)
	if u.Amount <= 0 {
		return
	}
	b.orders[u.ID] = entry{side: u.Side, price: u.Price, amount: u.Amount}
	l := b.levels[u.Side][u.Price]
	if l == nil {
		l = &level{}
		b.levels[u.Side][u.Price] = l
	}
	l.amount += u.Amount
	l.orders++
}

func (b *LocalBook) remove(id string) {
	e, ok := b.orders[id]
	if !ok {
		return
	}
	delete(b.orders, id)
	l := b.levels[e.side][e.price]
	l.amount -= e.amount
	l.orders--
	if l.orders == 0 {
		delete(b.levels[e.side], e.price)
	}
}
