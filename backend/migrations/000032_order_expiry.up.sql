-- Migration: 000032_order_expiry.up.sql
-- Order TTL: pending orders carry expires_at (set at creation from
-- PAYMENTS_ORDER_TTL, default 30m). The expiry worker transitions
-- expired pending rows to 'expired' without touching the ledger.
-- Existing webhook endpoints are subscribed to the new payment.expired
-- event type so expiry notifications are actually delivered.

ALTER TABLE app.payment_orders
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

UPDATE app.payment_orders
SET expires_at = created_at + INTERVAL '30 minutes'
WHERE expires_at IS NULL AND status = 'pending';

CREATE INDEX IF NOT EXISTS payment_orders_expiry_idx
  ON app.payment_orders (status, expires_at)
  WHERE status = 'pending';

UPDATE app.payment_webhook_endpoints
SET event_types = array_append(event_types, 'payment.expired'),
    updated_at = NOW()
WHERE NOT ('payment.expired' = ANY(event_types));
