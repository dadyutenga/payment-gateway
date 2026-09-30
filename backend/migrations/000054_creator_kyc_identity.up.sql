-- Migration: 000054_creator_kyc_identity.up.sql
-- Individual KYC evidence for creator accounts (Part 3): date of birth
-- (18+ age-gate, validated at submit — never stored on a rejected
-- attempt beyond the submission row itself), optional back-side ID
-- document, and a v1 selfie photo (simple capture; true liveness
-- detection is future scope). Same tables/state machine as merchant KYC
-- (Phase 0 decision: reuse kyc_submissions, discriminator is
-- organizations.account_kind plus which columns are filled).

ALTER TABLE app.kyc_submissions
  ADD COLUMN IF NOT EXISTS dob DATE,
  ADD COLUMN IF NOT EXISTS id_document_back_url TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS selfie_url TEXT NOT NULL DEFAULT '';

ALTER TABLE app.kyc_submission_attempts
  ADD COLUMN IF NOT EXISTS dob DATE,
  ADD COLUMN IF NOT EXISTS id_document_back_url TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS selfie_url TEXT NOT NULL DEFAULT '';
