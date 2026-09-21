-- Migration: 000029_ledger_idempotency.up.sql
-- Second line of defence against double-crediting the same order.
-- ApplyWebhookEvent now locks the order row (SELECT ... FOR UPDATE) and
-- re-checks its status inside the transaction, so a webhook racing GetOrder's
-- auto-refresh or racing reconciliation serializes on the lock. These partial
-- unique indexes catch anything that ever slips through: at most one
-- payment_credit, one platform_fee_debit, one refund_debit and one
-- refund_fee_reversal_credit per order. Ledger inserts use ON CONFLICT DO
-- NOTHING, so the loser becomes a no-op instead of a double credit.

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_payment_credit_once
	ON app.payment_ledger_entries (payment_order_id)
	WHERE payment_order_id IS NOT NULL AND entry_type = 'payment_credit';

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_platform_fee_once
	ON app.payment_ledger_entries (payment_order_id)
	WHERE payment_order_id IS NOT NULL AND entry_type = 'platform_fee_debit';

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_refund_debit_once
	ON app.payment_ledger_entries (payment_order_id)
	WHERE payment_order_id IS NOT NULL AND entry_type = 'refund_debit';

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_refund_fee_reversal_once
	ON app.payment_ledger_entries (payment_order_id)
	WHERE payment_order_id IS NOT NULL AND entry_type = 'refund_fee_reversal_credit';
