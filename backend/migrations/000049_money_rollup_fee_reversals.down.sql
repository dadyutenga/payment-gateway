-- Migration: 000049_money_rollup_fee_reversals.down.sql

ALTER TABLE app.analytics_daily_app_money
  DROP COLUMN IF EXISTS fee_reversals;
