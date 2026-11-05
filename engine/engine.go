// Package engine runs an order book on a single goroutine, its only writer (ADR 0001).
// Callers on any goroutine submit and cancel orders through the Engine, and every
// change to the book is published, in order, on a sequenced event stream (ADR 0002).
package engine

import (
	"context"
	"errors"

	"github.com/armando-calz/matching-engine/orderbook"
)

// ErrStopped is returned by calls made after the engine stopped.
var ErrStopped = errors.New("engine stopped")

// EventKind says what an Event reports.
type EventKind uint8

const (
	// Accepted: a valid order entered the engine with its assigned id, before matching.
	Accepted EventKind = iota + 1
	// Traded: the accepted order filled against a resting one. Trade is set.
	Traded
	// Rested: the unfilled quantity of a limit order now rests in the book.
	Rested
	// Dropped: the unfilled quantity of a market order was discarded.
	Dropped
	// Cancelled: a resting order left the book with this unfilled quantity.
	Cancelled
)

func (k EventKind) String() string {
	switch k {
	case Accepted:
		return "accepted"
	case Traded:
		return "traded"
	case Rested:
		return "rested"
	case Dropped:
		return "dropped"
	case Cancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// Event is one change to the book. Seq starts at 1 and increases by exactly one per
// event, so a consumer can detect a gap. For Rested, Dropped and Cancelled, Order's
// Quantity is the quantity concerned.
type Event struct {
	Seq   uint64
	Kind  EventKind
	Order orderbook.Order
	Trade orderbook.Trade
}

// Receipt is the outcome of an accepted order: the id the engine assigned to it,
// and its trades and rested or dropped quantity.
type Receipt struct {
	ID orderbook.OrderID
	orderbook.Result
}

// Engine owns one order book. Its methods are safe for concurrent use.
type Engine struct {
	commands chan func()
	events   chan Event
	stopped  chan struct{}

	// Owned by the run goroutine.
	ctx    context.Context
	book   *orderbook.Book
	nextID orderbook.OrderID
	seq    uint64
}

// Start runs a new engine with an empty book until ctx is cancelled. The event stream
// buffers up to buffer events; when it is full, the engine waits for the consumer.
func Start(ctx context.Context, buffer int) *Engine {
	e := &Engine{
		commands: make(chan func()),
		events:   make(chan Event, buffer),
		stopped:  make(chan struct{}),
		ctx:      ctx,
		book:     orderbook.NewBook(),
		nextID:   1,
	}
	go e.run()
	return e
}

// Events returns the event stream. It is closed after the engine stops.
// It has to be consumed, or the engine stalls once the buffer is full.
func (e *Engine) Events() <-chan Event { return e.events }

// Stopped is closed when the engine has stopped.
func (e *Engine) Stopped() <-chan struct{} { return e.stopped }

func (e *Engine) run() {
	defer close(e.events)
	defer close(e.stopped)
	for {
		select {
		case <-e.ctx.Done():
			return
		case cmd := <-e.commands:
			cmd()
		}
	}
}

// Submit assigns the order an id (any id in o is ignored), matches it and returns
// the outcome. An invalid order is rejected with no id assigned and no event.
//
// If ctx ends after the order reached the engine, Submit returns ctx's error but the
// order may still have been applied; the event stream is the record of what happened.
func (e *Engine) Submit(ctx context.Context, o orderbook.Order) (Receipt, error) {
	var r Receipt
	var err error
	if callErr := e.call(ctx, func() { r, err = e.submit(o) }); callErr != nil {
		return Receipt{}, callErr
	}
	return r, err
}

// Cancel removes a resting order and returns it with its unfilled quantity.
func (e *Engine) Cancel(ctx context.Context, id orderbook.OrderID) (orderbook.Order, error) {
	var o orderbook.Order
	var err error
	if callErr := e.call(ctx, func() { o, err = e.cancel(id) }); callErr != nil {
		return orderbook.Order{}, callErr
	}
	return o, err
}

// Depth returns up to n levels of one side of the book, best price first.
func (e *Engine) Depth(ctx context.Context, side orderbook.Side, n int) ([]orderbook.Level, error) {
	var levels []orderbook.Level
	if err := e.call(ctx, func() { levels = e.book.Depth(side, n) }); err != nil {
		return nil, err
	}
	return levels, nil
}

// call runs cmd on the engine's goroutine and waits for it to finish.
func (e *Engine) call(ctx context.Context, cmd func()) error {
	done := make(chan struct{})
	wrapped := func() { cmd(); close(done) }
	select {
	case e.commands <- wrapped:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stopped:
		return ErrStopped
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.stopped:
		return ErrStopped
	}
}

func (e *Engine) submit(o orderbook.Order) (Receipt, error) {
	if err := o.Validate(); err != nil {
		return Receipt{}, err
	}
	o.ID = e.nextID
	e.nextID++
	res, err := e.book.Submit(o)
	if err != nil {
		return Receipt{}, err // unreachable: validated, and ids are never reused
	}

	e.emit(Event{Kind: Accepted, Order: o})
	for _, t := range res.Trades {
		e.emit(Event{Kind: Traded, Trade: t})
	}
	rest := o
	switch {
	case res.Rested > 0:
		rest.Quantity = res.Rested
		e.emit(Event{Kind: Rested, Order: rest})
	case res.Dropped > 0:
		rest.Quantity = res.Dropped
		e.emit(Event{Kind: Dropped, Order: rest})
	}
	return Receipt{ID: o.ID, Result: res}, nil
}

func (e *Engine) cancel(id orderbook.OrderID) (orderbook.Order, error) {
	o, err := e.book.Cancel(id)
	if err != nil {
		return orderbook.Order{}, err
	}
	e.emit(Event{Kind: Cancelled, Order: o})
	return o, nil
}

// emit publishes the next event, waiting while the stream is full. If the engine is
// stopping, the event is lost; the stream is closed right after.
func (e *Engine) emit(ev Event) {
	e.seq++
	ev.Seq = e.seq
	select {
	case e.events <- ev:
	case <-e.ctx.Done():
	}
}
