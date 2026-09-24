-- Migration: 000031_payment_refunds.up.sql
-- Provider-integrated refunds. payment_refunds records every refund attempt
-- (provider-confirmed or local manual fallback) so partial refunds and
-- retries are accounted per order. The (order, provider_refund_id) unique
-- index makes async provider confirmations idempotent: replaying the same
-- provider refund is a no-op instead of a second reversal.
--
-- Partial refunds need several refund_debit / refund_fee_reversal_credit
-- rows per order, which the per-order unique indexes from 000029 forbid —
-- so those two indexes are dropped here. Double-reversal protection moves
-- to: order row lock + remaining-amount check + the unique index below
-- (payment_credit / platform_fee_debit stay one-per-order, untouched).
--
-- Existing webhook endpoints are subscribed to the new payment.refunded
-- event type so refund notifications are actually delivered.

CREATE TABLE IF NOT EXISTS app.payment_refunds (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_order_id UUID NOT NULL REFERENCES app.payment_orders(id) ON DELETE CASCADE,
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  provider_refund_id TEXT,
  amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
  currency TEXT NOT NULL DEFAULT 'TZS',
  status TEXT NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'processing', 'failed')),
  reason TEXT NOT NULL DEFAULT '',
  requested_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payment_refunds_order_idx
  ON app.payment_refunds (payment_order_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS payment_refunds_provider_refund_idx
  ON app.payment_refunds (payment_order_id, provider_refund_id)
  WHERE provider_refund_id IS NOT NULL AND provider_refund_id <> '';

DROP INDEX IF EXISTS app.payment_ledger_refund_debit_once;
DROP INDEX IF EXISTS app.payment_ledger_refund_fee_reversal_once;

UPDATE app.payment_webhook_endpoints
SET event_types = array_append(event_types, 'payment.refunded'),
    updated_at = NOW()
WHERE NOT ('payment.refunded' = ANY(event_types));
