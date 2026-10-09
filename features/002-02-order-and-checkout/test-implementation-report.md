# Test implementation: Create orders and hosted checkout

## Checkpoint

The test suite and execution configuration are at `10a247215041211a6936599b56bfe2deec431cfd` on `002-02-order-and-checkout`. The native post-test snapshot identifies that commit. The Kaba test session lock is armed. Test source, compilation scaffolds, production source, migrations, generated code, dependency pins, execution configuration, and the approved plan are unchanged by this gate finalization. The only checkpoint outputs are the validated plan lock, post-test snapshot, native scaffold manifest, and this report.

The original baseline contains 66 passed leaves, zero failed and zero pending. Its SHA256 is `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`; its current bytes match `5c128df9e23fe90bc1c27f96077fa0a66e83e93f:features/002-02-order-and-checkout/snapshots/baseline.json`. Baseline capture is not part of this checkpoint.

## Coverage and organization

- Planned test files: 29, comprising 21 NEW and 8 MODIFY. The MODIFY classification includes digest-only imported-support coverage; it does not authorize additional body edits. All 66 baseline identities remain present.
- Factories/fixtures: 7 NEW. Shared helpers: 12 NEW and 2 EXISTS, of 14 planned.
- Behavioral criteria mapped: 54/54; all 54 unique IDs are present in test-source comment markers. The plan maps 311/311 required variant groups to evidence locations. Marker and mapping completeness do not establish assertion adequacy; the fresh independent test review must inspect those assertions and named variants.
- Registered evidence: 395 new leaves across configuration, HTTP/input/authentication, payment lifecycle/retry/recovery, PostgreSQL invariants/concurrency, real SDK/local HTTP transport, and connected application tests. The one MODIFY targets `internal/config::TestLoadValidConfiguration` and requires the serving read default to be 11 seconds.
- The suite has payment-local fixtures/fakes without SDK/HTTP dependencies, shared test-owned PostgreSQL/HTTP/log/barrier helpers, and five runner-validated missing-declaration scaffolds. Scaffolds have default-return bodies and apply only through native post-test overlays.
- Named test families/subcases and package-local helpers provide the authored organization; criterion markers and the plan's mapping locate their behavioral contracts. This checkpoint makes no organization or assertion edits.
- Dependency pins: Stripe Go `v87.0.0`, compatible API `2026-09-30.endive`, PostgreSQL `18.6-alpine`, Go `1.27.2`. `go.mod`/`go.sum`, fixture snapshot version, and the SDK assertion contain these pins. Automated correctness tests use local Stripe HTTP fixtures and require no live Stripe calls or credentials.

## Executed native gates

All commands run from the application root with the configured script directory. `snapshot-tests.sh` SHA256 is `180b0d66d72d3f0cb956298ecfb5655ad1b7e7e0470041a375c11ffcacc33663`, matching the approved `fix/behavior-evidence-contracts@638047e7d671b86a061516dad300200fa0ade032` runtime.

| Check | Actual result |
| --- | --- |
| `snapshot-tests.sh validate-plan` | PASS; all 66 entries validate against the original baseline; native lock contains 1 MODIFY and 65 TOUCH. |
| `snapshot-tests.sh capture post-test` | Complete uncached `go test ./...` with native scaffold overlays: 461 leaves, 65 passed, 396 failed, 0 pending. Successful native capture establishes complete source/runtime inventories without build/collection failure or missing child outcomes. |
| `snapshot-tests.sh compare post-test` | PASS; 395 new conforming failed leaves, 0 removals, 1 expected status change, 65 unchanged passed outcomes, 65 allowlisted digest changes. 66 plan entries; 0 appended entries. No unused MODIFY warnings. |
| Native implementation targets | Exactly 396 unique targets: all 395 new leaves plus `github.com/filser89/stripe-payments-go/internal/config::TestLoadValidConfiguration`. Every target currently fails; all must pass against actual production code at implementation completion. |
| Native scaffold manifest | Five records; records match the post-test snapshot and every current scaffold SHA256. |
| `banned-patterns.sh` | PASS; 34 test files scanned, supported Go source structures and removal markers valid; RSpec matcher bans inapplicable. |
| `session-lock.sh check-dirty test` | PASS; checkpoint changes are shared feature evidence/report paths. |
| `gofmt -l cmd internal`; `git diff --check` | PASS; no reported formatting or whitespace defects. |
| Original baseline preservation | PASS; retained and committed baseline SHA256 match; all baseline identities remain present. |

The 395 new failed assertions and one configuration MODIFY are expected missing-behavior evidence. The 65 TOUCH leaves retain passed outcomes. Native PASS is a test-session checkpoint, not proof that checkout behavior is implemented or that a deliberately incorrect implementation cannot satisfy the suite.

| Native artifact | SHA256 |
| --- | --- |
| `snapshots/post-test.json` | `cdfd26c48132100176983b4e71a57201e72e7caf69ecec213cf5cf01deac4d25` |
| `snapshots/test-plan.lock.json` | `97dd44d7dd0e1706fefd509b68f4c84af6a40b335010e93ca6b30fd6936cbb93` |
| `snapshots/scaffolds.json` | `2fefab560749d199a47044f923b565fb475dc2e52ec60efb3de4f29887ddd34e` |

## Pending runtime evidence and deliverables

The six procedures in `test-plan.md` remain the implementation-verification contract. Limited current inspection confirms `.env` is ignored, dependency pins are exact, PostgreSQL fixtures use normal application migrations, and `.github/workflows/verify.yml` invokes the same `make verify` that local verification uses. These observations do not complete the implementation-dependent procedures.

| Procedure | Pending implementation check and expected result |
| --- | --- |
| NE-1 | Inspect serving configuration/composition and resolved Compose, then exercise normal startup and migration/dependency failures. Checkout settings reach real serving, migrate/probe remain independent, serving timeouts are 11s/15s, and migration failure prevents app startup. |
| NE-2 | Inspect production ID/key generation, tracked config/browser assets, and snapshot/log/history allowlists. IDs/keys are independent random identities and sensitive data is absent from tracked files, responses, browser assets, Stripe metadata, URLs, logs and history. |
| NE-3 | Inspect and exercise documented lifecycle/replay/status/history/continuation against a reproducible local harness. Creation/browser return never confirms paid; acceptance/409/202 boundaries, 23h retry cutoff, 23h59m expiry and 15m advisory are exact, with no manual override or future recovery worker. |
| NE-4 | Inspect the real SDK/backend and persisted dispatch snapshots; collect local wire/timing evidence. Exact pins, hosted_page, disabled SDK retries, immutable version/key/parameters, combined attempt budgets and propagated cancellation are preserved. |
| NE-5 | Inspect versioned migrations, parameterized SQL and repository guards; run generation and inspect real DB fault/activity evidence. Generated code matches its source, history/state are atomic, stale/paid guards hold, and transactions do not span external calls. |
| NE-6 | Run `make verify` and local startup/probe/shutdown/dependency recovery. Formatting, static analysis, vulnerability checks, all unit/integration tests and race detection must pass locally/CI; checkout work exits within budgets and logs remain sanitized/correlated. |

`make verify`, static analysis, vulnerability checks, real implementation race detection, normal production builds, and acceptance execution are not claimed complete at this intentionally red test checkpoint. Implementation-dependent assertions behind absent boundary declarations remain red; native complete registration does not prove later assertions execute successfully against real code.

DO-001 (API usage), DO-002 (configuration/local setup), DO-003 (lifecycle/recovery documentation), and DO-006 (migrations, SQL generation and architecture documentation) remain implementation deliverables. DO-004 has authored automated evidence and exact dependency pins; adequacy requires independent test review and successful execution against real implementation. DO-005 requires the complete local/CI verification results. No Delivery Obligation is inferred complete from native red-session gates.

## Handoff

Ready for fresh `/kaba:review-tests` inspection of the suite, plan and native evidence. Independent approval and a commit-pinned freeze of tests and execution configuration are still required before plan-code/implement-code. The test lock remains armed; all 396 native targets, the preserved baseline, and the five native scaffold records are part of the handoff.
