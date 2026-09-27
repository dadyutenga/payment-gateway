-- Migration: 000038_auth_verification.up.sql
-- Signup verification: per-user verification timestamps plus the OTP
-- code table. Raw codes are NEVER stored — only bcrypt hashes. Attempts
-- count wrong tries per code (lockout), consumed_at marks use.

ALTER TABLE app.users
  ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS phone_verified_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS app.otp_codes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES app.users(id) ON DELETE CASCADE,
  channel TEXT NOT NULL CHECK (channel IN ('email', 'sms')),
  code_hash TEXT NOT NULL,
  purpose TEXT NOT NULL CHECK (purpose IN ('email_verify', 'phone_verify', 'login_2fa')),
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  attempts INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS otp_codes_user_idx
  ON app.otp_codes (user_id, purpose, created_at DESC);
