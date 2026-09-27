-- Migration: 000043_org_live_limits.up.sql
-- Per-organization live guardrails. NULL means "platform default"
-- (PAYMENTS_LIVE_MAX_TXN_AMOUNT / PAYMENTS_LIVE_DAILY_VOLUME_CAP):
-- tighter for risky orgs, roomier for trusted volume. Plain TEXT holding
-- positive decimals, validated on write by the admin endpoint — the
-- order path falls back to the platform default on empty/unparsable.

ALTER TABLE app.organizations
  ADD COLUMN IF NOT EXISTS live_max_txn_amount TEXT,
  ADD COLUMN IF NOT EXISTS live_daily_volume_cap TEXT;
