# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Correction review**: PENDING

The correction scope is H1–H4. Independent review must cover the three affected test files and their source/digest dependencies. R26 remains a separate known manual verification obligation; this correction does not resolve or re-review it.

## Current correction evidence

| Finding | Source obligations | Current test contract |
| --- | --- | --- |
| H1 | FND-003, FND-004, STR-004; Q6/Q7 | The stalled-body shutdown connection remains open until its test callback exits. Its deferred closure checks the returned error through `require.NoError` in that same test goroutine. Shutdown, cancellation, admission, completion and durable-state assertions remain required. |
| H2 | HTTP-001/002/005, SEC-001, FND-002, CFG-001/002/003; Q7 | `commandCheckoutRequest` uses a credential switch. `valid` sets the configured username/password, `wrong` sets the configured username with the wrong password, and every other credential value leaves Basic Auth absent. Content-Type, client request deadline, response-body closure and return values retain their existing contracts. |
| H3 | FND-003, FND-004, STR-004 | The actual shutdown witness loads a valid `LISTEN_ADDR` of `127.0.0.1:8080`. Its separate `net.Listen("tcp", "127.0.0.1:0")` obtains the real ephemeral listener supplied to `Server.Serve`; the test sends requests to that bound address. Production configuration retains the 1–65535 port requirement and the foundation's zero-port rejection assertion. |
| H4 | HTTP-007, REC-002 | After a real PostgreSQL container stop and restart, the outage witness discovers the container host and current mapped 5432 port, preserves the database URL credentials/path/query, creates and cleans up a new pool, and requires that pool to become reachable within 15 seconds. A separate 20-second restart context bounds endpoint discovery/readiness. Fresh repository, service and HTTP dependencies use the restored DB fixture and an independent pool at the discovered URL. |

## Exact source blocks

`cmd/service/checkout_test.go:206`:

```go
switch credential {
case "valid":
    r.SetBasicAuth(commandUser, commandPassword)
case "wrong":
    r.SetBasicAuth(commandUser, "wrong")
}
```

`internal/integration/checkout_deadlines_test.go:381`:

```go
env := testutil.Environment(j.DB.URL, "127.0.0.1:8080")
```

`internal/integration/checkout_deadlines_test.go:443`:

```go
defer func() { require.NoError(t, conn.Close()) }()
```

`internal/integration/checkout_recovery_test.go:326`:

```go
restartCtx, restartCancel := context.WithTimeout(context.Background(), 20*time.Second)
defer restartCancel()
require.NoError(t, j.DB.Container.Start(restartCtx))
host, err := j.DB.Container.Host(restartCtx)
require.NoError(t, err)
port, err := j.DB.Container.MappedPort(restartCtx, "5432/tcp")
require.NoError(t, err)
databaseURL, err := url.Parse(j.DB.URL)
require.NoError(t, err)
databaseURL.Host = net.JoinHostPort(host, port.Port())
pool, err := pgxpool.New(restartCtx, databaseURL.String())
require.NoError(t, err)
t.Cleanup(pool.Close)
require.Eventually(t, func() bool { return pool.Ping(restartCtx) == nil }, 15*time.Second, 100*time.Millisecond)
restoredDB := &testutil.DB{URL: databaseURL.String(), Pool: pool, Container: j.DB.Container}
fresh := journeyWith(t, restoredDB, restoredDB.Independent(t), j.Stripe, j.Options)
```

H4 retains the real container stop after committed binding observation and the external-success barrier; bounded completion; exact HTTP 503/`temporarily_unavailable`; exact accepted order/operation IDs; meaningful sanitized correlated log assertion; one logical Stripe object; and post-restart HTTP 200 with those same IDs and one logical object. The recovery uses the same restarted container and persisted database; no fixed host port, new database, production fallback, exclusion or weakened outage assertion is required.

## Changed artifacts

```text
cmd/service/checkout_test.go
internal/integration/checkout_deadlines_test.go
internal/integration/checkout_recovery_test.go
features/002-02-order-and-checkout/snapshots/post-test.json
features/002-02-order-and-checkout/test-fixes.md
```

The configured native engine owns snapshot generation, completion targets and scaffold manifest validation. All five scaffold source paths and hashes remain portable repository-relative records. The original baseline, 66-entry plan/lock, execution configuration, specification, criteria, production sources and test review remain bound. No testutil helper or allowlist append is required.

## Validation

- Findings processed: 4 / 4; same-file corrections: 4; escalated: 0; skipped: 0.
- Pinned golangci-lint v2.14.0 preflight for `./cmd/service ./internal/integration`: PASS, zero issues. The preflight uses a temporary source copy with the installed native generator's default compilation scaffolds materialized because golangci-lint cannot read virtual added files through the overlay. The isolated application checkout retains test-source corrections and native evidence.
- Final native post-test capture: PASS, 662 complete leaves, 65 passed, 597 expected red, 0 pending.
- Native baseline → post-test compare: PASS, 596 new failed leaves, one conforming MODIFY, 65 retained baseline greens, 65 allowlisted digest changes, 66 plan entries and zero appends/removals.
- Identity/status/target comparison: PASS, all 662 identities and statuses match the frozen suite; all 597 implementation-required identities remain exact.
- Snapshot build context: PASS, unchanged Go 1.27.2 darwin/arm64, `go test ./...`, package/tag selection and effective compiler settings.
- Scaffold portability: PASS, all five repository-relative source paths/hashes, overlay packages and native manifest match. Manifest validation requires no content change.
- Native banned-pattern scan: PASS, 34 discovered test/support files inspected.
- Test-session ownership, `gofmt` and Git whitespace checks: PASS.
- Protected-artifact proof: PASS, original baseline, plan/lock, scaffold manifest, specification, criteria and test review remain byte-identical. Exactly the five listed paths are changed.

The original baseline SHA256 is `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`. It contains 66 passed leaves. The locked plan has one MODIFY and 65 TOUCH entries.

Independent scoped correction review is PENDING. Feature behavior remains deliberately red against missing production declarations supplied by five default compilation scaffolds. Static preflight and native red-session gates cannot establish implementation correctness, race safety or the restored connected outage/recovery runtime path. Those checks require the authorized implementation context. R26's manual verification remains separate.
