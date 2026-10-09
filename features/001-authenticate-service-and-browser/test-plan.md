# Test Plan: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Spec**: [spec.md](spec.md)
**Acceptance Criteria**: [acceptance-criteria.md](acceptance-criteria.md)

## Summary

- Test files: 6 (3 new, 3 modify, 0 existing)
- Factories/fixtures: 2 (2 new, 0 existing)
- Shared helpers: 1 existing (modified)
- Behavioral criteria mapped (unique IDs): 22 / 22
- Required outcomes/variants mapped: 108 / 108
- Non-automated runtime evidence: 2 planned checks
- Delivery Obligations: 4 separate completion tasks
- Contract deltas: 2
- Invalidation sweep: 38 examples hit — 38 keep / 0 modify / 0 remove
- Planned state entries: 11 TOUCH, 2 PIN

Architecture inventory: `.kaba/architecture.md` is absent. Project rules and established package-local Go tests govern placement. Existing public `config.Load`, `web.New`, `Server.Handler`, `Server.Serve`, and command `run` allow boundary evidence without choosing middleware names or credential field layout. No missing declaration or compilation scaffold is prescribed. Tests must compile against real public boundaries and fail on unmet behavior. No production interfaces, helpers, or architecture are invented.

## Test Files

### cmd/service/authentication_test.go (NEW)

- **Criteria**: CFG-001, CFG-002, CFG-003
- **Describe blocks**:
  - `TestAuthenticationServingConfiguration` — covers: CFG-001; every required outcome and V1, V2, V3, V4, V5, V6, V7, V8, V9. Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting.
  - `TestAuthenticationCommandIndependence` — covers: CFG-002; every required outcome and V1, V2, V3, V4, V5, V6. Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes.
  - `TestAuthenticationCredentialLifetime` — covers: CFG-003; every required outcome and V1, V2. The running service uses its startup pair until restart; restarting uses the new pair.
- **Dependencies**: command runtime fixtures
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

### internal/web/authentication_test.go (NEW)

- **Criteria**: AUTH-001, AUTH-002, AUTH-003, AUTH-004, AUTH-005, AUTH-006, HTTP-001, HTTP-002, HTTP-003, HTTP-004, FND-001, FND-002, SEC-001
- **Describe blocks**:
  - `TestAuthenticationMissingCredentials` — covers: AUTH-001; every required outcome and V1, V2, V3, V4. A credential-free protected request returns generic challenge rejection before handler/body/side effects.
  - `TestAuthenticationExactCredentials` — covers: AUTH-002; every required outcome and V1, V2, V3, V4, V5, V6, V7. Only the exact configured username/password authenticates.
  - `TestAuthenticationAuthorizationParsing` — covers: AUTH-003; every required outcome and V1, V2, V3, V4, V5, V6, V7, V8, V9, V10, V11, V12. Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate.
  - `TestAuthenticationCredentialSources` — covers: AUTH-004; every required outcome and V1, V2, V3, V4. Only Authorization authenticates protected access.
  - `TestAuthenticationDelegation` — covers: AUTH-005; every required outcome and V1, V2, V3, V4. Valid credentials delegate exactly once and preserve request and handler response.
  - `TestAuthenticationConcurrentIsolation` — covers: AUTH-006; every required outcome and V1, V2, V3. Overlapping valid and invalid requests have independent outcomes and exact handler invocation count.
  - `TestAuthenticationLanding` — covers: HTTP-001; every required outcome and V1, V2, V3, V4. Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body.
  - `TestAuthenticationRouting` — covers: HTTP-002; every required outcome and V1, V2, V3, V4. Authentication precedes normal route/method handling and never makes unsupported requests successful.
  - `TestAuthenticationLandingBodyBytes` — covers: HTTP-003; every required outcome and V1, V2, V3, V4, V5. Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading.
  - `TestAuthenticationBodyReadFailure` — covers: HTTP-004; every required outcome and V1, V2, V3, V4. An operating connection with a body-read failure receives generic 400 without false success or sensitive error details.
  - `TestAuthenticationPublicProbeBoundary` — covers: FND-001; every required outcome and V1, V2, V3, V4, V5. Exact probes remain public with preserved liveness/readiness/method semantics and no protected work.
  - `TestAuthenticationShutdownAdmission` — covers: FND-002 V1; operating shutdown admission rejects protected requests with/without credentials using the existing 503 behavior before protected work.
  - `TestAuthenticationSanitizedOutcomes` — covers: SEC-001; every required outcome and V1, V2, V3, V4, V5, V6, V7, V8. Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes.
- **Dependencies**: existing web settings/log capture helper; web request fixtures
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

### internal/web/authentication_transport_test.go (NEW)

- **Criteria**: HTTP-005, HTTP-006, FND-004
- **Describe blocks**:
  - `TestAuthenticationRealStreamingBodies` — covers: HTTP-005; every required outcome and V1, V2, V3. Real HTTP streaming bodies obey actual emptiness and nonempty rejection.
  - `TestAuthenticationStalledBodyDeadline` — covers: HTTP-006; every required outcome and V1, V2, V3. A real stalled request body cannot wait beyond the foundation read budget; no false successful landing result occurs.
  - `TestAuthenticationFoundationTransport` — covers: FND-004; every required outcome and V1, V2, V3, V4, V5. Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior.
- **Dependencies**: existing web settings/log capture helper; web request fixtures
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

### internal/web/server_test.go (MODIFY)

- **Criteria**: FND-001, FND-002
- **Describe blocks**:
  - `TestGracefulShutdownAllowsActiveRequestToFinish` — covers: FND-002 V2, admitted authenticated work finishes during grace before cleanup.
  - `TestShutdownCancelsOverdueWorkAndWaitsForCleanup` — covers: FND-002 V3, overdue authenticated work is canceled and completion precedes cleanup.
  - `TestCleanupFailureIsReported` and `TestCleanupIsBounded` — covers: FND-002 V4, cleanup errors and elapsed-time bounds respectively.
  - `TestServeFailureClosesResources` — covers: FND-002 V5, failed serving still closes resources.
  - All existing health/readiness, shutdown, panic, cleanup and serve-failure functions remain; covers: FND-001/FND-002 foundation outcomes. Authenticate /work in lifecycle/panic witnesses; preserve public probe calls. New shutdown-admission with/without credentials evidence belongs in `TestAuthenticationShutdownAdmission` in internal/web/authentication_test.go (covers FND-002 V1).
- **Dependencies**: existing web settings/log capture helper
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

### internal/integration/foundation_test.go (MODIFY)

- **Criteria**: FND-003
- **Describe blocks**:
  - `TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` — covers: FND-003 V1 (database outage/restart readiness) and V2 (retained data and migration version after recovery).
  - `TestDatabaseMigrationsReadinessAndRestart/generated_readiness_query`, `/empty_production_migrations`, `/apply_repeat_and_rollback`, `/failing_transaction_leaves_no_partial_table` — covers: FND-003 V2, preserve direct query/migration/repeat/rollback/atomicity checks unchanged.
  - `TestShutdownCancelsActivePostgresQuery` — covers: FND-003 V3, authenticated query canceled before pool cleanup.
  - `TestOpenRejectsUnavailableDatabaseWithinDeadline` — covers: FND-003 V4, unavailable PostgreSQL handshake fails within its startup budget without secret disclosure.
  - Add synthetic Basic values only inside the outage callback and active-query cancellation function; authenticate /work only in the cancellation function. All direct query/migration callbacks and unavailable-open function remain unchanged. Sibling callback digest changes do not authorize unrelated TOUCH entries.
- **Dependencies**: none (existing package-local setup retained)
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

### cmd/service/main_test.go (MODIFY)

- **Criteria**: CFG-001, SEC-001 (preserved startup diagnostics supporting new command-level coverage)
- **Describe blocks**:
  - Existing missing database diagnostic — covers: CFG-001, SEC-001 existing runtime failure behavior; supply valid synthetic Basic values so unrelated error precedence remains immaterial. Invalid-database sanitization and unknown-command rejection remain untouched; neither requires valid Basic fixtures because neither asserts competing setting-error precedence.
- **Dependencies**: none (existing package-local setup retained)
- **Notes**: Use named functions/literal subtests and literal anonymous struct rows supported by the Go runner. Place criterion markers beside registrations. Group assertions coherently without erasing mapped variants. Do not prescribe an implementation-only declaration.

AUTH-003 V11/V12 use a fixed matching supported credential pair: Base64-encode username + colon + password, then place one or more SP bytes after Basic so the complete header value is 4096 or 4097 bytes. [RFC 9110 §11.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4) defines this legal separator. No decoded credential whitespace is normalized. The valid at-limit value authenticates; the otherwise-valid over-limit value rejects before decoding; malformed or nonmatching in-cap values still return 401.

## Invalidation Sweep

### CD-1 — Serving without Basic configuration becomes startup rejection (FR-001; CFG-001)

- **Kind**: conditional on missing/invalid Basic settings.
- **Probes**: `config.Load`, `DATABASE_URL`, `run(` at configuration/command boundaries; existing success/error assertions at behavior boundary.

### CD-2 — Non-probe application requests require authentication before handlers/routing (FR-003; AUTH-001, HTTP-002)

- **Kind**: conditional on missing/invalid Authorization.
- **Probes**: `http.Get`, `ServeHTTP`, `/work`, `/healthz`, `/readyz`; handler entry channels, response status, request logs, cancellation/cleanup assertions at behavior boundary. New `/` route was also checked against negative-route guards; no such assertion exists in the current suite.

Config/default tests and the invalid-database and unknown-command command tests remain unedited. Credentials are serving-specific; these tests do not become credential-validation tests.

All probes were run over all configured test-owned source: four `_test.go` files and the integration testdata sources. Fixture SQL contains no relevant request/configuration assertions. Broad runtime/lifetime probes (`Timeout|Shutdown|Cleanup`) retained independent checks in the inventory. Every discovered leaf is dispositioned below. KEEP describes preserved outcomes with the explicit setup correction in Planned State Changes; it never means leaving credential-free protected witnesses intact.

| Example (address) | Description | Disposition | Reason |
|---|---|---|---|
| `github.com/filser89/stripe-payments-go/cmd/service::TestRunRejectsInvalidDatabaseWithoutExposingCredentials` | `TestRunRejectsInvalidDatabaseWithoutExposingCredentials` | KEEP | Missing-database diagnostic: supply valid Basic settings to isolate DATABASE_URL; invalid-database test only asserts error/sanitization and remains unchanged. |
| `github.com/filser89/stripe-payments-go/cmd/service::TestRunRejectsMissingConfiguration` | `TestRunRejectsMissingConfiguration` | KEEP | Missing-database diagnostic: supply valid Basic settings to isolate DATABASE_URL; invalid-database test only asserts error/sanitization and remains unchanged. |
| `github.com/filser89/stripe-payments-go/cmd/service::TestRunRejectsUnknownCommandBeforeConnecting` | `TestRunRejectsUnknownCommandBeforeConnecting` | KEEP | Unknown command still rejected before connection/configuration; no edit. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/bad_log_level` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/bad_log_level` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/bad_port` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/bad_port` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/invalid_database` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/invalid_database` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/invalid_timeout` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/invalid_timeout` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_database` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_database` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_database_name` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_database_name` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_host` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/missing_host` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/negative_readiness` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/negative_readiness` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/overflow_budget` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/overflow_budget` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/short_container_budget` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/short_container_budget` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/wrong_scheme` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/wrong_scheme` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_cleanup` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_cleanup` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_header` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_header` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_idle` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_idle` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_port` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_port` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_read` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_read` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_shutdown` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_shutdown` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_startup` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_startup` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_write` | `TestLoadRejectsInvalidConfigurationWithoutSecrets/zero_write` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/config::TestLoadValidConfiguration` | `TestLoadValidConfiguration` | KEEP | Common configuration tests do not serve HTTP; keep unchanged, with no Basic fixture or TOUCH required. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/apply_repeat_and_rollback` | `TestDatabaseMigrationsReadinessAndRestart/apply_repeat_and_rollback` | KEEP | Direct database query/migration callback has no serving config or protected request; unchanged, no TOUCH. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/empty_production_migrations` | `TestDatabaseMigrationsReadinessAndRestart/empty_production_migrations` | KEEP | Direct database query/migration callback has no serving config or protected request; unchanged, no TOUCH. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/failing_transaction_leaves_no_partial_table` | `TestDatabaseMigrationsReadinessAndRestart/failing_transaction_leaves_no_partial_table` | KEEP | Direct database query/migration callback has no serving config or protected request; unchanged, no TOUCH. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/generated_readiness_query` | `TestDatabaseMigrationsReadinessAndRestart/generated_readiness_query` | KEEP | Direct database query/migration callback has no serving config or protected request; unchanged, no TOUCH. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` | `TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` | KEEP | Preserve migration/readiness/PostgreSQL lifecycle assertions with valid serving setup; authenticate protected query calls. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestOpenRejectsUnavailableDatabaseWithinDeadline` | `TestOpenRejectsUnavailableDatabaseWithinDeadline` | KEEP | Direct PostgreSQL startup deadline is independent of Basic configuration; no edit. |
| `github.com/filser89/stripe-payments-go/internal/integration::TestShutdownCancelsActivePostgresQuery` | `TestShutdownCancelsActivePostgresQuery` | KEEP | Preserve migration/readiness/PostgreSQL lifecycle assertions with valid serving setup; authenticate protected query calls. |
| `github.com/filser89/stripe-payments-go/internal/web::TestCleanupFailureIsReported` | `TestCleanupFailureIsReported` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestCleanupIsBounded` | `TestCleanupIsBounded` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestGracefulShutdownAllowsActiveRequestToFinish` | `TestGracefulShutdownAllowsActiveRequestToFinish` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestHealthReadinessAndSanitizedLogs` | `TestHealthReadinessAndSanitizedLogs` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestPanicProducesSanitizedFailureLog` | `TestPanicProducesSanitizedFailureLog` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestReadinessDeadline` | `TestReadinessDeadline` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestServeFailureClosesResources` | `TestServeFailureClosesResources` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |
| `github.com/filser89/stripe-payments-go/internal/web::TestShutdownCancelsOverdueWorkAndWaitsForCleanup` | `TestShutdownCancelsOverdueWorkAndWaitsForCleanup` | KEEP | Preserve foundation assertions with valid serving setup and authenticated protected-work calls; no status change. |

## Planned State Changes

The 11 TOUCH entries permit credential fixture additions and authenticated /work calls while requiring unchanged baseline status. The shared web settings helper affects its eight dependent leaves. Integration credential additions stay local to the outage callback and active-query cancellation function; no parent/import/shared-fixture edit is planned. No assertion is weakened, no test is removed, and no existing failure is reclassified. The two PIN functions verify already-conforming command independence and transport settings against real code; all other new behavior functions must expose unmet feature behavior. The entire capture is overlay-free, so PIN is legal. TestAuthenticationFoundationTransport uses the existing exact public probes and existing injectable handler where needed to demonstrate transport deadlines without depending on landing/authentication success; it must pass against real foundation code. TestAuthenticationCommandIndependence uses actual run command execution with local PostgreSQL and a controlled ready/not-ready HTTP peer. No shell-only or mocked configuration shortcut substitutes for command execution.

| Action | Identity (address) | Description | Expected landing | Reason |
|---|---|---|---|---|
| TOUCH | `github.com/filser89/stripe-payments-go/cmd/service::TestRunRejectsMissingConfiguration` | `TestRunRejectsMissingConfiguration` | unchanged | FR-001/Edge Case 9: supply valid Basic settings to isolate the missing DATABASE_URL diagnostic without inventing invalid-setting precedence. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/integration::TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` | `TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` | unchanged | FR-009/FND-003 V1–V2: add valid Basic settings only in this serving callback; retain readiness/data assertions and baseline status. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/integration::TestShutdownCancelsActivePostgresQuery` | `TestShutdownCancelsActivePostgresQuery` | unchanged | FR-003/FR-009/FND-003 V3: add local serving credentials and authenticate /work; preserve query-cancellation/cleanup assertions. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestCleanupFailureIsReported` | `TestCleanupFailureIsReported` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestCleanupIsBounded` | `TestCleanupIsBounded` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestGracefulShutdownAllowsActiveRequestToFinish` | `TestGracefulShutdownAllowsActiveRequestToFinish` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestHealthReadinessAndSanitizedLogs` | `TestHealthReadinessAndSanitizedLogs` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestPanicProducesSanitizedFailureLog` | `TestPanicProducesSanitizedFailureLog` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestReadinessDeadline` | `TestReadinessDeadline` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestServeFailureClosesResources` | `TestServeFailureClosesResources` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| TOUCH | `github.com/filser89/stripe-payments-go/internal/web::TestShutdownCancelsOverdueWorkAndWaitsForCleanup` | `TestShutdownCancelsOverdueWorkAndWaitsForCleanup` | unchanged | Shared settings(t) receives valid serving credentials, affecting this dependent digest; authenticate protected /work where present and preserve foundation outcomes. |
| PIN | `—` | `TestAuthenticationCommandIndependence` | passed | Pin already-conforming command independence or foundation transport against real code; no scaffolds. |
| PIN | `—` | `TestAuthenticationFoundationTransport` | passed | Pin already-conforming command independence or foundation transport against real code; no scaffolds. |

## Factories / Fixtures

### command runtime fixtures (NEW)

- **File**: cmd/service/authentication_test.go
- **Base attributes**: synthetic distinct valid Basic pair, loopback listener, valid runtime settings, PostgreSQL 18.6 with application migrations, bounded cancellation and captured output.
- **Traits/variants**: each invalid ASCII/length/missing setting; alternate restart pair; probe ready/not-ready/unavailable; migrated database.
- **Associations**: real local PostgreSQL and HTTP only; no Stripe.
- **Used by**: cmd/service/authentication_test.go

### web request fixtures (NEW)

- **File**: internal/web/authentication_test.go; internal/web/authentication_transport_test.go (file-local fixtures)
- **Base attributes**: synthetic valid Basic pair, actual web server boundary, bounded local listener, distinctive secret sentinels.
- **Traits/variants**: malformed headers, exact byte bounds, counting/unread body, byte-empty EOF, read failure, real empty/nonempty chunking, stalled stream, blocked write, idle connection, coordinated callers.
- **Associations**: existing web settings/log capture helper. No entity factory or payment data.
- **Used by**: internal/web/authentication_test.go, internal/web/authentication_transport_test.go

## Shared Helpers

### existing web settings/log capture helper (MODIFY)

- **File**: internal/web/server_test.go
- **Type**: shared_setup
- **Purpose**: Retain existing settings(t) and safeBuffer, adding synthetic valid Basic configuration while preserving all defaults; capture synchronized structured output.
- **Interface**: existing settings(t) returns config.Config; safeBuffer implements Write and String. No new production interface.
- **Used by**: internal/web/server_test.go, internal/web/authentication_test.go, internal/web/authentication_transport_test.go

## Criteria Mapping

| Criterion ID | Required outcomes / named variants | Evidence kind | Location / procedure | Expected result |
|---|---|---|---|---|
| CFG-001 | V1: accepted username lengths 1/128 | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V2: rejected 0/129 | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V3: accepted password lengths 1/256 | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V4: rejected 0/257 | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V5: each missing setting | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V6: username space/colon/control/non-ASCII | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V7: password control/non-ASCII | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V8: password spaces/colon accepted | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-001 | V9: invalid diagnostic values sanitized | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting. |
| CFG-002 | V1: probe ready | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-002 | V2: probe not ready | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-002 | V3: probe unavailable bounded | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-002 | V4: migrate successful | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-002 | V5: invalid database/runtime setting still fails | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-002 | V6: each command with absent/invalid Basic settings | automated test | cmd/service/authentication_test.go :: TestAuthenticationCommandIndependence | Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes. |
| CFG-003 | V1: environment changed while running: original succeeds/new fails | automated test | cmd/service/authentication_test.go :: TestAuthenticationCredentialLifetime | The running service uses its startup pair until restart; restarting uses the new pair. |
| CFG-003 | V2: restart: original fails/new succeeds | automated test | cmd/service/authentication_test.go :: TestAuthenticationCredentialLifetime | The running service uses its startup pair until restart; restarting uses the new pair. |
| AUTH-001 | V1: fresh missing header | automated test | internal/web/authentication_test.go :: TestAuthenticationMissingCredentials | A credential-free protected request returns generic challenge rejection before handler/body/side effects. |
| AUTH-001 | V2: missing header after authenticated success | automated test | internal/web/authentication_test.go :: TestAuthenticationMissingCredentials | A credential-free protected request returns generic challenge rejection before handler/body/side effects. |
| AUTH-001 | V3: GET/HEAD rejection headers | automated test | internal/web/authentication_test.go :: TestAuthenticationMissingCredentials | A credential-free protected request returns generic challenge rejection before handler/body/side effects. |
| AUTH-001 | V4: HEAD no body | automated test | internal/web/authentication_test.go :: TestAuthenticationMissingCredentials | A credential-free protected request returns generic challenge rejection before handler/body/side effects. |
| AUTH-002 | V1: wrong username | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V2: wrong password | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V3: both wrong | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V4: case mismatch in either | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V5: password leading/trailing spaces significant | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V6: supported min/max byte bounds | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-002 | V7: no trimming/normalization | automated test | internal/web/authentication_test.go :: TestAuthenticationExactCredentials | Only the exact configured username/password authenticates. |
| AUTH-003 | V1: empty field | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V2: Basic without token | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V3: unsupported scheme | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V4: malformed Base64 | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V5: missing separator | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V6: decoded empty username/password | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V7: invalid controls/non-ASCII incl invalid UTF-8 | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V8: repeated fields | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V9: mixed-case Basic | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V10: password separator colon retained | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V11: matching Basic token with legal scheme-separator SP padding to 4096 bytes accepted; invalid in-cap values still rejected | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-003 | V12: equivalent matching-token value with separator SP padding to 4097 bytes rejected before decoding | automated test | internal/web/authentication_test.go :: TestAuthenticationAuthorizationParsing | Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate. |
| AUTH-004 | V1: query-only credentials | automated test | internal/web/authentication_test.go :: TestAuthenticationCredentialSources | Only Authorization authenticates protected access. |
| AUTH-004 | V2: cookie-only credentials | automated test | internal/web/authentication_test.go :: TestAuthenticationCredentialSources | Only Authorization authenticates protected access. |
| AUTH-004 | V3: body-only credentials | automated test | internal/web/authentication_test.go :: TestAuthenticationCredentialSources | Only Authorization authenticates protected access. |
| AUTH-004 | V4: rejected body not read | automated test | internal/web/authentication_test.go :: TestAuthenticationCredentialSources | Only Authorization authenticates protected access. |
| AUTH-005 | V1: method/path/query/body intact | automated test | internal/web/authentication_test.go :: TestAuthenticationDelegation | Valid credentials delegate exactly once and preserve request and handler response. |
| AUTH-005 | V2: handler success status/header/body | automated test | internal/web/authentication_test.go :: TestAuthenticationDelegation | Valid credentials delegate exactly once and preserve request and handler response. |
| AUTH-005 | V3: handler error status/header/body | automated test | internal/web/authentication_test.go :: TestAuthenticationDelegation | Valid credentials delegate exactly once and preserve request and handler response. |
| AUTH-005 | V4: no auth-created effects | automated test | internal/web/authentication_test.go :: TestAuthenticationDelegation | Valid credentials delegate exactly once and preserve request and handler response. |
| AUTH-006 | V1: coordinated valid/invalid/missing callers | automated test | internal/web/authentication_test.go :: TestAuthenticationConcurrentIsolation | Overlapping valid and invalid requests have independent outcomes and exact handler invocation count. |
| AUTH-006 | V2: rejected body/side effects untouched | automated test | internal/web/authentication_test.go :: TestAuthenticationConcurrentIsolation | Overlapping valid and invalid requests have independent outcomes and exact handler invocation count. |
| AUTH-006 | V3: race-detected execution | automated test | internal/web/authentication_test.go :: TestAuthenticationConcurrentIsolation | Overlapping valid and invalid requests have independent outcomes and exact handler invocation count. |
| HTTP-001 | V1: GET success | automated test | internal/web/authentication_test.go :: TestAuthenticationLanding | Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body. |
| HTTP-001 | V2: HEAD success | automated test | internal/web/authentication_test.go :: TestAuthenticationLanding | Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body. |
| HTTP-001 | V3: query preserved/accepted | automated test | internal/web/authentication_test.go :: TestAuthenticationLanding | Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body. |
| HTTP-001 | V4: no credential/form/payment-control/token HTML | automated test | internal/web/authentication_test.go :: TestAuthenticationLanding | Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body. |
| HTTP-002 | V1: authenticated unsupported method on / rejected | automated test | internal/web/authentication_test.go :: TestAuthenticationRouting | Authentication precedes normal route/method handling and never makes unsupported requests successful. |
| HTTP-002 | V2: authenticated unknown path 404 | automated test | internal/web/authentication_test.go :: TestAuthenticationRouting | Authentication precedes normal route/method handling and never makes unsupported requests successful. |
| HTTP-002 | V3: missing credentials on both rejected first | automated test | internal/web/authentication_test.go :: TestAuthenticationRouting | Authentication precedes normal route/method handling and never makes unsupported requests successful. |
| HTTP-002 | V4: no downstream business work | automated test | internal/web/authentication_test.go :: TestAuthenticationRouting | Authentication precedes normal route/method handling and never makes unsupported requests successful. |
| HTTP-003 | V1: nil/ordinary empty EOF accepted | automated test | internal/web/authentication_test.go :: TestAuthenticationLandingBodyBytes | Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading. |
| HTTP-003 | V2: zero-byte EOF reader accepted | automated test | internal/web/authentication_test.go :: TestAuthenticationLandingBodyBytes | Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading. |
| HTTP-003 | V3: immediate nonempty byte rejected | automated test | internal/web/authentication_test.go :: TestAuthenticationLandingBodyBytes | Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading. |
| HTTP-003 | V4: declared length cannot bypass actual-byte inspection | automated test | internal/web/authentication_test.go :: TestAuthenticationLandingBodyBytes | Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading. |
| HTTP-003 | V5: GET/HEAD nonempty rejection and HEAD no body | automated test | internal/web/authentication_test.go :: TestAuthenticationLandingBodyBytes | Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading. |
| HTTP-004 | V1: zero-byte injected read failure | automated test | internal/web/authentication_test.go :: TestAuthenticationBodyReadFailure | An operating connection with a body-read failure receives generic 400 without false success or sensitive error details. |
| HTTP-004 | V2: byte plus read failure | automated test | internal/web/authentication_test.go :: TestAuthenticationBodyReadFailure | An operating connection with a body-read failure receives generic 400 without false success or sensitive error details. |
| HTTP-004 | V3: GET/HEAD response/header/body contracts | automated test | internal/web/authentication_test.go :: TestAuthenticationBodyReadFailure | An operating connection with a body-read failure receives generic 400 without false success or sensitive error details. |
| HTTP-004 | V4: transport/shutdown inability to respond distinguished | automated test | internal/web/authentication_test.go :: TestAuthenticationBodyReadFailure | An operating connection with a body-read failure receives generic 400 without false success or sensitive error details. |
| HTTP-005 | V1: real empty unknown-length/chunked stream accepted | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationRealStreamingBodies | Real HTTP streaming bodies obey actual emptiness and nonempty rejection. |
| HTTP-005 | V2: real nonempty chunked stream rejected | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationRealStreamingBodies | Real HTTP streaming bodies obey actual emptiness and nonempty rejection. |
| HTTP-005 | V3: no reliance only on recorder ContentLength=-1 | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationRealStreamingBodies | Real HTTP streaming bodies obey actual emptiness and nonempty rejection. |
| HTTP-006 | V1: chunked stream stalls before first byte | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationStalledBodyDeadline | A real stalled request body cannot wait beyond the foundation read budget; no false successful landing result occurs. |
| HTTP-006 | V2: configured read deadline effective | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationStalledBodyDeadline | A real stalled request body cannot wait beyond the foundation read budget; no false successful landing result occurs. |
| HTTP-006 | V3: close/transport failure allowed when response impossible | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationStalledBodyDeadline | A real stalled request body cannot wait beyond the foundation read budget; no false successful landing result occurs. |
| FND-001 | V1: GET/HEAD health/ready without credentials | automated test | internal/web/authentication_test.go :: TestAuthenticationPublicProbeBoundary | Exact probes remain public with preserved liveness/readiness/method semantics and no protected work. |
| FND-001 | V2: failed readiness/recovery | automated test | internal/web/authentication_test.go :: TestAuthenticationPublicProbeBoundary | Exact probes remain public with preserved liveness/readiness/method semantics and no protected work. |
| FND-001 | V3: readiness deadline | automated test | internal/web/authentication_test.go :: TestAuthenticationPublicProbeBoundary | Exact probes remain public with preserved liveness/readiness/method semantics and no protected work. |
| FND-001 | V4: POST/other probe method rejection | automated test | internal/web/authentication_test.go :: TestAuthenticationPublicProbeBoundary | Exact probes remain public with preserved liveness/readiness/method semantics and no protected work. |
| FND-001 | V5: health/ready prefix paths protected | automated test | internal/web/authentication_test.go :: TestAuthenticationPublicProbeBoundary | Exact probes remain public with preserved liveness/readiness/method semantics and no protected work. |
| FND-002 | V1: shutdown protected with/without credentials may return existing 503 first | automated test | internal/web/authentication_test.go :: TestAuthenticationShutdownAdmission | Shutdown admission may return existing 503 before authentication; protected work remains untouched with/without credentials. |
| FND-002 | V2: admitted authenticated work completes within grace | automated test | internal/web/server_test.go :: TestGracefulShutdownAllowsActiveRequestToFinish | Admitted authenticated work completes during grace before cleanup. |
| FND-002 | V3: overdue authenticated work canceled | automated test | internal/web/server_test.go :: TestShutdownCancelsOverdueWorkAndWaitsForCleanup | Overdue authenticated work is canceled, completes, and cleanup waits for completion. |
| FND-002 | V4: cleanup error/timeout | automated test | internal/web/server_test.go :: TestCleanupFailureIsReported; TestCleanupIsBounded | Cleanup failure is reported and a stalled cleanup stays within its configured budget. |
| FND-002 | V5: serve failure cleanup | automated test | internal/web/server_test.go :: TestServeFailureClosesResources | Serving failure closes owned resources. |
| FND-003 | V1: database unavailable/restarted readiness | automated test | internal/integration/foundation_test.go :: TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data | Readiness fails during database outage, liveness remains available, and readiness recovers after restart. |
| FND-003 | V2: retained data/migrations | automated test | internal/integration/foundation_test.go :: TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data; TestDatabaseMigrationsReadinessAndRestart/generated_readiness_query; TestDatabaseMigrationsReadinessAndRestart/empty_production_migrations; TestDatabaseMigrationsReadinessAndRestart/apply_repeat_and_rollback; TestDatabaseMigrationsReadinessAndRestart/failing_transaction_leaves_no_partial_table | Retained data/migration version survive outage/restart; direct query/migration/repeat/rollback/atomicity assertions remain intact. |
| FND-003 | V3: active authenticated PostgreSQL query cancellation | automated test | internal/integration/foundation_test.go :: TestShutdownCancelsActivePostgresQuery | Active authenticated PostgreSQL query is canceled before pool cleanup. |
| FND-003 | V4: unavailable startup deadline | automated test | internal/integration/foundation_test.go :: TestOpenRejectsUnavailableDatabaseWithinDeadline | Unavailable handshake fails within startup budget and does not expose secrets. |
| FND-004 | V1: real incomplete header deadline | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationFoundationTransport | Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior. |
| FND-004 | V2: real stalled body deadline (HTTP-006) | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationFoundationTransport | Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior. |
| FND-004 | V3: blocked write deadline | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationFoundationTransport | Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior. |
| FND-004 | V4: idle keepalive expiry | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationFoundationTransport | Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior. |
| FND-004 | V5: over-limit/malformed HTTP header transport rejection without guaranteed challenge | automated test | internal/web/authentication_transport_test.go :: TestAuthenticationFoundationTransport | Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior. |
| SEC-001 | V1: startup invalid credentials | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V2: accepted landing | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V3: missing/wrong/malformed/duplicate/oversize auth | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V4: body rejection | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V5: zero-byte read failure | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V6: HEAD errors | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V7: encoded and decoded distinctive secret sentinels | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-001 | V8: configuration value/Authorization/body leak paths | automated test | internal/web/authentication_test.go :: TestAuthenticationSanitizedOutcomes | Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes. |
| SEC-002 | V1: runtime browser assets expose no privileged credentials | runtime HTTP and source inspection | Fetch authenticated GET / and inspect actual generated HTML/browser assets with distinctive synthetic secrets | No configured/submitted credentials or privileged tokens are delivered to the browser. |
| SEC-002 | V2: no authentication Stripe calls or credential disclosure to Stripe | source inspection | Trace authentication dependency/call paths and outgoing-request construction | Authentication makes no Stripe calls and never sends local application credentials to Stripe. |
| SEC-002 | V3: comparisons avoid content-dependent early exit | source inspection | Inspect the production credential comparison path | Comparison does not use content-dependent early exit; no timing-sensitive acceptance threshold is invented. |
| BRW-001 | V1: fresh browser challenge and credential success | manual check | Fresh local browser: visit /, observe native challenge, enter synthetic pair, inspect response/cookies; independently issue credential-free HTTP request after success | Browser uses the native challenge to access the protected landing and no application sessions/login/logout are introduced. |
| BRW-001 | V2: server rejects credential-free later request | manual check | Fresh local browser: visit /, observe native challenge, enter synthetic pair, inspect response/cookies; independently issue credential-free HTTP request after success | Browser uses the native challenge to access the protected landing and no application sessions/login/logout are introduced. |
| BRW-001 | V3: no session cookie/custom login/logout | manual check | Fresh local browser: visit /, observe native challenge, enter synthetic pair, inspect response/cookies; independently issue credential-free HTTP request after success | Browser uses the native challenge to access the protected landing and no application sessions/login/logout are introduced. |
| BRW-001 | V4: browser controls reuse/prompting | manual check | Fresh local browser: visit /, observe native challenge, enter synthetic pair, inspect response/cookies; independently issue credential-free HTTP request after success | Browser uses the native challenge to access the protected landing and no application sessions/login/logout are introduced. |
| CFG-001, SEC-001 | Existing runtime diagnostic compatibility | automated test | cmd/service/main_test.go :: TestRunRejectsMissingConfiguration; internal/config/config_test.go :: existing unchanged defaults/invalid-setting tests listed in sweep | Preserve defaults/invalid runtime outcomes, no supplied secrets. |
| SEC-001 | V1: serving startup diagnostics | automated test | cmd/service/authentication_test.go :: TestAuthenticationServingConfiguration | Invalid Basic setting named without its value; exit nonzero before accepting HTTP. |
| BRW-001 | V2–V3: per-request checks and no app session | automated HTTP plus inspection | internal/web/authentication_test.go :: TestAuthenticationMissingCredentials; inspect response cookies/router/browser assets | Earlier success does not authorize missing credentials; no application session/login/logout. |

## Delivery Obligations

| Obligation ID | Source | Deliverable | Completion check |
|---|---|---|---|
| DO-001 | FR-001, FR-002, FR-010; AGENTS.md configuration/security requirements | Empty BASIC_AUTH_USERNAME/PASSWORD placeholders and local/container credential setup | Review .env.example for empty Basic credential placeholders, verify local secret files are ignored and untracked, and inspect tracked files/image-build inputs and image artifacts for credential exclusion. Review Compose wiring; resolve and launch the local application with synthetic credentials; verify values are supplied without diagnostic or browser disclosure. Never commit local credential values. |
| DO-002 | FR-007, FR-011; Acceptance Scenarios 7, 11 | Current service/browser usage and lifecycle documentation | Run documented request with placeholders replaced by synthetic values or interactive entry; perform native-browser challenge smoke check; inspect restart and browser caching/logout instructions. |
| DO-003 | Summary, Out of Scope; FR-003, FR-010 | Local sandbox security and future-route/webhook boundaries | Review docs for lack of Basic encryption, dedicated local credentials, local-only/TLS boundary, subsequent payment-route protection, and separate SDK webhook signature verification; do not claim undelivered routes exist. |
| DO-004 | Acceptance Scenarios 1–12; Success Criteria; shared quality standards | Accepted feature and foundation verification evidence | Run make verify and affected race checks using local HTTP and PostgreSQL infrastructure; report actual results and gaps without live Stripe dependencies. |

## Validation

Complete coverage, coverage accountability, no empty files, describe-block mapping, fixture/helper completeness, used-by consistency, path conventions, mapping agreement, existing identity resolution, MODIFY consistency, removal reasons, delta completeness, sweep coverage, disposition/table consistency, KEEP satisfiability, and landing legality: PASS for this plan. All 22 behavioral IDs and 108 named variants have explicit evidence rows. No behavioral test code or execution result is claimed.

Inventory discovery completed with Go 1.27.2 and produced 38 leaves; existing identities/descriptions are copied exactly. No application tests, make verify, native-browser smoke, Compose launch, credential/source inspection, or new feature verification have been performed in this planning session. These remain required delivery/runtime evidence.
