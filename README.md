# matching-engine

[![CI](https://github.com/armando-calz/matching-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/armando-calz/matching-engine/actions/workflows/ci.yml)

An **order book and matching engine** in Go, the core of any exchange: it keeps the buy and sell orders waiting for a counterparty, and matches incoming orders against them with **price-time priority**.

> **Status:** 🚧 in development. See the roadmap below.

## What it does

An order book for a pair such as BTC/MXN holds resting orders on two sides:

```text
        ASKS (sell)            price        quantity
                          1,251,000.00     0.30 BTC
                          1,250,500.00     0.10 BTC   ← best ask
   ───────────────────── spread ─────────────────────
                          1,249,800.00     0.25 BTC   ← best bid
                          1,249,000.00     1.00 BTC
        BIDS (buy)
```

An incoming order to **buy 0.20 BTC at up to 1,251,000.00** crosses the spread. It fills 0.10 at 1,250,500.00 (the best price first), then 0.10 at 1,251,000.00. Each fill is a trade at the resting order's price, and whatever cannot be filled rests in the book.

The interesting parts:

- **Price-time priority:** the best price is matched first and, at the same price, the order that arrived first. That is the fairness rule of every exchange.
- **Exact arithmetic:** prices and quantities are integers (ticks and lots), never floating point.
- **Latency:** the book's data structures are chosen for fast best-price lookups, inserts and cancels, and measured with Go benchmarks: a cancel takes 35 ns and a realistic mixed flow about 130 ns per order on one core ([design note](docs/design.md)).
- **One writer:** a single goroutine owns the book and receives commands over a channel. There are no locks on the hot path, and the order of events is deterministic.

## Usage

```go
e := engine.Start(ctx, 1024) // one goroutine owns the book until ctx ends

go func() {
	for ev := range e.Events() { // Accepted, Traded, Rested, Dropped, Cancelled; Seq 1, 2, 3...
		fmt.Println(ev.Seq, ev.Kind)
	}
}()

r, err := e.Submit(ctx, orderbook.Order{Side: orderbook.Sell, Price: 1_250_500_00, Quantity: 10_000_000})
// r.ID is assigned by the engine; r.Trades, r.Rested and r.Dropped say what happened
```

Any number of goroutines can call `Submit`, `Cancel` and `Depth`. The event stream has no gaps and is a complete journal: replaying it into a new book reproduces every trade ([ADR 0002](docs/adr/0002-engine-api-and-event-stream.md)).

## Roadmap

- [x] Core types: sides, prices in ticks, quantities in lots, orders
- [x] Order book with price levels and FIFO queues
- [x] Matching of limit orders with price-time priority, partial fills and trades
- [x] Market orders and cancellations
- [x] Benchmarks and a [design note](docs/design.md) on the data structures
- [x] Single-writer engine goroutine and an API to submit orders and stream trades
- [ ] Live view of a real market: rebuild Bitso's public order book from its feed, detecting gaps by sequence number
- [ ] Docker and a demo

Design decisions are recorded as ADRs in [`docs/adr`](docs/adr).

## Development

```bash
go test -race ./...                                  # tests
go test -run '^$' -bench . -benchmem ./orderbook     # benchmarks
```

## License

[MIT](LICENSE)
