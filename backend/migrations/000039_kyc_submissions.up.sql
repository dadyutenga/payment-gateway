-- Migration: 000039_kyc_submissions.up.sql
-- KYC/KYB review trail: one row per org, rewritten on resubmission
-- (review fields cleared). The live organizations.kyc_status stays the
-- enforced source of truth; this table is the evidence + review queue.

CREATE TABLE IF NOT EXISTS app.kyc_submissions (
  org_id UUID PRIMARY KEY REFERENCES app.organizations(id) ON DELETE CASCADE,
  business_name TEXT NOT NULL DEFAULT '',
  tin TEXT NOT NULL DEFAULT '',
  id_document_url TEXT NOT NULL DEFAULT '',
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  reviewed_by TEXT,
  reviewed_at TIMESTAMPTZ,
  rejection_reason TEXT NOT NULL DEFAULT ''
);
