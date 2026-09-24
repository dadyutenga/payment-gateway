-- Migration: 000036_org_backfill.down.sql
-- NOTE: rolling back discards the whole org graph (orgs, members, app
-- links) created by the up migration AND any orgs/members created through
-- the API afterwards. payment_app_members is untouched throughout, so
-- re-running the up migration rebuilds access from the legacy rows.

UPDATE app.payment_apps SET org_id = NULL;
ALTER TABLE app.payment_apps DROP COLUMN IF EXISTS org_id;
DELETE FROM app.org_members;
DELETE FROM app.organizations;
