-- Migration: 000049_money_rollup_fee_reversals.up.sql
-- Block 6: revenue needs fee reversals per (day, org, app, currency).
-- Rebuild affected days after applying:
--   go run ./cmd/backfill-analytics -from YYYY-MM-DD -to YYYY-MM-DD

ALTER TABLE app.analytics_daily_app_money
  ADD COLUMN IF NOT EXISTS fee_reversals NUMERIC(18, 2) NOT NULL DEFAULT 0;
