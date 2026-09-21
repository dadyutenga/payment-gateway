-- Migration: 000023_payment_ledger.up.sql
-- Phase 1 of the AZSUBAY Gateway ledger/balance/withdrawal system.
--
-- Adds per-app fee configuration, an append-only ledger (the only source of
-- truth for balance — never a mutable "balance" column, always derived as
-- SUM(credit) - SUM(debit) per app_id, so it can never drift out of sync
-- with reality), and a withdrawal state machine. Admin-only for now: no
-- merchant self-service login exists yet, so withdrawals are recorded by an
-- admin on behalf of an app rather than self-requested.

ALTER TABLE app.payment_apps
  ADD COLUMN IF NOT EXISTS fee_type TEXT NOT NULL DEFAULT 'percentage' CHECK (fee_type IN ('fixed', 'percentage', 'hybrid')),
  ADD COLUMN IF NOT EXISTS fee_percent NUMERIC(6, 3) NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS fee_fixed NUMERIC(18, 2) NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS app.payment_withdrawals (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  amount NUMERIC(18, 2) NOT NULL CHECK (amount > 0),
  currency TEXT NOT NULL DEFAULT 'TZS',
  destination_type TEXT NOT NULL DEFAULT 'bank' CHECK (destination_type IN ('bank', 'mobile_money')),
  destination_details JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'requested' CHECK (status IN ('requested', 'approved', 'rejected', 'paid', 'failed')),
  requested_by TEXT NOT NULL,
  approved_by TEXT,
  notes TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payment_withdrawals_app_idx ON app.payment_withdrawals (app_id, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_withdrawals_status_idx ON app.payment_withdrawals (status);

CREATE TRIGGER update_payment_withdrawals_updated_at
  BEFORE UPDATE ON app.payment_withdrawals
  FOR EACH ROW
  EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE IF NOT EXISTS app.payment_ledger_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id UUID NOT NULL REFERENCES app.payment_apps(id) ON DELETE CASCADE,
  payment_order_id UUID REFERENCES app.payment_orders(id) ON DELETE SET NULL,
  withdrawal_id UUID REFERENCES app.payment_withdrawals(id) ON DELETE SET NULL,
  entry_type TEXT NOT NULL CHECK (entry_type IN (
    'payment_credit', 'platform_fee_debit', 'withdrawal_debit', 'withdrawal_reversal_credit',
    'adjustment_credit', 'adjustment_debit'
  )),
  direction TEXT NOT NULL CHECK (direction IN ('credit', 'debit')),
  amount NUMERIC(18, 2) NOT NULL CHECK (amount >= 0),
  currency TEXT NOT NULL DEFAULT 'TZS',
  description TEXT NOT NULL DEFAULT '',
  created_by TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS payment_ledger_entries_app_idx ON app.payment_ledger_entries (app_id, created_at DESC);
CREATE INDEX IF NOT EXISTS payment_ledger_entries_order_idx ON app.payment_ledger_entries (payment_order_id);
CREATE INDEX IF NOT EXISTS payment_ledger_entries_withdrawal_idx ON app.payment_ledger_entries (withdrawal_id);
