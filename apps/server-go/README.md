# Technews Go API

This directory is an isolated Go module for the API migration. It does not share a Go module or developer commands with the existing Node server.

## Safety defaults

Development configuration is deliberately isolated:

- `SERVER_ADDR` defaults to `127.0.0.1:4401`.
- `DATABASE_PATH` defaults to `data/technews.db` beneath the worktree root supplied by the composition root.
- Ports `3001` and `3002` are always rejected. Port `4001` and `/Users/ozan/Projects/technews/apps/server/data/technews.db` are rejected unless `APP_ENV=production` is explicitly set.
- Development and tests must never use the production checkout or database. Tests should use temporary databases.

The production override is a cutover guard, not a development convenience. Do not set `APP_ENV=production` without explicit cutover approval.

## HTTP compatibility

`OPTIONS` responses match Express in status (`204`), CORS headers and `Vary`, and empty-body semantics. They deliberately omit Express's wire-level `Content-Length: 0`: [RFC 9110 section 8.6](https://www.rfc-editor.org/rfc/rfc9110#section-8.6) forbids servers from sending `Content-Length` on a `204` response, and Go's `net/http` strips it accordingly.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ENV` | `development` | Typed runtime mode: `development` or `production` |
| `SERVER_ADDR` | `127.0.0.1:4401` | HTTP listen address |
| `DATABASE_PATH` | `<worktree>/data/technews.db` | SQLite database path |

Configuration is represented by `internal/config.Config` and validated before runtime resources are opened.

## Developer commands

Run commands from `apps/server-go`:

```sh
make test       # all module tests
make test-race  # all tests with the race detector
make vet        # static checks
make check      # tests and static checks
make fmt        # format Go sources
```

## Architecture

The service is a single binary with a `cmd` plus `internal` layout:

- `cmd/api` is the executable entry point and imports only configuration and the application composition root.
- `internal/app` wires modules and infrastructure, including gathering capability-owned migration descriptors in execution order.
- Cohesive capability packages such as `content`, `editorial`, `newsletter`, `media`, and `settings` own their domain, repository, service, HTTP handlers, relative route mounting, and migration SQL.
- `internal/database/migrate` owns only the migration ledger and runner; it does not own capability schema SQL.
- Narrow infrastructure packages live under `internal` and are named for their purpose.
- Interfaces are declared by the consuming package at the point of use. Constructors return concrete types unless a consumer needs an interface.

Capability packages do not import one another. Cross-capability behavior is injected through narrow interfaces owned by the consumer. Domain and service code remain independent of HTTP and concrete SQLite types. Do not create generic `utils`, `common`, or global service-locator packages.
