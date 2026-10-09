-- +goose Up
-- +goose StatementBegin
CREATE TABLE payment_orders (
 id uuid PRIMARY KEY, description text NOT NULL CHECK (char_length(description) BETWEEN 1 AND 200),
 amount bigint NOT NULL CHECK (amount BETWEEN 50 AND 100000), currency text NOT NULL CHECK(currency='usd'),
 payment_status text NOT NULL DEFAULT 'unpaid' CHECK(payment_status IN ('unpaid','paid')),
 current_operation_id uuid NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 history_sequence bigint NOT NULL DEFAULT 0 CHECK(history_sequence>=0)
);
CREATE TABLE payment_operations (
 id uuid PRIMARY KEY, order_id uuid NOT NULL REFERENCES payment_orders(id),
 state text NOT NULL CHECK(state IN ('prepared','unresolved','open','complete_unpaid','expired','rejected','paid')),
 stripe_key text NOT NULL UNIQUE CHECK(char_length(stripe_key) BETWEEN 1 AND 255), snapshot jsonb NOT NULL CHECK(octet_length(snapshot::text)<=16384),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), owner_token text NOT NULL DEFAULT '',
 prior_ambiguity boolean NOT NULL DEFAULT false, evidence_source text NOT NULL DEFAULT '', investigation_required boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 stripe_session_id text UNIQUE CHECK(stripe_session_id<>''), stripe_payment_intent_id text UNIQUE CHECK(stripe_payment_intent_id<>''),
 checkout_url text, expires_at timestamptz, first_dispatch_at timestamptz, last_dispatch_at timestamptz, dispatch_expires_at bigint,
 lease_until timestamptz, last_observed_at timestamptz, stripe_request_id text, failure_code text,
 UNIQUE(order_id,id),
 CHECK(snapshot->>'OrderID'=order_id::text AND snapshot->>'OperationID'=id::text AND snapshot->>'StripeKey'=stripe_key),
 CHECK((first_dispatch_at IS NULL AND snapshot->'FirstDispatchAt'='null'::jsonb AND snapshot->>'ExpiresAt'='0') OR
 (first_dispatch_at IS NOT NULL AND (snapshot->>'FirstDispatchAt')::timestamptz=first_dispatch_at AND (snapshot->>'ExpiresAt')::bigint=dispatch_expires_at AND dispatch_expires_at=floor(extract(epoch FROM first_dispatch_at))::bigint+86340)),
 CHECK((first_dispatch_at IS NULL AND last_dispatch_at IS NULL AND dispatch_expires_at IS NULL) OR
 (first_dispatch_at IS NOT NULL AND last_dispatch_at IS NOT NULL AND dispatch_expires_at IS NOT NULL AND last_dispatch_at>=first_dispatch_at))
);
ALTER TABLE payment_orders ADD CONSTRAINT payment_current_operation FOREIGN KEY(id,current_operation_id) REFERENCES payment_operations(order_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX payment_one_active ON payment_operations(order_id) WHERE state IN ('prepared','unresolved','open','complete_unpaid','paid');
CREATE INDEX payment_operations_order ON payment_operations(order_id,created_at,id);
CREATE TABLE payment_request_bindings (
 request_key uuid PRIMARY KEY CHECK(request_key::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 method text NOT NULL CHECK(method='POST'), target text NOT NULL,
 order_id uuid NOT NULL REFERENCES payment_orders(id), operation_id uuid NOT NULL,
 description text NOT NULL CHECK(char_length(description) BETWEEN 1 AND 200), amount bigint NOT NULL CHECK(amount BETWEEN 50 AND 100000), currency text NOT NULL CHECK(currency='usd'), created_at timestamptz NOT NULL,
 FOREIGN KEY(order_id,operation_id) REFERENCES payment_operations(order_id,id),
 CHECK(target='/api/orders' OR target='/api/orders/' || order_id::text || '/checkout')
);
CREATE INDEX payment_bindings_operation ON payment_request_bindings(order_id,operation_id);
CREATE TABLE payment_history (
 order_id uuid NOT NULL REFERENCES payment_orders(id), sequence bigint NOT NULL CHECK(sequence>0),
 kind text NOT NULL CHECK(kind IN ('order_created','operation_prepared','dispatch_started','operation_state_changed','payment_confirmed','recovery_recorded')),
 recorded_at timestamptz NOT NULL, operation_id uuid, from_state text, to_state text,
 stripe_session_id text, stripe_payment_intent_id text, stripe_event_id text, stripe_request_id text, failure_code text, event_at timestamptz,
 PRIMARY KEY(order_id,sequence), FOREIGN KEY(order_id,operation_id) REFERENCES payment_operations(order_id,id),
 CHECK(from_state IS NULL OR from_state IN ('prepared','unresolved','open','complete_unpaid','expired','rejected','paid')),
 CHECK(to_state IS NULL OR to_state IN ('prepared','unresolved','open','complete_unpaid','expired','rejected','paid'))
);
CREATE INDEX payment_history_operation ON payment_history(order_id,operation_id);
CREATE FUNCTION payment_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME IN ('payment_history','payment_request_bindings') THEN
  RAISE EXCEPTION 'append-only payment record';
 ELSIF TG_TABLE_NAME='payment_orders' THEN
  IF ROW(NEW.id,NEW.description,NEW.amount,NEW.currency,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.description,OLD.amount,OLD.currency,OLD.created_at) OR (OLD.payment_status='paid' AND NEW.payment_status<>'paid') THEN
   RAISE EXCEPTION 'immutable purchase or confirmed payment';
  END IF;
 ELSE
  IF (OLD.state='paid' AND NEW.state<>'paid') OR (OLD.last_dispatch_at IS NOT NULL AND NEW.last_dispatch_at<OLD.last_dispatch_at) OR ROW(NEW.id,NEW.order_id,NEW.stripe_key,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.order_id,OLD.stripe_key,OLD.created_at) OR
  (NEW.snapshot - 'FirstDispatchAt' - 'ExpiresAt') IS DISTINCT FROM (OLD.snapshot - 'FirstDispatchAt' - 'ExpiresAt') OR
  (OLD.first_dispatch_at IS NOT NULL AND ROW(NEW.first_dispatch_at,NEW.dispatch_expires_at,NEW.snapshot->'FirstDispatchAt',NEW.snapshot->'ExpiresAt') IS DISTINCT FROM ROW(OLD.first_dispatch_at,OLD.dispatch_expires_at,OLD.snapshot->'FirstDispatchAt',OLD.snapshot->'ExpiresAt')) THEN
   RAISE EXCEPTION 'immutable checkout snapshot';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_order_immutable BEFORE UPDATE ON payment_orders FOR EACH ROW EXECUTE FUNCTION payment_immutable();
CREATE TRIGGER payment_operation_immutable BEFORE UPDATE ON payment_operations FOR EACH ROW EXECUTE FUNCTION payment_immutable();
CREATE TRIGGER payment_binding_immutable BEFORE UPDATE OR DELETE ON payment_request_bindings FOR EACH ROW EXECUTE FUNCTION payment_immutable();
CREATE TRIGGER payment_history_immutable BEFORE UPDATE OR DELETE ON payment_history FOR EACH ROW EXECUTE FUNCTION payment_immutable();
-- +goose StatementEnd

-- +goose Down
ALTER TABLE payment_orders DROP CONSTRAINT payment_current_operation;
DROP TABLE payment_history, payment_request_bindings, payment_operations, payment_orders;
DROP FUNCTION payment_immutable();
