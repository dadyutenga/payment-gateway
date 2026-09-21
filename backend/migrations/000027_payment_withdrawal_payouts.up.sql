-- Migration: 000027_payment_withdrawal_payouts.up.sql
-- Phase 5 of the AZSUBAY Gateway: automated payouts. Adds the columns
-- needed to track a dispatched disbursement (which provider, its payout
-- ID, last known provider status, and any failure reason) and a new
-- 'processing' withdrawal status meaning "dispatched to the provider,
-- awaiting confirmation" — it sits between 'approved' and 'paid'/'failed'.
--
-- The unique index on (provider, provider_payout_id) is the hard guard
-- against ever recording two payout attempts against the same
-- provider-side payout; application code additionally only ever dispatches
-- a withdrawal once (provider_payout_id IS NULL is required to dispatch).

ALTER TABLE app.payment_withdrawals
  ADD COLUMN IF NOT EXISTS provider TEXT,
  ADD COLUMN IF NOT EXISTS provider_payout_id TEXT,
  ADD COLUMN IF NOT EXISTS provider_status TEXT,
  ADD COLUMN IF NOT EXISTS failure_reason TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS dispatched_at TIMESTAMPTZ;

ALTER TABLE app.payment_withdrawals DROP CONSTRAINT payment_withdrawals_status_check;

ALTER TABLE app.payment_withdrawals ADD CONSTRAINT payment_withdrawals_status_check CHECK (status IN (
  'requested', 'approved', 'processing', 'rejected', 'paid', 'failed'
));

CREATE UNIQUE INDEX IF NOT EXISTS payment_withdrawals_provider_payout_idx
  ON app.payment_withdrawals (provider, provider_payout_id) WHERE provider_payout_id IS NOT NULL;
