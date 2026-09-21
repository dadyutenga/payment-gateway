-- Migration: 000030_delivery_lease.down.sql

DROP INDEX IF EXISTS app.payment_webhook_deliveries_lease_idx;

ALTER TABLE app.payment_webhook_deliveries
	DROP COLUMN IF EXISTS lease_expires_at,
	DROP COLUMN IF EXISTS claimed_at;
