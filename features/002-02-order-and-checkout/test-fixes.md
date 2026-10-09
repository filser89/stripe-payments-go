# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Correction review**: PENDING

The pending correction scope is H5 in `TestResultAndReadUnavailableReturnKnownIdentity`, covering HTTP-007, REC-002, source A-08/FR-010, and approved code-plan D4/D6. Independent review must cover this case and its source/digest dependencies. H1–H4 retain their independently reviewed corrections at `52a1e9bab3ccffc9e8fb3213bedee804abe2fdac`. R26 remains a separate known manual verification obligation; this correction does not resolve or re-review it.

## Current correction evidence

| Finding | Source obligations | Current test contract |
| --- | --- | --- |
| H1 | FND-003, FND-004, STR-004; Q6/Q7 | The stalled-body shutdown connection remains open until its test callback exits. Its deferred closure checks the returned error through `require.NoError` in that same test goroutine. Shutdown, cancellation, admission, completion and durable-state assertions remain required. |
| H2 | HTTP-001/002/005, SEC-001, FND-002, CFG-001/002/003; Q7 | `commandCheckoutRequest` uses a credential switch. `valid` sets the configured username/password, `wrong` sets the configured username with the wrong password, and every other credential value leaves Basic Auth absent. Content-Type, client request deadline, response-body closure and return values retain their existing contracts. |
| H3 | FND-003, FND-004, STR-004 | The actual shutdown witness loads a valid `LISTEN_ADDR` of `127.0.0.1:8080`. Its separate `net.Listen("tcp", "127.0.0.1:0")` obtains the real ephemeral listener supplied to `Server.Serve`; the test sends requests to that bound address. Production configuration retains the 1–65535 port requirement and the foundation's zero-port rejection assertion. |
| H4 | HTTP-007, REC-002 | After a real PostgreSQL container stop and restart, the outage witness discovers the container host and current mapped 5432 port, preserves the database URL credentials/path/query, creates and cleans up a new pool, and requires that pool to become reachable within 15 seconds. A separate 20-second restart context bounds endpoint discovery/readiness. Fresh repository, service and HTTP dependencies use the restored DB fixture and an independent pool at the discovered URL. |
| H5 | HTTP-007, REC-002; A-08/FR-010; D4/D6 | The fresh same-key recovery witness accepts 202 while the retained dispatch lease is active and requires eventual 200 within a 17-second local observation budget. Each request inherits that absolute deadline and is additionally capped by the configured request timeout. Every response retains exact known IDs, immutable accepted binding, one order/operation/binding and one logical Checkout. Pending responses retain exact business rows/history and admit no additional Stripe call. |

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

H4 retains the real container stop after committed binding observation and the external-success barrier; bounded completion; exact HTTP 503/`temporarily_unavailable`; exact accepted order/operation IDs; meaningful sanitized correlated log assertion; one logical Stripe object; and bounded eventual post-restart HTTP 200 with those same IDs and one logical object. While the dispatch lease remains active, HTTP 202 preserves those IDs, binding, business rows/history and external-call count. The recovery uses the same restarted container and persisted database; no fixed host port, new database, production fallback, exclusion or weakened outage assertion is required.

## H5 recovery witness

`internal/integration/checkout_recovery_test.go:344`:

```go
	// An unavailable database cannot release the dispatch claim. Observe public
	// pending responses until its legitimate 12-second lease becomes reclaimable;
	// the five-second margin belongs only to this local recovery witness.
	recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 17*time.Second)
	defer recoveryCancel()
	observed, err := testutil.DurableRows(recoveryCtx, restoredDB.Pool)
	require.NoError(t, err)
	require.Equal(t, businessRows(t, durable), businessRows(t, observed), "outage commits no partial result or business history")
	wireCount := len(j.Stripe.Wires())
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		// The parent deadline caps every request by the remaining witness budget.
		requestCtx, requestCancel := context.WithTimeout(recoveryCtx, fresh.Options.RequestTimeout)
		r := testutil.Request("POST", "/api/orders", purchaseBody(key, "Single café product", 2500), true).WithContext(requestCtx)
		response := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			w := httptest.NewRecorder()
			fresh.Handler.ServeHTTP(w, r)
			response <- w
		}()
		select {
		case w = <-response:
			requestCancel()
		case <-recoveryCtx.Done():
			requestCancel()
			t.Fatal("same-key recovery did not establish checkout within the lease recovery witness budget")
		}
		require.Contains(t, []int{200, 202}, w.Code)
		id, op := envelopeIDs(t, testutil.JSON(t, w))
		require.Equal(t, binding.OrderID, id)
		require.Equal(t, binding.OperationID, op)
		require.Equal(t, 1, j.Stripe.LogicalObjects())
		recoveredBinding, err := fresh.Repo.LoadBinding(recoveryCtx, key)
		require.NoError(t, err)
		require.Equal(t, binding.OrderID, recoveredBinding.OrderID)
		require.Equal(t, binding.OperationID, recoveredBinding.OperationID)
		observed, err = testutil.DurableRows(recoveryCtx, restoredDB.Pool)
		require.NoError(t, err)
		require.Equal(t, durable["payment_request_bindings"], observed["payment_request_bindings"], "same key retains its complete accepted binding")
		for _, table := range []string{"payment_orders", "payment_operations", "payment_request_bindings"} {
			require.Len(t, observed[table], 1, "recovery cannot allocate another order, operation or binding")
		}
		if w.Code == 200 {
			break
		}
		require.Equal(t, "/api/orders/"+binding.OrderID, w.Header().Get("Location"))
		require.Equal(t, "1", w.Header().Get("Retry-After"))
		require.Equal(t, wireCount, len(j.Stripe.Wires()), "active ownership admits no additional Stripe call")
		require.Equal(t, businessRows(t, durable), businessRows(t, observed), "pending recovery preserves business state and exact history")
		select {
		case <-ticker.C:
		case <-recoveryCtx.Done():
			t.Fatal("same-key recovery remained pending beyond the lease recovery witness budget")
		}
	}
```

The test records durable rows before the real outage and verifies that the restored database contains no partial result/history before recovery. The 17-second deadline allows the approved 12-second database lease plus five seconds of local margin; it is fixture evidence, not a production recovery-time requirement. A deadline-bounded ticker observes outcomes rather than changing the clock, lease, schema, or serving budgets. HTTP execution runs in a goroutine with a buffered completion channel and no test assertions; assertions and durable inspection remain in the test goroutine.

## Changed artifacts

```text
internal/integration/checkout_recovery_test.go
features/002-02-order-and-checkout/snapshots/post-test.json
features/002-02-order-and-checkout/test-fixes.md
```

The configured native engine owns snapshot generation, completion targets and scaffold manifest validation. All five scaffold source paths and hashes remain portable repository-relative records. The original baseline, 66-entry plan/lock, execution configuration, specification, criteria, production sources and test review remain bound. No testutil helper or allowlist append is required.

## Validation

- Findings processed in this scope: H5, 1 / 1; same-file corrections: 1; escalated: 0; skipped: 0.
- Pinned golangci-lint v2.14.0 preflight for `./internal/integration`: PASS, zero issues. The preflight uses a temporary source copy with the installed native generator's default compilation scaffolds materialized because golangci-lint cannot read virtual added files through the overlay. The isolated application checkout retains test-source corrections and native evidence.
- Final native post-test capture: PASS, 662 complete leaves, 65 passed, 597 expected red, 0 pending.
- Native baseline → post-test compare: PASS, 596 new failed leaves, one conforming MODIFY, 65 retained baseline greens, 65 allowlisted digest changes, 66 plan entries and zero appends/removals.
- Identity/status/target comparison: PASS, all 662 identities and statuses match the frozen suite; all 597 implementation-required identities remain exact.
- Snapshot build context: PASS, unchanged Go 1.27.2 darwin/arm64, `go test ./...`, package/tag selection and effective compiler settings.
- Scaffold portability: PASS, all five repository-relative source paths/hashes, overlay packages and native manifest match. Manifest validation requires no content change.
- Native banned-pattern scan: PASS, 34 discovered test/support files inspected.
- Test-session ownership, `gofmt` and Git whitespace checks: PASS.
- Protected-artifact proof: PASS, original baseline, plan/lock, scaffold manifest, specification, criteria and test review remain byte-identical. Exactly the three listed paths are changed relative to `52a1e9bab3ccffc9e8fb3213bedee804abe2fdac`; H1–H4 test corrections are preserved.

The original baseline SHA256 is `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`. It contains 66 passed leaves. The locked plan has one MODIFY and 65 TOUCH entries.

Independent scoped correction review is PENDING. Feature behavior remains deliberately red against missing production declarations supplied by five default compilation scaffolds. Static preflight and native red-session gates cannot establish implementation correctness, race safety or the restored connected outage/recovery runtime path. Those checks require the authorized implementation context. R26's manual verification remains separate.
