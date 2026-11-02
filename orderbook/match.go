package orderbook

import "fmt"

// Trade is one fill between an incoming order (the taker) and a resting one (the maker).
// It happens at the maker's price: the resting order set it first.
type Trade struct {
	TakerID   OrderID
	MakerID   OrderID
	TakerSide Side
	Price     Price
	Quantity  Quantity
}

// Result reports what happened to a submitted order.
type Result struct {
	Trades []Trade
	// Rested is the quantity left in the book after matching; zero if fully filled.
	Rested Quantity
}

// Submit matches a limit order against the opposite side with price-time priority:
// the best price first and, at the same price, the oldest order first. Whatever
// cannot be filled at its limit price or better rests in the book.
//
// Order ids must be unique among resting orders. An invalid or duplicate order is
// rejected before anything changes.
func (b *Book) Submit(o Order) (Result, error) {
	if _, err := NewLimitOrder(o.ID, o.Side, o.Price, o.Quantity); err != nil {
		return Result{}, err
	}
	if _, ok := b.orders[o.ID]; ok {
		return Result{}, fmt.Errorf("order %d: %w", o.ID, ErrDuplicateOrderID)
	}

	var res Result
	opposite := b.side(o.Side.Opposite())
	for o.Quantity > 0 {
		price, ok := opposite.best()
		if !ok || !o.Crosses(price) {
			break
		}
		l := opposite.levels[price]
		for o.Quantity > 0 && l.head != nil {
			maker := l.head
			q := min(o.Quantity, maker.Quantity)
			res.Trades = append(res.Trades, Trade{
				TakerID: o.ID, MakerID: maker.ID, TakerSide: o.Side, Price: price, Quantity: q,
			})
			o.Quantity -= q
			maker.Quantity -= q
			l.quantity -= q
			if maker.Quantity == 0 {
				l.popFront()
				delete(b.orders, maker.ID)
			}
		}
		if l.head == nil {
			opposite.removeBest()
		}
	}

	if o.Quantity > 0 {
		if err := b.rest(o); err != nil {
			return Result{}, err // unreachable: the id was checked above
		}
		res.Rested = o.Quantity
	}
	return res, nil
}
