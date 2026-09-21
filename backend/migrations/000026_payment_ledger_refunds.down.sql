-- Migration: 000026_payment_ledger_refunds.down.sql

ALTER TABLE app.payment_ledger_entries DROP CONSTRAINT payment_ledger_entries_entry_type_check;

ALTER TABLE app.payment_ledger_entries ADD CONSTRAINT payment_ledger_entries_entry_type_check CHECK (entry_type IN (
  'payment_credit', 'platform_fee_debit', 'withdrawal_debit', 'withdrawal_reversal_credit',
  'adjustment_credit', 'adjustment_debit'
));
