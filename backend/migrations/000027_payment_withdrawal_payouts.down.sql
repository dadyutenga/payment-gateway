-- Migration: 000027_payment_withdrawal_payouts.down.sql

DROP INDEX IF EXISTS app.payment_withdrawals_provider_payout_idx;

ALTER TABLE app.payment_withdrawals DROP CONSTRAINT payment_withdrawals_status_check;

ALTER TABLE app.payment_withdrawals ADD CONSTRAINT payment_withdrawals_status_check CHECK (status IN (
  'requested', 'approved', 'rejected', 'paid', 'failed'
));

ALTER TABLE app.payment_withdrawals
  DROP COLUMN IF EXISTS provider,
  DROP COLUMN IF EXISTS provider_payout_id,
  DROP COLUMN IF EXISTS provider_status,
  DROP COLUMN IF EXISTS failure_reason,
  DROP COLUMN IF EXISTS dispatched_at;
