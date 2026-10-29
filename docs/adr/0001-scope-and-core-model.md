# ADR 0001 — Scope and core model

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

A matching engine can grow without limit: dozens of order types, fees, self-trade prevention, auctions, persistence, multiple instruments, market data distribution. This project needs a scope small enough to finish and test thoroughly, while keeping the parts that make matching engines interesting: correctness of the matching rules, and latency.

## Decision

### In scope

- **One instrument per book** (e.g. BTC/MXN). Several instruments are several independent books.
- **Limit orders** with **price-time priority**: better prices first; at equal prices, earlier orders first. A limit order matches as far as its price allows, and the rest rests in the book.
- **Market orders**: match against the best available prices until filled or the book side is empty. They never rest.
- **Cancellations** of resting orders, by id.
- **Partial fills**; every match produces a **trade** at the resting (maker) order's price.

### Exact integers for money

- **Price** is an integer number of **ticks** (the smallest price increment, e.g. 0.01 MXN).
- **Quantity** is an integer number of **lots** (the smallest tradable amount, e.g. 0.00000001 BTC, one satoshi).

Converting to and from human decimals happens only at the edges. Floating point is never used for prices or quantities, for the same reason as in [ledger-core](https://github.com/armando-calz/ledger-core): `0.1 + 0.2 != 0.3`.

### Concurrency: a single writer

One goroutine owns each book. Clients send commands (new order, cancel) over a channel, and the goroutine applies them one at a time and publishes the resulting events (accepted, trade, cancelled). There are no locks on the matching path, the order of events is deterministic, and the book code itself stays plain, single-threaded Go that is easy to test. This is also how many real exchanges run their matching engines.

### Out of scope

Stop orders, iceberg orders, time-in-force variants beyond "rest in the book", fees, self-trade prevention, auctions, persistence and recovery of the book, and accounts or balances. ([ledger-core](https://github.com/armando-calz/ledger-core) covers money movement.)

## Consequences

- ✅ A small, complete core that can be tested exhaustively and benchmarked.
- ✅ Deterministic behavior: replaying the same commands produces the same trades.
- ⚠️ One goroutine per book caps throughput at what a single core can do for one instrument. In practice this is the norm, and books for different instruments scale independently.
- ⚠️ No persistence: a restart loses the book. A production engine would journal commands and replay them on recovery (event sourcing), which the deterministic design makes straightforward.
