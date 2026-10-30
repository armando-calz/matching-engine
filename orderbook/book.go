package orderbook

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// ErrDuplicateOrderID is returned when an order id is already resting in the book.
var ErrDuplicateOrderID = errors.New("duplicate order id")

// Book is the limit order book of one instrument: bids and asks grouped in price levels,
// each level a FIFO queue. It is not safe for concurrent use; one goroutine owns it.
type Book struct {
	bids, asks bookSide
	orders     map[OrderID]*restingOrder
}

// NewBook returns an empty book.
func NewBook() *Book {
	return &Book{
		bids:   newBookSide(Buy),
		asks:   newBookSide(Sell),
		orders: make(map[OrderID]*restingOrder),
	}
}

// Level is a snapshot of one price level: its price, total quantity and number of orders.
type Level struct {
	Price    Price
	Quantity Quantity
	Orders   int
}

// BestBid returns the highest buy price, if any.
func (b *Book) BestBid() (Price, bool) { return b.bids.best() }

// BestAsk returns the lowest sell price, if any.
func (b *Book) BestAsk() (Price, bool) { return b.asks.best() }

// Depth returns up to n levels of one side, best price first.
func (b *Book) Depth(side Side, n int) []Level {
	s := b.side(side)
	n = min(n, len(s.prices))
	depth := make([]Level, 0, n)
	for i := len(s.prices) - 1; i >= len(s.prices)-n; i-- {
		l := s.levels[s.prices[i]]
		depth = append(depth, Level{Price: l.price, Quantity: l.quantity, Orders: l.count})
	}
	return depth
}

// Len returns the number of resting orders.
func (b *Book) Len() int { return len(b.orders) }

// rest adds an order to the back of its price level's queue. The caller has already
// matched it, so it does not cross the opposite side.
func (b *Book) rest(o Order) error {
	if _, ok := b.orders[o.ID]; ok {
		return fmt.Errorf("order %d: %w", o.ID, ErrDuplicateOrderID)
	}
	ro := &restingOrder{Order: o}
	b.side(o.Side).levelAt(o.Price).pushBack(ro)
	b.orders[o.ID] = ro
	return nil
}

func (b *Book) side(s Side) *bookSide {
	if s == Buy {
		return &b.bids
	}
	return &b.asks
}

// bookSide is one side of the book. prices is sorted from the worst price to the best,
// so the best level is the last element: matching consumes the best level, and
// removing it is then a cheap truncation instead of shifting the whole slice.
type bookSide struct {
	side   Side
	prices []Price
	levels map[Price]*level
}

func newBookSide(s Side) bookSide {
	return bookSide{side: s, levels: make(map[Price]*level)}
}

func (s *bookSide) best() (Price, bool) {
	if len(s.prices) == 0 {
		return 0, false
	}
	return s.prices[len(s.prices)-1], true
}

// levelAt returns the level at price p, creating it in sorted position if needed.
func (s *bookSide) levelAt(p Price) *level {
	if l, ok := s.levels[p]; ok {
		return l
	}
	i, _ := slices.BinarySearchFunc(s.prices, p, s.compareWorstFirst)
	s.prices = slices.Insert(s.prices, i, p)
	l := &level{price: p}
	s.levels[p] = l
	return l
}

// compareWorstFirst orders prices from worst to best: ascending for bids
// (the highest bid is best), descending for asks (the lowest ask is best).
func (s *bookSide) compareWorstFirst(a, b Price) int {
	if s.side == Buy {
		return cmp.Compare(a, b)
	}
	return cmp.Compare(b, a)
}
