-- Migration: 000046_analytics_foundation.down.sql

DROP TABLE IF EXISTS app.analytics_hourly_app;
DROP TABLE IF EXISTS app.analytics_daily_app_money;
DROP TABLE IF EXISTS app.analytics_daily_app;
DROP INDEX IF EXISTS app.payment_ledger_entries_analytics_idx;
DROP INDEX IF EXISTS app.payment_orders_open_status_idx;
DROP INDEX IF EXISTS app.payment_orders_channel_idx;
DROP INDEX IF EXISTS app.payment_orders_payer_hash_idx;
DROP INDEX IF EXISTS app.payment_orders_provider_status_created_idx;
DROP INDEX IF EXISTS app.payment_orders_analytics_idx;
ALTER TABLE app.payment_orders
  DROP CONSTRAINT IF EXISTS payment_orders_channel_check;
ALTER TABLE app.payment_orders
  DROP CONSTRAINT IF EXISTS payment_orders_failure_code_check;
ALTER TABLE app.payment_orders
  DROP COLUMN IF EXISTS provider_latency_ms,
  DROP COLUMN IF EXISTS payer_hash,
  DROP COLUMN IF EXISTS channel,
  DROP COLUMN IF EXISTS failure_message,
  DROP COLUMN IF EXISTS failure_code;
