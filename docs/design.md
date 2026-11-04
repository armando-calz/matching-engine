# Design note: the book's data structures

How the order book is laid out in memory, why, and what it costs, measured with the benchmarks in [`orderbook/bench_test.go`](../orderbook/bench_test.go).

## Operations that matter

A matching engine spends its time on four operations, and they are not equally frequent:

| Operation | When | How often |
|---|---|---|
| Read the best bid or ask | every incoming order | always |
| Add an order to an existing level | quoting near the best price | very often |
| Cancel an order by id | market makers requote constantly | very often; most orders are cancelled, not filled |
| Open or close a price level | the book moves | less often, and mostly near the best price |

## Layout

```text
Book
├── orders  map[OrderID]*restingOrder        cancel: find the order in O(1)
├── bids    bookSide
│   ├── prices  []Price  sorted worst → best  best price: the last element, O(1)
│   └── levels  map[Price]*level              level for a price, O(1)
└── asks    bookSide (same)

level (one price)
└── head ⇄ order ⇄ order ⇄ tail               FIFO queue, doubly linked through the orders
```

- **Price levels in a sorted slice, best price last.** Matching always consumes the best level, and removing the last element of a slice is a truncation. Opening a level costs a binary search plus moving the elements after it; near the best price that is almost nothing, and deep in the book it is one `memmove` of a few kilobytes. A balanced tree would make every operation O(log n) with a pointer chase per step, which costs more than a short copy for books of realistic depth.
- **Intrusive doubly linked FIFO queues.** Each resting order is its own queue node, so a cancel found through the `orders` index unlinks in O(1), wherever it sits in the queue. A slice per level would make cancels O(queue length).
- **Integer prices and quantities.** No decimal library and no floating point on the hot path (ADR 0001).

## Measurements

Apple M4, Go 1.24, median of 6 runs of `go test -run '^$' -bench . -benchmem ./orderbook`. The books start with 100 levels per side and 10 orders per level, unless noted.

| Benchmark | Time per op | Allocations per op |
|---|---:|---:|
| Cancel a random resting order | **35 ns** | 0 |
| Take one resting order, then put a new maker back (two `Submit` calls) | 95 ns | 2 |
| Open a level at the best price and close it (1,000 levels per side) | 108 ns | 2 |
| Open a level at the worst price and close it (1,000 levels per side) | 272 ns | 2 |
| Mixed flow: 55% limit orders near the mid price, 35% cancels, 10% market orders | **129 ns** (≈ 7.7 million orders per second on one core) | 1 |
| Rest a non-crossing order on an existing level, book growing by millions | 181 ns | 1 |
| Sweep 10 levels (100 fills) with one order | 4.2 µs (≈ 41 ns per fill) | 8 |

What the numbers say:

- **Cancels are the cheapest operation**, as intended: a map lookup and four pointer writes, with no allocation.
- **The sorted slice is a real trade-off, and a good one.** Opening a level at the far end of a 1,000-level book costs 2.5 times as much as at the best price (two `memmove`s of 8 KB), and it stays well under a microsecond. Real order flow concentrates near the best prices.
- **The garbage collector dominates in a growing book.** Profiling `BenchmarkSubmitResting` puts about half of the CPU time in the collector's mark phase (`scanobject`, `findObject`) and most of the rest in the runtime's memory management (`madvise`); the book's own code, the map lookup included, is a few percent. Every resting order is a heap object with three pointers, and the collector has to trace millions of them.
- **Sweeps allocate while the trade slice grows**: 100 trades take 8 allocations as `append` doubles the slice.

## What a production engine would change

These are deliberately left out to keep the code readable; the benchmarks are there to measure each one.

1. **Pool the orders in a preallocated slice and link them by index (`int32`) instead of pointers.** The heap then holds one large pointer-free array that the garbage collector does not need to scan, which removes the dominant cost above and keeps the nodes contiguous in memory.
2. **Let the caller pass a reusable trade buffer** (`SubmitInto(o, trades[:0])`), so matching does not allocate.
3. **Index levels by price offset from a reference price** (an array of levels, one per tick, within a band) when the tick size and price band are known. Best-price moves become array scans and the price map disappears.

## Reproducing

```bash
go test -run '^$' -bench . -benchmem -count 6 ./orderbook
go test -run '^$' -bench 'SubmitResting$' -cpuprofile cpu.out ./orderbook && go tool pprof -top orderbook.test cpu.out
```
