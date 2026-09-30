-- Migration: 000056_creator_payout_destinations.up.sql
-- Creator payout destinations (Part 5 risk posture): one saved
-- mobile-money destination per creator org, gated by OTP verification at
-- save time and a 24h cooling period on changes. Creator withdrawals must
-- target the effective destination — enforced in
-- payments.Service.CreateWithdrawal, so neither the merchant nor the
-- admin withdrawal path can bypass it.
--
-- name_match records the provider account-name lookup outcome. No
-- adapter implements the lookup today, so saves record 'unavailable'
-- explicitly (with the reason) rather than skipping silently; the
-- attested-name vs verified-identity comparison is the integrity check
-- available now.

CREATE TABLE IF NOT EXISTS app.creator_payout_destinations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id UUID NOT NULL UNIQUE REFERENCES app.organizations(id) ON DELETE CASCADE,
  provider TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  account_name TEXT NOT NULL DEFAULT '',
  name_match TEXT NOT NULL DEFAULT 'unavailable'
    CHECK (name_match IN ('unavailable', 'matched', 'mismatched')),
  name_match_detail TEXT NOT NULL DEFAULT '',
  otp_verified_at TIMESTAMPTZ,
  effective_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER update_creator_payout_destinations_updated_at
  BEFORE UPDATE ON app.creator_payout_destinations
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();
