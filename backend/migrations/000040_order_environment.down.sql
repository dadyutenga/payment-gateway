-- Migration: 000040_order_environment.down.sql

DROP INDEX IF EXISTS app.payment_orders_env_status_idx;

ALTER TABLE app.payment_orders
  DROP COLUMN IF EXISTS environment;
