# Feature: Authenticate service and browser access

**Branch**: `001-authenticate-service-and-browser` · **Created**: 2026-10-08 · **Status**: draft

## Summary

An authenticated local caller can access protected application operations using one environment-configured HTTP Basic username/password pair. The same mechanism supports the eventual browser interface through the browser's built-in authentication prompt, without embedding privileged credentials in delivered HTML or JavaScript. This feature establishes authentication for subsequent payment features while preserving the foundation's health, readiness, and shutdown behavior.

## Acceptance Scenarios

1. **Authenticated service access**: Given valid authentication configuration and a protected operation available in this feature, when a service caller sends the configured HTTP Basic credentials, then authentication permits the request to reach that operation.
2. **Unauthorized access**: Given a protected operation available in this feature, when a caller omits credentials or supplies incorrect or malformed credentials, then the request is rejected without performing the protected operation or returning its protected content.
3. **Browser authentication**: Given a browser request to a protected entry point without credentials, when the application challenges the request, then it uses HTTP Basic authentication compatible with the browser's built-in prompt. When the browser supplies the configured credentials, authentication permits access without a custom login page or application session.
4. **Invalid configuration**: Given missing or invalid authentication configuration, when the application starts serving requests, then startup fails with a useful error that does not disclose credential values.
5. **Input validation and limits**: Given an entry point introduced or changed by this feature, when a request violates its defined accepted input format, value constraints, or maximum body size, then the application rejects the invalid request without performing the protected operation. Exact endpoint contracts and limits are subject to Open Questions.
6. **Secret handling and observability**: Given successful and rejected authentication attempts, when responses and correlated structured logs are inspected, then they identify the request outcome without exposing credentials, raw authorization values, or unnecessary personal data. Browser-delivered code and committed configuration examples contain no privileged credential values.
7. **Foundation compatibility**: Given the authentication feature is configured, when callers exercise health/readiness checks, database-outage readiness recovery, and service shutdown, then the foundation's existing probe meanings and bounded shutdown behavior remain intact.
8. **Local usage documentation**: Given a caller following the application documentation, when they configure and use authentication, then the instructions explain service requests, the browser's built-in prompt, and the boundary between delivered authentication support and subsequent payment/browser features.

## Edge Cases

- Missing credentials, a wrong username, a wrong password, and malformed authentication input must not grant access to a protected operation.
- Missing or invalid configured credentials must not leave the application serving protected operations without authentication. Exact accepted credential constraints are subject to Open Questions.
- Invalid request input and oversized bodies must be rejected according to the contract of each entry point introduced or changed here; the exact limits remain to be specified.
- Authentication failures and configuration errors must not echo submitted or configured secrets in responses or logs.
- Health and readiness checks must retain their existing operational behavior; authentication must not disrupt the foundation's probe command or shutdown handling.
- There are no payment operations or browser pages in the current runtime. Their absence must not be represented as proof that their eventual entry points are protected.
- Stripe webhook requests use a separate signature-verification mechanism. Application HTTP Basic credentials must not become a webhook prerequisite or a substitute for signature verification.

## Functional Requirements

1. **FR-001 — Authentication mechanism**: The system MUST use HTTP Basic authentication with one username/password pair configured through environment variables for protected service operations and the local browser interface.
2. **FR-002 — Request enforcement**: The system MUST check credentials in Go HTTP middleware on each protected request and prevent unauthenticated callers from reaching protected behavior or content.
3. **FR-003 — Browser support**: The system MUST support the browser's built-in authentication prompt through HTTP Basic challenges. The eventual browser payment journey MUST NOT require privileged service credentials embedded in delivered HTML or JavaScript.
4. **FR-004 — Feature integration boundary**: The system MUST establish protection that subsequent payment features can apply to order creation, checkout, payment status/history, and their browser pages. This feature MUST verify unauthorized-request rejection at the actual application entry points it introduces or changes; subsequent features MUST verify protection at their own entry points.
5. **FR-005 — Configuration validation**: The system MUST validate authentication configuration before serving application requests and reject missing or invalid configuration without disclosing its values. Environment variable names and accepted credential constraints are subject to Open Questions.
6. **FR-006 — Input validation**: The system MUST enforce the accepted input formats, value constraints, and maximum request-body sizes specified for each endpoint introduced or changed by this feature, following Q5 of `FEATURE-QUALITY-STANDARDS.md`. Concrete endpoint contracts and limits are subject to Open Questions.
7. **FR-007 — Data minimization**: The system MUST keep secrets in environment-based configuration, keep real secrets out of version control, and exclude credentials and unnecessary personal data from logs, responses, and stored data. Committed configuration examples MUST contain placeholders; local secret configuration MUST remain ignored by Git.
8. **FR-008 — Logging**: The system MUST log outcomes of introduced or changed application operations and failures with request correlation identifiers and sanitized error context, following the shared logging and security standards.
9. **FR-009 — Foundation compatibility**: The system MUST preserve existing health/readiness behavior, request cancellation, and bounded shutdown behavior.
10. **FR-010 — Verification**: The system MUST have tests for valid authentication, unauthorized requests, invalid configuration, rejected inputs, and request-size limits at the entry points introduced or changed here. Verification MUST exercise the connected configuration-to-HTTP path where it is changed and inspect configuration, responses, logs, and any affected stored-data paths for secret exposure. Applicable shared quality standards and the existing local/CI checks MUST remain satisfied.
11. **FR-011 — Documentation**: The system MUST document authentication configuration, local service usage, browser prompt behavior, and the limits of the delivered functionality. Affected local setup instructions and configuration examples MUST remain usable. Documentation MUST state that later payment features wire and verify their own protected entry points.
12. **FR-012 — Webhook boundary**: The system MUST keep Stripe webhooks independent of application HTTP Basic credentials. Feature 3 owns wiring the Stripe SDK's signature verification into its webhook handler with an environment-configured webhook secret; installing the SDK alone does not provide that verification.

## Success Criteria

- Correct HTTP Basic credentials permit access through the protection established here; missing, incorrect, and malformed credentials cannot invoke protected behavior or reveal protected content.
- Browser requests receive the HTTP Basic challenge needed for the built-in prompt and can authenticate using the configured pair, without a custom login/session system or privileged credentials in browser-delivered code.
- Missing and invalid authentication configuration prevent serving protected operations and produce sanitized diagnostic errors.
- Tests demonstrate enforcement of the defined input contracts and body-size boundaries at each entry point introduced or changed in this feature.
- Configuration examples, responses, logs, and affected data paths contain no real secrets or unnecessary personal data; request outcomes remain correlated and observable.
- Existing health/readiness behavior, readiness recovery, and bounded shutdown remain verified through the foundation checks.
- Local callers can follow the documented authentication setup and service usage. Documentation accurately describes support for the eventual browser interface and assigns payment-route and webhook integration to their respective features.
- Local and CI verification use the same documented formatting, static-analysis, vulnerability, unit/integration-test, and race-detection checks. Reported verification distinguishes checks actually run from outstanding evidence.

## Out of Scope

- Order creation, checkout, payment status/history behavior, and their browser pages; these belong to subsequent features.
- A custom login page, user database, application sessions, registration, multiple users or merchants, or multi-tenant authorization.
- Stripe webhook handler implementation and signature-verification integration; these belong to feature 3.
- Changes to the application scope: one local Go application with PostgreSQL, one merchant, one Stripe sandbox account, and one configured currency.
- Real-money operation, cloud deployment, and the other lifecycle and infrastructure exclusions in `AGENTS.md`.
- New payment persistence, Stripe calls, reconciliation workers, or unrelated application behavior solely to demonstrate authentication.

## Open Questions

1. **Protected entry point and executable evidence**: The current runtime wires only `GET /healthz` and `GET /readyz` in `internal/web/server.go`; `cmd/service/main.go` supplies no additional application handler. What concrete non-payment entry point, if any, should this feature expose to demonstrate authenticated service access and the browser prompt? If no additional runtime endpoint is intended, what routing contract and test evidence should establish protection without claiming coverage of nonexistent payment/browser endpoints?
2. **Authentication configuration contract**: What environment variable names and credential validity constraints should apply, including empty values, whitespace, supported characters, and length limits? `internal/config/config.go` currently loads configuration for `serve`, `migrate`, and `probe`; which commands require the new credential configuration while preserving foundation behavior?
3. **Request contracts and limits**: For the entry points introduced or changed here, what methods, accepted input formats, value constraints, maximum body sizes, and observable rejection behavior should apply? The current HTTP runtime has timeouts but no explicit request-body limit. Resolve these concrete contracts under the shared security standards without introducing a login/session system or future payment request schemas.

The HTTP Basic authentication mechanism is settled. These questions concern the concrete scope and validation contracts needed to make the feature testable against the existing foundation.
