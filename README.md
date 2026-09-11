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
for stdin…” and accepts terminal input until EOF. Recognized JSON, logfmt, syslog, HTTP access, or timestamped
logs appear progressively; otherwise EOF switches to a raw text view. The health
endpoint is [localhost:5173/api/v1/health](http://localhost:5173/api/v1/health).
Press Ctrl+C to stop both processes.

In Coder, share port 5173 and open its HTTPS URL. Development also trusts
`https://*.coder.intranet` origins, including dynamically assigned shared-port
subdomains. Restart `make dev` after changing the trusted origins in
`internal/webassets/dev.go`.

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

### Build archive and installation

`make build` also produces `bin/streamline.<GOOS>.<GOARCH>.tar.gz`, containing the
`streamline` executable (with all web assets embedded) and `README.md`. It defaults
to the build machine's OS and architecture. Set `GOOS` and/or `GOARCH` as Make
arguments or environment variables to select a target, using Go's platform names:

```sh
make build                         # e.g. bin/streamline.linux.amd64.tar.gz
make build GOARCH=arm64             # host OS, ARM64 architecture
make build GOOS=darwin GOARCH=arm64  # bin/streamline.darwin.arm64.tar.gz (macOS)
```

Archives for different targets have distinct filenames. Each build replaces
`bin/streamline` with the selected target's executable.

### Binary version

`./bin/streamline -v` prints the package version, UTC build date and time, and Git
commit hash, then exits. Both `make build` and `make dev-go` embed this metadata.
Builds without a Git checkout report `unknown` for the commit hash; direct
`go build` commands without the Make linker flags report `dev` with unknown build
time and commit.

The package version is stored in `VERSION`, initially `0.1.0`. Edit it manually,
or run `make version-bump` to increment the minor number and reset the patch to
zero (for example, `0.1.0` becomes `0.2.0`). Commit the updated `VERSION` file and
rebuild to include the new version in the binary. Building does not bump it.

### Installation

Publish the archive, replace the default `TARBALL_URL` in `scripts/install.sh`
with its URL, and host the installer. Users can then run:

```sh
curl -sS https://your-host.example/install.sh | sh
```

The tarball URL can also be overridden without editing the script:

```sh
curl -sS https://your-host.example/install.sh | STREAMLINE_TARBALL_URL=https://your-host.example/streamline.linux.amd64.tar.gz sh
```

Choose a build matching the destination system's OS and architecture. Installation
requires `curl`, `tar` with gzip support, and standard POSIX shell utilities; no
Go, Bun, or root access is needed. The installer places the executable and all
supplementary archive files in `~/.local/streamline`, then links
`~/.local/bin/streamline` to the executable. Add `~/.local/bin` to your `PATH` if
needed. The installer displays the download URL, extraction paths, files being
transferred, installation directories, launcher link, and temporary-file cleanup.
Rerun the installer to update an existing installation.

Run `streamline` from any directory: the UI is embedded, so a working-directory
shim is unnecessary. Commands launched in the UI inherit the caller's working
directory. Saved configurations remain in `~/.config/streamline` (or the directory
selected by `-config-dir`).

- [Architecture and extension diagrams](.memory/architecture.md)
- [Frontend visual output and virtualization](.memory/frontend.md)
- [Development, commands, and debugging](.memory/development.md)
- [Saved configuration implementation](.memory/saved-configurations.md)

The frontend shell, stdin and command ingestion, parser, parsed/raw transport, in-memory
query service, saved configurations, health endpoint, and build tooling are implemented.


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
if a search is rejected. Raw-output search is not included. Search can be stored in a saved configuration.

### Field filters

The sidebar starts with no filters. Add conditions in the builder, or use **Import JSON** to replace the draft with a saved array. **Apply** updates the results; **Clear all** followed by **Apply** removes the filters. **Copy JSON** copies the valid draft for reuse. The `(?)` buttons beside Columns and Filters show a sample of the original JSON.

```json
[
  { "field": "level", "op": "eq", "value": "error" },
  { "field": "duration", "op": "gte", "value": 100 }
]
```

Conditions combine with AND in declaration order, before general search. Field names are case-sensitive dotted object paths into the original JSON, with no array indexing. `eq`, `contains`, and `regex` take string operands and match scalar text without case sensitivity. Missing fields, objects, and arrays do not match. Regex uses the same browser syntax checks and authoritative Go validation as search; each value is one expression, including embedded newlines.

Use `neq` (is not equal), `not_contains` (does not contain), or `not_regex` (does not match regex) to exclude matching scalar values. Numeric comparisons also have negative forms: `not_gt`, `not_gte`, `not_lt`, and `not_lte`. Negative operators use the same operand types and validation as their positive forms. Missing fields and incompatible source types still do not match; an explicit JSON null is the scalar text `"null"` for text operators.

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

## Command log sources

On Linux and macOS, click **+** in the tab bar to open a blank command tab. Enter a
shell command, choose an output mode, and click **Run**. Each tab has its own editor
and output; editing only changes the draft. **Run** stops and discards the previous
capture before starting the edited command in the same tab. **Run again** repeats
the last executed command and mode in that tab, leaving editor drafts intact.
Both preserve applied viewer settings, clear the previous row position, and follow
new output. New tabs start with default settings.

Stdin is permanently first and cannot be closed or repurposed. Source selection
sits below the tabs; file input remains disabled. Use each command tab’s **×** to
stop its process and discard its output. Closing selects the right neighbor, or
the left if there is none; closing a background tab leaves the active tab selected.
**Stop** keeps captured output available. Commands keep running when you switch
tabs or disconnect the browser. Wait for a pending Run to finish before reloading;
a reload between discard and creation can interrupt the replacement.

With a tab focused, use Left/Right, Home/End to select tabs and Delete to close a
command tab. The tab strip scrolls horizontally when needed; **+** stays accessible.

**Auto** uses the same log detection as stdin: JSON, logfmt, syslog, HTTP access, and timestamped logs stream
progressively, while unrecognized plain output appears when the command finishes
or is stopped. **Text** displays each sanitized, nonempty line immediately,
without interpreting JSON or timestamps. Command tabs without inherited settings
default to normalized `timestamp`, `severity`, and `message` columns. Switching tabs retains applied
search, filters, columns, follow preference, and the selected result position.

Commands run through `/bin/sh -c` with the binary's working directory and
environment. Quotes, pipelines, redirects, and environment expansion work as in
a POSIX shell. stdout and stderr share one captured pipe; child stdin is the null
device. Interactive aliases, terminal input, password prompts, and local PTYs are
not supported. Process output buffering still depends on the command itself.

For example, enter either of these commands in the UI:

```sh
ssh -o BatchMode=yes host 'journalctl -f -o json --no-pager'
ssh -tt -o BatchMode=yes host 'journalctl -f -o json --no-pager'
```

Use preconfigured keys or an SSH agent and an already trusted host key. The second
example forces a **remote** PTY when required, even without a local terminal.
Streamline executes exactly what you enter; it does not add SSH options.

**Stop** sends SIGTERM to the local process group and escalates to SIGKILL after
two seconds. Captured output is flushed before the final status is shown.
Nonzero exits expose the exit code and retain their output. Cleanup also runs
when a command source is removed or Streamline shuts down. This does not promise
termination of deliberately detached processes or remote jobs.

All sources are in memory. Browser reload discovers existing runs without
restarting them; restarting the binary discards them. There are no command CLI
flags, automatic restarts, or Windows command support. Named commands and viewer
settings can be saved explicitly as described below.

CrowdSec key/value output is detected automatically, for example
`time="2026-01-03T18:07:22+02:00" level=warning msg="blocked" module=db`.
It provides normalized timestamp, severity, and message columns while retaining
all source values as strings. Complete key/value lines such as `status=403` also
count as logs; use string filters for their fields. See the
[parser selection and extension guide](.memory/parser.md#parser-selection-and-extension).

### Syslog and HTTP access logs

Auto also recognizes RFC 5424 version 1, traditional RFC 3164, local syslog
with a hostname/application tag, and standard Common/Combined HTTP access logs.
Syslog exposes `hostname`, `app`, `procid`, and numeric priority/facility fields
when present. HTTP access exposes `client`, `method`, `target`, numeric `status`
and `bytes`, plus `referer` and `user_agent` for Combined logs.

For example, filter `app` equal to `sshd`, or filter `status` greater than or
equal to `500` and less than `600`. HTTP status does not create log severity.
Legacy syslog dates assume UTC and a year near source startup; old archives or
logs from another timezone may need a source with explicit timestamp context.
Custom access layouts stay as text, and malformed candidates retain their full
line with diagnostics in an otherwise parsed stream.

See [sample logs, saved configurations, and field reference](examples/README.md)
for runnable examples of all four formats and commands for local/SSH sources.

The source API is documented in [.memory/transport.md](.memory/transport.md).
Run `make build` before the command browser tests:

```sh
bun run --bun --filter @streamline/web test:e2e commands.spec.ts
```

These tests launch their own standalone binaries.


## Saved configurations

Click the **settings icon** at the right of the top bar to open **Saved configurations**.
**Save current as new** captures the active tab’s actual command/output mode and
applied columns, their displayed widths, date formats, field filters, and general search. Unrun command
text and unapplied editor drafts are excluded for tabs that have run. A prepared
tab that has not run saves its command draft and prepared settings. Stdin entries
have an empty command.

Give the entry a name and use the three textareas to edit its command, column JSON,
and filters/search JSON. **Save** writes the entry without changing the current tab.
**Edit** updates an entry; **Clone and edit** starts an independent unsaved copy.
**Delete** removes the saved entry from disk without changing the active tab.
Unsaved changes must be saved or discarded before leaving the editor.

**Load** replaces the active tab’s columns (including saved widths), filters, and search and fills the command
editor and output mode. It does not start, stop, or rename a running source. Click
**Run** to replace the capture in the same tab with those settings. **Run again**
uses the selected source’s original command/mode with its current applied settings.
Loading a command configuration from stdin opens a prepared command tab without
executing it; commandless configurations still apply to stdin. Blank command tabs
can also load configurations before their first Run.
Raw output retains prepared settings for reuse without filtering the raw text.

Settings live on the machine running the Go binary, in:

- Production: `~/.config/streamline/configs/`.
- Development: `.local/streamline/configs/` in the repository, ignored by Git.
- Override: `streamline -config-dir /path/to/settings` (files go in its `configs/` subdirectory).

Each entry is a self-contained UTF-8 JSON file. New filenames combine the sanitized
name and a random hex suffix, such as `service-errors-<hash>.json`. Editing an entry
keeps its filename stable; existing filenames remain supported. Copy files between configuration
folders to transfer entries; use **Refresh** or reopen settings to discover external
changes. A safe filename such as `errors.json` is supported; names shown in the UI
are independent of filenames and need not be unique. Malformed or unsupported files
are listed with errors and left unchanged so they can be repaired outside the app.
Files are limited to 64 KiB, including formatting, and writes replace files atomically.
New directories/files are private (`0700`/`0600`).

```json
{
  "version": 1,
  "name": "Service errors",
  "command": "journalctl -f -o json --no-pager",
  "mode": "auto",
  "columns": [
    { "path": "timestamp", "dateFormat": "iso", "width": 280 },
    { "path": "message", "dateFormat": "original", "width": 600 }
  ],
  "filters": {
    "filter": [{ "field": "PRIORITY", "op": "eq", "value": "3" }],
    "search": { "text": "timeout\nrefused", "mode": "plain", "operator": "or" }
  }
}
```

Column date formats are `original`, `iso`, `local`, `date`, and `time`.
Each column accepts an optional `width` in pixels (at least 144). Omit it for
automatic sizing. Saved widths apply when loading and remain adjustable in the table.
The Filters and search textarea edits the entire `filters` object shown above;
the existing sidebar filter importer continues to accept just a filter array.
Browser regex checks in settings are advisory because browser and Go syntax differ.
Save and Load require authoritative Go validation, with filter positions and search
line numbers in errors. Commands are stored as text and are not shell-validated.

Saved entries survive browser reloads and binary restarts. Logs, tabs, scroll positions,
panel sizes, follow state, and row heights remain session state. Commands are not
recorded automatically; there is no automatic command history or tab restoration.
