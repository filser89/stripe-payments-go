# Test Review: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Reviewed**: 2026-10-09 | **Verdict**: NO-GO (advisory) | **Scope**: CFG-001, CFG-002, HTTP-003, SEC-001

## Summary

- Criteria reviewed: 4 / 22 (scoped: CFG-001, CFG-002, HTTP-003, SEC-001), covering 28 required variants and their source dependencies.
- Findings: 1 critical, 0 high, 0 medium, 0 low.
- Verdict: NO-GO — invalid-credential startup can accept requests silently without failing the listener assertion.
- Review method: static inspection of specification, criteria, plan, test bodies, helpers, and relevant production boundaries. The suite was not run.

## Findings

| ID | Criterion | Location (file:block) | Severity | Weakness | Counterexample / evidence | Recommendation |
|----|-----------|-----------------------|----------|----------|--------------------------|----------------|
| R5 | CFG-001 | `cmd/service/authentication_test.go:213` > `TestAuthenticationServingConfiguration`, lines 201–229 | CRITICAL | The invalid-credential process is dialed only if its output contains `service listening`; the test then requires that message to be absent. A process that silently accepts requests before reporting the sanitized credential error leaves `accepted` false and passes every startup assertion. Acceptance Scenario 1 and FR-001 require rejection before requests are accepted. | Passing incorrect branch after common configuration loading and credential validation: `if credentialErr != nil { ln, err := net.Listen("tcp", c.ListenAddr); if err == nil { go func() { _ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })) }(); time.Sleep(100*time.Millisecond); _ = ln.Close() }; return credentialErr }`. The real entry point emits only the sanitized invalid-setting diagnostic and exits normally with a nonzero code within the test's 500 ms deadline. No `service listening` message is emitted, so the observer never attempts a connection while the temporary server accepts requests. | Observe the invalid process's configured address independently of log contents throughout startup, and retain the sanitized diagnostic, absent success message, and normal nonzero-exit assertions. A bounded direct connection/request witness must expose silent serving before credential rejection. |

## Strength Summary

- `TestAuthenticationMissingCredentials`, `TestAuthenticationExactCredentials`, and `TestAuthenticationAuthorizationParsing` meaningfully constrain handler invocation and unread rejected bodies alongside challenge/status assertions. Missing credentials after success, case sensitivity, significant password spaces, duplicate field ordering, and the valid 4096-byte versus invalid 4097-byte header boundary are explicit witnesses.
- `TestAuthenticationCredentialSources` checks query, cookie, and body alternatives without permitting body consumption. `TestAuthenticationDelegation` observes method, path, repeated query values, request body, exact invocation count, and successful/error handler results.
- `TestAuthenticationConcurrentIsolation` coordinates an admitted valid request with rejected callers using channels, checks each outcome and body reads, and verifies the exact invocation count. Race execution is planned and its reported red-suite result is distinguished from a green feature verification result.
- `TestAuthenticationLandingBodyBytes` and `TestAuthenticationBodyReadFailure` distinguish declared length, actual bytes, byte-empty EOF, and zero-byte failures.
- `TestAuthenticationRealStreamingBodies` sends real chunked requests. `TestAuthenticationStalledBodyDeadline` distinguishes a server-side bounded failure from a client timeout. `TestAuthenticationFoundationTransport` exercises incomplete headers, independent body reads, deterministic blocked writes through `net.Pipe`, idle expiry, and malformed/oversized transport input.
- `TestAuthenticationPublicProbeBoundary` checks exact public probes, method rejection, readiness failure/recovery, prefix protection, and absence of protected execution. Existing authenticated drain/cancellation, cleanup, serve-failure, PostgreSQL outage/restart, retained-data, migration, and active-query witnesses retain substantive foundation assertions.
- Serving configuration exercises independent accepted/rejected credential bounds and actual invalid-config process termination. `TestAuthenticationCredentialLifetime` checks both running-process capture and restarted credential rotation.
- All specification behavioral groups and FR-001–FR-011 have criteria/evidence mappings. SEC-002 source/runtime inspection and BRW-001 browser checks have concrete procedures and expected outcomes; their implementation-dependent pending status is legitimate. DO-001–DO-004 separately cover setup/secrets, usage/lifecycle documentation, local security boundaries, and complete project/race verification. No missing delivery completion procedure is identified.


## Next Actions

- Use `/kaba:fix-tests` to make CFG-001's startup listener observation independent of log output.
- Re-review CFG-001 before the human review gate. The human owns the final go/no-go decision; implementation is not the automatic next step.
