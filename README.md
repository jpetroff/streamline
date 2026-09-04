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

Open [localhost:5173](http://localhost:5173). The shell initially shows an empty
log table because runtime stdin ingestion is not connected yet. The health
endpoint is [localhost:5173/api/v1/health](http://localhost:5173/api/v1/health).
Press Ctrl+C to stop both processes.

```sh
make check
make build
./bin/streamline
```

The standalone binary serves the UI at [localhost:8080](http://localhost:8080).
Use `make run PORT=8081` or `./bin/streamline -port 8081` to choose another port.

Setup checks the global Go and Bun installations and installs dependencies
from `bun.lock`. Bun runs the frontend tooling. The built binary runs on its own.

- [Architecture and extension diagrams](.memory/architecture.md)
- [Frontend visual output and virtualization](.memory/frontend.md)
- [Development, commands, and debugging](.memory/development.md)

The frontend shell, binary–frontend transport, parser, in-memory query service,
health endpoint, and build tooling are implemented.
