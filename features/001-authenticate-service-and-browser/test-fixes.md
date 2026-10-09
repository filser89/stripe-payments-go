# Test Fixes: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Review source**: `test-review.md` | **Correction review**: GO (advisory)

## Summary

- Findings processed: 1 / 1.
- Mechanical fixes applied: 1 (R1).
- Escalated and resolved: 0.
- Skipped: 0.
- Snapshot compare: FAIL — post-test landing contract conflicts with implemented feature behavior.
- Banned pattern scan: PASS.
- Independent re-review: GO (advisory), zero findings.

## Mechanical Fixes

### R1 — CFG-001; SEC-001 V1: Bounded startup observation

**File**: `cmd/service/authentication_test.go:192`
**Status**: FIXED; independent scoped review is GO (advisory), zero findings.
**Current contract**: Invalid-account subprocess execution and listener observation share a single 3-second context deadline. The budget includes executable launch and aggregate-suite scheduling; it is a test hang guard, not a specified startup latency. Measured normal launches of 490–714 ms fit with margin. A deadline produces an explicit test failure, joins the process goroutine, and subjects the result to normal-exit and diagnostic assertions.

Current deadline setup:

```go
// This observation budget includes executable launch and suite scheduling;
// it is a test hang guard, not a startup performance requirement.
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
```

The listener-observation select uses the same context:

```go
case <-ctx.Done():
    // CommandContext kills overdue execution. Join the owned goroutine
    // and keep checking the result: a kill is not normal rejection.
    runErr = <-done
    t.Errorf("invalid serving configuration did not terminate within observation budget: %s", tc.name)
    break observeStartup
```

The observer ticker is stopped and the context canceled after completion. There is no independent observation timer. The witness requires every CFG-001 V1–V9 variant; listener nonacceptance through independent 5 ms probes with 20 ms TCP attempts; absence of `service listening`; `ExitError.Exited()`; nonzero exit; the expected invalid setting and exclusion of the other setting; valid structured diagnostic decoding; and raw, normalized, and encoded secret exclusion.

No behavioral assertion, criterion marker, registration, identity, or helper contract is exempted. Polling remains a bounded witness rather than proof against arbitrarily brief listeners. The repository diff supplies the exact code audit; this report describes current test and validation state under the project's current-state documentation rule.

## Resolved Escalations

None.

## Skipped

None.

## Validation

| Check | Result |
|---|---|
| `go test -count=1 -timeout=3m ./cmd/service -run '^TestAuthenticationServingConfiguration$'` | PASS; package reports 5.328 s |
| `snapshot-tests.sh capture post-test` | PASS; aggregate configured runner; 57 passed, 0 failed, 0 pending; real code, no overlays |
| `snapshot-tests.sh compare post-test` | FAIL; 15 new tests pass where the contract requires failed |
| `banned-patterns.sh cmd/service/authentication_test.go` | PASS; 7 test files inspected; RSpec matcher bans inapplicable |
| `go test -race -count=1 -timeout=3m ./cmd/service -run '^TestAuthenticationServingConfiguration$'` | PASS; package reports 6.468 s |
| `.tools/bin/golangci-lint run --timeout=5m ./cmd/service/...` | PASS; 0 issues |
| `gofmt -l cmd/service/authentication_test.go` | PASS; no output |
| `git diff --check` | PASS |
| `session-lock.sh check-dirty test` | PASS |

Go inventory, execution, and lint use sandbox escalation for existing module/build caches and local Docker infrastructure. Restricted inventory attempts fail on cache permissions; the successful results use escalated executions.

Snapshot comparison preserves all 38 baseline passing outcomes, reports no removals or status changes, and accepts all 11 allowlisted content drifts. Four planned PINs pass. Allowlist: 15 entries, 0 appended in this session. Neither plan artifact nor its lock is amended. Generated `snapshots/post-test.json` contains 57 passing outcomes; its comparison fails and `implementation_required` is absent. It cannot serve as a validated implementation handoff snapshot.

Full `make verify`, vulnerability checks, full-suite race detection, and post-implementation validation are not run in this correction session. Focused race and lint results do not establish those delivery gates.

## ESCALATION — test-plan defect

- **Criterion**: CFG-001 and SEC-001 startup evidence; the landing conflict affects 15 new outcomes across the implemented feature.
- **Requires**: A phase-compatible snapshot contract for corrections to implemented behavior. Comparator errors state `new test should be failed, got passed`.
- **Conflict**: The post-test gate accepts new green outcomes only through approved PIN entries. These 15 outcomes exercise implemented behavior and pass real assertions. The plan contains four different valid PINs. Artificial red failures, weaker assertions, renaming to match unrelated PINs, manual snapshot edits, and unapproved plan amendments are invalid corrections.
- **Amendment needed**: Resolve through the planning/workflow owner using `/kaba:plan-tests` or an approved phase-aware correction workflow. Preserve behavioral requirements and valid implementation targets. This session cannot certify the snapshot chain.

Affected runtime identities:

| Package | Test |
|---|---|
| `cmd/service` | `TestAuthenticationCredentialLifetime`, `TestAuthenticationServingConfiguration` |
| `internal/web` | `TestAuthenticationAuthorizationParsing`, `TestAuthenticationBodyReadFailure`, `TestAuthenticationConcurrentIsolation`, `TestAuthenticationCredentialSources`, `TestAuthenticationExactCredentials`, `TestAuthenticationLanding`, `TestAuthenticationLandingBodyBytes`, `TestAuthenticationMissingCredentials`, `TestAuthenticationPublicProbeBoundary`, `TestAuthenticationRealStreamingBodies`, `TestAuthenticationRouting`, `TestAuthenticationSanitizedOutcomes`, `TestAuthenticationStalledBodyDeadline` |

## Review Scope and Coverage

Correction path: `cmd/service/authentication_test.go`, named test `TestAuthenticationServingConfiguration`, CFG-001 and SEC-001 V1. Test layout, credential variants, listener witness, diagnostic checks, and other coverage remain intact. `test-review.md` retains the independent review's authoritative verdict.

Independent scoped review of CFG-001 and SEC-001 is GO (advisory), with zero findings in the merged report. The snapshot gate requires resolution before implementation completion; this review does not certify the snapshot chain or full project verification.
