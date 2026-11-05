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
	ErrInvalidType     = errors.New("invalid order type")
	ErrInvalidPrice    = errors.New("price must be positive")
	ErrInvalidQuantity = errors.New("quantity must be positive")
)

// OrderType is how an order is priced. The zero value is Limit.
type OrderType uint8

const (
	// Limit orders trade at Price or better, and the unfilled rest waits in the book.
	Limit OrderType = iota
	// Market orders trade at whatever prices the book offers and never rest:
	// what the opposite side cannot fill is dropped.
	Market
)

func (t OrderType) String() string {
	switch t {
	case Limit:
		return "limit"
	case Market:
		return "market"
	default:
		return fmt.Sprintf("OrderType(%d)", uint8(t))
	}
}

// Order is a buy or sell of up to Quantity. Quantity is what remains unfilled.
// Price is the limit price; it is zero for market orders.
type Order struct {
	ID       OrderID
	Type     OrderType
	Side     Side
	Price    Price
	Quantity Quantity
}

// NewLimitOrder validates and returns a limit order.
func NewLimitOrder(id OrderID, side Side, price Price, quantity Quantity) (Order, error) {
	o := Order{ID: id, Type: Limit, Side: side, Price: price, Quantity: quantity}
	return o, o.Validate()
}

// NewMarketOrder validates and returns a market order.
func NewMarketOrder(id OrderID, side Side, quantity Quantity) (Order, error) {
	o := Order{ID: id, Type: Market, Side: side, Quantity: quantity}
	return o, o.Validate()
}

// Validate checks the side, type, price and quantity. It does not check the id.
func (o Order) Validate() error {
	if o.Side != Buy && o.Side != Sell {
		return fmt.Errorf("order %d: %w: %v", o.ID, ErrInvalidSide, o.Side)
	}
	switch o.Type {
	case Limit:
		if o.Price <= 0 {
			return fmt.Errorf("order %d: %w: %d", o.ID, ErrInvalidPrice, o.Price)
		}
	case Market:
		if o.Price != 0 {
			return fmt.Errorf("order %d: %w: market orders have no price, got %d", o.ID, ErrInvalidPrice, o.Price)
		}
	default:
		return fmt.Errorf("order %d: %w: %v", o.ID, ErrInvalidType, o.Type)
	}
	if o.Quantity <= 0 {
		return fmt.Errorf("order %d: %w: %d", o.ID, ErrInvalidQuantity, o.Quantity)
	}
	return nil
}

// Crosses reports whether this order can trade against a resting order at price p:
// a buy crosses asks at or below its price, a sell crosses bids at or above its price,
// and a market order crosses any price.
func (o Order) Crosses(p Price) bool {
	switch {
	case o.Type == Market:
		return true
	case o.Side == Buy:
		return p <= o.Price
	default:
		return p >= o.Price
	}
}
