# Test Fixes: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Independent correction review**: PENDING

The pending scope is REC-003, STR-007, DATA-001, DATA-004, SEC-002, SEC-003 and STR-008, with source FR-010/011/017/020/021 and shared Q2/Q3/Q5/Q6. Corrections are limited to durable regressions for independently confirmed code-review defects. R26 remains the accepted manual SEC-003/NE-6/D11 production-verification condition; the shared logging helper and existing review are preserved.

## Current test contracts

| Finding | Source/evidence | Durable regression contract |
| --- | --- | --- |
| C1 — Safe replay age at gateway entry | REC-003, STR-007; FR-010/011; conservative strict 23-hour cutoff | `TestSafeReplayAgeAfterDispatchPreparation` starts 100ms before cutoff, advances controlled server time during repository dispatch preparation, and verifies allowed creation 50ms before cutoff plus zero creation exactly at/100ms after cutoff. Gateway entry itself asserts safe age and the original immutable Stripe snapshot. All rows retain order, operation, complete request binding, amount, key and parameters. Exhausted rows remain unresolved/pending/investigation; a fresh service can inspect/replay the same operation, while a fresh continuation key cannot allocate a replacement. |
| C2 — Same-state saved external outcome and audit | DATA-001, DATA-004; FR-017; Q2 | `TestSameUnresolvedOutcomeAuditAtomicity` covers a saved server failure and saved session/PaymentIntent evidence awaiting confirmation, all with state `unresolved`. Each business outcome requires one audit entry retaining failure/request/available external identifiers and per-order sequence. Real PostgreSQL write and deferred-commit failures leave every durable row/history unchanged; retry commits one matching entry. A genuinely identical repeat uses current version/ownership guards and adds no duplicate history. Timestamp/request-ID-only refreshes are outside this assertion scope. |
| C3 — Direct service logging semantics | SEC-002/003, STR-008; FR-020/021; Q5/Q6; approved D11 | `TestDirectInspectionOutcomeLogs` covers invalid read/history IDs, invalid history cursor/limits, missing records, and database read failures. Each direct operation must log actual `failed` outcome, precise category, and only known correlation; a database failure's returned known order ID must agree with the log. Unaccepted invalid/missing input cannot fabricate IDs. `TestRejectedReplayLogsSavedFailure` requires validation/credential/permission classification and known rejected identity on saved-rejection replay without DB/Stripe side effects. The local JSON oracle checks decoded semantic fields directly and rejects static metadata-only logs; sensitive input/cause/key sentinels remain absent. |

The logging oracle uses the field vocabulary already accepted in code-plan D11. It introduces no public contract, logging framework, helper redesign or additional gate. Direct-service request-context propagation remains within the independent D11 verification condition; the five frozen default scaffolds contain no request-context helper declaration.

## Scoped source evidence

- `internal/payment/recovery_test.go`: three controlled-delay leaves in `TestSafeReplayAgeAfterDispatchPreparation`.
- `internal/postgres/payment_test.go`: four real PostgreSQL outcome/rollback leaves in `TestSameUnresolvedOutcomeAuditAtomicity`.
- `internal/payment/outcome_logging_test.go`: nine direct-inspection leaves and three stored-rejection leaves, with one local decoded JSON assertion helper.

The existing transaction phase matrix exercises actual enum transitions: its unresolved rows explicitly install a prior open fixture. Its same-state zero-history branch is not exercised by any current row and presents no conflicting outcome assertion. Existing read/replay/history checks cover genuinely unchanged established observations or active-lease pending reads. No existing assertions or test helpers require correction for this scope.

## Counterexample evidence boundary

Independent reviewers supplied production counterexamples at application commit `1ec4bfb3907ade948042557b8c3dcb4e580128de`:

- `/private/tmp/feature2-spec-review/recovery_test.go`, `TestReviewSafeAgeCrossedDuringDispatchCommit`: one production creation POST at first-dispatch age 23h+100ms after a 200ms transaction delay.
- `/private/tmp/feature2-spec-review/payment_test.go`, `TestReviewUnresolvedObservationAudit`: real PostgreSQL retains server failure/investigation/request evidence while history stays at three entries without a corresponding outcome entry.
- `/private/tmp/feature2-standards-logging-repro/main.go`: production direct invalid reads emit no outcome; Get database failure omits the returned order reference; History database failure uses generic unavailable context; stored credential rejection uses generic rejection context.

These are reviewer-supplied red counterexamples, distinct from this author's native preimplementation red capture. The author does not claim an independent production reproduction, implementation correction, code-review approval or final runtime/race verification.

## Protected handoff

Baseline SHA256: `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`; 66 baseline leaves. The locked plan retains one MODIFY and 65 TOUCH entries, with no appends/removals. The original baseline, plan/lock, specification, criteria, test review, five scaffold sources/manifest, execution configuration, application rules, and shared dependency manifests are protected. Current H1–H5 fixture contracts remain intact, including bounded active-lease recovery and same-container PostgreSQL restart.

Production code and live application files receive no edits. The three authorized dependency security upgrades belong to the production handoff; this isolated correction neither reverts nor replays a shared manifest.

## Validation

- Findings processed: 3 / 3; same-file regression groups: 3; unresolved author-side escalations: 0; skipped: 0.
- Pinned golangci-lint v2.14.0 static preflight: PASS, zero issues for `./internal/payment ./internal/postgres` in a temporary source copy with the installed native generator's five default scaffolds materialized. No production behavior is substituted.
- One final native post-test capture: PASS; 681 complete leaves, 65 passed, 616 expected failed, zero pending. All 19 added regression leaves are red against default preimplementation declarations. This red capture establishes the native handoff contract rather than production counterexample execution.
- Native baseline → post-test compare: PASS; 615 new failed leaves, one conforming MODIFY, 65 retained baseline greens, 65 allowlisted content changes, 66 plan entries, zero appends/removals.
- Native banned-pattern scan: PASS; 35 test/support files inspected. The native scanner discovers source dependencies in addition to its three supplied test paths.
- Protected-artifact proof: PASS; 18 recorded baseline/plan/rules/configuration/specification/criteria/review/scaffold/dependency artifacts remain byte-identical. All five scaffold manifest entries, overlay packages and hashes retain their exact contracts.
- Existing-suite proof: PASS; all 662 prior identity/status pairs and leaf digests remain exact. The native 616 implementation-required identities equal the failed leaves, comprising the retained 597 targets plus 19 added regressions.
- Execution context and portability: PASS; unchanged Go 1.27.2 darwin/arm64, `go test ./...`, no tags, count 1, identical effective compiler context, repository-relative protected/scaffold paths.
- Test ownership, `gofmt` and Git whitespace checks: PASS. Exactly three test sources, native post-test snapshot and this report form the correction; no implementation, shared helper, scaffold, plan, baseline or manifest edits.
- Aggregate `make verify`, overlay-free production execution and race verification are outside this deliberately red test session. Their final evidence belongs to the implementation/review contexts. No new result archive or diagnostic-log collection is added to the application; author diagnostics stay under `/private/tmp/checkout-reviewed-defects-preflight`.

Independent scoped `/kaba:review-tests REC-003 STR-007 DATA-001 DATA-004 SEC-002 SEC-003 STR-008` must inspect all three changed test paths and their source/digest dependencies, retain unaffected findings, and preserve R26's separate manual condition. This author does not self-approve or integrate the correction.
