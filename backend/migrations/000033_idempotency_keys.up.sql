-- Migration: 000033_idempotency_keys.up.sql
-- Idempotency-Key support for POST /orders and POST /withdrawals. A claim
-- row is inserted BEFORE executing (NULL response = in progress, so a
-- concurrent duplicate waits briefly instead of re-executing), then filled
-- with the terminal response. Same key + same request hash replays the
-- stored response; same key + different hash is a 409. Rows expire after
-- 24h and are swept by the expiry worker.

CREATE TABLE IF NOT EXISTS app.idempotency_keys (
  key TEXT NOT NULL,
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  endpoint TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_status INTEGER,
  response_body TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',
  PRIMARY KEY (app_id, key, endpoint)
);

CREATE INDEX IF NOT EXISTS idempotency_keys_expiry_idx
  ON app.idempotency_keys (expires_at);
