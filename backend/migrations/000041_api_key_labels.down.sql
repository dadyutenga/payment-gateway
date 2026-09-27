-- Migration: 000041_api_key_labels.down.sql

ALTER TABLE app.payment_api_keys
  DROP COLUMN IF EXISTS label;
