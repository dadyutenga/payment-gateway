-- Migration: 000048_order_first_paid_at.up.sql
-- Block 6 proof: time-to-pay percentiles joined payment_status_history
-- per order (too slow at 1M rows). first_paid_at is stamped once, in the
-- same locked transaction that flips an order to paid, so it can never
-- disagree with the ledger credit. Rollups and dashboards read it
-- directly; history stays the audit trail.

ALTER TABLE app.payment_orders
  ADD COLUMN IF NOT EXISTS first_paid_at TIMESTAMPTZ;

-- Backfill from the earliest paid transition (one-off; new rows stamp it
-- in ApplyWebhookEvent).
UPDATE app.payment_orders o
SET first_paid_at = sub.first_paid
FROM (
  SELECT payment_order_id, MIN(created_at) AS first_paid
  FROM app.payment_status_history
  WHERE to_status = 'paid'
  GROUP BY payment_order_id
) sub
WHERE o.id = sub.payment_order_id AND o.first_paid_at IS NULL;
