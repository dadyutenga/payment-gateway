-- Migration: 000046_analytics_foundation.up.sql
-- Phase 2 (Analytics) data foundation.
--
-- New order columns (all nullable/backfilled empty — analytics-only, never
-- on the money path):
--   failure_code    normalized enum: expired|cancelled|provider_declined|
--                   timeout|system_error (set on final transitions only)
--   failure_message provider status/error text, truncated at write time
--   channel         normalized payer network: M-PESA|AIRTEL|TIGO|HALOPESA|OTHER
--   payer_hash      HMAC-SHA256 of the normalized payer phone (hex). Raw
--                   numbers NEVER enter analytics tables/exports.
--   provider_latency_ms round-trip ms of the last provider API call
-- payer_hash backfill runs in Go (cmd/backfill-analytics) since the HMAC
-- key must never appear in SQL history. Rows predating it keep NULL and
-- are excluded from repeat-customer metrics (documented in ANALYTICS.md).
--
-- Rollup tables are a rebuildable cache (see docs/ANALYTICS.md).

ALTER TABLE app.payment_orders
  ADD COLUMN IF NOT EXISTS failure_code TEXT,
  ADD COLUMN IF NOT EXISTS failure_message TEXT,
  ADD COLUMN IF NOT EXISTS channel TEXT,
  ADD COLUMN IF NOT EXISTS payer_hash TEXT,
  ADD COLUMN IF NOT EXISTS provider_latency_ms BIGINT;

ALTER TABLE app.payment_orders
  DROP CONSTRAINT IF EXISTS payment_orders_failure_code_check;
ALTER TABLE app.payment_orders
  ADD CONSTRAINT payment_orders_failure_code_check
  CHECK (failure_code IS NULL OR failure_code IN
    ('expired', 'cancelled', 'provider_declined', 'timeout', 'system_error'));

ALTER TABLE app.payment_orders
  DROP CONSTRAINT IF EXISTS payment_orders_channel_check;
ALTER TABLE app.payment_orders
  ADD CONSTRAINT payment_orders_channel_check
  CHECK (channel IS NULL OR channel IN
    ('M-PESA', 'AIRTEL', 'TIGO', 'HALOPESA', 'OTHER'));

-- Analytics hot paths (EAT-day bucketing reads created_at ranges).
CREATE INDEX IF NOT EXISTS payment_orders_analytics_idx
  ON app.payment_orders (app_id, environment, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_orders_provider_status_created_idx
  ON app.payment_orders (provider, status, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_orders_payer_hash_idx
  ON app.payment_orders (payer_hash, created_at DESC)
  WHERE payer_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS payment_orders_channel_idx
  ON app.payment_orders (channel, created_at DESC)
  WHERE channel IS NOT NULL;

-- Partial index for ops queries (stuck orders, reconciliation).
CREATE INDEX IF NOT EXISTS payment_orders_open_status_idx
  ON app.payment_orders (status, updated_at)
  WHERE status IN ('pending', 'processing');

-- Org-scoped ledger access: ledger rows carry no org_id, so the join
-- through apps needs (app_id, entry_type, created_at).
CREATE INDEX IF NOT EXISTS payment_ledger_entries_analytics_idx
  ON app.payment_ledger_entries (app_id, entry_type, created_at DESC);

CREATE TABLE IF NOT EXISTS app.analytics_daily_app (
  day          DATE NOT NULL,
  org_id       UUID NOT NULL,
  app_id       UUID NOT NULL,
  environment  TEXT NOT NULL,
  provider     TEXT NOT NULL,
  channel      TEXT NOT NULL,
  currency     TEXT NOT NULL,
  created      INTEGER NOT NULL DEFAULT 0,
  succeeded    INTEGER NOT NULL DEFAULT 0,
  failed       INTEGER NOT NULL DEFAULT 0,
  expired      INTEGER NOT NULL DEFAULT 0,
  gross        NUMERIC(18, 2) NOT NULL DEFAULT 0,
  p50_ttp_s    DOUBLE PRECISION,
  p90_ttp_s    DOUBLE PRECISION,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (day, org_id, app_id, environment, provider, channel, currency)
);
CREATE INDEX IF NOT EXISTS analytics_daily_app_org_day_idx
  ON app.analytics_daily_app (org_id, day DESC);

-- Money grain is coarser (ledger rows carry no provider/channel/
-- environment): per app+currency fee and refund totals. Kept separate so
-- joining can never double-count fees across provider/channel rows.
CREATE TABLE IF NOT EXISTS app.analytics_daily_app_money (
  day          DATE NOT NULL,
  org_id       UUID NOT NULL,
  app_id       UUID NOT NULL,
  currency     TEXT NOT NULL,
  fees         NUMERIC(18, 2) NOT NULL DEFAULT 0,
  refunds      INTEGER NOT NULL DEFAULT 0,
  refund_total NUMERIC(18, 2) NOT NULL DEFAULT 0,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (day, org_id, app_id, currency)
);

CREATE TABLE IF NOT EXISTS app.analytics_hourly_app (
  hour        TIMESTAMPTZ NOT NULL,
  org_id      UUID NOT NULL,
  app_id      UUID NOT NULL,
  environment TEXT NOT NULL,
  created     INTEGER NOT NULL DEFAULT 0,
  succeeded   INTEGER NOT NULL DEFAULT 0,
  failed      INTEGER NOT NULL DEFAULT 0,
  expired     INTEGER NOT NULL DEFAULT 0,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (hour, org_id, app_id, environment)
);
CREATE INDEX IF NOT EXISTS analytics_hourly_app_org_hour_idx
  ON app.analytics_hourly_app (org_id, hour DESC);
