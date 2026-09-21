-- Migration: 000030_delivery_lease.up.sql
-- Webhook deliveries could get stuck in 'processing' forever: claim set the
-- status with no lease, so a crash, a shutdown, or a cancelled context
-- (inline 25s attempt, worker shutdown) between claim and result-recording
-- left rows no worker would ever pick up again (claim only looked at
-- pending/retrying). Add a lease: claim stamps lease_expires_at, and the
-- claim query reclaims stale processing rows whose lease expired.

ALTER TABLE app.payment_webhook_deliveries
	ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ,
	ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;

-- Existing stuck 'processing' rows become immediately reclaimable.
UPDATE app.payment_webhook_deliveries
SET lease_expires_at = NOW()
WHERE status = 'processing' AND lease_expires_at IS NULL;

CREATE INDEX IF NOT EXISTS payment_webhook_deliveries_lease_idx
	ON app.payment_webhook_deliveries (status, lease_expires_at)
	WHERE status = 'processing';
