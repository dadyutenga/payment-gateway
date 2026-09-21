-- Migration: 000001_bootstrap.down.sql

DROP TRIGGER IF EXISTS update_users_updated_at ON app.users;
DROP TABLE IF EXISTS app.users;
DROP FUNCTION IF EXISTS update_updated_at_column();
