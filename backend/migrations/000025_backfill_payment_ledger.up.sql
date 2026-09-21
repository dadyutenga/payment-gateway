-- Migration: 000025_backfill_payment_ledger.up.sql
-- The ledger (000023) only started recording money from the moment that
-- code went live — every payment_orders row that had already settled to
-- 'paid' before then has no corresponding ledger entry, so every app's
-- derived balance reads 0.00 even though real payments happened. This
-- backfills exactly one payment_credit entry per already-paid order that's
-- missing one.
--
-- No platform_fee_debit is backfilled: every app's fee_percent/fee_fixed
-- default to 0 (fees didn't exist as a concept before Phase 1), so a fee
-- entry would be 0 anyway. Fees only ever apply to payments settling after
-- an admin configures them — this backfill doesn't change that.
--
-- Idempotent: safe to re-run, the NOT EXISTS guard means it only ever
-- inserts once per order.

INSERT INTO app.payment_ledger_entries (app_id, payment_order_id, entry_type, direction, amount, currency, description, created_at)
SELECT po.app_id, po.id, 'payment_credit', 'credit', po.amount, po.currency, 'Payment received (backfilled)', po.updated_at
FROM app.payment_orders po
WHERE po.status = 'paid'
  AND po.app_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM app.payment_ledger_entries le
    WHERE le.payment_order_id = po.id AND le.entry_type = 'payment_credit'
  );
