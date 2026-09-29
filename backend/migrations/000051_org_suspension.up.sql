-- Migration: 000051_org_suspension.up.sql
-- Tenant kill-switch: suspended orgs cannot move live money (orders,
-- live keys, withdrawals) until unsuspended. Members keep read access so
-- they see the notice. No backfill needed (default false).

ALTER TABLE app.organizations
  ADD COLUMN IF NOT EXISTS suspended BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS suspended_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ;
