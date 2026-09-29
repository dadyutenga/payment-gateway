-- Migration: 000047_analytics_range_indexes.down.sql

DROP INDEX IF EXISTS app.payment_ledger_entries_created_idx;
DROP INDEX IF EXISTS app.payment_orders_env_created_idx;
