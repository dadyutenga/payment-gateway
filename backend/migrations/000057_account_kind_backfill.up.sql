-- Migration: 000057_account_kind_backfill.up.sql
-- One-time backfill for rows created through the merged signup flow, where
-- account_kind came from a client-supplied field (or the 'merchant'
-- default) instead of the endpoint called.
--
-- BACK UP BEFORE APPLYING TO REAL DATA:
--   pg_dump "$DATABASE_URL" -Fc -f lipago-pre-000057.dump
--
-- Rules (conservative — never guess on real accounts):
--   * UNAMBIGUOUS creator lookalikes flip to 'creator': the org carries
--     creator identity (handle, survey, or individual KYC) and NO business
--     identity anywhere (no org business_name/tin, no business KYC row).
--     Flipped rows are stamped account_kind_source='backfill_000057'.
--   * CONTRADICTORY rows (creator AND business identity on the same org)
--     are NOT touched — they are inserted into app.account_kind_review
--     for a human to resolve.
--   * Everything else stays 'merchant' (the safe default).
-- Re-running up is safe: flips only touch source='signup' rows and review
-- inserts are ON CONFLICT DO NOTHING.

ALTER TABLE app.organizations
  ADD COLUMN IF NOT EXISTS account_kind_source TEXT NOT NULL DEFAULT 'signup';

CREATE TABLE IF NOT EXISTS app.account_kind_review (
  org_id UUID PRIMARY KEY REFERENCES app.organizations (id) ON DELETE CASCADE,
  current_kind TEXT NOT NULL,
  detected_kind TEXT NOT NULL,
  signals TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unambiguous creator lookalikes: creator identity, zero business identity.
UPDATE app.organizations o
SET account_kind = 'creator',
    account_kind_source = 'backfill_000057',
    updated_at = NOW()
WHERE o.account_kind = 'merchant'
  AND o.account_kind_source = 'signup'
  AND (
    (COALESCE(o.handle, '') <> '')
    OR EXISTS (SELECT 1 FROM app.creator_onboarding_surveys sv WHERE sv.org_id = o.id)
    OR EXISTS (
      SELECT 1 FROM app.kyc_submissions s
      WHERE s.org_id = o.id AND COALESCE(s.full_name, '') <> ''
    )
  )
  AND COALESCE(o.business_name, '') = ''
  AND COALESCE(o.tin, '') = ''
  AND NOT EXISTS (
    SELECT 1 FROM app.kyc_submissions s
    WHERE s.org_id = o.id
      AND (COALESCE(s.business_name, '') <> '' OR COALESCE(s.tin, '') <> '')
  );

-- Contradictory rows: flag for manual resolution, never flip.
INSERT INTO app.account_kind_review (org_id, current_kind, detected_kind, signals)
SELECT
  o.id,
  o.account_kind,
  'ambiguous',
  'creator identity ('
    || TRIM(BOTH ', ' FROM
      (CASE WHEN COALESCE(o.handle, '') <> '' THEN 'handle, ' ELSE '' END)
      || (CASE WHEN EXISTS (SELECT 1 FROM app.creator_onboarding_surveys sv WHERE sv.org_id = o.id) THEN 'survey, ' ELSE '' END)
      || (CASE WHEN EXISTS (SELECT 1 FROM app.kyc_submissions s WHERE s.org_id = o.id AND COALESCE(s.full_name, '') <> '') THEN 'individual-kyc' ELSE '' END)
    )
    || ') + business identity ('
    || TRIM(BOTH ', ' FROM
      (CASE WHEN COALESCE(o.business_name, '') <> '' OR COALESCE(o.tin, '') <> '' THEN 'org-profile, ' ELSE '' END)
      || (CASE WHEN EXISTS (SELECT 1 FROM app.kyc_submissions s WHERE s.org_id = o.id AND (COALESCE(s.business_name, '') <> '' OR COALESCE(s.tin, '') <> '')) THEN 'business-kyc' ELSE '' END)
    )
    || ')'
FROM app.organizations o
WHERE o.account_kind_source IN ('signup', 'backfill_000057')
  AND (
    COALESCE(o.handle, '') <> ''
    OR EXISTS (SELECT 1 FROM app.creator_onboarding_surveys sv WHERE sv.org_id = o.id)
    OR EXISTS (SELECT 1 FROM app.kyc_submissions s WHERE s.org_id = o.id AND COALESCE(s.full_name, '') <> '')
  )
  AND (
    COALESCE(o.business_name, '') <> ''
    OR COALESCE(o.tin, '') <> ''
    OR EXISTS (SELECT 1 FROM app.kyc_submissions s WHERE s.org_id = o.id AND (COALESCE(s.business_name, '') <> '' OR COALESCE(s.tin, '') <> ''))
  )
ON CONFLICT (org_id) DO NOTHING;
