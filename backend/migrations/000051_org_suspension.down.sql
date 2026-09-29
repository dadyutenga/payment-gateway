-- Migration: 000051_org_suspension.down.sql

ALTER TABLE app.organizations
  DROP COLUMN IF EXISTS suspended_at,
  DROP COLUMN IF EXISTS suspended_reason,
  DROP COLUMN IF EXISTS suspended;
