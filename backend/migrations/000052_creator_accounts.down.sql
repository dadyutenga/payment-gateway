-- Migration: 000052_creator_accounts.down.sql

DROP INDEX IF EXISTS app.organizations_handle_unique_idx;

ALTER TABLE app.kyc_submission_attempts
  DROP COLUMN IF EXISTS id_number,
  DROP COLUMN IF EXISTS id_type,
  DROP COLUMN IF EXISTS full_name;

ALTER TABLE app.kyc_submissions
  DROP COLUMN IF EXISTS id_number,
  DROP COLUMN IF EXISTS id_type,
  DROP COLUMN IF EXISTS full_name;

ALTER TABLE app.organizations DROP CONSTRAINT IF EXISTS organizations_account_kind_check;

ALTER TABLE app.organizations
  DROP COLUMN IF EXISTS bio,
  DROP COLUMN IF EXISTS handle,
  DROP COLUMN IF EXISTS display_name,
  DROP COLUMN IF EXISTS account_kind;
