-- Migration: 000045_org_settings.up.sql
-- Merchant Settings page backing: extended org profile, KYC attempt
-- history (the live kyc_submissions row stays the enforced source of
-- truth), and per-org notification preferences.
--
-- No existing rows are modified; all new columns are nullable/defaulted.

ALTER TABLE app.organizations
  ADD COLUMN IF NOT EXISTS address TEXT,
  ADD COLUMN IF NOT EXISTS phone TEXT,
  ADD COLUMN IF NOT EXISTS contact_email TEXT,
  ADD COLUMN IF NOT EXISTS logo_url TEXT,
  ADD COLUMN IF NOT EXISTS primary_color TEXT;

-- Every KYC submit AND every admin decision appends an attempt row, so the
-- Settings verification tab can show full history with dates and reasons.
CREATE TABLE IF NOT EXISTS app.kyc_submission_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL REFERENCES app.organizations(id) ON DELETE CASCADE,
  business_name TEXT NOT NULL DEFAULT '',
  tin TEXT NOT NULL DEFAULT '',
  id_document_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'submitted'
    CHECK (status IN ('submitted', 'verified', 'rejected')),
  rejection_reason TEXT NOT NULL DEFAULT '',
  reviewed_by TEXT,
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS kyc_attempts_org_idx
  ON app.kyc_submission_attempts (org_id, created_at DESC);

-- Per-org notification toggles. Absent row == all enabled.
CREATE TABLE IF NOT EXISTS app.notification_prefs (
  org_id UUID PRIMARY KEY REFERENCES app.organizations(id) ON DELETE CASCADE,
  payment_updated BOOLEAN NOT NULL DEFAULT TRUE,
  payment_refunded BOOLEAN NOT NULL DEFAULT TRUE,
  payment_expired BOOLEAN NOT NULL DEFAULT TRUE,
  withdrawal_updates BOOLEAN NOT NULL DEFAULT TRUE,
  kyc_decisions BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
