# Authentication implementation status

## Delivery status

Build order: 9 / 10 steps complete. Verification step 10 is blocked by the serving-configuration subprocess deadline. The implementation session lock remains armed. No test-owned file is modified.

| Component / step | Artifacts and result |
|---|---|
| C5 persistence prerequisites | Existing `internal/postgres/`, generated queries, migrations and protected SQL fixtures retained. PostgreSQL migration, readiness, restart and cancellation checks pass. No schema change. |
| C1 configuration | `internal/config/config.go` captures exact account values; `internal/config/authentication.go` validates serving only. Common configuration tests pass. |
| C2 Basic verification | `internal/web/authentication.go` enforces one header, the 4096-byte cap, Basic separator/token grammar and exact account matching. Both fixed-size digest comparisons execute for parsed credentials with a valid configured account. Authentication, delegation and concurrency groups pass. |
| C3 landing | `internal/web/landing.go` and embedded `internal/web/templates/landing.html` provide static GET/HEAD responses and bounded actual-byte inspection. Landing, body-read failure, real streaming and stalled-body tests pass. |
| C4 HTTP boundary | `internal/web/server.go` composes logging, shutdown admission, exact probes and protected handlers. Web and connected PostgreSQL suites pass, including affected race checks. |
| C6 entry point | `cmd/service/main.go` validates serving before database/listener acquisition. Focused command suite passes, including command independence and restart rotation. Aggregate startup witness has an execution-budget failure described below. |
| C7 local wiring | `.env.example` contains empty Basic placeholders; `compose.yaml` supplies them only to `app`. `scripts/up.sh` describes required local credentials. Protected shell workflow checks pass. |
| Documentation | `README.md` describes setup, exact credential policy, dotenv quoting, exported direct-binary configuration, native browser prompting/reuse, restart rotation and current security/route boundaries. |
| Runtime evidence | All SEC-002 and BRW-001 variants verified below. |
| End gates | Snapshot compare and aggregate quality command fail on the startup witness. Ownership passes. |

## Verification

Environment: Go 1.27.2 on darwin/arm64, Docker Desktop 29.7.2, PostgreSQL 18.6 Alpine. Integration/transport commands require local listener and Docker access beyond the execution sandbox. No live Stripe calls or credentials are used.

| Command / gate | Actual result |
|---|---|
| `go test -count=1 ./internal/integration ./internal/config -run 'TestDatabaseMigrationsReadinessAndRestart\|TestOpenRejectsUnavailableDatabaseWithinDeadline\|TestLoad'` | PASS |
| `go test -count=1 ./internal/web ./internal/integration ./internal/config` | PASS |
| `go test -race -count=1 ./internal/web ./internal/integration` | PASS |
| `go test -count=1 ./cmd/service` | PASS in focused execution; aggregate execution exposes the startup-budget blocker. |
| `go test -count=1 -run TestAuthenticationServingConfiguration ./cmd/service` | PASS in focused execution. |
| `./scripts/testdata/workflows.sh` | PASS: generation lookup, current migrations, failed-start isolation and ordered startup. |
| Kaba `snapshot-tests.sh capture post-impl` | Capture succeeds. `snapshots/post-impl.json`: 57 total, 56 passed, 1 failed, 0 pending. Post-test snapshot contains 15 failed leaves. |
| Kaba `snapshot-tests.sh compare post-impl` | FAIL: `cmd/service::TestAuthenticationServingConfiguration` must pass. No added or removed tests; protected inventory matches all 10 locked source/support files. |
| Kaba `session-lock.sh check-dirty implement` | PASS |
| `make verify` | FAIL in uncached full tests on the same serving startup witness. Formatting, SQL generation, shell workflows, golangci-lint (0 issues), govulncheck (no vulnerabilities) and build pass. Its race stage is not reached. |
| `go test -race -count=1 -timeout=5m ./...` | PASS across the full suite, including serving configuration, transport and PostgreSQL tests. |
| `git diff --check` | PASS |
| Hosted GitHub Actions | Unverified; no observed hosted run. Local and CI retain the same `make verify` command. |

### Blocking startup witness

`cmd/service/authentication_test.go:192` gives a freshly built subprocess 500 ms to exit. In aggregate runs, the first invalid-username process is killed before it emits its diagnostic. Assertions require a normal nonzero exit and structured setting diagnostics, so the test fails correctly for its current execution budget. No listener acceptance is observed. The serving entry point validates the account before resource acquisition.

A separate probe builds three executables and measures three invalid-account launches each, allowing time for observation without changing production code or the locked suite:

| Build | First launch | Second launch | Third launch |
|---|---|---|---|
| 1 | 714 ms | 19 ms | 15 ms |
| 2 | 495 ms | 18 ms | 16 ms |
| 3 | 490 ms | 12 ms | 10 ms |

All nine launches exit normally with status 1 and the expected invalid-setting diagnostic. These observations establish first-execution latency near or above the locked deadline; they do not identify whether the cost belongs to operating-system executable handling, runtime initialization or scheduling. Sanitized measurements are in `.work/auth-evidence/startup-results.json`; the reproduction harness is `.work/auth-evidence/startup.py`.

Re-planning must address the executable-startup verification budget while retaining nonzero exit, safe diagnostics, invalid-account rejection and absence of listener acceptance. Changing locked tests or adding warm-up behavior to production code is outside this implementation session. No exemption, skipped assertion, altered test command or quality-gate workaround is applied.

## Runtime evidence

The isolated Compose project uses synthetic credentials, separate loopback ports and a disposable PostgreSQL volume. The application image builds and the stack starts with the standard service migration command. Basic values containing significant spaces, a colon, dollar and hash characters arrive exactly in the running container and authenticate successfully. Rendered Compose configuration escapes dollar signs for serialization; container inspection and actual HTTP authentication establish value preservation.

| Criterion / variant | Actual evidence / result |
|---|---|
| SEC-002 V1 browser assets | PASS: authenticated running-image HTML identifies the service and contains none of the synthetic Basic/database/Stripe values or encoded Authorization token. No referenced scripts/styles, forms, payment controls or response cookies. |
| SEC-002 V2 Stripe boundary | PASS: source inspection of command wiring, configuration, HTTP authentication, landing and template shows no Stripe dependency/call or outgoing authentication request construction. Basic settings are absent from migration and Stripe service environments. |
| SEC-002 V3 comparison boundary | PASS: `internal/web/authentication.go` hashes both supplied values separately and executes both `crypto/subtle.ConstantTimeCompare` calls over fixed-size SHA-256 arrays before combining results. No content-dependent comparison short circuit or timing threshold. |
| BRW-001 V1 fresh challenge/success | PASS: a fresh Safari private window at loopback displays the native HTTP authentication prompt. Entering the synthetic pair displays the protected landing. |
| BRW-001 V2 later missing-header rejection | PASS: an independent credential-free HTTP GET after Safari success returns 401 with the specified Basic challenge and no session cookie. Locked missing-credential tests also pass. |
| BRW-001 V3 no application session/login/logout | PASS: actual HTML, response headers and source routing contain no application session cookie, custom login or logout controls/routes. |
| BRW-001 V4 browser-controlled reuse | PASS: reload in the same Safari private window displays the landing without another prompt. Documentation describes browser-owned reuse and restart rotation without promising per-load prompting or controlled logout. |

The in-app browser blocks navigation to this Basic-protected URL and Chrome control is unavailable; Safari provides the native witness. Sanitized runtime results are in `.work/auth-evidence/results.json`, with a landing screenshot in `.work/auth-evidence/browser.png`.

## Delivery obligations

| Obligation | Completion-check result |
|---|---|
| DO-001 | PASS: empty placeholders, app-only Compose credentials, quoted exact values, ignored/untracked local secrets, quiet Compose resolution, actual image build/launch, successful protected HTTP, missing-header 401 and public GET/HEAD probes. Image metadata/history, saved image layers including compressed OCI blobs and exported final filesystem contain no synthetic values, local `.env` or protected test fixtures. Runtime logs exclude sentinels. |
| DO-002 | PASS: current service/browser usage and lifecycle guidance in `README.md`; running-image GET/HEAD, independent 401 and public probes verified with synthetic values. Native Safari challenge/success/reload checked. Command tests verify direct serving and rotation; documented direct execution requires exported variables and does not source `.env`. |
| DO-003 | PASS: documentation review confirms dedicated local credentials, Basic's lack of encryption, loopback-only local scope, TLS boundary, exact probes, future protected payment routes and independent future webhook signature verification. No undelivered endpoint is claimed. |
| DO-004 | BLOCKED: full race checks pass and locked ownership is preserved; aggregate quality command and post-implementation compare fail on the 500 ms startup witness. Hosted CI remains unverified. |

The temporary private browser window, isolated Compose containers, disposable database volume and generated credential files are removed. Sanitized evidence remains in ignored `.work/auth-evidence/`.

No required runtime/manual variants remain unverified. Full delivery is blocked by the regular suite/quality gate. After resolution, every end gate must pass before clearing the session lock; `/kaba:architecture-diff` is the mandatory next feature step after successful implementation completion.
