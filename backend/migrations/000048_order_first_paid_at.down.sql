-- Migration: 000048_order_first_paid_at.down.sql

ALTER TABLE app.payment_orders DROP COLUMN IF EXISTS first_paid_at;
