-- Migration: 000032_order_expiry.down.sql

UPDATE app.payment_webhook_endpoints
SET event_types = array_remove(event_types, 'payment.expired'),
    updated_at = NOW()
WHERE 'payment.expired' = ANY(event_types);

DROP INDEX IF EXISTS app.payment_orders_expiry_idx;

ALTER TABLE app.payment_orders
  DROP COLUMN IF EXISTS expires_at;
