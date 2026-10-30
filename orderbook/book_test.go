package orderbook

import (
	"errors"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestEmptyBook(t *testing.T) {
	b := NewBook()
	if _, ok := b.BestBid(); ok {
		t.Error("empty book has a best bid")
	}
	if _, ok := b.BestAsk(); ok {
		t.Error("empty book has a best ask")
	}
	if d := b.Depth(Buy, 5); len(d) != 0 {
		t.Errorf("depth = %v, want empty", d)
	}
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
}

func TestBestPrices(t *testing.T) {
	tests := []struct {
		name             string
		orders           []Order
		wantBid, wantAsk Price
		hasBid, hasAsk   bool
	}{
		{
			name:    "highest bid and lowest ask are best",
			orders:  []Order{buy(1, 99, 1), buy(2, 101, 1), buy(3, 100, 1), sell(4, 105, 1), sell(5, 103, 1), sell(6, 104, 1)},
			wantBid: 101, hasBid: true, wantAsk: 103, hasAsk: true,
		},
		{
			name:    "bids only",
			orders:  []Order{buy(1, 50, 2), buy(2, 49, 2)},
			wantBid: 50, hasBid: true,
		},
		{
			name:    "asks only",
			orders:  []Order{sell(1, 70, 2), sell(2, 71, 2)},
			wantAsk: 70, hasAsk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bookWith(t, tt.orders...)
			if bid, ok := b.BestBid(); ok != tt.hasBid || bid != tt.wantBid {
				t.Errorf("BestBid = %d, %t; want %d, %t", bid, ok, tt.wantBid, tt.hasBid)
			}
			if ask, ok := b.BestAsk(); ok != tt.hasAsk || ask != tt.wantAsk {
				t.Errorf("BestAsk = %d, %t; want %d, %t", ask, ok, tt.wantAsk, tt.hasAsk)
			}
		})
	}
}

func TestDepthAggregatesLevelsBestFirst(t *testing.T) {
	b := bookWith(t,
		buy(1, 100, 5), buy(2, 98, 1), buy(3, 100, 2), buy(4, 99, 4),
		sell(5, 102, 3), sell(6, 101, 1), sell(7, 101, 6),
	)

	tests := []struct {
		side Side
		n    int
		want []Level
	}{
		{Buy, 10, []Level{{100, 7, 2}, {99, 4, 1}, {98, 1, 1}}},
		{Buy, 2, []Level{{100, 7, 2}, {99, 4, 1}}},
		{Sell, 10, []Level{{101, 7, 2}, {102, 3, 1}}},
		{Sell, 1, []Level{{101, 7, 2}}},
		{Sell, 0, []Level{}},
	}
	for _, tt := range tests {
		if got := b.Depth(tt.side, tt.n); !slices.Equal(got, tt.want) {
			t.Errorf("Depth(%v, %d) = %v, want %v", tt.side, tt.n, got, tt.want)
		}
	}
}

func TestLevelsKeepArrivalOrder(t *testing.T) {
	b := bookWith(t, sell(30, 101, 1), sell(10, 101, 4), sell(20, 102, 1), sell(40, 101, 2))

	if got, want := queue(b, Sell, 101), []OrderID{30, 10, 40}; !slices.Equal(got, want) {
		t.Errorf("queue at 101 = %v, want %v (arrival order, not id order)", got, want)
	}
	if got, want := queue(b, Sell, 102), []OrderID{20}; !slices.Equal(got, want) {
		t.Errorf("queue at 102 = %v, want %v", got, want)
	}
}

func TestRestRejectsDuplicateIDs(t *testing.T) {
	b := bookWith(t, buy(1, 100, 5))

	err := b.rest(sell(1, 110, 3))
	if !errors.Is(err, ErrDuplicateOrderID) {
		t.Fatalf("err = %v, want %v", err, ErrDuplicateOrderID)
	}
	if b.Len() != 1 || len(b.Depth(Sell, 10)) != 0 {
		t.Errorf("rejected order changed the book: Len = %d, asks = %v", b.Len(), b.Depth(Sell, 10))
	}
	checkInvariants(t, b)
}

// TestRandomOrdersKeepInvariants rests many orders at random prices and checks the
// book's structure against what it was given.
func TestRandomOrdersKeepInvariants(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	b := NewBook()
	var total [3]Quantity // indexed by Side
	for id := OrderID(1); id <= 2000; id++ {
		o := buy(id, Price(900+r.IntN(100)), Quantity(1+r.IntN(50)))
		if r.IntN(2) == 0 {
			o = sell(id, Price(1001+r.IntN(100)), o.Quantity)
		}
		if err := b.rest(o); err != nil {
			t.Fatal(err)
		}
		total[o.Side] += o.Quantity
	}
	checkInvariants(t, b)

	for _, side := range []Side{Buy, Sell} {
		var sum Quantity
		for _, l := range b.Depth(side, b.Len()) {
			sum += l.Quantity
		}
		if sum != total[side] {
			t.Errorf("%v depth sums to %d, want %d", side, sum, total[side])
		}
	}
}

// checkInvariants verifies the book's internal structure.
func checkInvariants(t *testing.T, b *Book) {
	t.Helper()
	seen := 0
	for _, s := range []*bookSide{&b.bids, &b.asks} {
		if len(s.prices) != len(s.levels) {
			t.Fatalf("%v: %d prices but %d levels", s.side, len(s.prices), len(s.levels))
		}
		for i, p := range s.prices {
			if i > 0 && s.compareWorstFirst(s.prices[i-1], p) >= 0 {
				t.Fatalf("%v: prices not strictly sorted worst to best: %v", s.side, s.prices)
			}
			l := s.levels[p]
			if l == nil || l.price != p {
				t.Fatalf("%v: no level for price %d", s.side, p)
			}
			var qty Quantity
			count := 0
			var prev *restingOrder
			for o := l.head; o != nil; o = o.next {
				if o.prev != prev || o.level != l || o.Price != p || o.Side != s.side || o.Quantity <= 0 {
					t.Fatalf("%v %d: broken queue node %+v", s.side, p, o.Order)
				}
				if b.orders[o.ID] != o {
					t.Fatalf("order %d is queued but not indexed", o.ID)
				}
				qty += o.Quantity
				count++
				prev = o
			}
			if l.tail != prev || count == 0 || count != l.count || qty != l.quantity {
				t.Fatalf("%v %d: level totals %d/%d, queue has %d/%d", s.side, p, l.count, l.quantity, count, qty)
			}
			seen += count
		}
	}
	if seen != len(b.orders) {
		t.Fatalf("%d orders queued but %d indexed", seen, len(b.orders))
	}
	bid, hasBid := b.BestBid()
	ask, hasAsk := b.BestAsk()
	if hasBid && hasAsk && bid >= ask {
		t.Fatalf("crossed book: best bid %d >= best ask %d", bid, ask)
	}
}

func bookWith(t *testing.T, orders ...Order) *Book {
	t.Helper()
	b := NewBook()
	for _, o := range orders {
		if err := b.rest(o); err != nil {
			t.Fatal(err)
		}
	}
	checkInvariants(t, b)
	return b
}

func queue(b *Book, side Side, p Price) []OrderID {
	var ids []OrderID
	for o := b.side(side).levels[p].head; o != nil; o = o.next {
		ids = append(ids, o.ID)
	}
	return ids
}

func buy(id OrderID, p Price, q Quantity) Order {
	return Order{ID: id, Side: Buy, Price: p, Quantity: q}
}
func sell(id OrderID, p Price, q Quantity) Order {
	return Order{ID: id, Side: Sell, Price: p, Quantity: q}
}
