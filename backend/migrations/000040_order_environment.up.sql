-- Migration: 000040_order_environment.up.sql
-- Order environment (live/sandbox) for KYC gating and sandbox isolation.
-- Backfilled live: every historical order settled real money.

ALTER TABLE app.payment_orders
  ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'live'
    CHECK (environment IN ('live', 'sandbox'));

CREATE INDEX IF NOT EXISTS payment_orders_env_status_idx
  ON app.payment_orders (environment, status);
