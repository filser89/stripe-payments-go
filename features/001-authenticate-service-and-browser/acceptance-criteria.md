# Acceptance Criteria: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser`
**Spec**: [spec.md](spec.md)
**Created**: 2026-10-09
**Status**: Draft

## Summary

- Total criteria: 101
- New: 90 | Modify: 11 | Exists: 0
- Categories: Authentication configuration; Credential enforcement and browser authentication; Auth-check response and input contract; Secret handling and request observability; Foundation compatibility; Local usage and integration boundaries

Category breakdown:

| Category | New | Modify | Exists | Total |
| --- | ---: | ---: | ---: | ---: |
| Authentication configuration | 17 | 0 | 0 | 17 |
| Credential enforcement and browser authentication | 23 | 0 | 0 | 23 |
| Auth-check response and input contract | 25 | 0 | 0 | 25 |
| Secret handling and request observability | 12 | 0 | 0 | 12 |
| Foundation compatibility | 5 | 11 | 0 | 16 |
| Local usage and integration boundaries | 8 | 0 | 0 | 8 |

Source notation: `AS1`–`AS8` reference numbered items in **Acceptance Scenarios**. `EC1`–`EC10` reference bullets in **Edge Cases**, in document order. `SC1`–`SC8` reference bullets in **Success Criteria**, in document order. `FR-001`–`FR-012` retain the specification identifiers.

Ownership: the configured Go test paths are `**/*_test.go`, `**/testdata/**`, and `internal/testutil/**`. Feature artifacts plus `go.mod`/`go.sum` are shared paths. There is no single test directory. The path classifier identifies `internal/config/config_test.go` as test-owned. The configured rules are `AGENTS.md` and `FEATURE-QUALITY-STANDARDS.md`; no `CLAUDE.md` is present.

Existing test inventory: `cmd/service/main_test.go`, `internal/config/config_test.go`, `internal/web/server_test.go`, and `internal/integration/foundation_test.go`. Supporting fixtures include `internal/integration/testdata/` and `scripts/testdata/workflows.sh`. Existing web/integration fixtures omit authentication values, so foundation criteria needing those fixtures are `MODIFY`. New assertions are `NEW` even when the test writer can place them in an existing file. Status denotes coverage work, not an executed test result.

Go test planning must use runner-discovered identities, supported named tests/subtests or literal table rows, and criterion markers beside the assertions they cover. Shared test-helper edits require planning the affected test inventory. This document assigns no test identities, scaffolds, production interfaces, or implementation structure.

## Criteria

### Authentication configuration

#### CFG-001: Configured username
- **Status**: NEW
- **Source**: FR-001; FR-005
- **Assertion**: The protected-request username comes from `AUTH_USERNAME`.
- **Notes**: Use distinct environment values to detect a hard-coded username.

#### CFG-002: Missing username
- **Status**: NEW
- **Source**: AS4; FR-005; EC2
- **Assertion**: Serving startup rejects an unset or empty `AUTH_USERNAME`.
- **Notes**: Supply otherwise valid configuration; no default username is permitted.

#### CFG-003: Missing password
- **Status**: NEW
- **Source**: AS4; FR-005; EC2
- **Assertion**: Serving startup rejects an unset or empty `AUTH_PASSWORD`.
- **Notes**: Supply otherwise valid configuration; no default password is permitted.

#### CFG-004: Invalid UTF-8
- **Status**: NEW
- **Source**: AS4; FR-005
- **Assertion**: Serving startup rejects invalid UTF-8 in either configured credential.
- **Notes**: Exercise each field independently.

#### CFG-005: ASCII controls
- **Status**: NEW
- **Source**: AS4; FR-005
- **Assertion**: Serving startup rejects any ASCII control character U+0000–U+001F or U+007F in either configured credential.
- **Notes**: Cover both fields. NUL cannot be supplied through a process environment; verify that boundary through configuration input.

#### CFG-006: Username colon
- **Status**: NEW
- **Source**: AS4; FR-005; EC6
- **Assertion**: Serving startup rejects a colon in the configured username.

#### CFG-007: Password colon
- **Status**: NEW
- **Source**: FR-005; EC6
- **Assertion**: Authentication configuration accepts a password containing a colon.

#### CFG-008: Whitespace preservation
- **Status**: NEW
- **Source**: FR-005; EC6
- **Assertion**: Authentication configuration preserves whitespace in each credential.
- **Notes**: Include leading/trailing spaces and nonempty all-space values; control characters remain invalid under CFG-005.

#### CFG-009: UTF-8 acceptance
- **Status**: NEW
- **Source**: FR-005
- **Assertion**: Authentication configuration accepts non-ASCII UTF-8 credentials that satisfy the stated syntax constraints.

#### CFG-010: Short password
- **Status**: NEW
- **Source**: AS4; FR-005; FR-010; SC3
- **Assertion**: Authentication configuration accepts the password `123`.

#### CFG-011: No extra length or strength policy
- **Status**: NEW
- **Source**: FR-005; SC3
- **Assertion**: Authentication configuration accepts syntactically valid nonempty credentials without an application-level length or complexity restriction.
- **Notes**: Include a one-character password and long credentials; HTTP header limits apply separately under HTTP-023.

#### CFG-012: Fail-closed startup
- **Status**: NEW
- **Source**: AS4; FR-005; EC2
- **Assertion**: Invalid authentication configuration prevents the application from serving protected requests.
- **Notes**: Exercise actual serving startup with otherwise valid configuration, rather than only a configuration validator.

#### CFG-013: Useful diagnostic
- **Status**: NEW
- **Source**: AS4; FR-005
- **Assertion**: An authentication configuration error identifies the invalid setting or constraint.
- **Notes**: Do not prescribe exact wording; credential-value exclusion is SEC-004.

#### CFG-014: Migrations without HTTP credentials
- **Status**: NEW
- **Source**: FR-002; FR-005
- **Assertion**: The migration command can execute with absent or invalid HTTP authentication configuration.
- **Notes**: Use valid database configuration; cover missing values and protocol-invalid values independently.

#### CFG-015: Probes without HTTP credentials
- **Status**: NEW
- **Source**: FR-002; FR-005; EC8
- **Assertion**: The probe command can execute with absent or invalid HTTP authentication configuration.
- **Notes**: Check successful readiness and existing failure behavior; the probe must not need an Authorization header.

#### CFG-016: No HTTP authorization of internal commands
- **Status**: NEW
- **Source**: FR-002; Out of Scope
- **Assertion**: Internal command execution requires no incoming HTTP authentication exchange.
- **Notes**: Serving still requires valid authentication configuration; starting a process is not a protected HTTP request.

#### CFG-017: Configured password
- **Status**: NEW
- **Source**: FR-001; FR-005
- **Assertion**: The protected-request password comes from `AUTH_PASSWORD`.
- **Notes**: Use distinct environment values to detect a hard-coded password.

### Credential enforcement and browser authentication

#### AUTH-001: Matching environment credentials
- **Status**: NEW
- **Source**: AS1; FR-001; FR-004; FR-010; SC1
- **Assertion**: A request with the configured pair reaches `/auth-check` through the connected configuration-to-HTTP path.
- **Notes**: Use the actual application entry point; HTTP-001 through HTTP-003 specify the success response.

#### AUTH-002: Missing credentials
- **Status**: NEW
- **Source**: AS2; FR-006; EC1
- **Assertion**: A request without an Authorization header receives `401 Unauthorized`.

#### AUTH-003: Wrong username
- **Status**: NEW
- **Source**: AS2; FR-006; EC1
- **Assertion**: A request with an incorrect username receives `401 Unauthorized`.

#### AUTH-004: Wrong password
- **Status**: NEW
- **Source**: AS2; FR-006; EC1
- **Assertion**: A request with an incorrect password receives `401 Unauthorized`.

#### AUTH-005: Malformed credentials
- **Status**: NEW
- **Source**: AS2; FR-006; EC4
- **Assertion**: A request with malformed Basic credentials receives `401 Unauthorized`.
- **Notes**: Cover an empty header, missing encoded credentials, malformed Base64, and decoded credentials without a colon.

#### AUTH-006: Duplicate headers
- **Status**: NEW
- **Source**: AS2; FR-006; EC4
- **Assertion**: A request with multiple Authorization headers receives `401 Unauthorized`.
- **Notes**: Include two valid identical headers and valid/invalid headers in both orders.

#### AUTH-007: Unsupported scheme
- **Status**: NEW
- **Source**: AS2; FR-006; EC4
- **Assertion**: A request using an authentication scheme other than Basic receives `401 Unauthorized`.

#### AUTH-008: Basic challenge
- **Status**: NEW
- **Source**: AS2; AS3; FR-003; FR-006; SC2
- **Assertion**: Every authentication rejection includes `WWW-Authenticate: Basic realm="stripe-payments-go"`.
- **Notes**: Apply to each rejection family in AUTH-002 through AUTH-007; use the same generic challenge for browser requests.

#### AUTH-009: Generic authentication error
- **Status**: NEW
- **Source**: AS2; FR-006; EC4
- **Assertion**: Authentication rejection uses the same generic error regardless of the rejected credential defect.
- **Notes**: No exact error body is specified; distinguish generic wording from the secret-exclusion assertions.

#### AUTH-010: Protected behavior not invoked
- **Status**: NEW
- **Source**: AS2; FR-002; FR-004; SC1
- **Assertion**: An unauthorized request never invokes protected behavior.
- **Notes**: Verify middleware enforcement as well as the real `/auth-check` route; a handler that runs then hides its response is incorrect.

#### AUTH-011: Protected content withheld
- **Status**: NEW
- **Source**: AS2; FR-002; SC1
- **Assertion**: An unauthorized response contains no protected success content.

#### AUTH-012: Query credentials ignored
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: Credentials supplied only in a query string do not authenticate a request.

#### AUTH-013: Body credentials ignored
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: Credentials supplied only in a request body do not authenticate a request.

#### AUTH-014: Per-request enforcement
- **Status**: NEW
- **Source**: FR-002
- **Assertion**: Every protected request is authenticated independently of earlier successful requests.
- **Notes**: Include successive requests from the same client or connection; no application session may authorize a later credential-free request.

#### AUTH-015: Case-sensitive credentials
- **Status**: NEW
- **Source**: FR-005; EC6
- **Assertion**: Changing the case of either submitted credential prevents authentication.
- **Notes**: Exercise username and password independently.

#### AUTH-016: Exact whitespace comparison
- **Status**: NEW
- **Source**: FR-005; EC6
- **Assertion**: Changing whitespace in either submitted credential prevents authentication.
- **Notes**: A matching pair with preserved spaces must succeed under AUTH-001; include all-space configured values.

#### AUTH-017: No Unicode normalization
- **Status**: NEW
- **Source**: FR-005
- **Assertion**: A canonically equivalent but byte-distinct Unicode credential does not authenticate.
- **Notes**: Include a matching non-ASCII UTF-8 control case.

#### AUTH-018: Case-insensitive scheme
- **Status**: NEW
- **Source**: EC6
- **Assertion**: A case variant of the Basic scheme name authenticates when the credential pair matches.

#### AUTH-019: Colon-containing password
- **Status**: NEW
- **Source**: FR-005; EC6
- **Assertion**: A matching password containing a colon authenticates successfully.

#### AUTH-020: Short password through HTTP
- **Status**: NEW
- **Source**: FR-005; FR-010; SC3
- **Assertion**: The configured password `123` authenticates a matching request through the actual configuration-to-HTTP path.

#### AUTH-021: Browser completion
- **Status**: NEW
- **Source**: AS3; FR-003; SC2
- **Assertion**: A browser supplying the configured pair displays the fixed plain-text success response.
- **Notes**: The built-in prompt is enabled by AUTH-008; no custom login page or application session is part of this feature. Browser rendering may require a documented manual check.

#### AUTH-022: Reusable authentication
- **Status**: NEW
- **Source**: FR-004; FR-006
- **Assertion**: A protected operation other than `/auth-check` can use the same authentication enforcement.
- **Notes**: Demonstrate with test-owned protected behavior; do not introduce a payment route or choose a new production API here.

#### AUTH-023: Endpoint body policy stays local
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: Authentication middleware permits a nonempty body on an otherwise valid authenticated request to protected behavior outside `/auth-check`.
- **Notes**: Use test-owned behavior with an explicit body allowance; this is not a payment schema.

### Auth-check response and input contract

#### HTTP-001: GET success status
- **Status**: NEW
- **Source**: AS1; FR-006; SC1
- **Assertion**: An authenticated bodyless `GET /auth-check` without a query returns `200 OK`.

#### HTTP-002: GET success media type
- **Status**: NEW
- **Source**: AS1; FR-006
- **Assertion**: A successful `GET /auth-check` returns `Content-Type: text/plain; charset=utf-8`.

#### HTTP-003: GET success body
- **Status**: NEW
- **Source**: AS1; FR-006
- **Assertion**: A successful `GET /auth-check` returns exactly `Authenticated\n` as its body.
- **Notes**: The final character is a newline, not the two literal characters backslash and n.

#### HTTP-004: HEAD success
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: An authenticated bodyless `HEAD /auth-check` without a query returns `200 OK`.

#### HTTP-005: HEAD status parity
- **Status**: NEW
- **Source**: AS5; FR-006
- **Assertion**: A `HEAD /auth-check` request receives the same status as the equivalent GET request for every applicable authentication or input outcome.
- **Notes**: Include unauthorized, nonempty query, nonempty body, and body-read failure cases.

#### HTTP-006: HEAD header parity
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A `HEAD /auth-check` response has the same contract-required headers as the equivalent GET response.
- **Notes**: Compare relevant values, not per-request correlation IDs; include content type, challenge, and cache policy where applicable.

#### HTTP-007: HEAD body suppression
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A `HEAD /auth-check` response has no response body for any outcome.
- **Notes**: Verify actual HTTP behavior for successful and rejected requests.

#### HTTP-008: Unsupported methods
- **Status**: NEW
- **Source**: AS5; FR-006
- **Assertion**: An authenticated request using a method other than GET or HEAD receives `405 Method Not Allowed`.
- **Notes**: Cover common mutation methods plus OPTIONS and a valid extension method; no method-specific bypass is permitted.

#### HTTP-009: Allow header
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A method rejection includes `Allow: GET, HEAD`.

#### HTTP-010: Nonempty query
- **Status**: NEW
- **Source**: AS5; FR-006
- **Assertion**: An authenticated GET or HEAD with a nonempty query string receives `400 Bad Request`.
- **Notes**: Include unknown keys, credential-shaped keys, and raw nonempty queries without an equals sign.

#### HTTP-011: Known nonempty body
- **Status**: NEW
- **Source**: AS5; FR-006; EC3
- **Assertion**: An authenticated GET or HEAD with a nonempty known-length body receives `413 Content Too Large`.
- **Notes**: No query; include exactly one byte as the first rejected boundary.

#### HTTP-012: Unknown-length nonempty body
- **Status**: NEW
- **Source**: AS5; FR-006; EC3
- **Assertion**: An authenticated GET or HEAD with a nonempty unknown-length body receives `413 Content Too Large`.

#### HTTP-013: Chunked nonempty body
- **Status**: NEW
- **Source**: AS5; FR-006; EC3
- **Assertion**: An authenticated GET or HEAD with a nonempty chunked body receives `413 Content Too Large`.
- **Notes**: Exercise wire-level chunked encoding rather than only a declared length.

#### HTTP-014: Empty streamed body
- **Status**: NEW
- **Source**: FR-006; EC3
- **Assertion**: An authenticated GET or HEAD with an empty streamed body satisfies the zero-byte body limit.
- **Notes**: Include unknown-length and chunked forms without a nonempty query.

#### HTTP-015: Body-read failure status
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A body-read failure returns `400 Bad Request` when the connection still permits a response.
- **Notes**: Use a failure before a body byte is established; no exact error wording is specified.

#### HTTP-016: Body-read failure cannot succeed
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A body-read failure never produces an authentication success response.

#### HTTP-017: Generic body-read error
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A body-read failure response contains only a generic error.
- **Notes**: Check detailed injected error text is absent; SEC-001 also excludes credential values.

#### HTTP-018: Authentication precedes validation
- **Status**: NEW
- **Source**: AS5; FR-006; EC5
- **Assertion**: An unauthorized request reaching `/auth-check` receives `401` regardless of endpoint method, query, or body defects.
- **Notes**: Cross unauthorized credential families with individual and combined defects. HTTP parsing/header-limit rejection and existing shutdown rejection are permitted before authentication.

#### HTTP-019: Method precedes input validation
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: An authenticated unsupported method receives `405` even when its query or body is invalid.

#### HTTP-020: Query precedes body validation
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: An authenticated GET or HEAD with a nonempty query receives `400` even when its body is nonempty.

#### HTTP-021: Rejected input prevents operation
- **Status**: NEW
- **Source**: AS5; FR-010; SC4
- **Assertion**: A request rejected by endpoint input validation does not perform the protected success operation.

#### HTTP-022: No-store responses
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: Every `/auth-check` endpoint response includes `Cache-Control: no-store`.
- **Notes**: Cover 200, 401, 405, 400, and 413, including HEAD. Server-level parsing/header-limit errors or pre-routing shutdown rejection are outside endpoint responses.

#### HTTP-023: Existing header limit
- **Status**: NEW
- **Source**: FR-005; FR-006
- **Assertion**: The HTTP server retains the `http.DefaultMaxHeaderBytes` header limit of 1 MiB.
- **Notes**: Preserve Go's actual parsing behavior; do not invent a stricter credential-length limit or assert an unsupported byte-exact wire cutoff.

#### HTTP-024: Bounded body inspection
- **Status**: NEW
- **Source**: FR-006; EC3
- **Assertion**: Inspection of an incomplete `/auth-check` request body is bounded by the existing HTTP read timeout.
- **Notes**: Use the real HTTP serving path with controlled input; a response is not required after an unusable connection.

#### HTTP-025: Exact endpoint path
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: A distinct path such as `/auth-check/` does not directly execute the `/auth-check` success operation.
- **Notes**: The spec does not prescribe a particular response status for other paths.

### Secret handling and request observability

#### SEC-001: Response data minimization
- **Status**: NEW
- **Source**: AS6; FR-007; EC7; SC5
- **Assertion**: Authentication-related responses contain no configured credentials, submitted credentials, raw authorization values, or unnecessary personal data.
- **Notes**: Inspect success and each rejection outcome, including attempted credentials in query strings or bodies.

#### SEC-002: Log data minimization
- **Status**: NEW
- **Source**: AS6; FR-007; EC7; SC5
- **Assertion**: Authentication-related logs contain no configured credentials, submitted credentials, raw authorization values, or unnecessary personal data.
- **Notes**: Include successful attempts, malformed input, query/body credential attempts, and injected failure text; use distinguishable synthetic sentinels.

#### SEC-003: Stored-data minimization
- **Status**: NEW
- **Source**: FR-007; FR-010; SC5
- **Assertion**: Any stored data affected by authentication excludes credentials or unnecessary personal data.
- **Notes**: Inspect actual affected paths. Do not introduce storage just to test this; absence of a write path is inspection evidence only.

#### SEC-004: Configuration diagnostic sanitization
- **Status**: NEW
- **Source**: AS4; FR-005; EC7
- **Assertion**: Authentication configuration diagnostics contain no credential values.
- **Notes**: Inspect both returned startup errors and emitted startup logs for each invalid-configuration family.

#### SEC-005: Browser-delivered credentials
- **Status**: NEW
- **Source**: AS3; AS6; FR-003; FR-007
- **Assertion**: Browser-delivered content contains no embedded privileged credential values.
- **Notes**: Inspect delivered content; absent future HTML/JavaScript is not proof of security for future browser pages.

#### SEC-006: Example placeholders
- **Status**: NEW
- **Source**: AS6; FR-007; SC5
- **Assertion**: Committed authentication configuration examples contain only placeholder credential values.
- **Notes**: Repository inspection; do not copy real local credentials into evidence.

#### SEC-007: Ignored local secrets
- **Status**: NEW
- **Source**: FR-007
- **Assertion**: Local secret configuration files are ignored by Git.
- **Notes**: Inspect ignore behavior, including `.env`; committed `.env.example` remains available.

#### SEC-008: Version-control secret exclusion
- **Status**: NEW
- **Source**: FR-007
- **Assertion**: The feature's version-controlled files contain no real credential values.
- **Notes**: Repository inspection is distinct from runtime logging checks.

#### SEC-009: Structured outcome logging
- **Status**: NEW
- **Source**: AS6; FR-008; SC5
- **Assertion**: Each introduced request outcome is recorded in structured logs.
- **Notes**: Include successful requests, authorization rejection, and endpoint validation/read failures.

#### SEC-010: Outcome status
- **Status**: NEW
- **Source**: AS6; FR-008
- **Assertion**: Each introduced request outcome log identifies the resulting HTTP status.

#### SEC-011: Request correlation
- **Status**: NEW
- **Source**: AS6; FR-006; FR-008
- **Assertion**: Each introduced request outcome log carries the corresponding request correlation identifier.
- **Notes**: Cover successes and failures; preserve the existing response correlation behavior.

#### SEC-012: Sanitized error context
- **Status**: NEW
- **Source**: AS6; FR-008
- **Assertion**: A logged authentication-related failure includes useful sanitized error context.
- **Notes**: The spec does not prescribe event names or exact diagnostic wording; secret exclusion remains SEC-002.

### Foundation compatibility

#### FND-001: Public health
- **Status**: MODIFY
- **Source**: AS7; FR-009; EC8; SC6
- **Assertion**: `/healthz` returns its existing healthy-process response without HTTP credentials.
- **Notes**: Existing TestHealthReadinessAndSanitizedLogs asserts status 200; extend coverage to the authentication-configured service.
- **Existing file**: `internal/web/server_test.go`

#### FND-002: Health independent of database
- **Status**: MODIFY
- **Source**: AS7; FR-009; SC6
- **Assertion**: `/healthz` remains successful during a database outage.
- **Notes**: Existing outage_recovery_and_retained_data covers real PostgreSQL outage; its configuration setup needs authentication values.
- **Existing file**: `internal/integration/foundation_test.go`

#### FND-003: Public ready
- **Status**: MODIFY
- **Source**: AS7; FR-009; EC8
- **Assertion**: `/readyz` returns its existing ready response without HTTP credentials when the database is available.
- **Notes**: Existing TestHealthReadinessAndSanitizedLogs covers status 200 with a successful dependency.
- **Existing file**: `internal/web/server_test.go`

#### FND-004: Unavailable readiness
- **Status**: MODIFY
- **Source**: AS7; FR-009; SC6
- **Assertion**: `/readyz` returns `503` while the database is unavailable.
- **Notes**: Existing outage_recovery_and_retained_data verifies actual PostgreSQL unavailability.
- **Existing file**: `internal/integration/foundation_test.go`

#### FND-005: Readiness recovery
- **Status**: MODIFY
- **Source**: AS7; FR-009; SC6
- **Assertion**: `/readyz` returns to `200` after the database recovers.
- **Notes**: Existing outage_recovery_and_retained_data verifies actual PostgreSQL restart.
- **Existing file**: `internal/integration/foundation_test.go`

#### FND-006: Readiness deadline
- **Status**: MODIFY
- **Source**: AS7; FR-009
- **Assertion**: Readiness checking remains bounded by its configured deadline.
- **Notes**: Existing TestReadinessDeadline; retain the observed timeout outcome with valid authentication configuration.
- **Existing file**: `internal/web/server_test.go`

#### FND-007: Graceful active request
- **Status**: MODIFY
- **Source**: AS7; FR-009
- **Assertion**: An admitted request can finish normally within the shutdown grace period.
- **Notes**: Existing TestGracefulShutdownAllowsActiveRequestToFinish; supply valid authentication wherever the request is protected.
- **Existing file**: `internal/web/server_test.go`

#### FND-008: Overdue request cancellation
- **Status**: MODIFY
- **Source**: AS7; FR-009
- **Assertion**: An overdue active request is canceled after the shutdown grace period.
- **Notes**: Existing TestShutdownCancelsOverdueWorkAndWaitsForCleanup; preserve deliberately coordinated overlap.
- **Existing file**: `internal/web/server_test.go`

#### FND-009: Database cancellation
- **Status**: MODIFY
- **Source**: FR-009; SC6
- **Assertion**: Shutdown cancellation reaches an active PostgreSQL operation.
- **Notes**: Existing TestShutdownCancelsActivePostgresQuery; retain real PostgreSQL evidence.
- **Existing file**: `internal/integration/foundation_test.go`

#### FND-010: Cleanup ordering
- **Status**: MODIFY
- **Source**: AS7; FR-009
- **Assertion**: Resource cleanup waits for canceled active request work to finish.
- **Notes**: Existing TestShutdownCancelsOverdueWorkAndWaitsForCleanup.
- **Existing file**: `internal/web/server_test.go`

#### FND-011: Cleanup budget
- **Status**: MODIFY
- **Source**: AS7; FR-009
- **Assertion**: Shutdown cleanup remains bounded by its configured budget.
- **Notes**: Existing TestCleanupIsBounded uses the web test configuration helper that requires valid authentication values.
- **Existing file**: `internal/web/server_test.go`

#### FND-012: Shutdown admission
- **Status**: NEW
- **Source**: FR-009; EC5
- **Assertion**: Requests arriving after shutdown admission closes retain the foundation's shutdown rejection behavior.
- **Notes**: Cover `/auth-check` with and without credentials; shutdown rejection may precede authentication.

#### FND-013: HTTP header timeout
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: The HTTP server preserves its configured header-read timeout behavior.
- **Notes**: Existing configuration-default assertions alone do not verify server behavior.

#### FND-014: HTTP read timeout
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: The HTTP server preserves its configured request-read timeout behavior.
- **Notes**: HTTP-024 specifically exercises body inspection on the new endpoint; this criterion covers preservation of the shared server contract.

#### FND-015: HTTP write timeout
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: The HTTP server preserves its configured response-write timeout behavior.

#### FND-016: HTTP idle timeout
- **Status**: NEW
- **Source**: FR-006
- **Assertion**: The HTTP server preserves its configured idle-connection timeout behavior.

### Local usage and integration boundaries

#### DOC-001: Configuration instructions
- **Status**: NEW
- **Source**: AS8; FR-011; SC7
- **Assertion**: Documentation explains how to configure `AUTH_USERNAME` and `AUTH_PASSWORD` under the stated validation rules.
- **Notes**: Include no defaults, UTF-8, whitespace preservation, protocol restrictions, and absence of password-strength enforcement.

#### DOC-002: Service request example
- **Status**: NEW
- **Source**: AS8; FR-011; SC7
- **Assertion**: The documented service-client request reaches the authenticated `/auth-check` success response.
- **Notes**: Use synthetic credentials for reproducible verification.

#### DOC-003: Browser instructions
- **Status**: NEW
- **Source**: AS8; FR-011; SC7
- **Assertion**: Documentation explains use of the browser's built-in HTTP Basic prompt.

#### DOC-004: Local setup
- **Status**: NEW
- **Source**: AS8; FR-011
- **Assertion**: The documented local setup supplies usable authentication configuration to the serving application.
- **Notes**: Verify affected configuration examples and local startup wiring; migrations and probes remain covered by CFG-014 and CFG-015.

#### DOC-005: Delivered scope
- **Status**: NEW
- **Source**: AS8; FR-011; EC9
- **Assertion**: Documentation describes `/auth-check` as the delivered protected operation rather than claiming payment operations or browser pages exist.

#### DOC-006: Future route responsibility
- **Status**: NEW
- **Source**: FR-004; FR-011; EC9
- **Assertion**: Documentation assigns protection verification at order, checkout, payment-status/history, and browser-page entry points to the features that introduce them.

#### DOC-007: Webhook independence
- **Status**: NEW
- **Source**: FR-012; EC10
- **Assertion**: Documentation states that Stripe webhooks use signature verification independent of application HTTP Basic credentials.

#### DOC-008: Webhook ownership
- **Status**: NEW
- **Source**: FR-012; SC7
- **Assertion**: Documentation assigns environment-configured Stripe SDK webhook signature-verification integration to feature 3.
- **Notes**: Installing the SDK alone is not verification; no webhook handler is introduced here.

## Gaps & Open Questions

- No blocking behavioral ambiguities are identified in the current specification. Generic errors, diagnostic wording, and nonmatching-path status codes have no exact specified text/value; criteria deliberately do not invent them.
- **Verification gates (FR-010, SC8):** `make verify` is the local/CI gate, through `scripts/verify.sh` and `.github/workflows/verify.yml`. It covers formatting, generated-query freshness, workflow checks, static analysis, vulnerability checks, build, unit/integration tests, and race detection. These are hook-level execution obligations, not new behavioral test examples. Report actual results separately from outstanding checks; no application checks are run by this criteria derivation.
- **Inspection/manual evidence (FR-007, FR-010, FR-011; SC2, SC5, SC7):** SEC-003 and SEC-005 through SEC-008 require inspection of actual delivered artifacts/data paths. DOC criteria require documentation review or local usage verification. AUTH-021 includes browser rendering. The test plan must assign suitable evidence rather than infer these outcomes from a passing middleware unit test. No product clarification is needed for these evidence obligations.
- **Deferred entry points (FR-003, FR-004, FR-012; EC9, EC10):** payment routes and browser pages have no current runtime entry points. Future features must verify their own protection; their absence supplies no authentication evidence. Webhook signature verification belongs to feature 3. DOC-005 through DOC-008 record these boundaries; this feature must not introduce payment operations or a webhook handler to test them.
- **Shared quality applicability (FR-010):** Q1/Q5 require actual entry-point and connected configuration-to-HTTP evidence. Q4/Q6 require preservation of request cancellation, timeout, readiness, logging, and shutdown guarantees; retain PostgreSQL-backed foundation evidence and race checks. Q2 payment persistence, Q3 Stripe recovery, and worker-pool requirements are outside this feature because it introduces none of those behaviors. Automated checks require no live Stripe access or credentials.
- **Existing-test planning:** configuration validation changes may require fixture updates beyond the specific foundation assertions listed here, including configuration-default and command tests. The test plan must inventory every affected registration and shared helper under the Go runner contract; criterion statuses do not authorize unplanned edits. No existing authentication coverage is claimed.
