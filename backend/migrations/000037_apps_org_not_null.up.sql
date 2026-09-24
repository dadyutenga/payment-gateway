-- Migration: 000037_apps_org_not_null.up.sql
-- Enforces the org linkage once 000036 has backfilled it. Fails loudly if
-- any app somehow missed backfill rather than leaving a NULL behind.

DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM app.payment_apps WHERE org_id IS NULL) THEN
    RAISE EXCEPTION 'payment_apps.org_id backfill incomplete: apps without org exist';
  END IF;
END
$$;

ALTER TABLE app.payment_apps
  ADD CONSTRAINT payment_apps_org_id_fkey
  FOREIGN KEY (org_id) REFERENCES app.organizations(id) ON DELETE RESTRICT;

ALTER TABLE app.payment_apps ALTER COLUMN org_id SET NOT NULL;
