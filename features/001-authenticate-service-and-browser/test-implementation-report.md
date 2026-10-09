# Authentication test implementation

## Summary

- Test files: 3 new, 3 modified, 0 existing-only (6 total in the plan).
- Factories/fixtures: 2 new, 0 modified, 0 existing-only (2 planned fixture groups, located in test files).
- Shared helpers: 0 new, 1 modified, 0 existing-only (1 total in the plan).
- Behavioral criteria mapped: 22 / 22 unique IDs, including explicit pending inspection and browser evidence.
- Required outcomes/variants with automated evidence: 101 / 101 planned automated variants. Seven source-inspection/browser variants remain pending implementation; BRW-001 V2–V3 also have supporting automated assertions.
- Snapshot comparison: PASS.
- Allowlist warnings: none.
- Quality check: PASS for runner registration/removal rules and banned patterns.
- Implementation code and dependencies: unchanged.
- Session: test lock armed; ready for `/kaba:review-tests`.

The baseline contains 38 passing tests. The post-test snapshot contains 57 tests: 42 passing, 15 failing, and none pending. All 38 existing tests pass. The 19 new tests comprise 15 expected behavioral failures and four passing PIN checks. The 11 planned TOUCH entries preserve their baseline outcomes.

## Test evidence

| File | Criteria and outcomes |
| --- | --- |
| `cmd/service/authentication_test.go` | CFG-001 V1–V9: supported credential lengths/characters, independent missing/invalid settings, real serving requests, actual service-process rejection/exit status, sanitized diagnostics. CFG-002 V1–V6: real probe success/failure/unavailability and migrations with absent/invalid Basic settings, preserving other configuration requirements. CFG-003 V1–V2: original credentials during execution and replacement after restart. SEC-001 V1: startup diagnostics. |
| `internal/web/authentication_test.go` | AUTH-001 V1–V4, AUTH-002 V1–V7, AUTH-003 V1–V12, AUTH-004 V1–V4, AUTH-005 V1–V4, AUTH-006 V1–V3; HTTP-001 V1–V4, HTTP-002 V1–V4, HTTP-003 V1–V5, HTTP-004 V1–V4; FND-001 V1–V5 and FND-002 V1; SEC-001 V2–V8. BRW-001 V2–V3 have supporting per-request and cookie assertions. |
| `internal/web/authentication_transport_test.go` | HTTP-005 V1–V3: real empty/nonempty chunked bodies. HTTP-006 V1–V3: actual stalled landing stream and server read deadline, distinguishing connection failure from an operating response. FND-004 V1–V5: incomplete headers, stalled body, blocked writes, idle keepalive expiry, and malformed/oversized headers. These also support HTTP-004 V4. |
| `internal/web/server_test.go` | FND-001/FND-002 foundation evidence with valid serving settings; authenticated work in drain, cancellation, and panic witnesses. Existing resource-cleanup and serving-failure assertions are preserved. |
| `internal/integration/foundation_test.go` | FND-003 V1–V4: real PostgreSQL outage/restart/readiness, retained data and migrations, authenticated active-query cancellation before pool cleanup, bounded unavailable startup. Direct query/migration and unavailable-open witnesses are unchanged. |
| `cmd/service/main_test.go` | Valid Basic fixtures isolate the existing missing-DATABASE_URL diagnostic, supporting CFG-001 and SEC-001. Other command test bodies remain unchanged. |

Four exact PIN identities describe preserved behavior:

- `TestAuthenticationCommandIndependence`
- `TestAuthenticationFoundationTransport`
- `TestAuthenticationShutdownAdmission`
- `TestAuthenticationDelegation`

## Verification

| Check | Result |
| --- | --- |
| `snapshot-tests.sh capture baseline` | 38 passed; complete runtime inventory. |
| `snapshot-tests.sh validate-plan` | PASS; 15 entries and matching plan lock. |
| `snapshot-tests.sh capture post-test` | Complete inventory: 57 tests, 42 passed, 15 failed, no pending cases. No compilation scaffold. |
| `snapshot-tests.sh compare post-test` | PASS; existing outcomes/identities preserved and all new outcomes conform. |
| `banned-patterns.sh --json` | PASS; 7 test files inspected, no violations. |
| `gofmt` | All six planned test files formatted. |
| `.tools/bin/golangci-lint run --timeout=5m ./...` | PASS; 0 issues. |
| `.tools/bin/govulncheck ./...` | PASS; no vulnerabilities found. |
| `go test -race -count=1 -timeout=3m ./...` | Executes all packages; exits nonzero for the same 15 intentional behavioral failures. No data-race diagnostics. Configuration and PostgreSQL integration packages pass. This is not a green race-suite result. |
| `session-lock.sh check-dirty test` | PASS. |
| `make verify` | Pending implementation: the aggregate suite must be green before feature completion. |

Go execution uses a writable temporary build cache. Local HTTP listeners and PostgreSQL 18.6 test containers require execution outside the restricted filesystem/network sandbox. Automated evidence uses synthetic credentials and local infrastructure, with no live Stripe requests.

## Organization and runner constraints

Variants use explicit rows/loops inside the planned named functions, retaining complete assertions and criterion markers. Existing runtime identities and all PIN descriptions are preserved. Command tests build a temporary executable to verify the actual process exit for invalid serving settings. No production declarations or compilation scaffolds are introduced.

The command log helper accessor is `contents()`. Kaba 0.3.0 conservatively links same-named test-support methods to selector uses; a helper named `String()` would add a false dependency to existing `bytes.Buffer.String()` calls. No extra TOUCH entries are needed for those unchanged tests.

## Non-automated runtime evidence

### SEC-002 — source and runtime secret boundaries

Current source inspection covers `cmd/service/main.go`, `internal/config/config.go`, `internal/web/server.go`, and the production package inventory. There is no implemented authentication path, credential comparison, landing/browser asset, or Stripe request construction in these paths. Authentication-specific conclusions remain pending; the current absence of Stripe integration is not proof about future authentication code.

After implementation:

1. Fetch authenticated `GET /` with distinctive synthetic credentials. Inspect returned HTML and any linked browser assets for configured/submitted secrets and privileged tokens. Expected: service identification without secrets, forms, payment controls, or application sessions.
2. Trace the authentication dependencies and outgoing-request construction. Expected: no authentication Stripe call or local application credential forwarded to Stripe.
3. Inspect the production username/password comparison path. Expected: comparisons avoid content-dependent early exit; do not introduce timing-sensitive acceptance thresholds.

### BRW-001 — native browser journey

Pending implementation. Use a fresh local browser context, visit `/`, observe the native Basic challenge, enter a synthetic valid pair, and inspect the successful landing response and cookies. Independently issue a later credential-free request. Expected: native challenge access; later credential-free rejection; no custom login/logout or application session cookies. Browser credential caching/reuse controls prompting; repeated prompts are not promised.

## Delivery obligations

These are separate completion checks, not conclusions established by the test snapshot:

- **DO-001:** Inspect empty `.env.example` Basic placeholders, ignored/untracked secret files, Compose wiring, build inputs and image artifacts. Launch the local service with synthetic credentials and verify credential exclusion from diagnostics and browser responses.
- **DO-002:** Verify documented service/browser requests, restart rotation, and browser caching/logout limitations; perform the native-browser smoke check.
- **DO-003:** Inspect current documentation for Basic transport limitations, dedicated local credentials, local-only/TLS boundary, future payment-route protection, and independent webhook signature verification. Do not describe undelivered routes as available.
- **DO-004:** After implementation, run `make verify` and affected race checks using local HTTP/PostgreSQL, report actual results, and complete the pending runtime inspections.
