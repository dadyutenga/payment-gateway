-- Migration: 000034_merchant_api_keys.up.sql
-- Merchant self-service key management: keys gain a lifecycle status
-- (active/rotating/revoked) with a grace-period expiry so rotation never
-- instantly kills in-flight traffic, a display prefix (raw secrets are
-- shown once at creation and never stored), and an environment tag
-- (live/sandbox — enforced by the sandbox feature, stored here).
-- Existing rows: revoked_at-set become revoked, the rest active.

ALTER TABLE app.payment_api_keys
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'rotating', 'revoked')),
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS prefix TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'live'
    CHECK (environment IN ('live', 'sandbox'));

UPDATE app.payment_api_keys
SET status = CASE WHEN revoked_at IS NOT NULL THEN 'revoked' ELSE 'active' END,
    prefix = CASE WHEN prefix = '' THEN 'legacy' ELSE prefix END;

CREATE INDEX IF NOT EXISTS payment_api_keys_status_idx
  ON app.payment_api_keys (app_id, status);
