-- Migration: 000008_payments.up.sql
-- Central payment gateway tables for provider webhooks, payment ledger, and app fan-out.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE SCHEMA IF NOT EXISTS app;

CREATE TABLE IF NOT EXISTS app.payment_apps (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT UNIQUE NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_payment_apps_updated_at
  BEFORE UPDATE ON app.payment_apps
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.payment_api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  key_hash TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS payment_api_keys_app_idx ON app.payment_api_keys (app_id);

CREATE TABLE IF NOT EXISTS app.payment_provider_accounts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider TEXT NOT NULL,
  name TEXT NOT NULL,
  environment TEXT NOT NULL DEFAULT 'production',
  base_url TEXT,
  is_default BOOLEAN NOT NULL DEFAULT false,
  status TEXT NOT NULL DEFAULT 'active',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(provider, name, environment)
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_provider_default_idx
  ON app.payment_provider_accounts (provider, environment)
  WHERE is_default = true;

CREATE TRIGGER update_payment_provider_accounts_updated_at
  BEFORE UPDATE ON app.payment_provider_accounts
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.payment_orders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID REFERENCES app.payment_apps(id) ON DELETE SET NULL,
  provider TEXT NOT NULL,
  provider_order_id TEXT,
  provider_transaction_id TEXT,
  external_reference TEXT,
  amount NUMERIC(18, 2) NOT NULL,
  currency TEXT NOT NULL DEFAULT 'TZS',
  buyer_name TEXT,
  buyer_email TEXT,
  buyer_phone TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  provider_status TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_provider_order_idx
  ON app.payment_orders (provider, provider_order_id)
  WHERE provider_order_id IS NOT NULL AND provider_order_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS payment_orders_app_external_reference_idx
  ON app.payment_orders (app_id, external_reference)
  WHERE app_id IS NOT NULL AND external_reference IS NOT NULL AND external_reference <> '';

CREATE INDEX IF NOT EXISTS payment_orders_status_updated_idx
  ON app.payment_orders (status, updated_at);

CREATE INDEX IF NOT EXISTS payment_orders_buyer_phone_idx
  ON app.payment_orders (buyer_phone);

CREATE TRIGGER update_payment_orders_updated_at
  BEFORE UPDATE ON app.payment_orders
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.payment_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider TEXT NOT NULL,
  event_type TEXT NOT NULL DEFAULT 'payment.updated',
  provider_event_id TEXT,
  provider_order_id TEXT,
  provider_transaction_id TEXT,
  payment_order_id UUID REFERENCES app.payment_orders(id) ON DELETE SET NULL,
  signature_valid BOOLEAN NOT NULL DEFAULT false,
  payload_hash TEXT NOT NULL,
  dedupe_key TEXT NOT NULL UNIQUE,
  headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  raw_body TEXT NOT NULL,
  normalized_status TEXT,
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at TIMESTAMPTZ,
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  duplicate_count INTEGER NOT NULL DEFAULT 0,
  error TEXT
);

CREATE INDEX IF NOT EXISTS payment_events_provider_received_idx
  ON app.payment_events (provider, received_at DESC);

CREATE INDEX IF NOT EXISTS payment_events_provider_order_idx
  ON app.payment_events (provider, provider_order_id);

CREATE INDEX IF NOT EXISTS payment_events_payment_order_idx
  ON app.payment_events (payment_order_id);

CREATE INDEX IF NOT EXISTS payment_events_processed_idx
  ON app.payment_events (processed_at)
  WHERE processed_at IS NULL;

CREATE TABLE IF NOT EXISTS app.payment_status_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  payment_order_id UUID NOT NULL REFERENCES app.payment_orders(id) ON DELETE CASCADE,
  from_status TEXT,
  to_status TEXT NOT NULL,
  provider_status TEXT,
  source TEXT NOT NULL,
  event_id UUID REFERENCES app.payment_events(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payment_status_history_order_idx
  ON app.payment_status_history (payment_order_id, created_at DESC);

CREATE TABLE IF NOT EXISTS app.payment_webhook_endpoints (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  event_types TEXT[] NOT NULL DEFAULT ARRAY['payment.updated'],
  secret_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payment_webhook_endpoints_app_idx
  ON app.payment_webhook_endpoints (app_id);

CREATE TRIGGER update_payment_webhook_endpoints_updated_at
  BEFORE UPDATE ON app.payment_webhook_endpoints
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.payment_webhook_deliveries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id UUID NOT NULL REFERENCES app.payment_events(id) ON DELETE CASCADE,
  endpoint_id UUID NOT NULL REFERENCES app.payment_webhook_endpoints(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'pending',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_attempt_at TIMESTAMPTZ,
  last_response_status INTEGER,
  last_error TEXT,
  delivered_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(event_id, endpoint_id)
);

CREATE INDEX IF NOT EXISTS payment_webhook_deliveries_next_attempt_idx
  ON app.payment_webhook_deliveries (status, next_attempt_at);

CREATE INDEX IF NOT EXISTS payment_webhook_deliveries_event_idx
  ON app.payment_webhook_deliveries (event_id);

