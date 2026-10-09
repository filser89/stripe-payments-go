# Acceptance Criteria: Authenticate service and browser access

**Feature**: `001-authentication-refresh`
**Spec**: [spec.md](spec.md)
**Status**: Draft

## Summary

- Total criteria: 22
- Required behavioral variants: 108
- New: 20 | Modify: 2 | Exists: 0
- Categories: Serving and command configuration; Protected boundary and credentials; Landing and transport; Foundation compatibility; Secrets and observability; Browser journey
- Source notation: numbered Acceptance Scenario and Edge Case references identify their ordered spec entries; FR IDs are literal requirements.

## Criteria

### Serving and command configuration

#### CFG-001: Serving startup validates account settings
- **Status**: NEW
- **Source**: Acceptance Scenario 1; FR-001; Edge Cases 1, 9
- **Behavior**: Starting serve with otherwise valid runtime settings accepts valid credentials; invalid Basic settings prevent accepting requests, exit nonzero, and identify only the invalid setting.
- **Required variants**: V1: accepted username lengths 1/128; V2: rejected 0/129; V3: accepted password lengths 1/256; V4: rejected 0/257; V5: each missing setting; V6: username space/colon/control/non-ASCII; V7: password control/non-ASCII; V8: password spaces/colon accepted; V9: invalid diagnostic values sanitized
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### CFG-002: Local commands retain independent configuration
- **Status**: NEW
- **Source**: Acceptance Scenario 9; FR-002
- **Behavior**: Actual probe/migrate execution ignores absent or invalid Basic settings while preserving existing runtime/database requirements and outcomes.
- **Required variants**: V1: probe ready; V2: probe not ready; V3: probe unavailable bounded; V4: migrate successful; V5: invalid database/runtime setting still fails; V6: each command with absent/invalid Basic settings
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### CFG-003: Credential lifetime and restart rotation
- **Status**: NEW
- **Source**: Acceptance Scenario 11; FR-002
- **Behavior**: The running service uses its startup pair until restart; restarting uses the new pair.
- **Required variants**: V1: environment changed while running: original succeeds/new fails; V2: restart: original fails/new succeeds
- **Evidence kind**: automated test; establish every listed outcome and variant.

### Protected boundary and credentials

#### AUTH-001: Missing credentials reject before work
- **Status**: NEW
- **Source**: Acceptance Scenario 2; FR-003, FR-005; Edge Case 4
- **Behavior**: A credential-free protected request returns generic challenge rejection before handler/body/side effects.
- **Required variants**: V1: fresh missing header; V2: missing header after authenticated success; V3: GET/HEAD rejection headers; V4: HEAD no body
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### AUTH-002: Exact credential matching
- **Status**: NEW
- **Source**: Acceptance Scenario 3; FR-004; Edge Case 1
- **Behavior**: Only the exact configured username/password authenticates.
- **Required variants**: V1: wrong username; V2: wrong password; V3: both wrong; V4: case mismatch in either; V5: password leading/trailing spaces significant; V6: supported min/max byte bounds; V7: no trimming/normalization
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### AUTH-003: Authorization syntax and header bounds
- **Status**: NEW
- **Source**: Acceptance Scenario 5; FR-004, FR-005; Edge Cases 1–3
- **Behavior**: Malformed or unsupported Authorization is rejected before work, while valid scheme case variants authenticate.
- **Required variants**: V1: empty field; V2: Basic without token; V3: unsupported scheme; V4: malformed Base64; V5: missing separator; V6: decoded empty username/password; V7: invalid controls/non-ASCII incl invalid UTF-8; V8: repeated fields; V9: mixed-case Basic; V10: password separator colon retained; V11: matching Basic token with legal scheme-separator SP padding to 4096 bytes accepted; invalid in-cap values still rejected; V12: equivalent matching-token value with separator SP padding to 4097 bytes rejected before decoding
- **Evidence kind**: automated test; establish every listed outcome and variant.

- **Notes**: AUTH-003 V11/V12 use the one-or-more-SP scheme separator permitted by [RFC 9110 §11.4](https://www.rfc-editor.org/rfc/rfc9110.html#section-11.4); decoded credentials remain supported and exact. This distinguishes cap enforcement from malformed-input rejection.

#### AUTH-004: Credentials are accepted only from Authorization
- **Status**: NEW
- **Source**: Acceptance Scenario 5; FR-004
- **Behavior**: Only Authorization authenticates protected access.
- **Required variants**: V1: query-only credentials; V2: cookie-only credentials; V3: body-only credentials; V4: rejected body not read
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### AUTH-005: Delegation preserves request and result
- **Status**: NEW
- **Source**: Acceptance Scenario 4; FR-006
- **Behavior**: Valid credentials delegate exactly once and preserve request and handler response.
- **Required variants**: V1: method/path/query/body intact; V2: handler success status/header/body; V3: handler error status/header/body; V4: no auth-created effects
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### AUTH-006: Concurrent caller isolation
- **Status**: NEW
- **Source**: Acceptance Scenario 12; FR-010
- **Behavior**: Overlapping valid and invalid requests have independent outcomes and exact handler invocation count.
- **Required variants**: V1: coordinated valid/invalid/missing callers; V2: rejected body/side effects untouched; V3: race-detected execution
- **Evidence kind**: automated test; establish every listed outcome and variant.

### Landing and transport

#### HTTP-001: Protected static landing response
- **Status**: NEW
- **Source**: Acceptance Scenario 7; FR-007
- **Behavior**: Authenticated GET / yields the static service identification HTML; HEAD yields corresponding headers without body.
- **Required variants**: V1: GET success; V2: HEAD success; V3: query preserved/accepted; V4: no credential/form/payment-control/token HTML
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### HTTP-002: Authentication before routing and methods
- **Status**: NEW
- **Source**: Acceptance Scenario 7; FR-003, FR-007; Edge Case 8
- **Behavior**: Authentication precedes normal route/method handling and never makes unsupported requests successful.
- **Required variants**: V1: authenticated unsupported method on / rejected; V2: authenticated unknown path 404; V3: missing credentials on both rejected first; V4: no downstream business work
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### HTTP-003: Actual body bytes and normal EOF
- **Status**: NEW
- **Source**: Acceptance Scenario 8; FR-008; Edge Case 5
- **Behavior**: Landing accepts actual byte-empty EOF and rejects any received body byte with generic 400 using bounded reading.
- **Required variants**: V1: nil/ordinary empty EOF accepted; V2: zero-byte EOF reader accepted; V3: immediate nonempty byte rejected; V4: declared length cannot bypass actual-byte inspection; V5: GET/HEAD nonempty rejection and HEAD no body
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### HTTP-004: Body-read failures never become success
- **Status**: NEW
- **Source**: Acceptance Scenario 8; FR-008; Edge Cases 5, 6
- **Behavior**: An operating connection with a body-read failure receives generic 400 without false success or sensitive error details.
- **Required variants**: V1: zero-byte injected read failure; V2: byte plus read failure; V3: GET/HEAD response/header/body contracts; V4: transport/shutdown inability to respond distinguished
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### HTTP-005: Real streamed-body handling
- **Status**: NEW
- **Source**: Acceptance Scenario 8; FR-008; Edge Case 5
- **Behavior**: Real HTTP streaming bodies obey actual emptiness and nonempty rejection.
- **Required variants**: V1: real empty unknown-length/chunked stream accepted; V2: real nonempty chunked stream rejected; V3: no reliance only on recorder ContentLength=-1
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### HTTP-006: Stalled-body request deadline
- **Status**: NEW
- **Source**: Acceptance Scenario 8; FR-008, FR-009; Edge Cases 5, 6
- **Behavior**: A real stalled request body cannot wait beyond the foundation read budget; no false successful landing result occurs.
- **Required variants**: V1: chunked stream stalls before first byte; V2: configured read deadline effective; V3: close/transport failure allowed when response impossible
- **Evidence kind**: automated test; establish every listed outcome and variant.

### Foundation compatibility

#### FND-001: Exact public probe boundaries
- **Status**: NEW
- **Source**: Acceptance Scenario 6; FR-003, FR-009
- **Behavior**: Exact probes remain public with preserved liveness/readiness/method semantics and no protected work.
- **Required variants**: V1: GET/HEAD health/ready without credentials; V2: failed readiness/recovery; V3: readiness deadline; V4: POST/other probe method rejection; V5: health/ready prefix paths protected
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### FND-002: Shutdown admission and bounded lifecycle
- **Status**: MODIFY
- **Source**: Acceptance Scenarios 6, 12; FR-009
- **Behavior**: Foundation shutdown preserves admission rejection, active draining/cancellation, resource cleanup, and bounded lifetime.
- **Required variants**: V1: shutdown protected with/without credentials may return existing 503 first; V2: admitted authenticated work completes within grace; V3: overdue authenticated work canceled; V4: cleanup error/timeout; V5: serve failure cleanup
- **Evidence kind**: automated test; establish every listed outcome and variant.
- **Existing file**: `internal/web/server_test.go`

#### FND-003: PostgreSQL readiness and cancellation
- **Status**: MODIFY
- **Source**: Acceptance Scenarios 6, 12; FR-009
- **Behavior**: Real PostgreSQL readiness recovers after outage and active database work is canceled before shutdown cleanup.
- **Required variants**: V1: database unavailable/restarted readiness; V2: retained data/migrations; V3: active authenticated PostgreSQL query cancellation; V4: unavailable startup deadline
- **Evidence kind**: automated test; establish every listed outcome and variant.
- **Existing file**: `internal/integration/foundation_test.go`

#### FND-004: Shared HTTP transport deadlines
- **Status**: NEW
- **Source**: Acceptance Scenario 6; FR-009; Edge Case 6
- **Behavior**: Shared HTTP transport keeps finite header/read/write/idle deadlines and header handling; transport-invalid requests follow transport behavior.
- **Required variants**: V1: real incomplete header deadline; V2: real stalled body deadline (HTTP-006); V3: blocked write deadline; V4: idle keepalive expiry; V5: over-limit/malformed HTTP header transport rejection without guaranteed challenge
- **Evidence kind**: automated test; establish every listed outcome and variant.

### Secrets and observability

#### SEC-001: Sanitized correlated outcomes
- **Status**: NEW
- **Source**: Acceptance Scenario 10; FR-001, FR-005, FR-008, FR-010
- **Behavior**: Diagnostics and all feature request outcomes expose no supplied secrets or sensitive failure details and retain sanitized structured correlation/outcomes.
- **Required variants**: V1: startup invalid credentials; V2: accepted landing; V3: missing/wrong/malformed/duplicate/oversize auth; V4: body rejection; V5: zero-byte read failure; V6: HEAD errors; V7: encoded and decoded distinctive secret sentinels; V8: configuration value/Authorization/body leak paths
- **Evidence kind**: automated test; establish every listed outcome and variant.

#### SEC-002: Runtime secret boundaries
- **Status**: NEW
- **Source**: Acceptance Scenario 10; FR-010
- **Behavior**: Runtime browser-delivered assets expose no privileged credentials, authentication sends no credentials to Stripe, and credential comparison avoids content-dependent early exit.
- **Required variants**: V1: runtime browser assets expose no privileged credentials; V2: no authentication Stripe calls or credential disclosure to Stripe; V3: comparisons avoid content-dependent early exit
- **Evidence kind**: source/configuration inspection; establish every listed outcome and variant.

### Browser journey

#### BRW-001: Native browser authentication journey
- **Status**: NEW
- **Source**: Acceptance Scenarios 7, 11; FR-011
- **Behavior**: Browser uses the native challenge to access the protected landing and no application sessions/login/logout are introduced.
- **Required variants**: V1: fresh browser challenge and credential success; V2: server rejects credential-free later request; V3: no session cookie/custom login/logout; V4: browser controls reuse/prompting
- **Evidence kind**: manual check plus automated HTTP evidence; establish every listed outcome and variant.

## Delivery Obligations

- **DO-001** — Source: FR-001, FR-002, FR-010; AGENTS.md configuration/security requirements. Deliverable: Empty BASIC_AUTH_USERNAME/PASSWORD placeholders and local/container credential setup. Completion check: Review .env.example for empty Basic credential placeholders, verify local secret files are ignored and untracked, and inspect tracked files/image-build inputs and image artifacts for credential exclusion. Review Compose wiring; resolve and launch the local application with synthetic credentials; verify values are supplied without diagnostic or browser disclosure. Never commit local credential values.
- **DO-002** — Source: FR-007, FR-011; Acceptance Scenarios 7, 11. Deliverable: Current service/browser usage and lifecycle documentation. Completion check: Run documented request with placeholders replaced by synthetic values or interactive entry; perform native-browser challenge smoke check; inspect restart and browser caching/logout instructions.
- **DO-003** — Source: Summary, Out of Scope; FR-003, FR-010. Deliverable: Local sandbox security and future-route/webhook boundaries. Completion check: Review docs for lack of Basic encryption, dedicated local credentials, local-only/TLS boundary, subsequent payment-route protection, and separate SDK webhook signature verification; do not claim undelivered routes exist.
- **DO-004** — Source: Acceptance Scenarios 1–12; Success Criteria; shared quality standards. Deliverable: Accepted feature and foundation verification evidence. Completion check: Run make verify and affected race checks using local HTTP and PostgreSQL infrastructure; report actual results and gaps without live Stripe dependencies.

## Gaps & Open Questions

No unresolved behavior. All 12 acceptance scenarios, FR-001–FR-011, edge cases, and success outcomes are represented. Supporting Context adds no behavior. Documentation/setup/CI tasks remain DO-001–DO-004.

Completeness witnesses: a sticky success flag violates AUTH-001; ignoring zero-byte read errors violates HTTP-004; trusting declared body length violates HTTP-003/HTTP-005; removing ReadTimeout violates HTTP-006; prefix-based public exceptions violate FND-001; unwired middleware violates CFG-001 and the connected landing journey. Byte bounds, normal EOF, read failure, real chunking, and stalled transport remain separate evidence.

Existing foundation assertions were inspected. FND-002/FND-003 need authenticated setup and additional boundary evidence, so neither is labeled EXISTS. Browser and source inspection evidence is planned, not completed.
