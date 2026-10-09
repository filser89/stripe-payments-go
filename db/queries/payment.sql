-- name: PaymentOrder :one
SELECT to_jsonb(o) AS data FROM payment_orders o WHERE id=$1;
-- name: LockPaymentOrder :one
SELECT to_jsonb(o) AS data FROM payment_orders o WHERE id=$1 FOR UPDATE;
-- name: PaymentView :one
SELECT to_jsonb(o) AS order_data,to_jsonb(p) AS operation_data,coalesce(p.lease_until<=clock_timestamp(),false)::boolean AS lease_expired FROM payment_orders o JOIN payment_operations p ON p.id=o.current_operation_id WHERE o.id=$1;
-- name: PaymentBinding :one
SELECT to_jsonb(b) AS binding_data,to_jsonb(o) AS order_data,to_jsonb(p) AS operation_data,coalesce(p.lease_until<=clock_timestamp(),false)::boolean AS lease_expired FROM payment_request_bindings b JOIN payment_orders o ON o.id=b.order_id JOIN payment_operations p ON p.id=b.operation_id WHERE b.request_key=$1;
-- name: LockPaymentOperations :many
SELECT to_jsonb(p) AS data, (lease_until IS NULL OR lease_until<=clock_timestamp())::boolean AS lease_expired FROM payment_operations p WHERE order_id=$1 ORDER BY id FOR UPDATE;
-- name: InsertPaymentOrder :exec
INSERT INTO payment_orders(id,description,amount,currency,payment_status,current_operation_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: InsertPaymentOperation :exec
INSERT INTO payment_operations(id,order_id,state,stripe_key,snapshot,version,owner_token,prior_ambiguity,evidence_source,investigation_required,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: InsertPaymentBinding :exec
INSERT INTO payment_request_bindings(request_key,method,target,order_id,operation_id,description,amount,currency,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9);
-- name: NextPaymentSequence :one
UPDATE payment_orders SET history_sequence=history_sequence+1 WHERE id=$1 RETURNING history_sequence;
-- name: InsertPaymentHistory :exec
INSERT INTO payment_history(order_id,sequence,kind,recorded_at,operation_id,from_state,to_state,stripe_session_id,stripe_payment_intent_id,stripe_event_id,stripe_request_id,failure_code,event_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13);
-- name: SetCurrentPaymentOperation :exec
UPDATE payment_orders SET current_operation_id=$2,updated_at=$3 WHERE id=$1;
-- name: ClaimPaymentDispatch :exec
UPDATE payment_operations SET state='unresolved',snapshot=$2,version=version+1,owner_token=$3,prior_ambiguity=true,first_dispatch_at=$4,last_dispatch_at=$5,dispatch_expires_at=$6,lease_until=clock_timestamp()+interval '12 seconds',updated_at=$5 WHERE id=$1;
-- name: ReleasePaymentClaim :exec
UPDATE payment_operations SET owner_token='',version=version+1,updated_at=clock_timestamp() WHERE id=$1;
-- name: SavePaymentObservation :exec
UPDATE payment_operations SET state=$2,stripe_session_id=$3,stripe_payment_intent_id=$4,checkout_url=$5,expires_at=$6,prior_ambiguity=$7,evidence_source=$8,failure_code=$9,investigation_required=$10,last_observed_at=$11,stripe_request_id=$12,owner_token='',version=version+1,updated_at=$11 WHERE id=$1;
-- name: PaymentHistory :many
SELECT to_jsonb(h) AS data FROM payment_history h WHERE order_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3;
-- name: CheckPaymentReady :one
SELECT (SELECT count(*) FROM payment_orders WHERE false)+(SELECT count(*) FROM payment_operations WHERE false)+(SELECT count(*) FROM payment_request_bindings WHERE false)+(SELECT count(*) FROM payment_history WHERE false) AS relation_count;
