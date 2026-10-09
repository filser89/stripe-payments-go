# Order and Checkout API

All endpoints require the configured HTTP Basic account. Authentication precedes routing and input reads. Exact GET/HEAD `/healthz` and `/readyz` are public. The protected browser root is a static landing page.

| Method | Resource | Input |
| --- | --- | --- |
| POST | `/api/orders` | JSON `description`, `amount`, `request_key` |
| POST | `/api/orders/{order_id}/checkout` | JSON `request_key` |
| GET, HEAD | `/api/orders/{order_id}` | Empty actual body, no query |
| GET, HEAD | `/api/orders/{order_id}/history` | Empty actual body; optional `after`, `limit` |

IDs/keys are lowercase canonical UUIDv4 strings with RFC variant bits. Generate a random key for an intentionally new request; preserve the key and exact purchase until its outcome is known. One global key permanently binds its method/target/purchase/order/operation. Equal initial replays resolve the original operation even when another becomes current; changed inputs/targets return 409. Backend order/operation/Stripe keys are independent random identities.

Description is valid UTF-8, 1–200 Unicode code points, without controls or leading/trailing Unicode whitespace. Interior case/spacing/normalization is preserved. Amount is a digits-only JSON integer token in minor units within 50–100000 and the configured narrower new-order limits. Currency is `usd`; callers supply no currency/quantity. Accepted bindings are compared before current narrower limits. JSON contains exactly the required fields, with no duplicates/nulls/unknowns/extra values/trailing junk. POST accepts application/json with optional UTF-8 charset and no compression. Actual streams are limited to 4096 bytes regardless of framing/Content-Length. All queries are rejected except history fields, each once: after is a nonnegative signed-64 decimal cursor; limit is 1–100, default 50. History is ascending sequence with entries [] for an empty page and next_after equal to the last returned sequence, or the supplied after cursor for an empty page.

```sh
BASE=http://localhost:8080
ACCOUNT=local-user
# Generate once; retain key and purchase together. curl prompts for password.
KEY=$(uuidgen | tr '[:upper:]' '[:lower:]')
curl --user "$ACCOUNT" -i -H 'Content-Type: application/json' "$BASE/api/orders" \
 --data "{\"description\":\"One product\",\"amount\":2500,\"request_key\":\"$KEY\"}"
ORDER_ID=replace-with-returned-order-id
curl --user "$ACCOUNT" -i "$BASE/api/orders/$ORDER_ID"
curl --user "$ACCOUNT" -i "$BASE/api/orders/$ORDER_ID/history?after=0&limit=50"
CONTINUE_KEY=$(uuidgen | tr '[:upper:]' '[:lower:]')
curl --user "$ACCOUNT" -i -H 'Content-Type: application/json' \
 "$BASE/api/orders/$ORDER_ID/checkout" --data "{\"request_key\":\"$CONTINUE_KEY\"}"
```

201 means a newly accepted operation with an established result; 200 an established replay/refresh. 202 means accepted prepared/unresolved work, with Location pointing to the order and Retry-After: 1. Retain keys/IDs and inspect before retrying the same operation. Reads make no Stripe calls. Checkout creation never confirms payment.

Responses contain order, operation, can_resume, can_retry_same_operation and can_start_new_attempt. Order exposes id/description/amount/currency/payment_status/created_at/updated_at. Operation exposes id/state/stripe_session_id/stripe_payment_intent_id/checkout_url/first_dispatch_at/expires_at/created_at/updated_at/failure_code/investigation_required. Optional values are explicit nulls; times are UTC RFC3339Nano. Flags are advisory and rechecked on POST; checkout_url is exposed only when resuming is safe. Owner tokens, Stripe/request keys, snapshots and raw diagnostics are private. History exposes sequence/kind/recorded_at/order_id/optional operation_id/from_state/to_state/Stripe association-event-request IDs/failure_code; no URL, description or raw payload.

Errors use {"error":{"code":"...","message":"..."}} plus known order_id/operation_id. invalid_request=400; body_too_large=413; unsupported_media_type=415; not_found=404; method_not_allowed=405 with Allow; idempotency_conflict/checkout_blocked=409; checkout_rejected=502; temporarily_unavailable=503. Authentication uses a generic 401 Basic challenge. Fresh 409 leaves the key unused; accepted 502 preserves its binding; 503 may retain known identity after result/readback failure. Inspect that identity instead of creating another order. JSON responses use application/json; charset=utf-8, no-store and generated X-Request-ID. HEAD executes the same read/error behavior and headers as GET with no body.
