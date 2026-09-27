-- Migration: 000043_org_live_limits.down.sql

ALTER TABLE app.organizations
  DROP COLUMN IF EXISTS live_max_txn_amount,
  DROP COLUMN IF EXISTS live_daily_volume_cap;
