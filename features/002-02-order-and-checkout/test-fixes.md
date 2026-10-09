# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Correction review**: PENDING

The correction scope contains all R1–R22 in `test-review.md`. Cross-file corrections include test-owned request/log/Stripe/database helpers and connected witnesses. Production code, migrations, generated SQL, specifications, criteria, plans, execution configuration, and `test-review.md` are outside the correction writes.

## Findings and current evidence

| Finding | Criteria | Current test evidence |
| --- | --- | --- |
| R1 | CFG-001–003 | `internal/config/checkout_test.go` pins defaults and every nondefault captured value, reversed limits, absent base URL, and semantic origin equivalence. `cmd/service/checkout_test.go` pins real configured limits, return URLs, call deadlines and attempt ceilings. Connected composition validates the effective serving settings. |
| R2 | INP-001/002, HTTP-002 | Web valid controls compare captured description/amount/key to independently decoded inputs. `TestConnectedAcceptedPurchasePreservation` pins case, interior whitespace, composed/decomposed Unicode and custom lower/upper amounts in HTTP, binding, purchase and actual wire product data. |
| R3 | INP-003–006 | The continuation parser has required/null/type/version/variant/duplicate/object/media/size/error-after-valid-input cases. Real TCP tests exercise both POST bodies at 4096/4097 with known length/chunked framing, bounded GET/HEAD unknown bodies, zero-byte errors and stalled forms. Invalid requests have no operation effects. |
| R4 | HTTP-004/002/005 | Local parser/routing errors pin stable codes, nonempty sanitized messages, UTF-8 JSON/no-store/request-ID headers and GET/HEAD error parity. Message wording remains an implementation choice. |
| R5 | HTTP-006, DATA-004 | Web history compares all represented entry values, optional identifiers, all representable kinds and UTC timestamps. `TestConnectedHistoryValuesPaginationAndIsolation` matches persisted entries across real HTTP pages and an independent order, with read-only durable/wire witnesses. |
| R6 | HTTP-007, REC-001/002, STR-002, ID-001 | Connected cases cover external response loss, caller response loss after local commit, a canceled service at the committed marker before SDK send, result-commit failure, unavailable result/read with known IDs, repeated fresh dependency recovery, and missing/present supplemental request IDs. |
| R7 | ID-002/004, LIFE-003/005, HTTP-004 | Web continuation capture preserves target/key. `TestConnectedGlobalHistoricalAndReusableBindings` tests initial/continuation and cross-order collisions, historical continuation-key replay, an unbound blocked key reused after verified expiry, and changed amount conflict outside tightened limits. |
| R8 | LIFE-004, DATA-002/003, LIFE-006, FND-004 | Independent DB transactions overlap replacement allocation behind real row locks, enforce safety of every prior attempt, recheck paid state at commit, and pin loser bindings/history. Two connected app compositions overlap expiry retrieval and replacement; delayed results encounter ownership/paid/current-operation changes. |
| R9 | LIFE-004, HTTP-001 | Lifecycle mutation targets only the predecessor session. The replacement is open/unpaid with a usable URL and distinct operation/session identities; logical object count is two. |
| R10 | LIFE-007/002 | SDK Create/Retrieve compare all translated evidence to actual response fields, including correlation/metadata, mode, expiry and nil/populated IDs. Connected mismatches preserve unresolved/investigation and prevent replacement. |
| R11 | STR-001, FR-001 | `CheckWire` and the stateful server validate exact card-only/one-item fields, quantity, forbidden additional fields and disabled pricing options. Fixture idempotency compares complete parameter forms; it cannot hide an extra charge component. |
| R12 | STR-002/003/007, REC-002 | Immutable snapshot comparisons exclude only LastDispatchAt. Policy and real SDK retry witnesses observe durable last-dispatch updates before every possible send and preserve first dispatch, expiry, key, versioned wire parameters and purchase data. |
| R13 | STR-004, INP-005/006, FND-004 | Valid two-second configured real transports cover stalled lengths/chunks/read bodies, earlier parent deadlines and response writes. SDK response-body stalls flush headers and join cancellation. Connected real DB locks before dispatch/after external success witness admission-bound completion, recoverability and no detached work. |
| R14 | STR-005/006, LIFE-009 | Local headers constrain real Retry-After and Stripe-Should-Retry translation/waits. Connected GET→POST cases cap total attempts at 1/2/3, cap calls by remaining elapsed budget, retain prepared intent after eligibility exhausts budget, cancel actual waits and recover the same bound operation. |
| R15 | DATA-001, HTTP-007, REC-002 | Direct failed repository transactions compare independently read complete order/operation/binding/history rows. Connected result failure compares all business data/history while treating explicitly safe coordination release separately, then recovers the same identity. |
| R16 | DATA-001, STR-002 | Repository phase matrices inject write and deferred commit faults into acceptance, dispatch, observation variants and later preparation, then prove successful retry with one actual transition. Connected dispatch-history commit failure has no SDK send and retains prepared work. |
| R17 | DATA-002, FR-003/018, Q2 | Direct independent DB attempts reject invalid amounts/currencies and duplicate session/PaymentIntent association. Normal repository paths cannot rewrite accepted purchase/binding/snapshot values. No privileged SQL immutability trigger is required. |
| R18 | DATA-003, LIFE-006 | Ownership/current/paid rejected observations compare complete persisted rows. Delayed success/rejection cases preserve current owner, version, business values and audit content and expose no stale action/URL. |
| R19 | DATA-005 | A real known-session GET is held while an independent order completes. Database activity checks cover any open client transaction, including active transactions. Cancellation joins SDK/service work and preserves saved correlation and independent success. |
| R20 | SEC-001 | Actual feature methods and command wiring cover valid/missing/wrong credentials, valid-then-missing sequences and deliberately overlapping requests. Rejection precedes body reads and preserves durable state/wire count. |
| R21 | SEC-003 | Correlated JSON logs require useful outcome/state/error context rather than IDs alone. Web and connected cases exercise accepted/pending/rejected, database acceptance/result/read faults, timeout/server/mismatch and ownership/cancellation paths with sanitized values. |
| R22 | FND-003/004 | `TestActualCheckoutServerShutdown` invokes Serve around real checkout work: active completion during grace, overdue Stripe/DB/body cancellation, owned-work/peer joins, cleanup after work, durable unfinished work, independent committed success and admission before parsing/effects. Implementation-time command exit inspection remains separate. |

## Validation

- Findings processed: 22 / 22; cross-file corrections: 22; skipped: 0.
- Native post-test capture: PASS — 662 complete leaves: 65 passed, 597 failed, 0 pending; 596 new failed leaves and the single MODIFY remain deliberate implementation targets.
- Native baseline → post-test compare: PASS — 65 retained baseline green outcomes, one conforming MODIFY, 65 allowed support-digest changes, no removed identities; 66 planned allowlist entries, 0 appended. Native implementation targets: 597. Native scaffold manifest: five scaffold sources.
- Native banned-pattern scan across all modified test paths: PASS — 34 discovered test/support files scanned.
- Original baseline: 66 passed; SHA256 `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`.
- Plan JSON, locked execution configuration, specification, criteria and original baseline remain bound; no allowlist append is needed.
- Formatting and Git whitespace checks: PASS.
- Independent correction review: PENDING. Gates establish valid test-session evidence, not behavioral implementation readiness.

Installed validation commands:

```sh
$(git config kaba.scriptdir)/snapshot-tests.sh capture post-test
$(git config kaba.scriptdir)/snapshot-tests.sh compare post-test
$(git config kaba.scriptdir)/banned-patterns.sh <the 25 changed test/helper paths below>
gofmt -l <the 25 changed test/helper paths below>
git diff --check
```

## Changed test paths

```text
cmd/service/checkout_test.go
internal/config/checkout_test.go
internal/integration/checkout_concurrency_test.go
internal/integration/checkout_deadlines_test.go
internal/integration/checkout_lifecycle_test.go
internal/integration/checkout_recovery_test.go
internal/integration/orders_test.go
internal/payment/checkout_test.go
internal/payment/fixtures_test.go
internal/payment/recovery_test.go
internal/postgres/payment_concurrency_test.go
internal/postgres/payment_test.go
internal/stripeapi/checkout_test.go
internal/stripeapi/transport_test.go
internal/testutil/postgres.go
internal/testutil/postgres_faults.go
internal/testutil/stripe_http.go
internal/web/checkout_helpers_test.go
internal/web/checkout_routing_test.go
internal/web/checkout_security_test.go
internal/web/checkout_transport_test.go
internal/web/checkout_validation_test.go
internal/web/orders_checkout_test.go
internal/web/orders_create_test.go
internal/web/orders_history_test.go
```

The native post-test snapshot and scaffold manifest are produced by the installed scripts. `test-fixes.md` is the correction report. The five scaffold sources require no edits.

## Review scope and verification limits

The fresh reviewer must inspect all R1–R22, their criterion/source mappings, all 25 changed test/helper paths, and native outcome preservation. Shared helper changes affect imported-support digests; the existing 65 TOUCH entries account for retained baseline green tests. Feature 3 payment confirmation/webhooks and Feature 5 reconciliation/listing/workers remain outside runtime scope. Confirmed-paid records are prerequisite guard fixtures.

The five minimal compilation scaffolds supply missing declarations with default bodies. Feature behavior remains deliberately red against missing implementation; successful compilation, complete outcomes and preservation gates do not establish that new behavior runs correctly. Production implementation and required aggregate/race/static/vulnerability verification belong to their authorized phases. No independent self-review or next Kaba phase is performed here.
