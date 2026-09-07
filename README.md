# Streamline

A local log-viewer scaffold with a Go binary, versioned query transport, and a
dark Svelte interface with viewport-driven log virtualization.

## Get started

Requires global:
* Go 1.27.1+, 

* Bun 1.4.0+, 

* Make.
 
See the [development setup](.memory/development.md#setup) for installation and
custom Homebrew paths.

```sh
make setup
make dev
```

Open [localhost:5173](http://localhost:5173). Development starts in “Waiting
for stdin…” and accepts terminal input until EOF. Recognized JSON or timestamped
logs appear progressively; otherwise EOF switches to a raw text view. The health
endpoint is [localhost:5173/api/v1/health](http://localhost:5173/api/v1/health).
Press Ctrl+C to stop both processes.

```sh
make check
make build
./bin/streamline
```

The standalone binary serves the UI at [localhost:8080](http://localhost:8080).
Pipe input into it, for example
`printf '{"message":"hello"}\n' | ./bin/streamline`, then open the UI. Plain
non-log output is shown in the raw view after EOF. Use `make run PORT=8081` or
`./bin/streamline -port 8081` to choose another port.

Setup checks the global Go and Bun installations and installs dependencies
from `bun.lock`. Bun runs the frontend tooling. The built binary runs on its own.

- [Architecture and extension diagrams](.memory/architecture.md)
- [Frontend visual output and virtualization](.memory/frontend.md)
- [Development, commands, and debugging](.memory/development.md)

The frontend shell, stdin ingestion, parser, parsed/raw transport, in-memory
query service, health endpoint, and build tooling are implemented.


## Search logs

Use the search textarea below the table, then click **Apply** or press
**Ctrl/Cmd+Enter**. Search is case-insensitive and includes nested values even
when their fields are not visible as columns. One expression goes on each line;
**OR** matches any line, while **AND** requires every line to match somewhere in
the same entry. Applying an empty search clears it.

**Plain** treats punctuation literally. **Regexp** accepts expressions such as
`timeout|refused` or `status=[45][0-9]{2}`, without slash delimiters. Invalid lines
have an error bullet with details on hover or keyboard focus. Browser syntax
validation runs immediately; Go validates again on Apply and can reject JS-only
features such as lookaround and backreferences. Previous results stay visible
if a search is rejected. Raw-output search and saved searches are not included.

### Field filters

The sidebar starts with no filters. Add conditions in the builder, or use **Import JSON** to replace the draft with a saved array. **Apply** updates the results; **Clear all** followed by **Apply** removes the filters. **Copy JSON** copies the valid draft for reuse. The `(?)` buttons beside Columns and Filters show a sample of the original JSON.

```json
[
  { "field": "level", "op": "eq", "value": "error" },
  { "field": "duration", "op": "gte", "value": 100 }
]
```

Conditions combine with AND in declaration order, before general search. Field names are case-sensitive dotted object paths into the original JSON, with no array indexing. `eq`, `contains`, and `regex` take string operands and match scalar text without case sensitivity. Missing fields, objects, and arrays do not match. Regex uses the same browser syntax checks and authoritative Go validation as search; each value is one expression, including embedded newlines.

`gt`, `gte`, `lt`, and `lte` take JSON number operands and match only numeric source fields, excluding numeric-looking strings. Fields do not need to appear in the sample to be used. For HTTP clients, `POST /api/v1/queries` now accepts the array as its `filter` property alongside `sort` and `search`; the former string expression is no longer accepted. Validation failures return `invalid_filter` with `filterErrors` identifying the one-based filter `index`, `property`, and `message` (index `0` identifies an invalid top-level array).

## Keyboard navigation

`Mod` means Command on macOS and Control on Windows/Linux. Explicit `Ctrl` means
Control on every platform; `Alt` means Option on macOS.

| Action | Shortcut |
| --- | --- |
| Focus search | `Mod+F` |
| Open sidebar and focus column paths | `Mod+B` |
| Add a filter and focus its field | `Mod+Alt+F` |
| Focus Jump to row | `Ctrl+G` |
| Move one row in the log list | `Up` / `Down` |
| Move ten rows in the log list | `Page Up` / `Page Down` |
| Move one row while retaining input focus | `Ctrl+Alt+Up` / `Down` |
| Move ten rows while retaining input focus | `Ctrl+Alt+Shift+Up` / `Down` |
| Open the active row's preview | `Shift+Enter` in the log list, or Shift-click a row |
| Apply the focused search, columns, or filters block | `Mod+Enter` |
| Execute Jump to row | `Enter` in the jump field, or **Go** |
| Dismiss overlay, close preview, or return to the active row | `Escape` |

The newest result starts active. Moving to an older row pauses live following,
even when the newest row is still visible. Moving back to the last row or
explicitly scrolling to the bottom resumes following. An open preview follows
the active row. Applying search or filters starts the new results at their newest
row while keeping keyboard focus in the editor.

Jump uses the **one-based position in the current results**, not the original
source ID. Invalid positions leave focus in the jump field and explain the valid
range. Filter Tab order is field, operator, value, remove, then the next filter;
**Add filter** is the final form action and focuses the newly added field.

Global shortcuts work from active inputs without first blurring. Ordinary arrows,
selection, Tab, and multiline Enter retain their normal input behavior. Modal
imports own keyboard focus until dismissed; `Mod+Enter` imports their JSON draft.
Record shortcuts are unavailable for raw output. Some operating systems reserve
the global movement combinations; local log-list keys remain available.

### Adding component commands

The root provides a `CommandRegistry` through Svelte context. Components call
`registerCommand` from `web/src/lib/keyboard-context.ts` during initialization;
registration and cleanup follow component mounting. Supply a unique ID, label,
bindings, handler, and optional live `scope`/`when` functions. `allowInInput`
explicitly enables input shortcuts; `repeat` is reserved for row movement.
`changesFocus` dismisses registered popovers before the handler executes.

Scoped bindings use the deepest matching element. A single window capture
listener routes each recognized event once, before input bubbling handlers.
Unrecognized combinations are untouched; composition and AltGraph are ignored.
Use `registerOverlay` for modal or popover ownership, `keyboard.execute(id)` for
programmatic commands, and `keyboard.label` / `keyboard.aria` for platform hints.
