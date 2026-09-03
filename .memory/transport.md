# Binary–frontend transport

## What changed

Streamline now has a versioned transport between the Go binary and Svelte
frontend. Go owns query execution, matching indexes, immutable result snapshots,
and session state. The browser sends commands and requests bounded row windows;
it never receives or retains the full result set.

The implementation adds:

- an in-memory query coordinator with atomic initial scans and live append support;
- JSON endpoints for session state, query commands, query recovery, pagination,
  and resource release;
- an SSE endpoint for progress and state notifications;
- a Svelte viewer controller with intent/revision guards, follow and pause modes,
  atomic filter replacement, and a 32 MB row-page cache;
- shared JSON fixtures plus Go and frontend tests for the wire contract and
  concurrency-sensitive behavior.

The binary currently has no stdin ingestion or shared filter-expression engine.
Its production compiler accepts the empty, input-order query. `query.Compiler`
and `MemoryService.Append` are the integration points for those later systems.

## Architecture

```mermaid
flowchart LR
  Ingest["Future stdin ingestion"] -->|"Append complete batch"| Query["Go query service"]
  Engine["Future expression compiler"] --> Query
  Query --> Index["Matching record IDs"]
  Index --> Snapshot["Immutable logical snapshots"]
  Browser["Svelte viewer controller"] -->|"commands and row windows / JSON"| API["Go HTTP API"]
  API --> Query
  Query -->|"progress and state only"| SSE["SSE subscriptions"]
  SSE --> Browser
  API -->|"bounded rows"| Browser
```

`internal/query` contains transport-facing domain types and `Service`,
`Compiler`, and `Subscription` interfaces. `MemoryService` is session-scoped and
keeps captured records, matching-ID indexes, and snapshot descriptors in RAM.
`internal/httpapi` maps those interfaces to `/api/v1`. The binary injects the
service when it builds the HTTP handler.

The frontend separates concerns into wire types, an HTTP/SSE client, a bounded
LRU page cache, a pure state reducer, and `ViewerController`. `App.svelte` owns
one controller for its lifecycle and renders only the current window.

## HTTP and SSE contract

All values that may exceed JavaScript's safe integer range use decimal strings.
Every displayable snapshot contains `sessionId`, `generationId`, `queryId`,
`revision`, `processedThrough`, `matchedCount`, and an opaque `snapshotToken`.
Every row has a stable decimal-string record ID.

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/session` | Returns session/generation identity and input status. |
| `POST /api/v1/queries` | Validates a filter/sort command and returns `202` with initial query state. |
| `GET /api/v1/queries/{id}` | Returns authoritative state for reconnect or Resume. |
| `GET /api/v1/queries/{id}/rows` | Reads an `offset`/`limit` window from an exact snapshot token. |
| `GET /api/v1/queries/{id}/events` | Sends initial state, then progress, snapshot, input, and generation notifications. |
| `DELETE /api/v1/queries/{id}` | Cancels and releases query resources without deleting session records. |

Row requests default to 200 and reject limits above 1,000. Errors use
`{"error":{"code":"…","message":"…"}}`; callers make decisions from the
stable code. SSE never carries records. The handler sends an authoritative
initial event, coalesces ordinary changes over 100 ms, keeps only the newest
queued event for a slow subscriber, sends 15-second heartbeats, and gives each
write a five-second deadline.

## Consistency and lifecycle

Creating a query captures the current committed record boundary. The initial
scan runs against that prefix. Before publishing `ready`, it takes the append
lock, evaluates any records accepted during the scan, and publishes one complete
snapshot. This prevents a gap between historical scanning and live evaluation.

A snapshot token identifies a count within the append-only matching-ID index.
Later snapshots can extend that index without changing earlier prefixes, so
pagination through an older token remains stable. Queries are pinned while an
SSE subscriber is connected. Creation and disconnect start a 60-second grace
period; expiry or explicit deletion releases the query's result state.

The frontend increments an intent for each filter change. Superseded fetches are
aborted, and responses must still match the current intent, query, revision, and
snapshot token before they can update the display. Revision requests are ordered
so a late page from an older notification cannot replace a newer one. Failed
page loads can retry the same revision.

A filter change leaves the prior rows visible while the replacement query scans.
When the first complete page arrives, the reducer performs a single `replace`
transition and releases the old query. An invalid filter retains the prior view
and reconnects its stream.

Following uses the newest snapshot and loads its tail. Navigating to an older
window applies `pause`, pinning the displayed snapshot while Go continues
capture and query evaluation. Resume fetches authoritative query state, loads
its latest tail, and then reenables live updates. Full prefix pages are reusable
across append-only revisions; partial tail pages are refetched when the matching
count grows.

EventSource reconnects automatically, while the controller also uses the query
state endpoint to recover missed or coalesced notifications. If a following
query has expired, it is rebuilt. A paused view never silently jumps when its
snapshot is unavailable; it keeps cached rows and exposes an explicit refresh
state. A generation notification similarly starts a replacement query while the
old display remains visible.

## Extension rules

- The future query engine implements `query.Compiler`; the HTTP layer must not
  introduce a second filter syntax.
- Ingestion calls `MemoryService.Append` only with complete committed batches and
  calls `SetInputStatus` for EOF or terminal input errors.
- New sort orders must state whether they are live append-only or fixed at query
  creation. Input order is currently the only accepted sort.
- Aggregates and record-detail endpoints should accept the same snapshot token
  so every part of the UI describes one revision.
- Future storage providers implement the query/storage contracts with the same
  stable logical IDs and snapshot behavior; browser-visible tokens stay opaque.
- Rows remain on bounded JSON endpoints. SSE remains a notification channel to
  preserve backpressure and reconnect behavior.

## Verification

`make check` runs Svelte diagnostics, frontend reducer/controller/cache tests,
Go tests, Go vet, and formatting checks. The Go suite covers append-during-scan,
stable old-snapshot pagination, compiler injection, notification coalescing,
query expiry, HTTP validation, structured errors, and initial SSE state. The
frontend suite covers atomic replacement, reversed query responses, pause
semantics, partial-tail cache invalidation, and the shared 64-bit JSON fixture.
A production frontend build and standalone Go build verify the embedded path.
