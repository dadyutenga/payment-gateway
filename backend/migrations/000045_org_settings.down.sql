-- Migration: 000045_org_settings.down.sql

DROP TABLE IF EXISTS app.notification_prefs;
DROP TABLE IF EXISTS app.kyc_submission_attempts;
ALTER TABLE app.organizations
  DROP COLUMN IF EXISTS primary_color,
  DROP COLUMN IF EXISTS logo_url,
  DROP COLUMN IF EXISTS contact_email,
  DROP COLUMN IF EXISTS phone,
  DROP COLUMN IF EXISTS address;
