-- Migration: 000047_analytics_range_indexes.up.sql
-- Block 6 proof: platform-wide analytics range scans need a leading
-- environment + created_at path (the per-app composite cannot serve
-- them). Verified by EXPLAIN on the 1M-row seed set.

CREATE INDEX IF NOT EXISTS payment_orders_env_created_idx
  ON app.payment_orders (environment, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_ledger_entries_created_idx
  ON app.payment_ledger_entries (created_at DESC);
