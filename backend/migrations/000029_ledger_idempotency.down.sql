-- Migration: 000029_ledger_idempotency.down.sql

DROP INDEX IF EXISTS app.payment_ledger_refund_fee_reversal_once;
DROP INDEX IF EXISTS app.payment_ledger_refund_debit_once;
DROP INDEX IF EXISTS app.payment_ledger_platform_fee_once;
DROP INDEX IF EXISTS app.payment_ledger_payment_credit_once;
