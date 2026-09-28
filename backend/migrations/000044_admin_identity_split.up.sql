-- Migration: 000044_admin_identity_split.up.sql
-- Phase 1 Option A: separate admin identity table.
--
-- app.users becomes the CUSTOMER table (all existing FKs — otp_codes,
-- org_members lookups, KYC reviewed_by email — keep working untouched).
-- Platform operators live in app.admin_users with their own password
-- hashes. Existing is_admin=true rows are copied over, then cleared on
-- app.users so the customer table can never confer admin again.
--
-- Also creates app.audit_log for admin mutations + failed admin logins.
--
-- BACK UP app.users BEFORE APPLYING (pg_dump at minimum).

CREATE TABLE IF NOT EXISTS app.admin_users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  -- TOTP hook (Phase 1 only reserves the column; verification lands later).
  totp_secret TEXT NOT NULL DEFAULT '',
  failed_attempts INTEGER NOT NULL DEFAULT 0,
  locked_until TIMESTAMPTZ,
  last_login_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS admin_users_email_lower_idx ON app.admin_users (lower(email));

-- Copy existing operators (seed-admin promotions + bootstrap admins).
INSERT INTO app.admin_users (id, email, password_hash, created_at, updated_at)
SELECT id, email, password_hash, created_at, updated_at
FROM app.users
WHERE is_admin = true
ON CONFLICT (id) DO NOTHING;

-- app.users is now customers-only: no row may claim admin.
UPDATE app.users SET is_admin = false;

CREATE TABLE IF NOT EXISTS app.audit_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_id TEXT NOT NULL DEFAULT '',
  actor_email TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  target_type TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  before_data JSONB,
  after_data JSONB,
  ip TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS audit_log_actor_idx ON app.audit_log (actor_email, created_at DESC);
CREATE INDEX IF NOT EXISTS audit_log_action_idx ON app.audit_log (action, created_at DESC);
