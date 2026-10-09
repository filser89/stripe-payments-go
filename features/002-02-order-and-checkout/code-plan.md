# Implementation Plan: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout` | **Spec**: [spec.md](spec.md) | **Criteria**: [acceptance-criteria.md](acceptance-criteria.md)
**Tests**: [locked test plan](test-plan.md), normal `go test ./...` suite, and [post-test inventory](snapshots/post-test.json).

## Summary

Deliver authenticated order creation, hosted Checkout, safe continuation, and local status/history using the declared HTTP → payment → PostgreSQL/Stripe boundaries. PostgreSQL owns durable identity, transaction guards, dispatch ownership and audit consistency; payment owns evidence policy and bounded retries; the Stripe adapter makes one SDK wire attempt per invocation.

## Implementation boundary and frozen contract

- Accepted implementation-planning checkpoint: `e255850452f9eb40a28ed22ea54dc7568b890a33`; test source: `cc601ac0eee3e59a357de22aca71802abcf41785`. Native inventory: 662 complete leaves, 65 passed, 597 expected missing-behavior failures, zero pending, 597 implementation targets. The schema-v3 plan has one MODIFY and 65 TOUCH entries. Five compilation scaffolds remain test-owned.
- Integrity authorities: baseline SHA256 `5b38d89b84226f36445dbb9c79a4dfc27d3aeb804b49aa139d48605a5db6689c`; post-test SHA256 `8faec0762c545d246a580c6833cd06da820d84ec3ae5eb1cfe5f08772e752030`; plan-lock SHA256 `97dd44d7dd0e1706fefd509b68f4c84af6a40b335010e93ca6b30fd6936cbb93`. Preserve the committed test source/support/fixtures/scaffolds, specification/criteria/test plan/JSON/locked evidence, execution configuration, and go.mod/go.sum pins. Descriptive prose counts in the test plan are not authority over this native inventory.
- Runtime source: `fix/behavior-evidence-contracts@638047e7d671b86a061516dad300200fa0ade032`; snapshot script SHA256 `180b0d66d72d3f0cb956298ecfb5655ad1b7e7e0470041a375c11ffcacc33663`. Resolve `kaba.scriptdir` at implementation entry; retain normal hooks/gates. Planning leaves the existing test-mode lock armed; implement-code owns its normal entry transition.
- Public seams are the declarations in the five frozen `internal/*/testdata/kaba/002-02-order-and-checkout/contracts.go` files and their actual callers. Implement these as production declarations without editing or copying placeholder bodies into behavior. `Operations`, `Repository`, `Gateway`, intent/result structs, `NewCheckoutHandler`, `NewPaymentRepository`, and `CheckoutSettings` keep their tested signatures and semantics. Payment has no SQL, SDK, HTTP-server, or testutil dependency.
- The integration branch is `main`; actual application/architecture comparison base is `faddd6a120b9087f2aa075b4b2074a110eb471ef`. Reuse the architecture's declared layers and patterns; its `master` metadata does not select the integration branch.
- R21/R23/R24/R25 are cleared at the accepted checkpoint. R26 remains an automated logging-oracle limitation: generic metadata can satisfy `OutcomeLogContext`. Acceptance permits implementation planning with mandatory SEC-003 source/runtime verification below. Metadata-only logs do not satisfy the feature, and green helper results cannot close R26 or SEC-003. No helper, manifest, gate, or test edits belong to implementation.
- No Feature 3 event/signature/paid-confirmation processing, Feature 4 browser controls, or Feature 5 session listing/manual command/workers are delivered here. Read/guard paid fixtures without implementing confirmation. Preserve durable first/last-dispatch data for their future contracts.

## Components

### C1 — `internal/payment/contracts.go`, `errors.go` (business contracts)

- **Responsibility**: Production domain values and the tested three interfaces; safe classified errors with optional known IDs and preserved causes; independent random UUIDv4 order/operation/Stripe identities.
- **Greens**: Typed construction and identity → `internal/payment/checkout_test.go` / `TestID003CheckoutRules`; error/outcome distinctions → `internal/payment/recovery_test.go` / `TestSTR008RecoveryRules`; durable identity → `internal/integration/orders_test.go` / `TestConnectedOrderCheckoutAndInspection`.
- **Collaborators**: Plain values consumed by service, repository, adapter and web. Add internal typed failure context where necessary; never classify arbitrary error-message substrings.

### C2 — `internal/payment/service.go`, `lifecycle.go` (application operations)

- **Responsibility**: `Create`, `Continue`, `Get`, and `History`; replay syntax/binding ordering; original-operation views; authoritative purchase validation; lifecycle/evidence validation; exact action and investigation predicates.
- **Greens**: Initial/distinct identity and LIFE-001–009 → every `TestLIFE*CheckoutRules` and `TestID003CheckoutRules` in `internal/payment/checkout_test.go`; age/investigation → `TestREC003RecoveryRules`/`TestREC004RecoveryRules` in `internal/payment/recovery_test.go`; local-only reads and historical bindings → `internal/integration/orders_test.go`, `checkout_lifecycle_test.go` / `TestConnectedCheckoutLifecycle`, `TestConnectedGlobalHistoricalAndReusableBindings`, `TestConnectedSDKCorrelationMismatchBlocksReplacement`.
- **Collaborators**: Repository returns committed current/bound values; gateway returns untrusted evidence. Outcome reports new operation versus established replay versus accepted pending; web encodes statuses. Reads never dispatch, mutate history, or clear ownership.

### C3 — `internal/payment/dispatch.go`, `budget.go` (external-call coordination)

- **Responsibility**: One request-local combined attempt/elapsed budget, cancellation-aware waits and per-call deadlines; committed marker before every possible POST; same snapshot/key retry; conservative ambiguity; persistence-failure recovery and sanitized business outcome logs.
- **Greens**: STR-004–008 and each-send bookkeeping → all `TestSTR*RecoveryRules` and `TestEachReplayDispatchBookkeepingIsDurable` in `internal/payment/recovery_test.go`; connected recovery → all groups in `internal/integration/checkout_recovery_test.go`; wire budgets, real DB waits, backoff, shutdown → all groups in `internal/integration/checkout_deadlines_test.go`; independent progress/stale results → all groups in `internal/integration/checkout_concurrency_test.go`.
- **Collaborators**: Short repository transactions surround synchronous gateway calls. No background retry/heartbeat goroutine, process-global retry counter, or detached external work.

### C4 — `db/migrations/00001_payment.sql`, `db/schema.sql`, `db/queries/payment.sql`, `internal/postgres/queries/` (schema and generated access)

- **Responsibility**: Four versioned relations, typed parameterized queries, constraint/immutability guards, and history sequencing; generated pgx access from authoritative SQL.
- **Greens**: Actual schema, constraint collision and rollback witnesses → `internal/postgres/payment_test.go` / `TestDATA001PaymentPersistence`, `TestDATA002PaymentPersistence`, `TestAssociatedStripeIDConstraintsAndSnapshotImmutability`, `TestPaymentTransactionPhaseMatrix`; concurrency invariants → `internal/postgres/payment_concurrency_test.go`; normal application migrations → connected suites and `cmd/service/checkout_test.go`.
- **Collaborators**: Existing Goose runner and sqlc configuration. Extend business SQL separately from `health.sql`; retain the foundation's genuinely empty supplied-source behavior.

### C5 — `internal/postgres/payment.go`, `payment_transactions.go` (persistence)

- **Responsibility**: Implement the tested Repository with consistent row-lock order, exact binding comparisons, guarded dispatch/result application, durable lease fencing, committed reads, append-only history and safe error translation.
- **Greens**: Acceptance/dispatch/observation/replacement atomicity and pagination → all groups in `internal/postgres/payment_test.go`; independent acceptance/replacement/paid/current/owner guards and network-free transaction boundaries → all groups in `internal/postgres/payment_concurrency_test.go`; database failures/restart/ownership takeover → connected recovery/concurrency/deadline suites.
- **Collaborators**: pgxpool, transaction-bound generated Queries, payment values. Every begin/query/scan/rows/commit error is checked. Never return a successful view until commit succeeds.

### C6 — `internal/stripeapi/checkout.go`, `errors.go` (SDK integration)

- **Responsibility**: Construct a per-client official v87 backend, encode the immutable hosted-card snapshot, make one Create/Retrieve invocation, translate full remote fields and sanitized failure/retry-header facts without business decisions.
- **Greens**: Request/key/disabled pricing options → `internal/stripeapi/checkout_test.go` / `TestSTR001SDKRequest`; complete wire fidelity → `TestSDKCheckoutEvidenceTranslation`, `TestSDKFullWireEvidenceFidelity`; structured errors/headers/real blocking headers and body cancellation/no hidden retry → all groups in `internal/stripeapi/transport_test.go`.
- **Collaborators**: `stripe.NewClient` with `stripe.WithBackends`, `NewBackendsWithConfig`, explicit `MaxNetworkRetries=0` and SDK logger level null; tested BackendURL/HTTPClient injection stays construction-only. Production HTTP client uses the default transport so existing command redirect witnesses remain valid. No global stripe key/backend mutation and no new serving endpoint override.

### C7 — `internal/config/checkout.go`, existing `config.go`/`authentication.go` (captured configuration)

- **Responsibility**: Capture checkout settings once, expose typed defaults through CheckoutSettings, validate checkout syntax/ranges/cross-setting relationships only for serve, and preserve independent Basic construction validation.
- **Greens**: Exact configured values/defaults/invalid boundaries → all groups in `internal/config/checkout_test.go`; 11s read default and common validation → `internal/config/config_test.go`; migrate/probe independence and real serving validation → `cmd/service/authentication_test.go` / `TestAuthenticationCommandIndependence`, `TestAuthenticationServingConfiguration`, `TestAuthenticationCredentialLifetime`, and `cmd/service/checkout_test.go` / `TestCheckoutServingValidation`.
- **Collaborators**: `run` calls full ValidateServing before acquiring DB/listener. `web.authenticate` validates only the captured Basic account: isolated foundation handlers without checkout settings remain usable. Reuse one Basic validator across these paths; do not turn absent Stripe configuration into 401 for standalone web.New fixtures. Invalid serving settings identify setting names without values; migrate/probe still validate common foundation settings.

### C8 — `internal/web/checkout.go`, `checkout_input.go` (protected HTTP dispatch)

- **Responsibility**: Exact four API route shapes, canonical IDs/methods/Allow, strict duplicate-aware JSON/input/query parsing, bounded actual streams, and delegation to Operations. Compose the existing root landing handler inside this protected extra.
- **Greens**: Inputs and effect absence → every group in `internal/web/checkout_validation_test.go`; route/HEAD behavior → `internal/web/checkout_routing_test.go` / `TestCheckoutEndpointRouting`, `TestCheckoutReadErrorHEADParity`; each protected endpoint → authentication/isolation groups in `internal/web/checkout_security_test.go`; actual valid/invalid connected effects → `internal/integration/orders_test.go` / `TestConnectedInputAndAuthenticationEffects`, `TestConnectedAcceptedPurchasePreservation`.
- **Collaborators**: Existing outer authentication/admission precedes routing/parsing. Syntactic token validation happens here; new-order configurable limits are payment policy after durable key lookup. Handlers use exact path dispatch without automatic redirects/cleaning that alter canonical resources.

### C9 — `internal/web/checkout_response.go`, `checkout_deadline.go`, existing `server.go`/`authentication.go` (transport, presentation, correlation)

- **Responsibility**: Request deadline at protected feature admission before body reads; actual read/write deadline and cancellation enforcement; explicit allowlisted JSON/null/UTC/HEAD rendering; stable status/errors and known IDs; correlated semantic outcome and rejection logging.
- **Greens**: Envelope/status/pagination → `internal/web/orders_create_test.go` / `TestCheckoutCreateResponses`, `TestHTTP004CreateResponse`; `orders_checkout_test.go` / `TestCheckoutContinuationResponses`; `orders_read_test.go` / `TestOrderReadResponses`; `orders_history_test.go` / both named groups; semantic capture → log groups in `internal/web/checkout_security_test.go`; real streams/blocked writes → all groups in `internal/web/checkout_transport_test.go`.
- **Greens (retained foundation)**: Public probes, shutdown, panic/cleanup and transport → `internal/web/server_test.go`, `authentication_test.go`, `authentication_transport_test.go`; ASCII Basic and exactly one body-read category → `authentication_audit_test.go`; migrated DB outage/readiness/shutdown → `internal/integration/foundation_test.go`. Keep their named groups/normal handler injection operational.
- **Collaborators**: Outer generated request ID is also placed in request context for payment logs. Authentication rejection logs fixed auth outcome without parsing a durable resource from untrusted path/body. ResponseController accesses the real writer via existing Unwrap; unsupported controllers in recorder tests remain valid for finite bodies. Cancellation callbacks only interrupt I/O and are stopped/joined before handler completion.

### C10 — `cmd/service/main.go`, `db/queries/health.sql` (composition/readiness)

- **Responsibility**: Wire captured serving config → pool/repository → Stripe client → payment service → protected Checkout handler; keep migrate/probe independent; verify schema availability in the serving readiness path without a live Stripe readiness call.
- **Greens**: Real composition/captured limits/origin/call timeout → `cmd/service/checkout_test.go` / `TestCheckoutCommandWiring`, `TestCheckoutCustomCallBudgetAndAttempts`, `TestCheckoutServingValidation`; command errors → `cmd/service/main_test.go`; retained serving/account/command behaviors → `cmd/service/authentication_test.go`.
- **Collaborators**: Retain generated basic CheckReady for isolated foundation databases; add a separate typed payment-schema readiness check to actual serve wiring. Missing/failed migrations cannot yield ready application endpoints. Existing signal/request draining and nonzero failure exit remain composition-owned.

### C11 — `README.md`, `docs/api.md`, `docs/recovery.md`, `docs/architecture.md`, `.env.example`, `compose.yaml`, `scripts/up.sh` (delivery)

- **Responsibility**: Usable current API/configuration/setup/lifecycle/architecture documentation and app-only serving settings; preserve existing ordered migrations, loopback ports, probes, secret exclusions, and main CI/local verification.
- **Greens**: No invented document tests. Existing `scripts/testdata/workflows.sh` and foundation tests retain their checks; NE-1–6 and DO-001–006 establish delivery evidence.
- **Collaborators**: Existing Makefile/scripts/CI remain authoritative. Architecture-diff updates `.kaba/architecture.md` in its separate authorized phase.

### Locked test-source coverage

The 29 runtime test files cited under C1–C10 equal all distinct source files in the frozen post-test examples. Every file has a production path; package-local support (`clock_test.go`, `fixtures_test.go`, `gateway_fake_test.go`, `repository_fake_test.go`, `checkout_helpers_test.go`), shared testutil and five scaffold files are preserved dependencies, not additional runtime test files. Components cite actual named groups or all actual groups in a file, including scoped correction cases, rather than generated-plan family names that differ from source.

## Data Model

Use PostgreSQL UUID for local identities, bigint for minor units/counters/Unix expiry, text for constrained states/opaque external IDs, timestamptz for observed times, and JSONB only for the complete bounded immutable creation snapshot. Domain projections preserve nil versus populated fields. Times crossing persistence use PostgreSQL microsecond precision consistently; outward encoding is UTC RFC3339Nano. No cart, customer/card, raw error/payload, webhook queue, or worker tables.

### `payment_orders`

- **Columns**: `id uuid` primary key; `description text`, `amount bigint`, `currency text`, `payment_status text` default unpaid, `current_operation_id uuid`, `created_at timestamptz`, `updated_at timestamptz`, all nonnull; `history_sequence bigint` nonnull default 0.
- **Indexes**: Primary key. Operations provide unique `(order_id,id)` for the same-order current reference.
- **Constraints/FKs**: Amount 50–100000 and currency exactly usd, status unpaid/paid, description length 1–200; nonnegative sequence. Deferred composite FK `(id,current_operation_id)` → operations `(order_id,id)` permits atomic initial circular insertion while requiring a durable same-order current operation at commit. No cascading deletion. Immutable purchase columns through a trigger; paid cannot regress. Configurable narrower limits apply to new acceptance in payment, not stored rows/replay.

### `payment_operations`

- **Columns**: `id uuid` PK; `order_id uuid`, `state text`, `stripe_key text`, `snapshot jsonb`, `version bigint` default 1, `owner_token text` default empty, `prior_ambiguity boolean` default false, `evidence_source text` default empty, `investigation_required boolean` default false, `created_at`/`updated_at timestamptz`, all nonnull. Nullable `stripe_session_id text`, `stripe_payment_intent_id text`, `checkout_url text`, `expires_at timestamptz` (last validated remote expiry), `first_dispatch_at timestamptz`, `last_dispatch_at timestamptz`, `dispatch_expires_at bigint` (immutable requested expiry), `lease_until timestamptz`, `last_observed_at timestamptz`, `stripe_request_id text`, `failure_code text`.
- **Snapshot**: Complete accepted order/operation IDs, description/amount/usd, independent Stripe key, credential-free success/cancel URLs, SDK/API version, hosted_page/payment/card/quantity1/immediate automatic capture, line-item inline pricing, both metadata maps, disabled tax/promotion/adaptive pricing/after-expiration recovery, and one-time first-dispatch/requested-expiry finalization. Last possible dispatch is mutable bookkeeping outside immutable wire JSON; populate public Snapshot.LastDispatchAt from the separate column. Snapshot.FirstDispatchAt and ExpiresAt reflect immutable finalized fields. Remote observed expiry never rewrites requested expiry.
- **Indexes**: Unique stripe_key (length 1–255); unique `(order_id,id)`; nullable unique session/PaymentIntent indexes; `(order_id,created_at,id)` for prior-attempt checks. Partial unique order_id for states prepared/unresolved/open/complete_unpaid/paid establishes at most one potentially active/confirmed operation. Current-operation membership and all-prior eligibility still require transactional guards; the partial index alone is insufficient.
- **Constraints/FKs**: Order FK with restricted deletion; allowed seven states, positive version, nonempty associated IDs when present, coherent first/last/requested-expiry tuple and last >= first. Trigger preserves order/key/purchase/URLs/versions/fixed options and finalized first/requested-expiry, permitting only their one-time prepared finalization and mutable dispatch/observation bookkeeping. Repository checks snapshot matches order and identity. Do not add a state-coupling check that prevents trusted paid or stale-owner test prerequisites from being installed independently.
- **Ownership**: lease_until is only advanced by a committed mutation-dispatch claim/renewal. Release clears owner_token and advances version/updated_at while retaining lease_until, so result-transaction failure releases coordination without changing business rows, evidence, snapshot, or history. Established known-ID reads do not change lease or dispatch timestamps. An expired lease is reclaimable using database time plus a version/token fence; no external send follows a failed claim.

### `payment_request_bindings`

- **Columns**: `request_key uuid` PK; `method text` fixed POST; `target text`, `order_id uuid`, `operation_id uuid`, `description text`, `amount bigint`, `currency text`, `created_at timestamptz`, all nonnull. Store complete accepted binding; View is a query projection, not a saved/rewritable response.
- **Indexes**: Global key PK; `(order_id,operation_id)` for association.
- **Constraints/FKs**: Composite same-order operation FK plus order FK, restricted deletion; request UUIDv4 variant and exact initial or canonical order-checkout target; accepted money/usd constraints. Immutable insert-only binding columns; no expiry/deletion/reassignment. LoadBinding joins current order payment state to the original bound operation. Initial comparison includes all accepted purchase/target facts before new-order limits; equal collision returns winner, changed collision conflicts atomically.

### `payment_history`

- **Columns**: `(order_id uuid, sequence bigint)` PK; `kind text`, `recorded_at timestamptz`, nonnull. Nullable `operation_id uuid`, `from_state text`, `to_state text`, `stripe_session_id`, `stripe_payment_intent_id`, `stripe_event_id`, `stripe_request_id`, `failure_code text`, `event_at timestamptz`. No raw payload/message, URL or purchase description in audit rows.
- **Indexes**: PK serves ascending per-order cursor reads; operation association index. A future event-ID dedup mechanism is not implemented here.
- **Constraints/FKs**: Positive sequence, allowlisted six kinds and nullable state enums; order FK, nullable same-order operation FK, restricted deletion. Allocate sequence from order counter under the same order lock/transaction as state/history insertion. Initial acceptance adds order_created and operation_prepared exactly once. First/subsequent actual dispatch markers and actual state changes get relevant entries; unchanged observation/read/established replay adds no transition. History has no normal UPDATE/DELETE queries; migration trigger rejects row modification/deletion. Event time is retained independently of sequence/observation time.

## Decisions

### D1 — Apply the declared package boundaries and tested result contracts

- **Constraints/facts**: AGENTS declares web/payment/postgres/stripeapi; frozen fakes prove these exact seams and independent rule tests.
- **Rejected trap**: Business rules in HTTP or adapter couple fake policy evidence to transport and duplicate later-entry-point logic. Broad repository/service interfaces beyond the tested needs add no value.
- **Trade-off/flip-point**: Modest responsibility-based files inside existing packages; split packages only when an actual independently owned concern appears.

### D2 — Use locked order-row transactions and constraint-backed acceptance

- **Constraints/facts**: Real independent-pool collisions, atomic commit-fault matrices, all-prior/paid/current guards; network locks must be released.
- **Directive**: Short pgx transactions lock order then relevant operations in stable ID order, compare version/current/paid, and save history atomically. Initial global-key conflict uses PostgreSQL uniqueness and full transaction rollback/reload of the committed winner. Never use an upsert to replace accepted input. BindContinuation rechecks every older operation is safe, not only its supplied predecessor.
- **Rejected trap**: Process mutex or isolated unique key leaves replacement/paid/stale-observation races unguarded.
- **Trade-off/flip-point**: Serialization is per order, allowing independent orders to progress; revisit lock strategy only with measured contention and equivalent invariants.

### D3 — Separate immutable wire identity from mutable dispatch/observation bookkeeping

- **Constraints/facts**: Actual wire equality, snapshot restart comparisons, first/last observer checks, and exact establishedReplayRows allowance.
- **Directive**: Persist all wire parameters before send; finalize first dispatch/requested expiry once; update last possible dispatch before each POST only. Keep remote observed expiry separate. Get and historical replay project saved data without replacing the bound operation.
- **Rejected trap**: Serializing mutable LastDispatchAt into wire snapshot or recomputing origin/expiry after restart changes the same-key request. Reusing current-operation View for a bound historical response silently changes identity.
- **Trade-off/flip-point**: Explicit snapshot fields plus JSONB duplicate some purchase facts but provide inspectable recovery evidence. Any snapshot-version migration requires separately reviewed behavior; unsupported snapshots remain unresolved.

### D4 — Fence mutation ownership with a bounded database lease

- **Constraints/facts**: Active duplicate must return pending without a second call; crash recovery cannot require an in-memory mutex; canceled/failed commit paths preserve recoverable identity and exact business rows.
- **Directive**: Each possible mutation acquires/reacquires a token/version claim with a fixed 12-second database-time lease (validated request maximum 10s plus margin). Renew before later sends only while holding the matching token/version, and recheck paid/current/all-prior safety. Post-call result application requires exact version/token/current guard. Conditional same-state observation with no business evidence/history change can release an owned claim after failure. Cancellation stops calls/retries and performs only this owned coordination cleanup synchronously, using a maximum 100ms cleanup context capped by the captured effective absolute request deadline and shutdown completion budget; retain the cancellation cause in outcome/logging. An already expired absolute deadline or unavailable DB leaves the claim for expiry. Release preserves lease_until, ambiguity, failure/evidence fields and every business/history value; no detached cleanup continues after the operation returns. This permits immediate fresh-instance recovery after a canceled pre-wire marker when the DB remains available. Clear neither another owner's token nor stale result state. Read projection exposes expired ownership as reclaimable without mutating persisted state; PrepareDispatch performs the actual fenced takeover.
- **Rejected trap**: Permanent nonempty owner tokens strand crashes; blindly clearing a token on restart permits live overlap; lease expiry alone proves neither no creation nor replacement eligibility.
- **Trade-off/flip-point**: A crash may require up to 12s before another process reclaims the same mutation. Known-ID GETs can run without a mutation claim, using optimistic version/current/token checks for observations; parallel reads do not create external effects. No request-lifetime network transaction or lease-renewal goroutine is required. Revisit duration only if serving bounds change; keep lease at least as long as bounded owned request work.

### D5 — Preserve ambiguity before send and restrict definitive rejection

- **Constraints/facts**: Pre-wire cancellation and response-loss restart must remain unresolved; first structured rejection can be rejected; rejection after an earlier uncertain send cannot.
- **Directive**: Save conservative possible-send ambiguity before each POST. Only the still-owning caller with proof of no earlier ambiguous dispatch can apply a fully observed validation/credential/permission first rejection and discharge that marker. Any interruption, failed persistence, transient/generic/idempotency/malformed outcome preserves ambiguity. Recheck strict 23h age before every possible POST, including after waits; known IDs use GET. Store sanitized failure classes and evidence provenance; investigation uses 15m/23h/mismatch/5xx/version/rejection triggers without spawning work.
- **Rejected trap**: Marking timeout or cached 5xx rejected permits another charge; treating a recovered validation rejection as proof about earlier work loses history.
- **Trade-off/flip-point**: Conservative unresolved outcomes require later evidence/operator investigation. Broaden definitive classification only with an approved, verifiable no-execution contract.

### D6 — Share one external budget across all request calls and waits

- **Constraints/facts**: Limits 1/2/3, mixed GET+POST counts, controlled fake time, actual elapsed-exhaustion and last-call-cap witnesses.
- **Directive**: Request-local attempt counter covers every gateway invocation, with SDK retries disabled. Shared elapsed clock starts with the external-work phase no later than its first call and is not reset after retrieval/preparation. Track elapsed through injected Now/Wait for policy tests and actual contexts for live I/O; earlier real parent deadline always wins. Calls are capped by call timeout, remaining retry budget and request deadline minus the one-second final-work reserve. Retries of a failed invocation use required 250ms/500ms or longer valid Retry-After; a successful eligibility GET followed by the first creation POST is not a retry and requires no intervening backoff. The wire ceiling remains combined across both methods. Honor should-retry false and never start a call when insufficient budget/age remains. Eligible committed preparation may complete after external budget expiry and return 202 for later bound-key dispatch.
- **Rejected trap**: Fresh budgets per gateway/method or full configured timeout on the last call multiply actual wire/time limits. An external-budget context must not kill final-work preparation immediately after an eligibility GET.
- **Trade-off/flip-point**: Final DB/response work gets at most one second and only remaining overall time, not a success guarantee or an extra detached deadline. Future batch recovery needs its own approved budget owner.

### D7 — Validate evidence in payment and preserve adapter fidelity

- **Constraints/facts**: Wire fidelity tests require remote missing/wrong fields remain visible; LIFE-007 checks initial/replay/retrieval equally.
- **Directive**: Adapter copies full remote fields without filling missing metadata from local requests. Payment validates session/client-reference/both metadata/amount/usd/mode/sandbox/known IDs, then interprets allowed session/payment combinations. Mismatch clears exposed URL, preserves trusted saved associations, sets unresolved/investigation. Correct complete/paid evidence still awaits future confirmation and cannot enable replacement. Verified expired/unpaid or no-ambiguity rejection alone permits a new operation under committing all-prior guards.
- **Rejected trap**: Adapter “repairing” evidence makes missing/wrong correlation appear trustworthy; complete or browser return alone never proves paid.
- **Trade-off/flip-point**: Adapter stays policy-free. Feature 3/5 confirmation must independently supply trusted correlation rules and atomic paid/history handling.

### D8 — Split structural input validation from new-purchase limits

- **Constraints/facts**: Duplicate JSON fields, errors after complete JSON, malformed UTF-8, amount token syntax, canonical keys, and changed-limit replay/conflict tests.
- **Directive**: Bounded source read to clean EOF before effects, raw UTF-8 validation, duplicate-aware decoder and explicit token/field allowlist. Accept only digits-only positive int64 amount tokens. Description preserves exact Unicode text and validates length/control/boundary whitespace. Domain compares syntax-valid binding first, then applies configured limits to truly new intent. GET/HEAD inspect at most one actual body byte and distinguish clean EOF from read error. Validate declared history query fields once with decimal signed-64 bounds.
- **Rejected trap**: Standard struct decoding alone accepts duplicate fields, replaces invalid UTF-8, normalizes values, or ignores a final read error; early current-limit rejection hides idempotency conflicts.
- **Trade-off/flip-point**: Small bounded parser is justified by 4096-byte exact contract; no new validation library.

### D9 — Bound actual transport I/O and preserve foundation composition

- **Constraints/facts**: TCP stalled Content-Length/chunked/earlier-parent tests and net.Pipe blocked response writes; foundation transport handlers inject independent tighter settings.
- **Directive**: Feature admission derives the effective request deadline, sets real read/write deadlines through ResponseController, and interrupts reads/writes on earlier cancellation. Flush bounded JSON while feature handling still owns the effective deadline so a buffered net/http response cannot outlive admission. Stop/join any cancellation callback; no reader goroutine may survive the handler. Keep global foundation read/write deadlines and custom extra-handler behavior; do not extend tighter existing transport settings. Root landing and exact probes retain their existing contracts.
- **Rejected trap**: Context cancellation without socket interruption leaves body readers blocked; returning before buffered output flush obscures response completion.
- **Trade-off/flip-point**: Recorder writers without deadline support can still exercise finite parsing/representation. Network evidence must use real server connections.

### D10 — Keep startup validation serving-only and readiness schema-aware

- **Constraints/facts**: Config.Load must work without Stripe/Basic for local commands; standalone authenticated web fixtures lack Stripe settings; actual serve must reject configuration before resources and expose truthful readiness.
- **Directive**: Capture raw/typed checkout fields and deferred safe validation errors once; Basic validation remains independently callable. Full serving validation checks exact USD/50–100000, sk_test suffix/whitespace/control rules, local HTTP origin, request/call/retry/attempt limits and read/write >= request+1s. Compose passes serving settings only to app; migrate/probe do not require them. Serving readiness checks migrated business relations/query usability plus DB reachability; it never calls live Stripe.
- **Rejected trap**: Making config.Load reject serving-only secrets breaks migrate/probe; extending authenticate's full ValidateServing dependency breaks the retained Basic boundary tests.
- **Trade-off/flip-point**: No new readiness service/dependency. Live external health policies require an approved operational contract.

### D11 — Log actual semantic outcomes with safe typed error context

- **Constraints/facts**: SEC-003 names success/pending/rejection/input/auth/DB/Stripe/cancellation/ownership variants; R26 allows arbitrary metadata through the helper. Existing JSON/request IDs are established.
- **Directive**: Consistent fields: `action` (create/continue/read/history/authenticate), `outcome` (accepted/established/pending/rejected/blocked/failed), actual `operation_state` and `payment_status` when known, stable `error_kind`/`failure_code` from explicit error/result classification, numeric HTTP status for web outcomes, and generated request_id plus known order_id/operation_id. Keep business logs for direct Service calls, including cancellation/stale ownership, with known IDs even without HTTP. Carry request correlation through context rather than raw headers. Categories distinguish database_acceptance/database_dispatch/database_result/database_read, transport_timeout/canceled/server/evidence_mismatch/ownership_lost/invalid_request/auth_rejected. Preserve causes internally through errors.Is/As but emit no raw cause/message/body, request key, owner token, checkout URL or credentials. Emit zero fabricated durable IDs for unaccepted input/auth rejection.
- **Rejected trap**: Generic request telemetry plus arbitrary metadata satisfies R26's helper yet communicates no feature outcome. A successful HTTP 202 may contain failed external/persistence work and must describe that reason when known.
- **Trade-off/flip-point**: Some fake arbitrary errors can only be categorized as temporarily_unavailable with actual state; production causes retain their concrete class. Use source plus decoded runtime inspection for every named variant before feature completion; no metadata allowlist/gate/test-helper project is added here.

### D12 — Deliver documentation and verification using existing tooling

- **Constraints/facts**: Six DOs and six NE procedures; main CI runs make verify; existing script covers generation/format/workflows/lint/vulnerability/build/uncached tests/races.
- **Directive**: Current-state README plus API/recovery/architecture docs describe actual delivery, fixed pins and scope; examples use placeholders and preserve keys/IDs. Keep all normal suite/race/CI checks. Record measured wall duration after real implementation is green for normal full Go tests, full races, and full make verify, with exact command/environment/exit code; intentionally red timing is not representative.
- **Rejected trap**: A green Kaba snapshot substitutes neither local/hosted quality checks nor semantic runtime logs; documentation must not claim future confirmation/listing/workers exist.
- **Trade-off/flip-point**: No new tool/dependency/gate is required; revisit performance only from measured final execution and concrete bottlenecks.

## Architectural Delta

Instantiate declared payment and Stripe integration packages plus the declared PostgreSQL business-repository boundary and web API composition. Add four business relations, a durable bounded dispatch lease, immutable request snapshots, and per-request budgets within the existing stack. The SDK is already frozen at `stripe-go/v87 v87.0.0`, API `2026-09-30.endive`; no additional dependency, layer, background paradigm, external account choice, or infrastructure commitment is needed.

## Runtime Evidence

All checks below are pending until production implementation. Preserve the approved six NE procedures and assess every listed variant. Automated green results do not replace source/command/runtime evidence.

| Evidence / criteria | Required outcomes / named variants | Exact procedure / locations | Expected result |
| --- | --- | --- | --- |
| NE-1 — CFG-001–004, FND-002 | Captured defaults/ranges; app-only Stripe settings; invalid serve; migrate/probe independence; migration/startup failure | Inspect `internal/config`, `cmd/service/main.go`, `.env.example`, `compose.yaml`, `scripts/up.sh`; run `docker compose config --quiet` with ignored placeholder/local env, `scripts/testdata/workflows.sh`, and documented `make up`; inspect readyz and serving logs; stop/start DB and retry readiness. | Defaults read11s/write15s/request10s/call2s/retry7s/attempts3; config rejected before DB/listener; DB→fresh migration→app ordering; failed migration leaves app stopped; missing schema not ready; loopback ports and service probe retained; no live Stripe readiness requirement. |
| NE-2 — INP-003, ID-003, SEC-002 | Independent random identities; placeholders/exclusions; response/browser/history/wire/log minimization | Inspect UUIDv4 generation and independent Stripe key; `git check-ignore .env`; inspect tracked `.env.example`, Docker exclusions, browser template, production DTO/history/adapter fields and logging arguments without printing secrets. Compare local SDK wire allowlists. | No amount-derived identity, secret or unnecessary personal/card/CVC/raw payload field; no application credentials in Stripe params or browser; placeholders only tracked; metadata/return URLs credential-free. |
| NE-3 — LIFE-001/005–008, REC-003/004 | State/action policy, blocked fresh versus accepted pending, distinct time boundaries | Trace Create/Continue/Get/History and `docs/api.md`/`docs/recovery.md`; execute documented authenticated create → read/history → same-key replay → continuation using sandbox or existing reproducible local stub composition; inspect persisted state/IDs. Corroborate exact boundary tests in payment suite. | No paid confirmation from creation/browser/decline/timeout; 409 before acceptance leaves key free, 202 after acceptance retains binding; 15m advisory/23h cutoff/23h59m requested expiry distinct; no list/background/force-paid/reset/recreate machinery. |
| NE-4 — STR-001–008 | Exact compatible pin/wire fields; disabled SDK retries; immutable snapshots and bounded real work | Inspect frozen go.mod/go.sum and local tagged API/client source; inspect per-client `MaxNetworkRetries=0`, null SDK logger and context-first SDK calls. Execute SDK request/error/headers/blocked-header/stalled-body tests and connected combined-budget/each-send/restart suites; inspect actual method/form/header/timing plus independent pre-send DB witnesses. | v87.0.0/endive and hosted_page; fixed mode/card/quantity/capture/disabled options; one SDK attempt per invocation; total GET+POST calls/waits bounded; canceled work joined; same immutable snapshot/key/version and durable latest-send marker. |
| NE-5 — DATA-001–005, HTTP-007 | Constraints/immutability/monotonic guards, every transaction phase, no network transaction, no uncommitted success | Inspect migration Up/Down, authoritative schema/query source, transaction/guard/error branches and history inserts; run `make generate`, check generated drift, execute all postgres and connected recovery/concurrency/failure groups; inspect pg_stat_activity/complete-row witnesses. | Normal Goose schema and generated pgx agree; all-prior/paid/current/version/owner and uniqueness guards hold; failure rolls state/history back; reads/unchanged observation never duplicate transitions; network begins only after commit; history has no normal mutation paths. |
| NE-6 — FND-001–004, SEC-003 | Local/CI checks, probe/outage recovery, shutdown joins/bounds/nonzero cleanup failure, semantic logs | Run full `make verify`; inspect verify script/main CI and `main → run → Serve` error/exit path. Perform local startup/probes/graceful stop/DB outage-recovery. Independently perform the SEC-003 matrix below, including actual decoded logger records and production-source error/context traces. | Same full format/sqlc/workflow/lint/vulnerability/build/unit/integration/race selection; recovery-safe cancellation and cleanup after owned work exits; nonzero cleanup-failure exit; actual outcomes/causes correlated and sanitized for all named variants. |

### SEC-003 semantic logging verification (NE-6; R26 condition)

The independent code review and final audit must record a file/branch trace and decoded runtime JSON evidence for each row. Use a temporary external verification module under `/private/tmp`, with module path `github.com/filser89/stripe-payments-go/verification`, a local replace directive pointing to this application checkout, and the pinned Go toolchain. Its small verification executable imports the real production constructors (`postgres.NewPaymentRepository`, `stripeapi.New`, `payment.New`, `web.NewCheckoutHandler`, `web.New`), supplies a synchronized buffer-backed slog JSON logger, a standard-library local HTTP Stripe stub through the existing adapter construction options, and real PostgreSQL 18.6 with the application Goose migrations. The executable calls authenticated requests through the actual handler and direct Service operations; it also uses separate control connections/temporary DB triggers or container stop/start to inject acceptance/result/read failures, version/owner takeover, and stalled/canceled calls. Use configured one-attempt and combined GET→POST budgets to produce unresolved and prepared outcomes. Capture the logger bytes from this executable, decode each JSON record, and retain sanitized per-scenario records plus exact harness source/commands and inspected commit in implementation/review evidence. Remove the temporary module/container after capturing evidence. This is a verification driver using existing seams, not a new application service, framework or reference implementation. Frozen tests/helpers are untouched; their private log buffers are not assumed to be printed by go test. Correlate response request IDs and only known durable IDs; check actual state/outcome/category against the triggered result. Arbitrary extra metadata, timestamps, IDs-only logs and passing OutcomeLogContext are insufficient. This condition remains open until independently verified.

| Named variants | Existing execution paths and required semantic evidence |
| --- | --- |
| Accepted/successful checkout | `TestConnectedUsefulSanitizedCheckoutOutcomeLogs/accepted`, create/replay response groups: actual accepted versus established outcome, open/unpaid state, numeric status and matching IDs. |
| Prepared/unresolved | `TestLIFE009CheckoutRules`, `TestMixedElapsedBudgetCapsCallsAndRetainsPreparedIntent`, `TestCheckoutPendingFailureLogContext`: pending plus actual prepared/deferred_budget or unresolved cause; no invented success. |
| Confirmed rejection | `TestConnectedUsefulSanitizedCheckoutOutcomeLogs/confirmed_rejection`, `TestSTR008RecoveryRules`: rejected plus actual validation/credential/permission class and known accepted identity; prior ambiguity remains unresolved. |
| Input/auth rejection | `TestCheckoutValidation`, `TestCheckoutEndpointAuthentication`, `TestCheckoutAuthenticationMethodMatrix`, connected invalid/credential isolation: invalid_request/media/size/body_read versus auth_rejected; generated request ID; no fabricated order/operation IDs or raw input. |
| DB acceptance/result/read failure | Connected useful-log acceptance fault; `TestConnectedCheckoutRecovery/success_before_result_commit_failure_then_restart`; `TestResultAndReadUnavailableReturnKnownIdentity`: distinguish failed acceptance, failed result commit with readable pending work, and failed reread/503 with known IDs. Inspect all database error branches including pre-send marker failure. |
| Stripe timeout/server/mismatch | Useful-log timeout/server/mismatch and connected correlation/retry groups: actual transport_timeout/server/evidence_mismatch, unresolved/investigation where required, matched IDs and no raw SDK diagnostics. |
| Cancellation/ownership loss | `TestConnectedCheckoutBudgetsAndShutdown/blocked_wire_caller_cancel_joins_and_retains_identity`, `TestConnectedDelayedWireCannotOverwritePaidOrOwner`, `TestDelayedEligibilityCannotOverwriteNewCurrentOperation`: canceled or ownership_lost with durable identity and correct suppression of stale success; no business/history mutation attributed to a rejected stale result. |

Record semantic logging verification in the implementation/code-review evidence, including exact inspected commit, source locations and runtime paths/results. Leave any missing variant explicit. The test-review advisory R26 is not claimed repaired; this procedure establishes the required production behavior independently of its permissive automated helper.

## Delivery Obligations

| ID | Source | Deliverable / target | Completion check |
| --- | --- | --- | --- |
| DO-001 | spec DO-001; Required behavior; E-07/Q6 | `docs/api.md`, README usage: create/continue/read/history, Basic auth, exact input/UUID/body/query/HEAD/envelope/nulls/pagination, statuses/flags and preserved request keys/IDs. | Follow documented calls against delivered service, including 202 inspection and 409/502/503 interpretation without creating a second operation. |
| DO-002 | spec DO-002; E-02/E-06/STACK | `.env.example`, `compose.yaml`, `scripts/up.sh`, README setup: sandbox key/local origin, amounts/budgets and USD prerequisites, app-only settings, ignored local env/secret exclusions. | NE-1/2 plus normal workflow regression checks; placeholders only, actual ordered startup/probes usable after prerequisites; later webhook setup remains future scope. |
| DO-003 | spec DO-003; F-06/F-07/E-07/Q3/Q6 | `docs/recovery.md`, API lifecycle: immutable identity, permitted continuation/replay, safe cutoff, expiry/advisory, unknown outcomes, operator inspection and unsupported dashboard refunds/overrides. | NE-3 plus recovery/age/lifecycle suites; current behavior documented; Feature 3 confirmation and Feature 5 bounded listing/manual workers remain distinct future contracts. |
| DO-004 | spec DO-004; E-05/STACK/Q1–Q4 | Real implementation against frozen fake-payment, PostgreSQL18.6, pinned SDK/local httptest, connected failure/restart/concurrency/transport suites. | All 597 implementation targets pass in normal overlay-free native post-impl gates; 65 baseline passes preserved; failure ordering/real wire/time/independent-connections/cancellation/races established without live Stripe. |
| DO-005 | spec DO-005; E-05/E-06/Q7/A-13 | Existing Makefile, verify script, `.github/workflows/verify.yml` main CI and README verification; scoped implementation evidence with measured execution duration. | Implement-code completes full local make verify with no narrowed suite/configuration and reports hosted CI explicitly pending. The dedicated final integration agent verifies PR/main hosted workflows after audit and authorized push. After green implementation, measure `/usr/bin/time -p make test`, `/usr/bin/time -p go test -race -count=1 -timeout=5m ./...`, and `/usr/bin/time -p make verify`; retain wall seconds, exit codes, toolchain, Docker/PG version and cache/run conditions. Native Kaba timing is additional evidence, not representative final normal-suite timing. |
| DO-006 | spec DO-006; E-01/E-07/Q2/Q6 | Versioned Goose migration, schema/query source and regenerated sqlc output; README/API/recovery/`docs/architecture.md`; separate architecture-diff phase for `.kaba/architecture.md`. | NE-5 plus normal migrate up/repeat/down inspection and no generated drift; current delivered code structure and limits agree with docs; no history/provenance narrative. |

## Build Order

1. C1: production tested contracts and classified errors/identity values. Preserve frozen files and dependency pins; make scaffolds unnecessary through real declarations without removing them.
2. C4: four-entity migration/schema, constraints/immutability/sequence guards and named SQL; regenerate sqlc (DO-006).
3. C5: acceptance/binding/current-versus-bound reads/history, then dispatch/result/replacement transactions and fenced lease recovery. Green postgres atomicity/independent-concurrency tests first; ensure external-call prerequisites are committed.
4. C6: per-client single-attempt SDK encoding/fidelity/error translation and cancellation; green stripeapi tests against local server.
5. C2/C3: identity replay, lifecycle/flags/evidence, combined budgets, conservative ambiguity/age, result-failure readback and semantic business logs; green fake policy/recovery suites. Recheck tests' whole-row preservation allowances before changing bookkeeping.
6. C7: captured serving-only checkout settings, independent Basic validation and read-default alignment; green config and retained account/common-setting contracts.
7. C8/C9: protected route/parser/deadline/DTO/error/history/HEAD composition and semantic request outcomes/rejections. Green web endpoint and real transport suites while retaining root/auth/probes/shutdown.
8. C10: real serve dependency wiring and payment-schema readiness, retained migrate/probe/cleanup exit paths; green cmd/service and connected journey/recovery/concurrency/deadline suites.
9. C11: current README/configuration/Compose/API/recovery/architecture delivery (DO-001/002/003/006); execute NE-1–5, including local startup and normal migration checks.
10. Complete DO-004/005: normal overlay-free native post-impl comparison, protected commit/inventory checks, full normal tests/races/make verify with measured durations, and report hosted CI pending. Implement-code may commit and complete/clear its normal local phase lock after local gates; it must not push early or claim whole-feature completion to satisfy hosted evidence. Do not refresh protected baselines to absorb failures.
11. Independently verify NE-6 and every SEC-003 matrix row in code review/final audit; resolve code findings through authorized fresh contexts. Route suspected frozen test/plan defects to the orchestrator rather than amending tests.
12. Separate architecture-diff and final audit/integration phases update delivered architecture/evidence and confirm obligations/findings. After the audit, the dedicated final integration agent performs authorized push/PR/CI/merge/main synchronization, verifies hosted PR/main checks for DO-005, and only then confirms whole-feature completion. Compilation-scaffold removal belongs only to the separately authorized cleanup phase after implementation completion.

## Validation

- Test coverage: PASS — 29/29 runtime test files mapped; support and five scaffolds retained.
- Greens consistency: PASS — references use actual source groups/files, including correction cases and foundation leaves.
- Schema completeness: PASS — four persisted table roles plus complete snapshot value specified.
- Decisions grounded: PASS — 12 directives identify constraints, rejected traps and trade-offs/flip-points.
- No escalation left silent: PASS — choices apply declared stack/layers and resolved behavior; no unresolved external fact or unsafe new pattern.
- Rules respected: PASS — source/SQL/SDK ownership, current-state documents, no future feature implementation and frozen test separation.
- No implementation code: PASS — responsibilities/schema/contracts/check procedures only, no bodies or migration/query implementations.
- Build order complete: PASS — C1–C11 and all delivery/evidence tasks dependency-ordered.
- Delivery/evidence complete: PASS — DO-001–006 and NE-1–6 mapped; R26/SEC-003 production verification explicitly pending.

Ready for a fresh `/kaba:implement-code` context against the accepted frozen checkpoint and this plan. Planning does not claim production behavior or final checks pass.
