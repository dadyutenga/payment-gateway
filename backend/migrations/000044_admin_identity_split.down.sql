-- Migration: 000044_admin_identity_split.down.sql
-- Restores the single-table identity: copies admin_users back onto
-- app.users as is_admin=true (matching by lower(email)), then drops
-- app.audit_log and app.admin_users.

UPDATE app.users u
SET is_admin = true
FROM app.admin_users a
WHERE lower(u.email) = lower(a.email);

-- Recreate operator accounts that never had a customer row (same email,
-- admin flag, KEEP the admin_users password hash).
INSERT INTO app.users (id, email, password_hash, is_admin, email_verified_at)
SELECT a.id, a.email, a.password_hash, true, NOW()
FROM app.admin_users a
WHERE NOT EXISTS (SELECT 1 FROM app.users u WHERE lower(u.email) = lower(a.email))
ON CONFLICT (id) DO NOTHING;

DROP TABLE IF EXISTS app.audit_log;
DROP TABLE IF EXISTS app.admin_users;
