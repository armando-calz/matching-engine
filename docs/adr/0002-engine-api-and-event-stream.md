# ADR 0002 — Engine API and sequenced event stream

- **Status:** Accepted
- **Date:** 2026-11-04

## Context

ADR 0001 decided that one goroutine owns each book. Clients on other goroutines still need to submit and cancel orders, get an answer, and learn about every change to the book, including trades against their resting orders that other clients' orders caused. The `orderbook` package is single-threaded on purpose and knows nothing about this.

## Decision

### Commands are closures run by the owner

The `engine` package keeps the book inside one goroutine. `Submit`, `Cancel` and `Depth` each send a closure over an unbuffered channel and wait for it to finish. The owner runs commands one at a time, so the book code needs no locks and the order of commands is the order in which the owner received them.

### The engine assigns order ids

Ids are 1, 2, 3… in acceptance order. Clients cannot collide with each other's ids, and an id also says which order arrived first. Rejected orders (invalid side, type, price or quantity) get no id.

### Every change is an event, numbered without gaps

| Event | Published when |
|---|---|
| `Accepted` | a valid order enters, with its id, before matching |
| `Traded` | it fills against a resting order (one event per fill) |
| `Rested` / `Dropped` | the unfilled rest of a limit order rests / of a market order is discarded |
| `Cancelled` | a resting order leaves the book |

`Seq` starts at 1 and increases by one per event. A consumer that sees a jump knows it missed events. The stream is a complete **journal**: replaying its `Accepted` and `Cancelled` events, in order, into a new book reproduces every other event. A test checks exactly this after concurrent clients.

### Backpressure, not loss

The stream is a buffered channel. When it is full, the engine waits for the consumer instead of dropping events: a gap in a journal is worse than a slow engine. A caller can bound its own wait with a context deadline.

## Alternatives considered

| Option | Why not |
|---|---|
| A mutex around the book | Every caller contends for the lock, and the order of events depends on the scheduler, not on one sequence the engine controls. |
| Clients choose order ids | Collisions between clients, and every client has to coordinate. |
| Drop events when the consumer is slow | Market data feeds do this and recover with snapshots, but this stream is also the journal. A separate, lossy market data feed can be built on top of it. |
| One channel per event type | Consumers would have to merge them back into one order. |

## Consequences

- ✅ Concurrent clients, no locks in the book, deterministic and replayable history.
- ✅ Gap detection on the consumer side, the same technique used to rebuild Bitso's book from its public feed later in the roadmap.
- ⚠️ A stuck consumer stalls the engine. That is deliberate, and a consumer that cannot keep up should hand events to its own queue.
- ⚠️ If a caller's context ends after its command reached the engine, the call returns the context's error but the command may have been applied. The event stream is the record of what happened.
- ⚠️ When the engine stops in the middle of a command, the events not yet published are lost with the in-memory book. Persistence is out of scope (ADR 0001).
