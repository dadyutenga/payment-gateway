-- Migration: 000053_creator_survey.up.sql
-- Creator onboarding survey (Part 2): segmentation / default-risk-tier
-- input collected at creator signup. One row per creator org, upserted on
-- (re)submit. This is a UX/segmentation tool, NOT a security control:
-- nothing here raises live limits — those still gate on kyc_status.
-- The suggested starting risk tier is derived in code from the
-- self-reported bands (see SuggestedRiskTier in the orgs module), so the
-- mapping stays reviewable without a data migration.

CREATE TABLE IF NOT EXISTS app.creator_onboarding_surveys (
  org_id UUID PRIMARY KEY REFERENCES app.organizations(id) ON DELETE CASCADE,
  category TEXT NOT NULL DEFAULT '',
  category_other TEXT NOT NULL DEFAULT '',
  referral_source TEXT NOT NULL DEFAULT '',
  use_cases JSONB NOT NULL DEFAULT '[]'::jsonb,
  expected_volume_band TEXT NOT NULL DEFAULT '',
  expected_txn_band TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_creator_onboarding_surveys_updated_at
  BEFORE UPDATE ON app.creator_onboarding_surveys
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();
