-- Migration: 000034_merchant_api_keys.down.sql

DROP INDEX IF EXISTS app.payment_api_keys_status_idx;

ALTER TABLE app.payment_api_keys
  DROP COLUMN IF EXISTS environment,
  DROP COLUMN IF EXISTS prefix,
  DROP COLUMN IF EXISTS expires_at,
  DROP COLUMN IF EXISTS status;
