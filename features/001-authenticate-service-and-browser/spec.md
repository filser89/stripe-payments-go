# Feature: Authenticate service and browser access

**Branch**: `001-authenticate-service-and-browser` · **Status**: draft

## Summary

Service callers authenticate with one environment-configured local account, and browser users access the same protected service through the browser's native Basic prompt. A minimal protected landing page at `/` identifies the running service before payment operations are available.

The service uses Go `net/http` and the browser's native Basic prompt. Authentication adds no database tables or Stripe calls. Preserve the application's foundation behavior and apply AGENTS.md and FEATURE-QUALITY-STANDARDS.md.

## Acceptance Scenarios

1. **Serving configuration:** Valid `BASIC_AUTH_USERNAME` and `BASIC_AUTH_PASSWORD` allow HTTP service startup. Missing, empty, invalid-character, non-ASCII, and over-limit values for either setting prevent serving, with a nonzero exit and a diagnostic identifying the invalid setting without disclosing its value. Exercise each setting and its accepted/rejected boundaries independently; no cross-product with unrelated invalid configuration is required.
2. **Rejection before work:** A protected request without credentials receives `401`, the specified challenge and generic rejection headers/message. The selected protected handler, its body reader, and its side effects remain untouched. Repeat a credential-free request after an authenticated success: prior success does not authorize it.
3. **Exact credential enforcement:** Wrong username, wrong password, and both wrong are rejected. Correct credentials authenticate case-sensitively without trimming or normalization. Verify supported character and byte-length boundaries so accepting any nonempty password, checking only the username, or silently normalizing credentials cannot satisfy the contract.
4. **Authenticated delegation:** Correct credentials invoke the selected protected handler exactly once. Its method, path, query, and body are preserved. Preserve an observable successful result and an observable handler error result, including status, headers, and body; authentication does not replace errors with success.
5. **Authorization parsing:** Unsupported schemes, malformed Base64, absent separators, empty/invalid decoded credentials, repeated Authorization fields, and values over 4 KiB produce rejection without handler execution or secret disclosure. A differently cased Basic scheme authenticates. Credentials supplied only through query parameters, cookies, or bodies do not authenticate.
6. **Public foundation routes:** Exact `GET`/`HEAD /healthz` and `/readyz` work without credentials. Preserve liveness, database readiness failure/recovery, probe method rejection, and bounded shutdown behavior. Probe-prefix paths such as `/healthz/private` remain protected. Unsupported probe methods cannot execute protected business work.
7. **Connected landing journey:** Start the service through its real configuration and routing. Unauthenticated `/` is rejected; authenticated `GET /` returns the static identification page and authenticated `HEAD /` returns corresponding headers with no body. HTML contains no credentials, forms, payment controls, or privileged tokens. Verify authenticated method rejection, unknown-path `404`, and the body outcomes below.
8. **Body-free landing behavior:** Authenticated requests containing any body byte return generic `400`. Empty bodies, including byte-empty EOF and a real empty unknown-length/chunked stream, permit the landing response. A real nonempty chunked stream is rejected. A body-read failure with no byte obtained produces generic `400` when the connection can still respond; it never produces a false successful landing response or exposes the underlying error. Real stalled body delivery remains bounded by the foundation's request-read deadline. These are distinct observations: immediate byte bounds alone do not establish a stalled-stream deadline.
9. **Command independence:** Serving requires valid Basic settings. Actual local `probe` and `migrate` execution remains usable when Basic settings are absent or invalid, subject to each command's existing database/runtime requirements. Readiness failure remains observable through the probe command; these commands are not authenticated HTTP endpoints.
10. **Sanitized observability:** Inspect captured startup diagnostics, responses, and structured request logs using distinctive supplied secrets, including encoded submitted credentials. Successful landing, authentication rejection, malformed authorization, body rejection, and body-read failure retain sanitized request correlation and outcomes. Relevant HEAD failures retain response headers without bodies. No credential value, raw Authorization header, request body, or sensitive error detail appears.
11. **Credential lifetime:** Changing environment values during a running process does not change its account. After restarting with a different configured pair, the old pair fails and the new pair succeeds. Every protected request is checked independently; no application session or logout endpoint is needed.
12. **Concurrent isolation and lifecycle:** Coordinate overlapping valid and invalid requests and check each outcome and the exact protected invocation count. Rejected requests never perform protected work. Run affected paths under race detection and preserve foundation lifecycle evidence, including shutdown admission and cleanup budgets. Authentication requires no new payment/database-concurrency scenarios.

Scenarios are behavioral groups, not a required one-assertion, one-test, or one-file layout. Exercise the actual application boundary and startup wiring; an isolated parsing or comparison helper is insufficient. Retain valuable existing foundation checks without duplicating them into every authentication scenario.

## Edge Cases

- The username is 1–128 ASCII bytes in `!` through `~`, excluding `:`; spaces and control characters are invalid. The password is 1–256 printable ASCII bytes (` ` through `~`), including spaces and `:`. Empty values, non-ASCII bytes, controls, and over-limit values are invalid. Leading/trailing password spaces are significant. Base64 is still the Basic wire encoding.
- An empty Authorization field or Basic scheme without a token is unauthenticated. Split decoded credentials at the username/password separator; password colons remain password data. Scheme casing has no effect on credential casing.
- Authorization values of exactly 4096 bytes are within the header-value cap; 4097 bytes are rejected before decoding. An at-limit value still needs valid syntax and matching supported decoded credentials; the boundary must distinguish size rejection from other rejection mechanisms. Passing the cap does not exempt a value from syntax or credential validation. [RFC 9110 §11.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4) permits one or more separator SP bytes between the scheme and token. A matching Basic token preceded by enough legal separator SP bytes to make the entire value exactly 4096 bytes remains valid and authenticates; the equivalent 4097-byte value is rejected before decoding. Padding applies to the wire separator, not to decoded credential values. Invalid in-cap values still return `401`.
- Missing Authorization after a successful request still returns `401`. Browser credential reuse does not create server-side remembered authentication.
- A declared body length does not establish actual emptiness. Inspect bounded actual input; distinguish EOF with no bytes from a read error, a received byte, and a stream that has not yet completed. Authentication rejection does not consume the body in application code.
- Transport-rejected malformed HTTP messages follow transport behavior. When a transport failure or shutdown prevents a response, no application-level `400` or challenge delivery is promised. An operating connection's body-read error must not become `200`.
- Shutdown admission may return the foundation's existing `503` before authentication. Preserve that ordering and bounded cleanup.
- Exact probe exceptions do not extend to similar prefixes. Valid credentials do not make an unknown path or unsupported method successful.
- Multiple unrelated startup settings may be invalid; no diagnostic precedence is prescribed.

## Functional Requirements

- **FR-001 — Startup account:** Read `BASIC_AUTH_USERNAME` and `BASIC_AUTH_PASSWORD` for HTTP serving and validate them against the ASCII policy. Missing, empty, or invalid settings prevent startup before requests are accepted. No built-in account, default password, or authentication-disable switch exists. Diagnostics identify the setting without its value.
- **FR-002 — Lifetime and commands:** Capture credentials for the process lifetime. Rotation requires restart; there is no hot reload or credential-management endpoint. Basic validation must not block local `migrate` or `probe`; preserve their existing configuration and execution behavior.
- **FR-003 — Protected boundary:** Authenticate `GET`/`HEAD /` and other application paths/methods before ordinary route/method handling. Exact `GET`/`HEAD /healthz` and `/readyz` remain public; other methods on those exact paths retain method rejection without business execution. Future payment routes/pages must verify their own protected wiring. Introduce no anonymous business-route exception.
- **FR-004 — Parsing and matching:** Accept one syntactically valid Basic Authorization header whose decoded values match the configured pair exactly. Recognize the scheme case-insensitively. Reject repeated Authorization fields, values longer than 4 KiB before decoding, unsupported/malformed credentials, and incorrect pairs. Never authenticate from a query, cookie, or body.
- **FR-005 — Authentication rejection:** Return `401` with `WWW-Authenticate: Basic realm="stripe-payments"`, `Content-Type: text/plain; charset=utf-8`, `Cache-Control: no-store`, and generic `Unauthorized` content. HEAD has no response body. Do not disclose which value failed, echo input, or redirect to a custom login page. Reject before downstream invocation, body parsing, or side effects.
- **FR-006 — Delegation:** For valid credentials, invoke the selected protected handler once, preserving method, path, query, body, and the handler's observable response. Authentication creates no payment effects and substitutes no success response.
- **FR-007 — Landing:** Authenticated `GET /` returns `200` with small static HTML identifying the local payment service. `HEAD /` returns corresponding headers without a body. Include no credentials, forms, payment controls, or privileged tokens. Reject other methods after authentication; authenticated unknown paths return `404`.
- **FR-008 — Landing input:** Accept no payload on the landing endpoint. Detect actual body bytes through bounded reading under existing request deadlines and return generic `400` for nonempty input. Byte-empty EOF is accepted. Body-read failures return generic `400` when a response is possible, without leaked error detail or false success. Transport/shutdown inability to respond follows foundation behavior. Authentication failures stay `401` without application body consumption. Do not introduce a query-rejection policy.
- **FR-009 — Foundation preservation:** Preserve public liveness/readiness, readiness deadlines and database failure/recovery, generated request identifiers, structured outcome logging, finite HTTP header/read/write/idle deadlines and header handling, shutdown admission, draining, and bounded cleanup. Keep loopback host publishing. Distinguish transport behavior from application rejection contracts.
- **FR-010 — Security and isolation:** Validate every protected request independently and maintain correct outcomes under concurrency. Keep credentials out of logs, responses, browser assets, setup diagnostics, and Stripe requests. Do not log raw Authorization, decoded credentials, configuration secrets, request bodies, or unnecessary personal data. Credential comparisons avoid content-dependent early exit; no timing-sensitive acceptance threshold is prescribed.
- **FR-011 — Browser behavior:** Use the native Basic challenge journey without application sessions or login/session cookies. The browser determines prompting and credential reuse; no guarantee of prompting on every load or application-controlled logout exists.

## Success Criteria

- Valid configured service and browser requests reach the actual protected landing route; invalid or missing credentials cannot invoke protected work, including after an earlier success.
- Startup and command behavior obey the serving-only credential requirement and credential lifetime contract.
- Parsing, exact matching, routing, body-stream outcomes, and independent concurrent requests meet the specified observable contract.
- Foundation probes, HTTP deadlines, request observability, and lifecycle guarantees remain effective, with regression evidence at their meaningful boundaries.
- Responses, startup diagnostics, logs, and delivered browser assets disclose no supplied secrets or sensitive failure details.

## Out of Scope

Payment operations/pages, order creation, checkout, payment status/history, webhook handlers, and endpoint-specific body limits for future payment payloads. Authentication introduces no database state, user database, registration, multiple accounts/tenants, custom login page, application sessions, logout, remote/network deployment, or TLS setup. Future webhook verification is independent of Basic authentication.

## Delivery Obligations

### DO-001 — Empty BASIC_AUTH_USERNAME/PASSWORD placeholders and local/container credential setup

- **Source**: FR-001, FR-002, FR-010; AGENTS.md configuration/security requirements
- **Completion check**: Review .env.example for empty Basic credential placeholders, verify local secret files are ignored and untracked, and inspect tracked files/image-build inputs and image artifacts for credential exclusion. Review Compose wiring; resolve and launch the local application with synthetic credentials; verify values are supplied without diagnostic or browser disclosure. Never commit local credential values.

### DO-002 — Current service/browser usage and lifecycle documentation

- **Source**: FR-007, FR-011; Acceptance Scenarios 7, 11
- **Completion check**: Run documented request with placeholders replaced by synthetic values or interactive entry; perform native-browser challenge smoke check; inspect restart and browser caching/logout instructions.

### DO-003 — Local sandbox security and future-route/webhook boundaries

- **Source**: Summary, Out of Scope; FR-003, FR-010
- **Completion check**: Review docs for lack of Basic encryption, dedicated local credentials, local-only/TLS boundary, subsequent payment-route protection, and separate SDK webhook signature verification; do not claim undelivered routes exist.

### DO-004 — Accepted feature and foundation verification evidence

- **Source**: Acceptance Scenarios 1–12; Success Criteria; shared quality standards
- **Completion check**: Run make verify and affected race checks using local HTTP and PostgreSQL infrastructure; report actual results and gaps without live Stripe dependencies.

## Supporting Context

The application is one local Go service with PostgreSQL, one merchant, and one Stripe sandbox account. Authentication establishes the service/browser boundary without adding payment persistence or Stripe interactions. Existing foundation evidence remains relevant; no new database-concurrency or Stripe-recovery behavior is introduced.

## Open Questions

No unresolved behavioral questions. Implementation details and evidence organization remain subject to the application engineering standards.
