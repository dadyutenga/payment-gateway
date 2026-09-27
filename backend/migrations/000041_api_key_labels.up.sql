-- Migration: 000041_api_key_labels.up.sql
-- Human-readable key labels for merchant self-service key management
-- ("production server", "staging"). Display-only: never secret, never
-- used in auth. Empty by default; set at creation or relabeled later.

ALTER TABLE app.payment_api_keys
  ADD COLUMN IF NOT EXISTS label TEXT NOT NULL DEFAULT '';
