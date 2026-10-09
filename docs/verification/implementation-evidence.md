# Checkout implementation evidence

The delivered paths are payment contracts/lifecycle/bounded dispatch, PostgreSQL schema and fenced transactions, pinned Stripe SDK, captured serving settings, protected HTTP transport/DTOs, command wiring, and API/recovery/setup documentation. Production compiles without scaffolding or a Go overlay. Test scaffolds remain for the separate authorized cleanup phase.

## Environment and commands

Go toolchain is go1.27.2 darwin/arm64; Docker Desktop server 29.7.2; PostgreSQL container image postgres:18.6-alpine reports server_version 18.6. Dependencies, tool pins, Kaba configuration, normal hooks, Makefile and verification selection remain authoritative. SDK is stripe-go/v87 v87.0.0, API 2026-09-30.endive; adapters disable client network retries and SDK diagnostics.

Focused commands: `go test ./internal/payment`, `go test ./internal/config`, `go test ./internal/web`, `go test ./internal/postgres`, `go test ./internal/stripeapi`, focused connected recovery/concurrency/mismatch tests, and focused command serving-validation tests. PostgreSQL/SDK scoped race verification also passes. The normal web suite completes in approximately 21.6 seconds because it includes actual stalled transport boundaries. These focused durations are supplementary; required full uncached timings belong below.

Static preflight uses the configured pinned golangci-lint. Local workflow verification is `./scripts/testdata/workflows.sh`. Full final native capture/compare/ownership and `make verify` are required for local completion; hosted PR/main CI remains pending final integration.

## Runtime and delivery evidence

| Requirement | Artifact / actual result |
| --- | --- |
| NE-1 / DO-002 | `localcheck.sh.txt` / `localcheck.log`: isolated documented make up, Compose config, real service probe, health 200/readiness 200, DB outage health 200/readiness 503, restart readiness 200. Injected invalid migrate direction makes make up fail and app_running_after_migration_failure=false; restored setup probes successfully. |
| NE-1 / DO-006 | `schema-check.sh.txt` / `schema-check.log` / `schema-server.jsonl`: actual binary serves health 200/readiness 503 without business schema; probe exits 1 independently of Basic/Stripe secrets. Normal migration up, repeated up, down all succeed; all four relations are absent after down. |
| NE-2 / SEC-002 | `.env` remains ignored; tracked examples are placeholders; Docker excludes env files/test fixtures. Random UUIDv4 order/operation/Stripe identities are separate. DTO/history/wire source is explicitly bounded/allowlisted; decoded logs contain no passwords, keys, owner tokens, Checkout URLs, raw dependency messages or submitted descriptions. |
| NE-3 / DO-001/003 | `docs/api.md`, `docs/recovery.md`; driver executes authenticated initial create, read/replay and new-key continuation against real production constructors/DB/SDK. Accepted success is 201, established replay 200, expired eligibility after one GET yields prepared 202, rejection 502, input/auth rejection 400/401. Read/status/history contracts and lifecycle boundaries are covered by frozen suites. |
| NE-4 / DO-004 | SDK source and tests use real one-attempt wire calls against local HTTP; immutable snapshot encoding, version, metadata, return URLs, options, error/header facts and cancellation are exercised without live Stripe. Connected suite establishes durable markers, budgets, response-loss/restart, stale owner/current/paid fences and no network-held transaction. |
| NE-5 / DO-006 | Versioned Goose migration plus db/schema.sql/db/queries/payment.sql and regenerated sqlc output; real PostgreSQL package tests/races establish constraints, immutable rows, per-phase rollback and independent-process guards. No frozen sources are generated or changed by implementation. |
| NE-6 / DO-005 | `localcheck.log` records graceful app exit 0. Main → run → Serve preserves bounded join/cleanup and nonzero cleanup failures. Main hosted workflow executes the same full make verify. Full local gate/timing results are recorded below. |

## Semantic log matrix

`semantic-driver.go.txt` is the retained external verification executable, with `semantic-driver.mod.txt` documenting its temporary module path/replace. Run it from a temporary module with `GOTOOLCHAIN=go1.27.2 go run .`, against the dedicated PostgreSQL18.6 database after Goose up. The driver imports real production constructors and uses a synchronized slog JSON buffer, local real-SDK HTTP stub, independent PostgreSQL control pool, deferred failure triggers, permission faults and ownership takeover. `semantic-logs.json` retains decoded per-scenario records; `semantic-run.log` records execution. Driver input credentials are disposable local fixture placeholders, not account secrets.

| Named variant | Driver scenarios | Production source trace |
| --- | --- | --- |
| Accepted/successful | accepted; established_replay | payment.Service.Create → execute → ApplyObservation; web checkout mutation response/record |
| Prepared/unresolved | prepared_after_get_budget; timeout | Continue → shared externalBudget → BindContinuation → deferred_budget; execute call failure → unresolved observation |
| Confirmed rejection | rejected | execute first owned validation rejection → rejected observation/error; web domainError preserves actual state/class |
| Input/auth rejection | input_rejection; unsupported_media; oversized; body_read; auth_rejection | checkout bounded parser/localKind; authenticate fixed auth_rejected outcome; no durable IDs |
| DB acceptance/result/read | database_acceptance; database_dispatch; database_result; database_result_and_read; database_read | acceptance failure; pre-send marker failure; result rollback → owned release/readback; readback unavailable → known-ID503; Get database_read |
| Stripe timeout/server/mismatch | timeout; server; mismatch | real SDK/context/error translation → execute classification/evidenceState; unresolved/investigation and suppressed URL |
| Cancellation/ownership loss | direct_canceled; ownership_lost | caller cancellation → joined gateway → bounded ReleaseDispatch → canceled log; stale ApplyObservation fence → ownership_lost readback, no stale success |

All 19 scenarios have decoded semantic outcomes and expected statuses/categories. HTTP records correlate to generated response request IDs; business records carry only known durable IDs. Direct cancellation has known order/operation IDs and canceled context without an invented HTTP request ID. Database acceptance/input/auth rejection contain no fabricated durable IDs. Decoded sanitization inspection excludes fixture passwords/Stripe keys/owner tokens/request keys, descriptions, raw SDK diagnostics and Checkout URLs.

SEC-003 independent code review/final audit and advisory R26 remain open until independently verified. These implementation artifacts do not close that independent condition.

## Final local gates and timings

Pending accepted fixture-maintenance handoff and final native gates. Required measurements are `/usr/bin/time -p make test`, `/usr/bin/time -p go test -race -count=1 -timeout=5m ./...`, and `/usr/bin/time -p make verify`, with actual wall seconds/exit codes. Tests use count=1; compilation/module/image caches remain available. No hosted CI, push, PR, merge, architecture-diff or scaffold cleanup is performed by this phase.
