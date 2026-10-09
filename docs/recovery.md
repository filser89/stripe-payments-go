# Checkout recovery

Orders retain immutable purchases and a current durable operation. Permanent request bindings refer to their original operations. Each operation has an independent Stripe key and immutable request snapshot fixing purchase, currency, URLs, SDK/API version, supported options and first requested expiry before send. Each possible POST records latest dispatch before network work; short database transactions end before external calls.

| State | Action |
| --- | --- |
| prepared | Continue the accepted operation. |
| unresolved | Retry the same compatible operation within safe age, or retrieve its known session. |
| open | Refresh known session, then resume only verified open/unpaid with future expiry. |
| complete_unpaid | Inspect/refresh; completion or decline cannot authorize another charge. |
| expired | Fresh operation requires verified expired/unpaid evidence and all-prior guards. |
| rejected | Confirmed first validation/credential/permission rejection without prior ambiguity permits a fresh attempt. |
| paid | No Checkout action. Trusted confirmation belongs to a separate feature. |

Initial retries preserve exact key/description/amount. Continuation retries preserve their key and order. A fresh key cannot bypass paid/current/all-prior guards. A blocked fresh request returns 409 without consuming its key. Once binding commits, transient failures retain accepted identity and return readable 202; confirmed rejection returns 502 and replays that result. Result/read failures return 503 with known identity where available.

One combined attempt/elapsed budget covers GET+POST. SDK retries are disabled. Retryable transport/rate-limit/transient-conflict/server failures wait at least 250ms then 500ms, or longer valid Retry-After. Stripe-Should-Retry false suppresses retry. Calls/waits/DB/response I/O honor effective cancellation/deadlines. A successful eligibility GET may exhaust budget while a new prepared operation commits, returning 202 for later same-key dispatch.

Missing-ID POST is safe strictly before 23h from first possible dispatch. At/after that boundary, missing local results cannot justify recreation. Known IDs use GET even beyond this age. Immutable requested expiry is first dispatch plus 23h59m; observed remote expiry is separate. Fifteen minutes unresolved is an investigation advisory, not expiry/retry cutoff. Server errors, mismatched evidence, unsupported snapshots and confirmed configuration/integration rejection also require investigation.

Timeout, cancellation, interruption, response loss and result-commit failure preserve ambiguity. Later rejection cannot erase earlier possible execution. A fixed 12-second database lease fences mutation ownership; bounded matching coordination cleanup may release sooner. Lease expiry means ownership availability, not nonpayment. Version/token/current/paid guards suppress stale results. Business changes and append-only sequence history commit atomically.

Inspect authenticated status/history and sanitized correlated logs. Preserve IDs/keys in operator records. Fix sandbox configuration and restart before retrying accepted work. There is no force-paid/reset/delete/recreate/override API. Browser returns, abandonment, declines, session creation and complete/paid Checkout evidence cannot confirm order payment here. External dashboard refunds/overrides are unsupported. Signed webhook confirmation and bounded listing/manual reconciliation workers are separate feature contracts.
