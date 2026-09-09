# Saved configurations

Explicitly saved, named command/viewer bundles. Persistence is owned by the Go
binary. Source data and browser session preferences remain in memory.

## Ownership

| Source | Responsibility |
| --- | --- |
| [configuration](../internal/configuration/) | `Document`, strict decoding, semantic validation, directory resolution, file store |
| [configurations.go](../internal/httpapi/configurations.go) | List/get/create/update/validate routes; injected through `NewConfiguredSourceHandler` |
| [configurations.ts](../web/src/lib/configurations.ts) | TypeScript document/draft types, serialization, validation feedback, HTTP client |
| [ConfigurationSettings.svelte](../web/src/lib/components/ConfigurationSettings.svelte) | Toolbar trigger, Bits UI dialog, entry list, editor, clone, dirty-state guards |
| [SourceViewer.svelte](../web/src/lib/components/SourceViewer.svelte), [App.svelte](../web/src/App.svelte) | Live snapshots, configuration application, command preparation, new-tab preference seeding |
| [viewer-controller.ts](../web/src/lib/transport/viewer-controller.ts) | `setQueryAndWait`: settle on displayed replacement, failure, supersession, or disposal |

## Persistence contract

- Production: `~/.config/streamline`; independent of `XDG_CONFIG_HOME`.
- Development: `.local/streamline`; `make dev-go` supplies the absolute repository
  path. Direct `dev` builds resolve this default against the working directory.
- `-config-dir` overrides the root; relative overrides become absolute.
- Entries: `configs/<id>.json`. API-generated IDs are 16 random bytes encoded as
  hex. Copied filenames accept 1–128 ASCII alphanumeric/underscore/hyphen characters,
  starting with an alphanumeric. Display names are independent and may repeat.
- Required v1 fields: `version`, `name`, `command`, `mode`, `columns`, `filters`.
  `columns` is ordered `{path, dateFormat}[]`; `filters` contains `filter: FilterSpec[]`
  and `search: {text, mode, operator}`. Command/search whitespace is retained.
- UTF-8, indented JSON; 64 KiB request/file limit, also checked after formatting.
  New directories/files use `0700`/`0600`. Updates use exclusive temporary-file
  creation, write, sync, close, and same-directory rename.
- File operations use `os.Root`, validated IDs, and regular-file checks. The
  `configs` directory and entry files cannot be symbolic links.
- Reads reopen files. Refresh/reopening settings discovers copied or edited files.
  List isolates invalid-file diagnostics. Malformed/unsupported documents are not
  rewritten; updates require an existing valid entry. Storage errors leave viewing available.

[Transport](transport.md#saved-configurations) defines endpoints and errors.
[README](../README.md#saved-configurations) contains a complete document example.

## State transitions

**Save current as new:** `snapshot()` copies applied columns/date formats and the
retained applied query specification without unmounting. The selected source
supplies command/mode; stdin supplies empty command/`auto`. Unsubmitted drafts and
pending query specifications are excluded. Clone copies a persisted document into
an unsaved draft with a new name; create assigns a new ID. Save changes only the file.

```mermaid
sequenceDiagram
  participant UI as Settings / App
  participant API as Configuration API
  participant Viewer as SourceViewer
  participant Query as ViewerController
  UI->>API: GET entry; POST validate
  UI->>UI: Verify original tab and intent
  UI->>Viewer: applyConfiguration(document)
  alt Records or pending input
    Viewer->>Query: setQueryAndWait(filter, search)
    Query-->>Viewer: Replacement displayed or error
  else Raw input
    Viewer->>Viewer: Retain specification without query
  end
  Viewer->>Viewer: On success: commit columns; reset editors
  Viewer-->>UI: Success or error
  UI->>UI: On success: fill command/mode; close dialog
```

Loading never executes or changes the source process. Query failure preserves the
previous display and columns. Tab changes/disposal reject stale completion. Editor
revision keys reset drafts even when loaded values equal the existing applied values.

**Run:** snapshot before source creation; seed the new tab before selection.
Command tabs inherit applied settings. Stdin inherits only after explicit preparation
(`inheritOnRun`); untouched stdin preserves normal command column defaults. New tabs
start following, with no saved row offset and one row line. **Run again** uses the
selected source's actual command/mode with its current applied settings.

## Validation and interaction

- Structural validation checks JSON shape, names, output modes, columns/date formats,
  filter operands, and search options. Empty commands are valid; shell syntax is not checked.
- Browser regex checks are advisory in settings. `TupleCompiler.Compile` and
  `ValidateSearch` provide authoritative Go validation without queries or execution.
  Errors identify fields, one-based column/filter indices, and physical search lines.
- The dialog owns focus and scoped `configurations.save` (`Mod+Enter`). Escape and
  outside dismissal preserve dirty drafts. Leaving requires save/discard; closing
  restores trigger focus. Save and Load are separate actions.
- No automatic history, tab restoration, file watcher, delete API, or import/export
  wizard. Scroll position, selected row, panel sizes, follow state, row height,
  source IDs, query IDs, and snapshot tokens are excluded from persisted documents.

## Verification

- [Store tests](../internal/configuration/store_test.go): round trips, clones, file
  transfer/external edits, permissions, confinement, validation, atomic-write failure.
  Build-tagged tests cover development and production directory defaults.
- [API tests](../internal/httpapi/configurations_test.go): request protection,
  validation without side effects, updates, diagnostics, storage-failure isolation.
- [Frontend tests](../web/tests/configurations.test.ts) and
  [controller tests](../web/tests/viewer-controller.test.ts): serialization, copied
  state, regex feedback, replacement completion/failure/cancellation.
- [Browser tests](../web/e2e/configurations.spec.ts): capture/edit/clone/restart/load/run,
  editor resets, raw/pending input, focus, unsaved changes, and source-removal races.
  [Command tests](../web/e2e/commands.spec.ts) verify inherited settings remain isolated.

Run `make check`, `make build`, and
`bun run --bun --filter @streamline/web test:e2e configurations.spec.ts commands.spec.ts`.
Browser configuration tests use temporary roots through `-config-dir`.
