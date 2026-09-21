-- Migration: 000026_payment_ledger_refunds.up.sql
-- Phase 4 of the AZSUBAY Gateway ledger: adds the two entry types needed to
-- reverse a payment that later refunds/reverses (chargeback, provider-side
-- refund). refund_debit reverses the original payment_credit;
-- refund_fee_reversal_credit reverses the original platform_fee_debit (so
-- AZsubay doesn't keep a fee on money that got refunded). Both are computed
-- from the amounts actually recorded at settlement time, never recalculated
-- from the app's current fee config.

ALTER TABLE app.payment_ledger_entries DROP CONSTRAINT payment_ledger_entries_entry_type_check;

ALTER TABLE app.payment_ledger_entries ADD CONSTRAINT payment_ledger_entries_entry_type_check CHECK (entry_type IN (
  'payment_credit', 'platform_fee_debit', 'withdrawal_debit', 'withdrawal_reversal_credit',
  'adjustment_credit', 'adjustment_debit', 'refund_debit', 'refund_fee_reversal_credit'
));
