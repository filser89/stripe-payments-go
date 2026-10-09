# Implementation Plan: Authenticate service and browser access

**Feature**: `001-authenticate-service-and-browser` | **Spec**: [spec.md](spec.md)
**Tests**: [test-plan.md](test-plan.md) and the protected suite in [snapshots/post-test.json](snapshots/post-test.json).

## Summary

Serve a protected static identification page using one environment-configured Basic account. Extend the declared configuration, HTTP and entry-point packages, using Go standard-library decoding, credential comparison and `html/template`; retain the PostgreSQL, logging, transport and shutdown foundation.

Scope: seven components, zero persisted entities, eight flagged decisions, eight executable test files (seven Go files and one shell workflow script), four Delivery Obligations, and two non-automated evidence groups covering seven named variants. `.kaba/architecture.md` is absent; project rules and current code supply the architecture reference. No new third-party dependency is required.

The Go runner discovers 57 leaf identities. All ten protected source/support files match the post-test snapshot hashes. Preserve the locked tests, two SQL fixtures, workflow script and empty scaffold inventory. Planning validation establishes mappings and decisions; application correctness and runtime evidence remain pending implementation.

## Components

### C1 — `internal/config/config.go`, `internal/config/authentication.go` (configuration)

- **Responsibility**: Capture exact account strings and validate them for serving while retaining common command configuration.
- **Primary seam**: Preserve `config.Load`; add `BasicAuthUsername` and `BasicAuthPassword` to `Config`, without defaults or normalization. Add `Config.ValidateServing` for the serving account policy, returning errors that name the invalid setting without its value. Common loading retains database/address/duration/logging validation and accepts absent or invalid Basic values for local commands.
- **Greens**:
  - Common defaults and sanitized validation → `internal/config/config_test.go`: `TestLoadValidConfiguration`, `TestLoadRejectsInvalidConfigurationWithoutSecrets` and all registered children.
  - Serving ASCII/length boundaries and safe diagnostics → `cmd/service/authentication_test.go`: `TestAuthenticationServingConfiguration` (CFG-001, SEC-001 V1), through C6.
  - Independent commands and startup account capture → `cmd/service/authentication_test.go`: `TestAuthenticationCommandIndependence`, `TestAuthenticationCredentialLifetime` (CFG-002/CFG-003), through C6/C2.
  - Sanitized common startup failures → `cmd/service/main_test.go`: `TestRunRejectsMissingConfiguration`, `TestRunRejectsInvalidDatabaseWithoutExposingCredentials`.
- **Collaborators**: C6 invokes serving validation before resource acquisition. C2 validates the captured account when constructing its verifier. Never log the complete configuration.

### C2 — `internal/web/authentication.go` (HTTP authentication)

- **Responsibility**: Authenticate each protected request independently, reject before downstream/body work, and preserve successful delegation.
- **Greens**:
  - Generic challenge and missing credentials after success; HEAD headers without body → `internal/web/authentication_test.go`: `TestAuthenticationMissingCredentials` (AUTH-001, BRW-001 V2–V3).
  - Exact byte values, significant spaces/colons, case sensitivity and bounds → `internal/web/authentication_test.go`: `TestAuthenticationExactCredentials` (AUTH-002).
  - Header cardinality, cap, Basic syntax, legal separator spaces and bounded Base64 decoding → `internal/web/authentication_test.go`: `TestAuthenticationAuthorizationParsing` (AUTH-003).
  - Authorization-only credentials and untouched rejected bodies → `internal/web/authentication_test.go`: `TestAuthenticationCredentialSources` (AUTH-004).
  - Exactly one invocation with preserved method/path/query/body and successful/error response → `internal/web/authentication_test.go`: `TestAuthenticationDelegation` (AUTH-005).
  - Independent overlapping callers with immutable verifier state → `internal/web/authentication_test.go`: `TestAuthenticationConcurrentIsolation` (AUTH-006).
  - Sanitized authentication outcomes → `internal/web/authentication_test.go`: `TestAuthenticationSanitizedOutcomes` (SEC-001), with C4.
  - Startup pair remains effective until restart → `cmd/service/authentication_test.go`: `TestAuthenticationCredentialLifetime` (CFG-003), with C1/C6.
- **Collaborators**: C4 wraps the selected protected handler with C2. Use request-local parsing and private immutable comparison state; no sessions, environment reads per request or auth goroutines. Direct construction with invalid account settings must fail closed, without changing `web.New`'s signature. Private plain-text rejection support can also serve C3.

### C3 — `internal/web/landing.go`, `internal/web/templates/landing.html` (browser presentation)

- **Responsibility**: Serve the exact landing path and its body-free input contract using an embedded static template.
- **Greens**:
  - Static identification, accepted query strings, matching GET/HEAD representation headers and no sensitive content/controls/cookies → `internal/web/authentication_test.go`: `TestAuthenticationLanding` (HTTP-001).
  - Authenticated method rejection and unknown-path 404 → `internal/web/authentication_test.go`: `TestAuthenticationRouting` (HTTP-002), with C4/C2.
  - Actual body bytes versus declared lengths and clean empty EOF → `internal/web/authentication_test.go`: `TestAuthenticationLandingBodyBytes` (HTTP-003).
  - Generic zero-byte/byte-bearing read failures and HEAD semantics → `internal/web/authentication_test.go`: `TestAuthenticationBodyReadFailure` (HTTP-004).
  - Real empty/nonempty chunked streams and bounded stalled delivery → `internal/web/authentication_transport_test.go`: `TestAuthenticationRealStreamingBodies`, `TestAuthenticationStalledBodyDeadline` (HTTP-005/HTTP-006), with C4.
  - Sanitized success/body/read-error outcomes → `internal/web/authentication_test.go`: `TestAuthenticationSanitizedOutcomes` (SEC-001), with C4.
- **Collaborators**: C4 selects C3's router when `extra` is nil; C2 authenticates first. The embedded `html/template` receives no request/configuration/credential/Stripe data. Use request-local buffered rendering before response commitment and handle rendering/write errors without exposing their text.

### C4 — `internal/web/server.go` (HTTP composition, probes and lifecycle)

- **Responsibility**: Compose logging/recovery, admission, exact public probes and protected routing within the existing transport and shutdown budgets.
- **Primary seam**: Preserve `web.New`, `Server.Handler` and `Server.Serve`. A supplied `extra` remains the protected fallback handler; exact probes retain their own handlers and method rejection.
- **Greens**:
  - Authentication before ordinary route/method handling → `internal/web/authentication_test.go`: `TestAuthenticationRouting` (HTTP-002).
  - Exact public GET/HEAD probes, readiness failure/recovery/deadline, unsupported-method rejection and protected prefixes → `internal/web/authentication_test.go`: `TestAuthenticationPublicProbeBoundary` (FND-001); `internal/web/server_test.go`: `TestHealthReadinessAndSanitizedLogs`, `TestReadinessDeadline`.
  - Shutdown rejection before protected work with/without credentials → `internal/web/authentication_test.go`: `TestAuthenticationShutdownAdmission` (FND-002 V1).
  - Authenticated draining, cancellation, cleanup failures/bounds and serve-failure cleanup → `internal/web/server_test.go`: `TestGracefulShutdownAllowsActiveRequestToFinish`, `TestShutdownCancelsOverdueWorkAndWaitsForCleanup`, `TestCleanupFailureIsReported`, `TestCleanupIsBounded`, `TestServeFailureClosesResources` (FND-002 V2–V5).
  - Sanitized panic recovery → `internal/web/server_test.go`: `TestPanicProducesSanitizedFailureLog`.
  - Effective header/read/write/idle deadlines and transport header rejection → `internal/web/authentication_transport_test.go`: `TestAuthenticationFoundationTransport`, `TestAuthenticationStalledBodyDeadline` (FND-004/HTTP-006).
  - Generated request identifiers and structured feature outcomes → `internal/web/authentication_test.go`: `TestAuthenticationSanitizedOutcomes` (SEC-001).
  - PostgreSQL readiness recovery and authenticated query cancellation before cleanup → `internal/integration/foundation_test.go`: `TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data`, `TestShutdownCancelsActivePostgresQuery` (FND-003), with C5.
- **Collaborators**: Composition order: outer request logging/recovery, shutdown admission/accounting, exact-probe dispatch, then C2 around the protected router. Probe matching is exact; unsupported probe methods receive 405 without protected execution. C5 supplies readiness and C6 supplies lifecycle cancellation/cleanup. Retain timeout settings, header limits and propagated request contexts.

### C5 — `internal/postgres/postgres.go`, `internal/postgres/queries/` (existing persistence integration)

- **Responsibility**: Preserve bounded connection startup, migrations, generated readiness queries and cancellation/cleanup. Authentication requires no database changes.
- **Greens**:
  - Generated readiness, empty production migrations, repeated apply/rollback, transaction failure and retained data/version → `internal/integration/foundation_test.go`: `TestDatabaseMigrationsReadinessAndRestart/generated_readiness_query`, `TestDatabaseMigrationsReadinessAndRestart/empty_production_migrations`, `TestDatabaseMigrationsReadinessAndRestart/apply_repeat_and_rollback`, `TestDatabaseMigrationsReadinessAndRestart/failing_transaction_leaves_no_partial_table`, `TestDatabaseMigrationsReadinessAndRestart/outage_recovery_and_retained_data` (FND-003 V1–V2).
  - Unavailable startup deadline and authenticated active-query cancellation → `internal/integration/foundation_test.go`: `TestOpenRejectsUnavailableDatabaseWithinDeadline`, `TestShutdownCancelsActivePostgresQuery` (FND-003 V3–V4).
  - Actual migration execution/failure independent of Basic settings → `cmd/service/authentication_test.go`: `TestAuthenticationCommandIndependence` (CFG-002), through C6.
- **Collaborators**: Reuse current pool/migration/query implementations and SQL inputs. Keep `internal/integration/testdata/migrations/00001_fixture.sql` and `internal/integration/testdata/failing/00002_failure.sql` unchanged and test-owned. Their tables are fixture entities, not an authentication Data Model. No migration or generated-model edit is planned.

### C6 — `cmd/service/main.go` (application entry point)

- **Responsibility**: Select commands, validate serving credentials before acquiring resources, and construct the actual protected landing service with bounded startup/shutdown.
- **Greens**:
  - Real serve acceptance and nonzero invalid-config process exit before listener acceptance → `cmd/service/authentication_test.go`: `TestAuthenticationServingConfiguration` (CFG-001/SEC-001).
  - Real probe/migrate execution with absent/invalid Basic settings and existing runtime/database requirements → `cmd/service/authentication_test.go`: `TestAuthenticationCommandIndependence` (CFG-002).
  - Process-lifetime account and restart rotation → `cmd/service/authentication_test.go`: `TestAuthenticationCredentialLifetime` (CFG-003).
  - Common startup diagnostics and early unknown-command rejection → `cmd/service/main_test.go`: `TestRunRejectsMissingConfiguration`, `TestRunRejectsInvalidDatabaseWithoutExposingCredentials`, `TestRunRejectsUnknownCommandBeforeConnecting`.
- **Collaborators**: C1 supplies the startup snapshot. Serve validates before database startup/listener creation and passes the captured configuration, generated readiness query and nil `extra` to C4. Probe/migrate retain common validation and their existing HTTP/C5 execution without Basic headers. Retain sanitized main failure reporting and resource cleanup.

### C7 — `compose.yaml`, `.env.example`, `scripts/up.sh`, `Makefile`, `Dockerfile`, `.dockerignore`, `.gitignore` (local orchestration)

- **Responsibility**: Supply local account settings only to the serving container, exclude secrets from source/images and preserve ordered startup and verification tooling.
- **Greens**:
  - Local sqlc lookup, current-image migration execution, failed migration isolation and ordered successful startup → `scripts/testdata/workflows.sh`: its `make generate`, `make migrate`, failed `scripts/up.sh` and successful `scripts/up.sh` checks.
- **Collaborators**: Merge the common environment into `app` and add its two Basic variables with empty defaults; leave the migration/Stripe environments independent. C6 rejects empty serving credentials. Retain loopback publishing, pinned images, secret exclusions, the healthcheck `probe` command and existing orchestration commands. Update only necessary wiring/setup wording; do not modify the protected shell checks or their empty `.env` fixture.
- **Delivery**: DO-001 and DO-004. Runtime Compose/image checks remain explicit delivery evidence.

## Decisions

### D1 — Capture exact account values in `Config`; validate serving separately

- **Constraints/facts**: CFG-001–CFG-003, FR-001/FR-002; all commands call `config.Load`, unchanged common-config tests omit Basic settings, and web fixtures construct through that loader.
- **Directive/trade-off**: Capture strings without defaults/normalization and introduce narrow serving validation. Username is 1–128 ASCII bytes `!`–`~` excluding colon; password is 1–256 printable ASCII bytes space–`~`. Serve validates before resources; web construction also fails closed for invalid state. Credentials remain process-memory configuration and must never be formatted into logs.
- **Rejected/trap**: Mandatory account validation in common loading blocks probe/migrate. Per-request environment lookup violates account lifetime; trimming violates exact password matching.
- **Flip-point**: Re-plan ownership if separate command configuration types or credential hot reload become required.
- **Defaultability**: Applies declared environment/configuration boundaries; local representation is replaceable without external commitments.

### D2 — Put authentication inside shutdown admission and outside ordinary routing

- **Constraints/facts**: AUTH-001/AUTH-005, HTTP-002, FND-001/FND-002; `web.New` owns probes, injectable handlers, admission and logging.
- **Directive/trade-off**: Use an exact-probe dispatcher and an unexported protected-handler wrapper in `internal/web`. Retain shutdown-first 503 and authenticate before ordinary routing, redirects and method handling. The verifier is immutable and request parsing is local. This adds HTTP composition within the existing package.
- **Rejected/trap**: Protecting only `/` exposes the fallback; public prefix matching exposes probe-like paths; authenticating probes blocks public readiness; placing admission after authentication changes shutdown behavior.
- **Flip-point**: Re-plan route policy for future payment/webhook features; introduce no webhook exception here.
- **Defaultability**: Reuses the declared HTTP layer and existing handler seam.

### D3 — Check header count/size and separator spaces before standard-library Base64 decoding

- **Constraints/facts**: AUTH-003 requires one field, 4096-byte acceptance/4097-byte rejection and legal extra separator spaces. Go 1.27.2 `Request.BasicAuth` uses `Header.Get` and decodes everything after one `Basic ` prefix; it does not enforce duplicate-field rejection and rejects additional SP before the token. [RFC 9110 §11.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4) permits one or more SP separators.
- **Directive/trade-off**: Keep the few Basic header checks in an unexported helper. Apply the cap to the original header value before decoding; use `encoding/base64` for the token and preserve decoded password colons/spaces. Scheme casing is insensitive; credential bytes are exact. This owns the narrow header grammar while reusing standard decoding.
- **Rejected/trap**: `Request.BasicAuth` alone cannot meet these locked cases. Trimming decoded input changes credentials; removing arbitrary whitespace accepts malformed tokens. The transport header limit does not establish this per-field cap.
- **Flip-point**: Use another helper only if it satisfies the same grammar, cardinality and cap contract.
- **Defaultability**: Private, cheaply replaceable HTTP implementation; no new dependency/layer.

### D4 — Compare fixed-size credential digests with `crypto/subtle`

- **Constraints/facts**: FR-010/SEC-002 V3 forbid content-dependent comparison early exit; AUTH-002 requires exact supported values. Standard-library `ConstantTimeCompare` returns immediately for different slice lengths. [Go documentation](https://pkg.go.dev/crypto/subtle#ConstantTimeCompare).
- **Directive/trade-off**: Store separate username/password SHA-256 digests in the private immutable verifier; compare both candidate digests on every syntactically valid request regardless of the other comparison result. Digests stay in memory. This uses standard-library fixed-size comparison with bounded hashing cost and reliance on SHA-256 collision resistance; it introduces no persisted password format or timing threshold.
- **Rejected/trap**: Plain string equality and short-circuit username/password checks can stop early. Variable-length comparison alone retains a length shortcut. Password-storage infrastructure does not serve this process-configured account.
- **Flip-point**: Re-plan verification/storage if persisted users or credential management enter scope.
- **Defaultability**: Replaceable verifier internals using existing standard-library primitives.

### D5 — Bound landing input inspection to one actual byte and retain server read deadlines

- **Constraints/facts**: HTTP-003–HTTP-006 distinguish EOF, actual bytes, read errors, real chunking and stalled delivery; `Server.Serve` already configures `ReadTimeout`.
- **Directive/trade-off**: Use a one-byte application inspection bound, preserving clean-empty-EOF versus failure semantics and the foundation read deadline/context. No unbounded buffering or reading goroutine is needed. Transport-owned draining/closure remains `net/http` behavior.
- **Rejected/trap**: `ContentLength` is not proof of emptiness; zero bytes with an error is not clean EOF. Full-body reads violate bounded consumption; recorder checks do not prove stalled-stream deadlines.
- **Flip-point**: Future payload-bearing routes need their own planned parsers/body limits.
- **Defaultability**: Private landing handler choices within declared request deadlines.

### D6 — Embed a static `html/template` and render into request-local buffers

- **Constraints/facts**: HTTP-001/SEC-002, FR-007; project rules select `html/template`; the image builds from `internal` and has no runtime template volume.
- **Directive/trade-off**: Embed the static template in `internal/web`, use immutable parsed template state, and render without sensitive data before committing a response. Set `text/html; charset=utf-8`, `Cache-Control: no-store` and the representation `Content-Length` consistently for GET/HEAD; HEAD sends no body. Asset edits require a rebuild.
- **Rejected/trap**: Runtime file lookup couples rendering to working directory/image layout. Passing configuration into HTML creates a secret path; login/session UI and a browser framework exceed scope.
- **Flip-point**: Re-plan shared presentation structure when additional pages need it.
- **Defaultability**: Applies declared presentation technology; packaging is local and replaceable.

### D7 — Use explicit HEAD-safe feature responses and the existing sanitized outcome logger

- **Constraints/facts**: AUTH-001/HTTP-004/SEC-001; tests call both real HTTP and `Server.Handler` with recorders. Transport HEAD suppression does not apply to recorder writes. The existing logger generates request IDs and omits raw request data.
- **Directive/trade-off**: Use small private response support for application-owned errors. Authentication returns the specified challenge/content-type/no-store headers and generic `Unauthorized` content; landing input errors use generic `Bad Request` text. Suppress owned HEAD bodies explicitly and preserve delegated responses. Keep centralized completion logs; check response-write errors and use fixed sanitized error categories when additional context is needed.
- **Rejected/trap**: Transport-only suppression fails the direct handler contract. Credential-specific rejection details, raw read errors and header/query/body/configuration logging expose inputs.
- **Flip-point**: Extend fixed categories as new operations require them; richer diagnostics need a separately scoped data policy.
- **Defaultability**: Package-local response support and the established `slog` pattern.

### D8 — Supply Basic credentials only to Compose `app`, using empty interpolation defaults

- **Constraints/facts**: DO-001, CFG-002, FR-001/FR-002; Compose shares a common environment anchor, local migration/probe must work independently, and shell workflow fixtures use an empty `.env`.
- **Directive/trade-off**: Add empty placeholders in `.env.example`; merge common settings into `app` and add its Basic variables with empty defaults. Serving performs mandatory validation. Keep account values out of migrate/Stripe environments, image build arguments/layers and copied files. Document dotenv quoting for significant spaces and special characters. Retain loopback bindings and pinned versions.
- **Rejected/trap**: Mandatory Compose interpolation can block migration before command selection. Shared mandatory account settings couple commands unnecessarily. Duplicated shell validation breaks preserved fixtures and creates a second account policy.
- **Flip-point**: Re-plan secret delivery only if deployment beyond the local sandbox enters scope.
- **Defaultability**: Applies the declared environment/Compose model without an external secret service.

## Architectural Delta

Apply declared package responsibilities: serving validation in `internal/config`, private authentication and embedded landing presentation in `internal/web`, and explicit wiring in `cmd/service`. Reuse the existing PostgreSQL/sqlc/Goose, logging, HTTP lifecycle and local/CI infrastructure. No new architectural layer, third-party dependency, persistent entity, session system or external service is introduced.

## Runtime Evidence

Both groups remain pending implementation-dependent verification. Record variant-specific results or gaps in `features/001-authenticate-service-and-browser/code-implementation-report.md`, without credential values, raw Authorization, request bodies, secret-bearing diagnostics or private browser transcripts.

| Criterion ID | Required outcomes / named variants | Evidence kind | Location / procedure | Expected result |
|---|---|---|---|---|
| SEC-002 | V1 browser assets; V2 authentication Stripe boundary; V3 comparison early-exit boundary | Runtime HTTP and source/configuration inspection | Run the local service with distinctive synthetic secrets; fetch authenticated `/`, inspect actual HTML and any referenced assets, response cookies and sanitized output. Trace `cmd/service/main.go`, configuration fields, `internal/web/authentication.go`, `landing.go`, the embedded template and outgoing call construction. Inspect both fixed-size comparisons and their execution regardless of the other result. Record separate V1–V3 results without copying sentinels. | V1 no privileged credentials/tokens in browser assets; V2 no authentication Stripe calls or local credentials sent to Stripe; V3 comparisons avoid content-dependent early exit, without a timing threshold. |
| BRW-001 | V1 fresh challenge/success; V2 later credential-free rejection; V3 no session/login/logout; V4 browser-controlled reuse | Native-browser smoke check plus automated HTTP evidence | In a fresh browser profile/context visit loopback `/`, observe the native Basic prompt, enter a synthetic pair and verify the landing. Inspect cookies/routes/assets for app sessions/login/logout. Reload to observe browser-owned reuse without requiring another prompt. Independently issue credential-free GET after success and inspect the 401 challenge; pair with `TestAuthenticationMissingCredentials`. Review restart/caching documentation. | V1 native challenge then protected landing; V2 independent missing-header request stays 401; V3 no application session cookie/custom login/logout; V4 browser owns prompting/reuse and the application promises no controlled logout. |

A tool that cannot operate a native browser prompt leaves that witness pending for the human; an authenticated header fetch cannot replace it. Complete all independent implementation and checks before requesting remaining human evidence.

## Delivery Obligations

| Obligation ID | Source | Deliverable / target path | Completion check |
|---|---|---|---|
| DO-001 | Spec FR-001/FR-002/FR-010; AGENTS.md configuration/security | `.env.example`, `compose.yaml`, setup guidance in `README.md`/`scripts/up.sh`; retained `.gitignore`, `.dockerignore`, `Dockerfile` exclusions; evidence in `features/001-authenticate-service-and-browser/code-implementation-report.md` | Check empty Basic placeholders, exact policy and quoted dotenv input. Use `git check-ignore .env` and tracked-file inspection to verify ignored/untracked secret files. Inspect build-copy inputs and absence of credential build args. With synthetic credentials resolve Compose quietly (`docker compose config --quiet`); inspect resolved credential placement without printing values. Build/launch the local application and verify protected success, missing-credential 401 and public probes. Inspect final filesystem, build metadata/layers for secret exclusion and runtime/browser diagnostics for non-disclosure without printing matches. Never commit local values. |
| DO-002 | Spec FR-007/FR-011; Acceptance Scenarios 7/11 | Current setup, configuration, direct-binary/service/browser usage and account lifecycle in `README.md`; evidence in `features/001-authenticate-service-and-browser/code-implementation-report.md` | Execute documented loopback requests with synthetic values or interactive password entry, including protected success, missing credentials and probes. Perform BRW-001. Cover both setting names, byte/ASCII rules, significant spaces/colons, serving-only validation, process capture/restart rotation, native prompting/reuse, no app sessions/logout and no per-load prompt promise. Explain that direct execution requires exported variables and does not source `.env`. |
| DO-003 | Spec Summary/Out of Scope; FR-003/FR-010 | Local sandbox security and current/future route boundaries in `README.md` | Review dedicated local credentials, Basic's lack of encryption, loopback-only publishing and local-only/TLS boundary. Explain exact public probes, protected application routing, future payment-route protection checks and independent Stripe SDK webhook signature verification. Describe only delivered landing/probes; payment/webhook endpoints, remote deployment and TLS setup remain outside this feature. |
| DO-004 | Spec Acceptance Scenarios 1–12/Success Criteria; shared Q1/Q4/Q5/Q6/Q7 | Retained `Makefile`, `scripts/verify.sh`, pinned setup, `.github/workflows/verify.yml`; verification report and `snapshots/post-impl.json` | Run `make verify` with pinned tools and working Docker/PostgreSQL 18.6: gofmt, generated SQL, shell workflows, golangci-lint, govulncheck, build, uncached full tests and full race checks. Run Kaba post-implementation capture/compare and `session-lock.sh check-dirty implement` through the configured script directory; all must pass. Preserve locked sources/support and foundation assertions. Report commands/results and runtime/Delivery Obligation gaps without live Stripe dependencies. Distinguish local evidence from hosted CI; claiming hosted CI requires an observed successful Actions run. |

## Build Order

1. **C5 — Retain persistence prerequisites.** Confirm current startup/migration/query/cancellation seams and production SQL inputs; keep SQL fixtures test-owned and introduce no schema → supports all `internal/integration/foundation_test.go` groups and CFG-002.
2. **C1 — Capture account and validate serving.** Preserve common configuration → greens `internal/config/config_test.go` and common startup checks; supplies CFG-001–CFG-003 prerequisites.
3. **C2 — Establish private Basic verification and rejection.** Complete header checks, standard decoding, immutable comparisons, exact delegation and HEAD-safe rejection → supplies the authentication/delegation/concurrency groups for C4 composition.
4. **C3 — Establish static landing and bounded input.** Complete embedding, rendering, GET/HEAD representation, body/EOF/failure contracts and protected router outcomes → supplies landing/routing/body/stream groups for C4 composition.
5. **C4 — Compose the real HTTP boundary.** Wire probes and protected handlers within logging/admission; preserve transport/lifecycle → greens remaining `internal/web/authentication_test.go`, all `internal/web/authentication_transport_test.go` and `internal/web/server_test.go` groups, plus the connected PostgreSQL journey. Run focused web/race checks.
6. **C6 — Wire actual serving and independent local commands.** Validate before resources, preserve capture/rotation, probe/migrate and cleanup → greens `cmd/service/authentication_test.go` and all `cmd/service/main_test.go` groups using real command/database fixtures.
7. **C7 — Complete local credential wiring.** Add placeholders and app-only Compose settings, preserve startup/image/ignore/tooling behavior → preserves `scripts/testdata/workflows.sh` and completes DO-001 configuration deliverables.
8. **Complete current documentation.** Update `README.md` setup/configuration, requests, browser/rotation guidance and security/route boundaries → completes DO-001 guidance and DO-002/DO-003 deliverables.
9. **Establish runtime/delivery evidence.** Execute SEC-002, BRW-001, quiet Compose resolution/launch, image-secret checks and documented requests → completes the two evidence groups and DO-001–DO-003 checks. Record variant-specific results or pending witnesses in `code-implementation-report.md`.
10. **Complete verification and ownership gates.** Run `make verify`, Kaba post-implementation capture/compare and the implementation dirty-path check. Confirm locked-file integrity; finish the sanitized `code-implementation-report.md` with every obligation/check result → completes DO-004. Follow the implementation workflow's lock lifecycle only after all required checks pass; report blocked evidence honestly without weakening tests.

## Validation

- [x] Test coverage: PASS — 8/8 executable files; both protected SQL fixtures are accounted for through C5.
- [x] Greens consistency: PASS — named Go groups exist in the 57-leaf inventory; shell checks exist in the workflow script.
- [x] Schema completeness: PASS — no persisted/read authentication entities; no Data Model entries or schema changes required.
- [x] Decisions grounded: PASS — eight decisions include constraints, observed facts, traps, trade-offs and flip-points.
- [x] No escalation left silent: PASS — declared patterns or private/swappable choices; no unresolved external commitment.
- [x] Rules respected: PASS — layering, bounded lifetimes, minimal public seams, test ownership and mandatory security/verification work.
- [x] No implementation code: PASS — responsibilities, collaboration, decisions and completion procedures only.
- [x] Build order complete: PASS — C1–C7 and all delivery/evidence work appear in dependency order.
- [x] Delivery/evidence complete: PASS — DO-001–DO-004 and all SEC-002/BRW-001 variants have targets/procedures/results, explicitly pending implementation.
