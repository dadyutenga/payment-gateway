-- Migration: 000050_ttp_covering_index.up.sql
-- Block 6 proof: TTP percentiles scan paid orders in range. Covering
-- index avoids heap fetches for the ~350k-row sort at 1M scale.

CREATE INDEX IF NOT EXISTS payment_orders_paid_ttp_idx
  ON app.payment_orders (environment, created_at DESC)
  INCLUDE (first_paid_at)
  WHERE status = 'paid';
