# Architecture: Go Stripe Payment Service

**Last updated**: 2026-10-09 — by /kaba:architecture (full scan) — commit 2a451c5
**Integration branch**: master (resolved from Git `init.defaultBranch`; the local integration branch and CI target are `main`)

## Overview

This Go application uses standard-library HTTP, HTML templates and structured JSON logging with PostgreSQL connectivity through pgx. The `cmd/service` executable selects `serve`, `migrate` or `probe`; serving exposes exact public health/readiness endpoints and an HTTP Basic-protected static landing page. Requests pass through logging/recovery and shutdown admission before probe dispatch or protected routing, and readiness executes a generated SQL query. There are no business tables, payment operations, Stripe SDK integration or webhook handlers in the application code.

## Directory Structure

```text
stripe-payments-go/
├── cmd/service/                 # Executable, command selection and dependency construction
├── internal/
│   ├── config/                  # Environment capture and common/serving validation
│   ├── web/                     # HTTP composition, authentication, presentation and lifecycle
│   │   └── templates/           # Embedded browser HTML
│   ├── postgres/                # Pool startup and Goose migration execution
│   │   └── queries/             # Generated sqlc package consumed by application wiring
│   └── integration/             # Integration tests and nearby fixtures
├── db/
│   ├── migrations/              # Production migration source; currently empty
│   ├── queries/                 # Named SQL query inputs for sqlc
│   └── schema.sql               # sqlc schema input; currently no business tables
├── scripts/                     # Tool setup, ordered local startup and verification
│   └── testdata/                # Workflow test support
├── .github/workflows/           # GitHub Actions verification
├── .kaba/                       # Feature workflow configuration and architecture reference
│   └── hooks/                   # Pre-commit session-lock shim
├── features/                    # Feature specifications, plans and verification artifacts
├── compose.yaml                 # PostgreSQL, migration, application and optional Stripe CLI services
├── Dockerfile                   # Multi-stage build and non-root runtime image
├── Makefile                     # Local build, startup, migration and verification entry points
├── sqlc.yaml                    # SQL input and generated-package configuration
└── .golangci.yml                # Static analysis configuration
```

## Layers

### Executable and composition

- **Location**: `cmd/service/`
- **Naming**: Package `main`; `main` delegates to `run` with a context, command arguments, environment reader and output writer.
- **Base class / interface**: None; explicit construction of concrete configuration, pool, generated queries and web server.
- **Responsibilities**: Select commands, validate configuration, construct JSON logging, establish signal cancellation and own startup resources. `serve` validates its account before database/listener acquisition; `migrate` invokes the shared migration runner; `probe` makes a bounded HTTP readiness request without Basic credentials.
- **Canonical example**: `cmd/service/main.go`

### Configuration

- **Location**: `internal/config/`
- **Naming**: Package `config`; `Config`, `Load` and `Config.ValidateServing`.
- **Base class / interface**: None; `Load` accepts an environment-reader function and returns `(Config, error)`.
- **Responsibilities**: Capture environment values, apply common defaults and validate database/address/logging/duration settings. Keep serving-account validation separate so migration and probe commands do not require credentials. Errors identify settings without including supplied values.
- **Canonical example**: `internal/config/config.go`

### HTTP and browser presentation

- **Location**: `internal/web/`, including `internal/web/templates/`
- **Naming**: Package `web`; exported `Server` and `New`, with private handler constructors and response helpers grouped by responsibility.
- **Base class / interface**: Standard-library `http.Handler` and `http.HandlerFunc`; readiness is injected as `func(context.Context) error`.
- **Responsibilities**: Compose probes and protected handlers, enforce authentication and bounded transport settings, provide request logging/recovery and coordinate shutdown. `New` uses the embedded landing handler when its `extra` handler is nil; supplied handlers share the same protected boundary. The landing handler accepts exact `/` GET/HEAD requests with an empty actual body, inspects at most one byte, and renders a parsed embedded `html/template` into a request-local buffer before committing headers.
- **Canonical example**: `internal/web/server.go`

### PostgreSQL integration

- **Location**: `internal/postgres/`, with SQL source in `db/`
- **Naming**: Package `postgres`; context-first `Open` and `Migrate` operations return resources/errors explicitly.
- **Base class / interface**: `pgxpool.Pool` for application connectivity; `database/sql` using the pgx driver and a Goose provider for migrations. Migration source is supplied as `fs.FS`.
- **Responsibilities**: Verify connectivity within the caller's startup budget and run `up`/`down` migrations with sanitized errors. Production migration files live under `db/migrations/`; test fixtures use the same runner with their own source. There is no application repository or business transaction layer yet.
- **Canonical example**: `internal/postgres/postgres.go`

### Generated SQL access

- **Location**: `internal/postgres/queries/`
- **Naming**: Generated package `queries`; named SQL produces methods on `Queries` such as `CheckReady`.
- **Base class / interface**: Generated `DBTX` interface implemented by pgx connections/pools/transactions; `WithTx` binds a `Queries` instance to a transaction.
- **Responsibilities**: Execute typed, context-aware queries generated from `db/queries/` and `db/schema.sql` through `sqlc.yaml`. Application wiring constructs `queries.New(pool)` and injects `CheckReady` through the web readiness callback. Generated files are regenerated from SQL inputs.
- **Canonical example**: `internal/postgres/queries/health.sql.go`

## Key Patterns

### Startup configuration and credential lifetime

- **What it does**: Establishes one captured configuration/account for the serving process while keeping local commands independent of that account.
- **How it works**: `config.Load` reads both Basic settings without normalization alongside common settings. `run` calls `ValidateServing` only for `serve`, before acquiring resources. The web authenticator also validates construction and captures immutable, separate SHA-256 username/password digests; it does not reread the environment or maintain sessions.
- **Files**: `internal/config/config.go`, `internal/config/authentication.go`, `cmd/service/main.go`, `internal/web/authentication.go`.

### Public probes and protected request dispatch

- **What it does**: Keeps exact probes public and all other handler execution behind per-request HTTP Basic verification.
- **How it works**: Logging/recovery wraps shutdown admission, then exact `/healthz` and `/readyz` dispatch accepts GET/HEAD and rejects other methods. Every other path passes through authentication before downstream routing, body inspection or method handling. Authentication requires one bounded Authorization field, checks Basic syntax and standard Base64 decoding, and compares both fixed-size credential digests before combining results. Rejections return a generic 401 with a Basic challenge; successful requests delegate once without changing request data. Browsers own credential prompting/reuse; the application provides no session or logout mechanism.
- **Files**: `internal/web/server.go`, `internal/web/authentication.go`, `internal/web/landing.go`.

### Bounded HTTP, database and shutdown lifetimes

- **What it does**: Propagates cancellation and bounds startup, transport, readiness, request draining and resource cleanup.
- **How it works**: Configuration supplies explicit budgets and validates the Compose stop period against shutdown grace, cleanup and a margin. Entry-point startup and readiness derive timed contexts. The HTTP server applies header/read/write/idle deadlines and gives requests a server-owned cancellable base context. Shutdown closes admission under a mutex, drains HTTP work within the grace budget, cancels request contexts, closes overdue connections and waits for active handlers before resource cleanup. Cleanup timeout or serving failure returns a sanitized error to the process caller.
- **Files**: `internal/config/config.go`, `cmd/service/main.go`, `internal/postgres/postgres.go`, `internal/web/server.go`, `compose.yaml`.

### Sanitized correlated responses and logs

- **What it does**: Reports request outcomes without exposing raw request inputs, credentials or dependency diagnostics.
- **How it works**: Entry-point construction supplies `slog` JSON logging. The outer HTTP wrapper generates `X-Request-ID`, records status/duration and recovers panics using fixed error categories. Readiness/render/write failures include correlation identifiers and sanitized categories. Application-owned plain responses use no-store headers, representation length and explicit HEAD-body suppression; landing HTML follows the same no-store/HEAD behavior. Configuration and database errors avoid supplied values.
- **Files**: `cmd/service/main.go`, `internal/config/config.go`, `internal/config/authentication.go`, `internal/postgres/postgres.go`, `internal/web/server.go`, `internal/web/authentication.go`, `internal/web/landing.go`.

### SQL source ownership and migration execution

- **What it does**: Keeps SQL generation and schema application separate, with one migration implementation for application and integration tests.
- **How it works**: sqlc reads named queries and the schema input, emitting the typed pgx package. Verification regenerates queries and checks for drift. `service migrate` runs Goose against filesystem migrations with a startup deadline; an empty migration source is accepted for `up`. Integration tests use real PostgreSQL 18.6 containers and isolated fixture sources through `postgres.Migrate`.
- **Files**: `db/queries/health.sql`, `db/schema.sql`, `sqlc.yaml`, `internal/postgres/postgres.go`, `internal/postgres/queries/db.go`, `scripts/verify.sh`, `internal/integration/foundation_test.go`.

### Ordered local startup and shared verification

- **What it does**: Coordinates database/migration/application startup and uses the same verification entry point locally and in CI.
- **How it works**: Compose publishes local ports on loopback and injects Basic credentials only into `app`. `scripts/up.sh` builds the image, waits for PostgreSQL, stops the app, recreates/runs the migration service and starts the app only after successful migration. Runtime health checks invoke `service probe`. The optional Stripe CLI profile forwards to a configured path that has no implemented webhook handler. `make setup` installs pinned tools; local verification and GitHub Actions invoke `make verify` for formatting, SQL generation consistency, workflow checks, static analysis, vulnerability checks, build, uncached tests and race detection.
- **Files**: `compose.yaml`, `Dockerfile`, `.env.example`, `.gitignore`, `.dockerignore`, `Makefile`, `scripts/setup.sh`, `scripts/up.sh`, `scripts/verify.sh`, `.github/workflows/verify.yml`.

## Dependencies

| Concern | Library/tool | Notes |
|---|---|---|
| PostgreSQL connectivity | `github.com/jackc/pgx/v5` v5.11.0 | Application pool and generated query types; stdlib driver for migrations. |
| SQL migrations | `github.com/pressly/goose/v3` v3.28.0 | Shared provider-based migration execution over supplied filesystem sources. |
| Typed SQL generation | sqlc v1.31.1 | Pinned in `scripts/setup.sh`; configured by `sqlc.yaml`; generated package under `internal/postgres/queries/`. |
| Real database test environment | `github.com/testcontainers/testcontainers-go` v0.44.0 | Integration tests manage real PostgreSQL 18.6 containers. |
