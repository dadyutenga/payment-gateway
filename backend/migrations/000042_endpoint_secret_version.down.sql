-- Migration: 000042_endpoint_secret_version.down.sql

ALTER TABLE app.payment_webhook_endpoints
  DROP COLUMN IF EXISTS secret_version;
