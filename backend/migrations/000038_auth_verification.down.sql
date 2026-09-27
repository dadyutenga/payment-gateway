-- Migration: 000038_auth_verification.down.sql

DROP TABLE IF EXISTS app.otp_codes;

ALTER TABLE app.users
  DROP COLUMN IF EXISTS phone_verified_at,
  DROP COLUMN IF EXISTS email_verified_at;
