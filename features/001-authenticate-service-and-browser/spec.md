# Feature: Authenticate service and browser access

**Branch**: `001-authenticate-service-and-browser` · **Status**: draft

## Summary

Authentication applies to incoming HTTP requests from browser and service clients to protected backend endpoints. The backend checks these requests against one environment-configured HTTP Basic username/password pair. Internal command execution does not use HTTP authentication. The same mechanism supports the eventual browser interface through the browser's built-in authentication prompt, without embedding privileged credentials in delivered HTML or JavaScript. This feature exposes a protected `GET /auth-check` endpoint returning a fixed plain-text success response and establishes reusable authentication for subsequent payment features while preserving the foundation's health, readiness, and shutdown behavior.

## Acceptance Scenarios

1. **Authenticated service access**: Given valid authentication configuration, when a service caller requests `GET /auth-check` with the configured HTTP Basic credentials, then the endpoint returns `200 OK`, `Content-Type: text/plain; charset=utf-8`, and the body `Authenticated\n`.
2. **Unauthorized access**: Given a request to `GET /auth-check`, when a caller omits credentials or supplies incorrect or malformed credentials, then the endpoint returns `401 Unauthorized` with `WWW-Authenticate: Basic realm="stripe-payments-go"`, without invoking the protected handler or returning its success response.
3. **Browser authentication**: Given a browser request to `GET /auth-check` without credentials, when the application challenges the request, then it uses HTTP Basic authentication compatible with the browser's built-in prompt. When the browser supplies the configured credentials, it displays the fixed plain-text success response without a custom login page or application session.
4. **Invalid configuration**: Given missing or invalid authentication configuration, when the application starts serving requests, then startup fails with a useful error that does not disclose credential values.
5. **Input validation and limits**: Given an entry point introduced or changed by this feature, when a request violates its defined accepted input format, value constraints, or maximum body size, then the application rejects the invalid request without performing the protected operation. The `/auth-check` contract in FR-006 defines the accepted methods, input, and rejection behavior.
6. **Secret handling and observability**: Given successful and rejected authentication attempts, when responses and correlated structured logs are inspected, then they identify the request outcome without exposing credentials, raw authorization values, or unnecessary personal data. Browser-delivered code and committed configuration examples contain no privileged credential values.
7. **Foundation compatibility**: Given the authentication feature is configured, when callers exercise health/readiness checks, database-outage readiness recovery, and service shutdown, then the foundation's existing probe meanings and bounded shutdown behavior remain intact.
8. **Local usage documentation**: Given a caller following the application documentation, when they configure and use authentication, then the instructions explain service requests, the browser's built-in prompt, and the boundary between delivered authentication support and subsequent payment/browser features.

## Edge Cases

- Missing credentials, a wrong username, a wrong password, and malformed authentication input must not grant access to a protected operation.
- Missing or invalid configured credentials must not leave the application serving protected operations without authentication. A nonempty password such as `123` is valid; password strength is not a configuration requirement.
- `/auth-check` accepts no body (maximum 0 bytes). A nonempty body is rejected even when its length is unknown or it uses chunked transfer encoding; body inspection remains bounded by the existing HTTP read timeout.
- Duplicate `Authorization` headers, unsupported authentication schemes, malformed Base64, and decoded credentials without a username/password separator receive the same `401` challenge as incorrect credentials. Neither submitted usernames nor password values appear in errors.
- Authentication takes precedence over endpoint method, query, and body validation for requests reaching `/auth-check`. HTTP parsing/header-limit errors and existing shutdown rejection may occur before authentication middleware.
- Credential comparison is exact and case-sensitive; spaces are preserved, and the Basic scheme name is case-insensitive. A colon is permitted in the password but not in the username.
- Authentication failures and configuration errors must not echo submitted or configured secrets in responses or logs.
- Health and readiness checks must retain their existing operational behavior; authentication must not disrupt the foundation's probe command or shutdown handling.
- There are no payment operations or browser pages in the current runtime. Their absence must not be represented as proof that their eventual entry points are protected.
- Stripe webhook requests use a separate signature-verification mechanism. Application HTTP Basic credentials must not become a webhook prerequisite or a substitute for signature verification.

## Functional Requirements

1. **FR-001 — Authentication mechanism**: The system MUST use HTTP Basic authentication with one username/password pair configured through environment variables for protected service operations and the local browser interface.
2. **FR-002 — Request enforcement**: The system MUST check credentials in Go HTTP middleware on each incoming protected request and prevent unauthenticated callers from reaching protected behavior or content. HTTP authentication MUST NOT be used to authorize starting the application, running migrations, invoking probes, or executing other internal commands.
3. **FR-003 — Browser support**: The system MUST support the browser's built-in authentication prompt through HTTP Basic challenges. The eventual browser payment journey MUST NOT require privileged service credentials embedded in delivered HTML or JavaScript.
4. **FR-004 — Feature integration boundary**: The system MUST expose protected `GET /auth-check` with a fixed plain-text success response and establish reusable protection that subsequent payment features can apply to order creation, checkout, payment status/history, and their browser pages. This feature MUST verify unauthorized-request rejection at the actual application entry points it introduces or changes; subsequent features MUST verify protection at their own entry points.
5. **FR-005 — Configuration validation**: The backend MUST have a configured username/password pair against which it verifies incoming protected HTTP requests. Missing or invalid configuration MUST prevent serving protected requests and produce a diagnostic without credential values. The system MUST accept nonempty passwords such as `123` without minimum-length or character-complexity requirements. Migrations and probe commands MUST NOT require HTTP authentication configuration. The pair is configured using `AUTH_USERNAME` and `AUTH_PASSWORD`, with no default credentials. Both values MUST be nonempty UTF-8 strings; whitespace MUST NOT be trimmed. HTTP Basic protocol constraints prohibit ASCII control characters (U+0000–U+001F and U+007F) in either value and a colon in the username. No additional application-level credential-length or strength policy applies; requests remain subject to the existing Go HTTP server header limit. Clients encode credentials as UTF-8 and values are compared without case folding or Unicode normalization. The control-character and username-colon restrictions follow the [HTTP Basic credential syntax](https://www.rfc-editor.org/rfc/rfc7617#section-2).
6. **FR-006 — HTTP contract**: The system MUST apply the following contract to the exact `/auth-check` path:

   - Require exactly one valid HTTP Basic `Authorization` header. Missing, incorrect, malformed, or duplicate credentials return `401 Unauthorized` with `WWW-Authenticate: Basic realm="stripe-payments-go"` and a generic error. Credentials in query strings or bodies do not authenticate a request.
   - After successful authentication, accept `GET` and `HEAD`; other methods return `405 Method Not Allowed` with `Allow: GET, HEAD`.
   - Accept no query parameters or request body. After method validation, a nonempty query string returns `400 Bad Request`; otherwise a nonempty body returns `413 Content Too Large`. The maximum accepted body size is 0 bytes, including requests with an unknown length or chunked encoding. A body read failure returns a generic `400` when the connection still permits a response; it never produces success.
   - Successful `GET` returns `200 OK`, `Content-Type: text/plain; charset=utf-8`, and `Authenticated\n`. `HEAD` follows the same authentication and validation rules and returns the corresponding status and headers without a response body.
   - Responses from this endpoint include `Cache-Control: no-store`. Request correlation, HTTP timeouts, and header limits retain their existing behavior; the [Go HTTP header limit](https://pkg.go.dev/net/http#DefaultMaxHeaderBytes) remains `http.DefaultMaxHeaderBytes` (1 MiB). Authentication middleware MUST NOT impose this endpoint's zero-body contract on future payment routes.
   - Health/readiness routes retain their existing contracts. This feature introduces no payment request schema or webhook handler.

7. **FR-007 — Data minimization**: The system MUST keep secrets in environment-based configuration, keep real secrets out of version control, and exclude credentials and unnecessary personal data from logs, responses, and stored data. Committed configuration examples MUST contain placeholders; local secret configuration MUST remain ignored by Git.
8. **FR-008 — Logging**: The system MUST log outcomes of introduced or changed application operations and failures with request correlation identifiers and sanitized error context, following the shared logging and security standards.
9. **FR-009 — Foundation compatibility**: The system MUST keep `/healthz` and `/readyz` public and preserve their existing behavior, request cancellation, and bounded shutdown behavior.
10. **FR-010 — Verification**: The system MUST have tests for valid authentication, unauthorized requests, invalid configuration, rejected inputs, and request-size limits at the entry points introduced or changed here. Tests MUST demonstrate that a configured password of `123` is accepted and permits a matching protected HTTP request. Verification MUST exercise the connected configuration-to-HTTP path where it is changed and inspect configuration, responses, logs, and any affected stored-data paths for secret exposure. Applicable shared quality standards and the existing local/CI checks MUST remain satisfied.
11. **FR-011 — Documentation**: The system MUST document authentication configuration, local service usage, browser prompt behavior, and the limits of the delivered functionality. Affected local setup instructions and configuration examples MUST remain usable. Documentation MUST state that later payment features wire and verify their own protected entry points.
12. **FR-012 — Webhook boundary**: The system MUST keep Stripe webhooks independent of application HTTP Basic credentials. Feature 3 owns wiring the Stripe SDK's signature verification into its webhook handler with an environment-configured webhook secret; installing the SDK alone does not provide that verification.

## Success Criteria

- Correct HTTP Basic credentials allow `GET /auth-check` to return its fixed plain-text success response; missing, incorrect, and malformed credentials cannot invoke protected behavior or reveal protected content.
- Browser requests receive the HTTP Basic challenge needed for the built-in prompt and can authenticate using the configured pair, without a custom login/session system or privileged credentials in browser-delivered code.
- Missing and invalid authentication configuration prevent serving protected operations and produce sanitized diagnostic errors. A configured nonempty password such as `123` passes validation and authenticates matching HTTP requests; no password-strength policy applies.
- Tests demonstrate enforcement of the defined input contracts and body-size boundaries at each entry point introduced or changed in this feature.
- Configuration examples, responses, logs, and affected data paths contain no real secrets or unnecessary personal data; request outcomes remain correlated and observable.
- Existing health/readiness behavior, readiness recovery, and bounded shutdown remain verified through the foundation checks.
- Local callers can follow the documented `AUTH_USERNAME`/`AUTH_PASSWORD` configuration, request `GET /auth-check` from a service client, and exercise the built-in browser prompt. Documentation accurately describes support for the eventual browser interface and assigns payment-route and webhook integration to their respective features.
- Local and CI verification use the same documented formatting, static-analysis, vulnerability, unit/integration-test, and race-detection checks. Reported verification distinguishes checks actually run from outstanding evidence.

## Out of Scope

- Authentication of internal command execution and password-strength enforcement.
- Order creation, checkout, payment status/history behavior, and their browser pages; these belong to subsequent features.
- A custom login page, user database, application sessions, registration, multiple users or merchants, or multi-tenant authorization.
- Stripe webhook handler implementation and signature-verification integration; these belong to feature 3.
- Changes to the application scope: one local Go application with PostgreSQL, one merchant, one Stripe sandbox account, and one configured currency.
- Real-money operation, cloud deployment, and the other lifecycle and infrastructure exclusions in `AGENTS.md`.
- New payment persistence, Stripe calls, reconciliation workers, or unrelated application behavior solely to demonstrate authentication.

## Open Questions

None.
