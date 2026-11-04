package orderbook

import (
	"math/rand/v2"
	"testing"
)

// Prices in the benchmarks sit around a mid price of 10,000 ticks.
const mid Price = 10_000

// seededBook returns a book with the given number of levels on each side, each level
// holding perLevel orders, and the next free order id.
func seededBook(b *testing.B, levels, perLevel int) (*Book, OrderID) {
	b.Helper()
	book := NewBook()
	id := OrderID(1)
	for i := range levels {
		for range perLevel {
			for _, o := range []Order{buy(id, mid-1-Price(i), 10), sell(id+1, mid+1+Price(i), 10)} {
				if _, err := book.Submit(o); err != nil {
					b.Fatal(err)
				}
			}
			id += 2
		}
	}
	return book, id
}

// BenchmarkSubmitResting adds limit orders that do not cross to existing levels:
// the common case of a market maker quoting.
func BenchmarkSubmitResting(b *testing.B) {
	book, id := seededBook(b, 100, 10)
	r := rand.New(rand.NewPCG(1, 1))
	for b.Loop() {
		if _, err := book.Submit(buy(id, mid-1-Price(r.IntN(100)), 10)); err != nil {
			b.Fatal(err)
		}
		id++
	}
}

// BenchmarkSubmitFillsOneOrder sends a taker that fills exactly one resting order,
// and puts a new maker back at the same price so the book keeps its shape.
// One op is the taker plus the replacement maker.
func BenchmarkSubmitFillsOneOrder(b *testing.B) {
	book, id := seededBook(b, 100, 10)
	for b.Loop() {
		if res, err := book.Submit(buy(id, mid+1, 10)); err != nil || len(res.Trades) != 1 {
			b.Fatal(res, err)
		}
		if _, err := book.Submit(sell(id+1, mid+1, 10)); err != nil {
			b.Fatal(err)
		}
		id += 2
	}
}

// BenchmarkSubmitSweepsTenLevels sends a taker that clears the ten best ask levels
// (100 fills), then rebuilds them. One op is the sweep only.
func BenchmarkSubmitSweepsTenLevels(b *testing.B) {
	book, id := seededBook(b, 100, 10)
	for b.Loop() {
		if res, err := book.Submit(buy(id, mid+10, 1000)); err != nil || len(res.Trades) != 100 {
			b.Fatal(len(res.Trades), err)
		}
		id++
		b.StopTimer()
		for i := range 10 {
			for range 10 {
				if _, err := book.Submit(sell(id, mid+1+Price(i), 10)); err != nil {
					b.Fatal(err)
				}
				id++
			}
		}
		b.StartTimer()
	}
}

// BenchmarkNewLevel opens a price level and cancels its only order, which removes it,
// in a book with 1,000 levels per side. Prices are kept in a slice sorted worst to best,
// so a level near the best price moves no elements and the worst level moves them all.
func BenchmarkNewLevel(b *testing.B) {
	for _, bc := range []struct {
		name  string
		price Price
	}{
		{"best", mid},          // inside the spread: the end of the slice
		{"worst", mid - 1_500}, // below every bid: the start of the slice
	} {
		b.Run(bc.name, func(b *testing.B) {
			book, id := seededBook(b, 1_000, 1)
			for b.Loop() {
				if _, err := book.Submit(buy(id, bc.price, 10)); err != nil {
					b.Fatal(err)
				}
				if _, err := book.Cancel(id); err != nil {
					b.Fatal(err)
				}
				id++
			}
		})
	}
}

// BenchmarkCancel cancels resting orders in random order, from the middle of their queues.
func BenchmarkCancel(b *testing.B) {
	const batch = 10_000
	book, id := seededBook(b, 100, 10)
	r := rand.New(rand.NewPCG(2, 2))
	var pending []OrderID
	for b.Loop() {
		if len(pending) == 0 {
			b.StopTimer()
			for range batch {
				if _, err := book.Submit(buy(id, mid-1-Price(r.IntN(100)), 10)); err != nil {
					b.Fatal(err)
				}
				pending = append(pending, id)
				id++
			}
			r.Shuffle(len(pending), func(i, j int) { pending[i], pending[j] = pending[j], pending[i] })
			b.StartTimer()
		}
		if _, err := book.Cancel(pending[len(pending)-1]); err != nil {
			b.Fatal(err)
		}
		pending = pending[:len(pending)-1]
	}
}

// BenchmarkMixedFlow replays a random but realistic flow: limit orders within 20 ticks
// of the mid price (some crossing), cancels of recent orders, and small market orders.
func BenchmarkMixedFlow(b *testing.B) {
	book, id := seededBook(b, 100, 10)
	r := rand.New(rand.NewPCG(3, 3))
	recent := make([]OrderID, 0, 1024)
	for b.Loop() {
		switch n := r.IntN(100); {
		case n < 35 && len(recent) > 0: // cancel; the order may already be filled
			i := r.IntN(len(recent))
			_, _ = book.Cancel(recent[i])
			recent[i] = recent[len(recent)-1]
			recent = recent[:len(recent)-1]
		case n < 90:
			side, price := Buy, mid-Price(r.IntN(20))+5
			if r.IntN(2) == 0 {
				side, price = Sell, mid+Price(r.IntN(20))-5
			}
			if _, err := book.Submit(Order{ID: id, Side: side, Price: price, Quantity: Quantity(1 + r.IntN(20))}); err != nil {
				b.Fatal(err)
			}
			if len(recent) < cap(recent) {
				recent = append(recent, id)
			}
			id++
		default:
			side := Buy
			if r.IntN(2) == 0 {
				side = Sell
			}
			if _, err := book.Submit(market(id, side, Quantity(1+r.IntN(10)))); err != nil {
				b.Fatal(err)
			}
			id++
		}
	}
}
