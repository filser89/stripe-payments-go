# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Correction review**: PENDING

The correction scope is R21, R23, R24 and R25 in `test-review.md`, including their required test/helper dependencies. The specification, criteria, test plan/lock, execution configuration, baseline, production sources and `test-review.md` remain bound. No allowlist append is required.

## Current correction evidence

| Finding | Criteria | Current assertions |
| --- | --- | --- |
| R21 | SEC-003 | Actual invalid-input feature requests and missing/wrong-auth endpoint, sequence and overlap requests inspect captured JSON records. `RequireRejectedLog` requires the response request ID, useful outcome/error context, sanitized logs and no invented durable IDs. A rejection before acceptance cannot invent an order/operation; an existing connected fixture permits only its actual known IDs. HTTP/effect/body-read assertions remain in place. |
| R23 | SEC-003 | Web and connected log checks share `OutcomeLogContext`. They inspect structured status, result/state/category/error fields and messages jointly. A meaningful `checkout_created` record with HTTP 201/result `created`, or category `stripe_http_500`, satisfies the context check. Correlation IDs, generic request-completion telemetry and empty context alone do not. Exact category/message vocabulary is unconstrained. The service-only ownership/cancellation callers retain their existing known-ID correlation without inventing an HTTP response/request ID. |
| R24 | STR-004/005/006, LIFE-009 | The successful GET leaves time for a distinct operation's first capped POST in the positive case. In `elapsed_exhausted_after_get`, a test-owned repository wrapper holds only the successful committed preparation return until later than the GET wire timestamp plus the entire 500ms external budget. Its wait uses the actual overall incoming request deadline. The test observes that boundary, one GET/no POST, committed prepared identity and bound-key recovery. No business retry wait is required between successful retrieval and first creation. |
| R25 | REC-001, ID-001, DATA-003/004 | Established lost-caller-response replays preserve immutable purchase/binding/snapshot data, business state/associations, exact append-only history, response IDs and logical session count. Only the observed order's update/version bookkeeping and the observed operation's owner/version/update/observation provenance bookkeeping are excluded from comparison. Retrieval ownership must be released, version cannot regress and update timestamps cannot move backward. All unrelated rows remain fully compared. Direct transaction-failure and stale/unauthorized full-row witnesses, including R15/R18, retain their existing assertions. |

## Changed test paths and dependencies

```text
internal/integration/checkout_concurrency_test.go
internal/integration/checkout_deadlines_test.go
internal/integration/checkout_recovery_test.go
internal/integration/orders_test.go
internal/testutil/logs.go
internal/web/checkout_security_test.go
internal/web/checkout_validation_test.go
```

The concurrency change is solely an affected shared-log-helper call. The shared `internal/testutil` source contributes to executable support digests; the bound 65 TOUCH entries cover retained baseline green tests. There are no new registrations or fixture/scaffold changes. The installed scripts produce `snapshots/post-test.json` and refresh the native scaffold manifest.

## Validation

- Findings processed: 4 / 4; cross-file corrections: 4; skipped: 0.
- Focused compilation: PASS — corrected web/integration/helper packages compile using the installed native scaffold generator and `go test -run '^$' -count=1`.
- Native source registration/digest preflight: PASS — 662 leaf identities.
- Native post-test capture: PASS — 662 complete leaves: 65 passed, 597 expected red, 0 pending; exactly one final capture.
- Native baseline → post-test compare: PASS — 596 new failed leaves, one conforming MODIFY, 65 retained baseline greens, 65 allowed support-digest changes; 597 native implementation targets and no removed identities.
- Native banned-pattern scan: PASS — 34 discovered test/support files scanned.
- Test-session ownership, formatting and Git whitespace checks: PASS. Protected artifacts are unchanged. The native scaffold manifest matches all five unchanged scaffold sources.
- Baseline: 66 passed; SHA256 `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`.
- Plan/lock: 66 entries, one MODIFY and 65 TOUCH; no append.
- Independent scoped correction review: PENDING.

The five compilation scaffolds supply missing declarations with default bodies. Feature behavior remains deliberately red against missing implementation. These gates establish test-session evidence; implementation, aggregate/race/static/vulnerability checks and behavioral readiness belong to their authorized phases.

## Independent review scope

A fresh `/kaba:review-tests SEC-003 STR-004 STR-005 STR-006 LIFE-009 REC-001 ID-001 DATA-003 DATA-004` reviewer must inspect R21/R23/R24/R25 and the seven affected paths/dependencies, retaining unaffected findings. Required focus: actual rejection-path log capture, meaningful logging equivalents versus IDs-only telemetry, the provable elapsed boundary and positive capped POST, and replay bookkeeping exclusions versus immutable/business/history equality. Correction review remains PENDING until that independent review completes.
