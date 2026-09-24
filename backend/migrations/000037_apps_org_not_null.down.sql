-- Migration: 000037_apps_org_not_null.down.sql

ALTER TABLE app.payment_apps ALTER COLUMN org_id DROP NOT NULL;
ALTER TABLE app.payment_apps DROP CONSTRAINT IF EXISTS payment_apps_org_id_fkey;
