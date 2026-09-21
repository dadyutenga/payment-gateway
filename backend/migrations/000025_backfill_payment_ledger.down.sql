-- Migration: 000025_backfill_payment_ledger.down.sql
-- Only removes entries this backfill created (distinguished by description),
-- never touches ledger entries created by live webhook processing.

DELETE FROM app.payment_ledger_entries
WHERE entry_type = 'payment_credit' AND description = 'Payment received (backfilled)';
