-- Migration: 000023_payment_ledger.down.sql

DROP TABLE IF EXISTS app.payment_ledger_entries;
DROP TRIGGER IF EXISTS update_payment_withdrawals_updated_at ON app.payment_withdrawals;
DROP TABLE IF EXISTS app.payment_withdrawals;

ALTER TABLE app.payment_apps
  DROP COLUMN IF EXISTS fee_type,
  DROP COLUMN IF EXISTS fee_percent,
  DROP COLUMN IF EXISTS fee_fixed;
