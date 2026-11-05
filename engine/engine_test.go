package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	ob "github.com/armando-calz/matching-engine/orderbook"
)

func TestSubmitAssignsIDsAndPublishesEvents(t *testing.T) {
	e, events := start(t, 16)
	ctx := context.Background()

	r1 := mustSubmit(t, e, ob.Order{ID: 99, Side: ob.Sell, Price: 101, Quantity: 5}) // the id is ignored
	r2 := mustSubmit(t, e, ob.Order{Side: ob.Buy, Price: 101, Quantity: 8})
	r3 := mustSubmit(t, e, ob.Order{Type: ob.Market, Side: ob.Sell, Quantity: 10})
	if r1.ID != 1 || r2.ID != 2 || r3.ID != 3 {
		t.Fatalf("ids = %d, %d, %d; want 1, 2, 3", r1.ID, r2.ID, r3.ID)
	}
	if _, err := e.Cancel(ctx, 1); !errors.Is(err, ob.ErrUnknownOrder) {
		t.Fatalf("cancel of a filled order: err = %v", err)
	}

	want := []Event{
		{Seq: 1, Kind: Accepted, Order: ob.Order{ID: 1, Side: ob.Sell, Price: 101, Quantity: 5}},
		{Seq: 2, Kind: Rested, Order: ob.Order{ID: 1, Side: ob.Sell, Price: 101, Quantity: 5}},
		{Seq: 3, Kind: Accepted, Order: ob.Order{ID: 2, Side: ob.Buy, Price: 101, Quantity: 8}},
		{Seq: 4, Kind: Traded, Trade: ob.Trade{TakerID: 2, MakerID: 1, TakerSide: ob.Buy, Price: 101, Quantity: 5}},
		{Seq: 5, Kind: Rested, Order: ob.Order{ID: 2, Side: ob.Buy, Price: 101, Quantity: 3}},
		{Seq: 6, Kind: Accepted, Order: ob.Order{ID: 3, Type: ob.Market, Side: ob.Sell, Quantity: 10}},
		{Seq: 7, Kind: Traded, Trade: ob.Trade{TakerID: 3, MakerID: 2, TakerSide: ob.Sell, Price: 101, Quantity: 3}},
		{Seq: 8, Kind: Dropped, Order: ob.Order{ID: 3, Type: ob.Market, Side: ob.Sell, Quantity: 7}},
	}
	if got := events(); !slices.Equal(got, want) {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}
}

func TestRejectionsChangeNothing(t *testing.T) {
	e, events := start(t, 16)

	if _, err := e.Submit(context.Background(), ob.Order{Side: ob.Buy, Price: 0, Quantity: 1}); !errors.Is(err, ob.ErrInvalidPrice) {
		t.Fatalf("err = %v, want %v", err, ob.ErrInvalidPrice)
	}
	if _, err := e.Cancel(context.Background(), 42); !errors.Is(err, ob.ErrUnknownOrder) {
		t.Fatalf("err = %v, want %v", err, ob.ErrUnknownOrder)
	}
	if r := mustSubmit(t, e, ob.Order{Side: ob.Buy, Price: 100, Quantity: 1}); r.ID != 1 {
		t.Errorf("first accepted order got id %d; a rejection consumed an id", r.ID)
	}
	if got := events(); len(got) != 2 || got[0].Seq != 1 {
		t.Errorf("events = %v, want only the accepted order's two", got)
	}
}

func TestCancelPublishesTheUnfilledQuantity(t *testing.T) {
	e, events := start(t, 16)
	ctx := context.Background()
	mustSubmit(t, e, ob.Order{Side: ob.Sell, Price: 101, Quantity: 10})
	mustSubmit(t, e, ob.Order{Side: ob.Buy, Price: 101, Quantity: 4})

	o, err := e.Cancel(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if o.Quantity != 6 {
		t.Errorf("cancelled quantity = %d, want 6", o.Quantity)
	}
	depth, err := e.Depth(ctx, ob.Sell, 10)
	if err != nil || len(depth) != 0 {
		t.Errorf("asks = %v, %v; want empty", depth, err)
	}
	got := events()
	if last := got[len(got)-1]; last.Kind != Cancelled || last.Order.ID != 1 || last.Order.Quantity != 6 {
		t.Errorf("last event = %+v, want order 1 cancelled with 6", last)
	}
}

// TestConcurrentClientsProduceAReplayableJournal has many goroutines submit and cancel
// at once. The engine serializes them; the test then checks that the event stream
// has no gaps, agrees with every receipt, and is a complete journal: replaying its
// Accepted and Cancelled events into a fresh book reproduces every other event.
func TestConcurrentClientsProduceAReplayableJournal(t *testing.T) {
	const clients, perClient = 8, 400
	e, events := start(t, 64)

	var mu sync.Mutex
	receipts := map[ob.OrderID]Receipt{}
	var wg sync.WaitGroup
	for c := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(c), 7))
			var mine []ob.OrderID
			for range perClient {
				if len(mine) > 0 && r.IntN(4) == 0 {
					_, _ = e.Cancel(context.Background(), mine[r.IntN(len(mine))]) // may be filled already
					continue
				}
				o := ob.Order{Side: ob.Buy, Price: ob.Price(95 + r.IntN(11)), Quantity: ob.Quantity(1 + r.IntN(20))}
				if r.IntN(2) == 0 {
					o.Side = ob.Sell
				}
				if r.IntN(10) == 0 {
					o.Type, o.Price = ob.Market, 0
				}
				rc, err := e.Submit(context.Background(), o)
				if err != nil {
					t.Error(err)
					return
				}
				mine = append(mine, rc.ID)
				mu.Lock()
				receipts[rc.ID] = rc
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	log := events()

	for i, ev := range log {
		if ev.Seq != uint64(i+1) {
			t.Fatalf("event %d has seq %d", i, ev.Seq)
		}
	}
	trades := map[ob.OrderID][]ob.Trade{}
	for _, ev := range log {
		if ev.Kind == Traded {
			trades[ev.Trade.TakerID] = append(trades[ev.Trade.TakerID], ev.Trade)
		}
	}
	for id, rc := range receipts {
		if !slices.Equal(rc.Trades, trades[id]) {
			t.Fatalf("order %d: receipt trades %v, events %v", id, rc.Trades, trades[id])
		}
	}
	if replayed := replay(t, log); !slices.Equal(replayed, log) {
		t.Fatal("replaying the journal into a new book produced different events")
	}
}

// replay rebuilds the event stream from the commands it records, with a plain book.
func replay(t *testing.T, log []Event) []Event {
	t.Helper()
	book := ob.NewBook()
	var out []Event
	add := func(ev Event) { ev.Seq = uint64(len(out) + 1); out = append(out, ev) }
	for _, ev := range log {
		switch ev.Kind {
		case Accepted:
			res, err := book.Submit(ev.Order)
			if err != nil {
				t.Fatal(err)
			}
			add(ev)
			for _, tr := range res.Trades {
				add(Event{Kind: Traded, Trade: tr})
			}
			rest := ev.Order
			if res.Rested > 0 {
				rest.Quantity = res.Rested
				add(Event{Kind: Rested, Order: rest})
			} else if res.Dropped > 0 {
				rest.Quantity = res.Dropped
				add(Event{Kind: Dropped, Order: rest})
			}
		case Cancelled:
			o, err := book.Cancel(ev.Order.ID)
			if err != nil {
				t.Fatal(err)
			}
			add(Event{Kind: Cancelled, Order: o})
		}
	}
	return out
}

func TestStopping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := Start(ctx, 0)
	cancel()
	<-e.Stopped()

	if _, err := e.Submit(context.Background(), ob.Order{Side: ob.Buy, Price: 1, Quantity: 1}); !errors.Is(err, ErrStopped) {
		t.Errorf("Submit after stop: err = %v, want %v", err, ErrStopped)
	}
	if _, ok := <-e.Events(); ok {
		t.Error("event stream still open after stop")
	}
}

func TestCallerDeadlineWhileTheConsumerLags(t *testing.T) {
	e := Start(t.Context(), 0) // nobody reads the events
	go func() { _, _ = e.Submit(context.Background(), ob.Order{Side: ob.Buy, Price: 1, Quantity: 1}) }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.Submit(ctx, ob.Order{Side: ob.Buy, Price: 1, Quantity: 1}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want %v", err, context.DeadlineExceeded)
	}
}

func ExampleEngine() {
	ctx, stop := context.WithCancel(context.Background())
	e := Start(ctx, 64)

	e.Submit(ctx, ob.Order{Side: ob.Sell, Price: 1_250_500_00, Quantity: 10_000_000})
	r, _ := e.Submit(ctx, ob.Order{Side: ob.Buy, Price: 1_251_000_00, Quantity: 20_000_000})
	fmt.Printf("order %d: %d trade(s), %d lots resting\n", r.ID, len(r.Trades), r.Rested)

	stop()
	for ev := range e.Events() {
		fmt.Println(ev.Seq, ev.Kind)
	}
	// Output:
	// order 2: 1 trade(s), 10000000 lots resting
	// 1 accepted
	// 2 rested
	// 3 accepted
	// 4 traded
	// 5 rested
}

// start runs an engine for the test and collects its events. The returned function
// stops the engine and returns every event it published.
func start(t *testing.T, buffer int) (*Engine, func() []Event) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := Start(ctx, buffer)
	var log []Event
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for ev := range e.Events() {
			log = append(log, ev)
		}
	}()
	return e, func() []Event {
		// A Depth call returns after every earlier command, and their events, went through.
		if _, err := e.Depth(context.Background(), ob.Buy, 0); err != nil {
			t.Fatal(err)
		}
		cancel()
		<-collected
		return log
	}
}

func mustSubmit(t *testing.T, e *Engine, o ob.Order) Receipt {
	t.Helper()
	r, err := e.Submit(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
