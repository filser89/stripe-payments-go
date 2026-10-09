# Architecture: Go Stripe Payment Service

**Source commit**: 542f06d182d4512e79febfa2afd2a3895a413058
**Integration branch**: main

## Overview

This Go application serves authenticated order creation, hosted Checkout continuation, and local payment status/history through standard-library HTTP. `cmd/service` selects `serve`, `migrate` or `probe` and constructs the HTTP → payment → PostgreSQL/Stripe boundaries explicitly. Payment policy operates on plain domain values; short PostgreSQL transactions own durable request bindings, immutable operation snapshots, fenced dispatch and atomic append-only history, while the pinned Stripe SDK adapter translates one wire attempt per invocation. Exact public probes, protected static landing HTML, correlated sanitized logs and bounded shutdown share the HTTP composition. Checkout creation and browser returns do not confirm payment; webhook processing, browser payment controls and reconciliation workers have no implementation.

## Directory Structure

```text
stripe-payments-go/
├── cmd/service/                 # Executable, command selection and dependency construction
├── internal/
│   ├── config/                  # Environment capture and common/serving validation
│   ├── web/                     # HTTP composition, authentication, presentation and lifecycle
│   │   └── templates/           # Embedded browser HTML
│   ├── payment/                 # Domain contracts, checkout policy and synchronous recovery
│   ├── stripeapi/               # Pinned SDK adapter and controlled single-attempt transport
│   ├── postgres/                # Pool, migrations and guarded payment transactions
│   │   └── queries/             # Generated sqlc package consumed by application wiring
│   ├── testutil/                # Shared test-only faults, barriers and dependency fixtures
│   └── integration/             # Connected journey tests and nearby fixtures
├── db/
│   ├── migrations/              # Versioned production payment schema and guards
│   ├── queries/                 # Named SQL query inputs for sqlc
│   └── schema.sql               # sqlc schema input for the payment relations
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
- **Base class / interface**: None; explicit construction of configuration, pgx pool, generated queries, repository, gateway, payment service and HTTP handlers.
- **Responsibilities**: Select commands, validate captured settings, construct JSON logging, establish signal cancellation and own startup resources. `serve` validates Basic and checkout settings before database/listener acquisition and injects repository/gateway into payment operations and operations into the protected handler. Its readiness callback executes `CheckPaymentReady` against the business schema without a Stripe call; unavailable schema leaves health usable and readiness failed. `migrate` runs shared Goose migrations; `probe` makes a bounded HTTP readiness request without Basic credentials.
- **Canonical example**: `cmd/service/main.go`

### Configuration

- **Location**: `internal/config/`
- **Naming**: Package `config`; `Config`, `Load`, `ValidateBasic`, `ValidateServing` and typed `CheckoutSettings`.
- **Base class / interface**: None; `Load` accepts an environment-reader function and returns `(Config, error)`.
- **Responsibilities**: Capture common, Basic and checkout settings once. Validate common connection/address/logging/lifetime settings during loading, validate only the captured Basic account for isolated authentication construction, and validate the full sandbox key, local return origin, currency, amount limits and interdependent request/call/retry/transport budgets for serving. Checkout parse failures are retained for serving validation so migrate/probe remain independent of serving credentials and checkout configuration. Errors identify settings without supplied values.
- **Canonical example**: `internal/config/config.go`

### HTTP and browser presentation

- **Location**: `internal/web/`, including `internal/web/templates/`
- **Naming**: Package `web`; exported `Server`, `New` and `NewCheckoutHandler`, with private handlers and parsing/response helpers grouped by responsibility.
- **Base class / interface**: Standard-library `http.Handler` and `http.HandlerFunc`; readiness is injected as `func(context.Context) error`, and the checkout handler consumes `payment.Operations`.
- **Responsibilities**: Compose exact public probes and Basic-protected routing, validate actual bounded input streams, map classified domain outcomes to allowlisted JSON, provide correlated semantic logs and coordinate HTTP/shutdown lifetimes. Checkout routing delegates create/continue/read/history to payment operations and root requests to the embedded static landing handler. The landing handler accepts exact `/` GET/HEAD requests with an empty actual body, inspects at most one byte, and renders a parsed embedded `html/template` into a request-local buffer before committing headers.
- **Canonical example**: `internal/web/checkout.go`

### PostgreSQL integration

- **Location**: `internal/postgres/`, with schema/query source in `db/`
- **Naming**: Package `postgres`; context-first `Open`/`Migrate` operations and a private `paymentRepository` constructed through `NewPaymentRepository`.
- **Base class / interface**: `payment.Repository` over `pgxpool.Pool` and transaction-bound generated queries; `database/sql` using the pgx driver and a Goose provider for migrations over supplied `fs.FS`.
- **Responsibilities**: Bound connectivity/migration startup and translate persistence failures into classified domain errors. Payment transactions lock an order before its operations in ID order, enforce binding/current/version/owner/prior-operation guards, commit acceptance or observations together with history, and release dispatch claims without altering business values. Committed reads expose a bound historical operation or the current operation; history reads paginate per-order sequences. No network call runs inside a transaction.
- **Canonical example**: `internal/postgres/payment_transactions.go`

### Generated SQL access

- **Location**: `internal/postgres/queries/`
- **Naming**: Generated package `queries`; named SQL produces methods on `Queries`, including payment reads, locks, writes and readiness.
- **Base class / interface**: Generated `DBTX` interface implemented by pgx connections/pools/transactions; `WithTx` binds a `Queries` instance to a transaction.
- **Responsibilities**: Execute typed, parameterized, context-aware SQL generated from `db/queries/` and `db/schema.sql` through `sqlc.yaml`. Repository operations construct queries against the pool or active transaction and translate selected database projections to domain values. Serving wiring injects `CheckPaymentReady`; basic `CheckReady` supports isolated foundation databases. Generated files are regenerated from SQL inputs.
- **Canonical example**: `internal/postgres/queries/payment.sql.go`

### Payment policy and application operations

- **Location**: `internal/payment/`
- **Naming**: Package `payment`; plain domain values and `Service` constructed by `New`, exposing context-first `Create`, `Continue`, `Get` and `History`.
- **Base class / interface**: `Operations` consumed by HTTP; `Repository` and `Gateway` consumed by the service. `Options` supplies captured policy, logging, clock and cancellable waits.
- **Responsibilities**: Own random identities, authoritative purchase validation, immutable request replay, checkout lifecycle and evidence correlation, derived safe action/investigation flags and bounded synchronous dispatch/recovery. Reads use committed local data and never call Stripe. Classify failures with `payment.Error` codes, optional known IDs and preserved causes; keep SQL, SDK and HTTP-server details outside this package.
- **Canonical example**: `internal/payment/service.go`

### Stripe integration

- **Location**: `internal/stripeapi/`
- **Naming**: Package `stripeapi`; private `gateway` constructed by `New`, with context-first `Create` and `Retrieve`.
- **Base class / interface**: Implements `payment.Gateway` with a per-client official `stripe.Client`; construction-only `Options` permit local backend/client injection.
- **Responsibilities**: Encode fixed hosted card payment parameters from the durable snapshot, use its Stripe idempotency key, and translate complete remote fields, request IDs and sanitized error/retry facts into `SessionEvidence`. Disable SDK retries/logging and redirect following; control the production transport for one verified-TLS HTTP/1 wire attempt. Return evidence to payment policy without deciding lifecycle eligibility or payment confirmation.
- **Canonical example**: `internal/stripeapi/checkout.go`

## Key Patterns

### Startup configuration and credential lifetime

- **What it does**: Establishes one captured serving configuration while keeping local commands independent of serving-only settings.
- **How it works**: `config.Load` captures Basic credentials without normalization and retains typed checkout settings/parse failures alongside common settings. `run` calls full `ValidateServing` only for `serve`, before acquiring resources. Authentication construction uses `ValidateBasic` and immutable separate SHA-256 username/password digests. Dependency construction passes captured checkout limits, origin and budgets to service/handler and a sandbox secret to its per-client Stripe backend; none reread the environment or maintain application sessions.
- **Files**: `internal/config/config.go`, `internal/config/authentication.go`, `internal/config/checkout.go`, `cmd/service/main.go`, `internal/web/authentication.go`, `internal/stripeapi/checkout.go`.

### Public probes and protected request dispatch

- **What it does**: Keeps exact probes public and all other handler execution behind per-request HTTP Basic verification.
- **How it works**: Logging/recovery wraps shutdown admission, then exact `/healthz` and `/readyz` dispatch accepts GET/HEAD and rejects other methods. Every other path passes through authentication before downstream routing, body inspection or method handling. Authentication requires one bounded Authorization field, checks Basic syntax and standard Base64 decoding, and compares both fixed-size credential digests before combining results. Rejections return a generic 401 with a Basic challenge; successful requests delegate once without changing request data. Browsers own credential prompting/reuse; the application provides no session or logout mechanism.
- **Files**: `internal/web/server.go`, `internal/web/authentication.go`, `internal/web/landing.go`.

### Bounded HTTP, database and shutdown lifetimes

- **What it does**: Propagates cancellation and bounds startup, transport, readiness, request draining and resource cleanup.
- **How it works**: Configuration supplies explicit budgets and validates the Compose stop period against shutdown grace, cleanup and a margin. Entry-point startup and readiness derive timed contexts. The HTTP server applies header/read/write/idle deadlines and gives requests a server-owned cancellable base context. Shutdown closes admission under a mutex, drains HTTP work within the grace budget, cancels request contexts, closes overdue connections and waits for active handlers before resource cleanup. Cleanup timeout or serving failure returns a sanitized error to the process caller.
- **Files**: `internal/config/config.go`, `cmd/service/main.go`, `internal/postgres/postgres.go`, `internal/web/server.go`, `compose.yaml`.

### Sanitized correlated responses and logs

- **What it does**: Reports semantic request/payment outcomes without exposing raw inputs, credentials, snapshots or dependency diagnostics.
- **How it works**: Entry-point construction supplies `slog` JSON logging. The outer HTTP wrapper generates `X-Request-ID`, carries it in the payment context, records status/duration and recovers panics using fixed categories. HTTP and payment outcome logs include action, outcome, known order/operation IDs, applicable states and classified failures. Authentication/parser rejection logs do not infer resource identities from untrusted input. Domain errors preserve causes internally while outward errors use fixed messages and allowlisted IDs; JSON/plain/HTML responses use no-store, representation length and explicit HEAD suppression. Configuration/database/SDK errors and SDK logging avoid raw diagnostics.
- **Files**: `cmd/service/main.go`, `internal/payment/service.go`, `internal/payment/dispatch.go`, `internal/stripeapi/errors.go`, `internal/stripeapi/checkout.go`, `internal/postgres/payment.go`, `internal/web/server.go`, `internal/web/authentication.go`, `internal/web/checkout.go`, `internal/web/landing.go`.

### SQL source ownership and migration execution

- **What it does**: Keeps SQL generation and schema application separate, with one migration implementation for application and integration tests.
- **How it works**: sqlc reads named queries and the schema input, emitting the typed pgx package. Verification regenerates queries and checks for drift. `service migrate` runs Goose against filesystem migrations with a startup deadline; an empty migration source is accepted for `up`. Integration tests use real PostgreSQL 18.6 containers and isolated fixture sources through `postgres.Migrate`.
- **Files**: `db/queries/health.sql`, `db/schema.sql`, `sqlc.yaml`, `internal/postgres/postgres.go`, `internal/postgres/queries/db.go`, `scripts/verify.sh`, `internal/integration/foundation_test.go`.

### Ordered local startup and shared verification

- **What it does**: Coordinates database/migration/application startup and uses the same verification entry point locally and in CI.
- **How it works**: Compose publishes local ports on loopback and injects Basic credentials and checkout settings only into `app`. `scripts/up.sh` builds the image, waits for PostgreSQL, stops the app, recreates/runs the migration service and starts the app only after successful migration. Runtime health checks invoke `service probe`. The optional Stripe CLI profile forwards to a configured path that has no implemented webhook handler. `make setup` installs pinned tools; local verification and GitHub Actions invoke `make verify` for formatting, SQL generation consistency, workflow checks, static analysis, vulnerability checks, build, uncached tests and race detection.
- **Files**: `compose.yaml`, `Dockerfile`, `.env.example`, `.gitignore`, `.dockerignore`, `Makefile`, `scripts/setup.sh`, `scripts/up.sh`, `scripts/verify.sh`, `.github/workflows/verify.yml`.

### Durable identities and immutable external intent

- **What it does**: Preserves one logical mutation across replay, concurrent requests, ambiguous responses and process restarts.
- **How it works**: A globally unique permanent browser request binding records method, logical target, accepted purchase and resulting order/operation. Replay compares stored binding before current configurable amount limits, and returns the bound operation even when it is historical. Independent random order/operation/Stripe identities and the complete version-pinned creation snapshot commit before external mutation. First dispatch finalizes immutable first-dispatch time and requested expiry; later dispatch times remain separate mutable bookkeeping. Replays use the same key/parameters; unsupported snapshot versions cannot silently change the wire contract.
- **Files**: `internal/payment/contracts.go`, `internal/payment/service.go`, `internal/payment/dispatch.go`, `internal/postgres/payment.go`, `db/migrations/00001_payment.sql`, `db/queries/payment.sql`.

### Guarded transactions, fenced dispatch and atomic history

- **What it does**: Enforces cross-process uniqueness, safe mutation ownership and reconstructable business state without holding database transactions across Stripe calls.
- **How it works**: Constraints enforce unique request/Stripe/session/payment IDs, same-order associations and one active operation; triggers preserve purchase/snapshot identity, append-only bindings/history and paid monotonicity. Transactions lock order then operations in ID order and recheck paid/current/version/all-prior safety. Each possible creation send first commits an unresolved marker, dispatch timing, fresh owner token, incremented version, fixed 12-second database-clock lease and history. Results require matching current/version/owner fences and commit business values/history atomically. History compares the complete relevant business outcome, including changed failure/evidence/investigation facts when the state enum is unchanged; duplicate observations and coordination-only releases add no history. Expired claims can be taken over with a new fence, and stale results cannot overwrite the winner.
- **Files**: `internal/postgres/payment.go`, `internal/postgres/payment_transactions.go`, `db/migrations/00001_payment.sql`, `db/queries/payment.sql`, `internal/payment/dispatch.go`.

### Correlated evidence and conservative recovery

- **What it does**: Separates unpaid checkout lifecycle, confirmed rejection and unresolved integration outcomes while preventing unsafe replacement.
- **How it works**: Payment policy validates session/reference/metadata/object IDs, amount, currency, mode and sandbox evidence before applying open, complete-unpaid or expired observations. Creation/continuation cannot confirm paid; a retrieved paid session requires confirmation processing. Known sessions use retrieval, while missing-session creation replay requires the immutable first-dispatch age to be strictly below 23 hours. Replacement requires an unpaid order and safe prior operations, with verified expired/unpaid evidence or rejection without prior ambiguity. Mismatch, incompatible snapshots, aged uncertainty and indeterminate failures derive investigation/action flags; reads remain local and read-only, and cancellation/result-persistence failures preserve recoverability.
- **Files**: `internal/payment/service.go`, `internal/payment/dispatch.go`, `internal/postgres/payment_transactions.go`, `internal/stripeapi/checkout.go`, `internal/stripeapi/errors.go`.

### Admission budgets and one-attempt external transport

- **What it does**: Bounds the complete protected API request and makes application-owned retries account for actual external sends.
- **How it works**: Checkout admission starts the request deadline before body reads and passes the remaining external-work deadline into payment policy, preserving earlier parent deadlines. `ResponseController` bounds actual reads/writes; cancellation interrupts I/O and its callback is joined before handler completion. One request-local budget shares attempts/elapsed time across create/retrieve and cancellable backoff, caps each call, honors retry headers and reserves one second for final persistence/response. The Stripe client disables SDK retries and redirects; standard transports are cloned with fresh HTTP/1 connections, disabled keepalive/HTTP2, verified production TLS, and non-replayable request bodies. Retries reacquire and commit dispatch markers before each possible POST, retaining the snapshot/key and rechecking safe age at the physical send boundary. No background retry or lease-heartbeat goroutine exists.
- **Files**: `internal/web/checkout.go`, `internal/payment/service.go`, `internal/payment/dispatch.go`, `internal/stripeapi/checkout.go`, `internal/stripeapi/errors.go`, `internal/config/checkout.go`.

### Strict protected API contracts

- **What it does**: Keeps authentication, accepted inputs and response data explicit at the HTTP boundary.
- **How it works**: After Basic verification, exact route/method/canonical-ID dispatch performs no automatic redirects. POST reads at most 4097 bytes to enforce the 4096-byte limit and accepts one UTF-8 object with declared fields exactly once and EOF; content type/encoding, integer tokens and allowed query fields are checked before operations. GET/HEAD detect any actual body with a bounded one-byte read. Responses encode an explicit allowlist with UTC timestamps and null optional values; classified operation errors map to fixed status codes, and accepted pending work exposes local Location/Retry-After without inventing a new operation.
- **Files**: `internal/web/server.go`, `internal/web/authentication.go`, `internal/web/checkout.go`, `internal/payment/service.go`.

## Dependencies

| Concern | Library/tool | Notes |
|---|---|---|
| PostgreSQL connectivity | `github.com/jackc/pgx/v5` v5.11.0 | Application pool and generated query types; stdlib driver for migrations. |
| SQL migrations | `github.com/pressly/goose/v3` v3.28.0 | Shared provider-based migration execution over supplied filesystem sources. |
| Typed SQL generation | sqlc v1.31.1 | Pinned in `scripts/setup.sh`; configured by `sqlc.yaml`; generated package under `internal/postgres/queries/`. |
| Real database test environment | `github.com/testcontainers/testcontainers-go` v0.44.0 | Integration tests manage real PostgreSQL 18.6 containers. |
| Stripe Checkout integration | `github.com/stripe/stripe-go/v87` v87.0.0 | Per-client backend; API `2026-09-30.endive`; immutable hosted-payment encoding and controlled one-attempt HTTP transport. |
