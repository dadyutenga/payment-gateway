-- Migration: 000021_payment_provider_credentials.up.sql
-- Brings the previously-dead app.payment_provider_accounts table (present
-- since 000008_payments.up.sql, never referenced by any Go code) to life as
-- an admin-manageable, encrypted-at-rest payment provider registry, mirroring
-- the SMS provider system. Every other column needed already exists.

ALTER TABLE app.payment_provider_accounts
  ADD COLUMN IF NOT EXISTS credentials_encrypted TEXT NOT NULL DEFAULT '';
