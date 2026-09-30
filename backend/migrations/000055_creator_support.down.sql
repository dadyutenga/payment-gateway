-- Migration: 000055_creator_support.down.sql

DROP INDEX IF EXISTS app.creator_support_links_org_idx;
DROP TABLE IF EXISTS app.creator_support_links;
DROP TRIGGER IF EXISTS update_creator_support_settings_updated_at ON app.creator_support_settings;
DROP TABLE IF EXISTS app.creator_support_settings;
