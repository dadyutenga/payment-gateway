-- Migration: 000050_ttp_covering_index.down.sql

DROP INDEX IF EXISTS app.payment_orders_paid_ttp_idx;
