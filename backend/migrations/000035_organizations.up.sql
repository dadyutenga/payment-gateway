-- Migration: 000035_organizations.up.sql
-- Multi-tenancy foundation: organizations own apps, and org_members
-- replaces payment_app_members as the access source of truth. (The old
-- table is left in place untouched — see 000036 for the data move.)
-- kyc_status defaults pending; Block 2 drives its transitions.

CREATE TABLE IF NOT EXISTS app.organizations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  kyc_status TEXT NOT NULL DEFAULT 'pending'
    CHECK (kyc_status IN ('pending', 'submitted', 'verified', 'rejected')),
  business_name TEXT,
  tin TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_organizations_updated_at
  BEFORE UPDATE ON app.organizations
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

-- invited_by stores the inviter's user id (TEXT like payment_app_members
-- .added_by, which holds JWT subs). Invites require an existing account,
-- so user_id stays NOT NULL — there are no email-only rows.
CREATE TABLE IF NOT EXISTS app.org_members (
  org_id UUID NOT NULL REFERENCES app.organizations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL,
  role TEXT NOT NULL DEFAULT 'viewer'
    CHECK (role IN ('owner', 'finance', 'developer', 'viewer')),
  invited_by TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('invited', 'active')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (org_id, user_id)
);

CREATE INDEX IF NOT EXISTS org_members_user_idx ON app.org_members (user_id);
