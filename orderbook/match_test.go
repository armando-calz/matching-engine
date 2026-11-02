package orderbook

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestSubmitMatchesWithPriceTimePriority(t *testing.T) {
	// Asks: 101 ×(4, 2) from orders 1 and 2, 102 ×3 from order 3. Bids: 99 ×5 from order 4.
	resting := []Order{sell(1, 101, 4), sell(2, 101, 2), sell(3, 102, 3), buy(4, 99, 5)}

	tests := []struct {
		name       string
		taker      Order
		wantTrades []Trade
		wantRested Quantity
		wantBids   []Level
		wantAsks   []Level
	}{
		{
			name:     "does not cross: rests",
			taker:    buy(10, 100, 2),
			wantBids: []Level{{100, 2, 1}, {99, 5, 1}}, wantRested: 2,
			wantAsks: []Level{{101, 6, 2}, {102, 3, 1}},
		},
		{
			name:       "partially fills the oldest order at the best price",
			taker:      buy(10, 101, 3),
			wantTrades: []Trade{{10, 1, Buy, 101, 3}},
			wantBids:   []Level{{99, 5, 1}},
			wantAsks:   []Level{{101, 3, 2}, {102, 3, 1}},
		},
		{
			name:       "fills a level in arrival order and removes it",
			taker:      buy(10, 101, 6),
			wantTrades: []Trade{{10, 1, Buy, 101, 4}, {10, 2, Buy, 101, 2}},
			wantBids:   []Level{{99, 5, 1}},
			wantAsks:   []Level{{102, 3, 1}},
		},
		{
			name:       "walks levels up to its limit, then rests the rest",
			taker:      buy(10, 102, 11),
			wantTrades: []Trade{{10, 1, Buy, 101, 4}, {10, 2, Buy, 101, 2}, {10, 3, Buy, 102, 3}},
			wantRested: 2,
			wantBids:   []Level{{102, 2, 1}, {99, 5, 1}},
			wantAsks:   []Level{},
		},
		{
			name:       "stops at its limit price",
			taker:      buy(10, 101, 8),
			wantTrades: []Trade{{10, 1, Buy, 101, 4}, {10, 2, Buy, 101, 2}},
			wantRested: 2,
			wantBids:   []Level{{101, 2, 1}, {99, 5, 1}},
			wantAsks:   []Level{{102, 3, 1}},
		},
		{
			name:       "trades at the maker's price, not the taker's",
			taker:      sell(10, 90, 2),
			wantTrades: []Trade{{10, 4, Sell, 99, 2}},
			wantBids:   []Level{{99, 3, 1}},
			wantAsks:   []Level{{101, 6, 2}, {102, 3, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bookWith(t, resting...)
			res, err := b.Submit(tt.taker)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.Trades, tt.wantTrades) {
				t.Errorf("trades = %v, want %v", res.Trades, tt.wantTrades)
			}
			if res.Rested != tt.wantRested {
				t.Errorf("rested = %d, want %d", res.Rested, tt.wantRested)
			}
			if got := b.Depth(Buy, 10); !slices.Equal(got, tt.wantBids) {
				t.Errorf("bids = %v, want %v", got, tt.wantBids)
			}
			if got := b.Depth(Sell, 10); !slices.Equal(got, tt.wantAsks) {
				t.Errorf("asks = %v, want %v", got, tt.wantAsks)
			}
			checkInvariants(t, b)
		})
	}
}

func TestSubmitRejectsBeforeChangingTheBook(t *testing.T) {
	tests := []struct {
		name    string
		order   Order
		wantErr error
	}{
		{"invalid quantity", buy(10, 101, 0), ErrInvalidQuantity},
		{"invalid price", buy(10, 0, 1), ErrInvalidPrice},
		{"invalid side", Order{ID: 10, Price: 101, Quantity: 1}, ErrInvalidSide},
		// Would cross order 1 if the duplicate check came after matching.
		{"duplicate id", buy(2, 101, 1), ErrDuplicateOrderID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bookWith(t, sell(1, 101, 4), buy(2, 99, 5))
			res, err := b.Submit(tt.order)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(res.Trades) != 0 || b.Len() != 2 || !slices.Equal(b.Depth(Sell, 10), []Level{{101, 4, 1}}) {
				t.Errorf("rejected order changed the book: %+v, asks %v", res, b.Depth(Sell, 10))
			}
		})
	}
}

// TestSubmitAgreesWithReferenceModel replays random order flow through the book and
// through a deliberately naive implementation of price-time priority, and requires
// identical trades and resting orders after every order.
func TestSubmitAgreesWithReferenceModel(t *testing.T) {
	for seed := range uint64(20) {
		t.Run(fmt.Sprint("seed ", seed), func(t *testing.T) {
			r := rand.New(rand.NewPCG(seed, 99))
			b := NewBook()
			ref := &referenceBook{}
			for id := OrderID(1); id <= 500; id++ {
				side := Buy
				if r.IntN(2) == 0 {
					side = Sell
				}
				o := Order{ID: id, Side: side, Price: Price(95 + r.IntN(11)), Quantity: Quantity(1 + r.IntN(20))}

				res, err := b.Submit(o)
				if err != nil {
					t.Fatal(err)
				}
				wantTrades, wantRested := ref.submit(o)
				if !slices.Equal(res.Trades, wantTrades) || res.Rested != wantRested {
					t.Fatalf("order %+v: got %v rested %d, want %v rested %d", o, res.Trades, res.Rested, wantTrades, wantRested)
				}
				checkInvariants(t, b)
			}
			for _, o := range ref.resting {
				ro := b.orders[o.ID]
				if ro == nil || ro.Order != o {
					t.Fatalf("resting order %+v: book has %+v", o, ro)
				}
			}
			if b.Len() != len(ref.resting) {
				t.Fatalf("book has %d resting orders, reference %d", b.Len(), len(ref.resting))
			}
		})
	}
}

// referenceBook keeps resting orders in one slice in arrival order and scans all of
// them for every fill. Slow, but obviously correct.
type referenceBook struct{ resting []Order }

func (r *referenceBook) submit(o Order) ([]Trade, Quantity) {
	var trades []Trade
	for o.Quantity > 0 {
		best := -1
		for i, m := range r.resting {
			if m.Side == o.Side || !o.Crosses(m.Price) {
				continue
			}
			// Strictly better only: at equal prices the earlier (lower) index wins.
			if best == -1 || betterPrice(m.Side, m.Price, r.resting[best].Price) {
				best = i
			}
		}
		if best == -1 {
			break
		}
		m := &r.resting[best]
		q := min(o.Quantity, m.Quantity)
		trades = append(trades, Trade{o.ID, m.ID, o.Side, m.Price, q})
		o.Quantity -= q
		m.Quantity -= q
		if m.Quantity == 0 {
			r.resting = slices.Delete(r.resting, best, best+1)
		}
	}
	if o.Quantity > 0 {
		r.resting = append(r.resting, o)
	}
	return trades, o.Quantity
}

func betterPrice(side Side, a, b Price) bool {
	if side == Buy {
		return a > b
	}
	return a < b
}

func ExampleBook_Submit() {
	b := NewBook()
	b.Submit(Order{ID: 1, Side: Sell, Price: 1_250_500_00, Quantity: 10_000_000}) // 0.10 BTC at 1,250,500.00
	b.Submit(Order{ID: 2, Side: Sell, Price: 1_251_000_00, Quantity: 30_000_000}) // 0.30 BTC at 1,251,000.00

	res, _ := b.Submit(Order{ID: 3, Side: Buy, Price: 1_251_000_00, Quantity: 20_000_000})
	for _, tr := range res.Trades {
		fmt.Printf("order %d bought %d lots from order %d at %d ticks\n", tr.TakerID, tr.Quantity, tr.MakerID, tr.Price)
	}
	ask, _ := b.BestAsk()
	fmt.Println("best ask now:", ask, "with", b.Depth(Sell, 1)[0].Quantity, "lots")
	// Output:
	// order 3 bought 10000000 lots from order 1 at 125050000 ticks
	// order 3 bought 10000000 lots from order 2 at 125100000 ticks
	// best ask now: 125100000 with 20000000 lots
}
