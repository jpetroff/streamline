# Configurable column view

Status: implemented in the Svelte frontend. This note supersedes the fixed
three-column and empty-sidebar descriptions in [frontend.md](frontend.md).
The parser, query protocol, and normalized row model are unchanged.

## Implementation

The left sidebar owns a session-only draft of newline-separated JSON paths.
`SourceViewer.svelte` owns applied column configurations. Stdin defaults to
`timestamp`, `level`, `msg`; blank command tabs start with `timestamp`,
`severity`, `message`. `TabController` retains applied configurations per UI tab
between switches and replacement runs. Editing does not affect the table until Apply sends a non-empty, trimmed
list to the viewer; blank lines are ignored while order and duplicates are retained.

[Saved configurations](saved-configurations.md) persist the ordered
`{path, dateFormat}[]` array. Loading commits columns after query replacement and
resets the sidebar draft even when paths are unchanged. A tab without a source
stores loaded columns locally until Run mounts its viewer. Run retains the tab's
applied or prepared columns; unapplied sidebar drafts are excluded. Opening a new
tab with + uses defaults rather than copying the current tab's columns.

VirtualLogTable receives the applied paths as a presentation prop. For each
loaded row, `resolveRowColumnValue` reads normalized timestamp/severity/message
when available, then resolves case-sensitive dotted paths under `LogRow.fields`. A terminal object or array is
compact-serialized as JSON, scalars become text, explicit null becomes null,
and an absent or invalid path becomes a muted em dash.

The column header and virtual rows share a generated CSS grid with a 12 rem
minimum per column. One horizontal overflow container moves both together; the
existing body scroller remains the vertical virtualization element. The 32 px
row-height, bigint logical offsets, viewport paging, snapshots, and follow/pause
behavior therefore remain independent of column configuration.

The helper pins the first non-empty fields object observed in the current input
generation and pretty-prints it. Pinning prevents the example from changing as
virtual pages enter and leave the browser cache. Raw input and record pages
without structured fields show explicit fallback states.

| Part | Responsibility |
| --- | --- |
| [SourceViewer.svelte](../web/src/lib/components/SourceViewer.svelte) | Own applied source state and connect sidebar to table |
| [ColumnSidebar.svelte](../web/src/lib/components/ColumnSidebar.svelte) | Draft editor, line-number gutter, Apply validation, stable sample |
| [columns.ts](../web/src/lib/columns.ts) | Defaults, parsing, path resolution, value formatting, sample selection |
| [VirtualLogTable.svelte](../web/src/lib/components/VirtualLogTable.svelte) | Dynamic headers/cells, absent markers, shared horizontal layout |
| [columns.test.ts](../web/tests/columns.test.ts) | Column contract and Caddy-shaped compatibility cases |

## Interoperability

The feature uses the existing fields member transported in bounded row pages,
so it adds no HTTP route, query parameter, persistence format, or Go type. JSON
and journald records expose their complete cleaned source object. Timestamped
or plain-text rows have no fields object and consequently display the absent
marker for every configured source path. See [parser.md](parser.md) for
normalization ownership and [transport.md](transport.md) for snapshot and
paging guarantees.

~~~mermaid
flowchart LR
  subgraph Go["Existing Go ownership"]
    Input["JSON or journald input"] --> Parser["Parser cleanup"]
    Parser --> Record["logmodel.Record.Fields"]
    Record --> Query["Snapshot row page"]
    Query --> API["GET rows JSON"]
  end

  subgraph Browser["Browser presentation"]
    API --> Viewer["ViewerState pages"]
    Viewer --> Sample["Pinned sample JSON"]
    Viewer --> Rows["Virtual rows"]
    Draft["Numbered path draft"] --> Apply["Apply"]
    Apply --> Columns["App applied string array"]
    Columns --> Header["Dynamic header"]
    Columns --> Resolve["Per-cell path resolver"]
    Rows --> Resolve
    Resolve --> Text["Scalar, compact JSON, or absent marker"]
  end
~~~

This separation preserves several useful contracts:

- Column changes are view-only and never rebuild, filter, reorder, or refetch a
  query. Existing snapshot race guards and the page cache remain authoritative.
- The browser resolves only data already present in a loaded page. It never
  scans the complete result set to discover a schema.
- Field names remain source-format-specific and case-sensitive. The sample is
  the interoperability guide for inputs such as Caddy, journald, or custom JSON.
- Svelte renders values as text, and parser sanitation remains the trust
  boundary; neither field names nor serialized objects are interpreted as HTML.
- Missing and explicit null remain distinct, which allows future typed
  formatters to preserve JSON semantics.

## Expansion boundaries

Keep string arrays as the compatibility shape until a feature needs labels,
widths, visibility, or formatting. At that point introduce a versioned
ColumnDefinition such as { path, label?, width?, formatter? } and migrate legacy
strings to { path }. The pure resolver and formatter functions provide a stable
adapter for both shapes.

~~~mermaid
flowchart TB
  Strings["Current string paths"] --> Definition["Versioned ColumnDefinition"]
  Definition --> Profiles["Saved local or server profiles"]
  Definition --> Layout["Labels, widths, ordering, visibility"]
  Definition --> Formatters["Typed and source-aware formatters"]

  Rows["Bounded row pages"] --> Helper["Current one-record helper"]
  Schema["Future generation-scoped schema endpoint"] --> Helper
  Helper --> Strings

  Definition -. "path reference" .-> QueryDSL["Future server filter, sort, or aggregation"]
  QueryDSL --> QueryService["Query compiler and snapshots"]
~~~

Future work should retain the boundaries above:

- Persist only applied definitions, not editor drafts. Version persisted
  profiles and keep them separate from query snapshots and input generations.
- Add escaped dots and array indexes only through an explicit path-syntax
  version; changing the current dot splitter in place could reinterpret saved
  paths.
- If broader field discovery is needed, expose a bounded, generation-identified
  server schema or field-summary contract. Do not fetch or retain all rows in
  the browser.
- Server-side filtering, sorting, and aggregation may reuse a column path, but
  must validate it in the query compiler rather than trusting browser resolution.
- Rich object inspection or truncation can replace compact cell text without
  changing LogRow.fields; preserve fixed summary-row height and open details
  outside the virtual row.
- Column resizing should update the shared header/body grid and horizontal
  surface only, leaving the vertical virtualizer and logical row identity intact.

Current tests cover draft normalization, duplicate ordering, case-sensitive
object traversal, invalid and missing paths, null/scalar/object/array rendering,
generation-aware sample selection, and the Caddy example: timestamp, level, msg,
request.host, and request.
