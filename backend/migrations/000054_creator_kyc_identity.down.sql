-- Migration: 000054_creator_kyc_identity.down.sql

ALTER TABLE app.kyc_submission_attempts
  DROP COLUMN IF EXISTS selfie_url,
  DROP COLUMN IF EXISTS id_document_back_url,
  DROP COLUMN IF EXISTS dob;

ALTER TABLE app.kyc_submissions
  DROP COLUMN IF EXISTS selfie_url,
  DROP COLUMN IF EXISTS id_document_back_url,
  DROP COLUMN IF EXISTS dob;
