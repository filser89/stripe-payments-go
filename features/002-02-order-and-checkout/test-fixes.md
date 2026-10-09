# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Scope**: R27 — REC-003, STR-007 | **Independent correction review**: PENDING

The correction scope is the dispatch-preparation safe-age boundary for fresh-key same-operation continuation, sourced by FR-010/011/019 and Q3. R27 is a mechanical same-file test correction. R26 remains the accepted manual SEC-003/NE-6/D11 production-verification condition: every named outcome/error variant requires independent actual semantic-log and source review during code review and final audit. Metadata-only logs or a green helper cannot satisfy that condition.

## Current test contract

`internal/payment/recovery_test.go:664`, `TestFreshContinuationSafeAgeAfterDispatchPreparation`, contains three named leaves. Each enters `Continue` with a fresh request key on the same existing unresolved operation, without a saved session ID, 100ms before first_dispatch_at+23h. A controlled `PrepareDispatch` hook verifies initial eligibility and the already accepted durable continuation binding, then advances server time by 50ms, 100ms or 200ms.

The before-cutoff control requires exactly one physical creation call, valid open evidence, and unchanged operation/key/snapshot identity. Gateway entry verifies the actual controlled send time, strict safe age, and immutable snapshot. Exact-cutoff and after-cutoff controls require zero physical gateway calls, unresolved/pending/investigation, disabled retry/replacement flags, and retention of both original and accepted continuation bindings, one order, one operation, stored purchase fields, first dispatch and original Stripe snapshot/key. Fresh dependencies can inspect and replay the accepted continuation binding as pending; an additional fresh key is blocked without a replacement operation or binding. This contract does not discard an accepted binding after dispatch preparation.

The existing `TestSafeReplayAgeAfterDispatchPreparation` original-key Create controls retain their source/digests. The same-state outcome/audit tests in `internal/postgres/payment_test.go`, direct semantic logging tests in `internal/payment/outcome_logging_test.go`, shared fakes/helpers, H1–H5 recovery fixtures, and all other prior leaves retain their source/digests.

The independently supplied counterexample is `/private/tmp/feature2-spec-review/recovery_test.go:586`, `TestReviewSafeAgeCrossedDuringDispatchCommit`: a fresh-key `Continue` beginning inside the safe window reaches creation after a 200ms dispatch transaction delay. A policy checking safe age after preparation only for Create violates this continuation test contract. This author does not claim independent execution against production or implementation correction.

## Retained scope evidence

The prior native handoff contains 681 leaves: 65 passed, 616 expected failed, zero pending. Its 19 regression leaves comprise three original-key Create controls, four real PostgreSQL same-state outcome/audit/rollback cases, and twelve direct-inspection/stored-rejection semantic-log cases. Their identities, digests and outcomes remain protected. R27 is the only correction finding in this author scope; R26 retains its separate manual condition.

## Protected handoff

Correction entry: `737682ed0938e89f14153ce3cfe3efea0b453d4d`, clean `002-checkout-frozen-hygiene`. Original author checkpoint: `763204b5ca7e74c3dbc0b6b4a085afbdef2ae28a`. Effective installed Kaba source: `638047e7d671b86a061516dad300200fa0ade032`; installed script tree matches the retained source. Configured runner remains `go test ./...`, Go 1.27.2 darwin/arm64, no tags, count 1, unchanged effective compiler context.

Original baseline SHA256: `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`; 66 baseline leaves. The locked 66-entry plan retains one MODIFY and 65 TOUCH entries, zero appends/removals. Original baseline, plan/lock, specification/criteria, test review, five default scaffolds/manifest, application rules, execution configuration and shared security dependency manifests are protected. The live application and production source receive no edits. R26 and the independent review report remain reviewer-owned.

## Validation

- Findings processed: 1 / 1; mechanical same-file fixes: 1; escalations: 0; skipped: 0.
- Static preflight: PASS; pinned golangci-lint v2.14.0 reports zero issues for `./internal/payment` in a temporary copy with the native generator's five unchanged default scaffolds materialized. Native source discovery finds 684 leaves; all 681 prior leaf identities/digests remain exact, with precisely the three new named continuation leaves. Formatting, ownership classification and Git whitespace checks pass.
- One final native post-test capture: PASS; 684 complete leaves, 65 passed, 619 expected failed, zero pending. All three new continuation leaves are red against the unchanged default scaffolds.
- Native baseline → post-test comparison: PASS; 618 new failed leaves, one conforming MODIFY, 65 retained baseline greens, 65 allowlisted content edits, 66 plan entries, zero appends/removals. The 619 implementation-required identities exactly equal failed leaves and retain all 616 prior targets.
- Native banned-pattern scan: PASS; 35 discovered test/support files inspected from the one supplied test path.
- Protected handoff proof: PASS; all 681 prior identity/status/digest tuples remain exact. All 18 recorded baseline/plan/lock/rules/configuration/specification/criteria/review/scaffold/dependency files, five scaffold contracts/manifest, overlay packages, effective compiler context and repository-relative protected paths remain intact. Only the one test source, this scoped report and the mandatory native post-test snapshot differ.
- Independent scoped review: PENDING. `/kaba:review-tests REC-003 STR-007` must inspect R27's three new leaves and their dependencies, preserve unaffected evidence and R26's manual condition, and recompute the merged verdict. This author does not edit `test-review.md`, approve, integrate or continue implementation.
- Aggregate `make verify`, overlay-free production execution and race verification remain outside this deliberately red test session. Author diagnostics stay under `/private/tmp/checkout-continuation-r27-preflight`; no result archive, raw runner event collection or extra manifest is added to the application.
