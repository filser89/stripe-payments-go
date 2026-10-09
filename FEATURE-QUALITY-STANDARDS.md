# Shared Feature Quality Standards

These standards define project-specific quality requirements. Each requirement is mandatory when its stated condition applies. A presentation-only change does not require new database-concurrency tests; a change to payment persistence must verify the database guarantees it changes.

## Q1. Project testing requirements

- Test payment rules with a fake Stripe dependency that returns prepared result objects or errors. These tests do not require the real SDK or an HTTP server.
- Verify database guarantees against real PostgreSQL at the agreed version with the application's migrations.
- Use Stripe integration tests with the real SDK pointed at a local `httptest` server that returns stubbed HTTP responses. Verify outgoing request parameters and idempotency keys, response/error handling, and actual timeout/cancellation behavior. The server runs within the tests; it is not a separately managed service.
- Test webhook handling with synthetic payloads signed using a test secret, including invalid-signature rejection.
- Exercise the connected application path when a change crosses HTTP, business logic, persistence, or Stripe integration. Independently passing isolated tests are insufficient evidence that those parts work together.
- Reproduce external failures through controlled injection. Automated correctness tests must work without real Stripe credentials or availability.

## Q2. Durable and consistent data

For features that create or change persisted state:

- Enforce the feature's required uniqueness and consistency guarantees with database constraints and transactions.
- Commit a business state change and its corresponding audit-history entry atomically. A failed transaction must leave neither partially committed.
- Store monetary values in integer minor units with explicit currency; reject unsupported currencies and amounts outside the configured limits.
- Express schema changes through versioned migrations and verify the affected behavior against that schema.

Evidence: PostgreSQL integration tests that inspect stored results; test duplicate operations for uniqueness guarantees and transaction failures for atomicity guarantees.

## Q3. Failure recovery and external calls

For features that interact with Stripe or recover interrupted work:

- Distinguish confirmed success, confirmed failure, and unresolved outcomes. A timeout alone must not become a confirmed business failure.
- Give external calls explicit deadlines and retries explicit attempt and elapsed-time limits. Preserve the same durable operation identity, idempotency key, and parameters when retrying the same mutation.
- For external mutations followed by local persistence, test both loss of the external response and interruption after external success but before the local result commits. Verify recovery after restart.
- Repeated processing and recovery, including duplicate or delayed evidence, must not repeat business effects or regress confirmed payment state.
- When available evidence does not establish the outcome, retain an explicit unresolved state and report it for investigation. Do not retry creation beyond Stripe's idempotency retention guarantee merely because no local result was saved.

Evidence: controlled failure/recovery tests that verify both the returned outcome and the resulting persisted state.

## Q4. Concurrency and resource lifetime

For features with concurrent execution or shared mutable state:

- Deliberately coordinate overlapping operations in tests; do not rely on timing sleeps to create a race.
- When concurrent operations access persisted state, verify the affected business invariants through PostgreSQL operations using independent connections. Process-local locks are insufficient for cross-process correctness.
- For worker pools, set explicit limits on worker count and queue capacity. Propagate cancellation and deadlines through channel operations and external calls.
- Ensure owned goroutines exit within the configured shutdown budget and unfinished work remains recoverable.
- Preserve successful independent results when another operation fails.
- Run the affected concurrent paths under Go race detection. A passing race check does not replace database-concurrency tests.

Evidence: tests for overlapping execution, worker/queue limits when workers are used, cancellation and shutdown of owned goroutines, and database invariants when persisted state is shared.

## Q5. Security and data minimization

For features that accept input, expose application operations, or handle sensitive data:

- Authenticate order creation, payment-status access, and payment-history access, including browser access. Verify webhook signatures before trusting webhook evidence.
- Enforce the accepted input formats, value constraints, and maximum request-body sizes defined for each endpoint. Verify order correlation, expected amount, and currency before accepting payment confirmation.
- Keep authoritative pricing under application control.
- Keep privileged credentials out of browser-delivered code. Keep secrets, card data, and unnecessary personal data out of logs, stored history, and version control.

Evidence: tests for rejected inputs and unauthorized requests at the entry points the feature introduces or changes, plus inspection of its configuration, responses, logs, and stored data paths.

## Q6. Go behavior, visibility, and documentation

- Check returned errors and propagate cancellation and deadlines through database and HTTP calls. Regenerate generated code from its source inputs.
- For application operations and failures introduced or changed by the feature, log the outcome with request/operation correlation identifiers and error context, subject to Q5's data restrictions.
- When startup or dependency readiness changes, verify that health checks identify a running process and readiness checks identify whether it can serve application requests. Recovery operations must report repaired and unresolved work.
- Update affected API usage, configuration examples, local setup, recovery instructions, and lifecycle limitations. Documentation must describe the behavior actually delivered.

Evidence: updated application documentation and reproducible local commands for the changed behavior.

## Q7. Required project checks

- Configure local and CI verification to run the same checks: `gofmt` formatting checks; golangci-lint static analysis; `govulncheck`; automated unit and integration tests; and Go race detection.
