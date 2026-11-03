package orderbook

import (
	"errors"
	"slices"
	"testing"
)

func TestSubmitMarketOrders(t *testing.T) {
	tests := []struct {
		name        string
		resting     []Order
		taker       Order
		wantTrades  []Trade
		wantDropped Quantity
		wantAsks    []Level
	}{
		{
			name:       "walks every level it needs, at any price",
			resting:    []Order{sell(1, 101, 2), sell(2, 150, 3)},
			taker:      market(10, Buy, 4),
			wantTrades: []Trade{{10, 1, Buy, 101, 2}, {10, 2, Buy, 150, 2}},
			wantAsks:   []Level{{150, 1, 1}},
		},
		{
			name:        "drops what the book cannot fill, never rests",
			resting:     []Order{sell(1, 101, 2)},
			taker:       market(10, Buy, 5),
			wantTrades:  []Trade{{10, 1, Buy, 101, 2}},
			wantDropped: 3,
			wantAsks:    []Level{},
		},
		{
			name:        "on an empty side drops everything",
			resting:     []Order{buy(1, 99, 2)},
			taker:       market(10, Buy, 5),
			wantDropped: 5,
			wantAsks:    []Level{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bookWith(t, tt.resting...)
			before := b.Depth(Buy, 10)
			res, err := b.Submit(tt.taker)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(res.Trades, tt.wantTrades) || res.Dropped != tt.wantDropped || res.Rested != 0 {
				t.Errorf("result = %+v, want trades %v dropped %d", res, tt.wantTrades, tt.wantDropped)
			}
			if got := b.Depth(Sell, 10); !slices.Equal(got, tt.wantAsks) {
				t.Errorf("asks = %v, want %v", got, tt.wantAsks)
			}
			if got := b.Depth(Buy, 10); !slices.Equal(got, before) {
				t.Errorf("market buy changed the bids: %v, was %v", got, before)
			}
			checkInvariants(t, b)
		})
	}
}

func TestMarketOrderValidation(t *testing.T) {
	if _, err := NewMarketOrder(1, Sell, 3); err != nil {
		t.Fatal(err)
	}
	b := NewBook()
	if _, err := b.Submit(Order{ID: 1, Type: Market, Side: Buy, Price: 100, Quantity: 1}); !errors.Is(err, ErrInvalidPrice) {
		t.Errorf("market order with a price: err = %v, want %v", err, ErrInvalidPrice)
	}
	if _, err := b.Submit(Order{ID: 1, Type: OrderType(7), Side: Buy, Price: 100, Quantity: 1}); !errors.Is(err, ErrInvalidType) {
		t.Errorf("unknown type: err = %v, want %v", err, ErrInvalidType)
	}
}

func TestCancelKeepsTheRestOfTheQueueInOrder(t *testing.T) {
	tests := []struct {
		name   string
		cancel OrderID
		want   []OrderID
	}{
		{"head", 1, []OrderID{2, 3}},
		{"middle", 2, []OrderID{1, 3}},
		{"tail", 3, []OrderID{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bookWith(t, buy(1, 100, 1), buy(2, 100, 2), buy(3, 100, 4))
			o, err := b.Cancel(tt.cancel)
			if err != nil {
				t.Fatal(err)
			}
			if o.ID != tt.cancel {
				t.Errorf("cancelled %+v, want order %d", o, tt.cancel)
			}
			if got := queue(b, Buy, 100); !slices.Equal(got, tt.want) {
				t.Errorf("queue = %v, want %v", got, tt.want)
			}
			if got, want := b.Depth(Buy, 1)[0].Quantity, 7-o.Quantity; got != want {
				t.Errorf("level quantity = %d, want %d", got, want)
			}
			checkInvariants(t, b)
		})
	}
}

func TestCancelRemovesEmptyLevels(t *testing.T) {
	b := bookWith(t, sell(1, 101, 1), sell(2, 102, 1), sell(3, 103, 1))

	for _, step := range []struct {
		cancel OrderID
		want   []Level
	}{
		{2, []Level{{101, 1, 1}, {103, 1, 1}}}, // inside the book
		{1, []Level{{103, 1, 1}}},              // the best level
		{3, []Level{}},
	} {
		if _, err := b.Cancel(step.cancel); err != nil {
			t.Fatal(err)
		}
		if got := b.Depth(Sell, 10); !slices.Equal(got, step.want) {
			t.Fatalf("after cancelling %d: asks = %v, want %v", step.cancel, got, step.want)
		}
		checkInvariants(t, b)
	}
}

func TestCancelReturnsTheUnfilledQuantity(t *testing.T) {
	b := bookWith(t, sell(1, 101, 10))
	if _, err := b.Submit(buy(2, 101, 4)); err != nil {
		t.Fatal(err)
	}
	o, err := b.Cancel(1)
	if err != nil {
		t.Fatal(err)
	}
	if o != sell(1, 101, 6) {
		t.Errorf("cancelled %+v, want 6 lots left", o)
	}
}

func TestCancelUnknownOrders(t *testing.T) {
	b := bookWith(t, sell(1, 101, 2), buy(2, 99, 1))
	if _, err := b.Submit(buy(3, 101, 2)); err != nil { // fills order 1
		t.Fatal(err)
	}
	if _, err := b.Cancel(2); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name string
		id   OrderID
	}{
		{"never existed", 42},
		{"already filled", 1},
		{"already cancelled", 2},
		{"filled on arrival, never rested", 3},
	} {
		if _, err := b.Cancel(tt.id); !errors.Is(err, ErrUnknownOrder) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, ErrUnknownOrder)
		}
	}
	if b.Len() != 0 {
		t.Errorf("Len = %d, want 0", b.Len())
	}
}

func market(id OrderID, side Side, q Quantity) Order {
	return Order{ID: id, Type: Market, Side: side, Quantity: q}
}
