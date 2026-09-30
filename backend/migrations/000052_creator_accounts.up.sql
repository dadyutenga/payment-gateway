-- Migration: 000052_creator_accounts.up.sql
-- Creator / influencer track (Part 1: account kind).
-- organizations.account_kind is set at signup and immutable after
-- (changing kind is support-assisted, not self-service).
-- Creator profile fields (display_name/handle/bio) live on the org row;
-- creator KYC captures an individual (full_name + national ID) instead of
-- business_name + TIN. All new columns are nullable/defaulted so existing
-- merchant rows read as account_kind='merchant'.

ALTER TABLE app.organizations
  ADD COLUMN IF NOT EXISTS account_kind TEXT NOT NULL DEFAULT 'merchant',
  ADD COLUMN IF NOT EXISTS display_name TEXT,
  ADD COLUMN IF NOT EXISTS handle TEXT,
  ADD COLUMN IF NOT EXISTS bio TEXT;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'organizations_account_kind_check'
  ) THEN
    ALTER TABLE app.organizations
      ADD CONSTRAINT organizations_account_kind_check
      CHECK (account_kind IN ('merchant', 'creator'));
  END IF;
END $$;

-- Case-insensitive unique handle for public support pages (/c/:handle).
CREATE UNIQUE INDEX IF NOT EXISTS organizations_handle_unique_idx
  ON app.organizations (lower(handle)) WHERE handle IS NOT NULL AND handle <> '';

-- Individual-KYC evidence for creator accounts. Merchant columns
-- (business_name/tin) stay untouched; creators fill these instead.
ALTER TABLE app.kyc_submissions
  ADD COLUMN IF NOT EXISTS full_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS id_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS id_number TEXT NOT NULL DEFAULT '';

ALTER TABLE app.kyc_submission_attempts
  ADD COLUMN IF NOT EXISTS full_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS id_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS id_number TEXT NOT NULL DEFAULT '';
