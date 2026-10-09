# Acceptance Criteria: Create orders and hosted checkout

**Feature**: `002-02-order-and-checkout`
**Spec**: `features/002-02-order-and-checkout/spec.md`
**Status**: Draft

## Summary

- Total criteria: 54
- New: 51 | Modify: 2 | Exists: 1
- Categories: Runtime configuration (4); Request validation (6); HTTP creation and inspection (7); Durable request identity (4); Checkout lifecycle and eligibility (9); Stripe dispatch and bounded execution (8); Interruption and recovery (4); PostgreSQL consistency and history (5); Authentication and visibility (3); Foundation preservation (4).

The approved contract is `spec.md` at `3bcb9e2a3784aa2b431941b16c3e82e5f3cb9448`. Existing evidence references below are source assertions at that checkpoint, not claims of execution in this phase. NEW includes behaviors with only partial prerequisite evidence; EXISTS requires the complete stated behavior and variants. MODIFY identifies an existing assertion that must be extended or aligned.

Automated evidence uses payment-rule fakes, real PostgreSQL 18.6 with application migrations, and focused real Stripe SDK calls to a test-local `httptest` server. Connected evidence exercises authenticated HTTP → payment behavior → PostgreSQL → SDK → local Stripe server, followed by authenticated inspection of persisted outcomes. Several evidence locations may establish one criterion; every required outcome and named variant must remain locatable. No automated correctness evidence requires live Stripe or credentials.

Feature 2 does not deliver signed webhook confirmation, list-based reconciliation, a worker pool, browser payment controls, or manual payment overrides. Later paid/expired states may be established as persisted prerequisites to exercise this feature's guards and reads. Future confirmation requires the spec's trusted, correlated evidence contract; this feature's retrieval of paid evidence remains unresolved and cannot mark payment confirmed. Feature 5's listing/page/worker limits remain dependency contracts, not Feature 2 execution gates.

## Criteria

### Runtime configuration

#### CFG-001: Validate configured USD amount limits without repricing accepted work
- **Status**: NEW
- **Source**: FR-002, FR-003, FR-008; Checkout contract / Input and configuration, currency and replay clauses.
- **Behavior**: Serving with valid settings uses explicit USD cents and the configured new-order limits; invalid currency/limit configuration prevents HTTP serving without exposing supplied values. A valid replay compares persisted inputs before applying current amount limits and preserves its accepted price/currency.
- **Required variants**: Defaults `usd`, minimum `50`, maximum `100000`; accepted limits at `50 <= min <= max <= 100000`, including equal limits; currency missing/default versus exact `usd` versus uppercase/other currencies; malformed/non-base-10/fractional/overflow limits, minimum below 50, maximum above 100000, and reversed limits rejected; identical persisted replay outside tightened limits accepted without repricing; changed input under its key conflicts; genuinely new order outside tightened limits rejected.
- **Evidence kind**: Automated configuration/startup tests establish validation and non-serving failures; real PostgreSQL replay tests establish persisted pricing across process/configuration changes.

#### CFG-002: Restrict serving to sandbox credentials and a local return origin
- **Status**: NEW
- **Source**: FR-003, FR-020; Checkout contract / Input and configuration, Stripe key and base URL clause.
- **Behavior**: HTTP serving requires a valid sandbox secret and local absolute HTTP origin; invalid settings fail before serving and never disclose values. Settings are fixed for the process.
- **Required variants**: `sk_test_` with nonempty suffix accepted; missing/empty suffix, live/other key classes, leading/trailing whitespace, and Unicode controls rejected; base default `http://localhost:8080`; localhost, 127.0.0.1, and bracketed ::1 with absent/valid port accepted; optional trailing `/` accepted; non-HTTP scheme, relative URL, missing/nonlocal host, invalid port, credentials, non-root path, query, and fragment rejected; environment changes after startup do not alter an accepted process's settings.
- **Evidence kind**: Automated startup/configuration and process-lifetime tests; response/log inspection establishes value minimization. No real credentials are used.

#### CFG-003: Validate request, Stripe, and serving time/attempt budgets
- **Status**: MODIFY
- **Source**: FR-011; Checkout contract / Stripe request, deadlines, and recovery, serving budgets clause.
- **Behavior**: Validated settings define request/call/retry/wire-attempt bounds and permit final response work; invalid or incompatible settings prevent serving. HTTP read/write settings each accommodate request timeout plus one second.
- **Required variants**: Defaults request `10s`, call `2s`, retry `7s`, max attempts `3`, HTTP read `11s`, write `15s`; accepted endpoints request 2–10 seconds, call 100ms–2s, retry 500ms–7s, attempts integer 1–3; missing/default, malformed, below/above each limit; retry greater than request minus 1s rejected; call greater than retry rejected; read/write exactly request+1s accepted and smaller rejected; default shutdown grace 10s and cleanup 5s retained.
- **Evidence kind**: Automated configuration/startup tests plus later runtime evidence in STR-004 and FND-003. Existing assertions at `internal/config/config_test.go:16–23` prove foundation defaults, including a read-time assertion requiring alignment with this contract; they do not prove the checkout settings/cross-setting constraints.
- **Existing file**: `internal/config/config_test.go`

#### CFG-004: Keep local migrate/probe independent of checkout serving settings
- **Status**: NEW
- **Source**: FR-003, FR-004; Checkout contract / Input and configuration, serving settings clause.
- **Behavior**: Local migrate/probe commands do their actual work with absent or invalid Stripe secret/base-origin serving settings while retaining applicable foundation configuration validation and bounded dependency failures.
- **Required variants**: Missing and invalid Stripe key/base URL for both commands; migration reaches real PostgreSQL and applies application migrations; probe reaches public readiness without credentials; unavailable database/probe remains a real bounded failure.
- **Evidence kind**: Automated command tests against real PostgreSQL and controlled local probe endpoint. `cmd/service/authentication_test.go:272–378` proves independence from Basic serving credentials and real command effects only; checkout setting independence is new evidence.

### Request validation

#### INP-001: Preserve exactly valid single-product descriptions
- **Status**: NEW
- **Source**: FR-001, FR-002, FR-003; AS-01, A-12; Checkout contract / Input and configuration, creation body clause.
- **Behavior**: A creation request accepts valid UTF-8 descriptions of 1–200 Unicode code points without controls or boundary whitespace; accepted text remains exact in stored purchase data, responses, and the Stripe snapshot. Invalid descriptions cause no durable or Stripe effect.
- **Required variants**: Lengths 1 and 200 accepted, 0 and 201 rejected; multibyte code points counted as characters; interior whitespace and case/canonical Unicode distinctions preserved; leading/trailing Unicode whitespace, Unicode control characters, invalid UTF-8, missing/null/non-string description rejected. Application order has one product without a quantity/cart/line-item model.
- **Evidence kind**: Automated validation/HTTP and real PostgreSQL persistence tests; SDK outgoing-parameter evidence in STR-001 verifies exact accepted product name.

#### INP-002: Accept only authoritative bounded integer minor-unit amounts
- **Status**: NEW
- **Source**: FR-002, FR-003; A-12, SC-06; Checkout contract / Input and configuration, amount clause.
- **Behavior**: New orders accept a positive signed-64-bit decimal-digits-only JSON integer within configured inclusive bounds; no client currency or price override is accepted. Invalid monetary inputs have no order/operation/history/Stripe effect.
- **Required variants**: Configured minimum/maximum accepted, each adjacent outside value rejected; zero/negative, string, fraction, exponent, null, boolean, missing amount, signed-64-bit overflow rejected; currency field and extra override fields rejected; accepted amount remains exact integer cents and currency `usd` through persistence/SDK calls. Replay ordering is CFG-001.
- **Evidence kind**: Automated HTTP/payment-rule tests and real PostgreSQL/SDK amount assertions.

#### INP-003: Require canonical request and resource identities
- **Status**: NEW
- **Source**: FR-006, FR-007, FR-008; A-12; Checkout contract / Input and configuration, request-key clause; Authenticated HTTP API, canonical paths clause.
- **Behavior**: Both POST operations require canonical lowercase hyphenated UUID v4 request keys; order paths require canonical UUIDs. The backend generates distinct random UUID v4 order/operation IDs and an independent Stripe key of at most 255 characters.
- **Required variants**: Valid request key accepted; missing/null/non-string, uppercase, absent hyphens, malformed characters/length, non-v4 version, and invalid UUID variant rejected; malformed order ID returns `400 invalid_request`, valid unknown order `404 not_found`; generated order/operation IDs differ from each other and caller key; Stripe key is independent of the caller key, not amount-derived.
- **Evidence kind**: Automated HTTP validation and persisted identity tests; source inspection establishes random generation and independence rather than statistical inference alone.

#### INP-004: Accept exactly one complete declared JSON object
- **Status**: NEW
- **Source**: FR-003; A-12; Checkout contract / Input and configuration, POST parsing clause; Authenticated HTTP API inputs.
- **Behavior**: Initial POST accepts exactly description/amount/request_key; checkout POST accepts exactly request_key. Bodies are one JSON object with each field once and EOF after trailing whitespace; malformed or unsupported structures produce `400 invalid_request` with no feature effects.
- **Required variants**: Valid declared object and trailing whitespace accepted; duplicate declared fields, unknown fields, missing/null fields, arrays/scalars, empty normal EOF, malformed JSON/UTF-8, extra JSON value/trailing non-whitespace, and read errors before/after partial valid input rejected. Initial fields on checkout POST rejected. A zero-byte normal EOF and a zero-byte read error are distinct rejected cases.
- **Evidence kind**: Automated entry-point tests with controlled read failures and real persistence/Stripe absence checks.

#### INP-005: Enforce media and byte limits on actual request streams
- **Status**: NEW
- **Source**: FR-003, FR-011; A-12; Checkout contract / Input and configuration, POST body clause; Stripe request, deadlines, and recovery, body-read deadline clause.
- **Behavior**: POST requires application/json with optional UTF-8 charset and at most 4096 bytes of JSON source including whitespace. Unsupported media/encoding returns `415 unsupported_media_type`; oversized bodies return `413 body_too_large`; no rejected stream creates effects.
- **Required variants**: application/json and its optional UTF-8 charset accepted; missing/other type or unsupported charset/content encoding rejected; exactly 4096 bytes accepted when otherwise valid, 4097 rejected; whitespace counts; known Content-Length and unknown/chunked streams enforce the same byte boundary; partial/error reads do not turn into successful input; stalled stream termination is STR-004, not proof from a Content-Length check alone.
- **Evidence kind**: Automated direct-handler and real HTTP stream tests with persistence/external-effect assertions.

#### INP-006: Reject read bodies and undeclared query/pagination inputs
- **Status**: NEW
- **Source**: FR-003, FR-014; A-12; Checkout contract / Input and configuration, GET/HEAD/query clause; Authenticated HTTP API, history input row.
- **Behavior**: GET/HEAD accept no body, detected with bounded reads; query fields are accepted only as the declared history cursor/limit once. Invalid inputs return `400 invalid_request` without state or Stripe effects.
- **Required variants**: Empty body with normal EOF accepted; nonempty Content-Length and chunked/unknown-length bodies rejected; zero-byte read error rejected; stalled read bounded under STR-004; queries rejected on creation/checkout/order reads; history defaults after 0/limit 50; after nonnegative signed-64-bit decimal integer including 0/max accepted; negative, malformed, overflow, duplicate/unknown query fields rejected; limit 1/100 accepted, 0/101/noninteger rejected; GET/HEAD validation agrees.
- **Evidence kind**: Automated validation and actual-stream tests; query boundary and no-side-effect assertions.

### HTTP creation and inspection

#### HTTP-001: Expose a committed order and successful hosted Checkout without payment confirmation
- **Status**: NEW
- **Source**: AS-01, SC-01; FR-001, FR-002, FR-005, FR-009; Checkout contract / Authenticated HTTP API, initial POST/result clauses.
- **Behavior**: An authenticated valid initial POST atomically accepts purchase, initial operation, key binding, and history before external creation. A new durable operation with a usable saved hosted result returns `201`; order remains unpaid. Identical replay/continuation with an established result returns `200`.
- **Required variants**: Connected initial success with one order/operation and hosted location; identical initial replay retains IDs/price and returns established result; established continuation retains its bound operation; session creation and success/cancel browser return never mark paid. Card entry stays on the hosted Stripe page.
- **Evidence kind**: Mandatory connected HTTP/payment/PostgreSQL/real-SDK local-server test, followed by status/history inspection; isolated successful tests alone are insufficient.

#### HTTP-002: Return the specified JSON envelope and action flags
- **Status**: NEW
- **Source**: FR-013, FR-014; AS-03; Checkout contract / Authenticated HTTP API, responses/flags clauses.
- **Behavior**: POST and order-read responses contain the specified order/operation objects, UTC RFC3339 timestamps, explicit nullable fields, and all three eligibility flags. Feature JSON responses have UTF-8 content type, no-store caching, and the existing request ID.
- **Required variants**: Order fields id/description/amount/currency/payment_status/created_at/updated_at; operation fields id/state/stripe_session_id/stripe_payment_intent_id/checkout_url/first_dispatch_at/expires_at/created_at/updated_at/failure_code/investigation_required; explicit nulls for absent values; populated values and optional fractional timestamps; top-level can_resume/can_retry_same_operation/can_start_new_attempt; initial/replay/continuation/read envelopes; no raw Stripe body or sensitive fields. Exact flags are LIFE-008.
- **Evidence kind**: Automated response schema/header tests across states; no-store/request ID checked on feature success and error responses subject to retained Basic/public-probe contracts.

#### HTTP-003: Report accepted pending work with durable inspection identifiers
- **Status**: NEW
- **Source**: A-03, A-07, SC-03; FR-010, FR-013; Checkout contract / Authenticated HTTP API, accepted result clause.
- **Behavior**: Accepted prepared/unresolved/in-flight work returns `202` with the same envelope/IDs, Location `/api/orders/{id}`, and Retry-After `1`. A concurrent duplicate may return this immediately without allocating another operation; callers can inspect without another creation.
- **Required variants**: Prepared, unresolved, active concurrent dispatch, unavailable Stripe exhausted within budget, and prepared new attempt after eligibility exhausts remaining budget; status/history remain accessible by local ID, including absent Stripe object ID.
- **Evidence kind**: Automated HTTP and connected injected-failure tests with real PostgreSQL; deliberately coordinated concurrent request evidence.

#### HTTP-004: Return stable generic errors and suppress rejected-request effects
- **Status**: NEW
- **Source**: A-12, SC-06; FR-003, FR-004, FR-012, FR-020; Checkout contract / Authenticated HTTP API, errors clause.
- **Behavior**: Feature errors use error.code/message, generic text, and known order_id/operation_id inside error for accepted work. Responses use the prescribed status/code; fresh-key requests blocked before durable acceptance allocate no binding/operation and can use that key after eligibility is established. An already accepted key retains its binding when subsequent dispatch/result handling becomes unresolved (HTTP-003/007).
- **Required variants**: `400 invalid_request`, `413 body_too_large`, `415 unsupported_media_type`, `404 not_found`, `405 method_not_allowed`, `409 idempotency_conflict`, `409 checkout_blocked`, `502 checkout_rejected`, `503 temporarily_unavailable`; body-read failure produces 400 when response remains possible; accepted rejection/unavailability includes known IDs; unknown IDs omitted rather than fabricated; submitted body/Stripe/internal error text never echoed; rejected pre-acceptance input has no durable/Stripe effects; blocked fresh key remains usable later.
- **Evidence kind**: Automated entry-point and dependency-failure tests plus response/log inspection; payment effects inspected in real PostgreSQL and controlled Stripe dependency.

#### HTTP-005: Preserve canonical routing and HEAD behavior
- **Status**: NEW
- **Source**: FR-004, FR-014; Checkout contract / Authenticated HTTP API, endpoint table and method/path clause.
- **Behavior**: The four specified API paths implement their stated methods; known paths with other methods return 405 and Allow, unknown paths return 404. HEAD on order/history reads has identical status/headers to GET and no response body after normal authentication/validation.
- **Required variants**: Initial POST, checkout POST, order GET/HEAD, history GET/HEAD; unsupported methods on each known path; unknown protected path; malformed canonical ID versus unknown valid ID; HEAD success and errors; unauthorized routing remains SEC-001, exact public probes FND-001.
- **Evidence kind**: Automated HTTP routing/status/header/body checks at the real authentication boundary.

#### HTTP-006: Read only local status and chronological paginated history
- **Status**: NEW
- **Source**: AS-03, SC-04; FR-013, FR-014, FR-016; Checkout contract / Authenticated HTTP API, read rows and history clause.
- **Behavior**: Authenticated order/status and history reads expose persisted purchase/current operation and distinguish unpaid/pending, unresolved integration work, and confirmed paid prerequisites. Reads call no Stripe and mutate no state/history. History returns per-order ascending sequence strictly after cursor, at most limit, and correct next_after.
- **Required variants**: Prepared/open/unresolved/complete_unpaid/expired/rejected/paid local states; no saved session ID; stale local observations reported as last observed; history empty, first/default page, exact-limit page, following page without omission/duplication, final partial/empty page; next_after last returned sequence or input cursor when empty; order isolation; specified entry fields/kinds/nulls per DATA-004.
- **Evidence kind**: Automated authenticated HTTP reads backed by real PostgreSQL, comparing before/after state/history and proving zero Stripe calls. Paid fixture is a read prerequisite, not a Feature 2 confirmation implementation.

#### HTTP-007: Never report success for an uncommitted external result
- **Status**: NEW
- **Source**: A-08, SC-03, SC-05; FR-009, FR-010, FR-017; Checkout contract / Authenticated HTTP API, acceptance/result persistence clause.
- **Behavior**: Initial durable acceptance commits before external work. If result persistence fails after Stripe returns, response is 202 when a database read establishes IDs, otherwise 503 with the same known IDs when available; no uncommitted result is claimed successful. Inability to durably accept work returns 503 and dispatches no Stripe mutation.
- **Required variants**: Pre-acceptance transaction failure; external success then result-commit failure with readable local state; external success then result-commit failure with read unavailable; known versus unavailable IDs; failed persistence followed by retry/restart retains original operation and evidence, with no duplicate creation.
- **Evidence kind**: Connected controlled database-failure tests and PostgreSQL state/history assertions; REC-002 covers process interruption at the same boundary.

### Durable request identity

#### ID-001: Repeated identical requests preserve one durable association
- **Status**: NEW
- **Source**: A-03, A-06, A-08, SC-02; FR-006, FR-007, FR-009, FR-010; Checkout contract / Input and configuration, key binding clause.
- **Behavior**: Identical initial requests with one key resolve to one order/operation, independent of response delivery or process lifetime. Each retry of its mutation preserves the separate durable Stripe key and snapshot.
- **Required variants**: Sequential replay, deliberately overlapping identical requests using independent PostgreSQL connections, retry after caller response loss, retry after process restart, replay after configured limits change; durable key binding remains present with no automatic expiry/deletion.
- **Evidence kind**: Real PostgreSQL sequential/concurrency/restart and connected request tests; inspect association/key/snapshot and externally observable logical session count.

#### ID-002: Conflicting key bindings are rejected without collateral effects
- **Status**: NEW
- **Source**: A-03, SC-02; FR-006, FR-008; Edge Cases, changed parameters; Checkout contract / Input and configuration, binding/replay clause.
- **Behavior**: A globally bound key cannot accept different purchase data, method/logical target, or order association; syntactically valid changed inputs return 409 idempotency_conflict before new-order amount-limit policy can hide the conflict. Its original binding/state remains intact.
- **Required variants**: Changed description, amount, initial-versus-continuation target, different order target; concurrent conflicting submissions across independent connections; changed amount outside current configured bounds still conflicts after syntax/binding resolution; malformed replay still rejects syntax first; no new binding/order/operation/Stripe effect for loser.
- **Evidence kind**: Automated HTTP plus real PostgreSQL coordinated conflicts inspecting winner/loser outcomes and immutable bindings.

#### ID-003: Separate intent uses separate caller and operation identities
- **Status**: NEW
- **Source**: AS-02, SC-02; FR-007, FR-008; Checkout contract / Input and configuration, request-key clause.
- **Behavior**: Identical purchase fields with distinct caller keys may create distinct orders; fields alone do not deduplicate intent. A supported later attempt has a distinct durable operation/Stripe key on the original order and retains its immutable purchase.
- **Required variants**: Same description/amount with different initial keys produces distinct IDs; same key deduplicates; new eligible attempt differs from previous operation/key while retaining order/price; later attempt is subject to LIFE-004 rather than fresh-key permission alone.
- **Evidence kind**: Real PostgreSQL/connected identity tests and key-generation source inspection.

#### ID-004: Replay targets its original operation even after later attempts
- **Status**: NEW
- **Source**: FR-006, FR-014, FR-015; Checkout contract / Authenticated HTTP API, bound-operation response clause; Orders, operations, and continuation, same-key replay clause.
- **Behavior**: A replay/continuation key remains bound to its original operation; it never creates or switches to a later attempt. Order reads report the current operation. Definitive-rejection replay returns the same rejection/IDs without another Stripe POST.
- **Required variants**: Original expired/rejected operation with a newer current operation; original initial key and continuation key; paid order initial-key replay reports existing result with URL suppressed and all flags false; paid checkout continuation/replay blocks per LIFE-006; old binding never deleted to permit reuse.
- **Evidence kind**: Real PostgreSQL/HTTP sequence tests inspecting bound/current IDs and absence of further creation.

### Checkout lifecycle and eligibility

#### LIFE-001: Represent integration outcomes separately from terminal payment state
- **Status**: NEW
- **Source**: A-02, SC-04; FR-005, FR-012, FR-014, FR-015; Checkout contract / Orders, operations, and continuation, states clauses.
- **Behavior**: Orders are unpaid or terminal paid; initial unpaid orders have a durable current operation. Operations distinguish prepared, unresolved, open, complete_unpaid, expired, rejected, and paid with their specified meanings. First possible dispatch atomically changes prepared to unresolved with history before the call.
- **Required variants**: Prepared with no dispatch marker; interrupted pre-call boundary unresolved even if no wire send occurs; usable verified unpaid open; verified complete/unpaid; verified expired/unpaid; definitive no-creation rejection; persisted confirmed-paid prerequisite; timeout/decline/browser cancellation/abandonment never terminally fail the whole order or mark paid.
- **Evidence kind**: Payment-rule fake and real PostgreSQL transition tests plus connected dispatch ordering. Later payment confirmation is outside scope.

#### LIFE-002: Refresh known open Checkout before POST resumption
- **Status**: NEW
- **Source**: A-02, FR-005, FR-015; Checkout contract / Orders, operations, and continuation, open/resumption clauses.
- **Behavior**: An open saved session can resume card entry/decline recovery on its same hosted page. Every resumed POST with a known session ID retrieves current Stripe evidence within budget before returning a usable URL; a read only reports local last-observed state. At/after saved expiry the response suppresses the URL until evidence refresh.
- **Required variants**: Verified open/unpaid before saved expiry resumes same ID/URL; locally expired time alone suppresses URL and cannot enable replacement; cancel/abandon/decline does not expire the session; retrieval fails/mismatches/establishes complete or paid → no usable resumed URL; known ID uses GET retrieval even beyond 23h; no creation POST to rediscover a known session.
- **Evidence kind**: Payment-rule tests and real SDK local-server retrieval assertions, with HTTP/persistence sequence evidence.

#### LIFE-003: New-key continuation on recoverable work preserves the current operation
- **Status**: NEW
- **Source**: AS-02, A-03; FR-006, FR-007, FR-015; Checkout contract / Orders, operations, and continuation, new-key clause.
- **Behavior**: A fresh continuation key on prepared/unresolved work binds to and retries that same operation only where safe; an open operation is retrieved/resumed under its identity. It cannot allocate a replacement merely because its key is fresh. Once its key is durably bound, later response loss/mismatched evidence retains that association and accepted unresolved work, even when no session ID is saved.
- **Required variants**: Prepared dispatch; unresolved with compatible snapshot/missing ID inside safe age; unresolved with known ID retrieval; active dispatch may return 202; open refresh/resume; incompatible snapshot, old missing-ID ambiguity, and mismatch block instead of replacement; repeated new continuation key retains its binding.
- **Evidence kind**: Payment-rule and connected real PostgreSQL sequence tests inspecting binding, operation count, Stripe key/parameters.

#### LIFE-004: Permit one distinct attempt only after verified safe lifecycle evidence
- **Status**: NEW
- **Source**: A-02, AS-02, FR-015, FR-018; Checkout contract / Orders, operations, and continuation, distinct-operation eligibility and atomic guard clauses.
- **Behavior**: For an unpaid order, a fresh key may allocate one distinct operation only after every prior potentially active attempt is verified expired/unpaid or confirmed rejected with no earlier ambiguity. A preceding known session is retrieved and correctly validated before allocation; rejected work has no session to retrieve. Paid/current-operation state is rechecked transactionally at commit.
- **Required variants**: Correctly correlated expired/unpaid predecessor permits new operation; confirmed no-ambiguity rejection permits it without session retrieval; multiple prior attempts all satisfy safety; locally expired time, absent object/result, and list absence cannot substitute for evidence; concurrent eligible fresh keys allocate one potentially active operation; stale eligibility losing to another current operation or paid prerequisite cannot allocate.
- **Evidence kind**: Payment-rule tests, real SDK GET evidence, and deliberately coordinated real PostgreSQL independent-connection transactions; inspect immutable price and new bound IDs.

#### LIFE-005: Block unsafe complete, paid, ambiguous, and mismatched continuation
- **Status**: NEW
- **Source**: A-02, A-11, SC-04, SC-07; FR-012, FR-015, FR-019; Checkout contract / Orders, operations, and continuation, blocking clauses.
- **Behavior**: Complete_unpaid, paid, unknown/mismatched evidence, failed retrieval, and old ambiguous creation cannot permit a fresh attempt. A fresh-key continuation blocked by its pre-acceptance eligibility check returns 409 checkout_blocked without binding/operation allocation. If that key has already been durably accepted onto recoverable work and its dispatch subsequently becomes ambiguous, preserve the binding and return the accepted unresolved outcome/IDs under HTTP-003; do not erase the association or reclassify it as an unaccepted blocked request. No card decline or browser action justifies a new creation key.
- **Required variants**: Complete/unpaid including decline; retrieved paid without local confirmation; local paid; PaymentIntent-only or no_payment_required evidence; unknown status/payment combination; correlation mismatch; retrieval network/error/malformed result; missing-ID ambiguity at/after safe cutoff; cancel/abandon; previously ambiguous operation followed by rejection remains unresolved rather than newly eligible.
- **Evidence kind**: Payment-rule fake and real PostgreSQL/HTTP guarded-outcome tests; SDK result/error translation tests. No signed event handling is required.

#### LIFE-006: Preserve paid state and refuse another charge across stale results
- **Status**: NEW
- **Source**: FR-005, FR-015, FR-017, FR-018; Edge Cases, delayed failure; Checkout contract / Orders, operations, and continuation, paid and atomic guard clauses.
- **Behavior**: Persisted paid is terminal. All checkout continuations, including replays, block without external mutation; initial creation-key replay reports its existing order/result with URL suppressed and all flags false. Stale external failure/success cannot regress paid or attach a second active operation.
- **Required variants**: Fresh checkout key, existing continuation key, initial creation-key replay bound to historical open operation; paid state established while dispatch/retrieval is outstanding; delayed failure result; new/current operation race; persisted state/history never records a paid regression.
- **Evidence kind**: Real PostgreSQL and coordinated outstanding-call tests using a confirmed-paid persisted prerequisite. This does not implement webhook/reconciliation confirmation.

#### LIFE-007: Validate all retrieved/created evidence before accepting lifecycle results
- **Status**: NEW
- **Source**: FR-002, FR-005, FR-012, FR-015; Checkout contract / Orders, operations, and continuation, evidence/eligibility clauses; Stripe request, deadlines, and recovery, response-validation clause.
- **Behavior**: First and replayed creation results and retrievals must correlate to local order/operation, associated IDs where known, immutable amount/currency, payment mode, and sandbox evidence before their result enables an open URL or safe expiration/replacement. Unknown/mismatched evidence preserves unresolved/investigation and cannot establish payment or eligibility.
- **Required variants**: Correct session/client_reference/order and operation metadata, amount_total, `usd`, mode payment, livemode false; wrong/missing correlation, wrong amount/currency/mode/live flag, differing known session/PaymentIntent IDs; missing required session ID/open URL; unknown state combinations; complete/paid retrieval with nonempty/matching PaymentIntent still awaits later confirmation, stays unresolved, and blocks replacement; absent/mismatched PaymentIntent in purported paid evidence also cannot confirm or enable replacement.
- **Evidence kind**: Payment-rule prepared objects and real SDK local-server decoding/result validation tests; persisted unresolved/history/flag assertions. Signed webhook/PaymentIntent-only confirmation is not Feature 2 scope.

#### LIFE-008: Expose exact locally observed action predicates
- **Status**: NEW
- **Source**: FR-013, FR-014, FR-015, FR-019; Checkout contract / Authenticated HTTP API, flags clause; Orders, operations, and continuation, expiry clause.
- **Behavior**: Flags describe permitted actions from last-observed local eligibility, and POST rechecks actual evidence and transactional guards. For unpaid work: can_resume requires open+saved URL+future saved expiry; can_retry_same_operation requires prepared or unresolved with supported snapshot and known ID or time strictly before first_dispatch_at+23h, and no mismatched evidence; can_start_new_attempt requires current confirmed rejection without prior ambiguity or verified expired/unpaid with no other potentially active operation. All flags are false for paid.
- **Required variants**: Each required predicate present versus independently missing/false; expiry just before/at/after saved expiry; retry age just before/at/after 23h; known-ID unresolved at any age; unsupported snapshot or mismatched evidence false; prepared true for same-operation retry; open/complete_unpaid/paid not unresolved-retry candidates; rejected with earlier ambiguity not new-attempt eligible; noncurrent bound operation replay versus current order read; locally eligible read followed by blocking refreshed POST.
- **Evidence kind**: Automated explicit predicate/HTTP tables and real PostgreSQL current/bound operation sequences; evaluate exact boundaries without time sleeps.

#### LIFE-009: Retain an eligible prepared attempt when dispatch budget is exhausted
- **Status**: NEW
- **Source**: FR-009, FR-011, FR-013, FR-015; Checkout contract / Authenticated HTTP API, flags and budget-exhaustion clause.
- **Behavior**: After retrieval verifies new-attempt eligibility but exhausts remaining wire/time budget, a durably prepared new operation returns 202. Its bound key can dispatch later under that identity without another operation.
- **Required variants**: Exhausted combined attempt budget; elapsed budget insufficient for another call; eligible prepared operation persists with history/key; later same-key dispatch retains snapshot; no unsafe dispatch beyond limits and no extra operation on retry.
- **Evidence kind**: Payment-rule and connected PostgreSQL/SDK request-sequence tests counting combined wire attempts and inspecting deferred intent.

### Stripe dispatch and bounded execution

#### STR-001: Send the immutable authoritative hosted card-payment snapshot
- **Status**: NEW
- **Source**: FR-001, FR-002, FR-005, FR-008, FR-009; Checkout contract / Stripe request, deadlines, and recovery, parameter snapshot clause.
- **Behavior**: Each creation sends the persisted compatible-version snapshot for hosted one-time immediate automatic-capture card payment: mode payment, explicit card-only methods, one Stripe line item quantity 1 with accepted inline product name/amount/currency, client_reference_id order ID, and session plus payment_intent_data metadata containing order/operation IDs. Pricing-changing/recovery features are disabled.
- **Required variants**: First dispatch and every retry/restart identical parameters/key; automatic tax, discounts/promotion codes, adaptive pricing, after-expiration recovery disabled; no credentials in metadata/URLs/purchase; API-version-compatible hosted-mode representation; inline amount/name exactly persisted; application remains a one-product model despite adapter's required line item.
- **Evidence kind**: Real pinned Stripe SDK against test-local HTTP server asserts outgoing form/header values; PostgreSQL snapshot inspection and source/configuration inspection establish fixed disabled options and separation from browser input.

#### STR-002: Commit dispatch identity and immutable correlation before a possible send
- **Status**: NEW
- **Source**: A-06, A-08, SC-03; FR-007, FR-009, FR-010, FR-019; Checkout contract / Stripe request, deadlines, and recovery, snapshot/first dispatch/ambiguity clauses.
- **Behavior**: Before each possible Stripe mutation, durable local identity, Stripe key, snapshot, conservative ambiguity marker, and last possible dispatch time are saved. First dispatch fixes server-time first_dispatch_at and integer Unix expiry 23h59m later, and records unresolved transition/history before external work.
- **Required variants**: External observer/independent DB connection can see committed prerequisite before call; failure of pre-call commit sends nothing; interruption after marker but before wire call remains unresolved; first and last possible dispatch timestamps survive restart; subsequent sends never recompute first dispatch/expiry/key; correlation persists even with no saved response/session/request ID.
- **Evidence kind**: Connected controlled-dispatch and real PostgreSQL ordering/failure tests, including independent connection observation before external response release.

#### STR-003: Freeze return URLs and SDK/API snapshot compatibility per operation
- **Status**: NEW
- **Source**: FR-008, FR-009, FR-010, FR-019; Checkout contract / Stripe request, deadlines, and recovery, URLs/version clause.
- **Behavior**: Snapshot uses validated origin plus `/?order_id=<id>&checkout_return=success` or cancel; URLs, metadata, API version, key, and expiry remain fixed across replay/restart. Unsupported old versions remain unresolved, never silently migrate or issue a changed creation.
- **Required variants**: Local origin with/without trailing slash/port/IPv6 yields specified credential-free URL; changed base URL/API configuration after restart leaves saved snapshot unchanged; compatible snapshot reused; incompatible stored version returns unresolved/investigation and disables same-operation replay/new attempt.
- **Evidence kind**: PostgreSQL restart/configuration tests and SDK outgoing request assertions; SDK/API exact pin presence inspected under DO-004 before SDK-based test authoring.

#### STR-004: Bound real work from protected admission through response completion
- **Status**: NEW
- **Source**: A-07, SC-03; FR-011, FR-022; Checkout contract / Stripe request, deadlines, and recovery, serving budgets clause.
- **Behavior**: Overall request deadline begins at protected admission and covers body reads, DB operations, Stripe calls, waits, final persistence/response. Earlier parent cancellation/deadline wins. Each Stripe call is bounded by call timeout and remaining shared retry budget; final database/response work is bounded by the remaining one-second reserve. Blocking body reads actually terminate; canceled owned work completes within its bound.
- **Required variants**: Stalled Content-Length and chunked bodies; blocked DB before dispatch and after external success; hung local SDK response/header/body; cancellation during call, database wait, retry wait, body read, and final persistence; earlier parent deadline versus configured deadline; budget exhausted without success retains accepted work recoverably; no work continues unbounded after response/shutdown. Invalid body read returns 400 when response is possible, not a false successful empty body.
- **Evidence kind**: Real transport/SDK local-server tests and real PostgreSQL cancellation tests prove elapsed bounds and completion; fake tests establish outcome policy. Context field inspection alone is insufficient evidence of actual stream cancellation.

#### STR-005: Enforce one combined wire-attempt and elapsed retry budget
- **Status**: NEW
- **Source**: FR-011, SC-03; Checkout contract / Stripe request, deadlines, and recovery, serving budgets clause.
- **Behavior**: STRIPE_MAX_ATTEMPTS bounds total actual HTTP wire attempts across creation and retrieval combined in one incoming request; all calls/waits share retry elapsed budget. SDK retries count, and an outer retry owner disables SDK automatic retries.
- **Required variants**: Limits 1/2/3; create-only retry; retrieval plus subsequent creation/retries share total; SDK hidden-retry multiplication cannot exceed total; fast failures hit attempts while slow calls/waits hit elapsed bound; remaining time caps the last call; exhausted work stays recoverable under same identity.
- **Evidence kind**: Real SDK local-server wire counters/timing and payment-rule tests; configuration/source inspection establishes actual retry ownership. Do not infer wire count from fake business-call count alone.

#### STR-006: Respect bounded cancellable backoff and Stripe retry instructions
- **Status**: NEW
- **Source**: FR-011, FR-012; Checkout contract / Stripe request, deadlines, and recovery, retry waits clause.
- **Behavior**: Attempt 2 waits at least 250ms and attempt 3 500ms; waits are cancellable, fit remaining elapsed budget, and honor longer valid Retry-After. Stripe-Should-Retry false suppresses retry; insufficient time for a required wait stops unresolved.
- **Required variants**: Default second/third waits; valid longer Retry-After versus shorter header minimum; required header wait cannot fit remaining budget; cancellation/deadline during wait prevents later wire call; Stripe-Should-Retry false on otherwise retryable response; delay/attempt total counted within STR-005.
- **Evidence kind**: Payment-rule controlled-time tests and real SDK local-server response-header/attempt timing evidence; establish no subsequent call after cancellation or insufficient budget.

#### STR-007: Retry only supported transient outcomes without changing identity
- **Status**: NEW
- **Source**: A-06, A-07, FR-010, FR-011, FR-012; Checkout contract / Stripe request, deadlines, and recovery, retry classification clause.
- **Behavior**: Transport timeout/reset, 429, and documented transient conflict/5xx retry only the same snapshot/key within safe age and request bounds. Indeterminate 5xx remains unresolved even when cached failure repeats; timeout never proves business rejection.
- **Required variants**: Transport timeout; connection reset/response loss; 429; documented transient conflict; 5xx first and repeated/cached; no retry beyond safe cutoff or when retry header/budget forbids; successful same-key recovery preserves one logical session; exhausted indeterminate 5xx sets investigation.
- **Evidence kind**: Payment-rule fakes and real SDK local-server controlled transport/error tests, with PostgreSQL key/state/history and safe-age assertions.

#### STR-008: Distinguish definitive first rejection from uncertain evidence
- **Status**: NEW
- **Source**: A-02, FR-012, FR-015, FR-019; Checkout contract / Stripe request, deadlines, and recovery, classification clause.
- **Behavior**: A fully observed structured pre-execution parameter rejection or sandbox credential/permission rejection with no earlier ambiguous dispatch is confirmed rejected without automatic retry. Generic/uncertain errors and any rejection after earlier ambiguity remain unresolved for investigation. Changing parameters requires an eligible distinct operation; replay of definitive rejection returns its saved rejection/IDs without creation.
- **Required variants**: First structured validation rejection; first sandbox credential/permission rejection; same responses after response loss/interruption/uncertain earlier dispatch; generic 4xx, idempotency mismatch, malformed success, missing required ID/open URL, correlation mismatch, uncertain dependency errors; hosted decline remains open/unpaid or complete_unpaid by verified evidence, never a fresh-key creation justification.
- **Evidence kind**: Payment-rule fake plus real SDK structured-error/decoding tests and persisted dispatch-history prerequisite sequences; inspect no automatic retry/duplicate mutation and correct HTTP rejection/unresolved result.

### Interruption and recovery

#### REC-001: Recover external creation whose response is lost
- **Status**: NEW
- **Source**: A-06, SC-03; FR-009, FR-010, FR-013; Checkout contract / Stripe request, deadlines, and recovery, same-operation replay clause.
- **Behavior**: After Stripe processes a mutation but response is lost, accepted work remains inspectable locally. Safe retry uses original operation/key/snapshot, validates replayed evidence, and recovers the established session without duplicate logical creation.
- **Required variants**: Response lost before local session ID saved; caller response loss after local result saved; repeated retry; missing Stripe request ID versus available supplemental request ID; unresolved status/history before recovery and correctly associated result afterward.
- **Evidence kind**: Connected authenticated HTTP/real PostgreSQL/real SDK local-server injected response-loss tests maintaining observable simulated external object identity; verify same key/parameters and one logical session, not merely one incoming HTTP request.

#### REC-002: Recover restart after external success before local commit
- **Status**: NEW
- **Source**: A-08, SC-03; FR-009, FR-010, FR-017, FR-019; Checkout contract / Stripe request, deadlines, and recovery, immutable dispatch clause.
- **Behavior**: A process/service restart after external success but before local result commits recovers original identity, price, Stripe key, correlation, URLs/version/expiry and snapshot without another logical Checkout. Failed partial local changes/history remain absent.
- **Required variants**: No saved Stripe object ID at interruption; result observed but result transaction not committed; interrupted before response receipt; restarted dependencies have no process-memory identity; configuration values changed without repricing/recomputing snapshot; current operation remains recoverable until established; incompatible version remains unresolved instead of automatic migration.
- **Evidence kind**: Connected controlled interruption/reconstruction tests against retained real PostgreSQL and local SDK endpoint, using fresh service dependencies; inspect durable state/history and external logical identity across restart.

#### REC-003: Refuse blind creation at the exact safe-age boundary
- **Status**: NEW
- **Source**: A-11, SC-07; FR-010, FR-015, FR-019; Checkout contract / Stripe request, deadlines, and recovery, 23h replay clause.
- **Behavior**: With no saved session ID, same-operation POST replay is allowed only strictly before first_dispatch_at+23h. At or beyond cutoff issue no creation POST, retain unresolved/investigation, and block replacement. Known IDs use evidence GET at any age; missing result/request ID cannot justify another creation.
- **Required variants**: Just before/exactly at/after 23h; restart at cutoff; same original key and fresh continuation key; known session ID older than cutoff retrieves without creation; insufficient/mismatched read evidence preserves unresolved; local 23h59m expiry is a separate boundary and cannot extend replay eligibility.
- **Evidence kind**: Payment-rule controlled-time tests, SDK wire-method assertions, and real PostgreSQL/HTTP persisted-outcome tests.

#### REC-004: Report exact investigation triggers without automatic replacement/recovery
- **Status**: NEW
- **Source**: FR-012, FR-013, FR-019; Checkout contract / Stripe request, deadlines, and recovery, investigation and later recovery clauses.
- **Behavior**: investigation_required is an advisory, not failed payment or permission for new creation. It is true for unresolved age at least 15 minutes from first dispatch, exhausted safe retry age, mismatched evidence, indeterminate 5xx, incompatible snapshot versions, or confirmed configuration/integration rejection. It starts no background task; safe same-key replay may still recover network ambiguity.
- **Required variants**: Unresolved just before/at/after 15m; young network ambiguity without other trigger; young mismatch/5xx/incompatible version; confirmed configuration/integration rejection; 23h exhausted age; absent first dispatch on prepared work; advisory true inside safe age still allows otherwise-safe replay; no automatic list/recreate/force-paid/reset/background recovery.
- **Evidence kind**: Explicit payment-rule/HTTP flag tests and persisted outcome/source inspection. Feature 5 listing and operator reporting are dependency obligations outside this phase's runtime scope.

### PostgreSQL consistency and history

#### DATA-001: Commit business state and corresponding history atomically
- **Status**: NEW
- **Source**: AS-04, SC-05; FR-009, FR-016, FR-017; Checkout contract / Orders, operations, and continuation, atomic guard clause.
- **Behavior**: Initial order/operation/key acceptance, dispatch marker/state, saved external result, and eligible later-attempt changes each commit with their corresponding append-only history in one PostgreSQL transaction. Failure leaves neither a partial state nor orphan/missing audit effect.
- **Required variants**: Successful atomic initial acceptance, first dispatch, open/rejected/unresolved/expired observations, later attempt preparation; injected failure between business and history writes and at commit; retry after failure creates only the actual committed transition entries; no external dispatch when prerequisite transaction failed. Paid fixture changes belong to later confirmation, but Feature 2 cannot regress them.
- **Evidence kind**: Real PostgreSQL migrations and transaction-failure tests inspect persisted state/bindings/history from independent connections; connected failure evidence in HTTP-007/REC-002.

#### DATA-002: Enforce persisted uniqueness and immutable snapshots across connections
- **Status**: NEW
- **Source**: A-03, A-18, SC-02, SC-05; FR-006, FR-008, FR-017, FR-018; Checkout contract / Orders, operations, and continuation, atomic guard clause.
- **Behavior**: PostgreSQL constraints/transactions enforce globally unique request and Stripe keys, unique associated session/payment IDs, immutable request bindings/purchase snapshots, and one current potentially active operation per order across independent connections/processes.
- **Required variants**: Concurrent identical/conflicting initial keys; different continuation keys racing on one eligible order; collisions in Stripe keys/session IDs/PaymentIntent IDs; attempts to alter an accepted binding/purchase snapshot; independent connections observe one committed winner with valid state/history; multiple application instances/process-local lock independence.
- **Evidence kind**: Deliberately coordinated real PostgreSQL concurrent transactions and constraint/invariant inspection with application migrations. Go race detection is additional evidence under FND-004 and cannot establish this guarantee.

#### DATA-003: Preserve durable dispatch coordination and transactional stale-result guards
- **Status**: NEW
- **Source**: A-18, FR-010, FR-015, FR-018, FR-022; Checkout contract / Orders, operations, and continuation, coordination/paid guard clause.
- **Behavior**: Dispatch/recovery overlap uses persisted coordination and the same Stripe key; eligibility and committing changes recheck paid/current state. Losing ownership or cancellation retains recoverability and cannot admit another attempt or attach stale results to another operation.
- **Required variants**: Overlapping same-operation dispatch/recovery through independent DB connections; ownership lost/canceled with outstanding call; paid/current operation changed before result commit; concurrent fresh-key replacement eligibility; verified expiration of every earlier attempt required before dispatching later session; no process-local-only synchronization.
- **Evidence kind**: Controlled overlap tests with real PostgreSQL, observable external calls, released barriers, and stored state/history assertions; Feature 3/5 entry points remain future scope.

#### DATA-004: Preserve reconstructable append-only per-order audit history
- **Status**: NEW
- **Source**: AS-03, AS-04; FR-016, FR-017; Checkout contract / Authenticated HTTP API, history clause.
- **Behavior**: History associates relevant local operations/state/recovery outcomes and available Stripe identifiers with order, uses strictly increasing per-order sequences and UTC observation timestamps, and is append-only through normal paths. Read/replay/unchanged observations add no duplicate business-transition entry.
- **Required variants**: Entry fields sequence/kind/recorded_at/order_id/nullable operation_id/from_state/to_state/session/PaymentIntent/event/request IDs/failure_code; relevant kinds order_created/operation_prepared/dispatch_started/operation_state_changed and future-compatible payment_confirmed/recovery_recorded representation; IDs absent/null versus available; separate external event time when present rather than causal ordering by it; concurrent order sequences strictly increase; identical replay/read/unchanged repeated observation preserves transition count; no normal update/delete or raw payload requirement.
- **Evidence kind**: Real PostgreSQL persisted rows and authenticated history response tests; source/migration inspection establishes available evidence retention and absence of normal mutation paths. Future event/recovery production is not required now.

#### DATA-005: Release database transactions during external network work
- **Status**: NEW
- **Source**: FR-011, FR-018, FR-022; Checkout contract / Orders, operations, and continuation, network/atomic guard clause.
- **Behavior**: Network calls do not hold a transaction open for their duration. Pre-call intent is already committed; post-call guards preserve correctness while unrelated successful operations can complete independently.
- **Required variants**: Blocked creation and blocked retrieval while another independent order progresses; persisted prerequisite visible externally; canceled/failed external call leaves recoverable operation; external result still performs committing guard after the wait.
- **Evidence kind**: Real PostgreSQL transaction/activity inspection plus barrier-coordinated connected requests; source inspection corroborates transaction boundaries. A passing fast happy-path test does not prove release during a stalled call.

### Authentication and visibility

#### SEC-001: Authenticate every feature endpoint before routing/parsing/effects
- **Status**: NEW
- **Source**: A-12, SC-06; FR-004; Checkout contract / Input and configuration, authentication clause.
- **Behavior**: Existing Basic authentication protects initial creation, checkout, status/history and HEAD access before protected routing or body parsing. Missing/invalid credentials cannot set price, read feature data, call Stripe, or affect durable state; valid authentication delegates to the feature contract without retained authorization leaking to another request.
- **Required variants**: Valid, missing, and invalid credentials on every introduced endpoint/method; successful authenticated request followed by credential-free request; deliberately overlapping valid and credential-free/wrong requests; protected unknown path/unsupported method/invalid body still rejects auth first; credential-bearing query/cookie/body cannot substitute for the existing Authorization boundary; no parser/DB/Stripe invocation on rejection.
- **Evidence kind**: New actual feature-endpoint tests plus real PostgreSQL/Stripe effect absence. `internal/web/authentication_test.go:154–409` proves authentication only with a test `/work` handler, including credential parsing/source/isolation; it is partial prerequisite evidence, not checkout coverage.

#### SEC-002: Minimize feature responses, logs, history, browser code, and Stripe parameters
- **Status**: NEW
- **Source**: A-12, SC-06; FR-001, FR-020; Checkout contract / Input and configuration, secrets; Authenticated HTTP API, response/error/history clauses; Stripe request, deadlines, and recovery, URL clause.
- **Behavior**: Feature paths never return/log/store secrets, card numbers/CVC, raw Stripe response bodies, or unnecessary personal data. Privileged service credentials stay out of browser-delivered code and Stripe request data; return URLs are credential-free and card entry remains hosted.
- **Required variants**: Successful creation/inspection, rejected submitted bodies, structured/unstructured dependency errors, timeout/retry/recovery logs/history, external response containing sensitive sentinels, Basic/Stripe/database secrets; response/history fields remain allowlisted; SDK authenticates with its sandbox Stripe secret only, without leaking application Basic credentials; credential-free metadata/URLs and browser assets.
- **Evidence kind**: Automated captured response/log/history/SDK request tests and source/template/configuration inspection. SDK authentication is necessary; this criterion excludes credentials from purchase parameters/metadata/URLs and application credentials from Stripe calls, not the Stripe API's required authentication header. Version-control packaging is DO-002.

#### SEC-003: Log sanitized correlated feature outcomes and failures
- **Status**: NEW
- **Source**: FR-021, FR-022; Q6 via Supporting Context; Checkout contract / Authenticated HTTP API, request ID clause.
- **Behavior**: Introduced operations/failures produce useful structured logs with request and known operation correlation identifiers and sanitized error context; explicit errors propagate without suppressing relevant failures.
- **Required variants**: Accepted/successful checkout, prepared/unresolved, confirmed rejection, input/auth rejection, database acceptance/result/read failure, Stripe timeout/server/mismatch, cancellation/ownership loss; known order/operation IDs included appropriately, request ID agrees with response; no sensitive error text/body from SEC-002.
- **Evidence kind**: Automated structured-log capture/field assertions and source inspection of error/context propagation. Existing generic request logs are a prerequisite, not proof of feature operation correlation.

### Foundation preservation

#### FND-001: Preserve exact public liveness/readiness behavior
- **Status**: EXISTS
- **Source**: FR-004, FR-021; Edge Cases, exact probes; Checkout contract / Authenticated HTTP API, public probe clause.
- **Behavior**: Exact /healthz and /readyz GET/HEAD remain public. Liveness does not depend on database readiness; readiness is bounded and distinguishes available/unavailable/recovered database. Probe-like protected paths do not become public; unsupported probe methods retain their existing contract.
- **Required variants**: Public GET/HEAD without Basic credentials; HEAD body empty; DB unavailable → readiness 503/liveness 200; dependency recovery → readiness 200; unsupported POST/DELETE → 405; /healthz/private, /readyz/private, and prefix variants remain unauthorized.
- **Evidence kind**: Existing automated assertions: `internal/web/authentication_test.go:531–582` sets real server boundary/counters and checks every named variant; `internal/web/server_test.go:52–98` checks no DB call for health, readiness result/recovery/deadline and correlation/sanitization; `internal/integration/foundation_test.go:89–125` verifies actual PostgreSQL outage/restart/recovered readiness and retained data. Feature payment readiness dependency changes require additional FND-002 evidence.
- **Existing file**: `internal/web/authentication_test.go`; `internal/web/server_test.go`; `internal/integration/foundation_test.go`

#### FND-002: Keep serving readiness truthful when checkout dependencies change
- **Status**: NEW
- **Source**: FR-003, FR-021; Q6 via Supporting Context; Checkout contract / Input and configuration, startup validation clause.
- **Behavior**: Invalid checkout serving configuration never exposes a serving HTTP process as ready; applicable dependency readiness reflects ability to serve application operations while liveness identifies a running process. Preserve the existing local migration/startup contract.
- **Required variants**: Valid configuration/migrated available database; invalid key/origin/currency/budgets; unavailable PostgreSQL and recovery; feature migration failure prevents useful serving readiness; externally unavailable Stripe is handled as bounded unresolved request work, not invented payment confirmation or background recovery.
- **Evidence kind**: Automated real command/startup/PostgreSQL/readiness tests and configuration/wiring inspection. No new live Stripe readiness dependency is prescribed.

#### FND-003: Preserve shutdown admission and complete canceled checkout work within budgets
- **Status**: MODIFY
- **Source**: FR-004, FR-011, FR-022; Edge Cases, foundation shutdown; Checkout contract / Stripe request, deadlines, and recovery, shutdown clause.
- **Behavior**: Shutdown rejects new protected work before body reads/effects, allows active requests within grace, cancels overdue contexts, waits for owned work before cleanup, and ends within grace+cleanup bound. Cleanup failure produces nonzero process exit. Interrupted checkout remains durably recoverable.
- **Required variants**: Authenticated and unauthenticated admission after stop; active checkout completes in grace; blocked Stripe/DB/body work canceled after grace; unfinished accepted operation persists safely; cleanup succeeds after work exits, fails, or exceeds timeout; default grace 10s/cleanup 5s/total 15s; nonzero real command exit on cleanup failure.
- **Evidence kind**: Extend foundation tests with actual checkout/DB/SDK cancellation and persisted outcomes; command/source verification of nonzero exit. `internal/web/authentication_test.go:38–86` proves shutdown rejects test protected handler/body; `internal/web/server_test.go:100–244` proves grace/cancellation/cleanup error/bound with test handlers; these do not establish checkout recoverability or process exit by themselves.
- **Existing file**: `internal/web/server_test.go`; `internal/web/authentication_test.go`; relevant command evidence to be located by plan-tests.

#### FND-004: Bound owned checkout work and synchronize affected concurrent paths
- **Status**: NEW
- **Source**: A-18; FR-011, FR-018, FR-022; Supporting Context, Q4/Go race detection.
- **Behavior**: Owned concurrent work has cancellation/completion and bounded lifetime; shared state is synchronized. Relevant coordinated request/dispatch/recovery paths complete without races while unfinished operations remain recoverable and independent successful work survives another failure.
- **Required variants**: Concurrent identical/conflicting requests, independent orders, outstanding call ownership/cancellation, earlier parent/request/shutdown cancellation; canceled work exits with no unbounded goroutine; one failed/canceled operation does not lose another committed success; database invariants separately verified by DATA-002/003.
- **Evidence kind**: Automated barrier-coordinated completion/outcome tests run under Go race detection, with real PostgreSQL for persisted-state overlap. No worker pool is introduced by this criterion; future reconciliation workers remain outside scope.

## Delivery Obligations

- **DO-001** — Source: spec Delivery Obligations DO-001; FR-013/014/021. Deliver API usage for initial creation, continuation, status/history, authentication, input/key contract, error/outcome interpretation, local IDs and flags. Completion: application documentation permits creation and inspection of accepted unresolved work without starting another operation and describes the delivered envelope/pagination/HEAD behavior.
- **DO-002** — Source: spec DO-002; Input and configuration; FR-020. Supply committed placeholder configuration examples, ignored local `.env`, sandbox/local startup and relevant Stripe setup prerequisites. Completion: examples describe exact validated defaults/ranges and local-origin/sandbox requirements, local configuration is ignored, and inspected tracked files contain no secrets. Sandbox setup supports the USD charge range. Later webhook/command setup belongs to its delivering feature.
- **DO-003** — Source: spec DO-003; FR-010/012/015/019. Document supported continuation/lifecycle, exact safe replay boundary, immutable identities/evidence, unresolved and investigation meanings, and operator inspection without force-paid/reset/recreate override. Completion: documented API/recovery examples preserve keys/purchase data and explain open/complete_unpaid/expired/rejected/paid behavior and unsupported external dashboard actions such as refunds. Shared confirmation/listing bounds remain explicit future feature contracts.
- **DO-004** — Source: spec DO-004; Stripe request, deadlines, and recovery, SDK pin clause; Supporting Context agreed stack/Q1–Q4. Supply fake-based payment rules, real PostgreSQL/migrations, focused real SDK/local HTTP, connected HTTP-payment-DB-SDK, controlled response-loss/precommit-restart, and independent-connection concurrency evidence. Pin exact Stripe SDK/compatible API version before SDK-based test authoring and freeze those pins with reviewed tests; record that version in durable snapshots. Completion: evidence maps every automated outcome/named variant without live Stripe credentials or availability, verifies real wire parameters/retries/cancellation, and coordinates concurrency without sleeps. Multiple tests/evidence locations may establish one criterion.
- **DO-005** — Source: spec DO-005; FR-022; Supporting Context Q7/A-13. Keep documented local/CI checks aligned for formatting, golangci-lint, govulncheck, all unit/integration tests, and Go race detection. Completion: full required project checks cover added payment paths without narrowing foundation checks; report actual outcomes and material gaps. No acceptance-phase execution is implied by this obligation.
- **DO-006** — Source: spec DO-006; FR-016/017/018/022; Supporting Context application conventions. Deliver versioned migrations, authoritative SQL/sqlc generation, and current API/configuration/architecture/recovery documentation. Completion: migrations create the business schema/invariants and apply through the normal commands; generated SQL agrees with source; docs describe delivered behavior/structure. Payment rules remain independently testable, HTTP/business/persistence/SDK responsibilities follow application conventions, explicit errors/context propagation and generated-source discipline are inspected.

## Coverage and evidence boundaries

| Spec source | Required criteria / obligations |
| --- | --- |
| AS-01 | INP-001/002/003, HTTP-001/002, STR-001/002, DATA-001 |
| A-03 | HTTP-003, ID-001/002, DATA-002/003, FND-004 |
| AS-02 | ID-003/004, LIFE-003/004/005/006 |
| A-06 | STR-002/007, REC-001, ID-001 |
| A-07 | HTTP-003, STR-004/005/006/007, REC-004 |
| A-08 | HTTP-007, REC-002, STR-002/003, DATA-001 |
| A-02 | LIFE-001/002/004/005, STR-008, HTTP-006 |
| AS-03 | HTTP-002/005/006, DATA-004, LIFE-008, SEC-001 |
| A-12 | CFG-001/002/003, INP-001–006, HTTP-004, SEC-001/002 |
| AS-04 | DATA-001/004, HTTP-007 |
| A-11 | REC-003/004, LIFE-005/008 |
| A-18 | DATA-002/003/005, LIFE-004/006, FND-004 |
| FR-001–003 | CFG-001/002/004, INP-001–005, HTTP-001, STR-001 |
| FR-004 | SEC-001, HTTP-005, FND-001/002/003 |
| FR-005 | HTTP-001, LIFE-001/002/005/006/007, STR-001 |
| FR-006–008 | INP-003, CFG-001, ID-001–004, LIFE-003, DATA-002 |
| FR-009–010 | HTTP-007, STR-001/002/003/007, REC-001/002/003, DATA-001/003 |
| FR-011 | CFG-003, STR-004/005/006/007, LIFE-009, FND-003/004 |
| FR-012 | LIFE-001/005/007, STR-007/008, REC-004, HTTP-004 |
| FR-013–014 | HTTP-002/003/006, ID-004, LIFE-008/009, REC-001/004 |
| FR-015 | LIFE-001–009, ID-004, STR-008, DATA-003 |
| FR-016–018 | DATA-001–005, HTTP-006/007, LIFE-004/006, DO-006 |
| FR-019 | REC-001–004, STR-002/003, LIFE-005/008, DO-003 |
| FR-020–022 | SEC-002/003, FND-001–004, STR-004, DATA-003/005, DO-001/002/005/006 |
| SC-01 | HTTP-001/002, STR-001/002, DATA-001 |
| SC-02 | ID-001/002/003, DATA-002, LIFE-004 |
| SC-03 | HTTP-003/007, STR-004–008, REC-001/002 |
| SC-04 | HTTP-006, LIFE-001/002/005/006/007/008 |
| SC-05 | DATA-001–005, LIFE-004/006, FND-004 |
| SC-06 | INP-001–006, HTTP-004, SEC-001/002/003 |
| SC-07 | REC-003, LIFE-005/008 |
| Edge Cases (all ten bullets) | INP-001–006; ID-001–004; STR-004–008; REC-001–004; LIFE-001–008; DATA-001–003; SEC-001; FND-001/003 |
| Spec DO-001–006 | DO-001–006, retaining each deliverable/completion check separately |

Concrete wrong-implementation counterexamples retained for the test-planning audit:

- A `/work` authentication test remains green while a new API handler is mounted publicly: SEC-001 requires actual endpoints and effect absence.
- A process-local mutex deduplicates one server while two PostgreSQL connections create two active operations: ID-001/002 and DATA-002/003 require independent connections and database guards.
- A context deadline returns while a body read or SDK response read remains blocked: INP-005/006, STR-004 and FND-003/004 require actual stream/work completion.
- A retry loop counts business calls but SDK retries multiply wire attempts, or retrieval and creation each receive a fresh retry budget: STR-005 requires combined actual wire/elapsed evidence.
- A rejection after response loss is treated as definitive, or a missing ID/locally expired URL creates a fresh attempt: STR-008, LIFE-004/005 and REC-003 preserve ambiguity and verified eligibility.
- A Stripe success appears in the response although its local transaction failed, or state commits without history: HTTP-007 and DATA-001 require persistence-failure evidence.
- A replay recomputes price, return URL, version, or expiry after configuration change/restart: CFG-001, STR-002/003 and REC-002 inspect the persisted snapshot.
- A late response overwrites paid or attaches another operation; an old bound key reports the newest operation: LIFE-006, DATA-003 and ID-004 require sequenced stale-result/binding evidence.
- A read has the correct status but creates history or invokes Stripe; pagination repeats/skips entries: HTTP-006 and DATA-004 require before/after effects and sequence assertions.
- A mostly correct flag ignores equality at expiry/23h/15m, snapshot incompatibility, prior ambiguity, or historical bound operations: LIFE-008 and REC-004 name every predicate/boundary.

## Gaps & Open Questions

- No unresolved Feature 2 behavioral policy is identified. SDK/API exact pin selection belongs before SDK-based test authoring (DO-004), without altering this contract. Exact Go interfaces/schema placement and evidence inventory belong to plan-tests; no implementation interfaces are chosen here.
- Existing evidence covers foundation predicates only as cited. Checkout-specific assertions, real feature connected execution, failure/restart/concurrency tests and delivery checks are required evidence awaiting later phases. No Feature 2 runtime checks or full project checks are claimed executed by acceptance derivation.
- Later Features 3/5 own signed confirmation, event acceptance/deduplication, missing-ID session listing (created interval, candidate validation, 10×100 page ceiling and 30s recovery budget), and worker/queue/shutdown selection. Feature 2 preserves durable first/last-dispatch/key/snapshot/IDs and safe guards for those contracts; green Feature 2 criteria do not imply those later journeys are complete.
