-- Migration: 000042_endpoint_secret_version.up.sql
-- Versioned webhook signing secrets so merchants can rotate a leaked
-- secret. Derivation is HMAC(delivery_secret, "azsubay-payment-webhook:"
-- + endpoint_id) for version 1 (unchanged — existing stored secrets keep
-- working) and includes ":v<N>" for versions >= 2. Rotation bumps the
-- version atomically; deliveries always sign with the version joined at
-- claim time, so a rotation takes effect on the next attempt.

ALTER TABLE app.payment_webhook_endpoints
  ADD COLUMN IF NOT EXISTS secret_version INTEGER NOT NULL DEFAULT 1;
