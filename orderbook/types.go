// Package orderbook implements a limit order book and its matching rules for one instrument.
// It is single-threaded by design: one goroutine owns a book (ADR 0001).
package orderbook

import (
	"errors"
	"fmt"
)

// Side of an order: buy (bid) or sell (ask).
type Side uint8

const (
	Buy Side = iota + 1
	Sell
)

func (s Side) String() string {
	switch s {
	case Buy:
		return "buy"
	case Sell:
		return "sell"
	default:
		return fmt.Sprintf("Side(%d)", uint8(s))
	}
}

// Opposite returns the side an order of this side matches against.
func (s Side) Opposite() Side {
	if s == Buy {
		return Sell
	}
	return Buy
}

// Price is an integer number of ticks, the instrument's smallest price increment.
// It is never a float (ADR 0001).
type Price int64

// Quantity is an integer number of lots, the instrument's smallest tradable amount.
type Quantity int64

// OrderID identifies an order within a book.
type OrderID uint64

// Validation errors, comparable with errors.Is.
var (
	ErrInvalidSide     = errors.New("invalid side")
	ErrInvalidPrice    = errors.New("price must be positive")
	ErrInvalidQuantity = errors.New("quantity must be positive")
)

// Order is a limit order: buy or sell up to Quantity at Price or better.
// Quantity is what remains unfilled.
type Order struct {
	ID       OrderID
	Side     Side
	Price    Price
	Quantity Quantity
}

// NewLimitOrder validates and returns a limit order.
func NewLimitOrder(id OrderID, side Side, price Price, quantity Quantity) (Order, error) {
	if side != Buy && side != Sell {
		return Order{}, fmt.Errorf("order %d: %w: %v", id, ErrInvalidSide, side)
	}
	if price <= 0 {
		return Order{}, fmt.Errorf("order %d: %w: %d", id, ErrInvalidPrice, price)
	}
	if quantity <= 0 {
		return Order{}, fmt.Errorf("order %d: %w: %d", id, ErrInvalidQuantity, quantity)
	}
	return Order{ID: id, Side: side, Price: price, Quantity: quantity}, nil
}

// Crosses reports whether this order can trade against a resting order at price p:
// a buy crosses asks at or below its price, a sell crosses bids at or above its price.
func (o Order) Crosses(p Price) bool {
	if o.Side == Buy {
		return p <= o.Price
	}
	return p >= o.Price
}
