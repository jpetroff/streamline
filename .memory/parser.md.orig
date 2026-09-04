# Parser engine

Status: implemented, tested, and connected to progressive runtime stdin ingestion.

## Flow

`parse.NewEngine(Options).Stream(io.Reader, callback)` frames input as it arrives
and emits records after the first recognizable log. `Load` is the whole-input
wrapper. `LoadResult` retains exact source bytes and contains exactly one of a
parsed-record result or display-safe raw text; parsed records refer to the source
with exclusive `RawStart` and `RawEnd` offsets.

```mermaid
flowchart LR
  input[/"streaming io.Reader"/] --> frame["Frame on LF, CRLF, or CR"]
  frame --> clean["Remove terminal controls"]
  clean --> detect{"Recognizable log?"}
  detect -->|"JSON object or timestamped text"| parsed["Parsed stream"]
  detect -->|"Not yet"| buffer["Buffer preamble"]
  buffer --> detect
  parsed --> append["Emit committed record batches"]
  detect -->|"EOF with no match"| raw["Display-safe raw text"]
```

Classification is stream-wide and irreversible. A valid journald/generic JSON
object or timestamped text selects parsed mode; preceding and later visible lines
remain ordinary text records. If no record recognizes the stream before EOF or
a read error, the complete source becomes sanitized raw text instead.

## Normalized record

`logmodel.Record` is the shared parser/query shape:

| Field | Current rule |
| --- | --- |
| `timestamp` | RFC3339Nano UTC; omitted when no supported timestamp is found |
| `severity` | `debug`, `info`, `warn`, `error`, or `fatal` |
| `message` | Clean display text; a recognized text timestamp prefix is removed |
| `fields` | Complete cleaned JSON object using JSON-compatible value types |
| `sourceFormat` | `journald-json`, `json`, or `text` |
| `diagnostics` | Stable codes for non-fatal cleanup or fallback decisions |

Journald recognition requires `__REALTIME_TIMESTAMP` plus a cursor,
monotonic timestamp, or boot ID. It uses `MESSAGE`, maps `PRIORITY`, and
prefers `_SOURCE_REALTIME_TIMESTAMP` over `__REALTIME_TIMESTAMP`. Generic
JSON checks `message/msg/log`, `severity/level/lvl/priority`, and
`timestamp/time/ts/@timestamp`. Plain text recognizes RFC3339,
`YYYY-MM-DD HH:mm:ss`, and syslog month/day timestamps. Nested JSON or logfmt
inside a message is intentionally not parsed again.

```mermaid
flowchart LR
  capture["Engine.Stream / Load"] --> raw[("Exact raw bytes")]
  capture --> entry["Normalized logmodel.Record"]
  raw --> offsets["CapturedRecord offsets"]
  offsets --> entry
  entry -->|"512 records / 100 ms"| append["MemoryService.Append copy"]
  append --> stored[("Query records")]
  stored --> page["Page result copy"]
  page --> wire["Frontend LogRow"]
  raw --> rawResult["Sanitized RawResult"]
  rawResult -->|"Bounded 64 KiB chunks after EOF/error"| browser["Raw frontend panel"]
```

## Decision ledger

| Revisitable decision | Implemented in | Revisit when |
| --- | --- | --- |
| Stream complete frames while retaining exact source bytes | `Engine.Stream`, `LoadResult` in [engine.go](../internal/parse/engine.go) | Input limits, disk spill, or raw replay are introduced |
| Treat LF, CRLF, lone CR, and a final unterminated line as record boundaries | `Engine.Stream` framing in [engine.go](../internal/parse/engine.go) | Multiline records or terminal screen replay are added |
| Strip ANSI/ECMA-48 and unsafe C0/C1 controls with a state machine | `sanitizeBytes` in [sanitize.go](../internal/parse/sanitize.go) | Styled output should be preserved or more terminal protocols are supported |
| Drop only styled `less` filler/status lines; retain uncertain content | `isPagerArtifact`, `hasLessTruncation` in [sanitize.go](../internal/parse/sanitize.go) | Other pagers need explicit artifact rules |
| Select parsed mode on the first JSON object or timestamped text record | `Engine.Stream`, `parseRecord` | A parser registry, confidence scoring, or more formats are added |
| Preserve structured JSON types with `json.Number` in Go | `decodeJSON`, `cleanJSONValue` in [normalize.go](../internal/parse/normalize.go) | Exact numeric spelling must cross the browser boundary |
| Prefer source time, fall back to journal receipt time, and map syslog priority | `normalizeJournald` in [normalize.go](../internal/parse/normalize.go) | Receipt-time ordering or all eight syslog levels are required |
| Use configurable timezone/reference context for incomplete text timestamps | `parseLeadingTimestamp` in [timestamp.go](../internal/parse/timestamp.go) | Input-specific timezone/year controls are exposed |
| Expose terminal unrecognized input only as display-safe bounded chunks | `RawResult`, `GET /api/v1/input/raw` | Exact-byte download or raw-record details are designed |
| Deep-copy nested fields at query append and page boundaries | `CloneRecord` in [model.go](../internal/logmodel/model.go), `Append` and `Page` in [memory.go](../internal/query/memory.go) | Records become immutable value objects or storage ownership changes |

Common diagnostic codes are
`terminal_controls_removed`, `terminal_artifact_skipped`,
`terminal_truncated`, `invalid_utf8_replaced`,
`malformed_json_fallback`, `timestamp_context_assumed`,
`invalid_timestamp`, `missing_message`, `non_text_message`,
`invalid_severity`, and `multiple_journal_values`.

## Boundaries and verification

- `cmd/streamline` starts `internal/ingest` in the background. It coalesces
  parsed entries into batches of 512 records or 100 ms before append.
- Pretty/multiline JSON, journal export format, stack-trace grouping, and nested
  message parsing are out of scope.
- [engine_test.go](../internal/parse/engine_test.go) covers the committed fixture,
  mixed formats, terminal cleanup, time context, malformed input, raw spans,
  long lines, partial reader failures, and the supplied local datasets when
  present.
- Query/API/frontend tests cover typed-field transport and defensive copying.
