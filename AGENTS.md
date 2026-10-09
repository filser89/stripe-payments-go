# Go Stripe Payment Service — Agent Instructions

Read and apply [FEATURE-QUALITY-STANDARDS.md](FEATURE-QUALITY-STANDARDS.md) when specifying, planning, testing, implementing, or reviewing a feature. It defines the mandatory project-specific quality requirements.

## Application scope

Build one Go application with PostgreSQL and a local browser interface for one merchant using one Stripe sandbox account. Support one configured currency, one-time card payments with immediate capture through Stripe-hosted Checkout, payment status/history, and manually invoked reconciliation with bounded concurrent workers.

Keep the order representation sufficient to associate payments with purchases. Local execution and automated CI are in scope. Refunds, subscriptions, disputes, payouts, separate authorization/capture, multiple merchants, Stripe Connect, multiple currencies, foreign exchange, inventory, tax, shipping, a full storefront/admin interface, user registration, multi-tenant authorization, lending, wallets, accounting, real-money operation, and cloud deployment are outside v1. No separate scheduler, message broker, distributed microservices, or Kubernetes is required. External actions such as dashboard refunds are outside the supported lifecycle and must be documented as such.

## Stack

| Area | Choice |
| --- | --- |
| Language | Go 1.27.2 |
| HTTP | Standard-library `net/http` |
| Database | PostgreSQL 18.6; the same version locally and in integration tests |
| Database access | pgx v5 with pgxpool, plus sqlc generating typed Go code from parameterized SQL |
| Database migrations | Goose with SQL migrations |
| Payments | Official Stripe Go SDK (`stripe-go`) and Stripe-hosted Checkout in sandbox |
| Browser interface | Go `html/template`, plain HTML, and minimal JavaScript |
| Logging | Standard-library `log/slog` with structured JSON output |
| Configuration | Environment variables validated at startup; ignored local `.env` and a committed `.env.example` containing placeholders |
| Local environment | Docker Compose for the application, PostgreSQL, and a separate Stripe CLI service for webhook forwarding |
| Test runner and assertions | Go `testing` and `go test`, with Testify `assert` and `require` |
| Database integration tests | Testcontainers for Go running real PostgreSQL with the application's Goose migrations |
| Payment-rule tests | Fake Stripe dependency returning prepared result objects or errors; no real SDK or HTTP server required |
| Stripe integration tests | Focused tests using the real Stripe SDK against a local `httptest` server returning stubbed HTTP responses; the server runs within tests; no live Stripe calls or credentials |
| Webhook tests | Synthetic payloads signed using a test secret, including invalid-signature rejection |
| Race detection | `go test -race` |
| Formatting | `gofmt` |
| Linting and static analysis | golangci-lint v2, including `govet`, `staticcheck`, `errcheck`, `ineffassign`, and `unused` |
| Dependency vulnerability checks | `govulncheck` |
| Continuous integration | GitHub Actions, running the same checks as locally |
| Command shortcuts | Makefile |
| Version pinning | Exact dependency and tool versions recorded during repository setup; no floating `latest` versions |

## High-level folder structure

This is a placement guide, not a feature implementation plan. Create folders when needed. Choose files, types, functions, interfaces, and any further package split during feature design; this document does not prescribe them.

| Folder | Responsibility |
| --- | --- |
| `cmd/service/` | Application entry point, command selection, dependency construction, startup, and shutdown. Keep feature behavior in internal packages. |
| `internal/config/` | Environment configuration loading and validation. |
| `internal/web/` | HTTP routing, handlers, middleware, request/response handling, and browser presentation. |
| `internal/web/templates/` | HTML templates used by the browser interface. |
| `internal/payment/` | Payment-related business behavior and application operations, including the associated orders and recovery behavior. Determine its internal organization through feature specifications. |
| `internal/postgres/` | PostgreSQL persistence, transaction handling, and database integration. Keep generated sqlc output in a clearly separated subdirectory selected during setup. |
| `internal/stripeapi/` | Stripe SDK integration, webhook verification/decoding, and translation of Stripe-specific results. |
| `internal/testutil/` | Test helpers genuinely shared across packages. Ordinary application code must not depend on this package or its subpackages. |
| `internal/integration/` | Tests exercising the connected application journey across packages. |
| `db/migrations/` | Versioned Goose SQL migrations. |
| `db/queries/` | SQL source used by sqlc. |
| `docs/` | Application usage, architecture, configuration, recovery, and lifecycle documentation. |

Keep payment rules independently testable from HTTP and Stripe integration. Keep SQL and SDK details in their integration packages. Share business behavior across entry points instead of duplicating it. Establish exact interfaces and dependency wiring in feature design, introducing abstractions only where they serve a concrete need.

## Commit messages

- Use descriptive branch names without the `codex/` prefix.
- Use `type(scope): description`; scope is optional.
- Allowed types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `build`, `ci`.
- Write the description in imperative mood with a lowercase opening and no trailing period.
- Add a body explaining the reason and important consequences when the subject is insufficient.

Examples:

```text
feat(checkout): persist payment operation before calling Stripe
fix(webhooks): prevent duplicate history entries
test(reconciliation): cover cancellation with a full queue
```

## Go coding conventions

- Accept `gofmt` output as the formatting standard. Use the agreed static analyzers: `govet`, `staticcheck`, `errcheck`, `ineffassign`, and `unused` through golangci-lint.
- Use short, descriptive, lowercase package names. Use Go mixed-case identifiers and conventional initialisms such as `OrderID` and `HTTP`. Avoid repeating package names in exported identifiers.
- Export only what other packages need. Unexported declarations remain accessible throughout their package; a file is an organizational unit, not an access-control boundary.
- Group related types, functions, and helpers by responsibility. Split files for readability and packages for meaningful responsibilities. There is no mandatory one-type-per-file rule or arbitrary file/function line limit. Keep helpers near their callers; avoid catch-all utility packages for unrelated production behavior.
- Handle returned errors explicitly. Add useful context when propagating errors; preserve wrapped causes where callers need them and classify with `errors.Is` or `errors.As`, not message matching. Use early returns to keep successful paths readable. Ordinary input, payment, database, and network failures use errors rather than panic.
- Pass `context.Context` as the first argument to operations that need cancellation or deadlines, and propagate it through database and external calls. Keep request contexts out of long-lived structs.
- Construct dependencies explicitly and pass them to their consumers. Introduce small interfaces at actual points of use; an interface is not required for every concrete type.
- Give every owned goroutine a defined lifetime, cancellation path, and completion mechanism. Keep shared state synchronized and work bounded.
- Document exported contracts and non-obvious guarantees or trade-offs. Comments should explain behavior and reasons rather than narrate obvious statements.
- Regenerate generated code from its source inputs rather than editing generated output manually.

## Tests and file ownership

- Keep package tests beside the code in `_test.go` files. Keep package-local test helpers in `_test.go` files as well.
- Both same-package tests and external `_test` packages are valid. Prefer observable behavior; use internal access when it materially improves verification.
- Place fixtures in nearby `testdata/` directories. Keep production behavior independent of test fixtures and shared test helpers.
- Use `internal/testutil/` for helpers needed by multiple test packages.
- Test-owned files are `_test.go` files, contents of `testdata/` directories, and the `internal/testutil/` subtree. Keep test support within these conventions so tools can identify it automatically.

## Verification

Test payment rules using a fake Stripe dependency that returns prepared result objects or errors; these tests do not require the real SDK or an HTTP server. Verify persistence and concurrency against real PostgreSQL with the application's migrations.

Use a focused set of Stripe integration tests with the real SDK pointed at a local `httptest` server returning stubbed HTTP responses. Verify outgoing request parameters and idempotency keys, response/error handling, and actual timeout/cancellation behavior. The server runs within tests, not as a separately managed service. Test webhooks using synthetic payloads signed with a test secret, including invalid-signature rejection. Automated correctness tests must not require real Stripe credentials, Stripe availability, or an actual outage.

Verify the connected payment journey and the applicable acceptance criteria in the current feature's approved artifacts, including repeated requests, duplicate/out-of-order notifications, ambiguous outcomes, interruption and restart, and reconciliation. Deliberately coordinate overlapping operations in concurrency tests instead of relying on timing sleeps.

Run Go race detection for workers and shared-state scenarios. Verify database invariants separately through independent connections and concurrent transactions; an in-memory mutex or a passing race check does not establish database correctness across processes.

Local and CI verification must use the same documented checks: formatting, static analysis, vulnerability checks, automated tests including integration tests, and race detection. Establish exact Makefile targets during setup. Report checks actually run, their results, and material verification gaps; distinguish unverified work from confirmed results.

## Requirements that apply across features

- Use integer minor units with explicit currency. Enforce critical uniqueness and consistency through PostgreSQL constraints and transactions. Keep state changes and their corresponding append-only history transactionally consistent.
- Preserve durable operation identity across repeated requests, external-call ambiguity, and process restarts. Treat unresolved outcomes explicitly. Payment confirmation requires verified, correctly correlated evidence; a browser return or checkout creation is not proof of payment.
- Authenticate application operations and verify webhook signatures. Validate inputs, configuration, and request sizes. Keep privileged credentials out of browser code. Keep secrets, card data, and unnecessary personal data out of version control, logs, and stored history.
- Bound Stripe calls and retries. Reconciliation uses a fixed goroutine pool, channels, bounded queued work, and cancellation/shutdown budgets. Preserve independent successful results and leave unfinished operations recoverable.
- Provide sanitized structured logs with correlation identifiers, health/readiness checks, and useful reconciliation results. Keep local setup, API usage, configuration, architecture, and recovery documentation usable and current.
