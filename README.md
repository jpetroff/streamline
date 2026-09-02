# Streamline

Scaffolding for a local log viewer: a Go binary and an empty Svelte web interface.

## Get started

Requires global Go 1.27.1+ from Linuxbrew, global Bun 1.4.0+, and Make.
See the [development setup](.memory/development.md#setup) for installation and
custom Homebrew paths.

```sh
make setup
make dev
```

Open [localhost:5173](http://localhost:5173). The page is intentionally empty.
The health endpoint is [localhost:5173/api/v1/health](http://localhost:5173/api/v1/health).
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
- [Development, commands, and debugging](.memory/development.md)

Only the frontend shell, health endpoint, and build tooling are implemented.
