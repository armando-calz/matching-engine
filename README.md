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
- **Latency:** the book's data structures are chosen for fast best-price lookups, inserts and cancels, and measured with Go benchmarks.
- **One writer:** a single goroutine owns the book and receives commands over a channel. There are no locks on the hot path, and the order of events is deterministic.

## Roadmap

- [x] Core types: sides, prices in ticks, quantities in lots, orders
- [x] Order book with price levels and FIFO queues
- [ ] Matching of limit orders with price-time priority, partial fills and trades
- [ ] Market orders and cancellations
- [ ] Benchmarks and a design note on the data structures
- [ ] Single-writer engine goroutine and an API to submit orders and stream trades
- [ ] Live view of a real market: rebuild Bitso's public order book from its feed, detecting gaps by sequence number
- [ ] Docker and a demo

Design decisions are recorded as ADRs in [`docs/adr`](docs/adr).

## Development

```bash
go test -race ./...
```

## License

[MIT](LICENSE)
