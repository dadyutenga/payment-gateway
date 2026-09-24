-- Migration: 000031_payment_refunds.down.sql

UPDATE app.payment_webhook_endpoints
SET event_types = array_remove(event_types, 'payment.refunded'),
    updated_at = NOW()
WHERE 'payment.refunded' = ANY(event_types);

-- Restored only when every order has at most one row of each type;
-- fails loudly otherwise instead of silently allowing duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_refund_debit_once
  ON app.payment_ledger_entries (payment_order_id)
  WHERE payment_order_id IS NOT NULL AND entry_type = 'refund_debit';

CREATE UNIQUE INDEX IF NOT EXISTS payment_ledger_refund_fee_reversal_once
  ON app.payment_ledger_entries (payment_order_id)
  WHERE payment_order_id IS NOT NULL AND entry_type = 'refund_fee_reversal_credit';

DROP TABLE IF EXISTS app.payment_refunds;
