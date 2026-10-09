# Stripe Payments Go

Go HTTP service foundation with PostgreSQL, typed SQL queries, transactional migrations, structured logs, and bounded shutdown.

## Prerequisites

- Go 1.27.2 (automatic Go toolchain download must be enabled).
- Docker with a running daemon and Docker Compose v2 or later.
- Make, a C compiler for the Go race detector, and network access to download pinned tools and container images.

## Local setup

```sh
cp .env.example .env
# Set your local database password in .env.
make setup
make generate
make build
make up
curl --fail http://localhost:8080/healthz
curl --fail http://localhost:8080/readyz
make verify
make down
```

`make up` builds the image, waits for PostgreSQL, stops any running application, runs migrations to completion, and gives application readiness up to 60 seconds. It prints diagnostic application/migration logs and exits nonzero on failure. `make down` preserves the named database volume. Change `APP_PORT` or `DB_PORT` in `.env` if a local port is occupied. Published ports bind only to loopback.

The foundation requires no Stripe credentials. The optional `stripe` Compose profile contains a pinned CLI image; forwarding becomes useful when a webhook endpoint exists. `STRIPE_API_KEY` belongs only in the ignored `.env` file.

| Command | Behavior |
| --- | --- |
| `make setup` | Check prerequisites and install pinned local tools. |
| `make generate` | Generate typed pgx code with sqlc. |
| `make build` | Build `bin/service`. |
| `make up` / `make down` | Start or stop the local stack, preserving data. |
| `make logs` | Follow application and migration logs. |
| `make migrate` | Rebuild the image and run current Goose migrations against the Compose database. |
| `make test` | Run all tests uncached, including isolated PostgreSQL containers. |
| `make verify` | Check formatting, generation, lint, vulnerabilities, build, tests, and race detection. |

## Runtime and configuration

Compose reads `.env` and supplies `DATABASE_URL`, `PGUSER`, and `PGPASSWORD` to the application. Database credentials are separate from the URL, so passwords containing URL punctuation do not need escaping. For running the binary directly, export a PostgreSQL URL with host and database plus the appropriate credentials. The binary does not source `.env`.

| Variable | Default / requirement |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL URL with host and database; Compose supplies it. |
| `PGUSER`, `PGPASSWORD` | PostgreSQL connection credentials; Compose supplies them from `POSTGRES_USER` and `POSTGRES_PASSWORD`. |
| `LISTEN_ADDR` | `:8080`; valid numeric TCP port required. |
| `LOG_LEVEL` | `info`; accepts `debug`, `info`, `warn`, `error`. |
| `DB_STARTUP_TIMEOUT` | `5s`; initial database connection and migration-command budget. |
| `READINESS_TIMEOUT` | `1s`; each readiness database round trip. |
| `HTTP_HEADER_TIMEOUT` | `5s`. |
| `HTTP_READ_TIMEOUT` | `10s`. |
| `HTTP_WRITE_TIMEOUT` | `15s`. |
| `HTTP_IDLE_TIMEOUT` | `60s`. |
| `SHUTDOWN_GRACE` | `10s`; admitted requests may complete without cancellation. |
| `CLEANUP_TIMEOUT` | `5s`; wait for canceled work and close owned database resources. |
| `COMPOSE_STOP_GRACE_PERIOD` | `20s`; must cover shutdown, cleanup, and an additional 5s margin. |

All durations must be positive. Invalid configuration, failed initial database connection, or failed listener startup exits nonzero. Probe bodies and application logs exclude raw database errors, credentials, request bodies, and URLs. Each response has `X-Request-ID`; JSON request logs contain the matching identifier, status, and elapsed milliseconds.

`GET /healthz` checks only HTTP process availability. `GET /readyz` executes a generated `SELECT 1` with a deadline and returns 503 during a database outage. Readiness recovers when the same database endpoint returns. SIGINT/SIGTERM makes the service unready and closes the listener. Active requests retain their contexts during the grace period; overdue work is canceled, including pgx calls. Cleanup must complete within its separate budget or the process exits nonzero.

## Manual outage check

```sh
docker compose stop db
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
docker compose start db
curl --fail --retry 10 --retry-delay 1 --max-time 2 -i http://localhost:8080/readyz
```

During the outage, health returns 200 and readiness returns 503. Starting the container does not wait for PostgreSQL to accept connections; readiness may briefly stay 503. The final command retries until recovery or its retry limit. Adjust the HTTP port if `APP_PORT` differs. Inspect correlated JSON logs with `make logs`.

## Database and tests

`db/migrations/` contains production Goose SQL migrations. No business schema is installed by the foundation; an empty migration directory is a successful startup check. Add versioned migrations there when implementing persisted application behavior. Keep the sqlc schema input aligned with those migrations. Regenerate `internal/postgres/queries/` through `make generate`.

Integration tests run PostgreSQL 18.6 through Testcontainers using the same application migration runner. Test-only SQL under `internal/integration/testdata/` exercises apply, repeated apply, rollback, transaction failure, and restart retention in isolated containers. These fixtures are excluded from the production image. Tests also verify database-outage readiness recovery and cancellation of an active PostgreSQL query during shutdown. Missing Docker fails the checks; integration tests do not silently skip.

GitHub Actions runs `make setup` and `make verify` for pull requests and pushes to `main`. Hosted CI requires an actual successful GitHub Actions run before it is considered verified.

## Scope

The service exposes health and readiness only. Authentication, payment operations, webhook processing, browser pages, and reconciliation are not implemented. Local execution and automated checks are supported; this is not a production deployment.
